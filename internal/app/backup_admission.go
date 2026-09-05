package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// backupPublicBundle authenticates release/build and persisted trust using only
// public custody. Export/status must not need the separately held root escrow.
func backupPublicBundle(ctx context.Context, cfg *config.Config) (upgradebundle.Bundle, upgradecompat.VerifiedNode, error) {
	public := *cfg
	public.Upgrade.EvidenceDirectory = ""
	request, cleanup, err := upgradeRequest(ctx, &public, nil, upgradegate.Migrate)
	if err != nil {
		return upgradebundle.Bundle{}, upgradecompat.VerifiedNode{}, err
	}
	defer cleanup()
	floor := releaseidentity.SnapshotFloor{}
	state, err := upgrade.InspectControl(ctx, request.Store)
	if err == nil {
		expectedDomain := upgrade.Production
		if cfg.Dev {
			expectedDomain = upgrade.LocalDevelopment
		}
		if state.TrustDomain != expectedDomain || state.ReleaseRootDigest != releaseidentity.Hash(request.Pinned.Root) {
			return upgradebundle.Bundle{}, upgradecompat.VerifiedNode{}, errors.New("backup release trust differs from installation")
		}
		floor = state.Floor
	} else if !errors.Is(err, upgrade.ErrAbsent) {
		return upgradebundle.Bundle{}, upgradecompat.VerifiedNode{}, err
	}
	bundle, err := upgradebundle.Load(ctx, request.BundleDirectory, request.Pinned, floor)
	if err != nil {
		return upgradebundle.Bundle{}, upgradecompat.VerifiedNode{}, err
	}
	raw, _, err := buildcompat.Current()
	if cfg.Dev {
		raw, _, err = buildcompat.Development()
	}
	if err != nil {
		return upgradebundle.Bundle{}, upgradecompat.VerifiedNode{}, err
	}
	node, err := bundle.MatchBuild(raw)
	if err != nil {
		return upgradebundle.Bundle{}, upgradecompat.VerifiedNode{}, err
	}
	verify := buildcompat.Verify
	if cfg.Dev {
		verify = buildcompat.VerifyDevelopment
	}
	if err := verify(node); err != nil {
		return upgradebundle.Bundle{}, upgradecompat.VerifiedNode{}, err
	}
	return bundle, node, nil
}
func backupGateConfig(sc store.Config) upgrade.Config {
	return upgrade.Config{Engine: releaseidentity.Engine(sc.Engine), Path: sc.Path, DSN: sc.DSN}
}
func backupTargetConfig(cfg *config.Config, sc store.Config) *config.Config {
	target := *cfg
	target.Store.Engine = config.Engine(sc.Engine)
	target.Store.Path, target.Store.DSN = sc.Path, sc.DSN
	return &target
}

