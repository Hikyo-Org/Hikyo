package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func (r *DynamicRuntime) readDB() adapterDB {
	if r.db.Engine() == EnginePostgres {
		return pgAdoptDB{db: r.db.PG()}
	}
	return sqliteAdoptDB{db: r.db.SQLiteRead()}
}

func (r *DynamicRuntime) transaction(ctx context.Context, fn func(adapterDBTX) error) error {
	return dynamicTransaction(ctx, r.db, fn)
}

// dynamicTransaction opens an engine-appropriate transaction wrapped in the
// same adapterDBTX dialect the adapter runtime uses.
func dynamicTransaction(ctx context.Context, db *DB, fn func(adapterDBTX) error) error {
	if db.Engine() == EnginePostgres {
		tx, err := db.PG().Begin(ctx)
		if err != nil {
			return err
		}
		wrapped := pgAdapterTx{tx: tx}
		if err := fn(wrapped); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		return tx.Commit(ctx)
	}
	tx, err := db.SQLiteWrite().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	wrapped := sqliteAdapterTx{tx: tx}
	if err := fn(wrapped); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func dynamicClaimTime(v any) time.Time {
	switch value := v.(type) {
	case time.Time:
		return value.UTC()
	case string:
		t, _ := time.Parse(timeFormat, value)
		return t.UTC()
	case []byte:
		t, _ := time.Parse(timeFormat, string(value))
		return t.UTC()
	}
	return time.Time{}
}

// ClaimDueLease leases one due lease for a transition term. Due = a transient
// state past its next_attempt_at, or an active lease past its expiry (a natural
// expiry the worker completes by dropping the now-dead role). A per-org cap and
// SKIP LOCKED keep concurrent workers off each other; the claim stamps
// lease_owner + lease_expires_at, which every settle re-asserts.
func (r *DynamicRuntime) ClaimDueLease(ctx context.Context, worker string, now, leaseUntil time.Time) (ClaimedLease, bool, error) {
	var out ClaimedLease
	err := r.transaction(ctx, func(tx adapterDBTX) error {
		selectQuery := tx.SQL(
			`SELECT l.id,l.org_id,l.project_id,l.environment_id,l.provider_id,l.provider_handle,l.principal_id,l.principal_class,l.state,l.max_ttl_seconds,l.issued_at,l.expires_at,l.attempt_count,l.lease_claim_token
             FROM dynamic_leases l
             WHERE ((l.state IN ('minting','renewing','revoking','unknown') AND l.next_attempt_at<=?) OR (l.state='active' AND l.expires_at IS NOT NULL AND l.expires_at<=? AND l.next_attempt_at<=?))
               AND (l.lease_owner IS NULL OR l.lease_expires_at IS NULL OR l.lease_expires_at<=?)
               AND (SELECT COUNT(*) FROM dynamic_leases x WHERE x.org_id=l.org_id AND x.lease_owner IS NOT NULL AND x.lease_expires_at>?) < 4
             ORDER BY l.next_attempt_at,l.id LIMIT 1`,
			`SELECT l.id,l.org_id,l.project_id,l.environment_id,l.provider_id,l.provider_handle,l.principal_id,l.principal_class,l.state,l.max_ttl_seconds,l.issued_at,l.expires_at,l.attempt_count,l.lease_claim_token
             FROM dynamic_leases l
             WHERE ((l.state IN ('minting','renewing','revoking','unknown') AND l.next_attempt_at<=$1) OR (l.state='active' AND l.expires_at IS NOT NULL AND l.expires_at<=$2 AND l.next_attempt_at<=$3))
               AND (l.lease_owner IS NULL OR l.lease_expires_at IS NULL OR l.lease_expires_at<=$4)
               AND (SELECT COUNT(*) FROM dynamic_leases x WHERE x.org_id=l.org_id AND x.lease_owner IS NOT NULL AND x.lease_expires_at>$5) < 4
             ORDER BY l.next_attempt_at,l.id FOR UPDATE SKIP LOCKED LIMIT 1`)
		nowArg := tx.Stamp(now)
		row := tx.QueryRow(ctx, selectQuery, nowArg, nowArg, nowArg, nowArg, nowArg)
		var issued, expires any
		var priorToken int64
		if err := row.Scan(&out.ID, &out.OrgID, &out.ProjectID, &out.EnvironmentID, &out.ProviderID, &out.ProviderHandle, &out.PrincipalID, &out.PrincipalClass, &out.State, &out.MaxTTLSeconds, &issued, &expires, &out.Attempt, &priorToken); err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		out.IssuedAt = dynamicClaimTime(issued)
		out.ExpiresAt = dynamicClaimTime(expires)
		out.Attempt++
		out.LeaseOwner = worker
		out.ClaimToken = priorToken + 1
		update := tx.SQL(
			`UPDATE dynamic_leases SET attempt_count=?,lease_owner=?,lease_expires_at=?,lease_claim_token=? WHERE id=?`,
			`UPDATE dynamic_leases SET attempt_count=$1,lease_owner=$2,lease_expires_at=$3,lease_claim_token=$4 WHERE id=$5`)
		_, err := tx.Exec(ctx, update, out.Attempt, worker, tx.Stamp(leaseUntil), out.ClaimToken, out.ID)
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return ClaimedLease{}, false, nil
	}
	return out, err == nil, err
}

// LoadProviderMaterial reads the lease's provider row (origin, grant role, TLS
// mode, sealed admin credential), re-asserting the lease crash fence.
func (r *DynamicRuntime) LoadProviderMaterial(ctx context.Context, lease ClaimedLease) (LeaseProviderMaterial, error) {
	db := r.readDB()
	query := db.SQL(
		`SELECT p.kind,p.origin,p.tls_mode,p.grant_role,p.id,p.admin_credential_ciphertext FROM dynamic_providers p JOIN dynamic_leases l ON l.provider_id=p.id AND l.org_id=p.org_id AND l.project_id=p.project_id WHERE l.id=? AND l.org_id=? AND l.project_id=? AND l.lease_owner=? AND l.lease_expires_at>? AND l.lease_claim_token=?`,
		`SELECT p.kind,p.origin,p.tls_mode,p.grant_role,p.id,p.admin_credential_ciphertext FROM dynamic_providers p JOIN dynamic_leases l ON l.provider_id=p.id AND l.org_id=p.org_id AND l.project_id=p.project_id WHERE l.id=$1 AND l.org_id=$2 AND l.project_id=$3 AND l.lease_owner=$4 AND l.lease_expires_at>$5 AND l.lease_claim_token=$6`)
	var out LeaseProviderMaterial
	var credential []byte
	if err := db.QueryRow(ctx, query, lease.ID, lease.OrgID, lease.ProjectID, lease.LeaseOwner, db.Stamp(time.Now().UTC()), lease.ClaimToken).Scan(&out.Kind, &out.Origin, &out.TLSMode, &out.GrantRole, &out.CredentialOwnerID, &credential); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return LeaseProviderMaterial{}, ErrNotFound
		}
		return LeaseProviderMaterial{}, err
	}
	if len(credential) == 0 {
		return LeaseProviderMaterial{}, ErrNoProviderCredential
	}
	out.CredentialCiphertext = append([]byte(nil), credential...)
	return out, nil
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
	db := r.readDB()
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM dynamic_leases WHERE state='active'`).Scan(&activeLeases); err != nil {
		return 0, 0, err
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM dynamic_effects WHERE outcome='unknown'`).Scan(&unknownEffects); err != nil {
		return 0, 0, err
	}
	return activeLeases, unknownEffects, nil
}

