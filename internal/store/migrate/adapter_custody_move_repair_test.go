package migrate

import (
	"database/sql"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func seedCustodyRepairMoves(t *testing.T, db *sql.DB, engine store.Engine, state string) {
	t.Helper()
	ciphertext := "X'01'"
	if engine == store.EnginePostgres {
		ciphertext = "decode('01','hex')"
	}
	for _, statement := range []string{
		`UPDATE adapter_targets SET state='moving' WHERE id IN ('tgt_bad','tgt_good')`,
		`INSERT INTO adapter_route_moves (id,org_id,project_id,adapter_id,kind,authority_principal_id,state,keep_remote,pending_origin,pending_credential_ciphertext,created_at) VALUES ('move_repair','org_repair','prj_repair','adp_gitlab','origin','usr_repair','` + state + `',FALSE,'https://next.example',` + ciphertext + `,'2026-10-01T00:00:00Z')`,
		`INSERT INTO adapter_route_move_targets (move_id,org_id,project_id,environment_id,target_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,destination_scope) VALUES ('move_repair','org_repair','prj_repair','env_repair','tgt_bad','repository','acme','bad',42,'','production'),('move_repair','org_repair','prj_repair','env_repair','tgt_good','repository','acme','good',42,'','production')`,
		`UPDATE adapter_outbox SET route_move_id='move_repair' WHERE id IN ('job_bad','job_good')`,
		`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ('job_move_terminal','org_repair','prj_repair','env_repair','tgt_good','scrub','move_repair','usr_repair',4,'tgt_good','2026-10-01T00:00:00Z','failed','2026-10-01T00:00:00Z')`,
		`INSERT INTO adapter_route_moves (id,org_id,project_id,adapter_id,target_id,kind,authority_principal_id,state,keep_remote,created_at) VALUES ('move_completed','org_repair','prj_repair','adp_forgejo','tgt_other','target','usr_repair','completed',FALSE,'2026-10-01T00:00:00Z')`,
		`INSERT INTO adapter_route_move_targets (move_id,org_id,project_id,environment_id,target_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix) VALUES ('move_completed','org_repair','prj_repair','env_repair','tgt_other','repository','acme','other',42,'')`,
		`UPDATE adapter_outbox SET route_move_id='move_completed' WHERE id='job_other'`,
		`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_foreign','Foreign',TRUE,'{}','2026-10-01T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_foreign','org_foreign','Foreign','2026-10-01T00:00:00Z')`,
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_foreign','org_foreign','prj_foreign','prod','','2026-10-01T00:00:00Z',0)`,
		`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_foreign','org_foreign','prj_foreign','gitlab','https://git.example','usr_repair','active','2026-10-01T00:00:00Z')`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,destination_scope,name_prefix,generation,state,sync_status,active_job_id,created_at) VALUES ('tgt_foreign','org_foreign','prj_foreign','env_foreign','adp_foreign','repository','acme','foreign',42,'production','',5,'moving','converging','job_foreign','2026-10-01T00:00:00Z')`,
		`INSERT INTO adapter_route_moves (id,org_id,project_id,adapter_id,kind,authority_principal_id,state,keep_remote,pending_origin,pending_credential_ciphertext,created_at) VALUES ('move_foreign','org_foreign','prj_foreign','adp_foreign','origin','usr_repair','scrubbing',FALSE,'https://next.example',` + ciphertext + `,'2026-10-01T00:00:00Z')`,
		`INSERT INTO adapter_route_move_targets (move_id,org_id,project_id,environment_id,target_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,destination_scope) VALUES ('move_foreign','org_foreign','prj_foreign','env_foreign','tgt_foreign','repository','acme','foreign',42,'','production')`,
		`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ('job_foreign','org_foreign','prj_foreign','env_foreign','tgt_foreign','scrub','move_foreign','usr_repair',5,'tgt_foreign','2026-10-01T00:00:00Z','queued','2026-10-01T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("seed move %q: %v", statement, err)
		}
	}
}

func assertCustodyRepairMoves(t *testing.T, cfg store.Config) {
	t.Helper()
	for _, tc := range []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM adapter_route_moves WHERE id='move_repair' AND state='attention_required'`, 1},
		{`SELECT COUNT(*) FROM adapter_outbox WHERE id='job_good' AND state='superseded' AND lease_owner IS NULL AND lease_expires_at IS NULL`, 1},
		{`SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_good' AND active_job_id IS NULL AND generation=5 AND paused_at IS NULL AND provider_lease_job_id='job_good' AND provider_lease_effect_id='effect_good' AND provider_lease_expires_at IS NOT NULL`, 1},
		{`SELECT COUNT(*) FROM adapter_outbox WHERE id='job_move_terminal' AND state='failed' AND generation=4`, 1},
		{`SELECT COUNT(*) FROM adapter_route_move_targets WHERE move_id='move_repair'`, 2},
		{`SELECT COUNT(*) FROM adapter_route_moves WHERE id='move_foreign' AND state='scrubbing'`, 1},
		{`SELECT COUNT(*) FROM adapter_outbox WHERE id='job_foreign' AND state='queued'`, 1},
		{`SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_foreign' AND active_job_id='job_foreign' AND generation=5 AND paused_at IS NULL`, 1},
		{`SELECT COUNT(*) FROM adapter_route_moves WHERE id='move_completed' AND state='completed'`, 1},
		{`SELECT COUNT(*) FROM adapter_outbox WHERE id='job_other' AND state='running'`, 1},
	} {
		if n := countUpgrade(t, cfg, tc.query); n != tc.want {
			t.Fatalf("%s = %d; want %d", tc.query, n, tc.want)
		}
	}
}
