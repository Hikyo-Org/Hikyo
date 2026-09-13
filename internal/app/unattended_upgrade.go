package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
	"github.com/gofrs/flock"
)

type UnattendedUpgradeOptions struct {
	Progress func(string)
	// PreparedConfiguration runs outside the migration lock. It may start the
	// public maintenance listener using only authenticated effective transport.
	PreparedConfiguration func(*config.Config) error
}

type unattendedEnrollment struct {
	Format   string                   `json:"format"`
	Target   releaseidentity.Identity `json:"initial_target"`
	Instance string                   `json:"instance"`
	Operator releaseidentity.Digest   `json:"operator"`
}

type unattendedInstaller interface {
	PrepareRelease(context.Context, updatecheck.Status) (selfupdate.PreparedNightly, error)
	PrepareReleaseSource(context.Context, updatecheck.Status, releaseidentity.Identity) (selfupdate.PreparedNightly, error)
	AssembleReleaseRoute(context.Context, selfupdate.PreparedNightly, []selfupdate.PreparedNightly) (string, error)
}

type unattendedRouteInstaller struct{ unattendedInstaller }

func (i unattendedRouteInstaller) PrepareNightlySource(ctx context.Context, status updatecheck.Status, expected releaseidentity.Identity) (selfupdate.PreparedNightly, error) {
	return i.PrepareReleaseSource(ctx, status, expected)
}
func (i unattendedRouteInstaller) AssembleNightlyRoute(ctx context.Context, target selfupdate.PreparedNightly, sources []selfupdate.PreparedNightly) (string, error) {
	return i.AssembleReleaseRoute(ctx, target, sources)
}

// RunUnattendedUpgrade prepares the exact running image before ordinary Boot.
// It never selects latest, replaces the image executable or contacts Docker.
// The caller retains cleanup until server shutdown to serialize replacements
// sharing the installation's persistent upgrade directory.
func RunUnattendedUpgrade(ctx context.Context, cfg *config.Config, options UnattendedUpgradeOptions) (cleanup func() error, err error) {
	cleanup = func() error { return nil }
	if !cfg.Upgrade.Unattended {
		return cleanup, nil
	}
	if cfg.Dev || cfg.HA || !cfg.AutoMigrate || cfg.ConfigRolloutEnrollment != "" || !filepath.IsAbs(cfg.Upgrade.StateDirectory) {
		return cleanup, errors.New("unattended upgrades require a signed singleton with persistent custody and automatic migrations")
	}
	if err := crypto.HardenProcess(); err != nil {
		return cleanup, err
	}
	claim, declaration, err := buildcompat.Current()
	if err != nil {
		return cleanup, err
	}
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return cleanup, err
	}
	root, err := resolveRootKey(cfg, Logger(cfg.Dev))
	if err != nil {
		return cleanup, err
	}
	defer crypto.Zero(root)
	if options.Progress != nil {
		options.Progress("preparing")
	}
	directory := filepath.Join(cfg.Upgrade.StateDirectory, "unattended")
	if err := os.Mkdir(directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return cleanup, err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return cleanup, errors.New("unattended state directory must be a private real directory")
	}
	lock := flock.New(filepath.Join(directory, "coordinator.lock"))
	held, err := lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return cleanup, err
	}
	if !held {
		return cleanup, errors.New("unattended coordinator lock unavailable")
	}
	cleanup = lock.Unlock
	installer, err := selfupdate.NewInstaller(selfupdate.Config{StateDir: filepath.Join(directory, "downloads"), TrustRootBase64: base64.StdEncoding.EncodeToString(pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(pinned.RecoveryPublicKey), TransientBundles: true})
	if err != nil {
		return cleanup, err
	}
	client, err := updatecheck.NewHTTPClient(60 * time.Second)
	if err != nil {
		return cleanup, err
	}
	source := updatecheck.NewGitHubSource(client)
	target, cached := cachedUnattendedImage(ctx, cfg, directory, claim, pinned, installer)
	if !cached {
		release, err := source.ReleaseByVersion(ctx, declaration.Version)
		if err != nil {
			return cleanup, err
		}
		channel := updatecheck.ChannelStable
		if release.Prerelease {
			channel = updatecheck.ChannelNightly
		}
		target, err = installer.PrepareRelease(ctx, updatecheck.Status{Available: true, Channel: channel, LatestVersion: release.Version, Prerelease: release.Prerelease, Immutable: release.Immutable, Assets: release.Assets, URL: release.URL})
		if err != nil {
			return cleanup, err
		}
	}
	bundle, err := upgradebundle.Load(ctx, target.BundleDirectory, pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		return cleanup, err
	}
	node, err := bundle.MatchBuild(claim)
	if err != nil || node.Identity() != target.Identity {
		return cleanup, errors.New("downloaded release differs from exact running image")
	}
	if err := buildcompat.Verify(node); err != nil {
		return cleanup, err
	}
	executable, err := os.Executable()
	if err != nil {
		return cleanup, err
	}
	digest, err := fileDigest(executable)
	if err != nil || strings.TrimPrefix(digest, "sha256:") != string(target.BinarySHA256) {
		return cleanup, errors.New("running image payload differs from authenticated release executable")
	}
	if !cached {
		raw, err := json.Marshal(target)
		if err != nil {
			return cleanup, err
		}
		if err := writeAutomaticFile(filepath.Join(directory, "image.json"), raw, 0600); err != nil {
			return cleanup, err
		}
	}
	if err := runPreparedUnattended(ctx, cfg, options, directory, root, pinned, installer, source, target); err != nil {
		return cleanup, err
	}
	target.BundleDirectory = cfg.Upgrade.BundleDirectory
	raw, err := json.Marshal(target)
	if err != nil {
		return cleanup, err
	}
	return cleanup, writeAutomaticFile(filepath.Join(directory, "image.json"), raw, 0600)
}

