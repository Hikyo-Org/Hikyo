package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/devupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// databaseGate is shared by server, local administration and explicit migration.
// The returned opaque admission is the sole runtime datastore constructor input.
func databaseGate(ctx context.Context, cfg *config.Config, root []byte, mode upgradegate.Mode) (upgradegate.Result, error) {
	request, cleanup, err := upgradeRequest(ctx, cfg, root, mode)
	if err != nil {
		return upgradegate.Result{}, err
	}
	defer cleanup()
	if cfg.Dev {
		return upgradegate.RunDevelopment(ctx, request)
	}
	return upgradegate.Run(ctx, request)
}

func upgradeRequest(ctx context.Context, cfg *config.Config, root []byte, mode upgradegate.Mode) (upgradegate.Request, func(), error) {
	request := upgradegate.Request{Store: upgrade.Config{Engine: releaseidentity.Engine(cfg.Store.Engine), Path: cfg.Store.Path, DSN: cfg.Store.DSN}, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(cfg.Store.Engine), Mode: mode, AllowMigrations: cfg.AutoMigrate, RootKey: root, LegacyWritersStopped: cfg.Upgrade.LegacyWritersStopped}
	if mode == upgradegate.Boot {
		request.CheckConfiguration = func(ctx context.Context) error { return checkCandidateConfiguration(ctx, cfg) }
	}
	cleanup := func() {}
	control, controlErr := upgrade.InspectControl(ctx, request.Store)
	if controlErr != nil && !errors.Is(controlErr, upgrade.ErrAbsent) {
		return request, cleanup, controlErr
	}
	if cfg.Dev {
		if controlErr == nil && control.TrustDomain != upgrade.LocalDevelopment {
			return request, cleanup, errors.New("development cannot adopt a production datastore")
		}
		stateDir := cfg.Upgrade.StateDirectory
		if stateDir == "" {
			if cfg.Store.Engine != config.EngineSQLite {
				return request, cleanup, errors.New("development PostgreSQL requires HIKYO_UPGRADE_STATE_DIR")
			}
			// The zero-config SQLite path is relative to the process working
			// directory. Resolve only its existing parent; the custody directory
			// itself must still pass devupgrade's descriptor-based nofollow checks.
			parent, err := filepath.Abs(filepath.Dir(cfg.Store.Path))
			if err != nil {
				return request, cleanup, err
			}
			parent, err = filepath.EvalSymlinks(parent)
			if err != nil {
				return request, cleanup, err
			}
			stateDir = filepath.Join(parent, ".hikyo-development")
		}
		if err := os.Mkdir(stateDir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return request, cleanup, err
		}
		material, err := devupgrade.Open(ctx, stateDir)
		if err != nil {
			return request, cleanup, err
		}
		request.BundleDirectory, request.Pinned = material.Directory, material.Pinned
	} else {
		pinned, err := buildcompat.ProductionTrust()
		if err != nil {
			return request, cleanup, err
		}
		if cfg.Upgrade.BundleDirectory == "" {
			return request, cleanup, errors.New("HIKYO_UPGRADE_BUNDLE must name the authenticated offline release bundle")
		}
		request.Pinned, request.BundleDirectory = pinned, cfg.Upgrade.BundleDirectory
		request.StateDirectory = cfg.Upgrade.StateDirectory
		if request.StateDirectory == "" || cfg.Upgrade.OperatorPublicKeyFile == "" {
			return request, cleanup, errors.New("production requires HIKYO_UPGRADE_STATE_DIR and HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY")
		}
		request.InitialOperatorPublicKey, err = backupreceipt.ReadPublicArtifact(cfg.Upgrade.OperatorPublicKeyFile, 1<<20)
		if err != nil {
			return request, cleanup, err
		}

	}
	if cfg.Upgrade.TargetManifestSHA256 != "" {
		digest := releaseidentity.Digest(cfg.Upgrade.TargetManifestSHA256)
		if digest.Validate() != nil {
			return request, cleanup, errors.New("HIKYO_UPGRADE_TARGET_MANIFEST must be an exact lowercase SHA-256")
		}
		floor := releaseidentity.SnapshotFloor{}
		if controlErr == nil {
			floor = control.Floor
		}
		bundle, err := upgradebundle.Load(ctx, request.BundleDirectory, request.Pinned, floor)
		if err != nil {
			return request, cleanup, err
		}
		for _, source := range bundle.Sources(request.Store.Engine) {
			if source.Identity.IsRelease() && source.Identity.Release.ManifestSHA256 == digest {
				request.Target = source.Identity.Release
			}
		}
		if request.Target == (releaseidentity.Identity{}) {
			return request, cleanup, errors.New("configured route target is absent from the authenticated bundle")
		}
	}
	if cfg.Upgrade.EvidenceDirectory != "" {
		for _, artifact := range []struct {
			name   string
			target *[]byte
		}{{"receipt.json", &request.Evidence.Receipt}, {"attestation.json", &request.Evidence.Attestation}, {"attestation.sigstore.json", &request.Evidence.Signature}} {
			raw, err := backupreceipt.ReadPublicArtifact(filepath.Join(cfg.Upgrade.EvidenceDirectory, artifact.name), 1<<20)
			if err != nil {
				return request, cleanup, fmt.Errorf("upgrade public evidence %s: %w", artifact.name, err)
			}
			*artifact.target = raw
		}
		receipt, err := backupreceipt.ParseReceipt(request.Evidence.Receipt)
		if err != nil {
			return request, cleanup, err
		}
		if cfg.Upgrade.OperatorPublicKeyFile == "" || cfg.Upgrade.CiphertextPath == "" {
			return request, cleanup, errors.New("upgrade evidence requires separate operator public-key pin and ciphertext path")
		}
		public, err := backupreceipt.ReadPublicArtifact(cfg.Upgrade.OperatorPublicKeyFile, 1<<20)
		if err != nil {
			return request, cleanup, err
		}
		request.Operator, err = backupreceipt.PinOperator(receipt.Snapshot.InstanceID, public)
		if err != nil {
			return request, cleanup, err
		}
		ciphertext, err := backupreceipt.PinCiphertext(ctx, cfg.Upgrade.CiphertextPath, cfg.Upgrade.StateDirectory)
		if err != nil {
			return request, cleanup, err
		}
		request.Ciphertext = ciphertext
		cleanup = func() { _ = ciphertext.Close() }
	}
	return request, cleanup, nil
}

