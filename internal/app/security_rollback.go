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
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
	"github.com/gofrs/flock"
)

// securityRollbackReport is the one line the owner trusts after the target is gone.
// "not-needed" is only a report: the source gate stays the authority on the fence.
type securityRollbackReport = securitydeployment.RollbackReport

// rollbackSecurityImage runs inside a one-shot of the stopped target's own image,
// holding the coordinator lock. It puts the snapshot the backup fence took back
// under the live database, drops the attestation that licensed the target, and
// leaves the journal in the state the source image already accepts. Any doubt
// returns an error, which keeps the operation pinned.
func rollbackSecurityImage(ctx context.Context, cfg *config.Config, expected releaseidentity.Digest) (report securityRollbackReport, err error) {
	report = securityRollbackReport{Format: securitydeployment.RollbackReportFormat}
	if expected.Validate() != nil || !cfg.Upgrade.Unattended || cfg.Dev || cfg.HA || !cfg.AutoMigrate || cfg.ConfigRolloutEnrollment != "" {
		return report, errors.Join(expected.Validate(), errors.New("security rollback requires an enrolled signed singleton and exact target manifest"))
	}
	directory := filepath.Join(cfg.Upgrade.StateDirectory, "unattended")
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return report, errors.Join(err, errors.New("security rollback requires existing private unattended state"))
	}
	// The coordinator lock is free only when the stopped target is really gone.
	for _, name := range []string{"coordinator.lock", "security-prepare.lock"} {
		lock := flock.New(filepath.Join(directory, name))
		held, lockErr := lock.TryLock()
		if lockErr != nil || !held {
			return report, errors.Join(lockErr, errors.New("security rollback needs "+name+": the target or another preparation is still running"))
		}
		defer func() { err = errors.Join(err, lock.Unlock()) }()
	}
	journalPath := filepath.Join(directory, "operation.json")
	journal, err := readAutomaticJournal(journalPath)
	if err != nil {
		return report, err
	}
	if journal != nil && journal.Format != containerJournalFormat {
		return report, errors.New("a container rollback refuses a journal of another deployment kind")
	}
	if journal != nil && journal.Phase == "preparing" && journal.Aborted == rolledBackAborted {
		if journal.Target.ManifestSHA256 != expected {
			return report, errors.New("journaled target differs from the selected manifest")
		}
		// A retry after a rollback that finished: the snapshot was restored then, and the report must say so.
		report.Result, report.Reason = "restored", "an earlier rollback restored the fenced snapshot"
		return report, nil
	}
	if journal == nil || journal.Phase == "complete" || journal.Phase == "preparing" {
		report.Result, report.Reason = "not-needed", "no upgrade past preparation is journaled"
		return report, nil
	}
	if journal.Target.ManifestSHA256 != expected {
		return report, errors.New("journaled target differs from the selected manifest")
	}
	if !journal.namesBackup() {
		return report, errors.New("journal names no backup to restore")
	}
	var enrollment unattendedEnrollment
	raw, err := readUnattendedPrivate(filepath.Join(directory, "enrollment.json"))
	if err == nil {
		err = definitions.DecodeStrict(raw, &enrollment)
	}
	if err != nil || enrollment.Format != "hikyo.unattended-enrollment/v1" || enrollment.Instance != journal.Instance {
		return report, errors.Join(err, errors.New("unattended enrollment differs from the journaled installation"))
	}
	public, err := backupreceipt.ReadPublicArtifact(filepath.Join(directory, "operator.pub"), 1<<20)
	if err != nil || releaseidentity.Hash(public) != enrollment.Operator {
		return report, errors.Join(err, errors.New("unattended operator pin differs from enrolled custody"))
	}
	root, err := resolveRootKey(cfg, Logger(cfg.Dev))
	if err != nil {
		return report, err
	}
	defer crypto.Zero(root)
	vault, err := upgradecustody.OpenLocal(filepath.Join(directory, "custody"), root, journal.Instance)
	if err != nil {
		return report, err
	}
	defer vault.Close()

	return rollbackFencedSnapshot(ctx, cfg, securityRollbackRun{database: securityDatabase(cfg), scratch: directory, operatorDirectory: cfg.Upgrade.StateDirectory, operatorOwner: os.Geteuid(),
		journalPath: journalPath, journal: journal, public: public, unlock: vault.BackupUnlock()})
}

