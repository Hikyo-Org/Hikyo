package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/securitystatus"
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
		timeout = securitydeployment.DefaultTimeout
	}
	return &securityDeploymentDriver{executor: executor, cache: cache, repository: c.ImageRepository, prepared: map[releaseidentity.Identity]securitydeployment.Target{}, routes: map[releaseidentity.Identity]releaseidentity.Digest{}, sources: map[releaseidentity.Identity]securitymaintenance.Observation{}, timeout: timeout}, nil
}

func maintenanceObservation(o securitydeployment.Observation) securitymaintenance.Observation {
	return securitymaintenance.Observation{Platform: o.Platform, Engine: o.Engine, Identity: o.Identity, StagedTarget: o.StagedTarget, Image: o.Image, RuntimeImageSHA256: releaseidentity.Digest(o.RuntimeImageSHA256),
		Instance: o.Instance, Generation: o.Generation, RestoreEpoch: o.RestoreEpoch, Incarnation: o.Incarnation, Phase: o.Phase, Maintenance: o.Maintenance, Ready: o.Ready,
		RouteSHA256: o.RouteSHA256, RequiresMaintenance: o.RequiresMaintenance, SchemaChanges: o.SchemaChanges, AbortedBeforeMutation: o.AbortedBeforeMutation}
}

func deploymentObservation(o securitymaintenance.Observation) securitydeployment.Observation {
	return securitydeployment.Observation{Platform: o.Platform, Engine: o.Engine, Identity: o.Identity, StagedTarget: o.StagedTarget, Image: o.Image, RuntimeImageSHA256: string(o.RuntimeImageSHA256),
		Instance: o.Instance, Generation: o.Generation, RestoreEpoch: o.RestoreEpoch, Incarnation: o.Incarnation, Phase: o.Phase, Maintenance: o.Maintenance, Ready: o.Ready,
		RouteSHA256: o.RouteSHA256, RequiresMaintenance: o.RequiresMaintenance, SchemaChanges: o.SchemaChanges, AbortedBeforeMutation: o.AbortedBeforeMutation}
}

func (d *securityDeploymentDriver) Observe(ctx context.Context) (securitymaintenance.Observation, error) {
	observed, err := d.executor.Observe(ctx)
	return maintenanceObservation(observed), err
}

// Reconcile releases a deployment recovery pin once observed is the exact healthy
// source. Optional on the seam because only the real executor owns a journal.
func (d *securityDeploymentDriver) Reconcile(ctx context.Context, observed securitymaintenance.Observation) error {
	owner, ok := d.executor.(interface {
		Reconcile(context.Context, securitydeployment.Observation) error
	})
	if !ok {
		return errors.New("deployment owner cannot reconcile an interrupted replacement")
	}
	if err := owner.Reconcile(ctx, deploymentObservation(observed)); err != nil {
		if errors.Is(err, securitydeployment.ErrFollowsRollback) {
			return securitymaintenance.ErrRolledBack
		}
		return err
	}
	return nil
}

// RestoreAborted is the coordinator's answer, on any later run, to a target that
// reports it stopped before the database was written. The pinned source comes
// from the persisted operation because this process did not prepare it. A result
// that is not ErrNotStarted keeps every pin and is asked again.
func (d *securityDeploymentDriver) RestoreAborted(ctx context.Context, aborted securitymaintenance.Observation, operation securitymaintenance.Operation) error {
	source := operation.Source
	if !aborted.AbortedBeforeMutation {
		// The engine's deadline passed with the target neither healthy nor aborted.
		return d.rollBackToSource(ctx, source)
	}
	return d.restoreAbortedSource(ctx, deploymentObservation(aborted), source)
}

