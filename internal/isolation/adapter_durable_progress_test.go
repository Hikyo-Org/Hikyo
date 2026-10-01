package isolation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type progressRateWait struct{ at time.Time }

func (e progressRateWait) Error() string      { return "provider attempt quota exhausted" }
func (e progressRateWait) RetryAt() time.Time { return e.at }

type progressModule struct {
	fanoutModule
	now       *time.Time
	writes    map[adapter.LedgerKey]int
	completed []int
	rateWait  time.Duration
}

func (m *progressModule) Sync(ctx context.Context, request adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	m.completed = append(m.completed, len(request.Completed))
	completed := adapter.CompletedNames(request.Completed)
	ledger, err := adapter.IndexLedger(request.Ledger)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	result := adapter.SyncResult{}
	for _, row := range adapter.DesiredRows(request.Target.NamePrefix, request.Manifest, true) {
		key := adapter.NewLedgerKey(row.Surface, row.EffectiveName)
		if completed[key] {
			continue
		}
		if len(result.Changes) == 1 {
			// Beyond the original lease: this must durably yield, not merely
			// reuse the process-local in-attempt cursor after a short wait.
			delay := m.rateWait
			if delay == 0 {
				delay = 3 * time.Minute
			}
			return result, progressRateWait{at: m.now.Add(delay)}
		}
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Create, InputRevision: 999}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		state, claimed := ledger[key]
		prior := state.State
		if !claimed {
			prior, err = journal.Reserve(ctx, effect)
			if err != nil {
				return result, err
			}
		} else {
			effect.Disposition = adapter.Update
		}
		if err := journal.Prepare(ctx, effect, prior); err != nil {
			return result, err
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned}); err != nil {
			return result, err
		}
		m.writes[key]++
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
	}
	return result, nil
}

func TestAdapterRateWaitPublishCannotConvergeNewSourceWithOldAcknowledgement(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		publishProgressFixture(t, db)
		runtime := generatedAdapterRuntime(db)
		clock := time.Now().UTC()
		module := &progressModule{now: &clock, writes: make(map[adapter.LedgerKey]int), rateWait: time.Second}
		published := false
		worker := adapter.Worker{Store: runtime, Loader: progressLoader{runtime: runtime, module: module}, ID: "publish_wait_worker", Now: func() time.Time { return clock }, Wait: func(ctx context.Context, _ time.Duration) error {
			if published {
				return nil
			}
			published = true
			staged, err := valueSvc(t, db).Set(ctx, service.LocalPrincipal(custodian), scopeEnv(orgA, prjA1, envA1), "SHARED_KEY", "changed-during-provider-wait", nil)
			if err != nil {
				return err
			}
			revisions := service.Revisions{DB: db, Keyring: probeKeyring(t, db)}
			if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(custodian), scopeEnv(orgA, prjA1, envA1), service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
				return err
			}
			clock = time.Now().UTC()
			return nil
		}}
		if worked, err := worker.RunOnce(t.Context()); !worked || !errors.Is(err, adapter.ErrSuperseded) || !published {
			t.Fatalf("publish during actual provider wait = %v, %v; published=%v", worked, err, published)
		}
		if len(module.writes) != 1 || queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE target_id='deadline_target_a' AND state='succeeded'`) != 0 {
			t.Fatal("old leased attempt converged across a real new publication")
		}
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
			t.Fatalf("new source delivery = %v, %v", worked, err)
		}
		if len(module.writes) != 3 || queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE target_id='deadline_target_a' AND state='succeeded'`) != 1 {
			t.Fatalf("new publication did not fully re-deliver: %v", module.writes)
		}
		writes := 0
		for _, count := range module.writes {
			writes += count
		}
		if writes != 4 || module.completed[1] != 0 {
			t.Fatalf("new source reused old prefix: writes=%v completed=%v", module.writes, module.completed)
		}
	})
}

type progressLoader struct {
	runtime *store.AdapterRuntime
	module  adapter.Module
}

type progressStore struct {
	*store.AdapterRuntime
	t *testing.T
}

func (s progressStore) Fail(_ context.Context, _ adapter.Job, _ int64, _ time.Time, cause error) error {
	s.t.Fatalf("progress attempt permanently refused: %v", cause)
	return cause
}

