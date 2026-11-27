package conformance

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func init() {
	corpus = append(corpus, scenario{"adapter_scrub_revision_survives_pre_settlement_delete", scenarioAdapterScrubBeforeSettlement})
}

func scenarioAdapterScrubBeforeSettlement(t *testing.T, db *store.DB) {
	for index, attempt := range []struct {
		name       string
		completion *adapter.Completion
	}{
		{"success", &adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned}},
		{"unknown", &adapter.Completion{Outcome: adapter.OutcomeUnknown, State: adapter.Dispatched}},
		{"crash_before_finish", nil},
	} {
		t.Run(attempt.name, func(t *testing.T) {
			label := fmt.Sprintf("scrubpresettle%d", index)
			who, scope, values, envs, keys := valueFixture(t, db, label)
			actor := service.LocalPrincipal(who)
			grantOrg(t, db, who, scope.Org, label+"adapter", "manage-adapters")
			env := mustEnv(t, envs, actor, scope, "prod")
			key := mustKey(t, keys, actor, scope, "TOKEN", string(schema.Secret), schema.DefaultPresenceRules())
			publishValue(t, db, values, actor, env, "TOKEN", "delivered-secret")
			adapterID, targetID, jobID := "adp_"+label, "tgt_"+label, "job_"+label
			sealer, err := sharedKeyring(t, db).ForProject(t.Context(), string(scope.Org), string(scope.Project))
			if err != nil {
				t.Fatal(err)
			}
			credential, err := sealer.SealField(adapter.CredentialAAD(string(scope.Org), string(scope.Project), adapterID), []byte("provider-credential"))
			if err != nil {
				t.Fatal(err)
			}
			execConformance(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,credential_ciphertext,created_at) VALUES ($1,$2,$3,'sealed-webhook','https://receiver.example',$4,'active',$5,'2026-08-17T00:00:00Z')`, adapterID, string(scope.Org), string(scope.Project), string(who), credential)
			execConformance(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,active_job_id,created_at) VALUES ($1,$2,$3,$4,$5,'organization','payments','',4201,'',1,'active','converging',$6,'2026-08-17T00:00:00Z')`, targetID, string(scope.Org), string(scope.Project), string(env.Env), adapterID, jobID)
			execConformance(t, db, `INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ($1,$2,$3,$4,$5,$6)`, string(scope.Org), string(scope.Project), string(env.Env), targetID, adapterID, key.ID)
			execConformance(t, db, `INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,lease_owner,lease_expires_at,created_at) VALUES ($1,$2,$3,$4,$5,'converge',$6,1,$5,1,'2026-08-17T00:00:00Z','running','worker_before_delete','2099-08-17T00:00:00Z','2026-08-17T00:00:00Z')`, jobID, string(scope.Org), string(scope.Project), string(env.Env), targetID, string(who))
			runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
			job := adapter.Job{ID: jobID, OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvironmentID: string(env.Env), TargetID: targetID, Kind: adapter.Converge, AuthorityPrincipal: string(who), Generation: 1, LeaseOwner: "worker_before_delete"}
			loaded, err := runtime.LoadExecution(t.Context(), job)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Revision <= 0 || len(loaded.Entries) != 1 {
				t.Fatalf("pinned source revision=%d entries=%d", loaded.Revision, len(loaded.Entries))
			}
			effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "TOKEN", Disposition: adapter.Create, KeyID: loaded.Entries[0].KeyID}
			journal := runtime.Journal(job)
			state, err := journal.Reserve(t.Context(), effect)
			if err != nil {
				t.Fatal(err)
			}
			if err := journal.Prepare(t.Context(), effect, state); err != nil {
				t.Fatal(err)
			}
			if attempt.completion != nil {
				if err := journal.Finish(t.Context(), effect, *attempt.completion); err != nil {
					t.Fatal(err)
				}
			} else {
				// Simulate delivery followed by process death before Finish. Once
				// the provider fence expires, deletion and the next claim recover
				// the dispatched ledger without a worker settlement ever landing.
				execConformance(t, db, `UPDATE adapter_targets SET provider_lease_expires_at='2026-08-17T00:00:00Z' WHERE id=$1 AND org_id=$2 AND project_id=$3 AND environment_id=$4`, targetID, string(scope.Org), string(scope.Project), string(env.Env))
			}
			// Delete wins after durable remote-effect completion releases its
			// fence, before the worker settles the initial job's revision.
			removed, err := (&service.Adapters{DB: db}).RemoveTarget(t.Context(), actor, scope, targetID, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(removed.Targets) != 1 || removed.Targets[0].SupersededJobID != jobID {
				t.Fatalf("teardown = %+v", removed)
			}
			if err := runtime.Succeed(t.Context(), job, loaded.Revision, nil, time.Now().UTC()); !errors.Is(err, adapter.ErrSuperseded) {
				t.Fatalf("superseded settlement = %v", err)
			}
			now := time.Now().UTC()
			scrub, ok, err := runtime.ClaimDue(t.Context(), "worker_scrub", now, now.Add(adapter.LeaseTime))
			if err != nil || !ok || scrub.Kind != adapter.Scrub || scrub.TargetID != targetID {
				t.Fatalf("scrub claim = %+v, %t, %v", scrub, ok, err)
			}
			material, err := runtime.LoadExecution(t.Context(), scrub)
			if err != nil {
				t.Fatal(err)
			}
			if material.Revision != loaded.Revision || len(material.Entries) != 0 || len(material.Ledger) != 1 {
				t.Fatalf("scrub lost trusted pinned revision or loaded snapshot material: revision=%d entries=%d ledger=%+v", material.Revision, len(material.Entries), material.Ledger)
			}
			prune := adapter.Effect{Surface: adapter.Secret, EffectiveName: "TOKEN", Disposition: adapter.Delete}
			scrubJournal := runtime.Journal(scrub)
			if err := scrubJournal.Prepare(t.Context(), prune, material.Ledger[0].State); err != nil {
				t.Fatal(err)
			}
			if err := scrubJournal.Finish(t.Context(), prune, adapter.Completion{Outcome: adapter.OutcomeSuccess, ReleaseLedger: true}); err != nil {
				t.Fatal(err)
			}
			if err := runtime.Succeed(t.Context(), scrub, material.Revision, nil, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