// Custody always precedes datastore exclusion, matching production boot and
// operator rotation. These public commands never load the root escrow key.
func withBackupOperatorCustody(ctx context.Context, cfg *config.Config, fn func(*upgradegate.OperatorCustody) error) error {
	if cfg.Dev {
		return fn(nil)
	}
	public, err := backupreceipt.ReadPublicArtifact(cfg.Upgrade.OperatorPublicKeyFile, 1<<20)
	if err != nil {
		return err
	}
	return upgradegate.WithInstalledOperator(ctx, cfg.Upgrade.StateDirectory, public, func(custody upgradegate.OperatorCustody) error { return fn(&custody) })
}
func openBackupRuntime(ctx context.Context, cfg *config.Config) (*store.DB, error) {
	sc := storeConfig(cfg)
	var db *store.DB
	err := withBackupOperatorCustody(ctx, cfg, func(custody *upgradegate.OperatorCustody) error {
		if _, _, err := backupPublicBundle(ctx, cfg); err != nil {
			return err
		}
		if _, err := upgrade.InspectControl(ctx, backupGateConfig(sc)); err != nil {
			return err
		}
		var admission upgrade.Admission
		err := upgrade.WithLock(ctx, backupGateConfig(sc), func(session *upgrade.Session) error {
			_, node, err := backupPublicBundle(ctx, cfg)
			if err != nil {
				return err
			}
			current, err := session.Read(ctx)
			if err != nil {
				return err
			}
			if custody != nil {
				if err := custody.Check(current); err != nil {
					return err
				}
			}
			admission, err = session.Admit(ctx, current, node)
			return err
		})
		if err != nil {
			return err
		}
		db, err = store.Open(ctx, sc, admission)
		return err
	})
	return db, err
}
func withDataRecovery(ctx context.Context, cfg *config.Config, sc store.Config, fn func(*store.RecoveryDB) error) error {
	target := backupTargetConfig(cfg, sc)
	return withBackupOperatorCustody(ctx, target, func(custody *upgradegate.OperatorCustody) error {
		bundle, node, err := backupPublicBundle(ctx, target)
		if err != nil {
			return err
		}
		if _, err := inspectDataRecoveryPlan(ctx, backupGateConfig(sc), bundle, node.Identity()); err != nil {
			return err
		}
		return upgrade.WithLock(ctx, backupGateConfig(sc), func(session *upgrade.Session) error {
			bundle, node, err := backupPublicBundle(ctx, target)
			if err != nil {
				return err
			}
			plan, err := inspectDataRecoveryPlan(ctx, backupGateConfig(sc), bundle, node.Identity())
			if err != nil {
				return err
			}
			current, err := upgrade.InspectControl(ctx, backupGateConfig(sc))
			if err == nil {
				if custody != nil {
					if err := custody.Check(current); err != nil {
						return err
					}
				}
			} else if errors.Is(err, upgrade.ErrAbsent) {
				manifest, err := plan.SourceManifest(backupGateConfig(sc).Engine)
				if err != nil {
					return err
				}
				actual, err := upgrade.InspectInstalled(ctx, backupGateConfig(sc), manifest)
				if err != nil {
					return err
				}
				if custody != nil {
					if err := custody.CheckSource(actual.InstanceID, actual.RestoreEpoch); err != nil {
						return err
					}
				}
			} else {
				return err
			}
			authority, err := session.DataRecoveryAdmission(ctx, plan)
			if err != nil {
				return err
			}
			db, err := store.OpenRecovery(ctx, sc, authority)
			if err != nil {
				return err
			}
			defer db.Close()
			return fn(db)
		})
	})
}

type reconciliationService interface {
	Status(context.Context) (service.Status, error)
	Reconcile(context.Context, domain.PrincipalID) (service.Status, error)
}

func withReconciliation(ctx context.Context, cfg *config.Config, fn func(reconciliationService) error) error {
	// Healthy installations retain ordinary per-principal reconciliation after
	// reactivation. Invalidated restored installations receive only data recovery.
	if db, err := openBackupRuntime(ctx, cfg); err == nil {
		defer db.Close()
		return fn(&service.Restore{DB: db})
	}
	return withDataRecovery(ctx, cfg, storeConfig(cfg), func(db *store.RecoveryDB) error { return fn(&service.Recovery{DB: db}) })
}

func restoreOrdinaryPostgres(ctx context.Context, cfg *config.Config, sc store.Config, plain io.ReadSeeker, manifest store.Manifest, now time.Time) error {
	return withBackupOperatorCustody(ctx, cfg, func(_ *upgradegate.OperatorCustody) error {
		return restoreOrdinaryPostgresWithCustody(ctx, cfg, sc, plain, manifest, now)
	})
}
func restoreOrdinaryPostgresWithCustody(ctx context.Context, cfg *config.Config, sc store.Config, plain io.ReadSeeker, manifest store.Manifest, now time.Time) error {
	plans, err := backupRestorePlans(ctx, cfg, sc, manifest)
	if err != nil {
		return err
	}
	return upgrade.WithLock(ctx, backupGateConfig(sc), func(session *upgrade.Session) error {
		if err := session.ApplyRestoreSchema(ctx, plans[0], store.MigrationsFS, "migrations/postgres"); err != nil {
			return err
		}
		var refusals []error
		for _, plan := range plans {
			authority, err := session.ValidateDataRestoreDestination(ctx, plan)
			if err != nil {
				refusals = append(refusals, err)
				continue
			}
			destination, err := store.OpenDataRestoreDestination(ctx, sc, authority, plan)
			if err != nil {
				return err
			}
			if _, err := plain.Seek(0, io.SeekStart); err != nil {
				destination.Close()
				return err
			}
			_, err = tx.RestoreDataDestinationPostgres(ctx, destination, plain, service.CompleteRestore(now, manifest))
			closeErr := destination.Close()
			if err == nil {
				return closeErr
			}
			refusals = append(refusals, errors.Join(err, closeErr))
		}
		return fmt.Errorf("ordinary archive does not match an authenticated source: %w", errors.Join(refusals...))
	})
}

