package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// adapterPushOutcomePayload is the audit payload for adapter.push_outcome events.
// omitempty mirrors the previous conditional map assembly exactly.
type adapterPushOutcomePayload struct {
	Surface        string `json:"surface"`
	EffectiveName  string `json:"effective_name"`
	Disposition    string `json:"disposition"`
	Finding        string `json:"finding,omitempty"`
	OwnedMissing   bool   `json:"owned_missing,omitempty"`
	ProviderStatus int    `json:"provider_status,omitempty"`
}

// adapterTestPayload is the audit payload for adapter.test events.
type adapterTestPayload struct {
	Version       string `json:"version"`
	DestinationID int64  `json:"destination_id"`
}

type AdapterAuthorizer func(context.Context, adapter.Job, adapter.Effect) error

// AdapterRuntime is the domain-specific outbox's system boundary. Tenant
// request paths never receive it; configuration and adoption remain ordinary
// proof-carrying repositories. Its methods bind every statement to the leased
// job's immutable chain and re-check generation/authority at every effect.
type AdapterRuntime struct {
	db        *DB
	authorize AdapterAuthorizer
}

type AdapterSnapshotEntry struct {
	ID, SnapshotID, KeyID, KeyName, Classification string
	Ciphertext                                     []byte
}

type AdapterExecution struct {
	Provider, Origin, CredentialOwnerID string
	CredentialCiphertext                []byte
	Target                              adapter.Target
	Entries                             []AdapterSnapshotEntry
	Ledger                              []adapter.LedgerEntry
	Revision                            int64
}

type AdapterActivation struct {
	Provider, Origin, CredentialOwnerID string
	CredentialCiphertext                []byte
	Target                              adapter.Target
}

func adapterJobScope(job adapter.Job) domain.Scope {
	return domain.Scope{
		Org:     domain.OrgID(job.OrgID),
		Project: domain.ProjectID(job.ProjectID),
	}
}

func NewAdapterRuntime(db *DB, authorize AdapterAuthorizer) *AdapterRuntime {
	return &AdapterRuntime{db: db, authorize: authorize}
}

// LoadExecution reads only the immutable inputs named by a leased job. The
// caller must Gate immediately before this method; every query repeats the
// complete job chain and generation fence. Ciphertexts remain sealed here.
func (r *AdapterRuntime) LoadExecution(ctx context.Context, job adapter.Job) (AdapterExecution, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) (AdapterExecution, error) {
		var out AdapterExecution
		var kind string
		query := db.SQL(
			`SELECT a.provider,a.origin,a.id,a.credential_ciphertext,t.destination_kind,t.destination_owner,t.destination_name,t.destination_environment,t.destination_id,t.repository_id,t.visibility,t.selected_repository_ids,t.name_prefix,t.generation FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id JOIN adapter_outbox j ON j.id=? AND j.target_id=t.id AND j.org_id=t.org_id AND j.project_id=t.project_id AND j.environment_id=t.environment_id WHERE t.id=? AND t.org_id=? AND t.project_id=? AND t.environment_id=? AND t.generation=? AND j.state='running' AND j.lease_owner=?`,
		)
		args := []any{job.ID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation, job.LeaseOwner}
		var credential, selectedRaw []byte
		if err := db.QueryRow(ctx, query, args...).Scan(&out.Provider, &out.Origin, &out.CredentialOwnerID, &credential, &kind, &out.Target.Destination.Owner, &out.Target.Destination.Name, &out.Target.Destination.Environment, &out.Target.Destination.NumericID, &out.Target.Destination.RepositoryID, &out.Target.Destination.Visibility, &selectedRaw, &out.Target.NamePrefix, &out.Target.Generation); err != nil {
			if isNoRows(err) {
				return AdapterExecution{}, ErrNotFound
			}
			return AdapterExecution{}, err
		}
		if len(credential) == 0 {
			return AdapterExecution{}, fmt.Errorf("%w: adapter credential is absent", adapter.ErrProviderAuth)
		}
		out.CredentialCiphertext = append([]byte(nil), credential...)
		out.Target.ID = job.TargetID
		out.Target.Environment = job.EnvironmentID
		out.Target.Destination.Kind = adapter.DestinationKind(kind)
		if err := json.Unmarshal(selectedRaw, &out.Target.Destination.SelectedRepositoryIDs); err != nil {
			return AdapterExecution{}, fmt.Errorf("store: adapter selected repository ids: %w", err)
		}

		ledgerQuery := db.SQL(
			`SELECT surface,effective_name,state,missing FROM adapter_ledger WHERE target_id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'released' ORDER BY surface,normalized_name`,
		)
		ledgerRows, err := db.Query(ctx, ledgerQuery, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID)
		if err != nil {
			return AdapterExecution{}, err
		}
		defer closeAdapterRows(ledgerRows)
		for ledgerRows.Next() {
			entry, err := scanAdapterLedgerEntry(ledgerRows)
			if err != nil {
				return AdapterExecution{}, err
			}
			out.Ledger = append(out.Ledger, entry)
		}
		if err := ledgerRows.Err(); err != nil {
			return AdapterExecution{}, err
		}
		if job.Kind == adapter.Scrub {
			return out, nil
		}

		snapshotQuery := db.SQLPerEngine(
			`SELECT id,revision FROM snapshots WHERE org_id=? AND project_id=? AND environment_id=? AND payload_present=1 ORDER BY revision DESC LIMIT 1`,
			`SELECT id,revision FROM snapshots WHERE org_id=$1 AND project_id=$2 AND environment_id=$3 AND payload_present=true ORDER BY revision DESC LIMIT 1`)
		var snapshotID string
		if err := db.QueryRow(ctx, snapshotQuery, job.OrgID, job.ProjectID, job.EnvironmentID).Scan(&snapshotID, &out.Revision); err != nil {
			if isNoRows(err) {
				return out, nil
			}
			return AdapterExecution{}, err
		}
		entryQuery := db.SQL(
			`SELECT e.id,e.snapshot_id,e.key_id,e.key_name,e.classification,e.ciphertext FROM snapshot_entries e JOIN adapter_target_keys k ON k.key_id=e.key_id AND k.target_id=? AND k.org_id=e.org_id AND k.project_id=e.project_id AND k.environment_id=e.environment_id WHERE e.snapshot_id=? AND e.org_id=? AND e.project_id=? AND e.environment_id=? ORDER BY e.key_name`,
		)
		entryRows, err := db.Query(ctx, entryQuery, job.TargetID, snapshotID, job.OrgID, job.ProjectID, job.EnvironmentID)
		if err != nil {
			return AdapterExecution{}, err
		}
		defer closeAdapterRows(entryRows)
		for entryRows.Next() {
			var entry AdapterSnapshotEntry
			if err := entryRows.Scan(&entry.ID, &entry.SnapshotID, &entry.KeyID, &entry.KeyName, &entry.Classification, &entry.Ciphertext); err != nil {
				return AdapterExecution{}, err
			}
			out.Entries = append(out.Entries, entry)
		}
		if err := entryRows.Err(); err != nil {
			return AdapterExecution{}, err
		}
		return out, nil
	})
}

// LoadActivation resolves only the pending route and its outbound credential.
// It never assembles snapshot plaintext. Caller must Gate immediately before
// this read; Activate later rechecks the exact leased job and move identity.
func (r *AdapterRuntime) LoadActivation(ctx context.Context, job adapter.Job) (AdapterActivation, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) (AdapterActivation, error) {
		if job.Kind != adapter.Activate || job.RouteMoveID == "" {
			return AdapterActivation{}, fmt.Errorf("%w: job is not a route activation", domain.ErrInvalid)
		}
		query := db.SQL(
			`SELECT a.provider,COALESCE(m.pending_origin,a.origin),a.id,COALESCE(m.pending_credential_ciphertext,a.credential_ciphertext),mt.environment_id,mt.destination_kind,mt.destination_owner,mt.destination_name,mt.destination_environment,mt.destination_id,mt.repository_id,mt.visibility,mt.selected_repository_ids,mt.name_prefix,t.generation FROM adapter_outbox j JOIN adapter_targets t ON t.id=j.target_id AND t.org_id=j.org_id AND t.project_id=j.project_id AND t.environment_id=j.environment_id JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id JOIN adapter_route_moves m ON m.id=j.route_move_id AND m.org_id=j.org_id AND m.project_id=j.project_id AND m.adapter_id=a.id JOIN adapter_route_move_targets mt ON mt.move_id=m.id AND mt.target_id=t.id AND mt.org_id=t.org_id AND mt.project_id=t.project_id WHERE j.id=? AND j.route_move_id=? AND j.target_id=? AND j.org_id=? AND j.project_id=? AND j.environment_id=? AND j.generation=? AND j.kind='activate' AND j.state='running' AND j.lease_owner=? AND m.state='activating' AND t.state='moving'`,
		)
		var out AdapterActivation
		var credential, selectedRaw []byte
		var kind string
		args := []any{job.ID, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation, job.LeaseOwner}
		if err := db.QueryRow(ctx, query, args...).Scan(&out.Provider, &out.Origin, &out.CredentialOwnerID, &credential, &out.Target.Environment, &kind,
			&out.Target.Destination.Owner, &out.Target.Destination.Name, &out.Target.Destination.Environment, &out.Target.Destination.NumericID, &out.Target.Destination.RepositoryID, &out.Target.Destination.Visibility, &selectedRaw,
			&out.Target.NamePrefix, &out.Target.Generation); err != nil {
			if isNoRows(err) {
				return AdapterActivation{}, ErrNotFound
			}
			return AdapterActivation{}, err
		}
		if len(credential) == 0 {
			return AdapterActivation{}, fmt.Errorf("%w: adapter credential is absent", adapter.ErrProviderAuth)
		}
		out.CredentialCiphertext = append([]byte(nil), credential...)
		out.Target.ID = job.TargetID
		out.Target.Destination.Kind = adapter.DestinationKind(kind)
		if err := json.Unmarshal(selectedRaw, &out.Target.Destination.SelectedRepositoryIDs); err != nil {
			return AdapterActivation{}, fmt.Errorf("store: adapter selected repository ids: %w", err)
		}
		return out, nil
	})
}

type adapterRow = interface{ Scan(...any) error }
type adapterRows = adapterTargetRows

type adapterDBTX interface {
	Exec(context.Context, string, ...any) (int64, error)
	QueryRow(context.Context, string, ...any) adapterRow
	Query(context.Context, string, ...any) (adapterRows, error)
	adapterDialect
	Commit(context.Context) error
	Rollback(context.Context) error
}

