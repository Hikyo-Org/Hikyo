package isolation

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func generatedAdapterRuntime(db *store.DB) *store.AdapterRuntime {
	return store.NewAdapterRuntime(db, func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
		return storetx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, domain.Scope{Org: domain.OrgID(job.OrgID), Project: domain.ProjectID(job.ProjectID), Env: domain.EnvID(job.EnvironmentID)})
			return err
		})
	})
}

// Read and activation independently repeat the claimed job's full chain,
// identities, owner and generation. A pending/current environment mismatch is
// a refused identity change, and even the initial job-finish write rolls back.
func TestAdapterRuntimeGeneratedActivationScopeAndEnvironmentBridge(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		now := time.Now().UTC()
		pending := gitLabPendingTarget("tgt_gitlab_a", "production", "generated-route")
		var result store.AdapterRouteMoveResult
		if err := gitLabMoveWrite(t, db, func(ctx context.Context, repos store.Repos, proof authz.Proof) error {
			var err error
			result, err = repos.Adapters().MoveTarget(ctx, proof, store.AdapterRouteMoveMutation{MoveID: "arm_runtime_scope", Target: pending, ExpectedGeneration: 1, AuthorityPrincipalID: "usr_gitlab", KeepRemote: true, At: now})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		runtime := generatedAdapterRuntime(db)
		job, ok, err := runtime.ClaimDue(t.Context(), "generated-worker", now.Add(time.Second), now.Add(time.Minute))
		if err != nil || !ok || job.ID != result.JobID || job.Kind != adapter.Activate {
			t.Fatalf("claim activation: %+v %v %v", job, ok, err)
		}
		if err := runtime.Journal(job).Gate(t.Context(), adapter.Effect{}); err != nil {
			t.Fatalf("actual authority gate: %v", err)
		}
		loaded, err := runtime.LoadActivation(t.Context(), job)
		if err != nil || loaded.Target.Environment != job.EnvironmentID || loaded.Target.Destination.Name != pending.DestinationName {
			t.Fatalf("scoped activation read: %+v %v", loaded, err)
		}
		snapshot := func() []string {
			return []string{
				queryString(t, db, fmt.Sprintf("SELECT state FROM adapter_outbox WHERE id='%s'", job.ID)),
				queryString(t, db, "SELECT state||':'||environment_id FROM adapter_targets WHERE id='tgt_gitlab_a'"),
				queryString(t, db, "SELECT state FROM adapter_route_moves WHERE id='arm_runtime_scope'"),
				fmt.Sprint(queryInt(t, db, "SELECT generation FROM adapter_targets WHERE id='tgt_gitlab_a'")),
				fmt.Sprint(queryInt(t, db, "SELECT COUNT(*) FROM adapter_outbox WHERE route_move_id='arm_runtime_scope'")),
				fmt.Sprint(queryInt(t, db, "SELECT COUNT(*) FROM adapter_target_keys WHERE target_id='tgt_gitlab_a'")),
				fmt.Sprint(queryInt(t, db, "SELECT COUNT(*) FROM adapter_route_move_claims WHERE move_id='arm_runtime_scope'")),
				fmt.Sprint(queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events")),
			}
		}
		before := snapshot()
		connection := adapter.Connection{Version: "fixture-approved", DestinationID: 77}
		for name, mutate := range map[string]func(*adapter.Job){
			"org": func(j *adapter.Job) { j.OrgID = "org_b" }, "project": func(j *adapter.Job) { j.ProjectID = "prj_move_other" },
			"environment": func(j *adapter.Job) { j.EnvironmentID = "env_gitlab_second" }, "target": func(j *adapter.Job) { j.TargetID = "tgt_gitlab_b" },
			"job": func(j *adapter.Job) { j.ID = "job_other" }, "move": func(j *adapter.Job) { j.RouteMoveID = "arm_other" },
			"generation": func(j *adapter.Job) { j.Generation++ }, "owner": func(j *adapter.Job) { j.LeaseOwner = "other-worker" },
		} {
			forged := job
			mutate(&forged)
			if _, err := runtime.LoadActivation(t.Context(), forged); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("%s forged read: %v", name, err)
			}
			if err := runtime.Activate(t.Context(), forged, connection, now.Add(2*time.Second)); !errors.Is(err, adapter.ErrSuperseded) {
				t.Fatalf("%s forged activation: %v", name, err)
			}
			if after := snapshot(); !reflect.DeepEqual(before, after) {
				t.Fatalf("%s refusal wrote state/audit: before=%v after=%v", name, before, after)
			}
		}
		// Composite foreign keys refuse a pending/current environment identity
		// mismatch before it can reach the worker, retaining the original bridge.
		if err := execRawErr(t, db, "UPDATE adapter_route_move_targets SET environment_id='env_gitlab_second' WHERE move_id='arm_runtime_scope'"); err == nil {
			t.Fatal("environment identity bridge accepted a different target environment")
		}
		if after := snapshot(); !reflect.DeepEqual(before, after) {
			t.Fatalf("environment refusal wrote state/audit: %v -> %v", before, after)
		}
		if err := runtime.Activate(t.Context(), job, connection, now.Add(2*time.Second)); err != nil {
			t.Fatalf("valid same-environment activation: %v", err)
		}
		if state := queryString(t, db, "SELECT state FROM adapter_route_moves WHERE id='arm_runtime_scope'"); state != "completed" {
			t.Fatalf("move state=%q", state)
		}
		if generation := queryInt(t, db, "SELECT generation FROM adapter_targets WHERE id='tgt_gitlab_a'"); generation != job.Generation+1 {
			t.Fatalf("activation CAS generation=%d", generation)
		}
	})
}

