package migrate

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestAdapterAdoptionIndexesSQLite(t *testing.T) {
	testAdapterAdoptionIndexes(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "adoption.db")})
}

func TestAdapterAdoptionIndexesPostgres(t *testing.T) {
	testAdapterAdoptionIndexes(t, postgresTestConfig(t, "adoption_indexes"))
}

func testAdapterAdoptionIndexes(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 80); err != nil {
		t.Fatal(err)
	}
	db := migrationFixtureSQL(t, cfg)
	exec := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		`INSERT INTO orgs(id,name,active,metadata,created_at) VALUES ('org_index','Adoption',TRUE,'{}','2026-01-01T00:00:00Z')`,
		`INSERT INTO projects(id,org_id,name,created_at) VALUES ('prj_index','org_index','Adoption','2026-01-01T00:00:00Z')`,
		`INSERT INTO environments(id,org_id,project_id,name,note,display_order,created_at) VALUES ('env_index','org_index','prj_index','prod','',0,'2026-01-01T00:00:00Z')`,
		`INSERT INTO principals(id,kind,created_at) VALUES ('usr_index','human','2026-01-01T00:00:00Z')`,
		`INSERT INTO adapters(id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_index','org_index','prj_index','aws-secrets-manager','https://secretsmanager.eu-west-1.amazonaws.com','usr_index','active','2026-01-01T00:00:00Z')`,
		`INSERT INTO adapter_targets(id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_index','org_index','prj_index','env_index','adp_index','per-key','123456789012','prod/',123456789012,'',2,'active','converging','2026-01-01T00:00:00Z')`,
		`INSERT INTO adapter_outbox(id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ('job_index','org_index','prj_index','env_index','tgt_index','converge','usr_index',2,'tgt_index','2026-01-01T00:00:00Z','queued','2026-01-01T00:00:00Z')`,
		`INSERT INTO adapter_ledger(id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_index','org_index','prj_index','env_index','tgt_index','https://secretsmanager.eu-west-1.amazonaws.com','per-key',0,123456789012,'secret','prod/0','PROD/0','owned','2026-01-01T00:00:00Z')`,
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<1999) INSERT INTO adapter_conflicts(id,artifact_id,org_id,project_id,environment_id,target_id,destination_id,repository_id,target_generation,surface,effective_name,observed_provider_version,observed_provider_witness,created_at,adopted_at) SELECT 'conf_'||i,'artifact_'||i,'org_index','prj_index','env_index','tgt_index',123456789012,0,1,'secret','prod/'||i,7,'reviewed-witness','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z' FROM n`,
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<1999) INSERT INTO adapter_effects(id,org_id,project_id,environment_id,target_id,job_id,surface,effective_name,disposition,intent_audit_id,outcome,created_at,finished_at) SELECT 'effect_'||i,'org_index','prj_index','env_index','tgt_index','job_index','secret','prod/'||i,'update','intent_'||i,'success','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z' FROM n`,
	} {
		exec(query)
	}
	queries := []struct{ file, name string }{{"adapter_repository", "AdapterPlanLedger"}, {"adapter_runtime", "AdapterWorkerLoadExecutionLedgerQuery"}}
	read := func(query string, pending int, witness string) {
		t.Helper()
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var surface, name, state, gotWitness string
			var missing bool
			var gotPending int
			var version sql.NullInt64
			if err := rows.Scan(&surface, &name, &state, &missing, &gotPending, &version, &gotWitness); err != nil {
				t.Fatal(err)
			}
			if surface != "secret" || name != "prod/0" || state != "owned" || missing || gotPending != pending || gotWitness != witness || (pending == 1 && (!version.Valid || version.Int64 != 7)) || (pending == 0 && version.Valid) {
				t.Fatalf("unexpected ledger row: %s %s %s %v %d %v %q", surface, name, state, missing, gotPending, version, gotWitness)
			}
			n++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("ledger rows=%d", n)
		}
	}
	bind := func(q struct{ file, name string }) string {
		return indexFixtureQuery(t, cfg.Engine, q.file, q.name, map[string]string{"target_id": "'tgt_index'", "chain_org": "'org_index'", "chain_project": "'prj_index'", "chain_env": "'env_index'", "environment_id": "'env_index'", "generation": "2"})
	}
	for _, q := range queries {
		query := bind(q)
		read(query, 1, "reviewed-witness")
		t.Logf("before %s:\n%s", q.name, indexFixturePlan(t, db, cfg.Engine, query))
	}
	if err := Run(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	exec("ANALYZE adapter_conflicts")
	exec("ANALYZE adapter_effects")
	for _, q := range queries {
		query := bind(q)
		read(query, 1, "reviewed-witness")
		plan := indexFixturePlan(t, db, cfg.Engine, query)
		t.Logf("after %s:\n%s", q.name, plan)
		for _, index := range []string{"adapter_conflicts_adoption_lookup", "adapter_effects_adoption_lookup"} {
			if !strings.Contains(plan, index) {
				t.Fatalf("missing scoped lookup %s:\n%s", index, plan)
			}
		}
	}
	// Later success consumes only this row's pending adoption; unrelated history
	// and both sets of 2,000 evidence rows survive installation and lookup.
	exec(`UPDATE adapter_effects SET finished_at='2026-01-03T00:00:00Z' WHERE id='effect_0'`)
	for _, q := range queries {
		read(bind(q), 0, "")
	}
	for _, table := range []string{"adapter_conflicts", "adapter_effects"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 2000 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	for _, file := range []string{"adapter_repository", "adapter_runtime"} {
		query := indexFixtureQuery(t, cfg.Engine, file, map[string]string{"adapter_repository": "AdapterPlanLedger", "adapter_runtime": "AdapterWorkerLoadExecutionLedgerQuery"}[file], map[string]string{"target_id": "'tgt_index'", "chain_org": "'foreign'", "chain_project": "'prj_index'", "chain_env": "'env_index'", "environment_id": "'env_index'", "generation": "2"})
		var n int
		if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM (%s) AS scoped", query)).Scan(&n); err != nil || n != 0 {
			t.Fatalf("foreign chain returned %d rows: %v", n, err)
		}
	}
}