type sqliteAdapterTx struct {
	sqliteDialect
	tx sqliteTransaction
}
type sqliteAdapterRows struct{ rows *sql.Rows }

func (r sqliteAdapterRows) Next() bool               { return r.rows.Next() }
func (r sqliteAdapterRows) Scan(values ...any) error { return r.rows.Scan(values...) }
func (r sqliteAdapterRows) Err() error               { return r.rows.Err() }
func (r sqliteAdapterRows) Close()                   { _ = r.rows.Close() }

func (s sqliteAdapterTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	result, err := s.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func (s sqliteAdapterTx) QueryRow(ctx context.Context, query string, args ...any) adapterRow {
	return s.tx.QueryRowContext(ctx, query, args...)
}
func (s sqliteAdapterTx) Query(ctx context.Context, query string, args ...any) (adapterRows, error) {
	rows, err := s.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return sqliteAdapterRows{rows: rows}, nil
}
func (s sqliteAdapterTx) Commit(context.Context) error   { return s.tx.Commit() }
func (s sqliteAdapterTx) Rollback(context.Context) error { return s.tx.Rollback() }

type pgAdapterTx struct {
	pgDialect
	tx postgresTransaction
}

func (p pgAdapterTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	tag, err := p.tx.Exec(ctx, query, args...)
	return tag.RowsAffected(), err
}
func (p pgAdapterTx) QueryRow(ctx context.Context, query string, args ...any) adapterRow {
	return p.tx.QueryRow(ctx, query, args...)
}
func (p pgAdapterTx) Query(ctx context.Context, query string, args ...any) (adapterRows, error) {
	return p.tx.Query(ctx, query, args...)
}
func (p pgAdapterTx) Commit(ctx context.Context) error   { return p.tx.Commit(ctx) }
func (p pgAdapterTx) Rollback(ctx context.Context) error { return p.tx.Rollback(ctx) }

func (r *AdapterRuntime) transaction(ctx context.Context, fn func(adapterDBTX) error) error {
	return dbTransaction(ctx, r.db, fn)
}

// dbTransaction runs fn in one engine-appropriate write transaction wrapped in
// the adapterDBTX dialect. Postgres uses SERIALIZABLE for BOTH the adapter and
// the dynamic-secret outbox (system-architecture § Transaction boundary:
// publish-class operations are serializable; #147's handoff pins the dynamic
// worker as "the adapter-outbox composition, verbatim"). Neither outbox wraps a
// 40001 retry here: a serialization failure surfaces as a loud error and the
// worker loop re-drives the transition on its next pass (via next_attempt_at),
// exactly as the adapter path does — so no retry wrapper is added, matching
// existing adapter behaviour (its only retry is retryAdapterProviderFence for
// ErrProviderBusy).
func dbTransaction(ctx context.Context, db *DB, fn func(adapterDBTX) error) error {
	if db.Engine() == EnginePostgres {
		tx, err := db.BeginPostgres(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return err
		}
		wrapped := pgAdapterTx{tx: tx}
		defer wrapped.Rollback(ctx)
		if err := fn(wrapped); err != nil {
			_ = wrapped.Rollback(ctx)
			return err
		}
		return wrapped.Commit(ctx)
	}
	tx, err := db.BeginSQLite(ctx, false)
	if err != nil {
		return err
	}
	wrapped := sqliteAdapterTx{tx: tx}
	defer wrapped.Rollback(ctx)
	if err := fn(wrapped); err != nil {
		_ = wrapped.Rollback(ctx)
		return err
	}
	return wrapped.Commit(ctx)
}

// Enqueue fences the previous goal and installs the newest target goal in one
// transaction. It waits while a provider request is durably marked in flight;
// a generation bump can therefore never race past an HTTP request already on
// the wire. The caller controls cancellation of that wait.
func (r *AdapterRuntime) Enqueue(ctx context.Context, job adapter.Job, now time.Time) (adapter.Job, error) {
	if job.Kind != adapter.Converge && job.Kind != adapter.Scrub {
		return adapter.Job{}, fmt.Errorf("store: invalid adapter job kind %q", job.Kind)
	}
	if job.OrgID == "" || job.ProjectID == "" || job.EnvironmentID == "" || job.TargetID == "" || job.AuthorityPrincipal == "" {
		return adapter.Job{}, errors.New("store: adapter job requires its complete tenant chain, target, and authority")
	}
	if job.ID == "" {
		job.ID = newAdapterID("job")
	}
	waitedForProvider := false
	for {
		out, err := r.tryEnqueue(ctx, job, now)
		if !errors.Is(err, adapter.ErrProviderBusy) {
			if waitedForProvider && ctx.Err() != nil {
				return adapter.Job{}, fmt.Errorf("%w: %v", adapter.ErrProviderBusy, ctx.Err())
			}
			return out, err
		}
		waitedForProvider = true
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return adapter.Job{}, fmt.Errorf("%w: %v", adapter.ErrProviderBusy, ctx.Err())
		case <-timer.C:
		}
	}
}

func (r *AdapterRuntime) tryEnqueue(ctx context.Context, job adapter.Job, now time.Time) (adapter.Job, error) {
	now = now.UTC()
	err := r.transaction(ctx, func(tx adapterDBTX) error {
		lookup := tx.SQLPerEngine(
			`SELECT generation FROM adapter_targets WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=?)`,
			`SELECT generation FROM adapter_targets WHERE id=$1 AND org_id=$2 AND project_id=$3 AND environment_id=$4 AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=$5) FOR UPDATE`)
		var current int64
		err := tx.QueryRow(ctx, lookup, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, tx.Stamp(now)).Scan(&current)
		if isNoRows(err) {
			var exists int
			existsQuery := tx.SQL(
				`SELECT COUNT(*) FROM adapter_targets WHERE id=? AND org_id=? AND project_id=? AND environment_id=?`,
			)
			if scanErr := tx.QueryRow(ctx, existsQuery, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID).Scan(&exists); scanErr != nil {
				return scanErr
			}
			if exists == 1 {
				return adapter.ErrProviderBusy
			}
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var depth int
		depthQuery := tx.SQL(
			`SELECT COUNT(*) FROM adapter_outbox WHERE target_id=? AND org_id=? AND project_id=? AND environment_id=? AND state IN ('queued','running')`,
		)
		if err := tx.QueryRow(ctx, depthQuery, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID).Scan(&depth); err != nil {
			return err
		}
		if depth >= 1000 {
			return adapter.ErrQueueFull
		}
		finished := tx.Stamp(now)
		var previousJob string
		previousQuery := tx.SQL(
			`SELECT id FROM adapter_outbox WHERE target_id=? AND org_id=? AND project_id=? AND environment_id=? AND state IN ('queued','running') ORDER BY created_at DESC LIMIT 1`,
		)
		previousErr := tx.QueryRow(ctx, previousQuery, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID).Scan(&previousJob)
		if previousErr != nil && !errors.Is(previousErr, sql.ErrNoRows) && !errors.Is(previousErr, pgx.ErrNoRows) {
			return previousErr
		}
		supersede := tx.SQL(
			`UPDATE adapter_outbox SET state='superseded',finished_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE target_id=? AND org_id=? AND project_id=? AND environment_id=? AND state IN ('queued','running')`,
		)
		if _, err := tx.Exec(ctx, supersede, finished, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID); err != nil {
			return err
		}
		job.Generation = current + 1
		insert := tx.SQL(
			`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (?,?,?,?,?,?,?,?,?,0,?,'queued',?)`,
		)
		if _, err := tx.Exec(ctx, insert, job.ID, job.OrgID, job.ProjectID, job.EnvironmentID, job.TargetID, string(job.Kind), job.AuthorityPrincipal, job.Generation, job.TargetID, finished, finished); err != nil {
			return err
		}
		state := "active"
		if job.Kind == adapter.Scrub {
			state = "tombstoned"
		}
		update := tx.SQL(
			`UPDATE adapter_targets SET generation=?,state=?,sync_status='converging',active_job_id=?,provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=?)`,
		)
		rows, err := tx.Exec(ctx, update, job.Generation, state, job.ID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, current, finished)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrProviderBusy
		}
		if previousJob != "" {
			payload, _ := json.Marshal(map[string]string{"previous_job_id": previousJob, "job_id": job.ID})
			if err := r.insertAdapterJobAudit(ctx, tx, job, "adapter.superseded", "success", now, payload); err != nil {
				return err
			}
		}
		return nil
	})
	return job, err
}

func (r *AdapterRuntime) ClaimDue(ctx context.Context, worker string, now, leaseUntil time.Time) (adapter.Job, bool, error) {
	var out adapter.Job
	err := r.transaction(ctx, func(tx adapterDBTX) error {
		// A paused target's jobs stay queued and are never claimed (#157):
		// pause is a claim-time gate, so configuration and adoption may keep
		// queueing goals that the next resume will run.
		selectQuery := tx.SQLPerEngine(
			`SELECT j.id,j.org_id,j.project_id,j.environment_id,j.target_id,j.kind,COALESCE(j.route_move_id,''),j.authority_principal_id,j.generation,j.attempt_count,j.created_at
             FROM adapter_outbox j
             JOIN adapter_targets t ON t.id=j.target_id AND t.org_id=j.org_id AND t.project_id=j.project_id AND t.environment_id=j.environment_id AND t.paused_at IS NULL
             WHERE ((j.state='queued' AND j.next_attempt_at<=?) OR (j.state='running' AND j.lease_expires_at<=?))
               AND (SELECT COUNT(*) FROM adapter_outbox x WHERE x.org_id=j.org_id AND x.state='running' AND x.lease_expires_at>?) < 4
               AND NOT EXISTS (SELECT 1 FROM adapter_outbox x WHERE x.target_id=j.target_id AND x.id<>j.id AND x.state='running' AND x.lease_expires_at>?)
             ORDER BY j.next_attempt_at,j.id LIMIT 1`,
			`SELECT j.id,j.org_id,j.project_id,j.environment_id,j.target_id,j.kind,COALESCE(j.route_move_id,''),j.authority_principal_id,j.generation,j.attempt_count,j.created_at
             FROM adapter_outbox j
             JOIN adapter_targets t ON t.id=j.target_id AND t.org_id=j.org_id AND t.project_id=j.project_id AND t.environment_id=j.environment_id AND t.paused_at IS NULL
             WHERE ((j.state='queued' AND j.next_attempt_at<=$1) OR (j.state='running' AND j.lease_expires_at<=$2))
               AND (SELECT COUNT(*) FROM adapter_outbox x WHERE x.org_id=j.org_id AND x.state='running' AND x.lease_expires_at>$3) < 4
               AND NOT EXISTS (SELECT 1 FROM adapter_outbox x WHERE x.target_id=j.target_id AND x.id<>j.id AND x.state='running' AND x.lease_expires_at>$4)
             ORDER BY j.next_attempt_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1`)
		nowArg := tx.Stamp(now)
		row := tx.QueryRow(ctx, selectQuery, nowArg, nowArg, nowArg, nowArg)
		var kind string
		var created any
		if err := row.Scan(&out.ID, &out.OrgID, &out.ProjectID, &out.EnvironmentID, &out.TargetID, &kind, &out.RouteMoveID, &out.AuthorityPrincipal, &out.Generation, &out.Attempt, &created); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		switch value := created.(type) {
		case time.Time:
			out.CreatedAt = value.UTC()
		case string:
			out.CreatedAt, _ = time.Parse(timeFormat, value)
		case []byte:
			out.CreatedAt, _ = time.Parse(timeFormat, string(value))
		}
		out.Kind = adapter.JobKind(kind)
		out.Attempt++
		out.LeaseOwner = worker
		update := tx.SQL(
			`UPDATE adapter_outbox SET state='running',attempt_count=?,lease_owner=?,lease_expires_at=? WHERE id=?`,
		)
		if _, err := tx.Exec(ctx, update, out.Attempt, worker, tx.Stamp(leaseUntil), out.ID); err != nil {
			return err
		}
		return r.closeIndeterminateEffects(ctx, tx, out, now)
	})
	if errors.Is(err, ErrNotFound) {
		return adapter.Job{}, false, nil
	}
	return out, err == nil, err
}

