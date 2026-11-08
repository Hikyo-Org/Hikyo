package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
)

// errNativeRolledBack is the whole outcome of a native rollback that put the source
// back and proved it serving. It counts as a disruptive failure, like the
// containers', and says plainly that the fix is not applied.
var errNativeRolledBack = errors.Join(securitymaintenance.ErrNotStarted, securitymaintenance.ErrSourceRestored, securitymaintenance.ErrRolledBack)

// errNativeSourceRestored is the outcome of a rollback that stopped the source and restarted it with no
// snapshot to restore, because the target had written nothing. It is still an outage, and no rollback.
var errNativeSourceRestored = errors.Join(securitymaintenance.ErrNotStarted, securitymaintenance.ErrSourceRestored)

// nativeRestoredOutcome names what a finished rollback did, by whether it put a snapshot back.
func nativeRestoredOutcome(snapshotRestored bool) error {
	if snapshotRestored {
		return errNativeRolledBack
	}
	return errNativeSourceRestored
}

// AutomaticUpgradeRolledBackExitCode is the exit of a verified target coordinator
// that rolled the database back and restarted the source.
const AutomaticUpgradeRolledBackExitCode = 25

const nativeSourceRecordFormat = "hikyo.native-security-source/v1"

// nativeSourceRecord is what the rollback needs to bring the source back: the exact
// binary and the runtime evidence it boots from. It is written before the stop and
// lives beside the host journal, not in it, because the source's own coordinator
// decodes that journal strictly and must still accept what a rollback leaves there.
// Its presence also marks the operation as a guarded security update, so a manual
// `hikyo upgrade` is never rolled back by it.
type nativeSourceRecord struct {
	Format       string                      `json:"format"`
	Instance     string                      `json:"instance"`
	Target       releaseidentity.Identity    `json:"target"`
	Route        releaseidentity.Digest      `json:"route"`
	Source       releaseidentity.Identity    `json:"source"`
	BinarySHA256 releaseidentity.Digest      `json:"binary_sha256"`
	Runtime      hostupgrade.RuntimeEvidence `json:"runtime"`
	// RolledBack is written once the snapshot is back. It outlives the journal, because the coordinator can
	// die before it records the rollback; a later Reconcile reads it and the next Prepare removes it.
	RolledBack bool `json:"rolled_back,omitempty"`
}

func nativeSourceRecordPath(stateDirectory string) string {
	return filepath.Join(stateDirectory, "operation-source.json")
}

func writeNativeSourceRecord(stateDirectory string, record nativeSourceRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return writeAutomaticFile(nativeSourceRecordPath(stateDirectory), raw, 0600)
}

// recordNativeSource is what the guarded update writes before its journal, so that
// every journal a rollback may meet has the source it must put back.
func recordNativeSource(stateDirectory string, journal *automaticJournal, guard nativeSecuritySourceGuard, evidence hostupgrade.RuntimeEvidence) error {
	return writeNativeSourceRecord(stateDirectory, nativeSourceRecord{Format: nativeSourceRecordFormat, Instance: journal.Instance, Target: journal.Target, Route: journal.Route,
		Source: guard.Source.Identity, BinarySHA256: guard.BinarySHA256, Runtime: nativeRestartEvidence(evidence, journal.Runtime.OperatorPublicKey)})
}

