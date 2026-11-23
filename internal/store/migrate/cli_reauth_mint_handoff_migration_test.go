package migrate

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestCLIReauthMintHandoffMigrationSQLite(t *testing.T) {
	testCLIReauthMintHandoffMigration(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "mint-handoff.db")})
}

func TestCLIReauthMintHandoffMigrationPostgres(t *testing.T) {
	testCLIReauthMintHandoffMigration(t, postgresTestConfig(t, "cli_reauth_mint_handoff"))
}

// Migration 00097 admits the mint handoff (#1516) and the publish, approve,
// reject and bypass handoffs the service already starts, which the checks of
// 00074 refused. A live row survives the rebuild.
func testCLIReauthMintHandoffMigration(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 96); err != nil {
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
		handoff("clh_live", "reveal", "value.reveal", "key_a"),
	} {
		if err := exec(stmt); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if err := exec(handoff("clh_before", "mint", "identity.credential-mint", "")); err == nil {
		t.Fatal("00074 admitted a mint handoff")
	}
	if err := RunUpTo(ctx, cfg, 97); err != nil {
		t.Fatal(err)
	}
	var live int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cli_reauth_handoffs WHERE id = 'clh_live' AND key_set = 'key_a'").Scan(&live); err != nil || live != 1 {
		t.Fatalf("the live handoff did not survive 00097: %d, %v", live, err)
	}
	admitted := []struct{ purpose, operation, keySet string }{
		{"adapter", "adapter.sync", ""},
		{"reveal", "value.reveal", "key_a"},
		{"copy", "value.copy-source", "key_a"},
		{"publish", "value.copy-destination", "key_a"},
		{"approve", "approval.vote", "key_a"},
		{"reject", "approval.vote", "key_a"},
		{"bypass", "approval.bypass", "key_a"},
		{"mint", "identity.credential-mint", ""},
	}
	for i, h := range admitted {
		if err := exec(handoff(fmt.Sprintf("clh_%d_%s", i, h.purpose), h.purpose, h.operation, h.keySet)); err != nil {
			t.Errorf("%s handoff refused after 00097: %v", h.purpose, err)
		}
	}
	for name, h := range map[string]struct{ purpose, operation, keySet string }{
		"mint with keys":      {"mint", "identity.credential-mint", "key_a"},
		"reveal without keys": {"reveal", "value.reveal", ""},
		"access":              {"access", "access.bypass", ""},
		"unknown operation":   {"mint", "identity.credential-rotate", ""},
	} {
		if err := exec(handoff("clh_bad_"+fmt.Sprint(len(name)), h.purpose, h.operation, h.keySet)); err == nil {
			t.Errorf("%s was admitted", name)
		}
	}
}