// securityRollbackRun is what an owner supplies to the shared restore: where its
// custody, scratch space and journal live. The deployment kind comes from the
// journal's format and nothing a caller passes.
type securityRollbackRun struct {
	database          upgrade.Config
	scratch           string
	operatorDirectory string
	operatorOwner     int
	journalPath       string
	journal           *automaticJournal
	public            []byte
	unlock            backup.Unlock
	// beforeRelease runs, once the live database is the fenced source, just before the
	// journal is rewritten to a state the source accepts. Native reinstalls the source here.
	beforeRelease func(context.Context) error
}

// namesBackup is whether the journal still points at the evidence and ciphertext a rollback restores from.
func (j *automaticJournal) namesBackup() bool {
	return j.Runtime.EvidenceDirectory != "" && j.Runtime.CiphertextPath != "" && j.Runtime.BundleDirectory != ""
}

const (
	hostJournalFormat      = "hikyo.host-upgrade/v1"
	containerJournalFormat = "hikyo.container-upgrade/v1"
)

// liveSnapshotFloor is the floor the control row retains: a bundle or target the live row has
// revoked is refused. Only an absent row (a rollback that crashed mid-restore) has none.
func liveSnapshotFloor(ctx context.Context, database upgrade.Config) (releaseidentity.SnapshotFloor, error) {
	state, err := upgrade.InspectControl(ctx, database)
	if errors.Is(err, upgrade.ErrAbsent) {
		return releaseidentity.SnapshotFloor{}, nil
	}
	return state.Floor, err
}

func rollbackFencedSnapshot(ctx context.Context, cfg *config.Config, run securityRollbackRun) (securityRollbackReport, error) {
	report := securityRollbackReport{Format: securitydeployment.RollbackReportFormat}
	journal, database := run.journal, run.database
	floor, err := liveSnapshotFloor(ctx, database)
	if err != nil {
		return report, err
	}
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return report, err
	}
	bundle, err := upgradebundle.Load(ctx, journal.Runtime.BundleDirectory, pinned, floor)
	if err != nil {
		return report, err
	}
	plan, err := bundle.Plan(journal.Source, journal.Target)
	if err != nil || plan.Digest() != journal.Route {
		return report, errors.Join(err, errors.New("journaled route differs from the authenticated bundle"))
	}
	receipt, err := backupreceipt.ReadPublicArtifact(filepath.Join(journal.Runtime.EvidenceDirectory, "receipt.json"), backupreceipt.MaxArtifactBytes)
	if err != nil {
		return report, err
	}
	ciphertext, err := backupreceipt.PinCiphertext(ctx, journal.Runtime.CiphertextPath, run.scratch)
	if err != nil {
		return report, err
	}
	defer ciphertext.Close()
	// Authenticating the ciphertext is rule 1; the live generation and the
	// attestation's expiry are deliberately not consulted, because the target
	// advanced the first and the second may have lapsed.
	archive, err := backupreceipt.AuthenticateArchive(ctx, ciphertext, receipt, plan, run.unlock, run.scratch)
	if err != nil {
		return report, err
	}
	defer archive.Close()
	snapshot := archive.Snapshot()
	if snapshot.InstanceID != journal.Instance || snapshot.SourceIdentity != journal.Source.Identity || snapshot.Engine != database.Engine || archive.PlanDigest() != journal.Route {
		return report, errors.New("backup names another installation, source or route than the journal")
	}
	incarnation := string(snapshot.RecoveryIncarnation)
	basis := upgrade.RollbackBasis{Instance: snapshot.InstanceID, Source: snapshot.SourceIdentity, Target: journal.Target, Route: journal.Route, BackupID: string(snapshot.BackupID),
		SourceGeneration: snapshot.SourceGeneration, RestoreEpoch: snapshot.RestoreEpoch, RecoveryIncarnation: incarnation,
		Rolling: journal.Phase == "rolling-back", Database: journal.RollbackDatabase, HostFenced: journal.Format == hostJournalFormat}

	restored := false
	err = upgradegate.WithInstalledOperatorOwnedBy(ctx, run.operatorDirectory, run.operatorOwner, run.public, func(custody upgradegate.OperatorCustody) error {
		if err := custody.CheckSource(journal.Instance, snapshot.RestoreEpoch); err != nil {
			return err
		}
		var err error
		restored, err = rollbackJournaled(ctx, cfg, database, run.journalPath, journal, archive, plan, basis, run.beforeRelease)
		return err
	})
	if err != nil {
		return report, err
	}
	if !restored {
		// Only the attestation and journal of a source the target never wrote were left to clear.
		report.Result, report.Reason = "not-needed", "the live database already is the fenced source; the target wrote nothing to undo"
		return report, nil
	}
	report.Result, report.Reason = "restored", fmt.Sprintf("fenced snapshot %s is the live database again", basis.BackupID)
	return report, nil
}

