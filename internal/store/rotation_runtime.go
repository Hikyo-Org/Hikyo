package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// RotationRuntime is the scheduled-rotation executor's system boundary (#148,
// slice S2a), the third domain outbox after AdapterRuntime and DynamicRuntime.
// Tenant request paths never receive it. Every statement binds the claimed run's
// immutable tenant chain and re-asserts the run's own crash fence (lease_owner,
// lease_expires_at, lease_claim_token), so a worker that lost its lease affects
// zero rows and rolls its transaction back.
type RotationRuntime struct {
	db *DB
}

func NewRotationRuntime(db *DB) *RotationRuntime { return &RotationRuntime{db: db} }

// ErrRotationFenceLost reports a settling write whose run no longer matched: the
// lease expired or was taken over, or the run left the phases the write may
// leave. The caller re-reads the run (RotationRuntime.GetRun) and follows the
// state it is now in; it never assumes which of the two happened.
var ErrRotationFenceLost = fmt.Errorf("%w: rotation run fence lost", ErrConflict)

// DueRotation is one active rotation whose next run is due.
type DueRotation struct {
	ID                     string
	OrgID                  string
	ProjectID              string
	EnvironmentID          string
	ProviderID             string
	KeyID                  string
	UsernameKeyID          string
	IntervalSeconds        int64
	RevocationDelaySeconds int64
	AuthorityPrincipalID   string
	Generation             int64
}

// RotationRun is one claimed or re-read run joined to its rotation's immutable
// configuration. Empty strings and zero times are the absent values.
type RotationRun struct {
	ID                     string
	OrgID                  string
	ProjectID              string
	EnvironmentID          string
	RotationID             string
	RunGeneration          int64
	Trigger                string
	Phase                  string
	CandidateHandle        string
	SupersededHandle       string
	CandidateValidUntil    time.Time
	PublishedRevision      int64
	RevocationDelaySeconds int64
	StartedAt              time.Time
	PhaseAt                time.Time
	NextAttemptAt          time.Time
	AttemptCount           int64
	LastError              string
	ProviderID             string
	KeyID                  string
	UsernameKeyID          string
	IntervalSeconds        int64
	AuthorityPrincipalID   string
	RotationState          string
	RotationGeneration     int64
	// LeaseOwner and ClaimToken are the crash fence this claim stamped. Every
	// settling write asserts both, so a stale term cannot overwrite a newer one.
	LeaseOwner string
	ClaimToken int64
}

// Chain is the run's immutable tenant chain.
func (r RotationRun) Chain() domain.Scope {
	return domain.Scope{Org: domain.OrgID(r.OrgID), Project: domain.ProjectID(r.ProjectID), Env: domain.EnvID(r.EnvironmentID)}
}

// RotationAudit is one tenant audit row the runtime writes in the same
// transaction as the effect or phase it records. Payload is the JSON object the
// service built; it carries no credential material.
type RotationAudit struct {
	ID      string
	Type    string
	Outcome string
	Payload []byte
}

// RotationEffectIntent opens an effect with its INTENT audit.
type RotationEffectIntent struct {
	EffectID string
	Kind     string
	Audit    RotationAudit
}

// RotationEffectOutcome closes an open effect with its OUTCOME audit.
type RotationEffectOutcome struct {
	EffectID string
	Outcome  string
	Audit    RotationAudit
}

// RotationSettle is the one settling write: optionally close an effect, open the
// next effect, write extra audit rows, move the run, and move the rotation's
// schedule or halt it, all in one transaction under the run's crash fence.
type RotationSettle struct {
	Now time.Time
	// FromPhases are the phases the write may leave. It is both the exact-row
	// fence and the fenced claim write of crash-safety rule 8.
	FromPhases []string
	Phase      string
	LastError  string
	// NextAttempt is the run's next due time.
	NextAttempt time.Time
	// ValidUntil, when non-zero, moves the run's delivered-role expiry (extend).
	ValidUntil time.Time
	// PublishedRevision, when non-zero, records the revision this run published.
	PublishedRevision int64
	// KeepLease leaves the lease held, for a settle that is followed by a
	// provider call in the same claim.
	KeepLease bool
	Close     *RotationEffectOutcome
	Open      *RotationEffectIntent
	Audits    []RotationAudit
	// NextRunAt, when non-zero, moves the rotation's schedule (no generation bump).
	NextRunAt time.Time
	// HaltReason, when set, halts the rotation (state halted, generation bump).
	HaltReason string
}

// StartRun is one run creation from a due rotation.
type StartRun struct {
	RunID               string
	Worker              string
	Now                 time.Time
	LeaseUntil          time.Time
	Trigger             string
	CandidateHandle     string
	CandidateValidUntil time.Time
	// ParkedUntil is where the rotation's next_run_at is parked while the run is
	// in flight (the pre-publish deadline); the publish transaction replaces it.
	ParkedUntil time.Time
	Intent      RotationEffectIntent
}

