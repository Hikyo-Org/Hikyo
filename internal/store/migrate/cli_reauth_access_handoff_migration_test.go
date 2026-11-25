package migrate

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestCLIReauthAccessHandoffMigrationSQLite(t *testing.T) {
	testCLIReauthAccessHandoffMigration(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "access-handoff.db")})
}

func TestCLIReauthAccessHandoffMigrationPostgres(t *testing.T) {
	testCLIReauthAccessHandoffMigration(t, postgresTestConfig(t, "cli_reauth_access_handoff"))
}

// Migration 00098 admits the emergency temporary access handoff (#1531): its
// purpose and operation, with an empty key set. A live row survives the rebuild.
func testCLIReauthAccessHandoffMigration(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 97); err != nil {
		t.Fatal(err)
	}
	db := migrationFixtureSQL(t, cfg)
	exec := func(stmt string) error {
		_, err := db.ExecContext(ctx, socialSQL(cfg, stmt))
		return err
	}
	blob := 10
	handoff := func(id, purpose, operation, keySet string) string {
		blob++
		return fmt.Sprintf("INSERT INTO cli_reauth_handoffs (id,state_verifier,session_id,principal_id,purpose,operation,environment_set,key_set,pkce_challenge,redirect_uri,created_at,expires_at) VALUES ('%s',{b%d},'ses_social','prn_social','%s','%s','env_a','%s','challenge','http://127.0.0.1/cb',%s,%s)",
			id, blob, purpose, operation, keySet, socialTS, socialTS)
	}
	for i, stmt := range []string{
		"INSERT INTO principals (id,kind,created_at) VALUES ('prn_social','human'," + socialTS + ")",
		socialSession("ses_social", "3", "", "", ""),
		handoff("clh_live", "mint", "identity.credential-mint", ""),
	} {
		if err := exec(stmt); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if err := exec(handoff("clh_before", "access", "access.bypass", "")); err == nil {
		t.Fatal("00097 admitted an access handoff")
	}
	if err := RunUpTo(ctx, cfg, 98); err != nil {
		t.Fatal(err)
	}
	var live int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cli_reauth_handoffs WHERE id = 'clh_live' AND purpose = 'mint'").Scan(&live); err != nil || live != 1 {
		t.Fatalf("the live handoff did not survive 00098: %d, %v", live, err)
	}
	admitted := []struct{ purpose, operation, keySet string }{
		{"adapter", "adapter.sync", ""},
		{"reveal", "value.reveal", "key_a"},
		{"bypass", "approval.bypass", "key_a"},
		{"mint", "identity.credential-mint", ""},
		{"access", "access.bypass", ""},
	}
	for i, h := range admitted {
		if err := exec(handoff(fmt.Sprintf("clh_%d_%s", i, h.purpose), h.purpose, h.operation, h.keySet)); err != nil {
			t.Errorf("%s handoff refused after 00098: %v", h.purpose, err)
		}
	}
	for i, h := range []struct{ name, purpose, operation, keySet string }{
		{"access with keys", "access", "access.bypass", "key_a"},
		{"unknown access operation", "access", "access.revoke", ""},
		{"mint with keys", "mint", "identity.credential-mint", "key_a"},
		{"reveal without keys", "reveal", "value.reveal", ""},
	} {
		if err := exec(handoff(fmt.Sprintf("clh_bad_%d", i), h.purpose, h.operation, h.keySet)); err == nil {
			t.Errorf("%s was admitted", h.name)
		}
	}
}