// closeIndeterminateEffects settles the crash window (#157). An effect whose
// INTENT was written but whose OUTCOME never landed belongs to an attempt that
// died between the two: the process crashed, or the lease lapsed mid-request.
// At the moment a target's next attempt is claimed, every such effect on that
// target is closed as OUTCOME unknown, correlated to the job that opened it,
// so INTENT and OUTCOME always pair in the trail. The ledger row stays
// dispatched (presumed written); the retry converges it. An effect whose
// provider-write lease is still live is left alone: a stalled node may still
// hold that request on the wire, and its Finish will close it.
func (r *AdapterRuntime) closeIndeterminateEffects(ctx context.Context, tx adapterDBTX, claimed adapter.Job, now time.Time) error {
	stamp := tx.Stamp(now)
	query := tx.SQL(
		`SELECT e.id,e.job_id,e.surface,e.effective_name,e.disposition,o.authority_principal_id FROM adapter_effects e JOIN adapter_outbox o ON o.id=e.job_id AND o.org_id=e.org_id AND o.project_id=e.project_id AND o.environment_id=e.environment_id WHERE e.target_id=? AND e.org_id=? AND e.project_id=? AND e.environment_id=? AND e.outcome IS NULL AND NOT EXISTS (SELECT 1 FROM adapter_targets t WHERE t.id=e.target_id AND t.org_id=e.org_id AND t.project_id=e.project_id AND t.environment_id=e.environment_id AND t.provider_lease_effect_id=e.id AND t.provider_lease_expires_at>?) ORDER BY e.created_at,e.id`,
	)
	rows, err := tx.Query(ctx, query, claimed.TargetID, claimed.OrgID, claimed.ProjectID, claimed.EnvironmentID, stamp)
	if err != nil {
		return err
	}
	type dangling struct{ id, jobID, surface, name, disposition, authority string }
	var open []dangling
	for rows.Next() {
		var row dangling
		if err := rows.Scan(&row.id, &row.jobID, &row.surface, &row.name, &row.disposition, &row.authority); err != nil {
			_ = closeAdapterRows(rows)
			return err
		}
		open = append(open, row)
	}
	if err := rows.Err(); err != nil {
		_ = closeAdapterRows(rows)
		return err
	}
	if err := closeAdapterRows(rows); err != nil {
		return err
	}
	for _, effect := range open {
		outcomeID := newAdapterID("aud")
		opener := adapter.Job{ID: effect.jobID, OrgID: claimed.OrgID, ProjectID: claimed.ProjectID, EnvironmentID: claimed.EnvironmentID, TargetID: claimed.TargetID, AuthorityPrincipal: effect.authority}
		payload, err := json.Marshal(adapterPushOutcomePayload{Surface: effect.surface, EffectiveName: effect.name, Disposition: effect.disposition, Finding: "crash_window"})
		if err != nil {
			return err
		}
		if err := r.insertAdapterJobAuditWithID(ctx, tx, opener, outcomeID, "adapter.push_outcome", string(adapter.OutcomeUnknown), now, payload); err != nil {
			return err
		}
		update := tx.SQL(
			`UPDATE adapter_effects SET outcome_audit_id=?,outcome='unknown',finding='crash_window',finished_at=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND target_id=? AND outcome IS NULL`,
		)
		rowsAffected, err := tx.Exec(ctx, update, outcomeID, stamp, effect.id, claimed.OrgID, claimed.ProjectID, claimed.EnvironmentID, claimed.TargetID)
		if err != nil {
			return err
		}
		if rowsAffected != 1 {
			return errors.New("store: indeterminate adapter effect was not closed exactly once")
		}
		release := tx.SQL(
			`UPDATE adapter_targets SET provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND provider_lease_effect_id=?`,
		)
		if _, err := tx.Exec(ctx, release, claimed.TargetID, claimed.OrgID, claimed.ProjectID, claimed.EnvironmentID, effect.id); err != nil {
			return err
		}
	}
	return nil
}

type adapterJournal struct {
	runtime *AdapterRuntime
	job     adapter.Job
	mu      sync.Mutex
	effects map[string]string
}

func (r *AdapterRuntime) Journal(job adapter.Job) adapter.Journal {
	return &adapterJournal{runtime: r, job: job, effects: make(map[string]string)}
}

func (j *adapterJournal) Gate(ctx context.Context, effect adapter.Effect) error {
	if j.runtime.authorize == nil {
		return adapter.ErrUnauthorized
	}
	if err := j.runtime.authorize(ctx, j.job, effect); err != nil {
		return fmt.Errorf("%w", adapter.ErrUnauthorized)
	}
	return dbRead(ctx, j.runtime.db, func(db adapterDB) error {
		query := db.SQL(
			`SELECT COUNT(*) FROM adapter_targets t JOIN adapter_outbox j ON j.target_id=t.id AND j.org_id=t.org_id AND j.project_id=t.project_id AND j.environment_id=t.environment_id WHERE t.id=? AND t.org_id=? AND t.project_id=? AND t.environment_id=? AND t.generation=? AND j.id=? AND j.state='running' AND j.lease_owner=? AND j.lease_expires_at>?`,
		)
		var count int
		now := db.Stamp(time.Now().UTC())
		if err := db.QueryRow(ctx, query, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.Generation, j.job.ID, j.job.LeaseOwner, now).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return adapter.ErrSuperseded
		}
		return nil
	})
}

func adapterEffectKey(effect adapter.Effect) string {
	return string(effect.Surface) + "\x00" + strings.ToUpper(effect.EffectiveName)
}

func newAdapterID(prefix string) string { return prefix + "_" + uuid.Must(uuid.NewV7()).String() }