// configuredBackupTrust verifies the executing build and installation's public
// release/operator pins without loading root-key or age private custody. An
// offline drill may explicitly name its operator instance; export must inspect
// the actual source. The drill subsequently authenticates that instance inside
// the complete archive before any proof can be signed.
func configuredBackupTrust(ctx context.Context, cfg *config.Config, offlineDrill bool) (TrustContext, error) {
	publicConfig := *cfg
	publicConfig.Upgrade.EvidenceDirectory = ""
	request, cleanup, err := upgradeRequest(ctx, &publicConfig, nil, upgradegate.Migrate)
	if err != nil {
		return TrustContext{}, err
	}
	defer cleanup()
	control, controlErr := upgrade.InspectControl(ctx, request.Store)
	if controlErr != nil && !errors.Is(controlErr, upgrade.ErrAbsent) {
		return TrustContext{}, controlErr
	}
	floor := releaseidentity.SnapshotFloor{}
	if controlErr == nil {
		domain := upgrade.Production
		if cfg.Dev {
			domain = upgrade.LocalDevelopment
		}
		if control.TrustDomain != domain || control.ReleaseRootDigest != releaseidentity.Hash(request.Pinned.Root) {
			return TrustContext{}, errors.New("upgrade backup trust differs from persisted installation domain or root")
		}
		floor = control.Floor
	}
	bundle, err := upgradebundle.Load(ctx, request.BundleDirectory, request.Pinned, floor)
	if err != nil {
		return TrustContext{}, err
	}
	raw, _, err := buildcompat.Current()
	if cfg.Dev {
		raw, _, err = buildcompat.Development()
	}
	if err != nil {
		return TrustContext{}, err
	}
	node, err := bundle.MatchBuild(raw)
	if err != nil {
		return TrustContext{}, err
	}
	verify := buildcompat.Verify
	if cfg.Dev {
		verify = buildcompat.VerifyDevelopment
	}
	if err := verify(node); err != nil {
		return TrustContext{}, err
	}
	target := request.Target
	if target == (releaseidentity.Identity{}) {
		target = node.Identity()
	}
	instance := cfg.Upgrade.OperatorInstanceID
	actualFound := false
	var actualEpoch int64
	for _, candidate := range bundle.Sources(request.Store.Engine) {
		actual, err := upgrade.InspectInstalled(ctx, request.Store, candidate.Migrations)
		if err != nil || actual.InstanceID == "" || actual.Source != candidate.Identity || actual.SchemaDigest != candidate.SchemaSHA256 {
			continue
		}
		if instance != "" && instance != actual.InstanceID {
			return TrustContext{}, errors.New("operator instance pin differs from actual source")
		}
		instance, actualFound = actual.InstanceID, true
		actualEpoch = actual.RestoreEpoch
		break
	}
	if instance == "" || (!offlineDrill && !actualFound) {
		return TrustContext{}, errors.New("upgrade backup needs an authenticated actual source; offline drill also accepts HIKYO_UPGRADE_OPERATOR_INSTANCE")
	}
	if cfg.Upgrade.OperatorPublicKeyFile == "" {
		return TrustContext{}, errors.New("HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY must name the separately pinned operator public key")
	}
	public, err := backupreceipt.ReadPublicArtifact(cfg.Upgrade.OperatorPublicKeyFile, 1<<20)
	if err != nil {
		return TrustContext{}, err
	}
	operator, err := backupreceipt.PinOperator(instance, public)
	if err != nil {
		return TrustContext{}, err
	}
	if !cfg.Dev {
		if errors.Is(controlErr, upgrade.ErrAbsent) && actualFound {
			operator, err = upgradegate.InstallLegacyOperator(ctx, cfg.Upgrade.StateDirectory, instance, public, actualEpoch)
		} else {
			operator, err = upgradegate.InstalledOperator(ctx, cfg.Upgrade.StateDirectory, instance, public)
		}
		if err != nil {
			return TrustContext{}, err
		}
	}
	if controlErr == nil && control.Pending != nil && !control.Pending.Invalidated && control.Pending.Acceptance.Attestation != nil && control.Pending.Acceptance.Attestation.OperatorKeyID != operator.KeyID() {
		return TrustContext{}, errors.New("operator key changed without an accepted rotation")
	}
	return TrustContext{BundleDirectory: request.BundleDirectory, Pinned: request.Pinned, Target: target, Floor: floor, OperatorPin: operator}, nil
}
