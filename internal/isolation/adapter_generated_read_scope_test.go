package isolation

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type adapterQueryScope struct{ org, project, environment string }
type adapterScopeQuery struct {
	name        string
	environment bool
	aws         bool
	read        func(context.Context, adapterQueryScope) (int, error)
}

func TestAdapterGeneratedQueriesBindOwningChain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		if err := gitLabMoveWrite(t, db, func(ctx context.Context, repos store.Repos, proof authz.Proof) error {
			_, err := repos.Adapters().MoveTarget(ctx, proof, store.AdapterRouteMoveMutation{MoveID: "arm_direct", Target: gitLabPendingTarget("tgt_gitlab_a", "production", "new-api"), ExpectedGeneration: 1, AuthorityPrincipalID: "usr_gitlab", KeepRemote: true, At: time.Now().UTC()})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		execRealAdoption(t, db, `INSERT INTO adapter_effects(id,org_id,project_id,environment_id,target_id,job_id,surface,effective_name,disposition,intent_audit_id,outcome,created_at,finished_at,finding) SELECT 'eff_direct',org_id,project_id,environment_id,target_id,id,'secret','P_TOKEN','create','eff_direct','failure','2026-08-17T00:00:00Z','2026-08-17T00:01:00Z','owned_missing' FROM adapter_outbox WHERE route_move_id='arm_direct' LIMIT 1`)
		execRealAdoption(t, db, `INSERT INTO adapters(id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_collision','org_gitlab','prj_gitlab','forgejo','https://collision.example','usr_gitlab','active','2026-08-17T00:00:00Z')`)
		execRealAdoption(t, db, `INSERT INTO adapter_route_moves(id,org_id,project_id,adapter_id,kind,pending_origin,pending_credential_ciphertext,authority_principal_id,state,keep_remote,created_at) VALUES ('arm_collision','org_gitlab','prj_gitlab','adp_collision','origin','https://pending.example',$1,'usr_gitlab','attention_required',TRUE,'2026-08-17T00:00:00Z')`, []byte("sealed"))
		// Findings belongs to the original outbox generation (the move increments the target).
		generation := int64(queryInt(t, db, `SELECT generation FROM adapter_outbox WHERE route_move_id='arm_direct' LIMIT 1`))
		observed := time.Now().UTC()
		collisionOrigin := "https://collision.example"
		var queries []adapterScopeQuery
		if db.Engine() == store.EngineSQLite {
			q := sqlitegen.New(db.SQLiteWrite())
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveTargets", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveTargets(ctx, sqlitegen.AdapterMoveTargetsParams{MoveID: "arm_direct", ChainOrg: scope.org, ChainProject: scope.project})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveBeginOriginAdapter", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				_, err := q.AdapterMoveBeginOriginAdapter(ctx, sqlitegen.AdapterMoveBeginOriginAdapterParams{ObservedAt: sql.NullString{String: observed.Format(time.RFC3339Nano), Valid: true}, MutationAdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project})
				if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
					return 0, nil
				}
				if err != nil {
					return 0, err
				}
				return 1, nil
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveBeginOriginTargets", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveBeginOriginTargets(ctx, sqlitegen.AdapterMoveBeginOriginTargetsParams{MutationAdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveBeginTarget", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				_, err := q.AdapterMoveBeginTarget(ctx, sqlitegen.AdapterMoveBeginTargetParams{ObservedAt: sql.NullString{String: observed.Format(time.RFC3339Nano), Valid: true}, MutationTargetID: "tgt_gitlab_b", ChainOrg: scope.org, ChainProject: scope.project})
				if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
					return 0, nil
				}
				if err != nil {
					return 0, err
				}
				return 1, nil
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConfigConfiguredNames", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConfigConfiguredNames(ctx, sqlitegen.AdapterConfigConfiguredNamesParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project, DestinationKind: "repository", DestinationID: 43, DestinationScope: "staging", ExcludeTargetID: ""})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConfigPendingNames", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConfigPendingNames(ctx, sqlitegen.AdapterConfigPendingNamesParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project, DestinationKind: "repository", DestinationOwner: "team", DestinationName: "new-api", DestinationEnvironment: "", DestinationScope: "production", ExcludeTargetID: ""})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConfigAWSConfiguredNames", environment: false, aws: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConfigAWSConfiguredNames(ctx, sqlitegen.AdapterConfigAWSConfiguredNamesParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project, DestinationOwner: "account", ExcludeTargetID: ""})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConfigAWSPendingNames", environment: false, aws: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConfigAWSPendingNames(ctx, sqlitegen.AdapterConfigAWSPendingNamesParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project, DestinationOwner: "account", ExcludeTargetID: ""})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConfigFindings", environment: true, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConfigFindings(ctx, sqlitegen.AdapterConfigFindingsParams{TargetID: "tgt_gitlab_a", ChainOrg: scope.org, ChainProject: scope.project, EnvironmentID: scope.environment, Generation: generation})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveUpdatePendingTarget", environment: true, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveUpdatePendingTarget(ctx, sqlitegen.AdapterMoveUpdatePendingTargetParams{TargetDestinationKind: "repository", TargetDestinationOwner: "team", TargetDestinationName: "new-api", TargetDestinationEnvironment: "", TargetDestinationScope: "production", TargetRepositoryID: 0, TargetVisibility: "", SelectedRepositoryIds: "null", TargetNamePrefix: "P_", MoveID: "arm_direct", TargetID: "tgt_gitlab_a", ChainOrg: scope.org, ChainProject: scope.project, TargetEnvironmentID: scope.environment})
				return int(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveAWSConfiguredNames", environment: false, aws: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveAWSConfiguredNames(ctx, sqlitegen.AdapterMoveAWSConfiguredNamesParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_gitlab_a", Origin: "https://gitlab.old.example", TargetDestinationOwner: "account"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveConfiguredCollision", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveConfiguredCollision(ctx, sqlitegen.AdapterMoveConfiguredCollisionParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_gitlab_a", Origin: "https://gitlab.old.example", TargetDestinationKind: "repository", TargetDestinationOwner: "team", TargetDestinationName: "api-b", TargetDestinationEnvironment: "", TargetDestinationScope: "staging", PendingEffective: "P_TOKEN", SentinelName: "__fixture_sentinel", PendingSurface: "secret", PendingEffective2: "P_TOKEN"})
				return int(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveBeginOriginCollisions", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveBeginOriginCollisions(ctx, sqlitegen.AdapterMoveBeginOriginCollisionsParams{ChainOrg: scope.org, ChainProject: scope.project, MutationAdapterID: "adp_gitlab", MutationOrigin: collisionOrigin, ChainOrg2: scope.org, ChainProject2: scope.project, MutationOrigin2: sql.NullString{String: collisionOrigin, Valid: true}})
				return int(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveReplaceOriginCollisions", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveReplaceOriginCollisions(ctx, sqlitegen.AdapterMoveReplaceOriginCollisionsParams{ChainOrg: scope.org, ChainProject: scope.project, MoveAdapterID: "adp_gitlab", Origin: collisionOrigin, ChainOrg2: scope.org, ChainProject2: scope.project, MoveID: "arm_direct", Origin2: sql.NullString{String: collisionOrigin, Valid: true}})
				return int(rows), err
			}})
		}
		if db.Engine() == store.EnginePostgres {
			q := pggen.New(db.PG())
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveTargets", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveTargets(ctx, pggen.AdapterMoveTargetsParams{MoveID: "arm_direct", ChainOrg: scope.org, ChainProject: scope.project})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveBeginOriginAdapter", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				_, err := q.AdapterMoveBeginOriginAdapter(ctx, pggen.AdapterMoveBeginOriginAdapterParams{ObservedAt: pgtype.Timestamptz{Time: observed, Valid: true}, MutationAdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project})
				if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
					return 0, nil
				}
				if err != nil {
					return 0, err
				}
				return 1, nil
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveBeginOriginTargets", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveBeginOriginTargets(ctx, pggen.AdapterMoveBeginOriginTargetsParams{MutationAdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveBeginTarget", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				_, err := q.AdapterMoveBeginTarget(ctx, pggen.AdapterMoveBeginTargetParams{ObservedAt: pgtype.Timestamptz{Time: observed, Valid: true}, MutationTargetID: "tgt_gitlab_b", ChainOrg: scope.org, ChainProject: scope.project})
				if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
					return 0, nil
				}
				if err != nil {
					return 0, err
				}
				return 1, nil
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConfigConfiguredNames", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConfigConfiguredNames(ctx, pggen.AdapterConfigConfiguredNamesParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project, DestinationKind: "repository", DestinationID: 43, DestinationScope: "staging", ExcludeTargetID: ""})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConfigPendingNames", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConfigPendingNames(ctx, pggen.AdapterConfigPendingNamesParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project, DestinationKind: "repository", DestinationOwner: "team", DestinationName: "new-api", DestinationEnvironment: "", DestinationScope: "production", ExcludeTargetID: ""})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConfigAWSConfiguredNames", environment: false, aws: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConfigAWSConfiguredNames(ctx, pggen.AdapterConfigAWSConfiguredNamesParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project, DestinationOwner: "account", ExcludeTargetID: ""})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConfigAWSPendingNames", environment: false, aws: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConfigAWSPendingNames(ctx, pggen.AdapterConfigAWSPendingNamesParams{AdapterID: "adp_gitlab", ChainOrg: scope.org, ChainProject: scope.project, DestinationOwner: "account", ExcludeTargetID: ""})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConfigFindings", environment: true, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConfigFindings(ctx, pggen.AdapterConfigFindingsParams{TargetID: "tgt_gitlab_a", ChainOrg: scope.org, ChainProject: scope.project, EnvironmentID: scope.environment, Generation: generation})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveUpdatePendingTarget", environment: true, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveUpdatePendingTarget(ctx, pggen.AdapterMoveUpdatePendingTargetParams{TargetDestinationKind: "repository", TargetDestinationOwner: "team", TargetDestinationName: "new-api", TargetDestinationEnvironment: "", TargetDestinationScope: "production", TargetRepositoryID: 0, TargetVisibility: "", SelectedRepositoryIds: []byte("null"), TargetNamePrefix: "P_", MoveID: "arm_direct", TargetID: "tgt_gitlab_a", ChainOrg: scope.org, ChainProject: scope.project, TargetEnvironmentID: scope.environment})
				return int(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveAWSConfiguredNames", environment: false, aws: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveAWSConfiguredNames(ctx, pggen.AdapterMoveAWSConfiguredNamesParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_gitlab_a", Origin: "https://gitlab.old.example", TargetDestinationOwner: "account"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveConfiguredCollision", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveConfiguredCollision(ctx, pggen.AdapterMoveConfiguredCollisionParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_gitlab_a", Origin: "https://gitlab.old.example", TargetDestinationKind: "repository", TargetDestinationOwner: "team", TargetDestinationName: "api-b", TargetDestinationEnvironment: "", TargetDestinationScope: "staging", PendingEffective: "P_TOKEN", SentinelName: "__fixture_sentinel", PendingSurface: "secret", PendingEffective2: "P_TOKEN"})
				return int(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveBeginOriginCollisions", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveBeginOriginCollisions(ctx, pggen.AdapterMoveBeginOriginCollisionsParams{ChainOrg: scope.org, ChainProject: scope.project, MutationAdapterID: "adp_gitlab", MutationOrigin: collisionOrigin, ChainOrg2: scope.org, ChainProject2: scope.project, MutationOrigin2: pgtype.Text{String: collisionOrigin, Valid: true}})
				return int(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterMoveReplaceOriginCollisions", environment: false, aws: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMoveReplaceOriginCollisions(ctx, pggen.AdapterMoveReplaceOriginCollisionsParams{ChainOrg: scope.org, ChainProject: scope.project, MoveAdapterID: "adp_gitlab", Origin: collisionOrigin, ChainOrg2: scope.org, ChainProject2: scope.project, MoveID: "arm_direct", Origin2: pgtype.Text{String: collisionOrigin, Valid: true}})
				return int(rows), err
			}})
		}

		for _, phase := range []bool{false, true} {
			if phase {
				execRealAdoption(t, db, `UPDATE adapter_targets SET destination_kind='per-key',destination_owner='account' WHERE id='tgt_gitlab_b'`)
				execRealAdoption(t, db, `UPDATE adapter_route_move_claims SET destination_kind='per-key',destination_owner='account' WHERE move_id='arm_direct'`)
			}
			for _, query := range queries {
				if query.aws != phase {
					continue
				}
				t.Run(query.name, func(t *testing.T) {
					own := adapterQueryScope{org: "org_gitlab", project: "prj_gitlab", environment: "env_gitlab_e2e"}
					if n, err := query.read(t.Context(), own); err != nil || n < 1 {
						t.Fatalf("owning chain control rows=%d err=%v", n, err)
					}
					for _, axis := range []string{"org", "project", "environment"} {
						if axis == "environment" && !query.environment {
							continue
						}
						foreign := own
						switch axis {
						case "org":
							foreign.org = "org_other"
						case "project":
							foreign.project = "prj_other"
						case "environment":
							foreign.environment = "env_gitlab_second"
						}
						if n, err := query.read(t.Context(), foreign); err != nil || n != 0 {
							t.Fatalf("foreign %s rows=%d err=%v", axis, n, err)
						}
					}
				})
			}
		}
		// Exercise the pending-move arm independently from the configured-adapter arm.
		collisionOrigin = "https://pending.example"
		for _, query := range queries {
			if query.name != "AdapterMoveBeginOriginCollisions" && query.name != "AdapterMoveReplaceOriginCollisions" {
				continue
			}
			t.Run(query.name+"/pending-origin", func(t *testing.T) {
				own := adapterQueryScope{org: "org_gitlab", project: "prj_gitlab", environment: "env_gitlab_e2e"}
				if n, err := query.read(t.Context(), own); err != nil || n < 1 {
					t.Fatalf("pending-origin control rows=%d err=%v", n, err)
				}
				for _, foreign := range []adapterQueryScope{{org: "org_other", project: own.project}, {org: own.org, project: "prj_other"}} {
					if n, err := query.read(t.Context(), foreign); err != nil || n != 0 {
						t.Fatalf("foreign pending-origin rows=%d err=%v", n, err)
					}
				}
			})
		}
	})
}