// RotationRestore is the restore assertion's observation: for each of the two
// keys, whether the entry currently served is one this run, or a later run of the
// key, published. No secret is read.
type RotationRestore struct {
	Holds         bool
	MismatchedKey string
}

type rotationRuntimeQueries interface {
	dueRotation(ctx context.Context, now time.Time) (DueRotation, error)
	lockDueRotation(ctx context.Context, d DueRotation, now time.Time) (DueRotation, error)
	latestPublished(ctx context.Context, chain domain.Scope, keyID string) (handle string, err error)
	insertRun(ctx context.Context, d DueRotation, s StartRun, superseded string) error
	setNextRun(ctx context.Context, chain domain.Scope, rotationID string, next, now time.Time) (int64, error)
	haltRotation(ctx context.Context, chain domain.Scope, rotationID, reason string, now time.Time) (int64, error)
	insertEffect(ctx context.Context, chain domain.Scope, runID string, e RotationEffectIntent, at time.Time) error
	closeEffect(ctx context.Context, chain domain.Scope, runID string, o RotationEffectOutcome, at time.Time) error
	insertAudit(ctx context.Context, chain domain.Scope, authority, rotationID, runID string, a RotationAudit, at time.Time) error
	claimDueRun(ctx context.Context, now time.Time) (RotationRun, error)
	claimRun(ctx context.Context, run RotationRun, until time.Time) error
	assertRun(ctx context.Context, run RotationRun, now time.Time) (int64, error)
	settleRun(ctx context.Context, run RotationRun, s RotationSettle) (int64, error)
	getRun(ctx context.Context, chain domain.Scope, runID string) (RotationRun, error)
	effectCount(ctx context.Context, chain domain.Scope, runID, kind, outcome string) (int64, error)
	failedAttempts(ctx context.Context, chain domain.Scope, rotationID string, generation int64, since time.Time) (int64, error)
	publishedAt(ctx context.Context, chain domain.Scope, revision int64) (time.Time, error)
	currentEntry(ctx context.Context, chain domain.Scope, keyID string) (string, error)
	publishedEntries(ctx context.Context, chain domain.Scope, keyID string, fromRevision int64) ([]string, error)
	laterPublished(ctx context.Context, chain domain.Scope, keyID string, revision int64) (int64, error)
	providerMaterial(ctx context.Context, run RotationRun, now time.Time) (LeaseProviderMaterial, error)
	openRunCount(ctx context.Context, chain domain.Scope, rotationID string) (int64, error)
	openEffect(ctx context.Context, chain domain.Scope, runID string) (id, kind string, err error)
}

func (r *RotationRuntime) transaction(ctx context.Context, fn func(rotationRuntimeQueries) error) error {
	return dbTransaction(ctx, r.db, func(tx adapterDBTX) error { return fn(tx.rotationRuntimeQueries()) })
}

func rotationRead[T any](ctx context.Context, r *RotationRuntime, fn func(rotationRuntimeQueries) (T, error)) (T, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) (T, error) { return fn(db.rotationRuntimeQueries()) })
}

func mapNoRows(err error) error {
	if isNoRows(err) {
		return ErrNotFound
	}
	return err
}

// DueRotation returns the oldest active rotation whose next run is due and that
// holds no open run, under the per-org lease cap. It is a read: StartRun
// re-asserts everything under the row lock.
func (r *RotationRuntime) DueRotation(ctx context.Context, now time.Time) (DueRotation, bool, error) {
	d, err := rotationRead(ctx, r, func(q rotationRuntimeQueries) (DueRotation, error) { return q.dueRotation(ctx, now) })
	if errors.Is(mapNoRows(err), ErrNotFound) {
		return DueRotation{}, false, nil
	}
	return d, err == nil, err
}

