package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DynamicRuntime is the dynamic-secret lease worker's system boundary, the
// second domain-specific outbox (#147; the first is AdapterRuntime). Tenant
// request paths never receive it. Its methods bind every statement to the
// claimed lease's immutable chain and re-assert the lease's own crash fence
// (lease_owner + lease_expires_at) on every write, so a worker that lost its
// lease affects zero rows.
type DynamicRuntime struct {
	db *DB
}

func NewDynamicRuntime(db *DB) *DynamicRuntime { return &DynamicRuntime{db: db} }

// ClaimedLease is one lease the worker has leased for a transition term.
type ClaimedLease struct {
	ID             string
	OrgID          string
	ProjectID      string
	EnvironmentID  string
	ProviderID     string
	ProviderHandle string
	PrincipalID    string
	PrincipalClass string
	State          string
	MaxTTLSeconds  int64
	IssuedAt       time.Time
	ExpiresAt      time.Time
	Attempt        int
	LeaseOwner     string
	// ClaimToken is a monotonic per-lease counter stamped by this claim. Every
	// settling write asserts it, so a stale term (even one that reacquired under
	// the same worker id) cannot overwrite a newer one: the newer claim bumped
	// the token, and the stale write's token no longer matches.
	ClaimToken int64
}

// LeaseProviderMaterial is the provider handle a transition needs: its origin,
// grant role, TLS mode, and the sealed admin credential (opened by the caller).
type LeaseProviderMaterial struct {
	Kind                 string
	Origin               string
	TLSMode              string
	GrantRole            string
	CredentialOwnerID    string
	CredentialCiphertext []byte
}

// transaction runs fn in a SERIALIZABLE (postgres) write transaction, unified
// with the adapter runtime via dbTransaction. Bug fix (#619): the previous
// dynamicTransaction opened postgres with the pool default (READ COMMITTED)
// while a comment claimed parity with the adapter runtime's SERIALIZABLE
// transaction; the two now share one helper and one isolation level.
func (r *DynamicRuntime) transaction(ctx context.Context, fn func(adapterDBTX) error) error {
	return dbTransaction(ctx, r.db, fn)
}

func dynamicClaimTime(value string) time.Time {
	t, _ := time.Parse(timeFormat, value)
	return t.UTC()
}

// ClaimDueLease leases one due lease for a transition term. Due = a transient
// state past its next_attempt_at, or an active lease past its expiry (a natural
// expiry the worker completes by dropping the now-dead role). A per-org cap and
// SKIP LOCKED keep concurrent workers off each other; the claim stamps
// lease_owner + lease_expires_at, which every settle re-asserts.
func (r *DynamicRuntime) ClaimDueLease(ctx context.Context, worker string, now, leaseUntil time.Time) (ClaimedLease, bool, error) {
	var out ClaimedLease
	err := r.transaction(ctx, func(tx adapterDBTX) error {
		var err error
		out, err = tx.dynamicQueries().dynamicClaimDueLease(ctx, now)
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out.Attempt++
		out.LeaseOwner = worker
		out.ClaimToken++
		return tx.dynamicQueries().dynamicClaimLease(ctx, out, leaseUntil)
	})
	if errors.Is(err, ErrNotFound) {
		return ClaimedLease{}, false, nil
	}
	return out, err == nil, err
}

// LoadProviderMaterial reads the lease's provider row (origin, grant role, TLS
// mode, sealed admin credential), re-asserting the lease crash fence.
func (r *DynamicRuntime) LoadProviderMaterial(ctx context.Context, lease ClaimedLease) (LeaseProviderMaterial, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) (LeaseProviderMaterial, error) {
		out, err := db.dynamicQueries().dynamicLoadProviderMaterial(ctx, lease, time.Now().UTC())
		if isNoRows(err) {
			return LeaseProviderMaterial{}, ErrNotFound
		}
		if err != nil {
			return LeaseProviderMaterial{}, err
		}
		if len(out.CredentialCiphertext) == 0 {
			return LeaseProviderMaterial{}, ErrNoProviderCredential
		}
		return out, nil
	})
}

