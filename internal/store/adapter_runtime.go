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
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/parameters"
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
	Transport                           AdapterTransport
	Target                              adapter.Target
	Entries                             []AdapterSnapshotEntry
	Ledger                              []adapter.LedgerEntry
	Revision                            int64
}

type AdapterActivation struct {
	Provider, Origin, CredentialOwnerID string
	CredentialCiphertext                []byte
	Transport                           AdapterTransport
	Target                              adapter.Target
}

// AdapterTransport is the stored provider transport policy (GitLab only).
type AdapterTransport struct {
	SPKIPin            string
	CABundlePEM        string
	AllowPersonalToken bool
}

// Transport returns the adapter's stored transport policy.
func (r AdapterRecord) Transport() AdapterTransport {
	return AdapterTransport{SPKIPin: r.SPKIPin, CABundlePEM: r.CABundlePEM, AllowPersonalToken: r.AllowPersonalToken}
}

// Config returns the module configuration for origin under this policy.
func (t AdapterTransport) Config(origin string) adapter.Config {
	return adapter.Config{Origin: origin, SPKIPin: t.SPKIPin, CABundlePEM: t.CABundlePEM, AllowPersonalToken: t.AllowPersonalToken}
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

// CheckProviderSwitch is a host-runtime safety predicate, not a tenant data
// read or an authority to mutate adapters. Any retained configuration or
// configure fence refuses a real/development-provider switch. Target, outbox,
// ledger, route and effect rows reference these configurations, so even an
// idle or historical configuration is unsafe to redirect. The application
// must call this after draining its requests/workers and must reject HA.
func (r *AdapterRuntime) CheckProviderSwitch(ctx context.Context) error {
	if operation.IsNetwork(ctx) {
		return errors.New("provider-switch safety checks require host runtime authority")
	}
	found, err := dbReadResult(ctx, r.db, func(db adapterDB) (int64, error) {
		return db.adapterRuntimeQueries().adapterWorkerCheckProviderSwitchQuery(ctx)
	})
	if err != nil {
		return err
	}
	if found != 0 {
		return errors.New("development provider changes require no retained adapter configuration or pending configuration work")
	}
	return nil
}

// LoadExecution reads only the immutable inputs named by a leased job. The
// caller must Gate immediately before this method; every query repeats the
// complete job chain and generation fence. Ciphertexts remain sealed here.
func (r *AdapterRuntime) LoadExecution(ctx context.Context, job adapter.Job) (AdapterExecution, error) {
	out, err := dbReadResult(ctx, r.db, func(db adapterDB) (AdapterExecution, error) {
		q := db.adapterRuntimeQueries()
		metadata, err := q.adapterWorkerLoadExecutionQuery(ctx, job.ID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation, job.LeaseOwner)
		if isNoRows(err) {
			return AdapterExecution{}, ErrNotFound
		}
		if err != nil {
			return AdapterExecution{}, err
		}
		out, err := workerExecutionMetadata(metadata, job.TargetID, job.EnvironmentID)
		if err != nil {
			return AdapterExecution{}, err
		}
		ledger, err := q.adapterWorkerLoadExecutionLedgerQuery(ctx, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation)
		if err != nil {
			return AdapterExecution{}, err
		}
		for _, entry := range ledger {
			out.Ledger = append(out.Ledger, adapter.LedgerEntry{Surface: adapter.Surface(entry.Surface), EffectiveName: entry.EffectiveName, State: adapter.LedgerState(entry.State), Missing: entry.Missing, AdoptionPending: entry.AdoptionPending})
		}
		if job.Kind == adapter.Scrub {
			return out, nil
		}
		snapshot, err := q.adapterWorkerLoadExecutionSnapshotQuery(ctx, job.OrgID, job.ProjectID, job.EnvironmentID)
		if isNoRows(err) {
			return out, nil
		}
		if err != nil {
			return AdapterExecution{}, err
		}
		out.Revision = snapshot.Revision
		var contract parameters.Contract
		if err := json.Unmarshal([]byte(snapshot.ParameterContract), &contract); err != nil {
			return AdapterExecution{}, err
		}
		if len(contract.Declarations) > 0 {
			return AdapterExecution{}, errors.New("adapter delivery does not support environment parameters; use a concrete environment")
		}
		entries, err := q.adapterWorkerLoadExecutionEntryQuery(ctx, job.TargetID, snapshot.ID, job.OrgID, job.ProjectID, job.EnvironmentID)
		if err != nil {
			return AdapterExecution{}, err
		}
		for _, entry := range entries {
			out.Entries = append(out.Entries, AdapterSnapshotEntry{ID: entry.ID, SnapshotID: entry.SnapshotID, KeyID: entry.KeyID, KeyName: entry.KeyName, Classification: entry.Classification, Ciphertext: entry.Ciphertext})
		}
		return out, nil
	})
	if err != nil || job.Kind == adapter.Scrub || out.Revision <= 0 {
		return out, err
	}
	// The exact pinned input revision must be durable before any remote effect.
	// A delete can supersede settlement after Journal.Finish releases its fence;
	// scrub still needs this revision for delivered or indeterminate effects.
	err = r.transaction(ctx, func(tx adapterDBTX) error {
		rows, err := tx.adapterRuntimeQueries().adapterWorkerPinExecutionRevision(ctx,
			out.Revision, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID,
			job.Generation)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		// Lock and validate the exact outbox lease in the same transaction.
		// Losing the lease rolls back the revision pin as well.
		rows, err = tx.adapterRuntimeQueries().adapterWorkerPinExecutionLease(ctx,
			job.ID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID,
			job.Generation, job.LeaseOwner)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		return nil
	})
	if err != nil {
		return AdapterExecution{}, err
	}
	return out, nil
}

// LoadActivation resolves only the pending route and its outbound credential.
// It never assembles snapshot plaintext. Caller must Gate immediately before
// this read; Activate later rechecks the exact leased job and move identity.
func (r *AdapterRuntime) LoadActivation(ctx context.Context, job adapter.Job) (AdapterActivation, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) (AdapterActivation, error) {
		if job.Kind != adapter.Activate || job.RouteMoveID == "" {
			return AdapterActivation{}, fmt.Errorf("%w: job is not a route activation", domain.ErrInvalid)
		}
		metadata, err := db.adapterRuntimeQueries().adapterWorkerLoadActivationQuery(ctx, job.ID, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation, job.LeaseOwner)
		if isNoRows(err) {
			return AdapterActivation{}, ErrNotFound
		}
		if err != nil {
			return AdapterActivation{}, err
		}
		return workerActivationMetadata(metadata, job.TargetID)
	})
}