func (j *adapterJournal) Reserve(ctx context.Context, effect adapter.Effect) (adapter.LedgerState, error) {
	state := adapter.Reserved
	err := j.runtime.transaction(ctx, func(tx adapterDBTX) error {
		pendingQuery := tx.SQL(
			`SELECT COUNT(*) FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id JOIN adapter_route_move_claims c ON c.provider_origin=a.origin AND c.destination_kind=t.destination_kind AND c.destination_owner=t.destination_owner AND c.destination_name=t.destination_name AND c.destination_environment=t.destination_environment WHERE t.id=? AND t.org_id=? AND t.project_id=? AND t.environment_id=? AND c.target_id<>t.id AND c.surface=? AND c.normalized_name=?`,
		)
		var pending int
		normalized := strings.ToUpper(effect.EffectiveName)
		if err := tx.QueryRow(ctx, pendingQuery, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, string(effect.Surface), normalized).Scan(&pending); err != nil {
			return err
		}
		if pending != 0 {
			return adapter.ErrConflict
		}
		selectQuery := tx.SQL(
			`SELECT state FROM adapter_ledger WHERE org_id=? AND project_id=? AND environment_id=? AND target_id=? AND surface=? AND normalized_name=?`,
		)
		var raw string
		err := tx.QueryRow(ctx, selectQuery, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), normalized).Scan(&raw)
		if err == nil {
			if adapter.LedgerState(raw) == adapter.Released {
				var origin, destinationKind string
				var destinationID, repositoryID int64
				currentRoute := tx.SQL(
					`SELECT a.origin,t.destination_kind,t.destination_id,t.repository_id FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.id=? AND t.org_id=? AND t.project_id=? AND t.environment_id=?`,
				)
				if err := tx.QueryRow(ctx, currentRoute, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID).Scan(&origin, &destinationKind, &destinationID, &repositoryID); err != nil {
					return err
				}
				reactivate := tx.SQLPerEngine(
					`UPDATE adapter_ledger SET state='reserved',missing=0,effective_name=?,provider_origin=?,destination_kind=?,repository_id=?,destination_id=?,updated_at=? WHERE org_id=? AND project_id=? AND environment_id=? AND target_id=? AND surface=? AND normalized_name=? AND state='released'`,
					`UPDATE adapter_ledger SET state='reserved',missing=false,effective_name=$1,provider_origin=$2,destination_kind=$3,repository_id=$4,destination_id=$5,updated_at=$6 WHERE org_id=$7 AND project_id=$8 AND environment_id=$9 AND target_id=$10 AND surface=$11 AND normalized_name=$12 AND state='released'`)
				rows, updateErr := tx.Exec(ctx, reactivate, effect.EffectiveName, origin, destinationKind, repositoryID, destinationID, tx.Stamp(time.Now()), j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), normalized)
				if constraint(updateErr) != nil {
					return adapter.ErrConflict
				}
				if updateErr != nil {
					return updateErr
				}
				if rows != 1 {
					return adapter.ErrSuperseded
				}
				state = adapter.Reserved
				return nil
			}
			state = adapter.LedgerState(raw)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		countQuery := tx.SQL(
			`SELECT COUNT(*) FROM adapter_ledger WHERE target_id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'released'`,
		)
		var ledgerRows int
		if err := tx.QueryRow(ctx, countQuery, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID).Scan(&ledgerRows); err != nil {
			return err
		}
		if ledgerRows >= 10_000 {
			return adapter.ErrLedgerFull
		}
		var origin, destinationKind string
		var destinationID, repositoryID int64
		lookup := tx.SQL(
			`SELECT a.origin,t.destination_kind,t.destination_id,t.repository_id FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.id=? AND t.org_id=? AND t.project_id=? AND t.environment_id=?`,
		)
		if err := tx.QueryRow(ctx, lookup, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID).Scan(&origin, &destinationKind, &destinationID, &repositoryID); err != nil {
			return err
		}
		insert := tx.SQL(
			`INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,surface,effective_name,normalized_name,state,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		)
		_, err = tx.Exec(ctx, insert, newAdapterID("led"), j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, origin, destinationKind, repositoryID, destinationID, string(effect.Surface), effect.EffectiveName, normalized, string(adapter.Reserved), tx.Stamp(time.Now()))
		if constraint(err) != nil {
			return adapter.ErrConflict
		}
		return nil
	})
	return state, err
}

func (j *adapterJournal) Prepare(ctx context.Context, effect adapter.Effect, prior adapter.LedgerState) error {
	effectID := newAdapterID("aef")
	intentID := newAdapterID("aud")
	now := time.Now().UTC()
	err := j.runtime.transaction(ctx, func(tx adapterDBTX) error {
		providerLease := tx.SQL(
			`UPDATE adapter_targets SET provider_lease_job_id=?,provider_lease_effect_id=?,provider_lease_expires_at=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=?)`,
		)
		leaseUntil := tx.Stamp(now.Add(adapter.LeaseTime))
		nowStamp := tx.Stamp(now)
		rows, err := tx.Exec(ctx, providerLease, j.job.ID, effectID, leaseUntil, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.Generation, nowStamp)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrProviderBusy
		}
		lease := tx.SQL(
			`UPDATE adapter_outbox SET lease_expires_at=? WHERE id=? AND state='running' AND lease_owner=?`,
		)
		rows, err = tx.Exec(ctx, lease, leaseUntil, j.job.ID, j.job.LeaseOwner)
		if err != nil || rows != 1 {
			return adapter.ErrSuperseded
		}
		if prior == adapter.Reserved {
			update := tx.SQL(
				`UPDATE adapter_ledger SET state='dispatched',updated_at=? WHERE org_id=? AND project_id=? AND environment_id=? AND target_id=? AND surface=? AND normalized_name=? AND state='reserved'`,
			)
			rows, err := tx.Exec(ctx, update, tx.Stamp(now), j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), strings.ToUpper(effect.EffectiveName))
			if err != nil || rows != 1 {
				return adapter.ErrSuperseded
			}
		}
		payload, _ := json.Marshal(map[string]string{"surface": string(effect.Surface), "effective_name": effect.EffectiveName, "disposition": string(effect.Disposition)})
		if err := j.insertAudit(ctx, tx, intentID, "adapter.push_intent", "intent", now, payload); err != nil {
			return err
		}
		insert := tx.SQL(
			`INSERT INTO adapter_effects (id,org_id,project_id,environment_id,target_id,job_id,surface,effective_name,disposition,intent_audit_id,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		)
		_, err = tx.Exec(ctx, insert, effectID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, j.job.ID, string(effect.Surface), effect.EffectiveName, string(effect.Disposition), intentID, tx.Stamp(now))
		return err
	})
	if err == nil {
		j.mu.Lock()
		j.effects[adapterEffectKey(effect)] = effectID
		j.mu.Unlock()
	}
	return err
}

func (j *adapterJournal) Finish(ctx context.Context, effect adapter.Effect, completion adapter.Completion) error {
	j.mu.Lock()
	effectID := j.effects[adapterEffectKey(effect)]
	j.mu.Unlock()
	if effectID == "" {
		return errors.New("store: adapter effect has no durable INTENT")
	}
	if err := adapter.ValidateCompletion(completion); err != nil {
		return err
	}
	outcomeID := newAdapterID("aud")
	now := time.Now().UTC()
	err := j.runtime.transaction(ctx, func(tx adapterDBTX) error {
		payloadJSON, err := json.Marshal(adapterPushOutcomePayload{
			Surface: string(effect.Surface), EffectiveName: effect.EffectiveName, Disposition: string(effect.Disposition),
			Finding: completion.Finding, OwnedMissing: completion.Missing, ProviderStatus: completion.ProviderStatus,
		})
		if err != nil {
			return err
		}
		if err := j.insertAudit(ctx, tx, outcomeID, "adapter.push_outcome", string(completion.Outcome), now, payloadJSON); err != nil {
			return err
		}
		updateEffect := tx.SQL(
			`UPDATE adapter_effects SET outcome_audit_id=?,outcome=?,finding=?,finished_at=? WHERE id=? AND outcome IS NULL`,
		)
		rows, err := tx.Exec(ctx, updateEffect, outcomeID, string(completion.Outcome), completion.Finding, tx.Stamp(now), effectID)
		if err != nil || rows != 1 {
			return errors.New("store: adapter effect OUTCOME was not recorded exactly once")
		}
		if completion.Conflict {
			if err := j.insertConflict(ctx, tx, effect, now); err != nil {
				return err
			}
		}
		if completion.ReleaseLedger {
			remove := tx.SQL(
				`DELETE FROM adapter_ledger WHERE org_id=? AND project_id=? AND environment_id=? AND target_id=? AND surface=? AND normalized_name=?`,
			)
			rows, err = tx.Exec(ctx, remove, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), strings.ToUpper(effect.EffectiveName))
		} else {
			update := tx.SQL(
				`UPDATE adapter_ledger SET state=CASE WHEN state='released' THEN 'released' ELSE ? END,missing=?,updated_at=? WHERE org_id=? AND project_id=? AND environment_id=? AND target_id=? AND surface=? AND normalized_name=? AND NOT (state='released' AND ?)`,
			)
			rows, err = tx.Exec(ctx, update, string(completion.State), completion.Missing, tx.Stamp(now), j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), strings.ToUpper(effect.EffectiveName), completion.Missing)
		}
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		if completion.Outcome == adapter.OutcomeSuccess && effect.KeyID != "" && effect.Disposition != adapter.Delete {
			payload, _ := json.Marshal(map[string]string{"key_id": effect.KeyID, "surface": string(effect.Surface), "effective_name": effect.EffectiveName})
			if err := j.insertAudit(ctx, tx, newAdapterID("aud"), "adapter.key_delivered", "success", now, payload); err != nil {
				return err
			}
		}
		releaseLease := tx.SQL(
			`UPDATE adapter_targets SET provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND provider_lease_job_id=? AND provider_lease_effect_id=?`,
		)
		rows, err = tx.Exec(ctx, releaseLease, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.ID, effectID)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		return nil
	})
	if err == nil {
		j.mu.Lock()
		delete(j.effects, adapterEffectKey(effect))
		j.mu.Unlock()
	}
	return err
}

func (j *adapterJournal) Refuse(ctx context.Context, effect adapter.Effect) error {
	return j.runtime.transaction(ctx, func(tx adapterDBTX) error {
		now := time.Now().UTC()
		if err := j.insertConflict(ctx, tx, effect, now); err != nil {
			return err
		}
		remove := tx.SQL(
			`DELETE FROM adapter_ledger WHERE org_id=? AND project_id=? AND environment_id=? AND target_id=? AND surface=? AND normalized_name=? AND state='reserved'`,
		)
		rows, err := tx.Exec(ctx, remove, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), strings.ToUpper(effect.EffectiveName))
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		return nil
	})
}

func (j *adapterJournal) ReleaseReservation(ctx context.Context, effect adapter.Effect) error {
	return j.runtime.transaction(ctx, func(tx adapterDBTX) error {
		remove := tx.SQL(
			`DELETE FROM adapter_ledger WHERE org_id=? AND project_id=? AND environment_id=? AND target_id=? AND surface=? AND normalized_name=? AND state='reserved' AND EXISTS (SELECT 1 FROM adapter_targets t JOIN adapter_outbox o ON o.target_id=t.id AND o.org_id=t.org_id AND o.project_id=t.project_id AND o.environment_id=t.environment_id WHERE t.id=? AND t.org_id=? AND t.project_id=? AND t.environment_id=? AND t.generation=? AND o.id=? AND o.generation=t.generation AND o.state='running' AND o.lease_owner=? AND o.lease_expires_at>?)`,
		)
		now := tx.Stamp(time.Now().UTC())
		rows, err := tx.Exec(ctx, remove,
			j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID,
			string(effect.Surface), strings.ToUpper(effect.EffectiveName),
			j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID,
			j.job.Generation, j.job.ID, j.job.LeaseOwner, now)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		return nil
	})
}

