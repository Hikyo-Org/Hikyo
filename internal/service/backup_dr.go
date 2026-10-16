package service

// The disaster-recovery program around the export (#145, ops spec § 11):
// the scheduled export's record and loud failure, the retention pruner, the
// restore drill's verdict, and the health surface doctor / the instance
// health read / /metrics assemble from the single backup_state row.
//
// Two things this file is careful NOT to do. It never loads the root key:
// every write here is bookkeeping about ciphertext archives, so the custody
// boundary the export keeps stays intact. And it never names a recipient,
// an identity or a key in anything it persists: archive names, byte counts,
// versions, elapsed times and bounded failure classes only.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// TriggerScheduled is the in-process scheduler's export.
const TriggerScheduled ExportTrigger = "scheduled"

// DrillStaleAfter is the ops spec's restore-test cadence (§ 11: quarterly,
// doctor warns past 90 days).
const DrillStaleAfter = 90 * 24 * time.Hour

// BackupRetentionCeiling is the managed-archive age ceiling. Recovery safety
// can prevent deletion; that is a persistent breach, never a successful prune.
const BackupRetentionCeiling = 180 * 24 * time.Hour

const backupArtifactLock = "backup-artifacts"
const backupPruneStaleAfter = 24 * time.Hour

// BackupPolicy is the resolved DR schedule the health surface reasons
// against. Scheduled false means no export policy is configured: nothing
// runs, and health reports that rather than an RPO breach.
type BackupPolicy struct {
	Scheduled   bool
	Interval    time.Duration
	RPO         time.Duration
	RetainCount int
	RetainDays  int
	RTOTarget   time.Duration
}

// BackupHealth is the DR half of the operator health read. Zero times mean
// "never"; the two verdict booleans are computed here, once, so doctor, the
// API and the metrics never disagree about what "exceeded" means.
type BackupHealth struct {
	Scheduled     bool
	LastSuccessAt time.Time
	ArtifactAge   time.Duration
	RPO           time.Duration
	// RPOExceeded is true when exports are scheduled and the newest
	// successful artifact is older than the RPO, or there has never been one.
	RPOExceeded             bool
	LastFailureAt           time.Time
	LastFailureReason       string
	LastPruneAt             time.Time
	LastPruneAttemptAt      time.Time
	RetainedExpiredArchives int64
	RetentionBreach         bool
	RetentionReason         string
	LastDrillAt             time.Time
	LastDrillOK             bool
	// DrillStale is true when no successful drill is younger than
	// DrillStaleAfter, whether or not exports are scheduled: a restore test
	// is owed regardless of how the archives are produced.
	DrillStale bool
}

// backupHealth assembles the verdicts from the persisted row.
func backupHealth(now time.Time, policy BackupPolicy, st store.BackupState) BackupHealth {
	h := BackupHealth{
		Scheduled: policy.Scheduled, RPO: policy.RPO,
		LastSuccessAt: st.LastSuccessAt, LastFailureAt: st.LastFailureAt, LastFailureReason: st.LastFailureReason,
		LastPruneAt: st.LastPruneAt, LastPruneAttemptAt: st.LastPruneAttemptAt,
		RetainedExpiredArchives: st.RetainedExpiredArchives, LastDrillAt: st.LastDrillAt, LastDrillOK: st.LastDrillOK,
	}
	if !st.LastSuccessAt.IsZero() {
		h.ArtifactAge = max(now.Sub(st.LastSuccessAt), 0)
	}
	if policy.Scheduled {
		h.RPOExceeded = st.LastSuccessAt.IsZero() || h.ArtifactAge > policy.RPO
	}
	switch {
	case backupClockUncertain(now, st):
		h.RetentionReason = "clock_uncertain"
	case backupNameExpired(st.LastArtifactName, now):
		h.RetentionReason = "protected_expired"
	case st.PruneFailureReason != "":
		h.RetentionReason = st.PruneFailureReason
	case st.RetainedExpiredArchives > 0:
		h.RetentionReason = "protected_expired"
	case backupRecoveryIdentityUncertain(st):
		h.RetentionReason = "recovery_uncertain"
	case !policy.Scheduled:
		h.RetentionReason = "not_scheduled"
	case policy.Scheduled && st.LastPruneAttemptAt.IsZero():
		h.RetentionReason = "not_checked"
	case policy.Scheduled && now.Sub(st.LastPruneAttemptAt) > backupPruneStaleAfter:
		h.RetentionReason = "stale"
	}
	// An absent schedule is configuration information, not evidence of a
	// retention breach. Keep it explicit without inventing mandatory backups.
	// Earlier cases retain actual failures/expired copies even after disablement.
	h.RetentionBreach = h.RetentionReason != "" && h.RetentionReason != "not_scheduled"
	h.DrillStale = st.LastDrillAt.IsZero() || !st.LastDrillOK || now.Sub(st.LastDrillAt) > DrillStaleAfter
	return h
}

