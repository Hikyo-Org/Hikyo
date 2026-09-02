package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Multi-target control and health (#157): pause is a claim-time gate that
// keeps the ledger, resume catches up to a named revision, every attempt
// stamps what it tried and why it failed, and a crash between INTENT and
// OUTCOME is settled as an unknown OUTCOME at the next claim.

func adapterControlDB(t *testing.T) *store.DB {
	t.Helper()
	db := adapterRuntimeDB(t)
	statements := []string{
		`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_control','usr_adapter','manage-adapters','org_adapter','prj_adapter',NULL,'2026-08-17T00:00:00Z')`,
		`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_control_reveal','usr_adapter','reveal','org_adapter','prj_adapter','env_adapter','2026-08-17T00:00:00Z')`,
		`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_control_instance','usr_adapter','instance-config',NULL,NULL,NULL,'2026-08-17T00:00:00Z')`,
		`INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ('key_b','org_adapter','prj_adapter','DB_URL','','secret','',0,'','optional','none','none','2026-08-17T00:00:00Z')`,
		`INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ('key_a','org_adapter','prj_adapter','APP_MODE','','config','',0,'','optional','none','none','2026-08-17T00:00:00Z')`,
		`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ('org_adapter','prj_adapter','env_adapter','tgt_1','adp_1','key_b')`,
		`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ('org_adapter','prj_adapter','env_adapter','tgt_1','adp_1','key_a')`,
		`INSERT INTO snapshots (id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at,payload_present) VALUES ('snap_7','org_adapter','prj_adapter','env_adapter',7,1,'usr_adapter','2026-08-17T00:00:00Z',1)`,
	}
	for _, statement := range statements {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func adapterControlProof(t *testing.T, db *store.DB, op authz.Operation, fn func(context.Context, store.Repos, authz.Proof) error) error {
	t.Helper()
	scope := domain.Scope{Org: "org_adapter", Project: "prj_adapter"}
	if op == authz.OpRetentionHealthRead {
		scope = domain.Scope{}
	}
	return storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adapter"}, op, scope)
		if err != nil {
			return err
		}
		return fn(ctx, repos, p)
	})
}

func adapterControlTarget(t *testing.T, db *store.DB) store.AdapterTarget {
	t.Helper()
	var target store.AdapterTarget
	if err := adapterControlProof(t, db, authz.OpAdapterInspect, func(ctx context.Context, repos store.Repos, p authz.Proof) error {
		var err error
		target, err = repos.Adapters().Target(ctx, p, "tgt_1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestAdapterPauseBlocksClaimsKeepsLedgerAndResumeCatchesUp(t *testing.T) {
	db := adapterControlDB(t)
	runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
	now := time.Now().UTC()
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_id,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_owned','org_adapter','prj_adapter','env_adapter','tgt_1','https://git.example',42,'secret','DB_URL','DB_URL','owned','2026-08-17T00:00:00.000000Z')`); err != nil {
		t.Fatal(err)
	}

	var paused store.AdapterPauseResult
	if err := adapterControlProof(t, db, authz.OpAdapterConfigure, func(ctx context.Context, repos store.Repos, p authz.Proof) error {
		var err error
		paused, err = repos.Adapters().PauseTarget(ctx, p, "tgt_1", now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if paused.SupersededJobID != "job_1" || paused.Generation != 2 || paused.AlreadyPaused {
		t.Fatalf("pause = %+v", paused)
	}
	target := adapterControlTarget(t, db)
	if target.PausedAt == nil || target.Health() != adapter.HealthPaused || target.ActiveJobState != "" {
		t.Fatalf("paused target = %+v", target)
	}
	if _, ok, err := runtime.ClaimDue(t.Context(), "worker_1", now.Add(time.Second), now.Add(time.Minute)); err != nil || ok {
		t.Fatalf("paused target's job was claimable: ok=%v err=%v", ok, err)
	}
	// Pause is idempotent and never bumps the generation twice.
	if err := adapterControlProof(t, db, authz.OpAdapterConfigure, func(ctx context.Context, repos store.Repos, p authz.Proof) error {
		again, err := repos.Adapters().PauseTarget(ctx, p, "tgt_1", now.Add(time.Second))
		if err != nil {
			return err
		}
		if !again.AlreadyPaused || again.Generation != 2 {
			t.Fatalf("second pause = %+v", again)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A push-shaped act is refused loudly while paused; the ledger is intact.
	err := adapterControlProof(t, db, authz.OpAdapterSync, func(ctx context.Context, repos store.Repos, p authz.Proof) error {
		_, err := repos.Adapters().EnqueueManual(ctx, p, "tgt_1", "usr_adapter", now.Add(2*time.Second))
		return err
	})
	if !errors.Is(err, store.ErrAdapterTargetPaused) || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("manual sync while paused = %v", err)
	}
	var ledgerState string
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT state FROM adapter_ledger WHERE id='led_owned'`).Scan(&ledgerState); err != nil || ledgerState != "owned" {
		t.Fatalf("pause touched the ledger: state=%q err=%v", ledgerState, err)
	}

	var resumed store.AdapterResumeResult
	if err := adapterControlProof(t, db, authz.OpAdapterSync, func(ctx context.Context, repos store.Repos, p authz.Proof) error {
		var err error
		resumed, err = repos.Adapters().ResumeTarget(ctx, p, "tgt_1", "usr_adapter", now.Add(3*time.Second))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if resumed.Revision != 7 || resumed.Enqueue.JobID == "" || resumed.Enqueue.Generation != 3 {
		t.Fatalf("resume = %+v", resumed)
	}
	target = adapterControlTarget(t, db)
	if target.PausedAt != nil || target.Health() != adapter.HealthPending || target.ActiveJobState != "queued" {
		t.Fatalf("resumed target = %+v", target)
	}
	claimed, ok, err := runtime.ClaimDue(t.Context(), "worker_1", now.Add(4*time.Second), now.Add(time.Minute))
	if err != nil || !ok || claimed.ID != resumed.Enqueue.JobID {
		t.Fatalf("resume's converge was not claimable: ok=%v err=%v job=%+v", ok, err, claimed)
	}
	if target = adapterControlTarget(t, db); target.Health() != adapter.HealthConverging {
		t.Fatalf("claimed target health = %q", target.Health())
	}
	// Resuming twice is refused: the second call has nothing to resume.
	err = adapterControlProof(t, db, authz.OpAdapterSync, func(ctx context.Context, repos store.Repos, p authz.Proof) error {
		_, err := repos.Adapters().ResumeTarget(ctx, p, "tgt_1", "usr_adapter", now.Add(5*time.Second))
		return err
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second resume = %v", err)
	}
}

func TestAdapterAttemptsRecordRevisionErrorClassAndAttention(t *testing.T) {
	db := adapterControlDB(t)
	runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
	now := time.Now().UTC()
	job, ok, err := runtime.ClaimDue(t.Context(), "worker_1", now, now.Add(adapter.LeaseTime))
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := runtime.Retry(t.Context(), job, now.Add(time.Minute), 7, nil, nil, adapter.ErrProviderAuth); err != nil {
		t.Fatal(err)
	}
	target := adapterControlTarget(t, db)
	if target.LastErrorClass != adapter.ErrorClassAuth || target.LastAttemptedRevision == nil || *target.LastAttemptedRevision != 7 || target.LastAttemptedAt == nil || target.DriftAttention || target.RetryAt == nil || target.Health() != adapter.HealthFailed {
		t.Fatalf("after auth retry = %+v", target)
	}
	if !target.RetryAt.Equal(store.CanonTime(now.Add(time.Minute))) {
		t.Fatalf("retry_at = %v, want %v", target.RetryAt, now.Add(time.Minute))
	}

	job, ok, err = runtime.ClaimDue(t.Context(), "worker_1", now.Add(2*time.Minute), now.Add(4*time.Minute))
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := runtime.Retry(t.Context(), job, now.Add(5*time.Minute), 7, nil, nil, errors.New("wrapped: "+adapter.ErrConflict.Error())); err != nil {
		t.Fatal(err)
	}
	if target = adapterControlTarget(t, db); target.LastErrorClass != adapter.ErrorClassNetwork {
		t.Fatalf("an unwrapped string is transport, got %q", target.LastErrorClass)
	}
	job, ok, err = runtime.ClaimDue(t.Context(), "worker_1", now.Add(6*time.Minute), now.Add(8*time.Minute))
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := runtime.Retry(t.Context(), job, now.Add(9*time.Minute), 7, nil, nil, adapter.ErrConflict); err != nil {
		t.Fatal(err)
	}
	if target = adapterControlTarget(t, db); target.LastErrorClass != adapter.ErrorClassConflict || !target.DriftAttention {
		t.Fatalf("conflict must raise attention: %+v", target)
	}

	job, ok, err = runtime.ClaimDue(t.Context(), "worker_1", now.Add(10*time.Minute), now.Add(12*time.Minute))
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := runtime.Succeed(t.Context(), job, 7, nil, now.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	target = adapterControlTarget(t, db)
	if target.LastErrorClass != "" || target.DriftAttention || target.Health() != adapter.HealthConverged || target.ConvergedRevision == nil || *target.ConvergedRevision != 7 || target.RetryAt != nil {
		t.Fatalf("success must clear class and attention: %+v", target)
	}

	// A later failure after a success is degraded, not failed: the
	// destination still holds revision 7.
	if err := adapterControlProof(t, db, authz.OpAdapterSync, func(ctx context.Context, repos store.Repos, p authz.Proof) error {
		_, err := repos.Adapters().EnqueueManual(ctx, p, "tgt_1", "usr_adapter", now.Add(13*time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	job, ok, err = runtime.ClaimDue(t.Context(), "worker_1", now.Add(14*time.Minute), now.Add(16*time.Minute))
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := runtime.Fail(t.Context(), job, 8, now.Add(15*time.Minute), adapter.ErrProviderAuth); err != nil {
		t.Fatal(err)
	}
	target = adapterControlTarget(t, db)
	if target.Health() != adapter.HealthDegraded || *target.LastAttemptedRevision != 8 || *target.ConvergedRevision != 7 || target.LastErrorClass != adapter.ErrorClassAuth {
		t.Fatalf("degraded = %+v", target)
	}
}

func TestAdapterClaimSettlesCrashWindowAsUnknownOutcome(t *testing.T) {
	db := adapterControlDB(t)
	runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
	now := time.Now().UTC()
	job, ok, err := runtime.ClaimDue(t.Context(), "worker_1", now, now.Add(adapter.LeaseTime))
	if err != nil || !ok {
		t.Fatal(err)
	}
	effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "DB_URL", Disposition: adapter.Create, KeyID: "key_b"}
	journal := runtime.Journal(job)
	state, err := journal.Reserve(t.Context(), effect)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Prepare(t.Context(), effect, state); err != nil {
		t.Fatal(err)
	}
	// The process dies here: INTENT durable, provider request maybe on the
	// wire, no OUTCOME. While the provider-write lease is live nobody may
	// settle it, so a claim before expiry finds nothing to claim (the job is
	// still leased) and the effect stays open.
	if _, ok, err := runtime.ClaimDue(t.Context(), "worker_2", now.Add(time.Minute), now.Add(3*time.Minute)); err != nil || ok {
		t.Fatalf("live lease was reclaimed: ok=%v err=%v", ok, err)
	}
	var open int
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM adapter_effects WHERE outcome IS NULL`).Scan(&open); err != nil || open != 1 {
		t.Fatalf("open effects before expiry = %d err=%v", open, err)
	}
	// Both leases expired: the replay claim settles the window.
	replay, ok, err := runtime.ClaimDue(t.Context(), "worker_2", now.Add(3*adapter.LeaseTime), now.Add(4*adapter.LeaseTime))
	if err != nil || !ok || replay.ID != job.ID || replay.Attempt != 2 {
		t.Fatalf("replay = %+v ok=%v err=%v", replay, ok, err)
	}
	var outcome, outcomeAudit string
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT outcome,COALESCE(outcome_audit_id,'') FROM adapter_effects WHERE job_id='job_1'`).Scan(&outcome, &outcomeAudit); err != nil {
		t.Fatal(err)
	}
	if outcome != "unknown" || outcomeAudit == "" {
		t.Fatalf("crash-window effect outcome=%q audit=%q", outcome, outcomeAudit)
	}
	var auditType, auditOutcome, correlation, intentCorrelation string
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT type,outcome,correlation_id FROM audit_tenant_events WHERE id=?`, outcomeAudit).Scan(&auditType, &auditOutcome, &correlation); err != nil {
		t.Fatal(err)
	}
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT e.correlation_id FROM audit_tenant_events e JOIN adapter_effects f ON f.intent_audit_id=e.id WHERE f.job_id='job_1'`).Scan(&intentCorrelation); err != nil {
		t.Fatal(err)
	}
	if auditType != "adapter.push_outcome" || auditOutcome != "unknown" || correlation != "job_1" || intentCorrelation != correlation {
		t.Fatalf("INTENT/OUTCOME linkage: type=%q outcome=%q correlation=%q intent=%q", auditType, auditOutcome, correlation, intentCorrelation)
	}
	var ledgerState string
	var leaseJob any
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT l.state,t.provider_lease_job_id FROM adapter_ledger l JOIN adapter_targets t ON t.id=l.target_id WHERE l.normalized_name='DB_URL'`).Scan(&ledgerState, &leaseJob); err != nil {
		t.Fatal(err)
	}
	if ledgerState != "dispatched" || leaseJob != nil {
		t.Fatalf("ledger=%q lease=%v: presumed-written custody must survive, the dead lease must not", ledgerState, leaseJob)
	}
	// The retry converges the same name from its dispatched state.
	retry := runtime.Journal(replay)
	if state, err := retry.Reserve(t.Context(), effect); err != nil || state != adapter.Dispatched {
		t.Fatalf("retry reserve = %q, %v", state, err)
	}
	if err := retry.Prepare(t.Context(), effect, adapter.Dispatched); err != nil {
		t.Fatal(err)
	}
	if err := retry.Finish(t.Context(), effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Succeed(t.Context(), replay, 7, nil, now.Add(4*adapter.LeaseTime)); err != nil {
		t.Fatal(err)
	}
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM adapter_effects WHERE outcome IS NULL`).Scan(&open); err != nil || open != 0 {
		t.Fatalf("open effects after retry = %d err=%v", open, err)
	}
}

func TestAdapterStaleWorkerIsFencedAfterReclaim(t *testing.T) {
	db := adapterControlDB(t)
	runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
	now := time.Now().UTC()
	stale, ok, err := runtime.ClaimDue(t.Context(), "node_a", now, now.Add(time.Minute))
	if err != nil || !ok {
		t.Fatal(err)
	}
	fresh, ok, err := runtime.ClaimDue(t.Context(), "node_b", now.Add(2*time.Minute), now.Add(4*time.Minute))
	if err != nil || !ok || fresh.LeaseOwner != "node_b" {
		t.Fatalf("reclaim = %+v ok=%v err=%v", fresh, ok, err)
	}
	effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "DB_URL", Disposition: adapter.Create}
	journal := runtime.Journal(stale)
	if err := journal.Gate(t.Context(), effect); !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatalf("stale Gate = %v", err)
	}
	if err := journal.Prepare(t.Context(), effect, adapter.Reserved); !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatalf("stale Prepare (push) = %v", err)
	}
	if err := runtime.Succeed(t.Context(), stale, 7, nil, now.Add(3*time.Minute)); !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatalf("stale Succeed = %v", err)
	}
	if err := runtime.Retry(t.Context(), stale, now.Add(time.Hour), 7, nil, nil, errors.New("late")); !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatalf("stale Retry = %v", err)
	}
	if err := runtime.Fail(t.Context(), stale, 7, now.Add(3*time.Minute), adapter.ErrProviderAuth); !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatalf("stale Fail = %v", err)
	}
	scrub := stale
	scrub.Kind = adapter.Scrub
	if err := runtime.Succeed(t.Context(), scrub, 0, nil, now.Add(3*time.Minute)); !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatalf("stale scrub completion (prune) = %v", err)
	}
	var state, owner string
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT state,lease_owner FROM adapter_outbox WHERE id='job_1'`).Scan(&state, &owner); err != nil {
		t.Fatal(err)
	}
	if state != "running" || owner != "node_b" {
		t.Fatalf("stale worker moved the job: state=%q owner=%q", state, owner)
	}
	if err := runtime.Succeed(t.Context(), fresh, 7, nil, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("fresh holder refused: %v", err)
	}
}

func TestAdapterTargetKeysAndHealthCounts(t *testing.T) {
	db := adapterControlDB(t)
	var keys []store.AdapterTargetKey
	if err := adapterControlProof(t, db, authz.OpAdapterInspect, func(ctx context.Context, repos store.Repos, p authz.Proof) error {
		var err error
		keys, err = repos.Adapters().TargetKeys(ctx, p, "tgt_1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0].Name != "APP_MODE" || keys[0].Classification != "config" || keys[1].ID != "key_b" {
		t.Fatalf("target keys = %+v", keys)
	}
	statements := []string{
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_two','org_adapter','prj_adapter','two','','2026-08-17T00:00:00Z',1)`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at,paused_at) VALUES ('tgt_paused','org_adapter','prj_adapter','env_two','adp_1','repository','acme','paused',43,'P_',1,'active','converged','2026-08-17T00:00:00Z','2026-08-17T00:00:00.000000Z')`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at,drift_attention) VALUES ('tgt_failed','org_adapter','prj_adapter','env_two','adp_1','repository','acme','failed',44,'F_',1,'active','failed','2026-08-17T00:00:00Z',1)`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at,drift_attention) VALUES ('tgt_gone','org_adapter','prj_adapter','env_two','adp_1','repository','acme','gone',45,'G_',1,'tombstoned','failed','2026-08-17T00:00:00Z',1)`,
	}
	for _, statement := range statements {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	var counts store.AdapterHealthCounts
	if err := adapterControlProof(t, db, authz.OpRetentionHealthRead, func(ctx context.Context, repos store.Repos, p authz.Proof) error {
		var err error
		counts, err = repos.Adapters().HealthCounts(ctx, p)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := store.AdapterHealthCounts{TargetsFailed: 1, TargetsPaused: 1, TargetsAttention: 1, JobsQueued: 1}
	if counts != want {
		t.Fatalf("health counts = %+v, want %+v", counts, want)
	}
}
