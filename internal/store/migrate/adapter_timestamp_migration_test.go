package migrate

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestSQLiteAdapterTimestampMigrationNormalizesLexicalColumns(t *testing.T) {
	cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "adapter-timestamps.db")}
	if err := RunUpTo(t.Context(), cfg, 33); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", store.SQLiteDSN(cfg.Path))
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_adapter','Adapter',1,'{}','2026-08-17T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_adapter','org_adapter','Adapter','2026-08-17T00:00:00Z')`,
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_adapter','org_adapter','prj_adapter','prod','','2026-08-17T00:00:00Z',0)`,
		`INSERT INTO principals (id,kind,created_at) VALUES ('usr_adapter','human','2026-08-17T00:00:00Z')`,
		`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_1','org_adapter','prj_adapter','forgejo','https://git.example','usr_adapter','active','2026-08-17T00:00:00Z')`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,provider_lease_job_id,provider_lease_effect_id,provider_lease_expires_at,created_at) VALUES ('tgt_1','org_adapter','prj_adapter','env_adapter','adp_1','repository','acme','app',42,'',1,'active','converging','job_1','effect_1','2026-08-17T12:34:05Z','2026-08-17T00:00:00Z')`,
		`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,lease_owner,lease_expires_at,state,created_at) VALUES ('job_1','org_adapter','prj_adapter','env_adapter','tgt_1','converge','usr_adapter',1,'tgt_1','2026-08-17T12:34:05.1Z','worker_1','2026-08-17T12:34:05.123456Z','running','2026-08-17T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Run(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", store.SQLiteDSN(cfg.Path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var providerLease, nextAttempt, jobLease string
	if err := db.QueryRowContext(t.Context(), `SELECT provider_lease_expires_at FROM adapter_targets WHERE id='tgt_1'`).Scan(&providerLease); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT next_attempt_at,lease_expires_at FROM adapter_outbox WHERE id='job_1'`).Scan(&nextAttempt, &jobLease); err != nil {
		t.Fatal(err)
	}
	if providerLease != "2026-08-17T12:34:05.000000Z" || nextAttempt != "2026-08-17T12:34:05.100000Z" || jobLease != "2026-08-17T12:34:05.123456Z" {
		t.Fatalf("normalized timestamps = provider %q, next %q, lease %q", providerLease, nextAttempt, jobLease)
	}
}
