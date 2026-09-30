package isolation

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

// Invoke generated owners directly so no Gate or repository preflight can
// conceal a missing query predicate. Every supported tenant coordinate is
// changed independently while durable identities stay constant.
func TestAdapterRuntimeDirectGeneratedQueriesBindOwningChain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		observed := time.Now().UTC()
		if err := gitLabMoveWrite(t, db, func(ctx context.Context, repos store.Repos, proof authz.Proof) error {
			_, err := repos.Adapters().MoveTarget(ctx, proof, store.AdapterRouteMoveMutation{MoveID: "arm_direct_runtime", Target: gitLabPendingTarget("tgt_gitlab_a", "production", "new-api"), ExpectedGeneration: 1, AuthorityPrincipalID: "usr_gitlab", KeepRemote: true, At: observed})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		runtime := generatedAdapterRuntime(db)
		job, ok, err := runtime.ClaimDue(t.Context(), "direct-runtime", observed.Add(time.Second), observed.Add(time.Minute))
		if err != nil || !ok {
			t.Fatalf("claim: %+v %v %v", job, ok, err)
		}
		for _, statement := range []string{
			`INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES ('snap_runtime','org_gitlab','prj_gitlab','env_gitlab_e2e',1,1,'usr_gitlab','2026-08-17T00:00:00Z')`,
			`INSERT INTO snapshot_entries(id,org_id,project_id,environment_id,snapshot_id,key_id,key_name,classification,ciphertext,value_entry_id) VALUES ('sen_runtime','org_gitlab','prj_gitlab','env_gitlab_e2e','snap_runtime','key_gitlab_move','TOKEN','secret','unused','val_runtime')`,
			`UPDATE adapter_targets SET destination_name='api-a',destination_id=42 WHERE id='tgt_gitlab_b'`,
			`UPDATE adapter_route_move_claims SET destination_name='api-a' WHERE move_id='arm_direct_runtime'`,
		} {
			execRealAdoption(t, db, statement)
		}

		// Reserve does not require an activation provider call; these two scoped
		// records isolate FinishUpdate and ReleaseReservationRemove positive effects.
		for _, name := range []string{"P_TOKEN", "RELEASE"} {
			_, err := runtime.Journal(job).Reserve(t.Context(), adapter.Effect{Surface: adapter.Secret, EffectiveName: name, Disposition: adapter.Create})
			if err != nil {
				t.Fatal(err)
			}
		}
		var queries []adapterScopeQuery
		var collision func(string, string, int64) (int64, error)
		if db.Engine() == store.EngineSQLite {
			q := sqlitegen.New(db.SQLiteWrite())
			stamp := observed.Format(time.RFC3339Nano)
			nullableStamp := sql.NullString{String: stamp, Valid: true}
			nullableOwner := sql.NullString{String: job.LeaseOwner, Valid: true}
			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerActivateLookup", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterWorkerActivateLookup(ctx, sqlitegen.AdapterWorkerActivateLookupParams{RouteMoveID: "arm_direct_runtime", TargetID: "tgt_gitlab_a", ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, Generation: job.Generation}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerLoadExecutionQuery", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterWorkerLoadExecutionQuery(ctx, sqlitegen.AdapterWorkerLoadExecutionQueryParams{JobID: job.ID, TargetID: "tgt_gitlab_a", ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, Generation: job.Generation, LeaseOwner: nullableOwner}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerLoadExecutionEntryQuery", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				result, err := q.AdapterWorkerLoadExecutionEntryQuery(ctx, sqlitegen.AdapterWorkerLoadExecutionEntryQueryParams{TargetID: "tgt_gitlab_a", SnapshotID: "snap_runtime", ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment})
				return len(result), err
			}})

			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerReservePendingQuery", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				result, err := q.AdapterWorkerReservePendingQuery(ctx, sqlitegen.AdapterWorkerReservePendingQueryParams{TargetID: "tgt_gitlab_b", ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, Surface: "secret", Normalized: "P_TOKEN"})
				return int(result), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerReleaseReservationRemove", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				result, err := q.AdapterWorkerReleaseReservationRemove(ctx, sqlitegen.AdapterWorkerReleaseReservationRemoveParams{ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, TargetID: "tgt_gitlab_a", Surface: "secret", NormalizedName: "RELEASE", Generation: job.Generation, JobID: job.ID, LeaseOwner: nullableOwner, Now: nullableStamp})
				return int(result), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerFinishUpdate", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				result, err := q.AdapterWorkerFinishUpdate(ctx, sqlitegen.AdapterWorkerFinishUpdateParams{State: "owned", Missing: 0, Now: stamp, ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, TargetID: "tgt_gitlab_a", Surface: "secret", NormalizedName: "P_TOKEN"})
				return int(result), err
			}})

			collision = func(move, target string, destination int64) (int64, error) {
				n, err := q.AdapterWorkerActivateCollisionQuery(t.Context(), sqlitegen.AdapterWorkerActivateCollisionQueryParams{RouteMoveID: move, TargetID: target, PendingOrigin: "https://gitlab.old.example", Kind: "repository", RepositoryID: 0, DestinationID: destination, SentinelName: adapter.SentinelName})
				return int64(n), err
			}
		} else {
			q := pggen.New(db.PG())
			stamp := pgtype.Timestamptz{Time: observed, Valid: true}
			nullableStamp := stamp
			nullableOwner := pgtype.Text{String: job.LeaseOwner, Valid: true}
			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerActivateLookup", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterWorkerActivateLookup(ctx, pggen.AdapterWorkerActivateLookupParams{RouteMoveID: "arm_direct_runtime", TargetID: "tgt_gitlab_a", ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, Generation: job.Generation}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerLoadExecutionQuery", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterWorkerLoadExecutionQuery(ctx, pggen.AdapterWorkerLoadExecutionQueryParams{JobID: job.ID, TargetID: "tgt_gitlab_a", ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, Generation: job.Generation, LeaseOwner: nullableOwner}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerLoadExecutionEntryQuery", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				result, err := q.AdapterWorkerLoadExecutionEntryQuery(ctx, pggen.AdapterWorkerLoadExecutionEntryQueryParams{TargetID: "tgt_gitlab_a", SnapshotID: "snap_runtime", ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment})
				return len(result), err
			}})

			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerReservePendingQuery", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				result, err := q.AdapterWorkerReservePendingQuery(ctx, pggen.AdapterWorkerReservePendingQueryParams{TargetID: "tgt_gitlab_b", ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, Surface: "secret", Normalized: "P_TOKEN"})
				return int(result), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerReleaseReservationRemove", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				result, err := q.AdapterWorkerReleaseReservationRemove(ctx, pggen.AdapterWorkerReleaseReservationRemoveParams{ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, TargetID: "tgt_gitlab_a", Surface: "secret", NormalizedName: "RELEASE", Generation: job.Generation, JobID: job.ID, LeaseOwner: nullableOwner, Now: nullableStamp})
				return int(result), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterWorkerFinishUpdate", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				result, err := q.AdapterWorkerFinishUpdate(ctx, pggen.AdapterWorkerFinishUpdateParams{State: "owned", Missing: false, Now: stamp, ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment, TargetID: "tgt_gitlab_a", Surface: "secret", NormalizedName: "P_TOKEN"})
				return int(result), err
			}})

			collision = func(move, target string, destination int64) (int64, error) {
				n, err := q.AdapterWorkerActivateCollisionQuery(t.Context(), pggen.AdapterWorkerActivateCollisionQueryParams{RouteMoveID: move, TargetID: target, PendingOrigin: "https://gitlab.old.example", Kind: "repository", RepositoryID: 0, DestinationID: destination, SentinelName: adapter.SentinelName})
				return int64(n), err
			}
		}

		owning := adapterQueryScope{org: "org_gitlab", project: "prj_gitlab", environment: "env_gitlab_e2e"}
		snapshot := func() []string {
			return []string{
				queryString(t, db, "SELECT state||':'||sync_status FROM adapter_targets WHERE id='tgt_gitlab_a'"),
				fmt.Sprint(queryInt(t, db, "SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_gitlab_a' AND state='reserved'")),
				fmt.Sprint(queryInt(t, db, "SELECT COUNT(*) FROM adapter_effects WHERE outcome IS NULL")),
				fmt.Sprint(queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events")),
			}
		}
		for _, query := range queries {
			t.Run(query.name, func(t *testing.T) {
				scope := owning
				if query.name == "AdapterWorkerReservePendingQuery" {
					scope.environment = "env_gitlab_second"
				}
				before := snapshot()
				for _, coordinate := range []string{"org", "project", "environment"} {
					if coordinate == "environment" && !query.environment {
						continue
					}
					foreign := scope
					switch coordinate {
					case "org":
						foreign.org = "org_b"
					case "project":
						foreign.project = "prj_a1"
					case "environment":
						foreign.environment = "env_gitlab_second"
						if scope.environment == "env_gitlab_second" {
							foreign.environment = "env_gitlab_e2e"
						}
					}
					n, err := query.read(t.Context(), foreign)
					if err != nil || n != 0 {
						t.Fatalf("%s refusal=%d %v", coordinate, n, err)
					}
					if after := snapshot(); !reflect.DeepEqual(before, after) {
						t.Fatalf("%s refusal wrote: %v -> %v", coordinate, before, after)
					}
				}
				n, err := query.read(t.Context(), scope)
				if err != nil || n < 1 {
					t.Fatalf("owning positive=%d %v", n, err)
				}
			})
		}
		// This global namespace count accepts durable move/target identity rather
		// than tenant coordinates; foreign identities must reveal no memberships.
		for _, identity := range [][2]string{{"arm_foreign", "tgt_gitlab_a"}, {"arm_direct_runtime", "tgt_foreign"}} {
			n, err := collision(identity[0], identity[1], 42)
			if err != nil || n != 0 {
				t.Fatalf("foreign collision membership: %d %v", n, err)
			}
		}
		if n, err := collision("arm_direct_runtime", "tgt_gitlab_a", 42); err != nil || n < 1 {
			t.Fatalf("shared namespace collision positive: %d %v", n, err)
		}
		// Custody on a resolved remote destination spans tenants. This count
		// must detect a foreign ledger without returning its tenant payload.
		for _, statement := range []string{
			`INSERT INTO orgs(id,name,active,metadata,created_at) VALUES ('org_runtime_foreign','Foreign',TRUE,'{}','2026-08-17T00:00:00Z')`,
			`INSERT INTO projects(id,org_id,name,created_at) VALUES ('prj_runtime_foreign','org_runtime_foreign','Foreign','2026-08-17T00:00:00Z')`,
			`INSERT INTO environments(id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_runtime_foreign','org_runtime_foreign','prj_runtime_foreign','foreign','','2026-08-17T00:00:00Z',0)`,
			`INSERT INTO adapters(id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_runtime_foreign','org_runtime_foreign','prj_runtime_foreign','gitlab','https://gitlab.old.example','usr_gitlab','active','2026-08-17T00:00:00Z')`,
			`INSERT INTO adapter_targets(id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_runtime_foreign','org_runtime_foreign','prj_runtime_foreign','env_runtime_foreign','adp_runtime_foreign','repository','team','api-a',77,'P_',1,'active','never','2026-08-17T00:00:00Z')`,
			`INSERT INTO adapter_ledger(id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,destination_id,repository_id,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_runtime_foreign','org_runtime_foreign','prj_runtime_foreign','env_runtime_foreign','tgt_runtime_foreign','https://gitlab.old.example','repository',77,0,'secret','P_TOKEN','P_TOKEN','owned','2026-08-17T00:00:00Z')`,
		} {
			execRealAdoption(t, db, statement)
		}
		if n, err := collision("arm_direct_runtime", "tgt_gitlab_a", 77); err != nil || n < 1 {
			t.Fatalf("foreign tenant custody collision: %d %v", n, err)
		}
		execRealAdoption(t, db, `UPDATE adapter_ledger SET state='released' WHERE id='led_runtime_foreign'`)
		if n, err := collision("arm_direct_runtime", "tgt_gitlab_a", 77); err != nil || n != 0 {
			t.Fatalf("released foreign custody collision: %d %v", n, err)
		}
		// Replace the local pending claim with an independently owned foreign
		// pending move in the same remote namespace.
		for _, statement := range []string{
			`DELETE FROM adapter_route_move_claims WHERE move_id='arm_direct_runtime'`,
			`INSERT INTO adapter_route_moves(id,org_id,project_id,adapter_id,target_id,kind,authority_principal_id,state,keep_remote,created_at) VALUES ('arm_runtime_foreign','org_runtime_foreign','prj_runtime_foreign','adp_runtime_foreign','tgt_runtime_foreign','target','usr_gitlab','activating',TRUE,'2026-08-17T00:00:00Z')`,
			`INSERT INTO adapter_route_move_targets(move_id,org_id,project_id,environment_id,target_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix) VALUES ('arm_runtime_foreign','org_runtime_foreign','prj_runtime_foreign','env_runtime_foreign','tgt_runtime_foreign','repository','team','api-a',77,'P_')`,
			`INSERT INTO adapter_route_move_claims(move_id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,destination_owner,destination_name,surface,effective_name,normalized_name) VALUES ('arm_runtime_foreign','org_runtime_foreign','prj_runtime_foreign','env_runtime_foreign','tgt_runtime_foreign','https://gitlab.old.example','repository','team','api-a','secret','P_TOKEN','P_TOKEN')`,
		} {
			execRealAdoption(t, db, statement)
		}
		for _, query := range queries {
			if query.name == "AdapterWorkerReservePendingQuery" {
				scope := adapterQueryScope{org: "org_gitlab", project: "prj_gitlab", environment: "env_gitlab_second"}
				if n, err := query.read(t.Context(), scope); err != nil || n < 1 {
					t.Fatalf("foreign tenant pending namespace: %d %v", n, err)
				}
			}
		}

	})
}
