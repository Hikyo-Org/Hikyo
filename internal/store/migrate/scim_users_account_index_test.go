package migrate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestSCIMUsersAccountIndexSQLite(t *testing.T) {
	testSCIMUsersAccountIndex(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "scim-account.db")})
}

func TestSCIMUsersAccountIndexPostgres(t *testing.T) {
	testSCIMUsersAccountIndex(t, postgresTestConfig(t, "scim_users_account_index"))
}

// The principal footprint read reaches scim_users through the account alone;
// the older (binding_id, account_id) index cannot serve that lookup. The index
// is read-only support: no rows change and the query answer is the same.
func testSCIMUsersAccountIndex(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 82); err != nil {
		t.Fatal(err)
	}
	db := migrationFixtureSQL(t, cfg)
	exec := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	const at = "'2026-01-01T00:00:00Z'"
	for _, query := range []string{
		`INSERT INTO orgs(id,name,active,metadata,created_at) VALUES ('org_index','Footprint',TRUE,'{}',` + at + `)`,
		`INSERT INTO principals(id,kind,created_at) VALUES ('usr_connection','human',` + at + `)`,
		`INSERT INTO scim_bindings(id,org_id,provider_kind,provider_id,provider_slug,provider_issuer,subject_source,connection_principal_id,created_at) VALUES ('bnd_index','org_index','oidc','prov','prov','https://idp.example','externalId','usr_connection',` + at + `)`,
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<19999) INSERT INTO principals(id,kind,created_at) SELECT 'usr_fp_'||i,'human',` + at + ` FROM n`,
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<19999) INSERT INTO accounts(id,principal_id,username,display_name,created_at) SELECT 'acc_fp_'||i,'usr_fp_'||i,'user'||i,'User',` + at + ` FROM n`,
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<19999) INSERT INTO scim_users(id,org_id,binding_id,account_id,user_name,user_name_lower,subject,created_at,updated_at) SELECT 'scu_fp_'||i,'org_index','bnd_index','acc_fp_'||i,'user'||i,'user'||i,'sub'||i,` + at + `,` + at + ` FROM n`,
	} {
		exec(query)
	}
	query := indexFixtureQuery(t, cfg.Engine, "permission", "ListPrincipalFootprintOrgs", map[string]string{"principal_id": "'usr_fp_777'"})
	read := func() {
		t.Helper()
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var orgs []string
		for rows.Next() {
			var org string
			if err := rows.Scan(&org); err != nil {
				t.Fatal(err)
			}
			orgs = append(orgs, org)
		}
		if err := rows.Err(); err != nil || len(orgs) != 1 || orgs[0] != "org_index" {
			t.Fatalf("footprint = %v, %v; want exactly org_index", orgs, err)
		}
	}
	read()
	if err := Run(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	exec("ANALYZE scim_users")
	read()
	plan := indexFixturePlan(t, db, cfg.Engine, query)
	t.Logf("after:\n%s", plan)
	if !strings.Contains(plan, "scim_users_account_only") {
		t.Fatalf("footprint read does not use the account index:\n%s", plan)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM scim_users").Scan(&count); err != nil || count != 20000 {
		t.Fatalf("scim_users count=%d err=%v", count, err)
	}
}