func runPreparedUnattended(ctx context.Context, cfg *config.Config, options UnattendedUpgradeOptions, directory string, root []byte, pinned releasetrust.PinnedTrust, installer unattendedInstaller, source automaticReleaseSource, target selfupdate.PreparedNightly) error {
	database := upgrade.Config{Engine: releaseidentity.Engine(cfg.Store.Engine), Path: cfg.Store.Path, DSN: cfg.Store.DSN}
	state, stateErr := upgrade.InspectControl(ctx, database)
	if stateErr != nil && !errors.Is(stateErr, upgrade.ErrAbsent) {
		return stateErr
	}
	path := filepath.Join(directory, "enrollment.json")
	var enrollment unattendedEnrollment
	raw, err := readUnattendedPrivate(path)
	enrolled := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if enrolled && (definitions.DecodeStrict(raw, &enrollment) != nil || enrollment.Format != "hikyo.unattended-enrollment/v1" || enrollment.Target.Validate() != nil || enrollment.Operator.Validate() != nil) {
		return errors.New("invalid unattended enrollment")
	}
	custody := filepath.Join(directory, "custody")
	publicPath := filepath.Join(directory, "operator.pub")
	cfg.Upgrade.BundleDirectory, cfg.Upgrade.OperatorPublicKeyFile = target.BundleDirectory, publicPath
	cfg.Upgrade.TargetManifestSHA256 = string(target.Identity.ManifestSHA256)
	if !enrolled {
		if stateErr == nil {
			return errors.New("existing installation is not enrolled for unattended upgrades; explicit enrollment is required")
		}
		empty := releaseidentity.MigrationManifest{Engine: database.Engine, Entries: []releaseidentity.Migration{}}
		inspection, err := upgrade.Inspect(ctx, database, empty)
		if err != nil || inspection.Genesis != releaseidentity.FreshGenesisV1 {
			return errors.New("unattended enrollment requires a fresh database; legacy adoption is unsupported")
		}
		if cfg.Store.Engine == config.EnginePostgres {
			_, cleanup, err := prepareUnattendedScratch(ctx, cfg, directory, inspection.CatalogDigest)
			if err != nil {
				return err
			}
			if err := cleanup(); err != nil {
				return err
			}
		}
		public, err := upgradecustody.PrepareLocalEnrollment(custody, root)
		if err != nil {
			return err
		}
		if err := writeAutomaticFile(publicPath, public, 0644); err != nil {
			return err
		}
		enrollment = unattendedEnrollment{Format: "hikyo.unattended-enrollment/v1", Target: target.Identity, Operator: releaseidentity.Hash(public)}
		if err := writeUnattendedEnrollment(path, enrollment); err != nil {
			return err
		}
	}
	public, err := backupreceipt.ReadPublicArtifact(publicPath, 1<<20)
	if err != nil || releaseidentity.Hash(public) != enrollment.Operator {
		return errors.New("unattended operator pin differs from enrolled custody")
	}
	if enrollment.Instance == "" {
		if enrollment.Target != target.Identity {
			return errors.New("unfinished fresh enrollment requires its original exact image")
		}
		if stateErr == nil && (state.Generation != 1 || state.Pending.RouteSource.Genesis != releaseidentity.FreshGenesisV1 || state.Pending.Target != target.Identity || state.Pending.Invalidated) {
			return errors.New("existing database does not match unfinished fresh enrollment")
		}
		booted, err := databaseGate(ctx, cfg, root, upgradegate.Boot)
		if err != nil {
			return err
		}
		vault, err := upgradecustody.BindLocalEnrollment(custody, root, booted.State.InstanceID, public)
		if err != nil {
			return err
		}
		vault.Close()
		enrollment.Instance = booted.State.InstanceID
		return writeUnattendedEnrollment(path, enrollment)
	}
	if stateErr != nil || state.InstanceID != enrollment.Instance {
		return errors.New("unattended enrollment differs from installed database")
	}
	vault, err := upgradecustody.OpenLocal(custody, root, state.InstanceID)
	if err != nil {
		return err
	}
	defer vault.Close()
	journalPath := filepath.Join(directory, "operation.json")
	previous, err := readAutomaticJournal(journalPath)
	if err != nil {
		return err
	}
	if previous != nil && (previous.Phase == "restore-required" || state.Pending.Phase == upgrade.RestoreRequired) {
		applyUnattendedEvidence(cfg, previous.Runtime)
		if state.Maintenance && options.PreparedConfiguration != nil {
			if err := prepareUnattendedMaintenance(ctx, cfg, root, options); err != nil {
				return err
			}
		}
		return upgradegate.ErrRestoreRequired
	}
	if previous != nil && previous.Phase != "complete" && previous.Target != target.Identity {
		return errors.New("unfinished unattended upgrade requires its original exact image")
	}
	route, err := discoverReleaseRoute(ctx, unattendedRouteInstaller{installer}, source, target, pinned, automaticStore{database}, database.Engine, previous, true)
	if err != nil {
		return err
	}
	cfg.Upgrade.BundleDirectory = route.Directory
	if len(route.Plan.Steps()) == 0 {
		return nil
	}
	if err := preflightUnattendedRoute(ctx, route); err != nil {
		return err
	}
	journal := previous
	if journal == nil || journal.Phase == "complete" {
		manifest, err := route.Plan.SourceManifest(database.Engine)
		if err != nil {
			return err
		}
		journal = &automaticJournal{Format: "hikyo.container-upgrade/v1", Phase: "preparing", Target: target.Identity, Source: upgradecompat.InstalledSource{Identity: route.Plan.Source(), Migrations: manifest, SchemaSHA256: route.Plan.SourceSchemaDigest()}, Instance: state.InstanceID, Route: route.Plan.Digest(), Runtime: hostupgrade.RuntimeEvidence{BundleDirectory: route.Directory, OperatorPublicKey: publicPath, TargetManifest: string(target.Identity.ManifestSHA256)}}
		if err := writeAutomaticJournal(journalPath, journal); err != nil {
			return err
		}
	} else {
		journal.Runtime.BundleDirectory = route.Directory
	}
	preparedTransport := false
	if state.Pending.Phase == upgrade.Healthy || state.Pending.Phase == upgrade.BackupPreparing {
		if journal.Phase == "preparing" || state.Pending.Phase == upgrade.BackupPreparing {
			scratch, cleanupScratch, err := prepareUnattendedScratch(ctx, cfg, directory, route.Plan.SourceSchemaDigest())
			if err != nil {
				return err
			}
			defer cleanupScratch()
			request, closeRequest, err := upgradeRequest(ctx, cfg, root, upgradegate.PrepareBackup)
			if err != nil {
				return err
			}
			defer closeRequest()
			var preparedConfig *config.Config
			request.CheckConfiguration = func(ctx context.Context, projection *upgrade.CandidateConfiguration, values map[string]string) error {
				var err error
				preparedConfig, err = resolveCandidateConfiguration(ctx, cfg, projection, values)
				return err
			}
			if _, err := upgradegate.Run(ctx, request); err != nil {
				return err
			}
			if options.PreparedConfiguration != nil && preparedConfig != nil {
				if err := options.PreparedConfiguration(preparedConfig); err != nil {
					return err
				}
			}
			preparedTransport = true
			// A retry on a frozen source makes fresh bounded proof, including
			// after an earlier attestation expired while the container was down.
			if err := prepareUnattendedEvidence(ctx, cfg, options, directory, route, vault, journal, scratch); err != nil {
				return err
			}
			journal.Phase = "write-intent"
			if err := writeAutomaticJournal(journalPath, journal); err != nil {
				return err
			}
			applyUnattendedEvidence(cfg, journal.Runtime)
			if _, err := databaseGate(ctx, cfg, root, upgradegate.PrepareRoute); err != nil {
				return err
			}
		}
	}
	applyUnattendedEvidence(cfg, journal.Runtime)
	if !preparedTransport && state.Maintenance && options.PreparedConfiguration != nil {
		if err := prepareUnattendedMaintenance(ctx, cfg, root, options); err != nil {
			return err
		}
	}
	host := &unattendedProcessHost{cfg: cfg, root: root, directory: directory, options: options}
	defer host.FenceAndStop(context.Background())
	staged := make(map[releaseidentity.Identity]string)
	for identity, prepared := range route.Executables {
		staged[identity] = prepared.BinaryPath
	}
	return applyAutomaticRoute(ctx, host, automaticStore{database}, route, staged, journal, journalPath, io.Discard)
}