func (j *adapterJournal) insertConflict(ctx context.Context, tx adapterDBTX, effect adapter.Effect, now time.Time) error {
	insert := tx.SQL(
		`INSERT INTO adapter_conflicts (id,artifact_id,org_id,project_id,environment_id,target_id,job_id,destination_id,repository_id,target_generation,surface,effective_name,created_at) SELECT ?,?,?,?,?,?,?,destination_id,repository_id,?,?,?,? FROM adapter_targets WHERE id=? AND org_id=? AND project_id=? AND environment_id=?`,
	)
	artifactID := newAdapterID("acf")
	rows, err := tx.Exec(ctx, insert, newAdapterID("acn"), artifactID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, j.job.ID, j.job.Generation, string(effect.Surface), effect.EffectiveName, tx.Stamp(now), j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	// An unowned name in the way is drift only an operator can settle, by
	// adopting it or renaming the key; the flag clears on the next success.
	return raiseDriftAttention(ctx, tx, adapterJobScope(j.job), j.job.TargetID, j.job.EnvironmentID)
}

func (j *adapterJournal) insertAudit(ctx context.Context, tx adapterDBTX, id, typ, outcome string, at time.Time, payload []byte) error {
	return j.runtime.insertAdapterJobAuditWithID(ctx, tx, j.job, id, typ, outcome, at, payload)
}

func (r *AdapterRuntime) insertAdapterJobAudit(ctx context.Context, tx adapterDBTX, job adapter.Job, typ, outcome string, at time.Time, payload []byte) error {
	return r.insertAdapterJobAuditWithID(ctx, tx, job, newAdapterID("aud"), typ, outcome, at, payload)
}

func (r *AdapterRuntime) insertAdapterJobAuditWithID(ctx context.Context, tx adapterDBTX, job adapter.Job, id, typ, outcome string, at time.Time, payload []byte) error {
	query := tx.SQLPerEngine(
		`INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES (?,?,1,?,0,?,NULL,'system',?,'env',?,?,?,'adapter-target',?,?,?,'adapter-job',?)`,
		`INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES ($1,$2,1,$3,false,$4,NULL,'system',$5,'env',$6,$7,$8,'adapter-target',$9,$10,$11,'adapter-job',$12)`)
	stamp := tx.Stamp(at)
	_, err := tx.Exec(ctx, query, id, typ, stamp, stamp, job.AuthorityPrincipal, job.OrgID, job.ProjectID, job.EnvironmentID, job.TargetID, outcome, job.ID, string(payload))
	return err
}

func (r *AdapterRuntime) Retry(ctx context.Context, job adapter.Job, due time.Time, revision int64, failed []adapter.Change, warnings []string, cause error) error {
	return r.finishJob(ctx, job, "queued", due, time.Time{}, "failed", revision, failed, warnings, cause)
}
func (r *AdapterRuntime) Fail(ctx context.Context, job adapter.Job, revision int64, at time.Time, cause error) error {
	return r.finishJob(ctx, job, "failed", time.Time{}, at, "failed", revision, nil, nil, cause)
}
func (r *AdapterRuntime) Succeed(ctx context.Context, job adapter.Job, revision int64, warnings []string, at time.Time) error {
	return r.finishJob(ctx, job, "succeeded", time.Time{}, at, "converged", revision, nil, warnings, nil)
}

// Activate commits a tested pending target route and installs its first
// converge goal atomically. Until this transaction commits, target continues
// to name old route and remains blocked in moving state.
func (r *AdapterRuntime) Activate(ctx context.Context, job adapter.Job, connection adapter.Connection, at time.Time) error {
	if job.Kind != adapter.Activate || job.RouteMoveID == "" || connection.DestinationID <= 0 || connection.Version == "" {
		return fmt.Errorf("%w: incomplete adapter route activation", domain.ErrInvalid)
	}
	return r.transaction(ctx, func(tx adapterDBTX) error {
		finish := tx.SQL(
			`UPDATE adapter_outbox SET state='succeeded',finished_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND route_move_id=? AND target_id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND kind='activate' AND state='running' AND lease_owner=?`,
		)
		stamp := tx.Stamp(at)
		rows, err := tx.Exec(ctx, finish, stamp, job.ID, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation, job.LeaseOwner)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		var adapterID, currentEnvironment, pendingEnvironment, kind, owner, name, destinationEnvironment, visibility, prefix, moveKind, pendingOrigin string
		var selectedRaw []byte
		lookup := tx.SQLPerEngine(
			`SELECT t.adapter_id,t.environment_id,mt.environment_id,mt.destination_kind,mt.destination_owner,mt.destination_name,mt.destination_environment,mt.visibility,mt.selected_repository_ids,mt.name_prefix,m.kind,COALESCE(m.pending_origin,a.origin) FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id JOIN adapter_route_moves m ON m.id=? AND m.org_id=t.org_id AND m.project_id=t.project_id AND m.adapter_id=t.adapter_id JOIN adapter_route_move_targets mt ON mt.move_id=m.id AND mt.target_id=t.id AND mt.org_id=t.org_id AND mt.project_id=t.project_id WHERE t.id=? AND t.org_id=? AND t.project_id=? AND t.environment_id=? AND t.generation=? AND t.state='moving' AND m.state='activating' AND (m.kind='origin' OR (m.kind='target' AND m.target_id=t.id))`,
			`SELECT t.adapter_id,t.environment_id,mt.environment_id,mt.destination_kind,mt.destination_owner,mt.destination_name,mt.destination_environment,mt.visibility,mt.selected_repository_ids,mt.name_prefix,m.kind,COALESCE(m.pending_origin,a.origin) FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id JOIN adapter_route_moves m ON m.id=$1 AND m.org_id=t.org_id AND m.project_id=t.project_id AND m.adapter_id=t.adapter_id JOIN adapter_route_move_targets mt ON mt.move_id=m.id AND mt.target_id=t.id AND mt.org_id=t.org_id AND mt.project_id=t.project_id WHERE t.id=$2 AND t.org_id=$3 AND t.project_id=$4 AND t.environment_id=$5 AND t.generation=$6 AND t.state='moving' AND m.state='activating' AND (m.kind='origin' OR (m.kind='target' AND m.target_id=t.id)) FOR UPDATE OF t,a,m,mt`)
		if err := tx.QueryRow(ctx, lookup, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation).Scan(&adapterID, &currentEnvironment, &pendingEnvironment, &kind, &owner, &name, &destinationEnvironment, &visibility, &selectedRaw, &prefix, &moveKind, &pendingOrigin); err != nil {
			if isNoRows(err) {
				return adapter.ErrSuperseded
			}
			return err
		}
		if pendingEnvironment != currentEnvironment {
			return fmt.Errorf("%w: target environment move requires a replacement identity", domain.ErrConflict)
		}
		collisionQuery := tx.SQL(
			`SELECT (SELECT COUNT(*) FROM adapter_route_move_claims c JOIN adapter_targets other ON other.org_id=c.org_id AND other.project_id=c.project_id AND other.id<>c.target_id AND other.state='active' JOIN adapters oa ON oa.id=other.adapter_id AND oa.org_id=other.org_id AND oa.project_id=other.project_id LEFT JOIN adapter_target_keys tk ON tk.target_id=other.id AND tk.org_id=other.org_id AND tk.project_id=other.project_id AND tk.environment_id=other.environment_id LEFT JOIN keys k ON k.id=tk.key_id AND k.org_id=tk.org_id AND k.project_id=tk.project_id WHERE c.move_id=? AND c.target_id=? AND oa.origin=? AND other.destination_kind=? AND other.repository_id=? AND other.destination_id=? AND (c.effective_name=other.name_prefix||? OR c.effective_name=other.name_prefix||k.name))+(SELECT COUNT(*) FROM adapter_route_move_claims c JOIN adapter_ledger l ON l.provider_origin=? AND l.destination_kind=? AND l.repository_id=? AND l.destination_id=? AND l.surface=c.surface AND l.normalized_name=c.normalized_name AND l.state<>'released' AND l.target_id<>c.target_id WHERE c.move_id=? AND c.target_id=?)`,
		)
		var collisions int
		if err := tx.QueryRow(ctx, collisionQuery, job.RouteMoveID, job.TargetID, pendingOrigin, kind, connection.RepositoryID, connection.DestinationID, adapter.SentinelName, pendingOrigin, kind, connection.RepositoryID, connection.DestinationID, job.RouteMoveID, job.TargetID).Scan(&collisions); err != nil {
			return err
		}
		if collisions != 0 {
			return fmt.Errorf("%w: pending effective names collide on the resolved destination", adapter.ErrConflict)
		}
		setResolved := tx.SQL(
			`UPDATE adapter_route_move_targets SET destination_id=?,repository_id=? WHERE move_id=? AND target_id=? AND org_id=? AND project_id=? AND environment_id=? AND destination_id=0`,
		)
		rows, err = tx.Exec(ctx, setResolved, connection.DestinationID, connection.RepositoryID, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, pendingEnvironment)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		if moveKind == "origin" {
			return r.activateOriginRouteMove(ctx, tx, job, connection, adapterID, stamp, at)
		}
		if moveKind != "target" {
			return fmt.Errorf("%w: unsupported adapter route move kind", domain.ErrInvalid)
		}
		deleteKeys := tx.SQL(
			`DELETE FROM adapter_target_keys WHERE target_id=? AND org_id=? AND project_id=? AND environment_id=?`,
		)
		if _, err := tx.Exec(ctx, deleteKeys, job.TargetID, job.OrgID, job.ProjectID, currentEnvironment); err != nil {
			return err
		}
		insertKeys := tx.SQL(
			`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) SELECT k.org_id,k.project_id,k.environment_id,k.target_id,?,k.key_id FROM adapter_route_move_keys k WHERE k.move_id=? AND k.target_id=? AND k.org_id=? AND k.project_id=? AND k.environment_id=?`,
		)
		inserted, err := tx.Exec(ctx, insertKeys, adapterID, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, pendingEnvironment)
		if err != nil {
			return err
		}
		if inserted == 0 {
			return ErrConflict
		}
		convergeID := newAdapterID("job")
		generation := job.Generation + 1
		insertJob := tx.SQL(
			`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (?,?,?,?,?,'converge',?,?,?,?,0,?,'queued',?)`,
		)
		rows, err = tx.Exec(ctx, insertJob, convergeID, job.OrgID, job.ProjectID, pendingEnvironment, job.TargetID, job.RouteMoveID, job.AuthorityPrincipal, generation, job.TargetID, stamp, stamp)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrConflict
		}
		applyTarget := tx.SQLPerEngine(
			`UPDATE adapter_targets SET destination_kind=?,destination_owner=?,destination_name=?,destination_environment=?,destination_id=?,repository_id=?,visibility=?,selected_repository_ids=?,name_prefix=?,generation=?,state='active',sync_status='converging',failure_names='[]',active_job_id=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND state='moving' AND provider_lease_job_id IS NULL`,
			`UPDATE adapter_targets SET destination_kind=$1,destination_owner=$2,destination_name=$3,destination_environment=$4,destination_id=$5,repository_id=$6,visibility=$7,selected_repository_ids=$8,name_prefix=$9,generation=$10,state='active',sync_status='converging',failure_names='[]'::jsonb,active_job_id=$11 WHERE id=$12 AND org_id=$13 AND project_id=$14 AND environment_id=$15 AND generation=$16 AND state='moving' AND provider_lease_job_id IS NULL`)
		rows, err = tx.Exec(ctx, applyTarget, kind, owner, name, destinationEnvironment, connection.DestinationID, connection.RepositoryID, visibility, selectedRaw, prefix, generation, convergeID, job.TargetID, job.OrgID, job.ProjectID, currentEnvironment, job.Generation)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		if !connection.CredentialExpiresAt.IsZero() {
			updateExpiry := tx.SQL(
				`UPDATE adapters SET credential_expires_at=? WHERE id=? AND org_id=? AND project_id=? AND state='active'`,
			)
			rows, err = tx.Exec(ctx, updateExpiry, tx.Stamp(connection.CredentialExpiresAt), adapterID, job.OrgID, job.ProjectID)
			if err != nil {
				return err
			}
			if rows != 1 {
				return adapter.ErrSuperseded
			}
		}
		deleteClaims := tx.SQL(`DELETE FROM adapter_route_move_claims WHERE move_id=? AND org_id=? AND project_id=?`)
		if _, err := tx.Exec(ctx, deleteClaims, job.RouteMoveID, job.OrgID, job.ProjectID); err != nil {
			return err
		}
		completeMove := tx.SQL(
			`UPDATE adapter_route_moves SET state='completed' WHERE id=? AND org_id=? AND project_id=? AND target_id=? AND state='activating'`,
		)
		rows, err = tx.Exec(ctx, completeMove, job.RouteMoveID, job.OrgID, job.ProjectID, job.TargetID)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		payload, err := json.Marshal(adapterTestPayload{Version: connection.Version, DestinationID: connection.DestinationID})
		if err != nil {
			return err
		}
		return r.insertAdapterJobAudit(ctx, tx, job, "adapter.test", "success", at, payload)
	})
}