// StartRun creates the run in `minting` for a due rotation in ONE transaction:
// it locks the rotation row (SKIP LOCKED, so a second worker moves on), asserts
// the rotation is still active at the generation the scheduler saw and holds no
// open run, derives the superseded handle across rotations, inserts the run
// already leased to the worker, parks the rotation's next_run_at at the
// pre-publish deadline and writes the INTENT effect. The candidate handle is
// therefore durable before the provider is touched. started is false when the
// rotation was taken, paused, or already running.
func (r *RotationRuntime) StartRun(ctx context.Context, d DueRotation, s StartRun) (RotationRun, bool, error) {
	var out RotationRun
	started := false
	err := r.transaction(ctx, func(q rotationRuntimeQueries) error {
		started = false
		locked, err := q.lockDueRotation(ctx, d, s.Now)
		if isNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		chain := domain.Scope{Org: domain.OrgID(d.OrgID), Project: domain.ProjectID(d.ProjectID), Env: domain.EnvID(d.EnvironmentID)}
		open, err := q.openRunCount(ctx, chain, locked.ID)
		if err != nil {
			return err
		}
		if open != 0 {
			return nil
		}
		superseded, err := q.latestPublished(ctx, chain, locked.KeyID)
		if err != nil {
			return err
		}
		if err := q.insertRun(ctx, d, s, superseded); err != nil {
			return err
		}
		if rows, err := q.setNextRun(ctx, chain, d.ID, s.ParkedUntil, s.Now); err != nil || rows != 1 {
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: rotation schedule write matched no row", ErrConflict)
		}
		if err := q.insertAudit(ctx, chain, d.AuthorityPrincipalID, d.ID, s.RunID, s.Intent.Audit, s.Now); err != nil {
			return err
		}
		if err := q.insertEffect(ctx, chain, s.RunID, s.Intent, s.Now); err != nil {
			return err
		}
		run, err := q.getRun(ctx, chain, s.RunID)
		if err != nil {
			return err
		}
		run.LeaseOwner = s.Worker
		out = run
		started = true
		return nil
	})
	return out, started && err == nil, err
}

// ClaimDueRun leases one due run for a term. Selections (see the claim query):
// an in-flight run, the candidate reconciler's pre-publish failed or quarantined
// run, the revoke-obligation loop's post-publish run, and a settled run due an
// extension. The service decides which action the run's phase calls for.
func (r *RotationRuntime) ClaimDueRun(ctx context.Context, worker string, now, until time.Time) (RotationRun, bool, error) {
	var out RotationRun
	err := r.transaction(ctx, func(q rotationRuntimeQueries) error {
		run, err := q.claimDueRun(ctx, now)
		if err != nil {
			return mapNoRows(err)
		}
		run.AttemptCount++
		run.ClaimToken++
		run.LeaseOwner = worker
		if err := q.claimRun(ctx, run, until); err != nil {
			return err
		}
		out = run
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return RotationRun{}, false, nil
	}
	return out, err == nil, err
}

// GetRun is a fresh read of a run and its rotation, used after a fence loss to
// follow the state the run is now in.
func (r *RotationRuntime) GetRun(ctx context.Context, chain domain.Scope, runID string) (RotationRun, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (RotationRun, error) {
		run, err := q.getRun(ctx, chain, runID)
		return run, mapNoRows(err)
	})
}

// LoadProviderMaterial reads the run's provider (origin, grant role, sealed admin
// credential) under the lease fence.
func (r *RotationRuntime) LoadProviderMaterial(ctx context.Context, run RotationRun, now time.Time) (LeaseProviderMaterial, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (LeaseProviderMaterial, error) {
		out, err := q.providerMaterial(ctx, run, now)
		if err != nil {
			return LeaseProviderMaterial{}, mapNoRows(err)
		}
		if len(out.CredentialCiphertext) == 0 {
			return LeaseProviderMaterial{}, ErrNoProviderCredential
		}
		return out, nil
	})
}

func (r *RotationRuntime) assert(ctx context.Context, q rotationRuntimeQueries, run RotationRun, now time.Time) error {
	n, err := q.assertRun(ctx, run, now)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRotationFenceLost
	}
	return nil
}

// RecordIntent writes an INTENT effect and its audit row before a provider call,
// re-asserting the run's crash fence.
func (r *RotationRuntime) RecordIntent(ctx context.Context, run RotationRun, now time.Time, e RotationEffectIntent) error {
	return r.transaction(ctx, func(q rotationRuntimeQueries) error {
		if err := r.assert(ctx, q, run, now); err != nil {
			return err
		}
		chain := run.Chain()
		if err := q.insertAudit(ctx, chain, run.AuthorityPrincipalID, run.RotationID, run.ID, e.Audit, now); err != nil {
			return err
		}
		return q.insertEffect(ctx, chain, run.ID, e, now)
	})
}