// rolledBackAborted is what a finished rollback leaves in the journal's Aborted field.
const rolledBackAborted = "rolled back to the fenced snapshot"

// rollbackJournaled is the locked part: decide from the live row, enter
// "rolling-back", restore, then leave the journal where the source accepts it.
// restored is whether a snapshot was put back, in this call or in an earlier one that
// crashed before leaving: "rolling-back" in the journal says it was.
func rollbackJournaled(ctx context.Context, cfg *config.Config, database upgrade.Config, journalPath string, journal *automaticJournal, archive *backupreceipt.AuthenticatedArchive, plan upgradecompat.Plan, basis upgrade.RollbackBasis, beforeRelease func(context.Context) error) (restored bool, err error) {
	err = upgrade.WithRollbackLock(ctx, database, func(rb *upgrade.Rollback) error {
		needed, err := rb.Decide(ctx, basis)
		if err != nil {
			return err
		}
		if needed {
			identity, err := rb.Identity(ctx)
			if err != nil {
				return err
			}
			if journal.Phase != "rolling-back" {
				rolling := *journal
				rolling.Phase, rolling.RollbackDatabase = "rolling-back", identity
				if err := writeAutomaticJournal(journalPath, &rolling); err != nil {
					return err
				}
			}
			admission, err := rb.Admit(basis)
			if err != nil {
				return err
			}
			if err := restoreFencedSnapshot(ctx, cfg, archive, plan, admission); err != nil {
				return err
			}
			if again, err := rb.Decide(ctx, basis); err != nil || again {
				return errors.Join(err, errors.New("restored database is not the fenced source"))
			}
		}
		// An untouched source under a journal past "preparing" (a coordinator killed after
		// "write-intent") leaves through the same exit: the stopped target wrote nothing.
		// The attestation licensed the target; without it a restarted target cannot cross write-intent again.
		for _, name := range []string{"attestation.json", "attestation.sigstore.json"} {
			if err := os.Remove(filepath.Join(journal.Runtime.EvidenceDirectory, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if beforeRelease != nil {
			if err := beforeRelease(ctx); err != nil {
				return err
			}
		}
		done := *journal
		done.Phase, done.Aborted, done.Hop, done.RollbackDatabase = "preparing", rolledBackAborted, 0, ""
		done.Runtime.EvidenceDirectory, done.Runtime.CiphertextPath = "", ""
		restored = needed || journal.Phase == "rolling-back"
		return writeAutomaticJournal(journalPath, &done)
	})
	return restored, err
}

func restoreFencedSnapshot(ctx context.Context, cfg *config.Config, archive *backupreceipt.AuthenticatedArchive, plan upgradecompat.Plan, admission upgrade.RollbackAdmission) error {
	if cfg.Store.Engine == config.EnginePostgres {
		return store.RollbackUpgradePostgres(ctx, storeConfig(cfg), archive, plan, admission, store.MigrationsFS, "migrations/postgres")
	}
	reader, err := archive.Open()
	if err != nil {
		return err
	}
	_, err = store.RollbackUpgradeSQLite(ctx, reader, plan, admission)
	return err
}