func TestAdapterGeneratedSnapshotInsertBindsParentChain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		execRealAdoption(t, db, `INSERT INTO adapter_route_moves(id,org_id,project_id,adapter_id,kind,authority_principal_id,state,keep_remote,created_at) VALUES ('arm_snapshot','org_gitlab','prj_gitlab','adp_gitlab','origin','usr_gitlab','completed',TRUE,'2026-08-17T00:00:00Z')`)
		var insert func(adapterQueryScope) (int64, error)
		if db.Engine() == store.EngineSQLite {
			q := sqlitegen.New(db.SQLiteWrite())
			insert = func(scope adapterQueryScope) (int64, error) {
				return q.AdapterMoveInsertTarget(t.Context(), sqlitegen.AdapterMoveInsertTargetParams{MutationMoveID: "arm_snapshot", ChainOrg: scope.org, ChainProject: scope.project, TargetEnvironmentID: scope.environment, TargetID: "tgt_gitlab_b", TargetKind: "repository", TargetOwner: "team", TargetName: "api-b", TargetDestinationEnvironment: "", TargetDestinationScope: "staging", TargetRepositoryID: 0, TargetVisibility: "", SelectedRepositoryIds: "null", TargetPrefix: "P_", OrphanedNames: "[]"})
			}
		}
		if db.Engine() == store.EnginePostgres {
			q := pggen.New(db.PG())
			insert = func(scope adapterQueryScope) (int64, error) {
				return q.AdapterMoveInsertTarget(t.Context(), pggen.AdapterMoveInsertTargetParams{MutationMoveID: "arm_snapshot", ChainOrg: scope.org, ChainProject: scope.project, TargetEnvironmentID: scope.environment, TargetID: "tgt_gitlab_b", TargetKind: "repository", TargetOwner: "team", TargetName: "api-b", TargetDestinationEnvironment: "", TargetDestinationScope: "staging", TargetRepositoryID: 0, TargetVisibility: "", SelectedRepositoryIds: []byte("null"), TargetPrefix: "P_", OrphanedNames: []byte("[]")})
			}
		}

		own := adapterQueryScope{org: "org_gitlab", project: "prj_gitlab", environment: "env_gitlab_second"}
		// Refusal cases run before the positive insert, so uniqueness cannot mask a missing parent fence.
		for _, axis := range []string{"org", "project", "environment"} {
			foreign := own
			switch axis {
			case "org":
				foreign.org = "org_other"
			case "project":
				foreign.project = "prj_other"
			case "environment":
				foreign.environment = "env_gitlab_e2e"
			}
			if n, err := insert(foreign); err == nil || n != 0 {
				t.Fatalf("foreign %s insert rows=%d err=%v", axis, n, err)
			}
			if n := queryInt(t, db, `SELECT COUNT(*) FROM adapter_route_move_targets WHERE move_id='arm_snapshot'`); n != 0 {
				t.Fatalf("foreign %s wrote %d snapshots", axis, n)
			}
		}
		if n, err := insert(own); err != nil || n != 1 {
			t.Fatalf("owning snapshot insert rows=%d err=%v", n, err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM adapter_route_move_targets WHERE move_id='arm_snapshot' AND org_id='org_gitlab' AND project_id='prj_gitlab' AND environment_id='env_gitlab_second' AND target_id='tgt_gitlab_b'`); n != 1 {
			t.Fatalf("owning snapshot count=%d", n)
		}
	})
}
