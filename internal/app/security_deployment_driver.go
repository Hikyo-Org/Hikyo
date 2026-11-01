package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type securityDeploymentDriver struct {
	executor   securityDeploymentExecutor
	cache      string
	repository string
	prepared   map[releaseidentity.Identity]securitydeployment.Target
	routes     map[releaseidentity.Identity]releaseidentity.Digest
	sources    map[releaseidentity.Identity]securitymaintenance.Observation
	timeout    time.Duration
}

type securityDeploymentExecutor interface {
	Observe(context.Context) (securitydeployment.Observation, error)
	Prepare(context.Context, securitydeployment.Target) (securitydeployment.Observation, error)
	Apply(context.Context, securitydeployment.Target) error
}

func newSecurityDeploymentDriver(c securitydeployment.Config, cache string) (*securityDeploymentDriver, error) {
	executor, err := securitydeployment.New(c)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(c.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	return &securityDeploymentDriver{executor: executor, cache: cache, repository: c.ImageRepository, prepared: map[releaseidentity.Identity]securitydeployment.Target{}, routes: map[releaseidentity.Identity]releaseidentity.Digest{}, sources: map[releaseidentity.Identity]securitymaintenance.Observation{}, timeout: timeout}, nil
}

func maintenanceObservation(o securitydeployment.Observation) securitymaintenance.Observation {
	return securitymaintenance.Observation{Platform: o.Platform, Engine: o.Engine, Identity: o.Identity, StagedTarget: o.StagedTarget, Image: o.Image, RuntimeImageSHA256: releaseidentity.Digest(o.RuntimeImageSHA256),
		Instance: o.Instance, Generation: o.Generation, RestoreEpoch: o.RestoreEpoch, Incarnation: o.Incarnation, Phase: o.Phase, Maintenance: o.Maintenance, Ready: o.Ready,
		RouteSHA256: o.RouteSHA256, RequiresMaintenance: o.RequiresMaintenance, SchemaChanges: o.SchemaChanges}
}

func (d *securityDeploymentDriver) Observe(ctx context.Context) (securitymaintenance.Observation, error) {
	observed, err := d.executor.Observe(ctx)
	return maintenanceObservation(observed), err
}

func signedSecurityImage(release releasetrust.VerifiedRelease, target securitymaintenance.Target) error {
	if !release.Valid() || release.Identity() != target.Identity || target.Image == "" {
		return errors.New("security image requires an independently authenticated exact release")
	}
	for _, artifact := range release.Artifacts() {
		if artifact.Kind == "image" && artifact.Image+"@"+artifact.Digest == target.Image {
			return nil
		}
	}
	return errors.New("security image digest is absent from independently signed release inventory")
}

func (d *securityDeploymentDriver) Prepare(ctx context.Context, target securitymaintenance.Target) (securitymaintenance.Preparation, error) {
	var result securitymaintenance.Preparation
	if !strings.HasPrefix(target.Image, d.repository+"@sha256:") {
		return result, errors.New("security feed cannot change the installed image repository")
	}
	prepared, err := prepareSecurityRelease(ctx, target.Identity, d.cache)
	if err != nil {
		return result, err
	}
	if err := signedSecurityImage(prepared.Release, target); err != nil {
		return result, err
	}
	runtimeDigest, err := resolveSecurityImagePlatform(ctx, target.Image, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return result, err
	}
	if target.RuntimeImageSHA256 != "" && string(target.RuntimeImageSHA256) != runtimeDigest {
		return result, errors.New("feed runtime digest differs from authenticated image index")
	}
	request := securitydeployment.Target{Identity: target.Identity, Image: target.Image, RuntimeImageSHA256: runtimeDigest}
	observed, err := d.executor.Prepare(ctx, request)
	if err != nil {
		return result, err
	}
	if observed.StagedTarget != target.Identity || observed.RouteSHA256.Validate() != nil {
		return result, errors.New("image preparation did not prove exact target and route")
	}
	d.prepared[target.Identity] = request
	d.routes[target.Identity] = observed.RouteSHA256
	d.sources[target.Identity] = maintenanceObservation(observed)
	return securitymaintenance.Preparation{Target: target.Identity, Source: maintenanceObservation(observed), Image: target.Image, RouteSHA256: observed.RouteSHA256,
		Impact: securitymaintenance.Impact{Restart: true, Schema: observed.SchemaChanges, Maintenance: observed.RequiresMaintenance}}, nil
}

func (d *securityDeploymentDriver) Apply(ctx context.Context, target securitymaintenance.Target) error {
	request, ok := d.prepared[target.Identity]
	if !ok || request.Image != target.Image {
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("security image has not completed authenticated preparation"))
	}
	// Revalidate executable authorization immediately before workload mutation,
	// independently of the separate bulletin/TUF freshness check.
	prepared, err := prepareSecurityRelease(ctx, target.Identity, d.cache)
	if err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	if err := signedSecurityImage(prepared.Release, target); err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	if err := d.applyAuthorizedDeployment(ctx, target, request); err != nil {
		return err
	}
	// Runtime caches and recovery evidence live with the installation. This
	// separate selector cache keeps only its current verified release payload.
	return prepared.Installer.PruneNightlyCache(ctx, target.Identity)
}

