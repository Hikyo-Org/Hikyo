package isolation

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestAdapterRuntimeGeneratedIndeterminateEffectsPreserveLiveCustody(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		for _, effect := range []struct {
			id      string
			outcome any
		}{
			{"eff_cleanup_a", nil}, {"eff_cleanup_b", nil}, {"eff_cleanup_held", nil}, {"eff_cleanup_done", "success"},
		} {
			execRealAdoption(t, db, `INSERT INTO adapter_effects(id,org_id,project_id,environment_id,target_id,job_id,surface,effective_name,disposition,intent_audit_id,outcome,created_at) VALUES ($1,'org_gitlab','prj_gitlab','env_gitlab_e2e','tgt_gitlab_a','job_gitlab','secret',$1,'create',$1,$2,'2026-08-17T00:00:00Z')`, effect.id, effect.outcome)
		}
		at := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
		execRealAdoption(t, db, `UPDATE adapter_targets SET provider_lease_job_id='job_gitlab',provider_lease_effect_id='eff_cleanup_held',provider_lease_expires_at=$1 WHERE id='tgt_gitlab_a'`, at.Add(time.Hour).Format("2006-01-02T15:04:05.000000Z"))
		var read func(adapterQueryScope, time.Time) ([]string, error)
		if db.Engine() == store.EngineSQLite {
			q := sqlitegen.New(db.SQLiteWrite())
			read = func(scope adapterQueryScope, at time.Time) ([]string, error) {
				rows, err := q.AdapterWorkerCloseIndeterminateEffectsQuery(t.Context(), sqlitegen.AdapterWorkerCloseIndeterminateEffectsQueryParams{TargetID: "tgt_gitlab_a", ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, At: sql.NullString{String: at.Format("2006-01-02T15:04:05.000000Z"), Valid: true}})
				ids := []string{}
				for _, row := range rows {
					ids = append(ids, row.ID)
				}
				return ids, err
			}
		} else {
			q := pggen.New(db.PG())
			read = func(scope adapterQueryScope, at time.Time) ([]string, error) {
				rows, err := q.AdapterWorkerCloseIndeterminateEffectsQuery(t.Context(), pggen.AdapterWorkerCloseIndeterminateEffectsQueryParams{TargetID: "tgt_gitlab_a", ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, At: pgtype.Timestamptz{Time: at, Valid: true}})
				ids := []string{}
				for _, row := range rows {
					ids = append(ids, row.ID)
				}
				return ids, err
			}
		}
		own := adapterQueryScope{org: "org_gitlab", project: "prj_gitlab", environment: "env_gitlab_e2e"}
		if got, err := read(own, at); err != nil || !reflect.DeepEqual(got, []string{"eff_cleanup_a", "eff_cleanup_b"}) {
			t.Fatalf("own unfinished unheld effects=%v err=%v", got, err)
		}
		for _, foreign := range []adapterQueryScope{{org: "org_other", project: own.project, environment: own.environment}, {org: own.org, project: "prj_other", environment: own.environment}, {org: own.org, project: own.project, environment: "env_gitlab_second"}} {
			if got, err := read(foreign, at); err != nil || len(got) != 0 {
				t.Fatalf("foreign scope %+v effects=%v err=%v", foreign, got, err)
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM adapter_effects WHERE id IN ('eff_cleanup_a','eff_cleanup_b','eff_cleanup_held') AND outcome IS NULL`); n != 3 {
			t.Fatalf("read query changed unfinished effects: %d", n)
		}
		if held := queryString(t, db, `SELECT provider_lease_effect_id FROM adapter_targets WHERE id='tgt_gitlab_a'`); held != "eff_cleanup_held" {
			t.Fatalf("read query released custody: %q", held)
		}
		if got, err := read(own, at.Add(2*time.Hour)); err != nil || !reflect.DeepEqual(got, []string{"eff_cleanup_a", "eff_cleanup_b", "eff_cleanup_held"}) {
			t.Fatalf("expired custody effects=%v err=%v", got, err)
		}
	})
}

func TestAdapterRuntimeGeneratedCredentialErasePreservesRetainedCustody(t *testing.T) {
	for _, name := range []string{"AdapterWorkerFinishJobErase", "AdapterWorkerFinishDeadCredentialScrubErase"} {
		t.Run(name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				seedGitLabMoves(t, db, "staging")
				execRealAdoption(t, db, `UPDATE adapters SET credential_set_at='2026-08-17T00:00:00Z' WHERE id='adp_gitlab'`)
				execRealAdoption(t, db, `INSERT INTO adapter_outbox(id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ('job_cleanup_scrub','org_gitlab','prj_gitlab','env_gitlab_e2e','tgt_gitlab_a','scrub','usr_gitlab',1,'cleanup-scrub','2026-08-17T00:00:00Z','queued','2026-08-17T00:00:00Z')`)
				var erase func(adapterQueryScope) (int64, error)
				if db.Engine() == store.EngineSQLite {
					q := sqlitegen.New(db.SQLiteWrite())
					erase = func(scope adapterQueryScope) (int64, error) {
						if name == "AdapterWorkerFinishJobErase" {
							return q.AdapterWorkerFinishJobErase(t.Context(), sqlitegen.AdapterWorkerFinishJobEraseParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project})
						}
						return q.AdapterWorkerFinishDeadCredentialScrubErase(t.Context(), sqlitegen.AdapterWorkerFinishDeadCredentialScrubEraseParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project})
					}
				} else {
					q := pggen.New(db.PG())
					erase = func(scope adapterQueryScope) (int64, error) {
						if name == "AdapterWorkerFinishJobErase" {
							return q.AdapterWorkerFinishJobErase(t.Context(), pggen.AdapterWorkerFinishJobEraseParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project})
						}
						return q.AdapterWorkerFinishDeadCredentialScrubErase(t.Context(), pggen.AdapterWorkerFinishDeadCredentialScrubEraseParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project})
					}
				}
				own := adapterQueryScope{org: "org_gitlab", project: "prj_gitlab"}
				refuse := func(label string, scope adapterQueryScope) {
					t.Helper()
					if n, err := erase(scope); err != nil || n != 0 {
						t.Fatalf("%s erase rows=%d err=%v", label, n, err)
					}
					if n := queryInt(t, db, `SELECT COUNT(*) FROM adapters WHERE id='adp_gitlab' AND credential_ciphertext IS NOT NULL AND credential_set_at IS NOT NULL`); n != 1 {
						t.Fatalf("%s erased credential: %d", label, n)
					}
				}
				// Isolate each guard: the other custody predicates are eligible.
				execRealAdoption(t, db, `UPDATE adapter_targets SET state='tombstoned' WHERE adapter_id='adp_gitlab'`)
				execRealAdoption(t, db, `UPDATE adapter_outbox SET state='succeeded' WHERE id='job_cleanup_scrub'`)
				refuse("active adapter", own)
				execRealAdoption(t, db, `UPDATE adapters SET state='tombstoned' WHERE id='adp_gitlab'`)
				execRealAdoption(t, db, `UPDATE adapter_targets SET state='active' WHERE id='tgt_gitlab_a'`)
				refuse("retained active target", own)
				execRealAdoption(t, db, `UPDATE adapter_targets SET state='tombstoned' WHERE id='tgt_gitlab_a'`)
				execRealAdoption(t, db, `UPDATE adapter_outbox SET state='queued' WHERE id='job_cleanup_scrub'`)
				refuse("queued scrub", own)
				execRealAdoption(t, db, `UPDATE adapter_outbox SET state='running' WHERE id='job_cleanup_scrub'`)
				refuse("running scrub", own)
				execRealAdoption(t, db, `UPDATE adapter_outbox SET state='succeeded' WHERE id='job_cleanup_scrub'`)
				refuse("foreign org", adapterQueryScope{org: "org_other", project: own.project})
				refuse("foreign project", adapterQueryScope{org: own.org, project: "prj_other"})
				if n, err := erase(own); err != nil || n != 1 {
					t.Fatalf("settled owning erase rows=%d err=%v", n, err)
				}
				if n := queryInt(t, db, `SELECT COUNT(*) FROM adapters WHERE id='adp_gitlab' AND credential_ciphertext IS NULL AND credential_set_at IS NULL`); n != 1 {
					t.Fatalf("settled cleanup did not erase credential: %d", n)
				}
			})
		})
	}
}

func TestAdapterRuntimeGeneratedFinishJobMarksOnlyScopedMoveTargets(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		if err := gitLabMoveWrite(t, db, func(ctx context.Context, repos store.Repos, proof authz.Proof) error {
			_, err := repos.Adapters().MoveTarget(ctx, proof, store.AdapterRouteMoveMutation{MoveID: "arm_cleanup", Target: gitLabPendingTarget("tgt_gitlab_a", "production", "new-api"), ExpectedGeneration: 1, AuthorityPrincipalID: "usr_gitlab", KeepRemote: true, At: time.Now().UTC()})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		execRealAdoption(t, db, `UPDATE adapter_targets SET state='moving' WHERE id='tgt_gitlab_b'`)
		execRealAdoption(t, db, `INSERT INTO adapter_route_moves(id,org_id,project_id,adapter_id,target_id,kind,authority_principal_id,state,keep_remote,created_at) VALUES ('arm_cleanup_other','org_gitlab','prj_gitlab','adp_gitlab','tgt_gitlab_b','target','usr_gitlab','completed',TRUE,'2026-08-17T00:00:00Z')`)
		execRealAdoption(t, db, `INSERT INTO adapter_route_move_targets(move_id,org_id,project_id,environment_id,target_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix) SELECT 'arm_cleanup_other',org_id,project_id,environment_id,id,destination_kind,destination_owner,destination_name,destination_id,name_prefix FROM adapter_targets WHERE id='tgt_gitlab_b'`)

		snapshot := func(id string) string {
			return queryString(t, db, `SELECT sync_status||'|'||CAST(failure_names AS TEXT)||'|'||COALESCE(active_job_id,'') FROM adapter_targets WHERE id='`+id+`'`)
		}
		beforeA, beforeB := snapshot("tgt_gitlab_a"), snapshot("tgt_gitlab_b")
		var mark func(adapterQueryScope, string) (int64, error)
		if db.Engine() == store.EngineSQLite {
			q := sqlitegen.New(db.SQLiteWrite())
			mark = func(scope adapterQueryScope, move string) (int64, error) {
				return q.AdapterWorkerFinishJobMarkTargets(t.Context(), sqlitegen.AdapterWorkerFinishJobMarkTargetsParams{ChainOrg: scope.org, ChainProject: scope.project, RouteMoveID: move})
			}
		} else {
			q := pggen.New(db.PG())
			mark = func(scope adapterQueryScope, move string) (int64, error) {
				return q.AdapterWorkerFinishJobMarkTargets(t.Context(), pggen.AdapterWorkerFinishJobMarkTargetsParams{ChainOrg: scope.org, ChainProject: scope.project, RouteMoveID: move})
			}
		}
		own := adapterQueryScope{org: "org_gitlab", project: "prj_gitlab"}
		for _, scope := range []adapterQueryScope{{org: "org_other", project: own.project}, {org: own.org, project: "prj_other"}} {
			if n, err := mark(scope, "arm_cleanup"); err != nil || n != 0 {
				t.Fatalf("foreign scope %+v mark rows=%d err=%v", scope, n, err)
			}
		}
		if n, err := mark(own, "arm_absent"); err != nil || n != 0 {
			t.Fatalf("unrelated move mark rows=%d err=%v", n, err)
		}
		if snapshot("tgt_gitlab_a") != beforeA || snapshot("tgt_gitlab_b") != beforeB {
			t.Fatal("refused scope or move mutated target state")
		}
		if n, err := mark(own, "arm_cleanup"); err != nil || n != 1 {
			t.Fatalf("owning move mark rows=%d err=%v", n, err)
		}
		if got := queryString(t, db, `SELECT sync_status FROM adapter_targets WHERE id='tgt_gitlab_a'`); got != "failed" {
			t.Fatalf("owning status=%q", got)
		}
		var names []string
		if err := json.Unmarshal([]byte(queryString(t, db, `SELECT CAST(failure_names AS TEXT) FROM adapter_targets WHERE id='tgt_gitlab_a'`)), &names); err != nil || !reflect.DeepEqual(names, []string{"route"}) {
			t.Fatalf("owning failure names=%v err=%v", names, err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_gitlab_a' AND active_job_id IS NULL`); n != 1 {
			t.Fatal("owning active job was not cleared")
		}
		if snapshot("tgt_gitlab_b") != beforeB {
			t.Fatal("unrelated moving target changed")
		}
	})
}