// Empty configuration has no claimed recovery identity. An existing but
// incomplete success record is different: it cannot justify recovery safety,
// including a legacy record predating ciphertext-digest verification.
func backupRecoveryIdentityUncertain(st store.BackupState) bool {
	known := !st.LastSuccessAt.IsZero() || st.LastArtifactName != "" || st.LastArtifactBytes != 0 || st.LastArtifactSHA256 != ""
	return known && (st.LastSuccessAt.IsZero() || !archiveName.MatchString(st.LastArtifactName) || st.LastArtifactBytes <= 0 || len(st.LastArtifactSHA256) != 64)
}

// Any future operational timestamp is evidence of a wall-clock rollback.
func backupClockUncertain(now time.Time, st store.BackupState) bool {
	for _, at := range []time.Time{st.LastSuccessAt, st.LastFailureAt, st.LastPruneAt, st.LastPruneAttemptAt, st.LastDrillAt} {
		if at.After(now) {
			return true
		}
	}
	return false
}

// State reads the DR health row under scheduler authority. It is the
// scheduler's per-job success probe and the drill's before/after read.
func (s *Backup) State(ctx context.Context) (store.BackupState, error) {
	var out store.BackupState
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return err
		}
		out, err = r.BackupState().Get(ctx, p)
		return err
	})
	return out, err
}

// LastExportSuccess is the scheduler's health probe shape.
func (s *Backup) LastExportSuccess(ctx context.Context) (time.Time, bool, error) {
	st, err := s.State(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	return st.LastSuccessAt, !st.LastSuccessAt.IsZero(), nil
}

// Due reports whether the scheduled export should run now: the scheduler
// ticks hourly and the interval is at least an hour, so the job gates
// itself on the persisted last success rather than on the tick. A never-
// exported instance is due immediately (startup catch-up).
func (s *Backup) Due(ctx context.Context, interval time.Duration) (bool, error) {
	st, err := s.State(ctx)
	if err != nil {
		return false, err
	}
	if st.LastSuccessAt.IsZero() {
		return true, nil
	}
	// A small tolerance so a job that ran at 02:00:00.4 is due again on the
	// 02:00 tick a day later rather than skidding to 03:00.
	return s.now().Sub(st.LastSuccessAt) >= interval-time.Minute, nil
}

// RecordFailure is the loud half of a scheduled export that did not happen:
// a configured policy was not honoured. It lands on the instance trail with
// a bounded error class (never the error text, which can quote a path) and
// updates the health row so doctor, the API and the metric all say so until
// the next success. Durable on purpose: an export failure that only reached
// a log line would convert the RPO to infinity silently.
func (s *Backup) RecordFailure(ctx context.Context, trigger ExportTrigger, runErr error) error {
	now := s.now()
	class := backupErrorClass(runErr)
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := tx.Write(recordCtx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		proof, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return err
		}
		ev, err := newAuditEvent(ctx, audit.EventBackupExportFailed, "",
			audit.Object{Type: "backup", ID: string(trigger)}, audit.OutcomeFailure, "",
			audit.Payload{"trigger": string(trigger), "error_class": class})
		if err != nil {
			return err
		}
		ev.Actor.Class = audit.ActorSystem
		ev.OccurredAt = now
		if err := r.Audit().InsertInstance(ctx, proof, ev); err != nil {
			return err
		}
		return r.BackupState().SetExportFailure(ctx, proof, now, boundedReason(runErr))
	})
	return errors.Join(runErr, err)
}

// backupErrorClass folds an export failure into the closed class the audit
// payload carries.
func backupErrorClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, os.ErrPermission), errors.Is(err, os.ErrNotExist):
		return "destination"
	default:
		return "internal"
	}
}

// boundedReason is the operator-facing text on the health row: the error's
// own words, cut at a size that cannot become a dump. It may name the
// destination directory, which is operator infrastructure the same way the
// export event's `destination` is; it can never name key material, because
// no export path holds any.
func boundedReason(err error) string {
	const limit = 256
	msg := err.Error()
	if len(msg) > limit {
		msg = msg[:limit]
	}
	return msg
}