// fenceRows turns a settling UPDATE's affected-row count into the fence
// decision: exactly one row means this worker still held the lease and the
// write landed; zero means the term was lost (expired, or taken over by another
// worker) between the claim and this write, so the whole transaction — audit
// and effect rows included — must roll back rather than overwrite a newer term.
func fenceRows(rows int64, err error) error {
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("%w: dynamic lease fence lost during settle", ErrConflict)
	}
	return nil
}

// ErrNoProviderCredential reports a lease whose provider has no admin
// credential (revoked or restored-invalidated): the worker cannot act at the
// provider, so it leaves the lease for a retry after the operator re-sets it.
var ErrNoProviderCredential = errors.New("store: dynamic provider has no admin credential")

// Gauges returns the two instance-wide dynamic-secret counts for /metrics: the
// number of currently usable leases and the number of effects stuck unknown.
// Proof-free like the rest of the runtime; it is a scrape-time system read.
func (r *DynamicRuntime) Gauges(ctx context.Context) (activeLeases, unknownEffects int64, err error) {
	err = dbRead(ctx, r.db, func(db adapterDB) error {
		var err error
		activeLeases, unknownEffects, err = db.dynamicQueries().dynamicGauges(ctx)
		return err
	})
	if err != nil {
		return 0, 0, err
	}
	return activeLeases, unknownEffects, nil
}

// LatestEffectKind returns the kind of the lease's most recent effect, so
// reconcile of an `unknown` lease can resume the exact transition that went
// ambiguous rather than guessing.
func (r *DynamicRuntime) LatestEffectKind(ctx context.Context, lease ClaimedLease) (string, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) (string, error) {
		kind, err := db.dynamicQueries().dynamicLatestEffectKind(ctx, lease)
		if isNoRows(err) {
			return "", ErrNotFound
		}
		return kind, err
	})
}

// leaseTransitionPayload is the audit payload for dynamic lease transition
// intent/outcome events.
type leaseTransitionPayload struct {
	Kind           string `json:"kind"`
	ProviderHandle string `json:"provider_handle"`
}

// RecordIntent writes the durable INTENT (effect row + audit event) before the
// provider call, re-asserting the lease crash fence. It returns the effect id
// the matching OUTCOME closes.
func (r *DynamicRuntime) RecordIntent(ctx context.Context, lease ClaimedLease, kind string) (string, error) {
	effectID := "def_" + uuid.Must(uuid.NewV7()).String()
	intentAudit := "dau_" + uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC()
	err := r.transaction(ctx, func(tx adapterDBTX) error {
		if err := r.assertLeased(ctx, tx, lease, now); err != nil {
			return err
		}
		payload, err := json.Marshal(leaseTransitionPayload{Kind: kind, ProviderHandle: lease.ProviderHandle})
		if err != nil {
			return err
		}
		if err := r.insertLeaseAudit(ctx, tx, lease, intentAudit, "dynamic.lease_transition_intent", "intent", now, payload); err != nil {
			return err
		}
		return tx.dynamicQueries().dynamicInsertEffect(ctx, lease, effectID, kind, intentAudit, now)
	})
	if err != nil {
		return "", err
	}
	return effectID, nil
}