// The caller independently authenticates the release before this final
// deployment-owner validation. Re-staging cannot adopt another source tuple.
func (d *securityDeploymentDriver) applyAuthorizedDeployment(ctx context.Context, target securitymaintenance.Target, request securitydeployment.Target) error {
	// Reauthenticate every staged route hop using the target image and current
	// release snapshot before replacement, then preserve the measured route.
	staged, err := d.executor.Prepare(ctx, request)
	if err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	if staged.RouteSHA256 != d.routes[target.Identity] || staged.StagedTarget != target.Identity || !samePreparedSecuritySource(maintenanceObservation(staged), d.sources[target.Identity]) {
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("security route authorization changed after policy evaluation"))
	}
	request.EligibilityUntil = target.EligibilityUntil
	request.BeforeMutation = target.BeforeMutation
	err = d.executor.Apply(ctx, request)
	if errors.Is(err, securitydeployment.ErrEligibilityExpired) || errors.Is(err, securitydeployment.ErrPreconditionRefused) {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	if err != nil {
		return err
	}
	return d.waitForSecurityTarget(ctx, target, d.sources[target.Identity])
}

func (d *securityDeploymentDriver) waitForSecurityTarget(ctx context.Context, target securitymaintenance.Target, source securitymaintenance.Observation) error {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		observed, err := d.executor.Observe(ctx)
		if err == nil {
			running := maintenanceObservation(observed)
			if running.Instance != source.Instance || running.Incarnation != source.Incarnation || running.RestoreEpoch != source.RestoreEpoch {
				return errors.New("deployment recovery incarnation changed during replacement")
			}
			if running.Phase == "restore-required" {
				return errors.New("target requires explicit post-write recovery")
			}
			if running.Healthy() && running.Identity == target.Identity && running.Image == target.Image && running.Generation > source.Generation {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("timed out waiting for exact running security target; preserve deployment recovery journal")
		case <-ticker.C:
		}
	}
}

type boundedRegistryTransport struct{ base http.RoundTripper }

func samePreparedSecuritySource(a, b securitymaintenance.Observation) bool {
	return a.Platform == b.Platform && a.Engine == b.Engine && a.Identity == b.Identity && a.Instance == b.Instance && a.Generation == b.Generation && a.RestoreEpoch == b.RestoreEpoch && a.Incarnation == b.Incarnation && a.Image == b.Image && a.RuntimeImageSHA256 == b.RuntimeImageSHA256
}

func (t boundedRegistryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response.ContentLength > 4<<20 {
		response.Body.Close()
		return nil, errors.New("registry metadata exceeds 4 MiB")
	}
	response.Body = &boundedRegistryBody{ReadCloser: response.Body, remaining: 4 << 20}
	return response, nil
}

type boundedRegistryBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedRegistryBody) Read(p []byte) (int, error) {
	if b.remaining < 0 {
		return 0, errors.New("registry metadata exceeds 4 MiB")
	}
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		return 0, errors.New("registry metadata exceeds 4 MiB")
	}
	return n, err
}

// A signed OCI index binds its platform manifest digests. The runtime digest
// must be derived from those authenticated bytes, never asserted by a bulletin.
func resolveSecurityImagePlatform(ctx context.Context, image, goos, goarch string) (string, error) {
	ref, err := name.NewDigest(image, name.StrictValidation)
	if err != nil {
		return "", errors.New("invalid immutable security image")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	descriptor, err := remote.Get(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithUserAgent("hikyo-security-maintenance/1"), remote.WithTransport(boundedRegistryTransport{base: http.DefaultTransport}))
	if err != nil {
		return "", errors.New("could not verify signed image platform inventory")
	}
	if "sha256:"+string(releaseidentity.Hash(descriptor.Manifest)) != ref.DigestStr() {
		return "", errors.New("registry manifest differs from signed image digest")
	}
	if descriptor.MediaType.IsIndex() {
		return securityIndexPlatform(descriptor.Manifest, goos, goarch)
	}
	if !descriptor.MediaType.IsImage() {
		return "", errors.New("security image is not a supported image or index")
	}
	img, err := descriptor.Image()
	if err != nil {
		return "", err
	}
	configuration, err := img.ConfigFile()
	if err != nil || configuration.OS != goos || configuration.Architecture != goarch {
		return "", errors.New("security image platform differs from installed executor")
	}
	return strings.TrimPrefix(ref.DigestStr(), "sha256:"), nil
}

func securityIndexPlatform(raw []byte, goos, goarch string) (string, error) {
	index, err := v1.ParseIndexManifest(bytes.NewReader(raw))
	if err != nil || len(index.Manifests) == 0 || len(index.Manifests) > 256 {
		return "", errors.New("invalid or oversized security image index")
	}
	matched := ""
	for _, manifest := range index.Manifests {
		if manifest.Platform == nil || manifest.Platform.OS != goos || manifest.Platform.Architecture != goarch {
			continue
		}
		if matched != "" || manifest.Digest.Algorithm != "sha256" || releaseidentity.Digest(manifest.Digest.Hex).Validate() != nil || !manifest.MediaType.IsImage() {
			return "", errors.New("ambiguous or unsupported image platform manifest")
		}
		matched = manifest.Digest.Hex
	}
	if matched == "" {
		return "", errors.New("signed image index has no installed platform")
	}
	return matched, nil
}