// PrunePolicy is the retention pruner's two bounds.
type PrunePolicy struct {
	// RetainCount archives are kept within the hard 180-day ceiling, newest first.
	RetainCount int
	// RetainDays is the age past which an archive outside the retained set
	// is deleted. No count exemption extends the hard 180-day ceiling.
	RetainDays int
}

// PruneResult reports what one run did.
type PruneResult struct {
	Deleted []string
	Kept    int
	Expired int
}

// archiveName matches exactly what publish() writes: the engine, a UTC
// second-resolution timestamp, and the optional same-second suffix. Nothing
// else in the directory is Hikyo's to touch: not `.partial` staging files,
// not the other engine's archives, not an operator's own copies.
var archiveName = regexp.MustCompile(`^hikyo-(sqlite|postgres)-(\d{8}T\d{6}Z)(?:-(\d+))?\.age$`)

type archive struct {
	name   string
	at     time.Time
	suffix int
}

// planPrune applies the soft count/age policy and hard age ceiling. Only the
// recorded successful recovery copy is exempt from the ceiling. The caller
// must establish recovery certainty before applying this plan.
func planPrune(names []string, engine store.Engine, now time.Time, policy PrunePolicy, protect string) (deleteNames []string, kept int) {
	var archives []archive
	for _, name := range names {
		m := archiveName.FindStringSubmatch(name)
		if m == nil || m[1] != string(engine) {
			continue
		}
		at, err := time.Parse("20060102T150405Z", m[2])
		if err != nil {
			continue
		}
		suffix := 0
		if m[3] != "" {
			suffix, _ = strconv.Atoi(m[3])
		}
		archives = append(archives, archive{name: name, at: at, suffix: suffix})
	}
	sort.Slice(archives, func(i, j int) bool {
		if !archives[i].at.Equal(archives[j].at) {
			return archives[i].at.After(archives[j].at)
		}
		return archives[i].suffix > archives[j].suffix
	})
	retain := max(policy.RetainCount, 1)
	cutoff := now.Add(-time.Duration(policy.RetainDays) * 24 * time.Hour)
	ceiling := now.Add(-BackupRetentionCeiling)
	for i := len(archives) - 1; i >= 0; i-- {
		item := archives[i]
		expired := item.at.Before(ceiling)
		if item.name == protect || (!expired && (i < retain || !item.at.Before(cutoff))) {
			kept++
			continue
		}
		deleteNames = append(deleteNames, item.name)
	}
	return deleteNames, kept
}