func (l progressLoader) Load(ctx context.Context, job adapter.Job, journal adapter.Journal) (adapter.LoadedSync, error) {
	if err := journal.Gate(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: "manifest", Disposition: adapter.Update}); err != nil {
		return adapter.LoadedSync{}, err
	}
	material, err := l.runtime.LoadExecution(ctx, job)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	manifest := make([]adapter.ManifestEntry, 0, len(material.Entries))
	for _, entry := range material.Entries {
		manifest = append(manifest, adapter.ManifestEntry{KeyID: entry.KeyID, CanonicalName: entry.KeyName, Classification: adapter.Classification(entry.Classification)})
	}
	return adapter.LoadedSync{Module: l.module, Revision: material.Revision, Request: adapter.SyncRequest{Target: material.Target, Manifest: manifest, Ledger: material.Ledger, Completed: material.Completed}}, nil
}

func publishProgressFixture(t *testing.T, db *store.DB) {
	t.Helper()
	seedDeadlineTargets(t, db)
	// LoadExecution retains sealed credentials. The provider double never opens
	// this fixture, while the real loader's absent-credential refusal stays on.
	execRealAdoption(t, db, `UPDATE adapters SET credential_ciphertext=$1 WHERE id='deadline_adapter_a'`, []byte("sealed-fixture"))
	execRaw(t, db, `UPDATE adapter_targets SET paused_at=`+ts+` WHERE id='deadline_target_b'`)
	execRaw(t, db, `INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ('org_a','prj_a1','env_a1','deadline_target_a','deadline_adapter_a','key_a1')`)
	staged, err := valueSvc(t, db).Set(t.Context(), service.LocalPrincipal(custodian), scopeEnv(orgA, prjA1, envA1), "SHARED_KEY", "progress-source", nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions := service.Revisions{DB: db, Keyring: probeKeyring(t, db)}
	if _, err := revisions.PublishPlanned(t.Context(), service.LocalPrincipal(custodian), scopeEnv(orgA, prjA1, envA1), service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterDurableRetryResumesAcknowledgedPrefixAndNewJobRedelivers(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		publishProgressFixture(t, db)
		runtime := generatedAdapterRuntime(db)
		clock := time.Now().UTC()
		module := &progressModule{now: &clock, writes: make(map[adapter.LedgerKey]int)}
		worker := adapter.Worker{Store: progressStore{AdapterRuntime: runtime, t: t}, Loader: progressLoader{runtime: runtime, module: module}, ID: "progress_worker", Now: func() time.Time { return clock }}
		for attempt := 0; attempt < 3; attempt++ {
			if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
				t.Fatalf("durable attempt %d = %v, %v; jobs=%s; completed=%v", attempt+1, worked, err, queryStrings(t, db, `SELECT id||':'||state||':'||authority_principal_id FROM adapter_outbox WHERE target_id='deadline_target_a'`), module.completed)
			}
			clock = clock.Add(4 * time.Minute)
		}
		if len(module.writes) != 3 || len(module.completed) != 3 || module.completed[0] != 0 || module.completed[1] != 1 || module.completed[2] != 2 {
			t.Fatalf("progress did not survive durable retries: writes=%v completed=%v", module.writes, module.completed)
		}
		for name, writes := range module.writes {
			if writes != 1 {
				t.Fatalf("acknowledged prefix %v rewritten %d times", name, writes)
			}
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE target_id='deadline_target_a' AND state='succeeded' AND attempt_count=3`); got != 1 {
			t.Fatal("quota-limited converge never reached success")
		}
		// The immutable witness is the loaded source, not the module's forged
		// revision 999. Presence is checked without provider values/hashes.
		revisionWitness := "CAST(json_extract(payload,'$.input_revision') AS INTEGER)"
		if db.Engine() == store.EnginePostgres {
			revisionWitness = "CAST(payload::jsonb->>'input_revision' AS BIGINT)"
		}
		if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM audit_tenant_events WHERE type='adapter.push_intent' AND org_id='org_a' AND %s>0 AND %s<>999`, revisionWitness, revisionWitness)); got != 3 {
			t.Fatalf("typed worker-bound revision witnesses=%d, want 3", got)
		}
		if _, err := runtime.Enqueue(t.Context(), adapter.Job{OrgID: "org_a", ProjectID: "prj_a1", EnvironmentID: "env_a1", TargetID: "deadline_target_a", Kind: adapter.Converge, AuthorityPrincipal: "usr_alice"}, clock); err != nil {
			t.Fatal(err)
		}
		for attempt := 0; attempt < 3; attempt++ {
			if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
				t.Fatalf("new job attempt %d = %v, %v", attempt+1, worked, err)
			}
			clock = clock.Add(4 * time.Minute)
		}
		for name, writes := range module.writes {
			if writes != 2 {
				t.Fatalf("new generation did not re-deliver %v: writes=%d", name, writes)
			}
		}
	})
}

