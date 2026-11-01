package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/gofrs/flock"
)

const securityStageFormat = "hikyo.security-stage/v1"

type securityImageStage struct {
	Format      string                          `json:"format"`
	Target      selfupdate.PreparedNightly      `json:"target"`
	Source      securitymaintenance.Observation `json:"source"`
	Route       releaseidentity.Digest          `json:"route"`
	Executables []selfupdate.PreparedNightly    `json:"executables"`
}

func securityDatabase(cfg *config.Config) upgrade.Config {
	return upgrade.Config{Engine: releaseidentity.Engine(cfg.Store.Engine), Path: cfg.Store.Path, DSN: cfg.Store.DSN}
}

func securityControlObservation(state upgrade.State) (securitymaintenance.Observation, error) {
	if state.Validate() != nil || state.TrustDomain != upgrade.Production || !state.Applied.IsRelease() || state.Pending == nil {
		return securitymaintenance.Observation{}, errors.New("security maintenance requires an admitted production release")
	}
	return securitymaintenance.Observation{Platform: runtime.GOOS + "/" + runtime.GOARCH, Identity: state.Applied.Release, Instance: state.InstanceID, Generation: state.Generation, RestoreEpoch: state.RestoreEpoch,
		Incarnation: fmt.Sprintf("%x", state.RecoveryIncarnation[:]), Phase: string(state.Pending.Phase), Maintenance: state.Maintenance,
		Ready: !state.Maintenance && state.Pending.Phase == upgrade.Healthy && !state.Pending.Invalidated}, nil
}

// inspectSecurityImage is operator-local. It checks the real catalog against
// this binary's embedded migration identity; public runtime/status stays small.
func inspectSecurityImage(ctx context.Context, cfg *config.Config) (securitymaintenance.Observation, error) {
	raw, declaration, err := buildcompat.Current()
	if err != nil {
		return securitymaintenance.Observation{}, err
	}
	state, err := upgrade.InspectControl(ctx, securityDatabase(cfg))
	if err != nil {
		return securitymaintenance.Observation{}, err
	}
	observed, err := securityControlObservation(state)
	if err != nil {
		return observed, err
	}
	observed.Engine = releaseidentity.Engine(cfg.Store.Engine)
	id := observed.Identity
	if id.Profile != declaration.Profile || id.Version != declaration.Version || id.Sequence != declaration.Sequence || id.Commit != declaration.Commit || id.CompatibilitySHA256 != releaseidentity.Hash(raw) {
		return observed, errors.New("running binary declaration differs from applied release")
	}
	manifest, err := releaseidentity.BuildMigrationManifest(store.MigrationsFS, "migrations/"+string(cfg.Store.Engine), releaseidentity.Engine(cfg.Store.Engine))
	if err != nil {
		return observed, err
	}
	actual, err := upgrade.InspectInstalled(ctx, securityDatabase(cfg), manifest)
	if err != nil {
		return observed, err
	}
	if actual.Source != state.Applied || actual.InstanceID != state.InstanceID || actual.Ledger == nil || actual.Ledger.Generation != state.Generation || actual.SchemaDigest != state.SchemaDigest || actual.MigrationDigest != state.MigrationDigest {
		return observed, errors.New("actual schema, migration history or generation differs from applied release")
	}
	return observed, nil
}