// LatestEffectKind returns the kind of the lease's most recent effect, so
// reconcile of an `unknown` lease can resume the exact transition that went
// ambiguous rather than guessing.
func (r *DynamicRuntime) LatestEffectKind(ctx context.Context, lease ClaimedLease) (string, error) {
	db := r.readDB()
	query := db.SQL(
		`SELECT kind FROM dynamic_effects WHERE lease_id=? AND org_id=? ORDER BY created_at DESC,id DESC LIMIT 1`,
		`SELECT kind FROM dynamic_effects WHERE lease_id=$1 AND org_id=$2 ORDER BY created_at DESC,id DESC LIMIT 1`)
	var kind string
	if err := db.QueryRow(ctx, query, lease.ID, lease.OrgID).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return kind, nil
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
		payload, _ := json.Marshal(map[string]string{"kind": kind, "provider_handle": lease.ProviderHandle})
		if err := r.insertLeaseAudit(ctx, tx, lease, intentAudit, "dynamic.lease_transition_intent", "intent", now, payload); err != nil {
			return err
		}
		insert := tx.SQL(
			`INSERT INTO dynamic_effects (id,org_id,project_id,environment_id,lease_id,kind,intent_audit_id,created_at) VALUES (?,?,?,?,?,?,?,?)`,
			`INSERT INTO dynamic_effects (id,org_id,project_id,environment_id,lease_id,kind,intent_audit_id,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`)
		_, err := tx.Exec(ctx, insert, effectID, lease.OrgID, lease.ProjectID, lease.EnvironmentID, lease.ID, kind, intentAudit, tx.Stamp(now))
		return err
	})
	if err != nil {
		return "", err
	}
	return effectID, nil
}

