package isolation

import (
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	"github.com/Hikyo-Org/hikyo/internal/testutil/reflectx"
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
	rules, err = peopleInCensus(r.RulesForProject(ctx, "org_hist", "prj_hist"))
	wantRule(t, "project census", "rul_hist", rules, err)
	rules, err = r.MachineRules(ctx, "mch_hist")
	wantRule(t, "machine rules", "rul_mhist", rules, err)
	if machine, err := r.MachineRules(ctx, "usr_hist"); err != nil || len(machine) != 0 {
		t.Fatalf("historical machine rules for a person: %+v, %v", machine, err)
	}
}

// peopleInCensus is a project census without its machine rules.
func peopleInCensus(census []authn.CensusRule, err error) ([]domain.Rule, error) {
	var rules []domain.Rule
	for _, c := range census {
		if !c.Machine {
			rules = append(rules, c.Rule)
		}
	}
	return rules, err
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

// The other side of the recovery boundaries: bound to the current source
// schema (102, which names rule environments, #1447; 99 added
// rules.project_mode), the historical recovery resolver reads exactly what the
// ordinary resolver reads, including an all-projects rule, which must stay
// all-projects rather than be mapped to 'only'. A schema-99 archive is read in
// TestRuleEnvNamesMigration*, whose fixture still holds environment ids.
func TestHistoricalRecoveryAtAllProjectsSchemaReadsAsCurrent(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		f := seedAllProjectsFixture(t, db)
		p1, _ := f.newProject(t, "hist-a"), f.newProject(t, "hist-b")
		all, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), claimAll(t, db, service.RuleSpec{Target: apHolder,
			Capability: domain.CapRead, Org: orgAP, Where: allProjectsWhere(domain.AxisAll, "secret")}))
		if err != nil {
			t.Fatalf("create all-projects rule: %v", err)
		}
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), service.RuleSpec{Target: apHolder, Capability: domain.CapEdit, Org: orgAP,
			Where: domain.Where{Projects: []domain.ProjectID{p1}, EnvMode: domain.AxisAll, KeyMode: domain.AxisAll}}); err != nil {
			t.Fatalf("create project-bound rule: %v", err)
		}
		execRaw(t, db, "INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, created_by, created_at) VALUES ('rul_hist_mch', 'mch_ap', 'read', 'org_ap', 'only', 'all', 'all', 'usr_apadmin', "+ts+")")
		execRaw(t, db, "INSERT INTO rule_items (id, rule_id, org_id, axis, project_id) VALUES ('rit_hist_mch', 'rul_hist_mch', 'org_ap', 'project', '"+string(p1)+"')")

		current, historical := authn.NewSQLite(db.SQLiteRead()), authn.NewHistoricalRecoverySQLite(db.SQLiteRead(), 102)
		if db.Engine() == store.EnginePostgres {
			current, historical = authn.NewPG(db.PG()), authn.NewHistoricalRecoveryPG(db.PG(), 102)
		}
		same := func(what string, read func(*authn.Resolver) ([]domain.Rule, error), wantLen int) []domain.Rule {
			t.Helper()
			got, err := read(historical)
			if err != nil {
				t.Fatalf("historical %s at schema 102: %v", what, err)
			}
			want, err := read(current)
			if err != nil {
				t.Fatalf("current %s: %v", what, err)
			}
			if len(want) != wantLen || !reflectx.Equal(got, want) {
				t.Fatalf("%s at schema 102 = %+v, want the current resolver's %d rules %+v", what, got, wantLen, want)
			}
			return got
		}
		human := same("rules", func(r *authn.Resolver) ([]domain.Rule, error) { return r.Rules(ctx, apHolder) }, 2)
		census := same("project census", func(r *authn.Resolver) ([]domain.Rule, error) {
			return peopleInCensus(r.RulesForProject(ctx, string(orgAP), string(p1)))
		}, 2)
		same("machine rules", func(r *authn.Resolver) ([]domain.Rule, error) { return r.MachineRules(ctx, "mch_ap") }, 1)
		for what, rules := range map[string][]domain.Rule{"rules": human, "project census": census} {
			i := slices.IndexFunc(rules, func(r domain.Rule) bool { return r.ID == all.ID })
			if i < 0 || !rules[i].Where.AllProjects || len(rules[i].Where.Keys[domain.AnyProject]) != 1 {
				t.Fatalf("%s at schema 102: all-projects rule %s read as %+v, want all projects with its folder", what, all.ID, rules)
			}
		}
	})
}
