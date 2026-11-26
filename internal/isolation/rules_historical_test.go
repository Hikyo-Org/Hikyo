package isolation

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
)

// Source schemas 89 through 98 hold the containment columns but no
// rules.project_mode (#1446, migration 00099), and their rule_items always
// name a project. A recovery drill against such an archive must read rules
// without the column and read every stored rule as project-bound; the current
// resolver must not run there at all.
func TestHistoricalRecoveryBeforeAllProjectsSQLite(t *testing.T) {
	cfg, db := scratchSQLite(t, "before-all-projects.db")
	seedBeforeAllProjects(t, cfg, func(stmt string) error { _, err := db.ExecContext(t.Context(), stmt); return err })
	assertBeforeAllProjectsReads(t, authn.NewHistoricalRecoverySQLite(db, 98))
	if _, err := authn.NewSQLite(db).Rules(t.Context(), "usr_hist"); err == nil {
		t.Fatal("the current resolver read rules on a schema that predates rules.project_mode")
	}
}

func TestHistoricalRecoveryBeforeAllProjectsPostgres(t *testing.T) {
	cfg, conn := scratchPostgres(t, "before_all_projects")
	seedBeforeAllProjects(t, cfg, func(stmt string) error { _, err := conn.Exec(t.Context(), stmt); return err })
	assertBeforeAllProjectsReads(t, authn.NewHistoricalRecoveryPG(conn, 98))
	if _, err := authn.NewPG(conn).Rules(t.Context(), "usr_hist"); err == nil {
		t.Fatal("the current resolver read rules on a schema that predates rules.project_mode")
	}
}

func seedBeforeAllProjects(t *testing.T, cfg store.Config, exec func(string) error) {
	t.Helper()
	if err := migrate.RunUpTo(t.Context(), cfg, 98); err != nil {
		t.Fatal(err)
	}
	const at = `'2026-01-01T00:00:00Z'`
	for _, stmt := range []string{
		`INSERT INTO principals (id, kind, created_at) VALUES ('usr_hist', 'human', ` + at + `)`,
		`INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ('org_hist', 'hist', TRUE, '{}', ` + at + `)`,
		`INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_hist', 'org_hist', 'hist', ` + at + `)`,
		`INSERT INTO rules (id, principal_id, capability, org_id, env_mode, key_mode, created_by, created_at)
		 VALUES ('rul_hist', 'usr_hist', 'read', 'org_hist', 'all', 'all', 'usr_hist', ` + at + `)`,
		`INSERT INTO rule_items (id, rule_id, org_id, axis, project_id) VALUES ('rit_hist', 'rul_hist', 'org_hist', 'project', 'prj_hist')`,
		`INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_hist', 'machine', 'automation', ` + at + `)`,
		`INSERT INTO rules (id, principal_id, capability, org_id, env_mode, key_mode, created_by, created_at)
		 VALUES ('rul_mhist', 'mch_hist', 'read', 'org_hist', 'all', 'all', 'usr_hist', ` + at + `)`,
		`INSERT INTO rule_items (id, rule_id, org_id, axis, project_id) VALUES ('rit_mhist', 'rul_mhist', 'org_hist', 'project', 'prj_hist')`,
	} {
		if err := exec(stmt); err != nil {
			t.Fatalf("seed exec: %v", err)
		}
	}
}

func assertBeforeAllProjectsReads(t *testing.T, r *authn.Resolver) {
	t.Helper()
	ctx := t.Context()
	rules, err := r.Rules(ctx, "usr_hist")
	wantRule(t, "rules", "rul_hist", rules, err)
	rules, err = r.RulesForProject(ctx, "org_hist", "prj_hist")
	wantRule(t, "project census", "rul_hist", rules, err)
	rules, err = r.MachineRules(ctx, "mch_hist")
	wantRule(t, "machine rules", "rul_mhist", rules, err)
	if machine, err := r.MachineRules(ctx, "usr_hist"); err != nil || len(machine) != 0 {
		t.Fatalf("historical machine rules for a person: %+v, %v", machine, err)
	}
}

func wantRule(t *testing.T, what, id string, rules []domain.Rule, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("historical %s: %v", what, err)
	}
	if len(rules) != 1 || rules[0].ID != id || rules[0].Where.AllProjects ||
		len(rules[0].Where.Projects) != 1 || rules[0].Where.Projects[0] != "prj_hist" {
		t.Fatalf("historical %s: want %s bound to prj_hist only, got %+v", what, id, rules)
	}
}