// RecordOutcome closes the effect with its terminal OUTCOME (effect row + audit
// event) and moves the lease to newState, stamping issued/expires when the
// transition established them. It clears the crash fence so the term ends.
func (r *DynamicRuntime) RecordOutcome(ctx context.Context, lease ClaimedLease, effectID, kind, outcome, newState string, issuedAt, expiresAt time.Time) error {
	outcomeAudit := "dau_" + uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC()
	return r.transaction(ctx, func(tx adapterDBTX) error {
		if err := r.assertLeased(ctx, tx, lease, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"kind": kind, "provider_handle": lease.ProviderHandle})
		if err := r.insertLeaseAudit(ctx, tx, lease, outcomeAudit, "dynamic.lease_transition_outcome", outcome, now, payload); err != nil {
			return err
		}
		closeEffect := tx.SQL(
			`UPDATE dynamic_effects SET outcome=?,outcome_audit_id=?,finished_at=? WHERE id=? AND org_id=?`,
			`UPDATE dynamic_effects SET outcome=$1,outcome_audit_id=$2,finished_at=$3 WHERE id=$4 AND org_id=$5`)
		if _, err := tx.Exec(ctx, closeEffect, outcome, outcomeAudit, tx.Stamp(now), effectID, lease.OrgID); err != nil {
			return err
		}
		var issued, expires any
		if !issuedAt.IsZero() {
			issued = tx.Stamp(issuedAt)
		}
		if !expiresAt.IsZero() {
			expires = tx.Stamp(expiresAt)
		}
		// next_attempt_at is set far ahead for a settled row; a re-derivable
		// future stamp keeps the not-null column satisfied without re-claiming.
		next := tx.Stamp(now.Add(365 * 24 * time.Hour))
		if newState == "active" && !expiresAt.IsZero() {
			next = tx.Stamp(expiresAt)
		}
		update := tx.SQL(
			`UPDATE dynamic_leases SET state=?,issued_at=COALESCE(?,issued_at),expires_at=COALESCE(?,expires_at),last_transition_at=?,next_attempt_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND org_id=? AND lease_owner=? AND lease_expires_at>? AND lease_claim_token=?`,
			`UPDATE dynamic_leases SET state=$1,issued_at=COALESCE($2,issued_at),expires_at=COALESCE($3,expires_at),last_transition_at=$4,next_attempt_at=$5,lease_owner=NULL,lease_expires_at=NULL WHERE id=$6 AND org_id=$7 AND lease_owner=$8 AND lease_expires_at>$9 AND lease_claim_token=$10`)
		rows, err := tx.Exec(ctx, update, newState, issued, expires, tx.Stamp(now), next, lease.ID, lease.OrgID, lease.LeaseOwner, tx.Stamp(now), lease.ClaimToken)
		return fenceRows(rows, err)
	})
}

// RecordOutcomeRetry closes an effect with a `failure` outcome but keeps the
// lease in a transient state for another attempt: the provider was unreachable
// (a definite non-event), so the transition simply runs again later.
func (r *DynamicRuntime) RecordOutcomeRetry(ctx context.Context, lease ClaimedLease, effectID, kind, keepState string, nextAttempt time.Time) error {
	outcomeAudit := "dau_" + uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC()
	return r.transaction(ctx, func(tx adapterDBTX) error {
		if err := r.assertLeased(ctx, tx, lease, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"kind": kind, "provider_handle": lease.ProviderHandle})
		if err := r.insertLeaseAudit(ctx, tx, lease, outcomeAudit, "dynamic.lease_transition_outcome", "failure", now, payload); err != nil {
			return err
		}
		closeEffect := tx.SQL(
			`UPDATE dynamic_effects SET outcome='failure',outcome_audit_id=?,finished_at=? WHERE id=? AND org_id=?`,
			`UPDATE dynamic_effects SET outcome='failure',outcome_audit_id=$1,finished_at=$2 WHERE id=$3 AND org_id=$4`)
		if _, err := tx.Exec(ctx, closeEffect, outcomeAudit, tx.Stamp(now), effectID, lease.OrgID); err != nil {
			return err
		}
		update := tx.SQL(
			`UPDATE dynamic_leases SET state=?,last_transition_at=?,next_attempt_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND org_id=? AND lease_owner=? AND lease_expires_at>? AND lease_claim_token=?`,
			`UPDATE dynamic_leases SET state=$1,last_transition_at=$2,next_attempt_at=$3,lease_owner=NULL,lease_expires_at=NULL WHERE id=$4 AND org_id=$5 AND lease_owner=$6 AND lease_expires_at>$7 AND lease_claim_token=$8`)
		rows, err := tx.Exec(ctx, update, keepState, tx.Stamp(now), tx.Stamp(nextAttempt), lease.ID, lease.OrgID, lease.LeaseOwner, tx.Stamp(now), lease.ClaimToken)
		return fenceRows(rows, err)
	})
}