func removeNativeSourceRecord(stateDirectory string) error {
	if err := os.Remove(nativeSourceRecordPath(stateDirectory)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// settleNativeSourceRecord follows the journal's removal, never precedes it: a journal without its record
// can be neither released nor rolled back. A record saying the attempt rolled back stays.
func settleNativeSourceRecord(stateDirectory string) error {
	record, err := readNativeSourceRecord(stateDirectory)
	if err != nil || record == nil || record.RolledBack {
		return err
	}
	return removeNativeSourceRecord(stateDirectory)
}

// clearNativeAttempt puts the journal back to previous (or removes it) and only then settles the
// record, so a failed journal write never leaves a journal without its record.
func clearNativeAttempt(journalPath string, previous *automaticJournal, stateDirectory string) error {
	if err := restoreAutomaticUnstartedJournal(journalPath, previous); err != nil {
		return err
	}
	return settleNativeSourceRecord(stateDirectory)
}

// readNativeSourceRecord returns nil when no guarded operation wrote one.
func readNativeSourceRecord(stateDirectory string) (*nativeSourceRecord, error) {
	path := nativeSourceRecordPath(stateDirectory)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 1<<16 {
		return nil, errors.New("invalid native source record")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var record nativeSourceRecord
	if definitions.DecodeStrict(raw, &record) != nil || record.Format != nativeSourceRecordFormat || record.Source.Validate() != nil || record.Target.Validate() != nil ||
		record.Route.Validate() != nil || record.BinarySHA256.Validate() != nil || record.Runtime.BundleDirectory == "" || record.Runtime.OperatorPublicKey == "" {
		return nil, errors.New("invalid native source record")
	}
	return &record, nil
}

// bind ties the record to the journal it was written for.
func (r *nativeSourceRecord) bind(j *automaticJournal) error {
	if r == nil || j == nil || j.Format != hostJournalFormat || r.Instance != j.Instance || r.Target != j.Target || r.Route != j.Route || j.Source.Identity != (releaseidentity.Source{Release: r.Source}) {
		return errors.New("the native source record belongs to another operation than the host journal")
	}
	return nil
}

// nativeRollbackHost is the part of the host a rollback uses. *hostupgrade.Host satisfies it.
type nativeRollbackHost interface {
	nativeSecurityHost
	nativeSourceHost
	StageCandidate(string, string) (string, error)
	InstallBinary(context.Context, string, string) error
	ConfigureRuntime(context.Context, hostupgrade.RuntimeEvidence) error
	Maintenance() (bool, error)
	RuntimeUID() int
}

// nativeRollback runs in the verified target's executable under the host upgrade lock.
// Only that release is known to read the archive it wrote, and the same code restored
// it to scratch before the stop. The service is stopped and proven stopped first.
type nativeRollback struct {
	host        nativeRollbackHost
	database    upgrade.Config
	journalPath string
	// verifySource authenticates the source release from the local signed cache.
	verifySource func(context.Context, nativeSecurityInstallation, upgrade.State) (preparedSecurityRelease, error)
	openCustody  func(instance string) (*upgradecustody.Vault, error)
}

// nativeRollbackPhases are the journal phases a rollback accepts: those past the backup fence,
// where the target may have written, or may have been killed with nothing written.
// "preparing" is the ordinary pre-write exit and never reaches here.
var nativeRollbackPhases = map[string]bool{"proved": true, "write-intent": true, "schema-applied": true, "hop-healthy": true, "restore-required": true, "rolling-back": true}

// run restores the fenced snapshot, reinstalls the source, restarts it and proves
// the host fence lifted. It leaves the journal removed by its caller, and returns
// served only once the source is serving; snapshotRestored is whether a snapshot was
// put back rather than a source found untouched. Any doubt returns an error with the
// service stopped and the journal pinned.
func (r nativeRollback) run(ctx context.Context) (served, snapshotRestored bool, err error) {
	c := r.host.Config()
	journal, err := readAutomaticJournal(r.journalPath)
	if err != nil {
		return false, false, err
	}
	record, err := readNativeSourceRecord(c.StateDirectory)
	if err != nil {
		return false, false, err
	}
	if journal == nil || record == nil || !nativeRollbackPhases[journal.Phase] {
		return false, false, errors.New("no guarded native security update is in flight past preparation")
	}
	if err := record.bind(journal); err != nil {
		return false, false, err
	}
	if !journal.namesBackup() {
		return false, false, errors.New("journal names no backup to restore")
	}
	if err := r.host.FenceAndStop(ctx); err != nil {
		return false, false, err
	}
	vault, err := r.openCustody(journal.Instance)
	if err != nil {
		return false, false, err
	}
	defer vault.Close()
	public, err := backupreceipt.ReadPublicArtifact(journal.Runtime.OperatorPublicKey, 1<<20)
	if err != nil {
		return false, false, err
	}
	if !bytes.Equal(public, vault.PublicKey()) {
		return false, false, errors.New("the journaled operator key differs from the custody vault")
	}
	cfg := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: r.database.Path}}
	report, err := rollbackFencedSnapshot(ctx, cfg, securityRollbackRun{database: r.database, scratch: c.StateDirectory, operatorDirectory: c.InstallationDirectory, operatorOwner: r.host.RuntimeUID(),
		journalPath: r.journalPath, journal: journal, public: public, unlock: vault.BackupUnlock(),
		beforeRelease: func(ctx context.Context) error {
			if err := r.reinstallSource(ctx, c, *record); err != nil {
				return err
			}
			return chownSQLiteFiles(r.database.Path, r.host.RuntimeUID())
		}})
	if err != nil {
		return false, false, err
	}
	var markErr error
	if report.Result == "restored" {
		record.RolledBack = true
		// Not a reason to leave the service down.
		// shortcut: a failed write loses this rollback from the disruptive-attempt count if the coordinator also dies before reconcile; make the write fatal if counts must never be lost.
		markErr = writeNativeSourceRecord(c.StateDirectory, *record)
	}
	// The journal now says "preparing" with the source installed: the ordinary native
	// exit starts the source, checks the untouched row and lifts the host fence.
	released, err := readAutomaticJournal(r.journalPath)
	if err != nil {
		return false, false, err
	}
	if released == nil || released.Phase != "preparing" {
		return false, false, errors.New("the rolled-back journal is not the ordinary pre-write state")
	}
	restored, err := restoreNativeSource(ctx, r.host, automaticStore{r.database}, released, c.StateDirectory, record.BinarySHA256, nil)
	if !restored {
		return false, false, errors.Join(err, markErr)
	}
	return true, report.Result == "restored", errors.Join(err, markErr)
}

// reinstallSource puts the exact source binary and its runtime evidence back. It is
// repeatable: a crash here leaves the journal at "rolling-back" and the next run does it again.
func (r nativeRollback) reinstallSource(ctx context.Context, c hostupgrade.Config, record nativeSourceRecord) error {
	state, err := upgrade.InspectControl(ctx, r.database)
	if err != nil {
		return err
	}
	source, err := r.verifySource(ctx, nativeSecurityInstallation{Host: r.host, Database: r.database, SourceBundle: record.Runtime.BundleDirectory, SourceCache: filepath.Join(c.StateDirectory, "downloads")}, state)
	if err != nil {
		return err
	}
	if source.Target.Identity != record.Source || source.Target.BinarySHA256 != record.BinarySHA256 || state.Applied != (releaseidentity.Source{Release: record.Source}) {
		return errors.New("the authenticated source binary differs from the one journaled before the stop")
	}
	candidate, err := r.host.StageCandidate(source.Target.BinaryPath, string(record.BinarySHA256))
	if err != nil {
		return err
	}
	if err := r.host.InstallBinary(ctx, candidate, string(record.BinarySHA256)); err != nil {
		return err
	}
	return r.host.ConfigureRuntime(ctx, record.Runtime)
}

// nativeRestartEvidence is what the source boots from again: its bundle and operator
// key, and its release manifest when an earlier upgrade recorded one. Backup evidence
// and the legacy-writers flag belong to the upgrade that failed.
func nativeRestartEvidence(source hostupgrade.RuntimeEvidence, journalKey string) hostupgrade.RuntimeEvidence {
	if source.OperatorPublicKey == "" {
		source.OperatorPublicKey = journalKey
	}
	source.EvidenceDirectory, source.CiphertextPath, source.LegacyWritersStopped = "", "", false
	return source
}

// defaultNativeRollbackTimeout bounds a rollback the way the containers' is bounded.
const defaultNativeRollbackTimeout = 30 * time.Minute

// runNativeSecurityRollback is `hikyo upgrade --security-rollback`: the coordinator's
// answer, on a later run, to a guarded update that never finished. It resolves the
// target from the local signed cache, never from the release API, so a host without
// network can still be put back. The current build hands off to the target's own
// executable first, exactly as an update does.
func runNativeSecurityRollback(ctx context.Context, host *hostupgrade.Host, c hostupgrade.Config, configuration, journalPath string, previous *automaticJournal, expected *releaseidentity.Identity, handoff bool) error {
	if expected == nil || previous == nil || previous.Phase == "complete" || previous.Target != *expected {
		return errors.New("--security-rollback needs an unfinished host operation for the exact release it names")
	}
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return err
	}
	cache := filepath.Join(c.StateDirectory, "downloads")
	installer, err := selfupdate.NewInstaller(selfupdate.Config{StateDir: cache, TrustRootBase64: base64.StdEncoding.EncodeToString(pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(pinned.RecoveryPublicKey)})
	if err != nil {
		return err
	}
	database, err := nativeDatabase(c, host.Environment())
	if err != nil {
		return err
	}
	floor, err := liveSnapshotFloor(ctx, database)
	if err != nil {
		return err
	}
	bundle, err := upgradebundle.Load(ctx, previous.Runtime.BundleDirectory, pinned, floor)
	if err != nil {
		return err
	}
	current, _, err := buildcompat.Current()
	if err != nil {
		return err
	}
	if node, matchErr := bundle.MatchBuild(current); matchErr != nil || node.Identity() != previous.Target {
		if handoff {
			return errors.New("verified target binary does not match its signed compatibility declaration")
		}
		target, err := installer.VerifyCachedRelease(ctx, selfupdate.PreparedNightly{Identity: previous.Target, Directory: selfupdate.CachedReleaseDirectory(cache, previous.Target), BundleDirectory: previous.Runtime.BundleDirectory}, floor)
		if err != nil {
			return fmt.Errorf("the target release is not in the local verified cache: %w", err)
		}
		next, err := automaticReleaseHandoffWithAuthorization(target, configuration, expected, nativeSecurityAuthorization{Rollback: true})
		if err != nil {
			return err
		}
		return next
	}
	served, snapshotRestored, err := newNativeRollback(host, c, database, journalPath).run(ctx)
	if !served {
		return err
	}
	// The source serves and its fence is lifted; the journal described the failed attempt.
	return errors.Join(nativeRestoredOutcome(snapshotRestored), err, clearNativeAttempt(journalPath, nil, c.StateDirectory))
}

// refetchNativeRollbackSource is all `hikyo upgrade` does at a "rolling-back" journal. The next coordinator
// run finishes the rollback, and the one thing it can need from a person is the source release again.
func refetchNativeRollbackSource(ctx context.Context, stateDirectory string, expected *releaseidentity.Identity, prepare func(context.Context, releaseidentity.Identity) error, out io.Writer) error {
	record, err := readNativeSourceRecord(stateDirectory)
	if err != nil {
		return err
	}
	if record == nil {
		return errors.New("the security rollback has no source record, so nothing can finish it; the service stays fenced: keep the journal and state directory and restore this host from your own backup")
	}
	if expected == nil || *expected != record.Source {
		return errors.New("a security rollback is unfinished and the next coordinator run finishes it; if its source release is missing, run " + nativeSourceEvidenceCommand(record.Source))
	}
	if err := prepare(ctx, record.Source); err != nil {
		return err
	}
	fmt.Fprintf(out, "Fetched source release %s. The next coordinator run finishes the rollback.\n", record.Source.Version)
	return nil
}

// newNativeRollback is the rollback both the failed update and a later coordinator run use.
func newNativeRollback(host nativeRollbackHost, c hostupgrade.Config, database upgrade.Config, journalPath string) nativeRollback {
	return nativeRollback{host: host, database: database, journalPath: journalPath, verifySource: verifyNativeSecuritySource,
		openCustody: func(instance string) (*upgradecustody.Vault, error) { return openAutomaticCustody(c, instance, nil) }}
}