func (r *AdapterRuntime) activateOriginRouteMove(ctx context.Context, tx adapterDBTX, job adapter.Job, connection adapter.Connection, adapterID string, stamp any, at time.Time) error {
	markProbe := tx.SQL(
		`UPDATE adapter_targets SET active_job_id=NULL WHERE id=? AND adapter_id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND state='moving' AND active_job_id=? AND provider_lease_job_id IS NULL`,
	)
	rows, err := tx.Exec(ctx, markProbe, job.TargetID, adapterID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation, job.ID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	payload, err := json.Marshal(adapterTestPayload{Version: connection.Version, DestinationID: connection.DestinationID})
	if err != nil {
		return err
	}
	if err := r.insertAdapterJobAudit(ctx, tx, job, "adapter.test", "success", at, payload); err != nil {
		return err
	}
	var unresolved int
	countUnresolved := tx.SQL(
		`SELECT COUNT(*) FROM adapter_route_move_targets WHERE move_id=? AND org_id=? AND project_id=? AND destination_id=0`,
	)
	if err := tx.QueryRow(ctx, countUnresolved, job.RouteMoveID, job.OrgID, job.ProjectID).Scan(&unresolved); err != nil {
		return err
	}
	if unresolved != 0 {
		return nil
	}
	var pendingOrigin string
	var pendingCredential []byte
	loadPending := tx.SQLPerEngine(
		`SELECT pending_origin,pending_credential_ciphertext FROM adapter_route_moves WHERE id=? AND org_id=? AND project_id=? AND adapter_id=? AND kind='origin' AND target_id IS NULL AND state='activating'`,
		`SELECT pending_origin,pending_credential_ciphertext FROM adapter_route_moves WHERE id=$1 AND org_id=$2 AND project_id=$3 AND adapter_id=$4 AND kind='origin' AND target_id IS NULL AND state='activating' FOR UPDATE`)
	if err := tx.QueryRow(ctx, loadPending, job.RouteMoveID, job.OrgID, job.ProjectID, adapterID).Scan(&pendingOrigin, &pendingCredential); err != nil {
		if isNoRows(err) {
			return adapter.ErrSuperseded
		}
		return err
	}
	if pendingOrigin == "" || len(pendingCredential) == 0 {
		return fmt.Errorf("%w: origin move lost pending provider custody", adapter.ErrSuperseded)
	}
	targetQuery := tx.SQLPerEngine(
		`SELECT t.id,t.environment_id,t.generation,mt.destination_kind,mt.destination_owner,mt.destination_name,mt.destination_environment,mt.destination_id,mt.repository_id,mt.visibility,mt.selected_repository_ids,mt.name_prefix FROM adapter_route_move_targets mt JOIN adapter_targets t ON t.id=mt.target_id AND t.org_id=mt.org_id AND t.project_id=mt.project_id AND t.environment_id=mt.environment_id WHERE mt.move_id=? AND mt.org_id=? AND mt.project_id=? AND t.adapter_id=? AND t.state='moving' AND t.active_job_id IS NULL AND t.provider_lease_job_id IS NULL ORDER BY t.id`,
		`SELECT t.id,t.environment_id,t.generation,mt.destination_kind,mt.destination_owner,mt.destination_name,mt.destination_environment,mt.destination_id,mt.repository_id,mt.visibility,mt.selected_repository_ids,mt.name_prefix FROM adapter_route_move_targets mt JOIN adapter_targets t ON t.id=mt.target_id AND t.org_id=mt.org_id AND t.project_id=mt.project_id AND t.environment_id=mt.environment_id WHERE mt.move_id=$1 AND mt.org_id=$2 AND mt.project_id=$3 AND t.adapter_id=$4 AND t.state='moving' AND t.active_job_id IS NULL AND t.provider_lease_job_id IS NULL ORDER BY t.id FOR UPDATE OF t,mt`)
	targetRows, err := tx.Query(ctx, targetQuery, job.RouteMoveID, job.OrgID, job.ProjectID, adapterID)
	if err != nil {
		return err
	}
	type activatedTarget struct {
		id, environment, kind, owner, name, destinationEnvironment, visibility, prefix string
		generation, destinationID, repositoryID                                        int64
		selectedRaw                                                                    []byte
	}
	var targets []activatedTarget
	for targetRows.Next() {
		var target activatedTarget
		if err := targetRows.Scan(&target.id, &target.environment, &target.generation, &target.kind, &target.owner, &target.name, &target.destinationEnvironment, &target.destinationID, &target.repositoryID, &target.visibility, &target.selectedRaw, &target.prefix); err != nil {
			_ = closeAdapterRows(targetRows)
			return err
		}
		if target.destinationID <= 0 {
			_ = closeAdapterRows(targetRows)
			return adapter.ErrSuperseded
		}
		targets = append(targets, target)
	}
	if err := targetRows.Err(); err != nil {
		_ = closeAdapterRows(targetRows)
		return err
	}
	_ = closeAdapterRows(targetRows)
	if len(targets) == 0 {
		return adapter.ErrSuperseded
	}
	for _, target := range targets {
		deleteKeys := tx.SQL(
			`DELETE FROM adapter_target_keys WHERE target_id=? AND org_id=? AND project_id=? AND environment_id=?`,
		)
		if _, err := tx.Exec(ctx, deleteKeys, target.id, job.OrgID, job.ProjectID, target.environment); err != nil {
			return err
		}
		insertKeys := tx.SQL(
			`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) SELECT k.org_id,k.project_id,k.environment_id,k.target_id,?,k.key_id FROM adapter_route_move_keys k WHERE k.move_id=? AND k.target_id=? AND k.org_id=? AND k.project_id=? AND k.environment_id=?`,
		)
		inserted, err := tx.Exec(ctx, insertKeys, adapterID, job.RouteMoveID, target.id, job.OrgID, job.ProjectID, target.environment)
		if err != nil {
			return err
		}
		if inserted == 0 {
			return ErrConflict
		}
		convergeID := newAdapterID("job")
		generation := target.generation + 1
		insertJob := tx.SQL(
			`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (?,?,?,?,?,'converge',?,?,?,?,0,?,'queued',?)`,
		)
		rows, err = tx.Exec(ctx, insertJob, convergeID, job.OrgID, job.ProjectID, target.environment, target.id, job.RouteMoveID, job.AuthorityPrincipal, generation, target.id, stamp, stamp)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrConflict
		}
		applyTarget := tx.SQLPerEngine(
			`UPDATE adapter_targets SET destination_kind=?,destination_owner=?,destination_name=?,destination_environment=?,destination_id=?,repository_id=?,visibility=?,selected_repository_ids=?,name_prefix=?,generation=?,state='active',sync_status='converging',failure_names='[]',active_job_id=? WHERE id=? AND adapter_id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND state='moving' AND active_job_id IS NULL AND provider_lease_job_id IS NULL`,
			`UPDATE adapter_targets SET destination_kind=$1,destination_owner=$2,destination_name=$3,destination_environment=$4,destination_id=$5,repository_id=$6,visibility=$7,selected_repository_ids=$8,name_prefix=$9,generation=$10,state='active',sync_status='converging',failure_names='[]'::jsonb,active_job_id=$11 WHERE id=$12 AND adapter_id=$13 AND org_id=$14 AND project_id=$15 AND environment_id=$16 AND generation=$17 AND state='moving' AND active_job_id IS NULL AND provider_lease_job_id IS NULL`)
		rows, err = tx.Exec(ctx, applyTarget, target.kind, target.owner, target.name, target.destinationEnvironment, target.destinationID, target.repositoryID, target.visibility, target.selectedRaw, target.prefix, generation, convergeID, target.id, adapterID, job.OrgID, job.ProjectID, target.environment, target.generation)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
	}
	activateAdapter := tx.SQL(
		`UPDATE adapters SET origin=?,credential_ciphertext=?,credential_set_at=?,credential_expires_at=?,state='active' WHERE id=? AND org_id=? AND project_id=? AND state='moving'`,
	)
	var expires any
	if !connection.CredentialExpiresAt.IsZero() {
		expires = tx.Stamp(connection.CredentialExpiresAt)
	}
	rows, err = tx.Exec(ctx, activateAdapter, pendingOrigin, pendingCredential, stamp, expires, adapterID, job.OrgID, job.ProjectID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	deleteClaims := tx.SQL(`DELETE FROM adapter_route_move_claims WHERE move_id=? AND org_id=? AND project_id=?`)
	if _, err := tx.Exec(ctx, deleteClaims, job.RouteMoveID, job.OrgID, job.ProjectID); err != nil {
		return err
	}
	completeMove := tx.SQL(
		`UPDATE adapter_route_moves SET state='completed',pending_origin=NULL,pending_credential_ciphertext=NULL WHERE id=? AND org_id=? AND project_id=? AND adapter_id=? AND kind='origin' AND state='activating'`,
	)
	rows, err = tx.Exec(ctx, completeMove, job.RouteMoveID, job.OrgID, job.ProjectID, adapterID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	return nil
}

func (r *AdapterRuntime) finishJob(ctx context.Context, job adapter.Job, state string, due, finished time.Time, targetStatus string, revision int64, failed []adapter.Change, warnings []string, terminalErr error) error {
	return r.transaction(ctx, func(tx adapterDBTX) error {
		if state == "queued" {
			query := tx.SQL(`UPDATE adapter_outbox SET state='queued',next_attempt_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND lease_owner=?`)
			rows, err := tx.Exec(ctx, query, tx.Stamp(due), job.ID, job.LeaseOwner)
			if err != nil {
				return err
			}
			if rows != 1 {
				return adapter.ErrSuperseded
			}
		} else {
			query := tx.SQL(`UPDATE adapter_outbox SET state=?,finished_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND lease_owner=?`)
			rows, err := tx.Exec(ctx, query, state, tx.Stamp(finished), job.ID, job.LeaseOwner)
			if err != nil {
				return err
			}
			if rows != 1 {
				return adapter.ErrSuperseded
			}
		}
		if state == "failed" && errors.Is(terminalErr, adapter.ErrSuperseded) {
			payload, _ := json.Marshal(map[string]string{"cause": "generation"})
			if err := r.insertAdapterJobAudit(ctx, tx, job, "adapter.abort", "failure", finished, payload); err != nil {
				return err
			}
			if job.Kind == adapter.Scrub {
				orphaned, err := adapterOrphans(ctx, tx, adapterJobScope(job), job.TargetID, job.EnvironmentID)
				if err != nil {
					return err
				}
				payload, _ := json.Marshal(map[string][]string{"orphaned": orphaned})
				return r.insertAdapterJobAudit(ctx, tx, job, "adapter.scrub", "failure", finished, payload)
			}
			return nil
		}
		if job.Kind == adapter.Scrub && state == "failed" && errors.Is(terminalErr, adapter.ErrProviderAuth) {
			return r.finishDeadCredentialScrub(ctx, tx, job, finished)
		}
		if job.Kind == adapter.Scrub && state == "succeeded" {
			if job.RouteMoveID != "" {
				return r.finishRouteMoveScrub(ctx, tx, job, finished, nil)
			}
			var adapterID string
			lookup := tx.SQL(`SELECT adapter_id FROM adapter_targets WHERE id=? AND org_id=? AND project_id=? AND environment_id=?`)
			if err := tx.QueryRow(ctx, lookup, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID).Scan(&adapterID); err != nil {
				return err
			}
			markTarget := tx.SQL(`UPDATE adapter_targets SET state='tombstoned',sync_status='converged',active_job_id=NULL WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND provider_lease_job_id IS NULL`)
			rows, err := tx.Exec(ctx, markTarget, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation)
			if err != nil {
				return err
			}
			if rows != 1 {
				return adapter.ErrSuperseded
			}
			erase := tx.SQL(
				`UPDATE adapters SET credential_ciphertext=NULL,credential_set_at=NULL WHERE id=? AND org_id=? AND project_id=? AND state='tombstoned' AND NOT EXISTS (SELECT 1 FROM adapter_targets WHERE adapter_id=? AND state<>'tombstoned') AND NOT EXISTS (SELECT 1 FROM adapter_outbox j JOIN adapter_targets t ON t.id=j.target_id AND t.org_id=j.org_id AND t.project_id=j.project_id AND t.environment_id=j.environment_id WHERE t.adapter_id=? AND j.kind='scrub' AND j.state IN ('queued','running'))`,
			)
			_, err = tx.Exec(ctx, erase, adapterID, job.OrgID, job.ProjectID, adapterID, adapterID)
			if err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string][]string{"orphaned": {}})
			return r.insertAdapterJobAudit(ctx, tx, job, "adapter.scrub", "success", finished, payload)
		}
		if job.Kind == adapter.Activate && state == "failed" && (errors.Is(terminalErr, adapter.ErrProviderAuth) || errors.Is(terminalErr, adapter.ErrConflict)) {
			attention := tx.SQL(
				`UPDATE adapter_route_moves SET state='attention_required' WHERE id=? AND org_id=? AND project_id=? AND state='activating'`,
			)
			rows, err := tx.Exec(ctx, attention, job.RouteMoveID, job.OrgID, job.ProjectID)
			if err != nil {
				return err
			}
			if rows != 1 {
				return adapter.ErrSuperseded
			}
			supersede := tx.SQL(
				`UPDATE adapter_outbox SET state='superseded',finished_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE route_move_id=? AND org_id=? AND project_id=? AND id<>? AND kind='activate' AND state IN ('queued','running')`,
			)
			if _, err := tx.Exec(ctx, supersede, tx.Stamp(finished), job.RouteMoveID, job.OrgID, job.ProjectID, job.ID); err != nil {
				return err
			}
			markTargets := tx.SQLPerEngine(
				`UPDATE adapter_targets SET sync_status='failed',failure_names='["route"]',active_job_id=NULL WHERE org_id=? AND project_id=? AND id IN (SELECT target_id FROM adapter_route_move_targets WHERE move_id=? AND org_id=? AND project_id=?) AND state='moving'`,
				`UPDATE adapter_targets SET sync_status='failed',failure_names='["route"]'::jsonb,active_job_id=NULL WHERE org_id=$1 AND project_id=$2 AND id IN (SELECT target_id FROM adapter_route_move_targets WHERE move_id=$3 AND org_id=$4 AND project_id=$5) AND state='moving'`)
			if _, err := tx.Exec(ctx, markTargets, job.OrgID, job.ProjectID, job.RouteMoveID, job.OrgID, job.ProjectID); err != nil {
				return err
			}
			return r.insertAdapterJobAudit(ctx, tx, job, "adapter.test", "failure", finished, []byte(`{}`))
		}
		activeJob := "active_job_id"
		if state != "queued" {
			activeJob = "NULL"
		}
		failureNames := make([]string, 0, len(failed))
		for _, change := range failed {
			failureNames = append(failureNames, string(change.Surface)+":"+change.EffectiveName)
		}
		failureJSON, err := json.Marshal(failureNames)
		if err != nil {
			return err
		}
		warningJSON, err := json.Marshal(append([]string{}, warnings...))
		if err != nil {
			return err
		}
		// Health columns (#157): every attempt stamps what it attempted; a
		// success clears the error class and the attention flag, a failure
		// records its bounded class and raises attention when the class says
		// only an operator can settle it.
		var errorClass any
		attention := "0"
		if targetStatus != "converged" {
			class := adapter.ClassifyError(terminalErr)
			if class != "" {
				errorClass = string(class)
			}
			attention = "drift_attention"
			if class.NeedsAttention() {
				attention = "1"
			}
		}
		// last_attempted_at is stamped at settlement. A terminal outcome
		// carries its own `finished`; the retry path settles with a zero
		// `finished` (its argument is the future due-time), so the attempt's
		// completion instant is now. This is the settlement clock the whole
		// runtime uses, not a fallback for a missing value.
		attemptedAt := finished
		if attemptedAt.IsZero() {
			attemptedAt = time.Now().UTC()
		}
		query := tx.SQLPerEngine(
			`UPDATE adapter_targets SET sync_status=?,converged_revision=CASE WHEN ?>0 THEN ? ELSE converged_revision END,failure_names=?,warnings=?,last_attempted_revision=CASE WHEN ?>0 THEN ? ELSE last_attempted_revision END,last_attempted_at=?,last_error_class=?,drift_attention=`+attention+`,active_job_id=`+activeJob+` WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND provider_lease_job_id IS NULL`,
			`UPDATE adapter_targets SET sync_status=$1,converged_revision=CASE WHEN $2>0 THEN $3 ELSE converged_revision END,failure_names=$4,warnings=$5,last_attempted_revision=CASE WHEN $6>0 THEN $7 ELSE last_attempted_revision END,last_attempted_at=$8,last_error_class=$9,drift_attention=`+attentionPG(attention)+`,active_job_id=`+activeJob+` WHERE id=$10 AND org_id=$11 AND project_id=$12 AND environment_id=$13 AND generation=$14 AND provider_lease_job_id IS NULL`)
		var rev any
		if revision > 0 {
			rev = revision
		}
		// converged_revision moves only on success; last_attempted_revision
		// moves on every attempt that got as far as loading a revision.
		var convergedRevision int64
		var convergedRev any
		if targetStatus == "converged" {
			convergedRevision, convergedRev = revision, rev
		}
		rows, err := tx.Exec(ctx, query, targetStatus, convergedRevision, convergedRev, string(failureJSON), string(warningJSON), revision, rev, tx.Stamp(attemptedAt), errorClass, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		if state == "failed" && errors.Is(terminalErr, adapter.ErrUnauthorized) {
			payload, _ := json.Marshal(map[string]string{"cause": "authority"})
			if err := r.insertAdapterJobAudit(ctx, tx, job, "adapter.abort", "failure", finished, payload); err != nil {
				return err
			}
		}
		if job.Kind == adapter.Scrub && state == "failed" {
			orphaned, err := adapterOrphans(ctx, tx, adapterJobScope(job), job.TargetID, job.EnvironmentID)
			if err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string][]string{"orphaned": orphaned})
			if err := r.insertAdapterJobAudit(ctx, tx, job, "adapter.scrub", "failure", finished, payload); err != nil {
				return err
			}
		}
		return nil
	})
}

