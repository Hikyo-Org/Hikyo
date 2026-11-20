package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	// ManualRunID is the run a pending run-now request asked for, empty for a
	// scheduled start; ManualSkipOverlap asks for a run whose revocation delay is
	// zero (#148, S2b-1).
	ManualRunID       string
	ManualSkipOverlap bool
}

// RotationRun is one claimed or re-read run joined to its rotation's immutable
// configuration. Empty strings and zero times are the absent values.
type RotationRun struct {
	ID                  string
	OrgID               string
	ProjectID           string
	EnvironmentID       string
	RotationID          string
	RunGeneration       int64
	Trigger             string
	Phase               string
	CandidateHandle     string
	SupersededHandle    string
	CandidateValidUntil time.Time
	// RevokeNotBefore is the earliest the superseded credential may be revoked,
	// kept apart from NextAttemptAt so a wake earlier than it (an extension of
	// the delivered credential) never revokes early. Zero: the wake is the deadline.
	RevokeNotBefore        time.Time
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
	// RotationDelaySeconds is the rotation's own revocation delay; the run's
	// RevocationDelaySeconds is its snapshot (zero for a run-now that skipped the
	// overlap).
	RotationDelaySeconds int64
	AuthorityPrincipalID string
	RotationState        string
	RotationGeneration   int64
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
	// RevokeNotBefore, when non-zero, records the earliest revoke time (see RotationRun).
	RevokeNotBefore time.Time
	// PublishedRevision, when non-zero, records the revision this run published.
	PublishedRevision int64
	// NotConverged, when set, records the adapter targets still on the superseded
	// revision at the revoke decision (#148, S2b-1).
	NotConverged *int64
	// DueIfHeld leaves the run due now, instead of at NextAttempt, when a restore holds
	// the rotation inside the settle's own transaction. It is for a settle that parks a
	// run at "never": the decision is made with the halt write, so a hold landing after
	// the caller last read the rotation cannot strand the run.
	DueIfHeld bool
	// RestoreChecked says this settle follows a fresh read of the restore assertion
	// (the revoke step). Without it a run held as published_versions_changed keeps
	// that code through any settle that leaves it refused or quarantined.
	RestoreChecked bool
	// KeepLease leaves the lease held, for a settle that is followed by a
	// provider call in the same claim.
	KeepLease bool
	Close     *RotationEffectOutcome
	Open      *RotationEffectIntent
	Audits    []RotationAudit
	// NextRunAt, when non-zero, moves the rotation's schedule (no generation bump).
	NextRunAt time.Time
	// HaltReason, when set, halts the rotation (state halted, generation bump).
	// HaltAudit is written only when the halt changed a row: a rotation paused or
	// deleted while the run was in flight is no longer active, so nothing halted
	// and no halt is recorded.
	HaltReason string
	HaltAudit  *RotationAudit
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

// SupersededRole is the role a published run replaces, bound to the provider
// that minted it (see RotationRuntime.LoadSupersededRole).
type SupersededRole struct {
	// Material is that provider's connection material; its ciphertext is empty
	// when the provider has since lost its credential. The provider may have been
	// deleted since it minted the role: the delete guard only counts runs that
	// already owe the role, so a provider whose last run settled can be deleted
	// before a later rotation supersedes its role. The revoke still works because
	// a provider delete is a tombstone (state='tombstoned') that keeps the sealed
	// credential, and the load has no state filter. Adding one would break it.
	Material LeaseProviderMaterial
	// ValidUntil is the minting run's recorded VALID UNTIL; zero when unrecorded.
	ValidUntil time.Time
	// ExtendUncertain is whether an extend of that role ended unknown or never
	// recorded an outcome, so the recorded ValidUntil is only a lower bound.
	ExtendUncertain bool
}

type rotationRuntimeQueries interface {
	gauges(ctx context.Context, now time.Time) (RotationGauges, error)
	dueRotation(ctx context.Context, now time.Time) (DueRotation, error)
	lockDueRotation(ctx context.Context, d DueRotation, now time.Time) (DueRotation, error)
	latestPublished(ctx context.Context, chain domain.Scope, keyID string) (handle string, validUntil time.Time, err error)
	insertRun(ctx context.Context, d DueRotation, s StartRun, superseded string) error
	setNextRun(ctx context.Context, chain domain.Scope, rotationID string, next, now time.Time) (int64, error)
	// deferNextRun is setNextRun for a deferral: it keeps the first due time in deferred_from.
	deferNextRun(ctx context.Context, chain domain.Scope, rotationID string, next, now time.Time) (int64, error)
	haltRotation(ctx context.Context, chain domain.Scope, rotationID, reason string, now time.Time) (int64, error)
	insertEffect(ctx context.Context, chain domain.Scope, runID string, e RotationEffectIntent, at time.Time) error
	closeEffect(ctx context.Context, chain domain.Scope, runID string, o RotationEffectOutcome, at time.Time) error
	insertAudit(ctx context.Context, chain domain.Scope, authority, rotationID, runID string, a RotationAudit, at time.Time) error
	claimDueRun(ctx context.Context, now, leaseCut time.Time) (RotationRun, error)
	claimRun(ctx context.Context, run RotationRun, until time.Time) error
	assertRun(ctx context.Context, run RotationRun, now time.Time) (int64, error)
	settleRun(ctx context.Context, run RotationRun, s RotationSettle) (int64, error)
	getRun(ctx context.Context, chain domain.Scope, runID string) (RotationRun, error)
	failedAttempts(ctx context.Context, chain domain.Scope, rotationID string, generation int64, since time.Time) (int64, error)
	infraFaults(ctx context.Context, chain domain.Scope, rotationID string, generation int64, since time.Time) (int64, error)
	publishedAt(ctx context.Context, chain domain.Scope, revision int64) (time.Time, error)
	currentEntry(ctx context.Context, chain domain.Scope, keyID string) (string, error)
	publishedEntries(ctx context.Context, chain domain.Scope, keyID string, fromRevision int64) ([]string, error)
	laterPublished(ctx context.Context, chain domain.Scope, keyID string, revision int64) (int64, error)
	restoreHeld(ctx context.Context, chain domain.Scope, rotationID string) (int64, error)
	dueRun(ctx context.Context, run RotationRun, now time.Time) error
	providerMaterial(ctx context.Context, run RotationRun, now time.Time) (LeaseProviderMaterial, error)
	supersededRole(ctx context.Context, run RotationRun, now time.Time) (SupersededRole, error)
	openRunCount(ctx context.Context, chain domain.Scope, rotationID string) (int64, error)
	orgLeaseCount(ctx context.Context, org domain.OrgID, now time.Time) (int64, error)
	clearManual(ctx context.Context, chain domain.Scope, rotationID string) error
	notConverged(ctx context.Context, run RotationRun) (int64, error)
	openEffect(ctx context.Context, chain domain.Scope, runID string) (id, kind string, err error)
	ownPublished(ctx context.Context, chain domain.Scope, rotationID string, generation int64) (int64, error)
	cycleStart(ctx context.Context, chain domain.Scope, rotationID string, generation int64) (time.Time, error)
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

// NotConverged counts the adapter targets that mirror the run's keys and have
// not converged to the run's published revision: the adapter targets that may
// still hold the superseded credential (#148, S2b-1). Machine principals reading the
// keys directly are not counted: no per-principal delivery is recorded.
func (r *RotationRuntime) NotConverged(ctx context.Context, run RotationRun) (int64, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (int64, error) { return q.notConverged(ctx, run) })
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

// RotationOrgLeaseCap is the most runs of one org that may hold a live lease at
// once. The due-rotation and claim queries carry the same number as a literal.
const RotationOrgLeaseCap = 4

// StartRun creates the run in `minting` for a due rotation in ONE transaction:
// it locks the rotation row (SKIP LOCKED, so a second worker moves on), asserts
// the rotation is still active at the generation the scheduler saw and holds no
// open run, derives the superseded handle across rotations, inserts the run
// already leased to the worker, parks the rotation's next_run_at at the
// pre-publish deadline and writes the INTENT effect. The candidate handle is
// therefore durable before the provider is touched. started is false when the
// rotation was taken, paused, already running, or a run-now request arrived
// since the selection (the next sweep re-selects it as a manual run).
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
		// A run-now request that arrived after the selection read changes what the
		// start must be: re-select rather than start a scheduled run over it.
		if locked.ManualRunID != d.ManualRunID || locked.ManualSkipOverlap != d.ManualSkipOverlap {
			return nil
		}
		chain := domain.Scope{Org: domain.OrgID(d.OrgID), Project: domain.ProjectID(d.ProjectID), Env: domain.EnvID(d.EnvironmentID)}
		open, err := q.openRunCount(ctx, chain, locked.ID)
		if err != nil {
			return err
		}
		if open != 0 {
			return nil
		}
		// The org's lease cap, re-read here: the selection read it in another
		// transaction, so concurrent starts could each have seen room. SQLite runs
		// one writer at a time and PostgreSQL runs this transaction SERIALIZABLE, so
		// two starts that both count below the cap cannot both commit (the loser fails
		// with a serialization error and the rotation is deferred and retried).
		leased, err := q.orgLeaseCount(ctx, chain.Org, s.Now)
		if err != nil {
			return err
		}
		if leased >= RotationOrgLeaseCap {
			return nil
		}
		superseded, _, err := q.latestPublished(ctx, chain, locked.KeyID)
		if err != nil {
			return err
		}
		if err := q.insertRun(ctx, d, s, superseded); err != nil {
			return err
		}
		if d.ManualRunID != "" {
			if err := q.clearManual(ctx, chain, d.ID); err != nil {
				return err
			}
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

// ClaimDueRun leases one due run for a term. leaseCut is when an expired lease
// counts as free: the caller passes now minus one provider deadline, so a takeover
// never overlaps a provider call the previous holder started inside its lease.
// Selections (see the claim query):
// an in-flight run, the candidate reconciler's pre-publish failed or quarantined
// run, the revoke-obligation loop's post-publish run, and a settled run due an
// extension. The service decides which action the run's phase calls for.
func (r *RotationRuntime) ClaimDueRun(ctx context.Context, worker string, now, until, leaseCut time.Time) (RotationRun, bool, error) {
	var out RotationRun
	err := r.transaction(ctx, func(q rotationRuntimeQueries) error {
		run, err := q.claimDueRun(ctx, now, leaseCut)
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

// LoadSupersededRole reads the role a published run replaces together with the
// provider that MINTED it, under the run's lease fence. That provider is the one
// of the run that created the role, not the claimed run's own: a rotation
// recreated on another provider still has the old role to revoke, and the old
// role lives on the old server. A provider that has lost its credential reads as
// a role with empty Material.CredentialCiphertext, never as an error, so the
// caller can still take the expiry exit.
func (r *RotationRuntime) LoadSupersededRole(ctx context.Context, run RotationRun, now time.Time) (SupersededRole, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (SupersededRole, error) {
		out, err := q.supersededRole(ctx, run, now)
		return out, mapNoRows(err)
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
			// Zero rows (the rotation was paused or deleted while the run was in
			// flight) is correct: the schedule is then not this run's to move.
			if _, err := q.setNextRun(ctx, chain, run.RotationID, s.NextRunAt, s.Now); err != nil {
				return err
			}
		}
		if s.HaltReason != "" {
			halted, err := q.haltRotation(ctx, chain, run.RotationID, s.HaltReason, s.Now)
			if err != nil {
				return err
			}
			if halted == 1 && s.HaltAudit != nil {
				if err := q.insertAudit(ctx, chain, run.AuthorityPrincipalID, run.RotationID, run.ID, *s.HaltAudit, s.Now); err != nil {
					return err
				}
			}
		}
		if s.DueIfHeld {
			// Read last, in the transaction that wrote the settle and tried the halt: under
			// PG serializable a hold committed after this transaction began conflicts with
			// the halt write and fails the transaction (dbTransaction does not retry it: the
			// error goes to the sweep, and the run is reclaimed after the release and the
			// lease term, when staleEffect closes the open extend INTENT as unknown), and
			// one committed before it is seen here, so the held decision and the halt
			// always agree.
			held, err := q.restoreHeld(ctx, chain, run.RotationID)
			if err != nil || held == 0 {
				return err
			}
			return q.dueRun(ctx, run, s.Now)
		}
		return nil
	})
}

// HaltRotation halts a rotation whose pre-publish gate failed before any run
// existed (the scheduler's own check), writing the audit row beside it, and only
// when the halt changed a row: a rotation that is no longer active has nothing to
// halt and records nothing.
func (r *RotationRuntime) HaltRotation(ctx context.Context, d DueRotation, reason string, now time.Time, audit RotationAudit) error {
	return r.transaction(ctx, func(q rotationRuntimeQueries) error {
		chain := d.Chain()
		rows, err := q.haltRotation(ctx, chain, d.ID, reason, now)
		if err != nil || rows != 1 {
			return err
		}
		return q.insertAudit(ctx, chain, d.AuthorityPrincipalID, d.ID, "", audit, now)
	})
}

// DeferRotation moves an active rotation's next run out, so a rotation whose
// pre-start checks keep failing for a reason that is not its own refusal (a
// datastore fault) cannot stay the oldest due row and starve every other tenant's
// rotations behind it. It writes nothing else; zero rows (the rotation is no
// longer active) is correct.
func (r *RotationRuntime) DeferRotation(ctx context.Context, d DueRotation, next, now time.Time) error {
	return r.transaction(ctx, func(q rotationRuntimeQueries) error {
		_, err := q.deferNextRun(ctx, d.Chain(), d.ID, next, now)
		return err
	})
}

// Chain is the due rotation's tenant chain.
func (d DueRotation) Chain() domain.Scope {
	return domain.Scope{Org: domain.OrgID(d.OrgID), Project: domain.ProjectID(d.ProjectID), Env: domain.EnvID(d.EnvironmentID)}
}

// RotationCycle is what the failure schedule needs to know about the rotation's
// current cycle (the runs since its last pause, resume or halt).
type RotationCycle struct {
	// OwnPublishedRevision is the newest revision this rotation published at its
	// current generation; zero when it has not published since the cycle began.
	OwnPublishedRevision int64
	// FirstRunAt is the start of the cycle's first run; zero before any run.
	FirstRunAt time.Time
	// KeyEverPublished is whether any run of any rotation ever published the key,
	// that is, whether a superseded credential exists.
	KeyEverPublished bool
	// DeliveredValidUntil is the recorded VALID UNTIL of that delivered
	// credential, the anchor a cycle that began over it (a resumed or recreated
	// rotation) derives its window from; zero when none.
	DeliveredValidUntil time.Time
}

// Cycle reads the schedule anchors for a due rotation.
func (r *RotationRuntime) Cycle(ctx context.Context, d DueRotation) (RotationCycle, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (RotationCycle, error) {
		var out RotationCycle
		chain := d.Chain()
		rev, err := q.ownPublished(ctx, chain, d.ID, d.Generation)
		if err != nil && !isNoRows(err) {
			return out, err
		}
		out.OwnPublishedRevision = rev
		first, err := q.cycleStart(ctx, chain, d.ID, d.Generation)
		if err != nil && !isNoRows(err) {
			return out, err
		}
		out.FirstRunAt = first
		handle, validUntil, err := q.latestPublished(ctx, chain, d.KeyID)
		if err != nil {
			return out, err
		}
		out.KeyEverPublished = handle != ""
		out.DeliveredValidUntil = validUntil
		return out, nil
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

// FailedAttempts counts the rotation's runs of one generation since `since` that never published
// and ended failed or quarantined: the attempts one schedule cycle has spent.
func (r *RotationRuntime) FailedAttempts(ctx context.Context, chain domain.Scope, rotationID string, generation int64, since time.Time) (int64, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (int64, error) {
		return q.failedAttempts(ctx, chain, rotationID, generation, since)
	})
}

// InfraFaults counts the rotation's runs of one generation since `since` that never
// published and failed only on the executor's own infrastructure: not attempts, but
// the infrastructure faults of the cycle, which size the infrastructure backoff.
func (r *RotationRuntime) InfraFaults(ctx context.Context, chain domain.Scope, rotationID string, generation int64, since time.Time) (int64, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (int64, error) {
		return q.infraFaults(ctx, chain, rotationID, generation, since)
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

// RestoreHeld reports whether a restore holds the run's rotation right now. The
// run was loaded at its claim, so its own rotation state cannot show a hold that
// landed afterwards.
func (r *RotationRuntime) RestoreHeld(ctx context.Context, run RotationRun) (bool, error) {
	n, err := rotationRead(ctx, r, func(q rotationRuntimeQueries) (int64, error) {
		return q.restoreHeld(ctx, run.Chain(), run.RotationID)
	})
	return n > 0, err
}

// RestoreAssertion is crash-safety rule 7. For both keys the value entry
// currently served must be one a run of the key published at or after this run's
// revision, compared by entry id with no secret read. A key with no served entry
// at all (deleted, or its value unset after the publish) passes: nothing serves
// the superseded role through it, so the revoke cannot cause an outage. The
// observation and the drop that follows are not one transaction; a restore that
// lands between them is the documented window in the crash table.
func (r *RotationRuntime) RestoreAssertion(ctx context.Context, run RotationRun) (bool, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (bool, error) {
		chain := run.Chain()
		for _, keyID := range []string{run.KeyID, run.UsernameKeyID} {
			current, err := q.currentEntry(ctx, chain, keyID)
			if isNoRows(err) {
				continue
			}
			if err != nil {
				return false, err
			}
			published, err := q.publishedEntries(ctx, chain, keyID, run.PublishedRevision)
			if err != nil {
				return false, err
			}
			if !slices.Contains(published, current) {
				return false, nil
			}
		}
		return true, nil
	})
}

// RotationOverdueGrace is how long an active rotation may sit due with no run
// started before it counts as overdue. A legitimate retry backoff schedules its
// next attempt in the future, so only a stalled or starved executor trips it.
const RotationOverdueGrace = 15 * time.Minute

// RotationExpiringWithin is the horizon of the approaching-expiry count: a
// rotation's delivered credential that expires inside it should have been extended
// already (the extend step starts at twice the retry window), so one still inside
// it needs an operator.
const RotationExpiringWithin = 6 * time.Hour

// RotationGauges are the label-free instance-wide counts /metrics and doctor read.
// They carry no tenant identity.
type RotationGauges struct {
	// Quarantined is runs closed as quarantine: an abandoned run whose candidate or
	// superseded role is still owed a drop.
	Quarantined int64
	// RevokeRefused is runs whose superseded role could not be dropped (it owns
	// objects, or the restore assertion refused it).
	RevokeRefused int64
	// UnknownOutcome is runs not yet done or failed that recorded an effect whose
	// outcome is unknown: a possibly live credential exists.
	UnknownOutcome int64
	// Halted is rotations stopped by a failed gate or an exhausted retry window.
	Halted int64
	// RestoreHeld is rotations a restore held and no operator has released yet.
	RestoreHeld int64
	// Overdue is active rotations due for longer than RotationOverdueGrace with no
	// run open.
	Overdue int64
	// Expiring is rotations (not deleted) whose delivered credential expires within
	// RotationExpiringWithin from now. A credential already past its expiry is not
	// counted: a lapsed credential halts or overdues its rotation instead.
	Expiring int64
}

// Gauges is a proof-free scrape-time system read, like DynamicRuntime.Gauges.
func (r *RotationRuntime) Gauges(ctx context.Context, now time.Time) (RotationGauges, error) {
	return rotationRead(ctx, r, func(q rotationRuntimeQueries) (RotationGauges, error) { return q.gauges(ctx, now) })
}