// Prune records a durable attempt before touching files. Publication recording
// and pruning share cross-process datastore serialization: a success record
// cannot advance to a file between planning and unlink. Export itself needs no
// root key and never waits on this lock while taking a datastore snapshot.
func (s *Backup) Prune(ctx context.Context, dir string, policy PrunePolicy) (PruneResult, error) {
	if dir == "" || policy.RetainCount < 1 || policy.RetainDays < 1 || policy.RetainDays > 180 {
		return PruneResult{}, errors.New("backup prune needs a directory and a valid bounded policy")
	}
	attemptID, err := newID("backup_prune")
	if err != nil {
		return PruneResult{}, err
	}
	unlock, err := lockBackupDirectory(ctx, dir)
	if err != nil {
		// Even an inaccessible directory must invalidate an earlier healthy result.
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		recordErr := tx.WriteSerialized(recordCtx, s.DB, backupArtifactLock, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			proof, proofErr := authz.SystemAuthority(authz.SiteScheduler, az.Token())
			if proofErr != nil {
				return proofErr
			}
			return r.BackupState().SetPruneResult(ctx, proof, s.now(), attemptID, "directory_unavailable", 0)
		})
		return PruneResult{}, errors.Join(err, recordErr)
	}
	defer unlock()
	priorClockUncertain := false
	// A crash or failed transaction leaves this attempt visibly incomplete,
	// preserving the previous successful timestamp instead of reporting green.
	if err := tx.WriteSerialized(ctx, s.DB, backupArtifactLock, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		proof, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return err
		}
		prior, err := r.BackupState().Get(ctx, proof)
		if err != nil {
			return err
		}
		priorClockUncertain = backupClockUncertain(s.now(), prior)
		return r.BackupState().SetPruneResult(ctx, proof, s.now(), attemptID, "in_progress", 0)
	}); err != nil {
		return PruneResult{}, err
	}
	// Hash outside the bounded transaction, under the directory lock. A record
	// from another directory can still advance instance state; compare the exact
	// identity again in the deletion transaction before trusting this proof.
	verifiedState, verifyErr := s.State(ctx)
	if verifyErr == nil {
		verifyErr = verifyBackupArtifact(ctx, filepath.Join(dir, verifiedState.LastArtifactName), verifiedState.LastArtifactBytes, verifiedState.LastArtifactSHA256)
	}
	var result PruneResult
	var runErr error
	err = tx.WriteSerialized(ctx, s.DB, backupArtifactLock, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		result, runErr = PruneResult{}, nil
		proof, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return err
		}
		finish := func(reason string, cause error) error {
			runErr = cause
			return r.BackupState().SetPruneResult(ctx, proof, s.now(), attemptID, reason, int64(result.Expired))
		}
		state, err := r.BackupState().Get(ctx, proof)
		if err != nil {
			return finish("state_unavailable", err)
		}
		// A prune of another directory can supersede our durable attempt. Do not
		// touch files unless our own committed in-progress marker is still current.
		if state.LastPruneAttemptID != attemptID || state.PruneFailureReason != "in_progress" {
			return errors.New("backup prune attempt superseded before deletion")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return finish("directory_unavailable", fmt.Errorf("backup prune: list directory: %w", err))
		}
		now := s.now()
		names := make([]string, 0, len(entries))
		protected := false
		uncertain := priorClockUncertain || backupClockUncertain(now, state) || state.LastSuccessAt.IsZero() || state.LastArtifactBytes <= 0 || state.LastArtifactSHA256 == ""
		expired := make(map[string]bool)
		for _, entry := range entries {
			m := archiveName.FindStringSubmatch(entry.Name())
			if m == nil || m[1] != string(s.DB.Engine()) {
				continue
			}
			at, err := time.Parse("20060102T150405Z", m[2])
			if err != nil {
				continue
			}
			// A non-regular managed name may hide a recovery artifact. Never follow it
			// or use it as evidence that the recorded recovery point still exists.
			if !entry.Type().IsRegular() {
				uncertain = true
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return finish("directory_unavailable", err)
			}
			names = append(names, entry.Name())
			if at.After(now) {
				uncertain = true
			}
			if at.Before(now.Add(-BackupRetentionCeiling)) {
				expired[entry.Name()] = true
			}
			if entry.Name() == state.LastArtifactName && info.Size() == state.LastArtifactBytes {
				protected = verifyErr == nil && state.LastSuccessAt.Equal(verifiedState.LastSuccessAt) && state.LastArtifactName == verifiedState.LastArtifactName && state.LastArtifactBytes == verifiedState.LastArtifactBytes && state.LastArtifactSHA256 == verifiedState.LastArtifactSHA256
			}
		}
		result.Kept, result.Expired = len(names), len(expired)
		if uncertain || !protected {
			return finish("recovery_uncertain", errors.New("backup retention unverified: preserve candidates until a durable recovery copy and clock are verified"))
		}
		toDelete, _ := planPrune(names, s.DB.Engine(), now, policy, state.LastArtifactName)
		remove := s.removeFile
		if remove == nil {
			remove = os.Remove
		}
		for _, name := range toDelete {
			if err := ctx.Err(); err != nil {
				return finish("canceled", err)
			}
			if err := remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return finish("unlink_failed", fmt.Errorf("backup prune: remove %s: %w", name, err))
			}
			result.Deleted = append(result.Deleted, name)
			result.Kept--
			if expired[name] {
				result.Expired--
			}
		}
		// Even a no-op traversal must confirm directory durability: a previous
		// unlink may be visible here after its fsync failed or its transaction died.
		syncDirectory := s.syncDirectory
		if syncDirectory == nil {
			syncDirectory = filedurability.SyncDirectory
		}
		if err := syncDirectory(dir); err != nil {
			return finish("directory_sync_failed", err)
		}
		if result.Expired > 0 {
			return finish("protected_expired", errors.New("backup retention ceiling exceeded: the last successful recovery copy remains protected"))
		}
		return finish("", nil)
	})
	return result, errors.Join(runErr, err)
}