// NoFinalGate proves it for an unobservable container target by reading the control row
// through a one-off container or Pod, or from the journal of a rollback that already began.
// An executor that cannot, or a row that cannot be read, leaves the pin for a person.
func (d *securityDeploymentDriver) NoFinalGate(ctx context.Context, operation securitymaintenance.Operation) (bool, error) {
	owner, ok := d.executor.(interface {
		NoFinalGate(context.Context, securitydeployment.Observation) (bool, error)
	})
	if !ok {
		return false, fmt.Errorf("%w: the container target cannot be read, so the control row is unknown", securitydeployment.ErrUnsupported)
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	return owner.NoFinalGate(ctx, deploymentObservation(operation.Source))
}

// deploymentDriverOf finds the deployment driver under the policy guard, if any.
func deploymentDriverOf(d securitymaintenance.Driver) *securityDeploymentDriver {
	if bound, ok := d.(policyBoundSecurityDriver); ok {
		d = bound.Driver
	}
	driver, _ := d.(*securityDeploymentDriver)
	return driver
}

// PinIdentity records the Deployment identity at explicit enrollment. Executors
// that need no pin (Compose, ECS) do not implement it.
func (d *securityDeploymentDriver) PinIdentity(ctx context.Context) error {
	if owner, ok := d.executor.(interface{ PinIdentity(context.Context) error }); ok {
		return owner.PinIdentity(ctx)
	}
	return nil
}

// repinIfRemoved lets an enrolled installation recover a replaced Deployment: the
// runbook removes the pin, and only enroll can record it again.
func (d *securityDeploymentDriver) repinIfRemoved(ctx context.Context) error {
	if d == nil {
		return nil
	}
	owner, ok := d.executor.(interface{ IdentityPinned() (bool, error) })
	if !ok {
		return nil
	}
	pinned, err := owner.IdentityPinned()
	if err != nil || pinned {
		return err
	}
	return d.PinIdentity(ctx)
}

// PublishStatus copies the public status file into the cluster for the serving
// Pod. It is a courtesy: a failure is logged and never fails maintenance. Safe
// on a nil driver.
func (d *securityDeploymentDriver) PublishStatus(ctx context.Context, file string) {
	if d == nil || file == "" {
		return
	}
	owner, ok := d.executor.(interface {
		PublishStatus(context.Context, []byte) error
	})
	if !ok {
		return
	}
	raw, err := readBounded(file, securitystatus.MaxBytes)
	if err == nil {
		err = owner.PublishStatus(ctx, raw)
	}
	if err != nil {
		slog.Warn("security maintenance status was not copied to the cluster", "err", err)
	}
}

// FollowCoordinator moves the in-cluster coordinator's CronJob to image. Only
// the follower calls it, after authenticating the image against its signed release.
func (d *securityDeploymentDriver) FollowCoordinator(ctx context.Context, image string) error {
	owner, ok := d.executor.(interface {
		FollowCoordinator(context.Context, string) error
	})
	if !ok {
		return securitydeployment.ErrUnsupported
	}
	return owner.FollowCoordinator(ctx, image)
}

func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(raw)) > limit {
		err = errors.New("status exceeds its size bound")
	}
	return raw, err
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
	// separate selector cache keeps only its current verified release payload. The
	// update is already proven, so leftover cache must not report it as failed; the
	// next successful update prunes again.
	_ = prepared.Installer.PruneNightlyCache(ctx, target.Identity)
	return nil
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