func backupRestorePlans(ctx context.Context, cfg *config.Config, sc store.Config, manifest store.Manifest) ([]upgradecompat.Plan, error) {
	bundle, node, err := backupPublicBundle(ctx, backupTargetConfig(cfg, sc))
	if err != nil {
		return nil, err
	}
	// The v1 schema version is only a bounded candidate hint. Each candidate is
	// independently signed; the imported full source identity is checked before
	// commit. Failed candidates roll back and never create runtime authority.
	var plans []upgradecompat.Plan
	for _, source := range bundle.Sources(releaseidentity.Engine(sc.Engine)) {
		entries := source.Migrations.Entries
		if len(entries) == 0 || int64(entries[len(entries)-1].Version) != manifest.SchemaVersion {
			continue
		}
		plan, err := bundle.Plan(source, node.Identity())
		if err != nil || store.VerifyEmbeddedUpgradeSource(plan, sc.Engine) != nil {
			continue
		}
		plans = append(plans, plan)
	}
	if len(plans) == 0 {
		return nil, errors.New("archive source has no authenticated embedded schema route")
	}
	return plans, nil
}
func restoreOrdinarySQLite(ctx context.Context, cfg *config.Config, sc store.Config, plain io.ReadSeeker, manifest store.Manifest, now time.Time) error {
	return withBackupOperatorCustody(ctx, cfg, func(_ *upgradegate.OperatorCustody) error {
		return restoreOrdinarySQLiteWithCustody(ctx, cfg, sc, plain, manifest, now)
	})
}
func restoreOrdinarySQLiteWithCustody(ctx context.Context, cfg *config.Config, sc store.Config, plain io.ReadSeeker, manifest store.Manifest, now time.Time) error {
	plans, err := backupRestorePlans(ctx, cfg, sc, manifest)
	if err != nil {
		return err
	}
	var refusals []error
	for _, plan := range plans {
		if _, err := plain.Seek(0, io.SeekStart); err != nil {
			return err
		}
		_, err := tx.RestoreDataSQLite(ctx, plain, sc.Path, plan, service.CompleteRestore(now, manifest))
		if err == nil {
			return nil
		}
		if errors.Is(err, store.ErrTargetNotEmpty) {
			return err
		}
		refusals = append(refusals, err)
	}
	return fmt.Errorf("ordinary archive does not match an authenticated source: %w", errors.Join(refusals...))
}

func inspectDataRecoveryPlan(ctx context.Context, cfg upgrade.Config, bundle upgradebundle.Bundle, target releaseidentity.Identity) (upgradecompat.Plan, error) {
	for _, candidate := range bundle.Sources(cfg.Engine) {
		actual, err := upgrade.InspectInstalled(ctx, cfg, candidate.Migrations)
		if err != nil || actual.Source != candidate.Identity || actual.SchemaDigest != candidate.SchemaSHA256 || actual.InstanceID == "" {
			continue
		}
		return bundle.Plan(upgradecompat.InstalledSource{Identity: actual.Source, Migrations: candidate.Migrations, SchemaSHA256: actual.SchemaDigest}, target)
	}
	return upgradecompat.Plan{}, errors.New("restored source has no authenticated recovery route")
}