// settle closes a lease's OUTCOME effect (effect row + audit event) and moves
// the lease to newState under its crash fence, all in one transaction — the
// shared body of RecordOutcome/RecordOutcomeRetry/EnterUnknown. issuedAt and
// expiresAt stamp the lease only when non-zero (COALESCE keeps the existing
// value otherwise); nextAttempt is the re-derived far-future stamp for a
// terminal row or the retry deadline for a transient one.
func (r *DynamicRuntime) settle(ctx context.Context, lease ClaimedLease, effectID, kind, outcome, newState string, issuedAt, expiresAt, nextAttempt time.Time) error {
	outcomeAudit := "dau_" + uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC()
	return r.transaction(ctx, func(tx adapterDBTX) error {
		if err := r.assertLeased(ctx, tx, lease, now); err != nil {
			return err
		}
		payload, err := json.Marshal(leaseTransitionPayload{Kind: kind, ProviderHandle: lease.ProviderHandle})
		if err != nil {
			return err
		}
		if err := r.insertLeaseAudit(ctx, tx, lease, outcomeAudit, "dynamic.lease_transition_outcome", outcome, now, payload); err != nil {
			return err
		}
		if err := tx.dynamicQueries().dynamicCloseEffect(ctx, lease, effectID, outcome, outcomeAudit, now); err != nil {
			return err
		}
		rows, err := tx.dynamicQueries().dynamicSettleLease(ctx, lease, newState, issuedAt, expiresAt, now, nextAttempt)
		return fenceRows(rows, err)
	})
}

// RecordOutcome closes the effect with its terminal OUTCOME and moves the lease
// to newState, stamping issued/expires when the transition established them. It
// clears the crash fence so the term ends.
func (r *DynamicRuntime) RecordOutcome(ctx context.Context, lease ClaimedLease, effectID, kind, outcome, newState string, issuedAt, expiresAt time.Time) error {
	// A settled row parks next_attempt_at far ahead so it is never re-claimed;
	// an activated lease parks it at expiry so the worker expires it on time.
	next := time.Now().UTC().Add(365 * 24 * time.Hour)
	if newState == "active" && !expiresAt.IsZero() {
		next = expiresAt
	}
	return r.settle(ctx, lease, effectID, kind, outcome, newState, issuedAt, expiresAt, next)
}

// RecordOutcomeRetry closes an effect with a `failure` outcome but keeps the
// lease in a transient state for another attempt: the provider was unreachable
// (a definite non-event), so the transition simply runs again later.
func (r *DynamicRuntime) RecordOutcomeRetry(ctx context.Context, lease ClaimedLease, effectID, kind, keepState string, nextAttempt time.Time) error {
	return r.settle(ctx, lease, effectID, kind, "failure", keepState, time.Time{}, time.Time{}, nextAttempt)
}

// Retry releases the lease for another attempt without changing its state: the
// provider was unreachable, so the transition is simply due again later.
func (r *DynamicRuntime) Retry(ctx context.Context, lease ClaimedLease, nextAttempt time.Time) error {
	now := time.Now().UTC()
	return r.transaction(ctx, func(tx adapterDBTX) error {
		if err := r.assertLeased(ctx, tx, lease, now); err != nil {
			return err
		}
		rows, err := tx.dynamicQueries().dynamicRetryLease(ctx, lease, nextAttempt, now)
		return fenceRows(rows, err)
	})
}

// EnterUnknown records an ambiguous outcome: the OUTCOME effect is `unknown`,
// the lease moves to `unknown`, and reconcile settles it later.
func (r *DynamicRuntime) EnterUnknown(ctx context.Context, lease ClaimedLease, effectID, kind string, nextAttempt time.Time) error {
	return r.settle(ctx, lease, effectID, kind, "unknown", "unknown", time.Time{}, time.Time{}, nextAttempt)
}

// assertLeased re-checks this worker still holds the lease crash fence inside
// the settling transaction. A worker that lost its lease affects zero rows.
func (r *DynamicRuntime) assertLeased(ctx context.Context, tx adapterDBTX, lease ClaimedLease, now time.Time) error {
	count, err := tx.dynamicQueries().dynamicAssertLease(ctx, lease, now)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%w: dynamic lease no longer held", ErrConflict)
	}
	return nil
}

func (r *DynamicRuntime) insertLeaseAudit(ctx context.Context, tx adapterDBTX, lease ClaimedLease, id, typ, outcome string, at time.Time, payload []byte) error {
	return tx.dynamicQueries().dynamicInsertLeaseAudit(ctx, lease, id, typ, outcome, at, payload)
}