func (d *securityDeploymentDriver) waitForSecurityTarget(parent context.Context, target securitymaintenance.Target, source securitymaintenance.Observation) error {
	ctx, cancel := context.WithTimeout(parent, d.timeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		observed, err := d.executor.Observe(ctx)
		if err == nil {
			if observed.AbortedBeforeMutation {
				if err := d.restoreAbortedSource(parent, observed, source); !errors.Is(err, securitydeployment.ErrUnsupported) {
					return err
				}
			}
			running := maintenanceObservation(observed)
			if running.Instance != source.Instance || running.Incarnation != source.Incarnation || running.RestoreEpoch != source.RestoreEpoch {
				return errors.New("deployment recovery incarnation changed during replacement")
			}
			if running.Phase == "restore-required" {
				return d.rollBackOrReport(parent, source, errors.New("target requires explicit post-write recovery"))
			}
			if running.Healthy() && running.Identity == target.Identity && running.Image == target.Image && running.Generation > source.Generation {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return d.rollBackAtDeadline(parent, source, errors.New("timed out waiting for exact running security target; preserve deployment recovery journal"))
		case <-ticker.C:
		}
	}
}

// rollBackAtDeadline reads the target afresh before stopping it: the wait's last read may be old,
// or may never have succeeded. Healthy at another release, ready or not, may have served, so a
// person decides. A target that cannot be read is stopped only if the owner proves no gate was
// crossed. An owner with no such proof has no rollback either, and keeps the pins as before.
func (d *securityDeploymentDriver) rollBackAtDeadline(parent context.Context, source securitymaintenance.Observation, cause error) error {
	if parent.Err() != nil {
		return cause
	}
	// shortcut: a target can cross the gate between this read and the stop, and rule 2 then refuses with it stopped; upgrade by stopping under a lock that excludes the gate.
	read, cancel := context.WithTimeout(parent, d.timeout)
	observed, err := d.executor.Observe(read)
	cancel()
	if err == nil {
		running := maintenanceObservation(observed)
		if running.Instance != source.Instance || running.Incarnation != source.Incarnation || running.RestoreEpoch != source.RestoreEpoch {
			return errors.Join(cause, securitymaintenance.ErrPastFinalGate, errors.New("deployment recovery incarnation changed during replacement"))
		}
		if observed.PastFinalGate(source.Identity) {
			return errors.Join(cause, securitymaintenance.ErrPastFinalGate)
		}
	} else {
		// A hung read must not spend the proof's window: NoFinalGate bounds itself.
		proven, proofErr := d.NoFinalGate(parent, securitymaintenance.Operation{Source: source})
		if errors.Is(proofErr, securitydeployment.ErrUnsupported) {
			return cause
		}
		if proofErr != nil || !proven {
			return errors.Join(cause, securitymaintenance.ErrPastFinalGate, proofErr)
		}
	}
	return d.rollBackOrReport(parent, source, cause)
}

// rollBackOrReport answers a target that did not become healthy, using the caller's
// context because the wait's own deadline has ended. An owner without a rollback
// path keeps the pins and reports cause, as before.
func (d *securityDeploymentDriver) rollBackOrReport(parent context.Context, source securitymaintenance.Observation, cause error) error {
	if parent.Err() != nil {
		return cause
	}
	err := d.rollBackToSource(parent, source)
	if errors.Is(err, securitymaintenance.ErrNotStarted) {
		return err
	}
	if !errors.Is(err, securitydeployment.ErrUnsupported) {
		return errors.Join(cause, err)
	}
	return cause
}

// rollBackToSource has the owner stop the target, restore the fenced snapshot and
// write the source back, then requires the exact source, at the generation observed
// before the replacement, to be healthy. Only then is the pin released. parent carries
// shutdown but no deadline; each step gets its own window from it.
func (d *securityDeploymentDriver) rollBackToSource(parent context.Context, source securitymaintenance.Observation) error {
	owner, ok := d.executor.(interface {
		RollBack(context.Context) (bool, error)
	})
	if !ok {
		return securitydeployment.ErrUnsupported
	}
	rollback, cancel := context.WithTimeout(parent, d.timeout)
	restored, err := owner.RollBack(rollback)
	cancel()
	if err != nil {
		return err
	}
	// The source's start gets its own window: the one-shot may have used all of its own.
	ctx, cancel := context.WithTimeout(parent, d.timeout)
	defer cancel()
	outcome := errors.New("the target had written nothing to restore; the source image is serving again")
	if restored {
		outcome = securitymaintenance.ErrRolledBack
	}
	return d.awaitSource(ctx, source, outcome)
}

// restoreAbortedSource answers a target that reports it stopped before the live
// database was written: the source image returns, must be observed untouched and
// healthy, and only then is the deployment pin released and the attempt refused.
// A source that does not return, or came back changed, keeps every pin. Like
// rollBackToSource, the restore and the wait each get their own window from parent.
func (d *securityDeploymentDriver) restoreAbortedSource(parent context.Context, aborted securitydeployment.Observation, source securitymaintenance.Observation) error {
	owner, ok := d.executor.(interface {
		RestoreSource(context.Context, securitydeployment.Observation) error
	})
	if !ok {
		return securitydeployment.ErrUnsupported
	}
	restore, cancel := context.WithTimeout(parent, d.timeout)
	err := owner.RestoreSource(restore, aborted)
	cancel()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, d.timeout)
	defer cancel()
	return d.awaitSource(ctx, source, errors.New("the target stopped before the database was written; the source image is serving again"))
}

func (d *securityDeploymentDriver) awaitSource(ctx context.Context, source securitymaintenance.Observation, outcome error) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		observed, err := d.executor.Observe(ctx)
		if err == nil {
			if running := maintenanceObservation(observed); running.Healthy() && samePreparedSecuritySource(running, source) {
				// The outcome was decided by the one-shot's report; the marker the rollback left only says it ran.
				if err := d.Reconcile(ctx, running); err != nil && !errors.Is(err, securitymaintenance.ErrRolledBack) {
					return err
				}
				return errors.Join(securitymaintenance.ErrNotStarted, securitymaintenance.ErrSourceRestored, outcome)
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("stopped waiting for the source image after the target was stopped (%w); preserve deployment recovery journal", ctx.Err())
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