// Settle is the one settling write; see RotationSettle. A write whose run no
// longer matched returns ErrRotationFenceLost with the whole transaction (effect
// and audit rows included) rolled back.
func (r *RotationRuntime) Settle(ctx context.Context, run RotationRun, s RotationSettle) error {
	return r.transaction(ctx, func(q rotationRuntimeQueries) error {
		chain := run.Chain()
		// The settling write first: it is the exact-row fence, and it waits on a
		// row lock a publish transaction holds (rule 8).
		rows, err := q.settleRun(ctx, run, s)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrRotationFenceLost
		}
		if s.Close != nil {
			if err := q.insertAudit(ctx, chain, run.AuthorityPrincipalID, run.RotationID, run.ID, s.Close.Audit, s.Now); err != nil {
				return err
			}
			if err := q.closeEffect(ctx, chain, run.ID, *s.Close, s.Now); err != nil {
				return err
			}
		}
		for _, a := range s.Audits {
			if err := q.insertAudit(ctx, chain, run.AuthorityPrincipalID, run.RotationID, run.ID, a, s.Now); err != nil {
				return err
			}
		}
		if s.Open != nil {
			if err := q.insertAudit(ctx, chain, run.AuthorityPrincipalID, run.RotationID, run.ID, s.Open.Audit, s.Now); err != nil {
				return err
			}
			if err := q.insertEffect(ctx, chain, run.ID, *s.Open, s.Now); err != nil {
				return err
			}
		}
		if !s.NextRunAt.IsZero() {
			if _, err := q.setNextRun(ctx, chain, run.RotationID, s.NextRunAt, s.Now); err != nil {
				return err
			}
		}
		if s.HaltReason != "" {
			if _, err := q.haltRotation(ctx, chain, run.RotationID, s.HaltReason, s.Now); err != nil {
				return err
			}
		}
		return nil
	})
}

// HaltRotation halts a rotation whose pre-publish gate failed before any run
// existed (the scheduler's own check), writing the audit row beside it.
func (r *RotationRuntime) HaltRotation(ctx context.Context, d DueRotation, reason string, now time.Time) error {
	return r.transaction(ctx, func(q rotationRuntimeQueries) error {
		chain := domain.Scope{Org: domain.OrgID(d.OrgID), Project: domain.ProjectID(d.ProjectID), Env: domain.EnvID(d.EnvironmentID)}
		_, err := q.haltRotation(ctx, chain, d.ID, reason, now)
		return err
	})
}

// OpenEffect returns the effect a crashed worker left without an OUTCOME.
func (r *RotationRuntime) OpenEffect(ctx context.Context, run RotationRun) (id, kind string, err error) {
	err = dbRead(ctx, r.db, func(db adapterDB) error {
		var qerr error
		id, kind, qerr = db.rotationRuntimeQueries().openEffect(ctx, run.Chain(), run.ID)
		return mapNoRows(qerr)
	})
	return id, kind, err
}

// EffectSucceeded reports whether the run recorded a successful effect of kind.
func (r *RotationRuntime) EffectSucceeded(ctx context.Context, run RotationRun, kind string) (bool, error) {
	n, err := rotationRead(ctx, r, func(q rotationRuntimeQueries) (int64, error) {
		return q.effectCount(ctx, run.Chain(), run.ID, kind, "success")
	})
	return n > 0, err
}

// FailedAttempts counts the rotation's runs of one generation since `since` that never published
// and ended failed or quarantined: the attempts one schedule cycle has spent.
func (r *RotationRuntime) FailedAttempts(ctx context.Context, chain domain.Scope, rotationID string, generation int64, since time.Time) (int64, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (int64, error) {
		return q.failedAttempts(ctx, chain, rotationID, generation, since)
	})
}

// PublishedAt is the commit time of the snapshot a run published: the single
// anchor P of the failure schedule.
func (r *RotationRuntime) PublishedAt(ctx context.Context, run RotationRun) (time.Time, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (time.Time, error) {
		t, err := q.publishedAt(ctx, run.Chain(), run.PublishedRevision)
		return t, mapNoRows(err)
	})
}

// LaterPublished reports whether a later run published for the run's key, so the
// run's delivered credential is superseded.
func (r *RotationRuntime) LaterPublished(ctx context.Context, run RotationRun) (bool, error) {
	n, err := rotationRead(ctx, r, func(q rotationRuntimeQueries) (int64, error) {
		return q.laterPublished(ctx, run.Chain(), run.KeyID, run.PublishedRevision)
	})
	return n > 0, err
}

// RestoreAssertion is crash-safety rule 7. For both keys the value entry
// currently served must be one a run of the key published at or after this run's
// revision, compared by entry id with no secret read.
func (r *RotationRuntime) RestoreAssertion(ctx context.Context, run RotationRun) (RotationRestore, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (RotationRestore, error) {
		chain := run.Chain()
		for _, keyID := range []string{run.KeyID, run.UsernameKeyID} {
			current, err := q.currentEntry(ctx, chain, keyID)
			if isNoRows(err) {
				return RotationRestore{MismatchedKey: keyID}, nil
			}
			if err != nil {
				return RotationRestore{}, err
			}
			published, err := q.publishedEntries(ctx, chain, keyID, run.PublishedRevision)
			if err != nil {
				return RotationRestore{}, err
			}
			ok := false
			for _, id := range published {
				if id == current {
					ok = true
					break
				}
			}
			if !ok {
				return RotationRestore{MismatchedKey: keyID}, nil
			}
		}
		return RotationRestore{Holds: true}, nil
	})
}