// Every selected executable must understand the platform bundle before the
// coordinator creates recovery scratch or changes durable source admission.
func preflightUnattendedRoute(ctx context.Context, route automaticRoute) error {
	for _, step := range route.Plan.Steps() {
		prepared, ok := route.Executables[step.Target]
		if !ok || prepared.Identity != step.Target || prepared.BinaryPath == "" || prepared.BinarySHA256.Validate() != nil {
			return errors.New("authenticated route executable is missing or mismatched")
		}
		digest, err := fileDigest(prepared.BinaryPath)
		if err != nil || strings.TrimPrefix(digest, "sha256:") != string(prepared.BinarySHA256) {
			return errors.New("candidate executable differs from authenticated payload")
		}
		if err := checkAutomaticBundleFormat(ctx, prepared.BinaryPath); err != nil {
			return fmt.Errorf("release %s cannot use the platform bundle: %w", step.Target.Version, err)
		}
	}
	return nil
}

func writeUnattendedEnrollment(path string, enrollment unattendedEnrollment) error {
	raw, err := json.Marshal(enrollment)
	if err != nil {
		return err
	}
	return writeAutomaticFile(path, raw, 0600)
}

func readUnattendedPrivate(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 1<<20 {
		return nil, errors.New("invalid private unattended state artifact")
	}
	return os.ReadFile(path)
}