// stageSecurityImage runs inside an isolated copy of the exact target image.
// It may write only authenticated cache files. It never acquires the runtime's
// lifetime coordinator lock, enrolls, fences, migrates, stops or starts writers.
func stageSecurityImage(ctx context.Context, cfg *config.Config, expected releaseidentity.Digest) (securitymaintenance.Observation, error) {
	var observed securitymaintenance.Observation
	if expected.Validate() != nil || !cfg.Upgrade.Unattended || cfg.Dev || cfg.HA || !cfg.AutoMigrate || cfg.ConfigRolloutEnrollment != "" {
		return observed, errors.New("security staging requires an enrolled signed singleton and exact target manifest")
	}
	directory := filepath.Join(cfg.Upgrade.StateDirectory, "unattended")
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return observed, errors.New("security staging requires existing private unattended state")
	}
	lock := flock.New(filepath.Join(directory, "security-prepare.lock"))
	held, err := lock.TryLock()
	if err != nil || !held {
		return observed, errors.Join(err, errors.New("another security preparation owns the cache"))
	}
	defer lock.Unlock()
	state, err := upgrade.InspectControl(ctx, securityDatabase(cfg))
	if err != nil {
		return observed, err
	}
	observed, err = securityControlObservation(state)
	observed.Engine = releaseidentity.Engine(cfg.Store.Engine)
	if err != nil || !observed.Healthy() {
		return observed, errors.Join(err, errors.New("security staging requires a healthy admitted source"))
	}
	var enrollment unattendedEnrollment
	raw, err := readUnattendedPrivate(filepath.Join(directory, "enrollment.json"))
	if err != nil || definitions.DecodeStrict(raw, &enrollment) != nil || enrollment.Format != "hikyo.unattended-enrollment/v1" || enrollment.Instance != state.InstanceID {
		return observed, errors.New("existing unattended enrollment is required; staging cannot adopt a database")
	}
	previous, err := readAutomaticJournal(filepath.Join(directory, "operation.json"))
	if err != nil {
		return observed, err
	}
	if previous != nil && previous.Phase != "complete" {
		return observed, errors.New("an unfinished image upgrade must be reconciled before staging a security target")
	}
	claim, declaration, err := buildcompat.Current()
	if err != nil {
		return observed, err
	}
	// The stage image knows its build fields, while the externally authenticated
	// feed binds the manifest. Release verification supplies the remaining fields.
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return observed, err
	}
	installer, err := newSecurityImageInstaller(directory, pinned)
	if err != nil {
		return observed, err
	}
	client, err := updatecheck.NewHTTPClient(securityRequestTimeout)
	if err != nil {
		return observed, err
	}
	source := updatecheck.NewGitHubSource(client)
	release, err := source.ReleaseByVersion(ctx, declaration.Version)
	if err != nil {
		return observed, err
	}
	channel := updatecheck.ChannelStable
	if declaration.Profile == releaseidentity.NightlyV1 {
		channel = updatecheck.ChannelNightly
	}
	target, err := installer.PrepareRelease(ctx, updatecheck.Status{Available: true, Channel: channel, LatestVersion: release.Version, Prerelease: release.Prerelease, Immutable: release.Immutable, Assets: release.Assets, URL: release.URL})
	if err != nil {
		return observed, err
	}
	if target.Identity.ManifestSHA256 != expected {
		return observed, errors.New("staging image differs from selected security manifest")
	}
	bundle, err := upgradebundle.Load(ctx, target.BundleDirectory, pinned, state.Floor)
	if err != nil {
		return observed, err
	}
	node, err := bundle.MatchBuild(claim)
	if err != nil || node.Identity() != target.Identity || buildcompat.Verify(node) != nil {
		return observed, errors.New("security staging image build is not the authenticated target")
	}
	executable, err := os.Executable()
	if err != nil {
		return observed, err
	}
	digest, err := fileDigest(executable)
	if err != nil || strings.TrimPrefix(digest, "sha256:") != string(target.BinarySHA256) {
		return observed, errors.New("staging executable differs from authenticated image payload")
	}
	route, err := discoverReleaseRoute(ctx, unattendedRouteInstaller{installer}, source, target, pinned, automaticStore{securityDatabase(cfg)}, releaseidentity.Engine(cfg.Store.Engine), nil, true)
	if err != nil {
		return observed, err
	}
	if route.Plan.Source() != state.Applied || route.Instance != state.InstanceID {
		return observed, errors.New("source changed while preparing security route")
	}
	if err := preflightUnattendedRoute(ctx, route); err != nil {
		return observed, err
	}
	stage := securityImageStage{Format: securityStageFormat, Target: target, Source: observed, Route: route.Plan.Digest()}
	stage.Target.BundleDirectory = route.Directory
	for _, prepared := range route.Executables {
		prepared.BundleDirectory = route.Directory
		stage.Executables = append(stage.Executables, prepared)
	}
	slices.SortFunc(stage.Executables, func(a, b selfupdate.PreparedNightly) int {
		return strings.Compare(string(a.Identity.ManifestSHA256), string(b.Identity.ManifestSHA256))
	})
	current, err := upgrade.InspectControl(ctx, securityDatabase(cfg))
	if err != nil || current.Applied != state.Applied || current.InstanceID != state.InstanceID || current.Generation != state.Generation || current.RecoveryIncarnation != state.RecoveryIncarnation || current.Maintenance {
		return observed, errors.Join(err, errors.New("source changed during security staging"))
	}
	raw, err = json.Marshal(stage)
	if err != nil {
		return observed, err
	}
	if err := writeAutomaticFile(filepath.Join(directory, "security-stage.json"), raw, 0600); err != nil {
		return observed, err
	}
	observed.StagedTarget, observed.RouteSHA256, observed.Phase, observed.Ready = target.Identity, route.Plan.Digest(), "staged", false
	impact, err := securityRouteImpact(route.Plan)
	if err != nil {
		return observed, err
	}
	observed.RequiresMaintenance, observed.SchemaChanges = impact.Maintenance, impact.Schema
	return observed, nil
}

