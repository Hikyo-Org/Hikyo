package migrate

import (
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// 00083 widens transit_keys.algorithm for mldsa65 (#1129). On SQLite that is a
// rebuild of the parent of two tables, so the proof is that existing keys,
// their versions and caller entries survive unchanged, the child foreign keys
// and cascades still bind, the indexes still bite, and the new identity is
// admitted while every other value stays refused.
func TestTransitMLDSA65MigrationSQLite(t *testing.T) {
	testTransitMLDSA65Migration(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "transit.db")})
}

func TestTransitMLDSA65MigrationPostgres(t *testing.T) {
	testTransitMLDSA65Migration(t, postgresTestConfig(t, "transit_mldsa65"))
}

func testTransitMLDSA65Migration(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 82); err != nil {
		t.Fatal(err)
	}
	db := migrationFixtureSQL(t, cfg)
	// Material and public keys are seeded as exact byte strings and compared byte
	// for byte afterwards, on either engine's blob literal syntax.
	blob := func(hex string) string {
		if cfg.Engine == store.EnginePostgres {
			return `'\x` + hex + `'::bytea`
		}
		return `X'` + hex + `'`
	}
	const key = `INSERT INTO transit_keys (id,org_id,project_id,environment_id,name,algorithm,custody,allowed_operations,state,latest_version,min_encrypt_version,min_decrypt_version,compromised_through_version,created_by,created_at,updated_at) VALUES `
	for _, statement := range []string{
		`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_t','T',TRUE,'{}','2026-09-01T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_t','org_t','T','2026-09-01T00:00:00Z')`,
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_t','org_t','prj_t','prod','','2026-09-01T00:00:00Z',0)`,
		key + `('tk_ed','org_t','prj_t','env_t','signer','ed25519','software','sign,verify','active',2,1,1,1,'usr_t','2026-09-01T00:00:00Z','2026-09-02T00:00:00Z')`,
		`INSERT INTO transit_key_versions (id,org_id,project_id,environment_id,key_id,version,material_ciphertext,public_key,created_at) VALUES ('tkv_1','org_t','prj_t','env_t','tk_ed',1,` + blob("00112233445566778899aabbccddeeff") + `,` + blob("a1a2a3a4a5a6a7a8") + `,'2026-09-01T00:00:00Z')`,
		`INSERT INTO transit_key_versions (id,org_id,project_id,environment_id,key_id,version,material_ciphertext,public_key,created_at) VALUES ('tkv_2','org_t','prj_t','env_t','tk_ed',2,` + blob("ffeeddccbbaa99887766554433221100") + `,` + blob("b1b2b3b4b5b6b7b8") + `,'2026-09-02T00:00:00Z')`,
		`INSERT INTO transit_keys (id,org_id,project_id,environment_id,name,algorithm,custody,allowed_operations,state,latest_version,min_encrypt_version,min_decrypt_version,min_available_version,compromised_through_version,rotation_period_seconds,purge_started,deletion_after,created_by,created_at,updated_at) VALUES ('tk_wide','org_t','prj_t','env_t','wide','ed25519','software','sign','pending-deletion',5,4,3,2,1,3600,1,'2026-10-01T00:00:00Z','usr_t','2026-09-01T00:00:00Z','2026-09-02T00:00:00Z')`,
		`INSERT INTO transit_key_callers (org_id,project_id,environment_id,key_id,principal_id,operations) VALUES ('org_t','prj_t','env_t','tk_ed','usr_t','sign')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed %q: %v", statement, err)
		}
	}
	if _, err := db.ExecContext(ctx, key+`('tk_early','org_t','prj_t','env_t','early','mldsa65','software','sign,verify','active',1,1,1,0,'usr_t','2026-09-01T00:00:00Z','2026-09-01T00:00:00Z')`); err == nil {
		t.Fatal("pre-00083 schema admitted mldsa65")
	}
	if err := Run(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	// Existing identities are untouched: algorithm, version window, compromise
	// mark, both versions' material and public key, and the caller entry.
	if n := countUpgrade(t, cfg, `SELECT COUNT(*) FROM transit_keys WHERE id='tk_ed' AND algorithm='ed25519' AND custody='software' AND state='active' AND latest_version=2 AND min_encrypt_version=1 AND min_decrypt_version=1 AND compromised_through_version=1 AND name='signer'`); n != 1 {
		t.Fatalf("rebuilt transit_keys lost row state: %d", n)
	}
	if n := countUpgrade(t, cfg, `SELECT COUNT(*) FROM transit_keys WHERE id='tk_wide' AND org_id='org_t' AND project_id='prj_t' AND environment_id='env_t' AND name='wide' AND algorithm='ed25519' AND custody='software' AND allowed_operations='sign' AND exportable=0 AND state='pending-deletion' AND latest_version=5 AND min_encrypt_version=4 AND min_decrypt_version=3 AND min_available_version=2 AND compromised_through_version=1 AND rotation_period_seconds=3600 AND purge_started=1 AND deletion_after='2026-10-01T00:00:00Z' AND created_by='usr_t' AND created_at='2026-09-01T00:00:00Z' AND updated_at='2026-09-02T00:00:00Z'`); n != 1 {
		t.Fatalf("rebuilt transit_keys changed a copied column: %d", n)
	}
	if n := countUpgrade(t, cfg, `SELECT COUNT(*) FROM transit_key_versions WHERE key_id='tk_ed' AND (
		(version=1 AND material_ciphertext=`+blob("00112233445566778899aabbccddeeff")+` AND public_key=`+blob("a1a2a3a4a5a6a7a8")+` AND created_at IS NOT NULL) OR
		(version=2 AND material_ciphertext=`+blob("ffeeddccbbaa99887766554433221100")+` AND public_key=`+blob("b1b2b3b4b5b6b7b8")+` AND created_at IS NOT NULL))`); n != 2 {
		t.Fatalf("sealed material or public keys changed across the rebuild: %d of 2 versions match byte for byte", n)
	}
	if n := countUpgrade(t, cfg, `SELECT COUNT(*) FROM transit_key_callers WHERE key_id='tk_ed' AND principal_id='usr_t' AND operations='sign'`); n != 1 {
		t.Fatalf("caller entry did not survive: %d", n)
	}
	// The new identity is admitted; every neighbour stays refused.
	if _, err := db.ExecContext(ctx, key+`('tk_pq','org_t','prj_t','env_t','pq-signer','mldsa65','software','sign,verify','active',1,1,1,0,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`); err != nil {
		t.Fatalf("post-00083 mldsa65: %v", err)
	}
	for _, bad := range []string{"mldsa44", "mldsa87", "ml-dsa-65", "MLDSA65", "slh-dsa"} {
		if _, err := db.ExecContext(ctx, key+`('tk_bad_`+bad+`','org_t','prj_t','env_t','bad-`+bad+`','`+bad+`','software','sign,verify','active',1,1,1,0,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`); err == nil {
			t.Fatalf("algorithm %q admitted: the CHECK is no longer closed", bad)
		}
	}
	// Every other CHECK survived the rebuild.
	for what, bad := range map[string]string{
		"exportable":                 `('tk_x1','org_t','prj_t','env_t','x1','ed25519','software','sign','active',1,1,1,0,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`,
		"custody":                    `('tk_x2','org_t','prj_t','env_t','x2','ed25519','hsm','sign','active',1,1,1,0,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`,
		"state":                      `('tk_x3','org_t','prj_t','env_t','x3','ed25519','software','sign','bogus',1,1,1,0,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`,
		"version window":             `('tk_x4','org_t','prj_t','env_t','x4','ed25519','software','sign','active',1,1,2,0,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`,
		"compromise above latest":    `('tk_x5','org_t','prj_t','env_t','x5','ed25519','software','sign','active',1,1,1,2,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`,
		"latest below one":           `('tk_x6','org_t','prj_t','env_t','x6','ed25519','software','sign','active',0,1,1,0,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`,
		"pending without a deadline": `('tk_x7','org_t','prj_t','env_t','x7','ed25519','software','sign','pending-deletion',1,1,1,0,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`,
	} {
		statement := key + bad
		if what == "exportable" {
			statement = `INSERT INTO transit_keys (id,org_id,project_id,environment_id,name,algorithm,custody,allowed_operations,exportable,state,latest_version,min_encrypt_version,min_decrypt_version,compromised_through_version,created_by,created_at,updated_at) VALUES ('tk_x1','org_t','prj_t','env_t','x1','ed25519','software','sign',1,'active',1,1,1,0,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`
		}
		if _, err := db.ExecContext(ctx, statement); err == nil {
			t.Errorf("the %s CHECK no longer refuses", what)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO transit_keys (id,org_id,project_id,environment_id,name,algorithm,custody,allowed_operations,state,latest_version,min_encrypt_version,min_decrypt_version,compromised_through_version,rotation_period_seconds,created_by,created_at,updated_at) VALUES ('tk_x8','org_t','prj_t','env_t','x8','ed25519','software','sign','active',1,1,1,0,60,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`); err == nil {
		t.Error("the rotation period CHECK no longer refuses 60 seconds")
	}
	// The live-name unique index and the child keys survived.
	if _, err := db.ExecContext(ctx, key+`('tk_dup','org_t','prj_t','env_t','pq-signer','ed25519','software','sign,verify','active',1,1,1,0,'usr_t','2026-09-03T00:00:00Z','2026-09-03T00:00:00Z')`); err == nil {
		t.Fatal("rebuilt transit_keys admitted a second live key of one name")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO transit_key_versions (id,org_id,project_id,environment_id,key_id,version,public_key,created_at) VALUES ('tkv_orphan','org_t','prj_t','env_t','tk_missing',1,'p','2026-09-01T00:00:00Z')`); err == nil {
		t.Fatal("transit_key_versions lost its foreign key to the rebuilt parent")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO transit_key_versions (id,org_id,project_id,environment_id,key_id,version,public_key,created_at) VALUES ('tkv_pq','org_t','prj_t','env_t','tk_pq',1,'p','2026-09-03T00:00:00Z')`); err != nil {
		t.Fatalf("version of the new key: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM environments WHERE id='env_t'`); err != nil {
		t.Fatalf("environment delete: %v", err)
	}
	if n := countUpgrade(t, cfg, `SELECT (SELECT COUNT(*) FROM transit_keys) + (SELECT COUNT(*) FROM transit_key_versions) + (SELECT COUNT(*) FROM transit_key_callers)`); n != 0 {
		t.Fatalf("the environment cascade no longer reaches transit rows: %d", n)
	}
}
