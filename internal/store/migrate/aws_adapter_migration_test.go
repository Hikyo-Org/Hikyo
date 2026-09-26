package migrate

import (
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// 00060 widens the provider and destination-kind CHECK sets for AWS Secrets
// Manager (#158). On SQLite that is a rebuild of five tables, so the proof is
// that pre-existing rows, foreign keys, and the partial unique indexes survive
// byte for byte, and that the new identities are then admitted.
func TestAWSAdapterMigrationSQLite(t *testing.T) {
	testAWSAdapterMigration(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "aws.db")})
}

func TestAWSAdapterMigrationPostgres(t *testing.T) {
	testAWSAdapterMigration(t, postgresTestConfig(t, "aws_adapter"))
}

func testAWSAdapterMigration(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 59); err != nil {
		t.Fatal(err)
	}
	db := migrationFixtureSQL(t, cfg)
	for _, statement := range []string{
		`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_aws','AWS',TRUE,'{}','2026-09-01T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_aws','org_aws','AWS','2026-09-01T00:00:00Z')`,
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_aws','org_aws','prj_aws','prod','','2026-09-01T00:00:00Z',0)`,
		`INSERT INTO principals (id,kind,created_at) VALUES ('usr_aws','human','2026-09-01T00:00:00Z')`,
		`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_gh','org_aws','prj_aws','github-actions','https://api.github.com','usr_aws','active','2026-09-01T00:00:00Z')`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at,paused_at,last_error_class,drift_attention) VALUES ('tgt_gh','org_aws','prj_aws','env_aws','adp_gh','repository','acme','app',42,'',3,'active','failed','2026-09-01T00:00:00Z','2026-09-02T00:00:00Z','auth',TRUE)`,
		`INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,destination_id,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_gh','org_aws','prj_aws','env_aws','tgt_gh','https://api.github.com','repository',42,'secret','TOKEN','TOKEN','owned','2026-09-01T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed %q: %v", statement, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_early','org_aws','prj_aws','aws-secrets-manager','https://secretsmanager.eu-west-1.amazonaws.com','usr_aws','active','2026-09-01T00:00:00Z')`); err == nil {
		t.Fatal("pre-00060 schema admitted the aws-secrets-manager provider")
	}
	if err := Run(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if n := countUpgrade(t, cfg, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_gh' AND generation=3 AND paused_at IS NOT NULL AND last_error_class='auth' AND drift_attention=TRUE`); n != 1 {
		t.Fatalf("rebuilt adapter_targets lost row state: %d", n)
	}
	if n := countUpgrade(t, cfg, `SELECT COUNT(*) FROM adapter_ledger WHERE id='led_gh' AND state='owned'`); n != 1 {
		t.Fatalf("rebuilt adapter_ledger lost custody: %d", n)
	}
	for _, statement := range []string{
		`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_aws','org_aws','prj_aws','aws-secrets-manager','https://secretsmanager.eu-west-1.amazonaws.com','usr_aws','active','2026-09-01T00:00:00Z')`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_json','org_aws','prj_aws','env_aws','adp_aws','json-object','123456789012','prod/app',123456789012,'',1,'active','never','2026-09-01T00:00:00Z')`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_keys','org_aws','prj_aws','env_aws','adp_aws','per-key','123456789012','prod/',123456789012,'APP_',1,'active','never','2026-09-01T00:00:00Z')`,
		`INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,destination_id,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_aws','org_aws','prj_aws','env_aws','tgt_keys','https://secretsmanager.eu-west-1.amazonaws.com','per-key',123456789012,'secret','prod/APP_TOKEN','PROD/APP_TOKEN','owned','2026-09-01T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("post-00060 %q: %v", statement, err)
		}
	}
	// The rebuilt partial unique index still refuses a second active owner.
	if _, err := db.ExecContext(ctx, `INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,destination_id,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_twice','org_aws','prj_aws','env_aws','tgt_json','https://secretsmanager.eu-west-1.amazonaws.com','per-key',123456789012,'secret','prod/APP_TOKEN','PROD/APP_TOKEN','reserved','2026-09-01T00:00:00Z')`); err == nil {
		t.Fatal("rebuilt ledger admitted a second active owner of one provider name")
	}
	// The rebuilt child foreign key still points at the rebuilt parent.
	if _, err := db.ExecContext(ctx, `INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,destination_id,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_orphan','org_aws','prj_aws','env_aws','tgt_missing','https://secretsmanager.eu-west-1.amazonaws.com','per-key',123456789012,'secret','X','X','owned','2026-09-01T00:00:00Z')`); err == nil {
		t.Fatal("rebuilt ledger lost its target foreign key")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_bad','org_aws','prj_aws','env_aws','adp_aws','bucket','123456789012','x',1,'',1,'active','never','2026-09-01T00:00:00Z')`); err == nil {
		t.Fatal("destination kind CHECK is no longer closed")
	}
}
