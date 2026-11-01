package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type nativeSecurityHost interface {
	Config() hostupgrade.Config
	Environment() map[string]string
	CheckRunning(context.Context, string) error
}

type nativeSecurityInstallation struct {
	Host         nativeSecurityHost
	Database     upgrade.Config
	Inspection   automaticInspection
	SourceBundle string
	SourceCache  string
}

type nativeSecurityObservation struct {
	Observation  securitymaintenance.Observation
	Installation nativeSecurityInstallation
	State        upgrade.State
	BinarySHA256 releaseidentity.Digest
}

type nativeSecurityPreparation struct {
	Proof       securitymaintenance.Preparation
	Route       automaticRoute
	SourceGuard nativeSecuritySourceGuard
}

type nativeSecurityDriver struct {
	configuration  string
	out            io.Writer
	load           func(context.Context) (nativeSecurityInstallation, error)
	prepareRelease func(context.Context, releaseidentity.Identity, string) (preparedSecurityRelease, error)
	verifySource   func(context.Context, nativeSecurityInstallation, upgrade.State) (preparedSecurityRelease, error)
	runUpgrade     func(context.Context, releaseidentity.Identity, string, nativeSecurityAuthorization, io.Writer) error
	executeHandoff func(context.Context, *AutomaticHandoff, io.Writer) error
	now            func() time.Time
	prepared       *nativeSecurityPreparation
}

func newNativeSecurityDriver(configuration string, out io.Writer) (securitymaintenance.Driver, error) {
	if configuration == "" {
		configuration = hostupgrade.ConfigPath
	}
	c, err := hostupgrade.LoadConfig(configuration)
	if err != nil {
		return nil, err
	}
	if _, err := hostupgrade.New(c); err != nil {
		return nil, err
	}
	if out == nil {
		out = io.Discard
	}
	driver := &nativeSecurityDriver{configuration: configuration, out: out,
		prepareRelease: prepareSecurityRelease, verifySource: verifyNativeSecuritySource, runUpgrade: runNativeSecurityUpgrade, executeHandoff: executeNativeSecurityHandoff, now: time.Now}
	driver.load = func(ctx context.Context) (nativeSecurityInstallation, error) {
		current, err := hostupgrade.LoadConfig(configuration)
		if err != nil {
			return nativeSecurityInstallation{}, err
		}
		if current != c {
			return nativeSecurityInstallation{}, errors.New("native deployment configuration changed; restart the security coordinator")
		}
		host, err := hostupgrade.New(current)
		if err != nil {
			return nativeSecurityInstallation{}, err
		}
		if err := host.Preflight(ctx); err != nil {
			return nativeSecurityInstallation{}, err
		}
		if err := host.PrepareCacheDirectory(filepath.Join(current.StateDirectory, "downloads")); err != nil {
			return nativeSecurityInstallation{}, err
		}
		bundle, err := host.LocalBundleDirectory()
		if err != nil {
			return nativeSecurityInstallation{}, err
		}
		environment := host.Environment()
		cfg, _, err := config.Load("upgrade", nil, func(key string) string { return environment[key] }, nil)
		if err != nil {
			return nativeSecurityInstallation{}, err
		}
		if cfg.Store.Engine != config.EngineSQLite {
			return nativeSecurityInstallation{}, errors.New("native security maintenance requires the local SQLite deployment contract")
		}
		path := cfg.Store.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(current.WorkingDirectory, path)
		}
		database := upgrade.Config{Engine: releaseidentity.SQLite, Path: path}
		return nativeSecurityInstallation{Host: host, Database: database, Inspection: automaticStore{database}, SourceBundle: bundle, SourceCache: filepath.Join(current.StateDirectory, "downloads")}, nil
	}
	return driver, nil
}

func (d *nativeSecurityDriver) Observe(ctx context.Context) (securitymaintenance.Observation, error) {
	observed, err := d.observe(ctx)
	return observed.Observation, err
}

