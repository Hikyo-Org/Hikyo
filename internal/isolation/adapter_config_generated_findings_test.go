package isolation

import (
	"context"
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestAdapterConfigGeneratedFindingsLatestCompletedGeneration(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		for _, job := range []struct {
			id, target, env string
			generation      int
		}{
			{"job_findings", "tgt_gitlab_a", "env_gitlab_e2e", 1},
			{"job_findings_next", "tgt_gitlab_a", "env_gitlab_e2e", 2},
			{"job_findings_other", "tgt_gitlab_b", "env_gitlab_second", 1},
		} {
			execRealAdoption(t, db, `INSERT INTO adapter_outbox(id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ($1,'org_gitlab','prj_gitlab',$2,$3,'converge','usr_gitlab',$4,$1,'2026-08-17T00:00:00Z','succeeded','2026-08-17T00:00:00Z')`, job.id, job.env, job.target, job.generation)
		}
		for _, effect := range []struct {
			id, job, target, env, surface, name, finding string
			outcome                                      any
		}{
			{"eff_a", "job_findings", "tgt_gitlab_a", "env_gitlab_e2e", "secret", "P_TOKEN", "possible_capture", "failure"},
			// Equal completion times exercise the deterministic ID tie breaker and case folding.
			{"eff_b", "job_findings", "tgt_gitlab_a", "env_gitlab_e2e", "secret", "p_token", "", "success"},
			// An unfinished effect cannot conceal the most recent completed finding.
			{"eff_c", "job_findings", "tgt_gitlab_a", "env_gitlab_e2e", "variable", "P_TOKEN", "owned_missing", "failure"},
			{"eff_d", "job_findings", "tgt_gitlab_a", "env_gitlab_e2e", "variable", "p_token", "", nil},
			{"eff_e", "job_findings_next", "tgt_gitlab_a", "env_gitlab_e2e", "secret", "NEXT", "crash_window", "unknown"},
			{"eff_f", "job_findings_other", "tgt_gitlab_b", "env_gitlab_second", "secret", "OTHER", "possible_capture", "failure"},
		} {
			execRealAdoption(t, db, `INSERT INTO adapter_effects(id,org_id,project_id,environment_id,target_id,job_id,surface,effective_name,disposition,intent_audit_id,outcome,created_at,finished_at,finding) VALUES ($1,'org_gitlab','prj_gitlab',$2,$3,$4,$5,$6,'create',$1,$7,'2026-08-17T00:00:00Z','2026-08-17T00:01:00Z',$8)`, effect.id, effect.env, effect.target, effect.job, effect.surface, effect.name, effect.outcome, effect.finding)
		}
		read := func() []store.AdapterFinding {
			var findings []store.AdapterFinding
			err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
				proof, err := az.Authorize(ctx, authz.Identity{Principal: "usr_gitlab"}, authz.OpAdapterInspect, domain.Scope{Org: "org_gitlab", Project: "prj_gitlab"})
				if err != nil {
					return err
				}
				target, err := repos.Adapters().Target(ctx, proof, "tgt_gitlab_a")
				findings = target.Findings
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			return findings
		}
		if got, want := read(), []store.AdapterFinding{{Surface: "variable", EffectiveName: "P_TOKEN", Finding: "owned_missing"}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("current findings=%+v want %+v", got, want)
		}
		execRealAdoption(t, db, `UPDATE adapter_targets SET generation=2 WHERE id='tgt_gitlab_a'`)
		if got, want := read(), []store.AdapterFinding{{Surface: "secret", EffectiveName: "NEXT", Finding: "crash_window"}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("next generation findings=%+v want %+v", got, want)
		}
		execRealAdoption(t, db, `UPDATE adapter_targets SET generation=3 WHERE id='tgt_gitlab_a'`)
		if got := read(); got == nil || len(got) != 0 {
			t.Fatalf("new generation findings=%+v want nonnil empty", got)
		}
	})
}
