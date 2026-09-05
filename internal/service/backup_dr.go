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
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// TriggerScheduled is the in-process scheduler's export.
const TriggerScheduled ExportTrigger = "scheduled"

// DrillStaleAfter is the ops spec's restore-test cadence (§ 11: quarterly,
// doctor warns past 90 days).
const DrillStaleAfter = 90 * 24 * time.Hour

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
	RPOExceeded       bool
	LastFailureAt     time.Time
	LastFailureReason string
	LastPruneAt       time.Time
	LastDrillAt       time.Time
	LastDrillOK       bool
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
		LastPruneAt: st.LastPruneAt, LastDrillAt: st.LastDrillAt, LastDrillOK: st.LastDrillOK,
	}
	if !st.LastSuccessAt.IsZero() {
		h.ArtifactAge = max(now.Sub(st.LastSuccessAt), 0)
	}
	if policy.Scheduled {
		h.RPOExceeded = st.LastSuccessAt.IsZero() || h.ArtifactAge > policy.RPO
	}
	h.DrillStale = st.LastDrillAt.IsZero() || !st.LastDrillOK || now.Sub(st.LastDrillAt) > DrillStaleAfter
	return h
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
	// RetainCount archives are always kept, newest first, regardless of age.
	RetainCount int
	// RetainDays is the age past which an archive outside the retained set
	// is deleted. Bounded by configuration to the ops spec's 180-day ceiling.
	RetainDays int
}

// PruneResult reports what one run did.
type PruneResult struct {
	Deleted []string
	Kept    int
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

// planPrune decides which of the named archives to delete. It is pure so the
// policy can be tested against a directory listing without a filesystem:
// only complete archives of THIS engine are considered, ordered by the
// timestamp in the name (never mtime, which a copy resets), the newest
// RetainCount survive unconditionally, and of the rest only those older than
// RetainDays are returned, oldest first. Returning them oldest-first is what
// makes "stop on the first error" preserve the newest: whatever a failing
// unlink leaves behind is the younger end of the list.
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
	if len(archives) <= retain {
		return nil, len(archives)
	}
	cutoff := now.Add(-time.Duration(policy.RetainDays) * 24 * time.Hour)
	candidates := archives[retain:]
	kept = retain
	for i := len(candidates) - 1; i >= 0; i-- {
		// The persisted newest-successful archive is never a deletion
		// candidate, whatever its filename timestamp: a wall-clock rollback can
		// leave it with an older name than an earlier archive, and the
		// unconditional guarantee is about the last export that SUCCEEDED, not
		// the lexically newest name.
		if candidates[i].name == protect || !candidates[i].at.Before(cutoff) {
			kept++
			continue
		}
		deleteNames = append(deleteNames, candidates[i].name)
	}
	return deleteNames, kept
}

// Prune applies the retention policy to dir and records the run. Deletion
// is oldest-first and stops at the first error, so a half-failed prune can
// only ever have removed the oldest archives; the newest successful export
// is never a candidate at all. A failed run records nothing, which leaves
// the last-prune timestamp stale for the metric and the health read to
// report.
func (s *Backup) Prune(ctx context.Context, dir string, policy PrunePolicy) (PruneResult, error) {
	if dir == "" {
		return PruneResult{}, errors.New("backup prune needs a destination directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return PruneResult{}, fmt.Errorf("backup prune: list %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	state, err := s.State(ctx)
	if err != nil {
		return PruneResult{}, err
	}
	toDelete, kept := planPrune(names, s.DB.Engine(), s.now(), policy, state.LastArtifactName)
	remove := s.removeFile
	if remove == nil {
		remove = os.Remove
	}
	result := PruneResult{Kept: kept}
	for _, name := range toDelete {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return result, fmt.Errorf("backup prune: remove %s: %w", name, err)
		}
		result.Deleted = append(result.Deleted, name)
	}
	now := s.now()
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		proof, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return err
		}
		return r.BackupState().SetPruneSuccess(ctx, proof, now)
	})
	return result, err
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