func (d *nativeSecurityDriver) observe(ctx context.Context) (nativeSecurityObservation, error) {
	var result nativeSecurityObservation
	installation, err := d.load(ctx)
	if err != nil {
		return result, err
	}
	state, err := installation.Inspection.Control(ctx)
	if err != nil {
		return result, err
	}
	observed, err := securityControlObservation(state)
	if err != nil {
		return result, err
	}
	observed.Engine = installation.Database.Engine
	// Authenticate local source evidence independently of the checker build's
	// application version and migrations. No installed-version request is sent.
	source, err := d.verifySource(ctx, installation, state)
	if err != nil {
		return result, err
	}
	if source.Target.Identity != observed.Identity || source.Target.BinarySHA256.Validate() != nil ||
		state.ReleaseRootDigest != releaseidentity.Hash(source.Pinned.Root) {
		return result, errors.New("native source identity or installed release trust differs from independently verified evidence")
	}
	bundle, err := upgradebundle.Load(ctx, source.Target.BundleDirectory, source.Pinned, state.Floor)
	if err != nil {
		return result, err
	}
	if _, err := bundle.Release(observed.Identity); err != nil {
		return result, err
	}
	var claim upgradecompat.InstalledSource
	for _, candidate := range bundle.Sources(installation.Database.Engine) {
		if candidate.Identity.IsRelease() && candidate.Identity.Release == observed.Identity {
			if claim.Identity.IsRelease() {
				return result, errors.New("native source has ambiguous authenticated schema evidence")
			}
			claim = candidate
		}
	}
	if !claim.Identity.IsRelease() {
		return result, errors.New("installed native source lacks an authenticated engine schema")
	}
	actual, err := installation.Inspection.Installed(ctx, claim.Migrations)
	if err != nil {
		return result, err
	}
	digest, err := claim.Migrations.Digest()
	if err != nil || actual.Source != state.Applied || actual.SchemaDigest != claim.SchemaSHA256 || actual.MigrationDigest != digest ||
		actual.InstanceID != state.InstanceID || actual.RestoreEpoch != state.RestoreEpoch || actual.Ledger == nil || !reflect.DeepEqual(*actual.Ledger, state) {
		return result, errors.New("actual native source schema, migration inventory or recovery incarnation differs from verified release state")
	}
	if observed.Ready {
		if err := installation.Host.CheckRunning(ctx, string(source.Target.BinarySHA256)); err != nil {
			return result, fmt.Errorf("prove running native source: %w", err)
		}
	}
	current, err := installation.Inspection.Control(ctx)
	if err != nil || !reflect.DeepEqual(current, state) {
		return result, errors.Join(err, errors.New("native source changed during identity and readiness observation"))
	}
	return nativeSecurityObservation{Observation: observed, Installation: installation, State: state, BinarySHA256: source.Target.BinarySHA256}, nil
}

func verifyNativeSecuritySource(ctx context.Context, installation nativeSecurityInstallation, state upgrade.State) (preparedSecurityRelease, error) {
	var result preparedSecurityRelease
	if !state.Applied.IsRelease() || installation.SourceBundle == "" || installation.SourceCache == "" {
		return result, errors.New("native source proof unavailable: retain the installed release's local signed bundle and archive before enrollment")
	}
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return result, err
	}
	if state.TrustDomain != upgrade.Production || state.ReleaseRootDigest != releaseidentity.Hash(pinned.Root) {
		return result, errors.New("native installed release belongs to another production trust root")
	}
	installer, err := selfupdate.NewInstaller(selfupdate.Config{StateDir: installation.SourceCache,
		TrustRootBase64: base64.StdEncoding.EncodeToString(pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(pinned.RecoveryPublicKey)})
	if err != nil {
		return result, err
	}
	identity := state.Applied.Release
	prefix := "stable-"
	if identity.Profile == releaseidentity.NightlyV1 {
		prefix = "nightly-"
	}
	directory := filepath.Join(installation.SourceCache, prefix+string(identity.ManifestSHA256)+"-"+runtime.GOOS+"-"+runtime.GOARCH)
	prepared, err := installer.VerifyCachedRelease(ctx, selfupdate.PreparedNightly{Identity: identity, Directory: directory, BundleDirectory: installation.SourceBundle}, state.Floor)
	if err != nil {
		return result, fmt.Errorf("native source proof unavailable: local signed bundle/archive verification failed; retain or restore native release evidence before enrollment: %w", err)
	}
	bundle, err := upgradebundle.Load(ctx, installation.SourceBundle, pinned, state.Floor)
	if err != nil {
		return result, err
	}
	release, err := bundle.Release(identity)
	if err != nil {
		return result, err
	}
	return preparedSecurityRelease{Installer: installer, Pinned: pinned, Target: prepared, Bundle: bundle, Release: release}, nil
}