// LastPruneSuccess is the scheduler's health probe shape for the pruner.
func (s *Backup) LastPruneSuccess(ctx context.Context) (time.Time, bool, error) {
	st, err := s.State(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	return st.LastPruneAt, !st.LastPruneAt.IsZero(), nil
}

// DrillReport is what a restore drill hands back to be recorded: identity
// of the archive, the versions involved, the clock, and the step verdicts.
// It carries no secret material by construction: there is no field for one.
type DrillReport struct {
	Archive        string
	ArchiveDigest  string
	Engine         store.Engine
	SchemaVersion  int64
	BinaryVersion  string
	Elapsed        time.Duration
	RTOTarget      time.Duration
	ValuesReadable bool
	Principal      string
	Minted         bool
	// FailedStep names the first step that did not complete; empty when
	// every step passed. A recorded failure is still a completed drill: the
	// operator learns the recovery path is broken while it is still a drill.
	FailedStep string
}

// OK reports whether every step passed AND the clock met the RTO target.
func (d DrillReport) OK() bool {
	return d.FailedStep == "" && d.ValuesReadable && d.Minted && d.Elapsed <= d.RTOTarget
}

// ErrNoSecretToProve reports that the restored instance holds no `secret`
// value at all, so the drill cannot prove protected values are readable.
// It is loud: a drill that "passed" only because there was nothing to open
// proves nothing about the recovery.
var ErrNoSecretToProve = errors.New("restored instance holds no secret value to prove readable")

// ProveValuesReadable is the drill's decrypt proof (#145). It reads ONE
// stored secret cell from the restored scratch instance under scheduler
// authority and opens it through the project sealer built on the SEPARATELY
// supplied root key, proving the custody pair (backup identity already used
// to decrypt the container, root key now) reconstitutes plaintext. It never
// returns, prints or logs the plaintext: the boolean is the whole answer.
func (s *Backup) ProveValuesReadable(ctx context.Context, kr *crypto.Keyring) (bool, error) {
	return proveValuesReadable(ctx, func(ctx context.Context, fn tx.ReadFn) error { return tx.Read(ctx, s.DB, fn) }, kr)
}

func proveValuesReadable(ctx context.Context, read func(context.Context, tx.ReadFn) error, kr *crypto.Keyring) (bool, error) {
	if kr == nil {
		return false, errors.New("service: proving values readable needs the root keyring")
	}
	var entry store.ValueEntry
	err := read(ctx, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return err
		}
		entry, err = r.Values().SampleSecretEntry(ctx, p)
		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		return false, ErrNoSecretToProve
	}
	if err != nil {
		return false, err
	}
	sealer, err := kr.ForProject(ctx, entry.OrgID, entry.ProjectID)
	if err != nil {
		return false, err
	}
	// openCell round-trips the ciphertext to plaintext; a non-nil result
	// means the pair opened it. The plaintext is discarded immediately.
	if _, err := openCell(sealer, entry); err != nil {
		return false, err
	}
	return true, nil
}

// RecordDrill writes the drill's verdict to the LIVE instance: one audit
// event on the instance trail and the health row doctor reads. The drill
// itself ran against a scratch target; this is the only thing it leaves on
// the instance it was rehearsing for.
func (s *Backup) RecordDrill(ctx context.Context, rep DrillReport) error {
	now := s.now()
	outcome := audit.OutcomeSuccess
	if !rep.OK() {
		outcome = audit.OutcomeFailure
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		ev, err := newAuditEvent(ctx, audit.EventRestoreDrillCompleted, "",
			audit.Object{Type: "backup", ID: rep.Archive}, outcome, "", audit.Payload{
				"archive":              rep.Archive,
				"archive_digest":       rep.ArchiveDigest,
				"engine":               string(rep.Engine),
				"schema_version":       rep.SchemaVersion,
				"binary_version":       rep.BinaryVersion,
				"elapsed_ms":           rep.Elapsed.Milliseconds(),
				"rto_target_ms":        rep.RTOTarget.Milliseconds(),
				"rto_met":              rep.Elapsed <= rep.RTOTarget,
				"values_readable":      rep.ValuesReadable,
				"reconciled_principal": rep.Principal,
				"credential_minted":    rep.Minted,
				"failed_step":          rep.FailedStep,
				"authority":            "local-host",
			})
		if err != nil {
			return err
		}
		if err := az.RecordAuthEvent(ctx, ev); err != nil {
			return err
		}
		proof, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return err
		}
		return r.BackupState().SetDrill(ctx, proof, store.BackupDrillRecord{
			At: now, OK: rep.OK(), Archive: rep.Archive, Elapsed: rep.Elapsed,
			BinaryVersion: rep.BinaryVersion, SchemaVersion: rep.SchemaVersion,
		})
	})
}