type adapterDBTX interface {
	adapterDB
	Commit(context.Context) error
	Rollback(context.Context) error
}

type sqliteAdapterTx struct{ tx sqliteTransaction }

func (s sqliteAdapterTx) Commit(context.Context) error   { return s.tx.Commit() }
func (s sqliteAdapterTx) Rollback(context.Context) error { return s.tx.Rollback() }

type pgAdapterTx struct{ tx postgresTransaction }

func (p pgAdapterTx) Commit(ctx context.Context) error   { return p.tx.Commit(ctx) }
func (p pgAdapterTx) Rollback(ctx context.Context) error { return p.tx.Rollback(ctx) }

func (r *AdapterRuntime) transaction(ctx context.Context, fn func(adapterDBTX) error) error {
	return dbTransaction(ctx, r.db, fn)
}

// dbTransaction runs fn in one engine-appropriate write transaction wrapped in
// the adapterDBTX typed query owners. Postgres uses SERIALIZABLE for BOTH the adapter and
// the dynamic-secret outbox (system-architecture § Transaction boundary:
// publish-class operations are serializable; #147's handoff pins the dynamic
// worker as "the adapter-outbox composition, verbatim"). Neither outbox wraps a
// 40001 retry here: a serialization failure surfaces as a loud error and the
// worker loop re-drives the transition on its next pass (via next_attempt_at),
// exactly as the adapter path does, so no retry wrapper is added, matching
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
		q := tx.adapterRuntimeQueries()
		current, err := q.adapterWorkerTryEnqueueLookup(ctx, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, now)
		if isNoRows(err) {
			exists, scanErr := q.adapterWorkerTryEnqueueExistsQuery(ctx, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID)
			if scanErr != nil {
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
		depth, err := q.adapterWorkerTryEnqueueDepthQuery(ctx, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID)
		if err != nil {
			return err
		}
		if depth >= 1000 {
			return adapter.ErrQueueFull
		}
		previousJob, previousErr := q.adapterWorkerTryEnqueuePreviousQuery(ctx, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID)
		if previousErr != nil && !errors.Is(previousErr, sql.ErrNoRows) && !errors.Is(previousErr, pgx.ErrNoRows) {
			return previousErr
		}
		if _, err := q.adapterWorkerTryEnqueueSupersede(ctx, now, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID); err != nil {
			return err
		}
		job.Generation = current + 1
		if _, err := q.adapterWorkerTryEnqueueInsert(ctx, job.ID, job.OrgID, job.ProjectID, job.EnvironmentID, job.TargetID, string(job.Kind), job.AuthorityPrincipal, job.Generation, now); err != nil {
			return err
		}
		state := "active"
		if job.Kind == adapter.Scrub {
			state = "tombstoned"
		}
		rows, err := q.adapterWorkerTryEnqueueUpdate(ctx, job.Generation, state, job.ID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, current, now)
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
		q := tx.adapterRuntimeQueries()
		// Paused targets remain queued; claim retains per-org and same-target concurrency fences.
		row, err := q.adapterWorkerClaimDueSelectQuery(ctx, now)
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		created, _ := time.Parse(timeFormat, row.CreatedAt)
		out = adapter.Job{ID: row.ID, OrgID: row.OrgID, ProjectID: row.ProjectID, EnvironmentID: row.EnvironmentID, TargetID: row.TargetID, Kind: adapter.JobKind(row.Kind), RouteMoveID: row.RouteMoveID, AuthorityPrincipal: row.AuthorityPrincipalID, Generation: row.Generation, Attempt: int(row.AttemptCount) + 1, CreatedAt: created, LeaseOwner: worker}
		if _, err := q.adapterWorkerClaimDueUpdate(ctx, int64(out.Attempt), worker, leaseUntil, out.ID); err != nil {
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
	q := tx.adapterRuntimeQueries()
	open, err := q.adapterWorkerCloseIndeterminateEffectsQuery(ctx, claimed.TargetID, claimed.OrgID, claimed.ProjectID, claimed.EnvironmentID, now)
	if err != nil {
		return err
	}
	for _, effect := range open {
		outcomeID := newAdapterID("aud")
		opener := adapter.Job{ID: effect.JobID, OrgID: claimed.OrgID, ProjectID: claimed.ProjectID, EnvironmentID: claimed.EnvironmentID, TargetID: claimed.TargetID, AuthorityPrincipal: effect.AuthorityPrincipalID}
		payload, err := json.Marshal(adapterPushOutcomePayload{Surface: effect.Surface, EffectiveName: effect.EffectiveName, Disposition: effect.Disposition, Finding: "crash_window"})
		if err != nil {
			return err
		}
		if err := r.insertAdapterJobAuditWithID(ctx, tx, opener, outcomeID, "adapter.push_outcome", string(adapter.OutcomeUnknown), now, payload); err != nil {
			return err
		}
		rowsAffected, err := q.adapterWorkerCloseIndeterminateEffectsUpdate(ctx, outcomeID, now, effect.ID, claimed.OrgID, claimed.ProjectID, claimed.EnvironmentID, claimed.TargetID)
		if err != nil {
			return err
		}
		if rowsAffected != 1 {
			return errors.New("store: indeterminate adapter effect was not closed exactly once")
		}
		if _, err := q.adapterWorkerCloseIndeterminateEffectsRelease(ctx, claimed.TargetID, claimed.OrgID, claimed.ProjectID, claimed.EnvironmentID, effect.ID); err != nil {
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
		count, err := db.adapterRuntimeQueries().adapterWorkerGateQuery(ctx, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.Generation, j.job.ID, j.job.LeaseOwner, time.Now().UTC())
		if err != nil {
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
		q := tx.adapterRuntimeQueries()
		normalized := strings.ToUpper(effect.EffectiveName)
		pending, err := q.adapterWorkerReservePendingQuery(ctx, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, string(effect.Surface), normalized)
		if err != nil {
			return err
		}
		if pending != 0 {
			return adapter.ErrConflict
		}
		raw, err := q.adapterWorkerReserveSelectQuery(ctx, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), normalized)
		if err == nil {
			if adapter.LedgerState(raw) == adapter.Released {
				route, err := q.adapterWorkerReserveCurrentRoute(ctx, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID)
				if err != nil {
					return err
				}
				rows, updateErr := q.adapterWorkerReserveReactivate(ctx, effect.EffectiveName, route.Origin, route.DestinationKind, route.RepositoryID, route.DestinationID, route.DestinationScope, time.Now(), j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), normalized)
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
		ledgerRows, err := q.adapterWorkerReserveCountQuery(ctx, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID)
		if err != nil {
			return err
		}
		if ledgerRows >= 10000 {
			return adapter.ErrLedgerFull
		}
		route, err := q.adapterWorkerReserveLookup(ctx, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID)
		if err != nil {
			return err
		}
		_, err = q.adapterWorkerReserveInsert(ctx, newAdapterID("led"), j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, route.Origin, route.DestinationKind, route.RepositoryID, route.DestinationID, route.DestinationScope, string(effect.Surface), effect.EffectiveName, normalized, string(adapter.Reserved), time.Now())
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

		leaseUntil := now.Add(adapter.LeaseTime)
		nowStamp := now
		rows, err := tx.adapterRuntimeQueries().adapterWorkerPrepareProviderLease(ctx, j.job.ID, effectID, leaseUntil, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.Generation, nowStamp)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrProviderBusy
		}

		rows, err = tx.adapterRuntimeQueries().adapterWorkerPrepareLease(ctx, leaseUntil, j.job.ID, j.job.LeaseOwner)
		if err != nil || rows != 1 {
			return adapter.ErrSuperseded
		}
		if prior == adapter.Reserved {

			rows, err := tx.adapterRuntimeQueries().adapterWorkerPrepareUpdate(ctx, now, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), strings.ToUpper(effect.EffectiveName))
			if err != nil || rows != 1 {
				return adapter.ErrSuperseded
			}
		}
		payload, _ := json.Marshal(map[string]string{"surface": string(effect.Surface), "effective_name": effect.EffectiveName, "disposition": string(effect.Disposition)})
		if err := j.insertAudit(ctx, tx, intentID, "adapter.push_intent", "intent", now, payload); err != nil {
			return err
		}

		_, err = tx.adapterRuntimeQueries().adapterWorkerPrepareInsert(ctx, effectID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, j.job.ID, string(effect.Surface), effect.EffectiveName, string(effect.Disposition), intentID, now)
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

		rows, err := tx.adapterRuntimeQueries().adapterWorkerFinishUpdateEffect(ctx, outcomeID, string(completion.Outcome), completion.Finding, now, effectID)
		if err != nil || rows != 1 {
			return errors.New("store: adapter effect OUTCOME was not recorded exactly once")
		}
		if completion.Conflict {
			if err := j.insertConflict(ctx, tx, effect, now); err != nil {
				return err
			}
		}
		if completion.ReleaseLedger {

			rows, err = tx.adapterRuntimeQueries().adapterWorkerFinishRemove(ctx, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), strings.ToUpper(effect.EffectiveName))
		} else {

			rows, err = tx.adapterRuntimeQueries().adapterWorkerFinishUpdate(ctx, string(completion.State), completion.Missing, now, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), strings.ToUpper(effect.EffectiveName))
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

		rows, err = tx.adapterRuntimeQueries().adapterWorkerFinishReleaseLease(ctx, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.ID, effectID)
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

		rows, err := tx.adapterRuntimeQueries().adapterWorkerRefuseRemove(ctx, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), strings.ToUpper(effect.EffectiveName))
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

		now := time.Now().UTC()
		rows, err := tx.adapterRuntimeQueries().adapterWorkerReleaseReservationRemove(ctx, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, string(effect.Surface), strings.ToUpper(effect.EffectiveName), j.job.Generation, j.job.ID, j.job.LeaseOwner, now)
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
	// One conflict artifact per (target, generation, surface, name). A retried
	// job re-runs the same effect against the same generation; without this
	// guard every attempt minted a fresh acf/acn row, flooding the target with
	// duplicate conflict groups (PROD_SSH_KEY x3 at generation 4 in #744). The
	// row is already un-adopted, so re-raise drift and return without a dupe.
	var existing int

	if queryValue, err := tx.adapterRuntimeQueries().adapterWorkerInsertConflictDedup(ctx, j.job.TargetID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.Generation, string(effect.Surface), effect.EffectiveName); err != nil {
		return err
	} else {
		existing = int(queryValue)
	}
	if existing > 0 {
		return raiseDriftAttention(ctx, tx, adapterJobScope(j.job), j.job.TargetID, j.job.EnvironmentID)
	}

	artifactID := newAdapterID("acf")
	rows, err := tx.adapterRuntimeQueries().adapterWorkerInsertConflictInsert(ctx, newAdapterID("acn"), artifactID, j.job.OrgID, j.job.ProjectID, j.job.EnvironmentID, j.job.TargetID, j.job.ID, j.job.Generation, string(effect.Surface), effect.EffectiveName, now)
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

	stamp := at
	_, err := tx.adapterRuntimeQueries().adapterWorkerInsertAdapterJobAuditWithIDQuery(ctx, id, typ, stamp, job.AuthorityPrincipal, job.OrgID, job.ProjectID, job.EnvironmentID, job.TargetID, outcome, job.ID, string(payload))
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

		stamp := at
		rows, err := tx.adapterRuntimeQueries().adapterWorkerActivateFinish(ctx, stamp, job.ID, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation, job.LeaseOwner)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		var adapterID, currentEnvironment, pendingEnvironment, kind, owner, name, destinationEnvironment, visibility, prefix, moveKind, pendingOrigin string
		var selectedRaw []byte

		if queryValue, err := tx.adapterRuntimeQueries().adapterWorkerActivateLookup(ctx, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation); err != nil {
			if isNoRows(err) {
				return adapter.ErrSuperseded
			}
			return err
		} else {
			adapterID = queryValue.AdapterID
			currentEnvironment = queryValue.EnvironmentID
			pendingEnvironment = queryValue.EnvironmentID_2
			kind = queryValue.DestinationKind
			owner = queryValue.DestinationOwner
			name = queryValue.DestinationName
			destinationEnvironment = queryValue.DestinationEnvironment
			visibility = queryValue.Visibility
			selectedRaw = queryValue.SelectedRepositoryIds
			prefix = queryValue.NamePrefix
			moveKind = queryValue.Kind
			pendingOrigin = queryValue.PendingOrigin
		}
		if pendingEnvironment != currentEnvironment {
			return fmt.Errorf("%w: target environment move requires a replacement identity", domain.ErrConflict)
		}

		var collisions int
		if queryValue, err := tx.adapterRuntimeQueries().adapterWorkerActivateCollisionQuery(ctx, job.RouteMoveID, job.TargetID, pendingOrigin, kind, connection.RepositoryID, connection.DestinationID, adapter.SentinelName); err != nil {
			return err
		} else {
			collisions = int(queryValue)
		}
		if collisions != 0 {
			return fmt.Errorf("%w: pending effective names collide on the resolved destination", adapter.ErrConflict)
		}

		rows, err = tx.adapterRuntimeQueries().adapterWorkerActivateSetResolved(ctx, connection.DestinationID, connection.RepositoryID, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, pendingEnvironment)
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

		if _, err := tx.adapterRuntimeQueries().adapterWorkerActivateDeleteKeys(ctx, job.TargetID, job.OrgID, job.ProjectID, currentEnvironment); err != nil {
			return err
		}

		inserted, err := tx.adapterRuntimeQueries().adapterWorkerActivateInsertKeys(ctx, adapterID, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, pendingEnvironment)
		if err != nil {
			return err
		}
		if inserted == 0 {
			return ErrConflict
		}
		convergeID := newAdapterID("job")
		generation := job.Generation + 1

		rows, err = tx.adapterRuntimeQueries().adapterWorkerActivateInsertJob(ctx, convergeID, job.OrgID, job.ProjectID, pendingEnvironment, job.TargetID, job.RouteMoveID, job.AuthorityPrincipal, generation, stamp)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrConflict
		}

		rows, err = tx.adapterRuntimeQueries().adapterWorkerActivateApplyTarget(ctx, kind, owner, name, destinationEnvironment, connection.DestinationID, connection.RepositoryID, visibility, selectedRaw, prefix, generation, job.Generation, convergeID, job.TargetID, job.OrgID, job.ProjectID, currentEnvironment)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
		if !connection.CredentialExpiresAt.IsZero() {

			rows, err = tx.adapterRuntimeQueries().adapterWorkerActivateUpdateExpiry(ctx, connection.CredentialExpiresAt, adapterID, job.OrgID, job.ProjectID)
			if err != nil {
				return err
			}
			if rows != 1 {
				return adapter.ErrSuperseded
			}
		}

		if _, err := tx.adapterRuntimeQueries().adapterWorkerActivateDeleteClaims(ctx, job.RouteMoveID, job.OrgID, job.ProjectID); err != nil {
			return err
		}

		rows, err = tx.adapterRuntimeQueries().adapterWorkerActivateCompleteMove(ctx, job.RouteMoveID, job.OrgID, job.ProjectID, job.TargetID)
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

func (r *AdapterRuntime) activateOriginRouteMove(ctx context.Context, tx adapterDBTX, job adapter.Job, connection adapter.Connection, adapterID string, stamp time.Time, at time.Time) error {

	rows, err := tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveMarkProbe(ctx, job.TargetID, adapterID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation, job.ID)
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

	if queryValue, err := tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveCountUnresolved(ctx, job.RouteMoveID, job.OrgID, job.ProjectID); err != nil {
		return err
	} else {
		unresolved = int(queryValue)
	}
	if unresolved != 0 {
		return nil
	}
	var pendingOrigin string
	var pendingCredential []byte

	if queryValue, err := tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveLoadPending(ctx, job.RouteMoveID, job.OrgID, job.ProjectID, adapterID); err != nil {
		if isNoRows(err) {
			return adapter.ErrSuperseded
		}
		return err
	} else {
		if !queryValue.PendingOrigin.Valid {
			return fmt.Errorf("store: adapter pending origin is NULL")
		}
		pendingOrigin = queryValue.PendingOrigin.String
		pendingCredential = queryValue.PendingCredentialCiphertext
	}
	if pendingOrigin == "" || len(pendingCredential) == 0 {
		return fmt.Errorf("%w: origin move lost pending provider custody", adapter.ErrSuperseded)
	}
	targets, err := tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveTargetQuery(ctx, job.RouteMoveID, job.OrgID, job.ProjectID, adapterID)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.DestinationID <= 0 {
			return adapter.ErrSuperseded
		}
	}
	if len(targets) == 0 {
		return adapter.ErrSuperseded
	}
	for _, target := range targets {

		if _, err := tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveDeleteKeys(ctx, target.ID, job.OrgID, job.ProjectID, target.EnvironmentID); err != nil {
			return err
		}

		inserted, err := tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveInsertKeys(ctx, adapterID, job.RouteMoveID, target.ID, job.OrgID, job.ProjectID, target.EnvironmentID)
		if err != nil {
			return err
		}
		if inserted == 0 {
			return ErrConflict
		}
		convergeID := newAdapterID("job")
		generation := target.Generation + 1

		rows, err = tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveInsertJob(ctx, convergeID, job.OrgID, job.ProjectID, target.EnvironmentID, target.ID, job.RouteMoveID, job.AuthorityPrincipal, generation, stamp)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrConflict
		}

		rows, err = tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveApplyTarget(ctx, target.DestinationKind, target.DestinationOwner, target.DestinationName, target.DestinationEnvironment, target.DestinationID, target.RepositoryID, target.Visibility, target.SelectedRepositoryIds, target.NamePrefix, generation, target.Generation, convergeID, target.ID, adapterID, job.OrgID, job.ProjectID, target.EnvironmentID)
		if err != nil {
			return err
		}
		if rows != 1 {
			return adapter.ErrSuperseded
		}
	}

	var expires *time.Time
	if !connection.CredentialExpiresAt.IsZero() {
		expires = &connection.CredentialExpiresAt
	}
	rows, err = tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveActivateAdapter(ctx, pendingOrigin, pendingCredential, stamp, expires, adapterID, job.OrgID, job.ProjectID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}

	if _, err := tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveDeleteClaims(ctx, job.RouteMoveID, job.OrgID, job.ProjectID); err != nil {
		return err
	}

	rows, err = tx.adapterRuntimeQueries().adapterWorkerActivateOriginRouteMoveCompleteMove(ctx, job.RouteMoveID, job.OrgID, job.ProjectID, adapterID)
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

			rows, err := tx.adapterRuntimeQueries().adapterWorkerFinishJobQuery(ctx, due, job)
			if err != nil {
				return err
			}
			if rows != 1 {
				return adapter.ErrSuperseded
			}
		} else {

			rows, err := tx.adapterRuntimeQueries().adapterWorkerCompleteJob(ctx, state, finished, job)
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

			if queryValue, err := tx.adapterRuntimeQueries().adapterWorkerFinishJobLookup(ctx, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID); err != nil {
				return err
			} else {
				adapterID = queryValue
			}

			rows, err := tx.adapterRuntimeQueries().adapterWorkerFinishJobMarkTarget(ctx, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation)
			if err != nil {
				return err
			}
			if rows != 1 {
				return adapter.ErrSuperseded
			}

			_, err = tx.adapterRuntimeQueries().adapterWorkerFinishJobErase(ctx, adapterID, job.OrgID, job.ProjectID)
			if err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string][]string{"orphaned": {}})
			return r.insertAdapterJobAudit(ctx, tx, job, "adapter.scrub", "success", finished, payload)
		}
		if job.Kind == adapter.Activate && state == "failed" && (errors.Is(terminalErr, adapter.ErrProviderAuth) || errors.Is(terminalErr, adapter.ErrConflict)) {

			rows, err := tx.adapterRuntimeQueries().adapterWorkerFinishJobAttention(ctx, job.RouteMoveID, job.OrgID, job.ProjectID)
			if err != nil {
				return err
			}
			if rows != 1 {
				return adapter.ErrSuperseded
			}

			if _, err := tx.adapterRuntimeQueries().adapterWorkerFinishJobSupersede(ctx, finished, job.RouteMoveID, job.OrgID, job.ProjectID, job.ID); err != nil {
				return err
			}

			if _, err := tx.adapterRuntimeQueries().adapterWorkerFinishJobMarkTargets(ctx, job.OrgID, job.ProjectID, job.RouteMoveID); err != nil {
				return err
			}
			return r.insertAdapterJobAudit(ctx, tx, job, "adapter.test", "failure", finished, []byte(`{}`))
		}
		retainActiveJob := boolInt(state == "queued")
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
		var errorClass sql.NullString
		attentionMode := int64(0)
		if targetStatus != "converged" {
			class := adapter.ClassifyError(terminalErr)
			if class != "" {
				errorClass = sql.NullString{String: string(class), Valid: true}
			}
			attentionMode = 2
			if class.NeedsAttention() {
				attentionMode = 1
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

		var rev sql.NullInt64
		if revision > 0 {
			rev = sql.NullInt64{Int64: revision, Valid: true}
		}
		// converged_revision moves only on success; last_attempted_revision
		// moves on every attempt that got as far as loading a revision.
		var convergedRevision int64
		var convergedRev sql.NullInt64
		if targetStatus == "converged" {
			convergedRevision, convergedRev = revision, rev
		}
		rows, err := tx.adapterRuntimeQueries().adapterWorkerRecordJobOutcome(ctx, targetStatus, convergedRevision, convergedRev, failureJSON, warningJSON, revision, rev, attemptedAt, errorClass, attentionMode, retainActiveJob, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation)
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

	_, err := tx.adapterRuntimeQueries().adapterWorkerRaiseDriftAttentionQuery(ctx, targetID, string(chain.Org), string(chain.Project), environmentID)
	return err
}

func (r *AdapterRuntime) finishRouteMoveScrub(ctx context.Context, tx adapterDBTX, job adapter.Job, finished time.Time, orphaned []string) error {
	var moveKind string

	if queryValue, err := tx.adapterRuntimeQueries().adapterWorkerFinishRouteMoveScrubLookupMove(ctx, job.RouteMoveID, job.OrgID, job.ProjectID, job.TargetID, job.EnvironmentID); err != nil {
		if isNoRows(err) {
			return adapter.ErrSuperseded
		}
		return err
	} else {
		moveKind = queryValue
	}
	var activeLedger int

	if queryValue, err := tx.adapterRuntimeQueries().adapterWorkerFinishRouteMoveScrubActiveLedgerQuery(ctx, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID); err != nil {
		return err
	} else {
		activeLedger = int(queryValue)
	}
	if activeLedger != 0 {
		return fmt.Errorf("%w: route move scrub retained active custody", adapter.ErrSuperseded)
	}
	payload, _ := json.Marshal(map[string][]string{"orphaned": orphaned})
	outcome := "success"
	if len(orphaned) != 0 {
		outcome = "failure"
		orphanJSON, _ := json.Marshal(orphaned)

		rows, err := tx.adapterRuntimeQueries().adapterWorkerFinishRouteMoveScrubPersistOrphans(ctx, orphanJSON, job.RouteMoveID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID)
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

	rows, err := tx.adapterRuntimeQueries().adapterWorkerFinishRouteMoveScrubMove(ctx, job.RouteMoveID, job.OrgID, job.ProjectID, job.TargetID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	activateID := newAdapterID("job")
	stamp := finished

	rows, err = tx.adapterRuntimeQueries().adapterWorkerFinishRouteMoveScrubInsert(ctx, activateID, job.OrgID, job.ProjectID, job.EnvironmentID, job.TargetID, job.RouteMoveID, job.AuthorityPrincipal, job.Generation, stamp)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConflict
	}
	failureJSON, _ := json.Marshal(orphaned)

	rows, err = tx.adapterRuntimeQueries().adapterWorkerFinishRouteMoveScrubMark(ctx, failureJSON, activateID, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation)
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

	rows, err := tx.adapterRuntimeQueries().adapterWorkerFinishOriginRouteMoveScrubMarkDone(ctx, failureJSON, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation, job.ID)
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

	if queryValue, err := tx.adapterRuntimeQueries().adapterWorkerFinishOriginRouteMoveScrubCountPending(ctx, job.RouteMoveID, job.OrgID, job.ProjectID); err != nil {
		return err
	} else {
		pendingScrubs = int(queryValue)
	}
	if pendingScrubs != 0 {
		return nil
	}

	rows, err = tx.adapterRuntimeQueries().adapterWorkerFinishOriginRouteMoveScrubActivateMove(ctx, job.RouteMoveID, job.OrgID, job.ProjectID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}
	targets, err := tx.adapterRuntimeQueries().adapterWorkerFinishOriginRouteMoveScrubTargetQuery(ctx, job.RouteMoveID, job.OrgID, job.ProjectID)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return adapter.ErrSuperseded
	}
	stamp := finished
	for _, target := range targets {
		activateID := newAdapterID("job")

		rows, err = tx.adapterRuntimeQueries().adapterWorkerFinishOriginRouteMoveScrubInsert(ctx, activateID, job.OrgID, job.ProjectID, target.EnvironmentID, target.ID, job.RouteMoveID, target.AuthorityPrincipalID, target.Generation, stamp)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrConflict
		}

		rows, err = tx.adapterRuntimeQueries().adapterWorkerFinishOriginRouteMoveScrubMark(ctx, activateID, target.ID, job.OrgID, job.ProjectID, target.EnvironmentID, target.Generation)
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

	if queryValue, err := tx.adapterRuntimeQueries().adapterWorkerFinishDeadCredentialScrubLookup(ctx, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID); err != nil {
		return err
	} else {
		adapterID = queryValue
	}

	if _, err := tx.adapterRuntimeQueries().adapterWorkerFinishDeadCredentialScrubReleaseLedger(ctx, finished, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID); err != nil {
		return err
	}
	if job.RouteMoveID != "" {
		return r.finishRouteMoveScrub(ctx, tx, job, finished, orphaned)
	}
	failureJSON, _ := json.Marshal(orphaned)

	rows, err := tx.adapterRuntimeQueries().adapterWorkerFinishDeadCredentialScrubMarkTarget(ctx, failureJSON, job.TargetID, job.OrgID, job.ProjectID, job.EnvironmentID, job.Generation)
	if err != nil {
		return err
	}
	if rows != 1 {
		return adapter.ErrSuperseded
	}

	if _, err := tx.adapterRuntimeQueries().adapterWorkerFinishDeadCredentialScrubErase(ctx, adapterID, job.OrgID, job.ProjectID); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string][]string{"orphaned": orphaned})
	return r.insertAdapterJobAudit(ctx, tx, job, "adapter.scrub", "failure", finished, payload)
}