func TestAdapterRuntimeGeneratedJournalScopeAndLeaseRefusal(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		runtime := generatedAdapterRuntime(db)
		now := time.Now().UTC()
		_, err := runtime.Enqueue(t.Context(), adapter.Job{OrgID: "org_gitlab", ProjectID: "prj_gitlab", EnvironmentID: "env_gitlab_e2e", TargetID: "tgt_gitlab_a", Kind: adapter.Converge, AuthorityPrincipal: "usr_gitlab"}, now)
		if err != nil {
			t.Fatal(err)
		}
		job, ok, err := runtime.ClaimDue(t.Context(), "scope-journal", now.Add(time.Second), now.Add(time.Minute))
		if err != nil || !ok {
			t.Fatalf("claim: %+v %v %v", job, ok, err)
		}
		if _, err := runtime.LoadExecution(t.Context(), job); err != nil {
			t.Fatalf("valid execution: %v", err)
		}
		effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_TOKEN", Disposition: adapter.Create, KeyID: "key_gitlab_move"}
		journal := runtime.Journal(job)
		if err := journal.Gate(t.Context(), effect); err != nil {
			t.Fatalf("actual authority: %v", err)
		}
		state, err := journal.Reserve(t.Context(), effect)
		if err != nil || state != adapter.Reserved {
			t.Fatalf("reserve: %s %v", state, err)
		}
		snapshot := func() []string {
			return []string{
				queryString(t, db, "SELECT state FROM adapter_ledger WHERE target_id='tgt_gitlab_a' AND normalized_name='P_TOKEN'"),
				fmt.Sprint(queryInt(t, db, "SELECT COUNT(*) FROM adapter_effects")),
				fmt.Sprint(queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events")),
				queryString(t, db, fmt.Sprintf("SELECT state FROM adapter_outbox WHERE id='%s'", job.ID)),
			}
		}
		before := snapshot()
		for name, mutate := range map[string]func(*adapter.Job){
			"org": func(j *adapter.Job) { j.OrgID = "org_b" }, "project": func(j *adapter.Job) { j.ProjectID = "prj_other" },
			"environment": func(j *adapter.Job) { j.EnvironmentID = "env_gitlab_second" }, "target": func(j *adapter.Job) { j.TargetID = "tgt_gitlab_b" },
			"generation": func(j *adapter.Job) { j.Generation++ }, "owner": func(j *adapter.Job) { j.LeaseOwner = "other-worker" },
		} {
			forged := job
			mutate(&forged)
			if _, err := runtime.LoadExecution(t.Context(), forged); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("%s execution: %v", name, err)
			}
			if err := runtime.Journal(forged).ReleaseReservation(t.Context(), effect); !errors.Is(err, adapter.ErrSuperseded) {
				t.Fatalf("%s release: %v", name, err)
			}
			if err := runtime.Succeed(t.Context(), forged, 0, nil, now.Add(2*time.Second)); !errors.Is(err, adapter.ErrSuperseded) {
				t.Fatalf("%s finish: %v", name, err)
			}
			if after := snapshot(); !reflect.DeepEqual(before, after) {
				t.Fatalf("%s refusal wrote: %v -> %v", name, before, after)
			}
		}
		if err := journal.Prepare(t.Context(), effect, state); err != nil {
			t.Fatal(err)
		}
		if err := journal.Finish(t.Context(), effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned, ProviderStatus: 201}); err != nil {
			t.Fatal(err)
		}
		if state := queryString(t, db, "SELECT state FROM adapter_ledger WHERE target_id='tgt_gitlab_a' AND normalized_name='P_TOKEN'"); state != "owned" {
			t.Fatalf("ledger=%s", state)
		}
		if err := runtime.Succeed(t.Context(), job, 0, nil, now.Add(2*time.Second)); err != nil {
			t.Fatalf("valid finish: %v", err)
		}
	})
}
