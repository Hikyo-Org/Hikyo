package migrate

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestAdapterCustodyRepairsSQLite(t *testing.T) {
	for _, state := range []string{"scrubbing", "activating"} {
		t.Run(state, func(t *testing.T) {
			runAdapterCustodyRepairs(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "adapter-repairs.db")}, state)
		})
	}
}

func TestAdapterCustodyRepairsPostgres(t *testing.T) {
	for _, state := range []string{"scrubbing", "activating"} {
		t.Run(state, func(t *testing.T) {
			runAdapterCustodyRepairs(t, postgresTestConfig(t, "adapter_repairs"), state)
		})
	}
}

func runAdapterCustodyRepairs(t *testing.T, cfg store.Config, moveState string) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 71); err != nil {
		t.Fatal(err)
	}
	db := migrationFixtureSQL(t, cfg)
	exec := func(statement string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed %q: %v", statement, err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_repair','Repair',TRUE,'{}','2026-10-01T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_repair','org_repair','Repair','2026-10-01T00:00:00Z')`,
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_repair','org_repair','prj_repair','prod','','2026-10-01T00:00:00Z',0)`,
		`INSERT INTO principals (id,kind,created_at) VALUES ('usr_repair','human','2026-10-01T00:00:00Z')`,
		`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_gitlab','org_repair','prj_repair','gitlab','https://git.example','usr_repair','active','2026-10-01T00:00:00Z')`,
		`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_forgejo','org_repair','prj_repair','forgejo','https://forgejo.example','usr_repair','active','2026-10-01T00:00:00Z')`,
	} {
		exec(statement)
	}
	for _, tc := range []struct {
		id, adapter, origin, scope, ledgerScope, ledgerState string
	}{
		{"bad", "adp_gitlab", "https://git.example", "production", "", "owned"},
		{"pending", "adp_gitlab", "https://git.example", "staging", "", "reserved"},
		{"good", "adp_gitlab", "https://git.example", "production", "production", "owned"},
		{"released", "adp_gitlab", "https://git.example", "production", "", "released"},
		{"other", "adp_forgejo", "https://forgejo.example", "", "", "owned"},
	} {
		exec(fmt.Sprintf(`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,destination_scope,name_prefix,generation,state,sync_status,warnings,active_job_id,provider_lease_job_id,provider_lease_effect_id,provider_lease_expires_at,created_at) VALUES ('tgt_%s','org_repair','prj_repair','env_repair','%s','repository','acme','%s',42,'%s','',5,'active','converging','["existing_warning"]','job_%s','job_%s','effect_%s','2027-10-01T00:00:00Z','2026-10-01T00:00:00Z')`, tc.id, tc.adapter, tc.id, tc.scope, tc.id, tc.id, tc.id))
		exec(fmt.Sprintf(`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,state,lease_owner,lease_expires_at,created_at) VALUES ('job_%s','org_repair','prj_repair','env_repair','tgt_%s','converge','usr_repair',5,'tgt_%s','2026-10-01T00:00:00Z','running','worker','2027-10-01T00:00:00Z','2026-10-01T00:00:00Z')`, tc.id, tc.id, tc.id))
		exec(fmt.Sprintf(`INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,destination_scope,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_%s','org_repair','prj_repair','env_repair','tgt_%s','%s','repository',0,42,'%s','variable','%s','%s','%s','2026-10-01T00:00:00Z')`, tc.id, tc.id, tc.origin, tc.ledgerScope, tc.id, tc.id, tc.ledgerState))
	}
	// Historical terminal pointers are detached only when the exact target
	// chain and older generation match. A replacement/live pointer is retained.
	for _, tc := range []struct {
		id, state     string
		jobGeneration int
	}{
		{"old_terminal", "failed", 4},
		{"current_terminal", "failed", 5},
		{"old_live", "queued", 4},
		{"replacement", "queued", 5},
	} {
		exec(fmt.Sprintf(`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,active_job_id,provider_lease_job_id,provider_lease_effect_id,provider_lease_expires_at,created_at) VALUES ('tgt_%s','org_repair','prj_repair','env_repair','adp_forgejo','repository','acme','%s',99,'',5,'active','converging','job_%s','held_%s','effect_%s','2027-10-01T00:00:00Z','2026-10-01T00:00:00Z')`, tc.id, tc.id, tc.id, tc.id, tc.id))
		exec(fmt.Sprintf(`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ('job_%s','org_repair','prj_repair','env_repair','tgt_%s','converge','usr_repair',%d,'tgt_%s','2026-10-01T00:00:00Z','%s','2026-10-01T00:00:00Z')`, tc.id, tc.id, tc.jobGeneration, tc.id, tc.state))
	}
	exec(`UPDATE adapter_targets SET active_job_id='job_old_terminal' WHERE id='tgt_other'`)
	seedCustodyRepairMoves(t, db, cfg.Engine, moveState)
	if err := Run(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	assertCount := func(query string, want int) {
		t.Helper()
		if n := countUpgrade(t, cfg, query); n != want {
			t.Fatalf("%s = %d; want %d", query, n, want)
		}
	}
	assertCount(`SELECT COUNT(*) FROM adapter_targets WHERE id IN ('tgt_bad','tgt_pending') AND generation=6 AND paused_at IS NOT NULL AND sync_status='failed' AND drift_attention=TRUE AND last_error_class='refused' AND active_job_id IS NULL AND provider_lease_job_id IS NULL AND provider_lease_effect_id IS NULL AND provider_lease_expires_at IS NULL`, 2)
	assertCount(`SELECT COUNT(*) FROM adapter_ledger WHERE id IN ('led_bad','led_pending') AND state='released' AND destination_scope=''`, 2)
	assertCount(`SELECT COUNT(*) FROM adapter_outbox WHERE id IN ('job_bad','job_pending') AND state='superseded' AND lease_owner IS NULL AND lease_expires_at IS NULL AND finished_at IS NOT NULL`, 2)
	assertCount(`SELECT COUNT(*) FROM adapter_targets WHERE id IN ('tgt_bad','tgt_pending') AND CAST(warnings AS TEXT) LIKE '%existing_warning%' AND CAST(warnings AS TEXT) LIKE '%gitlab_scope_claims_released_operator_review%'`, 2)
	assertCount(`SELECT COUNT(*) FROM adapter_targets WHERE id IN ('tgt_good','tgt_released','tgt_other') AND generation=5 AND paused_at IS NULL AND provider_lease_job_id IS NOT NULL`, 3)
	assertCount(`SELECT COUNT(*) FROM adapter_ledger WHERE id IN ('led_good','led_other') AND state='owned'`, 2)
	assertCount(`SELECT COUNT(*) FROM adapter_ledger WHERE id='led_released' AND state='released'`, 1)
	assertCount(`SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_old_terminal' AND active_job_id IS NULL AND generation=5 AND provider_lease_job_id='held_old_terminal' AND provider_lease_effect_id='effect_old_terminal' AND provider_lease_expires_at IS NOT NULL`, 1)
	assertCount(`SELECT COUNT(*) FROM adapter_outbox WHERE id='job_old_terminal' AND state='failed' AND generation=4`, 1)
	assertCount(`SELECT COUNT(*) FROM adapter_targets WHERE id IN ('tgt_current_terminal','tgt_old_live','tgt_replacement','tgt_other') AND active_job_id IS NOT NULL`, 4)
	assertCustodyRepairMoves(t, cfg)
}