func TestAdapterDurableProgressRequiresExactAcknowledgedRevisionAndCustody(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		publishProgressFixture(t, db)
		runtime := generatedAdapterRuntime(db)
		now := time.Now().UTC()
		job, claimed, err := runtime.ClaimDue(t.Context(), "witness_worker", now, now.Add(adapter.LeaseTime))
		if err != nil || !claimed {
			t.Fatalf("claim = %v, %v", claimed, err)
		}
		journal := runtime.Journal(job)
		if err := journal.Gate(t.Context(), adapter.Effect{Surface: adapter.Secret, EffectiveName: "manifest", Disposition: adapter.Update}); err != nil {
			t.Fatal(err)
		}
		material, err := runtime.LoadExecution(t.Context(), job)
		if err != nil || material.Revision <= 0 {
			t.Fatalf("published source = %d, %v", material.Revision, err)
		}
		for _, test := range []struct {
			name     string
			revision int64
			outcome  adapter.Outcome
			state    adapter.LedgerState
			missing  bool
		}{
			{"ACKNOWLEDGED", material.Revision, adapter.OutcomeSuccess, adapter.Owned, false},
			{"LEGACY_NO_WITNESS", 0, adapter.OutcomeSuccess, adapter.Owned, false},
			{"OTHER_REVISION", material.Revision + 1, adapter.OutcomeSuccess, adapter.Owned, false},
			{"UNKNOWN_WRITE", material.Revision, adapter.OutcomeUnknown, adapter.Dispatched, false},
			{"MISSING_WRITE", material.Revision, adapter.OutcomeSuccess, adapter.Owned, true},
			{"RELEASED_WRITE", material.Revision, adapter.OutcomeSuccess, adapter.Released, false},
		} {
			effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: test.name, Disposition: adapter.Create, InputRevision: test.revision}
			prior, err := journal.Reserve(t.Context(), effect)
			if err != nil {
				t.Fatal(err)
			}
			if err := journal.Prepare(t.Context(), effect, prior); err != nil {
				t.Fatal(err)
			}
			if err := journal.Finish(t.Context(), effect, adapter.Completion{Outcome: test.outcome, State: test.state, Missing: test.missing}); err != nil {
				t.Fatal(err)
			}
		}
		material, err = runtime.LoadExecution(t.Context(), job)
		if err != nil {
			t.Fatal(err)
		}
		if len(material.Completed) != 1 || material.Completed[0].EffectiveName != "ACKNOWLEDGED" {
			t.Fatalf("reused unproven or unowned progress: %v", material.Completed)
		}
		for _, mutate := range []func(*adapter.Job){
			func(job *adapter.Job) { job.OrgID = "org_b" },
			func(job *adapter.Job) { job.ProjectID = "prj_b1" },
			func(job *adapter.Job) { job.EnvironmentID = "env_b1" },
			func(job *adapter.Job) { job.TargetID = "deadline_target_b" },
			func(job *adapter.Job) { job.LeaseOwner = "other_worker" },
			func(job *adapter.Job) { job.Generation++ },
		} {
			foreign := job
			mutate(&foreign)
			if material, err := runtime.LoadExecution(t.Context(), foreign); err == nil || len(material.Completed) != 0 {
				t.Fatalf("foreign lease/chain reused progress: %+v, %v", foreign, err)
			}
		}
	})
}
