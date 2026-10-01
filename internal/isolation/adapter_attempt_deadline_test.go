package isolation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type deadlineRuntimeModule struct {
	fanoutModule
	t  *testing.T
	db *store.DB
}

func (m *deadlineRuntimeModule) Sync(ctx context.Context, request adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	finish := func(name string, outcome adapter.Outcome, state adapter.LedgerState) error {
		return journal.Finish(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: name, Disposition: adapter.Create}, adapter.Completion{Outcome: outcome, State: state})
	}
	prepare := func(name string) error {
		effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: name, Disposition: adapter.Create}
		if err := journal.Gate(ctx, effect); err != nil {
			return err
		}
		state, err := journal.Reserve(ctx, effect)
		if err != nil {
			return err
		}
		return journal.Prepare(ctx, effect, state)
	}
	if err := prepare("APPLIED"); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := finish("APPLIED", adapter.OutcomeSuccess, adapter.Owned); err != nil {
		return adapter.SyncResult{}, err
	}
	if request.JobID == "deadline_b" {
		return adapter.SyncResult{}, nil
	}
	if err := prepare("DISPATCHED"); err != nil {
		return adapter.SyncResult{}, err
	}
	// A cancellable provider operation consumes the actual production attempt
	// budget. Its outcome is unknown, not success, even though an earlier name
	// was applied. The durable journal must retain both without canceled writes.
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return adapter.SyncResult{}, ctx.Err()
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
	if held := queryInt(m.t, m.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_targets WHERE id='deadline_target_a' AND provider_lease_job_id='deadline_a' AND provider_lease_expires_at>'%s'`, now)); held != 1 {
		m.t.Fatal("provider lease expired before the cancellation outcome could settle")
	}
	if err := finish("DISPATCHED", adapter.OutcomeUnknown, adapter.Dispatched); err != nil {
		return adapter.SyncResult{}, err
	}
	return adapter.SyncResult{Changes: []adapter.Change{{Surface: adapter.Secret, EffectiveName: "APPLIED"}}, Failed: []adapter.Change{{Surface: adapter.Secret, EffectiveName: "DISPATCHED"}}}, ctx.Err()
}

type deadlineRuntimeLoader struct{ module adapter.Module }

func (l deadlineRuntimeLoader) Load(ctx context.Context, _ adapter.Job, journal adapter.Journal) (adapter.LoadedSync, error) {
	if err := journal.Gate(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: "manifest", Disposition: adapter.Update}); err != nil {
		return adapter.LoadedSync{}, err
	}
	return adapter.LoadedSync{Module: l.module, Revision: 1}, nil
}

func TestAdapterAttemptDeadlineRetainsDurableCustodyAndRunsNextTenant(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		// The engine fixtures are independent; both real two-minute attempts
		// may run together, without multiplying the suite's wall-clock cost.
		t.Parallel()
		seedDeadlineTargets(t, db)
		runtime := generatedAdapterRuntime(db)
		worker := adapter.Worker{Store: runtime, Loader: deadlineRuntimeLoader{module: &deadlineRuntimeModule{t: t, db: db}}, ID: "deadline_worker", Jitter: func(d time.Duration) time.Duration { return d }}
		started := time.Now()
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
			t.Fatalf("timed-out attempt settlement = %v, %v", worked, err)
		}
		if elapsed := time.Since(started); elapsed < 2*time.Minute || elapsed > 2*time.Minute+5*time.Second {
			t.Fatalf("actual attempt elapsed = %s, want two-minute execution plus bounded settlement", elapsed)
		}
		checks := []string{
			`SELECT COUNT(*) FROM adapter_outbox WHERE id='deadline_a' AND state='queued' AND attempt_count=1 AND lease_owner IS NULL`,
			`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='deadline_target_a' AND effective_name='APPLIED' AND state='owned'`,
			`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='deadline_target_a' AND effective_name='DISPATCHED' AND state='dispatched'`,
			`SELECT COUNT(*) FROM adapter_effects WHERE target_id='deadline_target_a' AND effective_name='DISPATCHED' AND outcome='unknown' AND outcome_audit_id IS NOT NULL`,
			`SELECT COUNT(*) FROM adapter_targets WHERE id='deadline_target_a' AND provider_lease_job_id IS NULL AND sync_status='failed'`,
		}
		for _, query := range checks {
			if got := queryInt(t, db, query); got != 1 {
				t.Fatalf("timeout custody check %q = %d, want 1", query, got)
			}
		}
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
			t.Fatalf("next tenant execution = %v, %v", worked, err)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE id='deadline_b' AND state='succeeded' AND attempt_count=1`); got != 1 {
			t.Fatal("next tenant did not complete after the blocked attempt yielded")
		}
	})
}

func seedDeadlineTargets(t *testing.T, db *store.DB) {
	t.Helper()
	for _, tenant := range []struct{ suffix, org, project, env, principal string }{
		{"a", "org_a", "prj_a1", "env_a1", "usr_alice"},
		{"b", "org_b", "prj_b1", "env_b1", "usr_bob"},
	} {
		execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('deadline_manage_%s','%s','manage-adapters','%s','%s',NULL,%s)`, tenant.suffix, tenant.principal, tenant.org, tenant.project, ts))
		if tenant.suffix == "a" {
			execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('deadline_reveal_a','usr_alice','reveal','org_a','prj_a1','env_a1',`+ts+`)`)
		}
		execRaw(t, db, fmt.Sprintf(`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('deadline_adapter_%s','%s','%s','forgejo','https://deadline-%s.example','%s','active',%s)`, tenant.suffix, tenant.org, tenant.project, tenant.suffix, tenant.principal, ts))
		execRaw(t, db, fmt.Sprintf(`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,active_job_id,created_at) VALUES ('deadline_target_%s','%s','%s','%s','deadline_adapter_%s','repository','acme','app',42,'',1,'active','converging','deadline_%s',%s)`, tenant.suffix, tenant.org, tenant.project, tenant.env, tenant.suffix, tenant.suffix, ts))
		execRaw(t, db, fmt.Sprintf(`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ('deadline_%s','%s','%s','%s','deadline_target_%s','converge','%s',1,'deadline_target_%s',%s,'queued',%s)`, tenant.suffix, tenant.org, tenant.project, tenant.env, tenant.suffix, tenant.principal, tenant.suffix, ts, ts))
	}
}
