package isolation

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func adapterCoreOne[T any](_ T, err error) (int, error) {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return 1, nil
}
func TestAdapterCoreGeneratedQueriesBindOwningChain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedAdapterUpdateTarget(t, db)
		execRaw(t, db, `INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES ('snap_core','org_a','prj_a1','env_a1',7,1,'usr_alice',`+ts+`)`)
		execRaw(t, db, `INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES ('snap_core_other','org_a','prj_a1','env_prod',99,1,'usr_alice',`+ts+`)`)
		// Values are deliberately unused: these queries return names/classifications only.
		execRaw(t, db, `INSERT INTO snapshot_entries(id,org_id,project_id,environment_id,snapshot_id,key_id,key_name,classification,ciphertext,value_entry_id) VALUES ('sen_core','org_a','prj_a1','env_a1','snap_core','key_a1','OWNED','config','unused','val_core')`)
		execRaw(t, db, `INSERT INTO snapshot_entries(id,org_id,project_id,environment_id,snapshot_id,key_id,key_name,classification,ciphertext,value_entry_id) VALUES ('sen_core_other','org_a','prj_a1','env_prod','snap_core_other','key_a1','FOREIGN','config','unused','val_core_other')`)
		execRaw(t, db, `INSERT INTO adapter_conflicts(id,artifact_id,org_id,project_id,environment_id,target_id,destination_id,repository_id,target_generation,surface,effective_name,created_at) VALUES ('acn_core','artifact_core','org_a','prj_a1','env_a1','tgt_dialect',42,0,1,'secret','OWNED',`+ts+`)`)
		observed := time.Now().UTC()
		var queries []adapterScopeQuery
		if db.Engine() == store.EngineSQLite {
			q := sqlitegen.New(db.SQLiteWrite())
			queries = append(queries, adapterScopeQuery{name: "AdapterMapping", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMapping(ctx, sqlitegen.AdapterMappingParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterPlanManifest", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterPlanManifest(ctx, sqlitegen.AdapterPlanManifestParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect", EnvironmentID: scope.environment})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterGetTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterGetTarget(ctx, sqlitegen.AdapterGetTargetParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterListTargets", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterListTargets(ctx, sqlitegen.AdapterListTargetsParams{ChainOrg: scope.org, ChainProject: scope.project, AdapterID: "adp_dialect"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterGetActiveTargetForUpdate", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterGetActiveTargetForUpdate(ctx, sqlitegen.AdapterGetActiveTargetForUpdateParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterTargetKeys", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterTargetKeys(ctx, sqlitegen.AdapterTargetKeysParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterTargetEnvironments", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterTargetEnvironments(ctx, sqlitegen.AdapterTargetEnvironmentsParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConflicts", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConflicts(ctx, sqlitegen.AdapterConflictsParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterAdoptionTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterAdoptionTarget(ctx, sqlitegen.AdapterAdoptionTargetParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect", At: sql.NullString{String: store.CanonTime(observed).Format("2006-01-02T15:04:05.000000Z"), Valid: true}}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterPublishedTargets", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterPublishedTargets(ctx, sqlitegen.AdapterPublishedTargetsParams{ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterPauseTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterPauseTarget(ctx, sqlitegen.AdapterPauseTargetParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect", At: sql.NullString{String: store.CanonTime(observed).Format("2006-01-02T15:04:05.000000Z"), Valid: true}}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterTeardownTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterTeardownTarget(ctx, sqlitegen.AdapterTeardownTargetParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect", At: sql.NullString{String: store.CanonTime(observed).Format("2006-01-02T15:04:05.000000Z"), Valid: true}}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterTeardownTargets", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterTeardownTargets(ctx, sqlitegen.AdapterTeardownTargetsParams{ChainOrg: scope.org, ChainProject: scope.project, AdapterID: "adp_dialect", At: sql.NullString{String: store.CanonTime(observed).Format("2006-01-02T15:04:05.000000Z"), Valid: true}})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterReplaceCredentialTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterReplaceCredentialTarget(ctx, sqlitegen.AdapterReplaceCredentialTargetParams{ChainOrg: scope.org, ChainProject: scope.project, AdapterID: "adp_dialect", At: sql.NullString{String: store.CanonTime(observed).Format("2006-01-02T15:04:05.000000Z"), Valid: true}}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterRevokeCredentialTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterRevokeCredentialTarget(ctx, sqlitegen.AdapterRevokeCredentialTargetParams{ChainOrg: scope.org, ChainProject: scope.project, AdapterID: "adp_dialect"}))
			}})
		}
		if db.Engine() == store.EnginePostgres {
			q := pggen.New(db.PG())
			queries = append(queries, adapterScopeQuery{name: "AdapterMapping", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterMapping(ctx, pggen.AdapterMappingParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterPlanManifest", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterPlanManifest(ctx, pggen.AdapterPlanManifestParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect", EnvironmentID: scope.environment})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterGetTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterGetTarget(ctx, pggen.AdapterGetTargetParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterListTargets", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterListTargets(ctx, pggen.AdapterListTargetsParams{ChainOrg: scope.org, ChainProject: scope.project, AdapterID: "adp_dialect"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterGetActiveTargetForUpdate", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterGetActiveTargetForUpdate(ctx, pggen.AdapterGetActiveTargetForUpdateParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterTargetKeys", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterTargetKeys(ctx, pggen.AdapterTargetKeysParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterTargetEnvironments", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterTargetEnvironments(ctx, pggen.AdapterTargetEnvironmentsParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterConflicts", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterConflicts(ctx, pggen.AdapterConflictsParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect"})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterAdoptionTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterAdoptionTarget(ctx, pggen.AdapterAdoptionTargetParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect", At: pgtype.Timestamptz{Time: observed, Valid: true}}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterPublishedTargets", environment: true, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterPublishedTargets(ctx, pggen.AdapterPublishedTargetsParams{ChainOrg: scope.org, ChainProject: scope.project, ChainEnv: scope.environment})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterPauseTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterPauseTarget(ctx, pggen.AdapterPauseTargetParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect", At: pgtype.Timestamptz{Time: observed, Valid: true}}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterTeardownTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterTeardownTarget(ctx, pggen.AdapterTeardownTargetParams{ChainOrg: scope.org, ChainProject: scope.project, TargetID: "tgt_dialect", At: pgtype.Timestamptz{Time: observed, Valid: true}}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterTeardownTargets", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				rows, err := q.AdapterTeardownTargets(ctx, pggen.AdapterTeardownTargetsParams{ChainOrg: scope.org, ChainProject: scope.project, AdapterID: "adp_dialect", At: pgtype.Timestamptz{Time: observed, Valid: true}})
				return len(rows), err
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterReplaceCredentialTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterReplaceCredentialTarget(ctx, pggen.AdapterReplaceCredentialTargetParams{ChainOrg: scope.org, ChainProject: scope.project, AdapterID: "adp_dialect", At: pgtype.Timestamptz{Time: observed, Valid: true}}))
			}})
			queries = append(queries, adapterScopeQuery{name: "AdapterRevokeCredentialTarget", environment: false, read: func(ctx context.Context, scope adapterQueryScope) (int, error) {
				return adapterCoreOne(q.AdapterRevokeCredentialTarget(ctx, pggen.AdapterRevokeCredentialTargetParams{ChainOrg: scope.org, ChainProject: scope.project, AdapterID: "adp_dialect"}))
			}})
		}

		own := adapterQueryScope{org: "org_a", project: "prj_a1", environment: "env_a1"}
		for _, query := range queries {
			t.Run(query.name, func(t *testing.T) {
				if n, err := query.read(t.Context(), own); err != nil || n != 1 {
					t.Fatalf("owning control rows=%d err=%v", n, err)
				}
				for axis, foreign := range adapterForeignScopes(own, adapterQueryScope{org: "org_b", project: "prj_a2", environment: "env_prod"}, query.environment) {
					if n, err := query.read(t.Context(), foreign); err != nil || n != 0 {
						t.Fatalf("foreign %s rows=%d err=%v", axis, n, err)
					}
				}
			})
		}
	})
}
func TestAdapterCoreCredentialEraseRefusesForeignScope(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedAdapterUpdateTarget(t, db)
		execRaw(t, db, `UPDATE adapter_targets SET state='tombstoned' WHERE id='tgt_dialect'`)
		execRaw(t, db, `UPDATE adapters SET state='tombstoned',credential_ciphertext='retained-secret' WHERE id='adp_dialect'`)
		var erase func(adapterQueryScope) error
		if db.Engine() == store.EngineSQLite {
			q := sqlitegen.New(db.SQLiteWrite())
			erase = func(scope adapterQueryScope) error {
				return q.AdapterEraseUnusedCredential(t.Context(), sqlitegen.AdapterEraseUnusedCredentialParams{AdapterID: "adp_dialect", ChainOrg: scope.org, ChainProject: scope.project})
			}
		}
		if db.Engine() == store.EnginePostgres {
			q := pggen.New(db.PG())
			erase = func(scope adapterQueryScope) error {
				return q.AdapterEraseUnusedCredential(t.Context(), pggen.AdapterEraseUnusedCredentialParams{AdapterID: "adp_dialect", ChainOrg: scope.org, ChainProject: scope.project})
			}
		}
		for _, scope := range []adapterQueryScope{{org: "org_b", project: "prj_a1"}, {org: "org_a", project: "prj_a2"}} {
			if err := erase(scope); err != nil {
				t.Fatal(err)
			}
			if queryInt(t, db, `SELECT COUNT(*) FROM adapters WHERE id='adp_dialect' AND credential_ciphertext IS NOT NULL`) != 1 {
				t.Fatal("foreign scope erased owning credential")
			}
		}
		if err := erase(adapterQueryScope{org: "org_a", project: "prj_a1"}); err != nil {
			t.Fatal(err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM adapters WHERE id='adp_dialect' AND credential_ciphertext IS NOT NULL`) != 0 {
			t.Fatal("owning control did not erase credential")
		}
	})
}
