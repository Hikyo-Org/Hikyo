package isolation

import (
	"errors"
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// A rule create binds its names before the caller is proved (#1447). A rule
// over one named project reads only that project's environments, the same
// statements whether the project exists or not, so a refused caller's cost
// does not grow with the organization; a rule over several projects reads
// the organization's.
func TestRuleCreateReadsOneProjectsEnvironmentsBeforeAuthorization(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		const caller, target = domain.PrincipalID("usr_refs_caller"), domain.PrincipalID("usr_refs_target")
		for _, p := range []domain.PrincipalID{caller, target} {
			execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+string(p)+"', 'human', "+ts+")")
		}
		rules := &service.Rules{DB: db}
		create := func(t *testing.T, projects ...domain.ProjectID) ([]string, error) {
			t.Helper()
			var seen []string
			// The refusal's audit row lands in the tenant or the instance log by
			// whether the project resolves; the caller sees not found either way.
			restore := authn.SetQueryObserver(func(sql string) {
				if q := queryIdentity(sql); q != "InsertTenantAuditEvent" && q != "InsertInstanceAuditEvent" {
					seen = append(seen, q)
				}
			})
			defer restore()
			_, err := rules.Create(t.Context(), service.LocalPrincipal(caller), service.RuleSpec{
				Target: target, Capability: domain.CapRead, Org: orgA,
				Where: domain.Where{Projects: projects, EnvMode: domain.AxisOnly, EnvNames: []string{"dev"}, KeyMode: domain.AxisAll},
			})
			return seen, err
		}
		one, err := create(t, prjA1)
		if !errors.Is(err, domain.ErrNotFound) || slices.Contains(one, "ListRuleEnvRefsInOrg") || !slices.Contains(one, "ListRuleEnvRefsInProject") {
			t.Fatalf("refused single-project create: statements %v, err %v; want one project's environments, not found", one, err)
		}
		if missing, err := create(t, "prj_refs_missing"); !errors.Is(err, domain.ErrNotFound) || !slices.Equal(missing, one) {
			t.Fatalf("refused create over a missing project: statements %v, err %v; want %v, not found", missing, err, one)
		}
		// Control: the observer sees the organization-wide read where it is kept.
		if both, err := create(t, prjA1, prjA2); !errors.Is(err, domain.ErrNotFound) || !slices.Contains(both, "ListRuleEnvRefsInOrg") {
			t.Fatalf("refused two-project create: statements %v, err %v; want the organization's environments, not found", both, err)
		}
	})
}

// A project-local check binds the caller's rules from one read of the
// addressed project (#1447, the one read per rule-authorized check): a
// principal holding rules in three orgs costs one environment read, not one
// per org, and a missing project costs the same. Binding stays per org: the
// addressed org's named rule reaches its environment, and another org's rule
// reaches nothing in the addressed project.
func TestProjectCheckBindsRulesFromOneEnvironmentRead(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		const holder = "usr_refs_multi"
		for _, stmt := range []string{
			"INSERT INTO principals (id, kind, created_at) VALUES ('" + holder + "', 'human', " + ts + ")",
			"INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ('org_refs_c', 'refs-c', TRUE, '{}', " + ts + ")",
			"INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_refs_c1', 'org_refs_c', 'c1', " + ts + ")",
		} {
			execRaw(t, db, stmt)
		}
		for _, r := range []struct{ id, org, project, envMode, name string }{
			{"rul_refs_a", string(orgA), string(prjA1), "only", "dev"},
			{"rul_refs_b", string(orgB), string(prjB1), "all", ""},
			{"rul_refs_c", "org_refs_c", "prj_refs_c1", "all", ""},
		} {
			execRaw(t, db, "INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, created_by, created_at) VALUES ('"+
				r.id+"', '"+holder+"', 'read', '"+r.org+"', 'only', '"+r.envMode+"', 'all', 'usr_orgadmin', "+ts+")")
			execRaw(t, db, "INSERT INTO rule_items (id, rule_id, org_id, axis, project_id) VALUES ('"+r.id+"_p', '"+r.id+"', '"+r.org+"', 'project', '"+r.project+"')")
			if r.name != "" {
				execRaw(t, db, "INSERT INTO rule_items (id, rule_id, org_id, axis, env_name) VALUES ('"+r.id+"_e', '"+r.id+"', '"+r.org+"', 'env', '"+r.name+"')")
			}
		}
		check := func(t *testing.T, scope domain.Scope) ([]string, error) {
			t.Helper()
			var seen []string
			restore := authn.SetQueryObserver(func(sql string) {
				if q := queryIdentity(sql); q != "InsertTenantAuditEvent" && q != "InsertInstanceAuditEvent" {
					seen = append(seen, q)
				}
			})
			defer restore()
			// countedAuthorize builds its resolver under the observer, which
			// wraps the handle at construction.
			_, err := countedAuthorize(t, db, holder, scope)
			return seen, err
		}
		reads := func(seen []string) (project, org int) {
			for _, q := range seen {
				switch q {
				case "ListRuleEnvRefsInProject":
					project++
				case "ListRuleEnvRefsInOrg":
					org++
				}
			}
			return project, org
		}
		allowed, err := check(t, scopeEnv(orgA, prjA1, envA1))
		if err != nil {
			t.Fatalf("the org's named rule does not reach its dev: %v", err)
		}
		if project, org := reads(allowed); project != 1 || org != 0 {
			t.Fatalf("rule-authorized check over rules in three orgs: statements %v; want one project environment read", allowed)
		}
		// Another org's whole-project rule reaches nothing here.
		refused, err := check(t, scopeEnv(orgA, prjA1, envProd))
		if err == nil {
			t.Fatal("prod, which no rule of the org names, is reached")
		}
		if project, org := reads(refused); project != 1 || org != 0 {
			t.Fatalf("refused check: statements %v; want one project environment read", refused)
		}
		if missing, err := check(t, scopeEnv(orgA, "prj_refs_missing", "env_refs_missing")); err == nil || !slices.Equal(missing, refused) {
			t.Fatalf("missing project: statements %v, err %v; want the refused check's %v", missing, err, refused)
		}
		// Each org's rule still binds when its own org is addressed.
		if _, err := check(t, scopeEnv(orgB, prjB1, envB1)); err != nil {
			t.Fatalf("org b's rule does not reach its project: %v", err)
		}
	})
}