func cachedUnattendedImage(ctx context.Context, cfg *config.Config, directory string, claim []byte, pinned releasetrust.PinnedTrust, installer *selfupdate.Installer) (selfupdate.PreparedNightly, bool) {
	var prepared selfupdate.PreparedNightly
	raw, err := readUnattendedPrivate(filepath.Join(directory, "image.json"))
	if err != nil || definitions.DecodeStrict(raw, &prepared) != nil || prepared.Identity.Validate() != nil || prepared.BinarySHA256.Validate() != nil {
		return prepared, false
	}
	floor := releaseidentity.SnapshotFloor{}
	state, err := upgrade.InspectControl(ctx, upgrade.Config{Engine: releaseidentity.Engine(cfg.Store.Engine), Path: cfg.Store.Path, DSN: cfg.Store.DSN})
	if err == nil {
		floor = state.Floor
	} else if !errors.Is(err, upgrade.ErrAbsent) {
		return prepared, false
	}
	bundle, err := upgradebundle.Load(ctx, prepared.BundleDirectory, pinned, floor)
	if err != nil {
		return prepared, false
	}
	node, err := bundle.MatchBuild(claim)
	if err != nil || node.Identity() != prepared.Identity || buildcompat.Verify(node) != nil {
		return prepared, false
	}
	verified, err := installer.VerifyCachedRelease(ctx, prepared, floor)
	return verified, err == nil
}

func applyUnattendedEvidence(cfg *config.Config, evidence hostupgrade.RuntimeEvidence) {
	cfg.Upgrade.BundleDirectory, cfg.Upgrade.OperatorPublicKeyFile = evidence.BundleDirectory, evidence.OperatorPublicKey
	cfg.Upgrade.EvidenceDirectory, cfg.Upgrade.CiphertextPath = evidence.EvidenceDirectory, evidence.CiphertextPath
	cfg.Upgrade.TargetManifestSHA256 = evidence.TargetManifest
	cfg.Upgrade.LegacyWritersStopped = false
}

func prepareUnattendedMaintenance(ctx context.Context, cfg *config.Config, root []byte, options UnattendedUpgradeOptions) error {
	request, closeRequest, err := upgradeRequest(ctx, cfg, root, upgradegate.MaintenanceConfiguration)
	if err != nil {
		return err
	}
	defer closeRequest()
	var preparedConfig *config.Config
	request.CheckConfiguration = func(ctx context.Context, projection *upgrade.CandidateConfiguration, values map[string]string) error {
		var err error
		preparedConfig, err = resolveCandidateConfiguration(ctx, cfg, projection, values)
		return err
	}
	if _, err := upgradegate.Run(ctx, request); err != nil {
		return err
	}
	if preparedConfig == nil {
		return errors.New("maintenance transport configuration unavailable")
	}
	return options.PreparedConfiguration(preparedConfig)
}