// Retry releases the lease for another attempt without changing its state: the
// provider was unreachable, so the transition is simply due again later.
func (r *DynamicRuntime) Retry(ctx context.Context, lease ClaimedLease, nextAttempt time.Time) error {
	now := time.Now().UTC()
	return r.transaction(ctx, func(tx adapterDBTX) error {
		if err := r.assertLeased(ctx, tx, lease, now); err != nil {
			return err
		}
		update := tx.SQL(
			`UPDATE dynamic_leases SET next_attempt_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND org_id=? AND lease_owner=? AND lease_expires_at>? AND lease_claim_token=?`,
			`UPDATE dynamic_leases SET next_attempt_at=$1,lease_owner=NULL,lease_expires_at=NULL WHERE id=$2 AND org_id=$3 AND lease_owner=$4 AND lease_expires_at>$5 AND lease_claim_token=$6`)
		rows, err := tx.Exec(ctx, update, tx.Stamp(nextAttempt), lease.ID, lease.OrgID, lease.LeaseOwner, tx.Stamp(now), lease.ClaimToken)
		return fenceRows(rows, err)
	})
}

// EnterUnknown records an ambiguous outcome: the OUTCOME effect is `unknown`,
// the lease moves to `unknown`, and reconcile settles it later.
func (r *DynamicRuntime) EnterUnknown(ctx context.Context, lease ClaimedLease, effectID, kind string, nextAttempt time.Time) error {
	outcomeAudit := "dau_" + uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC()
	return r.transaction(ctx, func(tx adapterDBTX) error {
		if err := r.assertLeased(ctx, tx, lease, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"kind": kind, "provider_handle": lease.ProviderHandle})
		if err := r.insertLeaseAudit(ctx, tx, lease, outcomeAudit, "dynamic.lease_transition_outcome", "unknown", now, payload); err != nil {
			return err
		}
		closeEffect := tx.SQL(
			`UPDATE dynamic_effects SET outcome='unknown',outcome_audit_id=?,finished_at=? WHERE id=? AND org_id=?`,
			`UPDATE dynamic_effects SET outcome='unknown',outcome_audit_id=$1,finished_at=$2 WHERE id=$3 AND org_id=$4`)
		if _, err := tx.Exec(ctx, closeEffect, outcomeAudit, tx.Stamp(now), effectID, lease.OrgID); err != nil {
			return err
		}
		update := tx.SQL(
			`UPDATE dynamic_leases SET state='unknown',last_transition_at=?,next_attempt_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND org_id=? AND lease_owner=? AND lease_expires_at>? AND lease_claim_token=?`,
			`UPDATE dynamic_leases SET state='unknown',last_transition_at=$1,next_attempt_at=$2,lease_owner=NULL,lease_expires_at=NULL WHERE id=$3 AND org_id=$4 AND lease_owner=$5 AND lease_expires_at>$6 AND lease_claim_token=$7`)
		rows, err := tx.Exec(ctx, update, tx.Stamp(now), tx.Stamp(nextAttempt), lease.ID, lease.OrgID, lease.LeaseOwner, tx.Stamp(now), lease.ClaimToken)
		return fenceRows(rows, err)
	})
}

// assertLeased re-checks this worker still holds the lease crash fence inside
// the settling transaction. A worker that lost its lease affects zero rows.
func (r *DynamicRuntime) assertLeased(ctx context.Context, tx adapterDBTX, lease ClaimedLease, now time.Time) error {
	query := tx.SQL(
		`SELECT COUNT(*) FROM dynamic_leases WHERE id=? AND org_id=? AND lease_owner=? AND lease_expires_at>? AND lease_claim_token=?`,
		`SELECT COUNT(*) FROM dynamic_leases WHERE id=$1 AND org_id=$2 AND lease_owner=$3 AND lease_expires_at>$4 AND lease_claim_token=$5`)
	var count int
	if err := tx.QueryRow(ctx, query, lease.ID, lease.OrgID, lease.LeaseOwner, tx.Stamp(now), lease.ClaimToken).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%w: dynamic lease no longer held", ErrConflict)
	}
	return nil
}

func (r *DynamicRuntime) insertLeaseAudit(ctx context.Context, tx adapterDBTX, lease ClaimedLease, id, typ, outcome string, at time.Time, payload []byte) error {
	query := tx.SQL(
		`INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES (?,?,1,?,0,?,NULL,'system',?,'env',?,?,?,'dynamic-lease',?,?,?,'system',?)`,
		`INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES ($1,$2,1,$3,false,$4,NULL,'system',$5,'env',$6,$7,$8,'dynamic-lease',$9,$10,$11,'system',$12)`)
	stamp := tx.Stamp(at)
	_, err := tx.Exec(ctx, query, id, typ, stamp, stamp, lease.PrincipalID, lease.OrgID, lease.ProjectID, lease.EnvironmentID, lease.ID, outcome, lease.ID, string(payload))
	return err
}