func (d *nativeSecurityDriver) Prepare(ctx context.Context, target securitymaintenance.Target) (securitymaintenance.Preparation, error) {
	d.prepared = nil
	var result securitymaintenance.Preparation
	if target.Identity.Validate() != nil || target.Image != "" || target.RuntimeImageSHA256 != "" {
		return result, errors.New("native security preparation requires an exact application release without an OCI image")
	}
	before, err := d.observe(ctx)
	if err != nil {
		return result, err
	}
	if !before.Observation.Healthy() {
		return result, errors.New("native security preparation requires an admitted healthy source")
	}
	previous, err := readAutomaticJournal(filepath.Join(before.Installation.Host.Config().StateDirectory, "operation.json"))
	if err != nil {
		return result, err
	}
	if previous != nil && previous.Phase != "complete" {
		return result, errors.New("unfinished native upgrade requires exact-target reconciliation before new security staging")
	}
	// Share the privileged host coordinator's verified cache. Staging is real
	// before the stop, without downloading a second copy during target handoff.
	prepared, err := d.prepareRelease(ctx, target.Identity, filepath.Join(before.Installation.Host.Config().StateDirectory, "downloads"))
	if err != nil {
		return result, err
	}
	if prepared.Target.Identity != target.Identity || before.State.ReleaseRootDigest != releaseidentity.Hash(prepared.Pinned.Root) {
		return result, errors.New("security target differs from independent native release authority")
	}
	route, err := stageNativeSecurityRoute(ctx, prepared, before.Installation)
	if err != nil {
		return result, err
	}
	manifest, err := route.Plan.SourceManifest(before.Installation.Database.Engine)
	if err != nil {
		return result, err
	}
	sourceDigest, err := manifest.Digest()
	if err != nil || route.Plan.Source() != before.State.Applied || route.Instance != before.State.InstanceID ||
		sourceDigest != before.State.MigrationDigest || route.Plan.SourceSchemaDigest() != before.State.SchemaDigest {
		return result, errors.New("prepared native route belongs to a different installed source")
	}
	impact, err := securityRouteImpact(route.Plan)
	if err != nil {
		return result, err
	}
	after, err := d.observe(ctx)
	if err != nil || !sameNativeSecurityObservation(before.Observation, after.Observation) {
		return result, errors.Join(err, errors.New("native source changed during security staging"))
	}
	result = securitymaintenance.Preparation{Target: target.Identity, Source: before.Observation, RouteSHA256: route.Plan.Digest(), Impact: impact}
	hostDigest, err := nativeSecurityHostFingerprint(before.Installation.Host.Config(), before.Installation.Host.Environment())
	if err != nil {
		return result, err
	}
	d.prepared = &nativeSecurityPreparation{Proof: result, Route: route, SourceGuard: nativeSecuritySourceGuard{Source: before.Observation, HostSHA256: hostDigest, BinarySHA256: before.BinarySHA256}}
	return result, nil
}

func stageNativeSecurityRoute(ctx context.Context, prepared preparedSecurityRelease, installation nativeSecurityInstallation) (automaticRoute, error) {
	if prepared.Installer == nil || prepared.Source == nil {
		return automaticRoute{}, errors.New("native security route requires independent release preparation")
	}
	route, err := discoverReleaseRoute(ctx, unattendedRouteInstaller{prepared.Installer}, prepared.Source, prepared.Target, prepared.Pinned,
		installation.Inspection, installation.Database.Engine, nil, true)
	if err != nil {
		return route, err
	}
	if !route.Plan.Valid() || route.Plan.Target() != prepared.Target.Identity || len(route.Plan.Steps()) == 0 {
		return route, errors.New("native security target has no applicable authenticated upgrade route")
	}
	return route, preflightUnattendedRoute(ctx, route)
}

func sameNativeSecurityObservation(a, b securitymaintenance.Observation) bool {
	return a.Healthy() && b.Healthy() && a.Platform == b.Platform && a.Engine == b.Engine && a.Identity == b.Identity && a.Instance == b.Instance &&
		a.Generation == b.Generation && a.RestoreEpoch == b.RestoreEpoch && a.Incarnation == b.Incarnation
}

func (d *nativeSecurityDriver) Apply(ctx context.Context, target securitymaintenance.Target) error {
	if d.prepared == nil || target.Identity != d.prepared.Proof.Target || target.Image != "" || target.RuntimeImageSHA256 != "" {
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("native security application requires the exact independently staged target"))
	}
	if target.EligibilityUntil.IsZero() {
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("native security application requires verified feed eligibility expiry"))
	}
	before, err := d.Observe(ctx)
	if err != nil || !sameNativeSecurityObservation(d.prepared.Proof.Source, before) {
		return errors.Join(securitymaintenance.ErrNotStarted, err, errors.New("native source changed before security application"))
	}
	if err := preflightUnattendedRoute(ctx, d.prepared.Route); err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	if err := authorizeAutomaticMutation(target.EligibilityUntil, d.now, target.BeforeMutation); err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	err = d.runUpgrade(ctx, target.Identity, d.configuration, nativeSecurityAuthorization{Route: d.prepared.Proof.RouteSHA256, Until: target.EligibilityUntil, PolicyFile: target.PolicyFile, PolicySHA256: target.PolicySHA256, Source: &d.prepared.SourceGuard}, d.out)
	var handoff *AutomaticHandoff
	if errors.As(err, &handoff) {
		err = d.executeHandoff(ctx, handoff, d.out)
	}
	return err
}

func executeNativeSecurityHandoff(ctx context.Context, handoff *AutomaticHandoff, out io.Writer) error {
	command := exec.CommandContext(ctx, handoff.Executable, handoff.Arguments...)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	command.Stdout, command.Stderr = out, out
	return AutomaticUpgradeHandoffError(command.Run())
}