// cachedSecurityStage is only a locator. Every executable, bundle and release
// is reauthenticated against the runtime's independently pinned trust/floors.
func cachedSecurityStage(directory string) (securityImageStage, error) {
	var stage securityImageStage
	raw, err := readUnattendedPrivate(filepath.Join(directory, "security-stage.json"))
	if err != nil {
		return stage, err
	}
	if definitions.DecodeStrict(raw, &stage) != nil || stage.Format != securityStageFormat || stage.Target.Identity.Validate() != nil || stage.Source.Validate() != nil || stage.Route.Validate() != nil || len(stage.Executables) == 0 || len(stage.Executables) > upgradecompat.MaxReleases {
		return stage, errors.New("invalid security preparation cache")
	}
	return stage, nil
}

func cachedSecurityStageImage(ctx context.Context, cfg *config.Config, directory string, claim []byte, pinned releasetrust.PinnedTrust, installer *selfupdate.Installer) (selfupdate.PreparedNightly, bool) {
	stage, err := cachedSecurityStage(directory)
	if err != nil {
		return selfupdate.PreparedNightly{}, false
	}
	state, err := upgrade.InspectControl(ctx, securityDatabase(cfg))
	if err != nil || state.InstanceID != stage.Source.Instance || state.RestoreEpoch != stage.Source.RestoreEpoch || fmt.Sprintf("%x", state.RecoveryIncarnation[:]) != stage.Source.Incarnation {
		return selfupdate.PreparedNightly{}, false
	}
	bundle, err := upgradebundle.Load(ctx, stage.Target.BundleDirectory, pinned, state.Floor)
	if err != nil {
		return selfupdate.PreparedNightly{}, false
	}
	node, err := bundle.MatchBuild(claim)
	if err != nil || node.Identity() != stage.Target.Identity || buildcompat.Verify(node) != nil {
		return selfupdate.PreparedNightly{}, false
	}
	verified, err := installer.VerifyCachedRelease(ctx, stage.Target, state.Floor)
	return verified, err == nil
}

type stagedSecurityInstaller struct {
	installer *selfupdate.Installer
	stage     securityImageStage
	floor     releaseidentity.SnapshotFloor
}

func (i stagedSecurityInstaller) PrepareNightlySource(ctx context.Context, _ updatecheck.Status, expected releaseidentity.Identity) (selfupdate.PreparedNightly, error) {
	for _, prepared := range i.stage.Executables {
		if prepared.Identity == expected {
			return i.installer.VerifyCachedRelease(ctx, prepared, i.floor)
		}
	}
	return selfupdate.PreparedNightly{}, errors.New("security preparation lacks an authenticated route executable")
}
func (i stagedSecurityInstaller) AssembleNightlyRoute(context.Context, selfupdate.PreparedNightly, []selfupdate.PreparedNightly) (string, error) {
	return i.stage.Target.BundleDirectory, nil
}
func (i stagedSecurityInstaller) Releases(context.Context) ([]updatecheck.Release, error) {
	return nil, errors.New("cached security routes never discover latest")
}
func (i stagedSecurityInstaller) ReleaseByVersion(_ context.Context, version string) (updatecheck.Release, error) {
	for _, prepared := range i.stage.Executables {
		if prepared.Identity.Version == version {
			return updatecheck.Release{Version: version, Prerelease: prepared.Identity.Profile == releaseidentity.NightlyV1, Immutable: true, Assets: []updatecheck.Asset{{Name: "release-manifest.sigstore.json"}}}, nil
		}
	}
	return updatecheck.Release{}, errors.New("security route requires an unstaged release")
}

func preparedSecurityImageRoute(ctx context.Context, cfg *config.Config, directory string, pinned releasetrust.PinnedTrust, installer *selfupdate.Installer, target selfupdate.PreparedNightly, previous *automaticJournal) (automaticRoute, bool, error) {
	stage, err := cachedSecurityStage(directory)
	if errors.Is(err, os.ErrNotExist) {
		return automaticRoute{}, false, nil
	}
	if err != nil {
		return automaticRoute{}, true, err
	}
	if stage.Target.Identity != target.Identity {
		return automaticRoute{}, false, nil
	}
	state, err := upgrade.InspectControl(ctx, securityDatabase(cfg))
	if err != nil {
		return automaticRoute{}, true, err
	}
	if state.InstanceID != stage.Source.Instance || state.RestoreEpoch != stage.Source.RestoreEpoch || fmt.Sprintf("%x", state.RecoveryIncarnation[:]) != stage.Source.Incarnation {
		return automaticRoute{}, true, errors.New("security preparation belongs to a different recovery incarnation")
	}
	cached := stagedSecurityInstaller{installer: installer, stage: stage, floor: state.Floor}
	route, err := discoverReleaseRoute(ctx, cached, cached, target, pinned, automaticStore{securityDatabase(cfg)}, releaseidentity.Engine(cfg.Store.Engine), previous, true)
	if err != nil {
		return route, true, err
	}
	if len(route.Plan.Steps()) != 0 && route.Plan.Digest() != stage.Route {
		return route, true, errors.New("cached security route differs from staged source")
	}
	return route, true, nil
}