// raiseDriftAttention flags a target as needing operator action (#157): an
// unowned name in the way, or names orphaned by a route move. One helper so
// the dual-dialect UPDATE lives in a single place. It is cleared only by the
// next successful converge (finishJob).
func raiseDriftAttention(ctx context.Context, tx adapterDBTX, chain domain.Scope, targetID, environmentID string) error {
	query := tx.SQLPerEngine(
		`UPDATE adapter_targets SET drift_attention=1 WHERE id=? AND org_id=? AND project_id=? AND environment_id=?`,
		`UPDATE adapter_targets SET drift_attention=TRUE WHERE id=$1 AND org_id=$2 AND project_id=$3 AND environment_id=$4`)
	_, err := tx.Exec(ctx, query, targetID, string(chain.Org), string(chain.Project), environmentID)
	return err
}

// attentionPG renders the drift_attention assignment for Postgres, whose
// column is BOOLEAN where sqlite's is INTEGER.
func attentionPG(sqlite string) string {
	switch sqlite {
	case "0":
		return "FALSE"
	case "1":
		return "TRUE"
	default:
		return sqlite
	}
}

func (r *AdapterRuntime) finishRouteMoveScrub(ctx context.Context, tx adapterDBTX, job adapter.Job, finished time.Time, orphaned []string) error {
	var moveKind string
	lookupMove := tx.SQLPerEngine(
		`SELECT m.kind FROM adapter_route_moves m JOIN adapter_route_move_targets mt ON mt.move_id=m.id AND mt.org_id=m.org_id AND mt.project_id=m.project_id WHERE m.id=? AND m.org_id=? AND m.project_id=? AND m.state='scrubbing' AND mt.target_id=? AND mt.environment_id=?`,
		`SELECT m.kind FROM adapter_route_moves m JOIN adapter_route_move_targets mt ON mt.move_id=m.id AND mt.org_id=m.org_id AND mt.project_id=m.project_id WHERE m.id=$1 AND m.org_id=$2 AND m.project_id=$3 AND m.state='scrubbing' AND mt.target_id=$4 AND mt.environment_id=$5 FOR UPDATE OF m,mt`)
	if err := tx.QueryRow(ctx, lookupMove, job.RouteMoveID, job.OrgID, job.ProjectID, job.TargetID, job.EnvironmentID).Scan(&moveKind); err != nil {
		if isNoRows(err) {
			return adapter.ErrSuperseded
		}
		return err
	}
	var activeLedger int
	activeLedgerQuery := tx.SQL(
		`SELECT COUNT(*) FROM adapter_ledger WHERE target_id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'released'`,
	)
	if err := tx.QueryRow(ctx, activeLedgerQuery, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID).Scan(&activeLedger); err != nil {
		return err
	}
	if activeLedger != 0 {
		return fmt.Errorf("%w: route move scrub retained active custody", adapter.ErrSuperseded)
	}
	payload, _ := json.Marshal(map[string][]string{"orphaned": orphaned})
	outcome := "success"
	if len(orphaned) != 0 {
		outcome = "failure"
		orphanJSON, _ := json.Marshal(orphaned)
		persistOrphans := tx.SQL(
			`UPDATE adapter_route_move_targets SET orphaned_names=? WHERE move_id=? AND target_id=? AND org_id=? AND project_id=? AND environment_id=?`,
		)
		rows, err := tx.Exec(ctx, persistOrphans, string(orphanJSON), job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		// Names left behind on the old route are the operator's to clean up.
		if err := raiseDriftAttention(ctx, tx, adapterJobScope(job), job.TargetID, job.EnvironmentID); err != nil {
			return err
		}
	}
	if moveKind == "origin" {
		return r.finishOriginRouteMoveScrub(ctx, tx, job, finished, orphaned, payload, outcome)
	}
	if moveKind != "target" {
		return fmt.Errorf("%w: unsupported adapter route move kind", domain.ErrInvalid)
	}
	move := tx.SQL(
		`UPDATE adapter_route_moves SET state='activating' WHERE id=? AND org_id=? AND project_id=? AND target_id=? AND state='scrubbing'`,
	)
	rows, err := tx.Exec(ctx, move, job.RouteMoveID, job.OrgID, job.ProjectID, job.TargetID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	activateID := newAdapterID("job")
	stamp := tx.Stamp(finished)
	insert := tx.SQL(
		`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (?,?,?,?,?,'activate',?,?,?,?,0,?,'queued',?)`,
	)
	rows, err = tx.Exec(ctx, insert, activateID, job.OrgID, job.ProjectID, job.EnvironmentID, job.TargetID, job.RouteMoveID, job.AuthorityPrincipal, job.Generation, job.TargetID, stamp, stamp)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConflict
	}
	failureJSON, _ := json.Marshal(orphaned)
	mark := tx.SQL(
		`UPDATE adapter_targets SET state='moving',sync_status='converging',failure_names=?,active_job_id=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND provider_lease_job_id IS NULL`,
	)
	rows, err = tx.Exec(ctx, mark, string(failureJSON), activateID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	return r.insertAdapterJobAudit(ctx, tx, job, "adapter.scrub", outcome, finished, payload)
}

func (r *AdapterRuntime) finishOriginRouteMoveScrub(ctx context.Context, tx adapterDBTX, job adapter.Job, finished time.Time, orphaned []string, payload []byte, outcome string) error {
	failureJSON, _ := json.Marshal(orphaned)
	markDone := tx.SQL(
		`UPDATE adapter_targets SET state='moving',sync_status='converging',failure_names=?,active_job_id=NULL WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND state='moving' AND active_job_id=? AND provider_lease_job_id IS NULL`,
	)
	rows, err := tx.Exec(ctx, markDone, string(failureJSON), job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation, job.ID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	if err := r.insertAdapterJobAudit(ctx, tx, job, "adapter.scrub", outcome, finished, payload); err != nil {
		return err
	}
	var pendingScrubs int
	countPending := tx.SQL(
		`SELECT COUNT(*) FROM adapter_outbox WHERE route_move_id=? AND org_id=? AND project_id=? AND kind='scrub' AND state IN ('queued','running')`,
	)
	if err := tx.QueryRow(ctx, countPending, job.RouteMoveID, job.OrgID, job.ProjectID).Scan(&pendingScrubs); err != nil {
		return err
	}
	if pendingScrubs != 0 {
		return nil
	}
	activateMove := tx.SQL(
		`UPDATE adapter_route_moves SET state='activating' WHERE id=? AND org_id=? AND project_id=? AND kind='origin' AND target_id IS NULL AND state='scrubbing'`,
	)
	rows, err = tx.Exec(ctx, activateMove, job.RouteMoveID, job.OrgID, job.ProjectID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	targetQuery := tx.SQLPerEngine(
		`SELECT t.id,t.environment_id,t.generation,m.authority_principal_id FROM adapter_route_move_targets mt JOIN adapter_route_moves m ON m.id=mt.move_id AND m.org_id=mt.org_id AND m.project_id=mt.project_id JOIN adapter_targets t ON t.id=mt.target_id AND t.org_id=mt.org_id AND t.project_id=mt.project_id AND t.environment_id=mt.environment_id WHERE mt.move_id=? AND mt.org_id=? AND mt.project_id=? AND t.state='moving' AND t.active_job_id IS NULL ORDER BY t.id`,
		`SELECT t.id,t.environment_id,t.generation,m.authority_principal_id FROM adapter_route_move_targets mt JOIN adapter_route_moves m ON m.id=mt.move_id AND m.org_id=mt.org_id AND m.project_id=mt.project_id JOIN adapter_targets t ON t.id=mt.target_id AND t.org_id=mt.org_id AND t.project_id=mt.project_id AND t.environment_id=mt.environment_id WHERE mt.move_id=$1 AND mt.org_id=$2 AND mt.project_id=$3 AND t.state='moving' AND t.active_job_id IS NULL ORDER BY t.id FOR UPDATE OF t`)
	targetRows, err := tx.Query(ctx, targetQuery, job.RouteMoveID, job.OrgID, job.ProjectID)
	if err != nil {
		return err
	}
	type activationTarget struct {
		id, environment, authority string
		generation                 int64
	}
	var targets []activationTarget
	for targetRows.Next() {
		var target activationTarget
		if err := targetRows.Scan(&target.id, &target.environment, &target.generation, &target.authority); err != nil {
			_ = closeAdapterRows(targetRows)
			return err
		}
		targets = append(targets, target)
	}
	if err := targetRows.Err(); err != nil {
		_ = closeAdapterRows(targetRows)
		return err
	}
	_ = closeAdapterRows(targetRows)
	if len(targets) == 0 {
		return adapter.ErrSuperseded
	}
	stamp := tx.Stamp(finished)
	for _, target := range targets {
		activateID := newAdapterID("job")
		insert := tx.SQL(
			`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (?,?,?,?,?,'activate',?,?,?,?,0,?,'queued',?)`,
		)
		rows, err = tx.Exec(ctx, insert, activateID, job.OrgID, job.ProjectID, target.environment, target.id, job.RouteMoveID, target.authority, target.generation, target.id, stamp, stamp)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrConflict
		}
		mark := tx.SQL(
			`UPDATE adapter_targets SET active_job_id=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND state='moving' AND active_job_id IS NULL AND provider_lease_job_id IS NULL`,
		)
		rows, err = tx.Exec(ctx, mark, activateID, target.id, job.OrgID, job.ProjectID, target.environment, target.generation)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
	}
	return nil
}

func (r *AdapterRuntime) finishDeadCredentialScrub(ctx context.Context, tx adapterDBTX, job adapter.Job, finished time.Time) error {
	orphaned, err := adapterOrphans(ctx, tx, adapterJobScope(job), job.TargetID, job.EnvironmentID)
	if err != nil {
		return err
	}
	var adapterID string
	lookup := tx.SQL(
		`SELECT adapter_id FROM adapter_targets WHERE id=? AND org_id=? AND project_id=? AND environment_id=?`,
	)
	if err := tx.QueryRow(ctx, lookup, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID).Scan(&adapterID); err != nil {
		return err
	}
	releaseLedger := tx.SQLPerEngine(
		`UPDATE adapter_ledger SET state='released',missing=0,updated_at=? WHERE target_id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'released'`,
		`UPDATE adapter_ledger SET state='released',missing=false,updated_at=$1 WHERE target_id=$2 AND org_id=$3 AND project_id=$4 AND environment_id=$5 AND state<>'released'`)
	if _, err := tx.Exec(ctx, releaseLedger, tx.Stamp(finished), job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID); err != nil {
		return err
	}
	if job.RouteMoveID != "" {
		return r.finishRouteMoveScrub(ctx, tx, job, finished, orphaned)
	}
	failureJSON, _ := json.Marshal(orphaned)
	markTarget := tx.SQL(
		`UPDATE adapter_targets SET state='tombstoned',sync_status='failed',failure_names=?,active_job_id=NULL WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND generation=? AND provider_lease_job_id IS NULL`,
	)
	rows, err := tx.Exec(ctx, markTarget, string(failureJSON), job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	erase := tx.SQL(
		`UPDATE adapters SET credential_ciphertext=NULL,credential_set_at=NULL WHERE id=? AND org_id=? AND project_id=? AND state='tombstoned' AND NOT EXISTS (SELECT 1 FROM adapter_targets WHERE adapter_id=? AND state<>'tombstoned') AND NOT EXISTS (SELECT 1 FROM adapter_outbox j JOIN adapter_targets t ON t.id=j.target_id AND t.org_id=j.org_id AND t.project_id=j.project_id AND t.environment_id=j.environment_id WHERE t.adapter_id=? AND j.kind='scrub' AND j.state IN ('queued','running'))`,
	)
	if _, err := tx.Exec(ctx, erase, adapterID, job.OrgID, job.ProjectID, adapterID, adapterID); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string][]string{"orphaned": orphaned})
	return r.insertAdapterJobAudit(ctx, tx, job, "adapter.scrub", "failure", finished, payload)
}
