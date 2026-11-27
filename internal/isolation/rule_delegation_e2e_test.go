package isolation

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestRuleDelegation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		mode, envs := onlyEnvs(envA1)
		where := whereIn(mode, envs, domain.AxisOnly, folderItem("db"))
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapManageMembers, Org: orgA, Where: where})
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapEdit, Org: orgA, Where: where})
		rule, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), service.RuleSpec{Target: gina, Capability: domain.CapEdit, Org: orgA, Where: where})
		if err != nil {
			t.Fatalf("delegate held permission inside managed folder: %v", err)
		}
		single, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), service.RuleSpec{Target: hank, Capability: domain.CapEdit, Org: orgA, Where: whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})})
		if err != nil {
			t.Fatalf("delegate stable key within managed folder: %v", err)
		}
		if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(frank), orgA, single.ID); err != nil {
			t.Fatalf("revoke stable key inside managed folder: %v", err)
		}

		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapRead, Org: orgA, Where: where})
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), service.RuleSpec{Target: gina, Capability: domain.CapRead, Org: orgA, Where: where}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("folder manager cannot confer environment-wide See: %v", err)
		}
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), service.RuleSpec{Target: gina, Capability: domain.CapReveal, Org: orgA, Where: where}); !errors.Is(err, service.ErrGrantorLacksCapability) {
			t.Fatalf("delegate unheld Reveal: %v", err)
		}
		for name, outside := range map[string]domain.Where{
			"sibling folder":      whereIn(mode, envs, domain.AxisOnly, folderItem("stripe")),
			"all keys":            whereIn(mode, envs, domain.AxisAll),
			"future environments": whereIn(domain.AxisAll, nil, domain.AxisOnly, folderItem("db")),
		} {
			if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), service.RuleSpec{Target: gina, Capability: domain.CapEdit, Org: orgA, Where: outside}); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("delegate outside %s: %v", name, err)
			}
		}
		sibling := f.create(t, orgAdmin, service.RuleSpec{Target: gina, Capability: domain.CapEdit, Org: orgA, Where: whereIn(mode, envs, domain.AxisOnly, folderItem("stripe"))})
		listed, err := f.rules.List(t.Context(), service.LocalPrincipal(frank), scopeProject(orgA, prjA1))
		if err != nil {
			t.Fatalf("list through narrowed Manage access: %v", err)
		}
		found := false
		for _, item := range listed {
			if item.ID == sibling {
				t.Fatal("listing disclosed outside-managed rule")
			}
			if item.ID == rule.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("listing omitted manageable rule")
		}

		if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(frank), orgA, sibling); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("revoke sibling folder rule: %v", err)
		}
		if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(frank), orgA, rule.ID); err != nil {
			t.Fatalf("revoke rule inside managed folder: %v", err)
		}
		grants := &service.Grants{DB: db}
		if _, err := grants.Create(t.Context(), service.LocalPrincipal(frank), service.GrantSpec{Target: gina, Capability: domain.CapEdit, Scope: scopeProject(orgA, prjA1)}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("narrowed manager cannot mint scope-wide legacy grant: %v", err)
		}
		// Complementary exceptions must union symbolically, including future
		// folders, rather than approximating reach from today's key rows.
		for _, capability := range []domain.Capability{domain.CapManageMembers, domain.CapEdit} {
			f.create(t, orgAdmin, service.RuleSpec{Target: ivan, Capability: capability, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisAll, domain.RuleKeyItem{KeyID: f.dbKeyID})})
			f.create(t, orgAdmin, service.RuleSpec{Target: ivan, Capability: capability, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})})
		}
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(ivan), service.RuleSpec{Target: hank, Capability: domain.CapEdit, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisAll)}); err != nil {
			t.Fatalf("delegate through complementary rule union: %v", err)
		}

	})
}

func TestRuleDelegationRootFolderException(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		exceptDB := whereIn(domain.AxisAll, nil, domain.AxisAll, folderItem("db"))
		exceptRoot := whereIn(domain.AxisAll, nil, domain.AxisAll, folderItem(""))
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapManageMembers, Org: orgA, Where: exceptDB})
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapEdit, Org: orgA, Where: exceptDB})
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), service.RuleSpec{Target: gina, Capability: domain.CapEdit, Org: orgA, Where: exceptDB}); err != nil {
			t.Fatalf("control delegation outside db subtree: %v", err)
		}
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), service.RuleSpec{Target: gina, Capability: domain.CapEdit, Org: orgA, Where: exceptRoot}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("root-only exception cannot widen management into db subtree: %v", err)
		}
		// Independently exercise the capability bound with broad management:
		// excluding root keys does not exclude any db key the grantor lacks.
		f.create(t, orgAdmin, service.RuleSpec{Target: hank, Capability: domain.CapManageMembers, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisAll)})
		f.create(t, orgAdmin, service.RuleSpec{Target: hank, Capability: domain.CapEdit, Org: orgA, Where: exceptDB})
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(hank), service.RuleSpec{Target: gina, Capability: domain.CapEdit, Org: orgA, Where: exceptDB}); err != nil {
			t.Fatalf("control capability delegation outside db subtree: %v", err)
		}
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(hank), service.RuleSpec{Target: gina, Capability: domain.CapEdit, Org: orgA, Where: exceptRoot}); !errors.Is(err, service.ErrGrantorLacksCapability) {
			t.Fatalf("root-only exception cannot widen held capability into db subtree: %v", err)
		}
	})
}

// An old client's create is refused while the target's rules except a name no
// environment carries (#1447), but only rules the caller may manage count: a
// folder-limited manager must not learn the exceptions of a rule outside its
// folders from the refusal.
func TestRuleHiddenExceptionsOnlyFromManageableRules(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		inDB := whereIn(domain.AxisAll, nil, domain.AxisOnly, folderItem("db"))
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapManageMembers, Org: orgA, Where: inDB})
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapEdit, Org: orgA, Where: inDB})
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapPublish, Org: orgA, Where: inDB})
		excepting := func(name, folder string) string {
			t.Helper()
			w := whereIn(domain.AxisAll, nil, domain.AxisOnly, folderItem(folder))
			w.EnvNames = []string{name}
			return f.create(t, orgAdmin, service.RuleSpec{Target: gina, Capability: domain.CapEdit, Org: orgA, Where: w})
		}
		excepting("ghost", "stripe")
		// An all-projects rule is bound per project and counts the same way.
		everywhere := allProjectsWhere(domain.AxisOnly, "stripe")
		everywhere.EnvNames = []string{"phantom"}
		f.create(t, orgAdmin, claimAll(t, db, service.RuleSpec{Target: gina, Capability: domain.CapEdit, Org: orgA, Where: everywhere}))
		legacy := service.RuleSpec{Target: gina, Capability: domain.CapPublish, Org: orgA, Where: inDB}
		// The stripe rule is outside frank's folders: it must not refuse, nor name.
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), legacy); err != nil {
			t.Fatalf("legacy create beside an unmanageable rule: %v", err)
		}
		// A rule frank manages still keeps its hidden exception, named.
		spectre := excepting("spectre", "db")
		legacy.Capability = domain.CapEdit
		namesOnly := func(t *testing.T, err error) {
			t.Helper()
			if !errors.Is(err, service.ErrRuleUnmatchedNamesDropped) {
				t.Fatalf("legacy create beside a manageable rule: %v, want the hidden exception kept", err)
			}
			var detailed interface{ SafeDetail() string }
			if !errors.As(err, &detailed) || !strings.Contains(detailed.SafeDetail(), "spectre") ||
				strings.Contains(detailed.SafeDetail(), "ghost") || strings.Contains(detailed.SafeDetail(), "phantom") {
				t.Fatalf("refusal detail %v names the wrong exceptions", err)
			}
		}
		_, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), legacy)
		namesOnly(t, err)
		// Replacing the manageable rule by a legacy one is refused the same way.
		_, err = f.rules.Replace(t.Context(), service.LocalPrincipal(frank), service.ReplaceRulesSpec{Org: orgA, Target: gina, Revoke: []string{spectre}, Create: []service.RuleSpec{legacy}})
		namesOnly(t, err)
	})
}

// An environment grant follows its id, so it never stands in for the name of
// a future environment (#1447): a manager excepting prod by rule while holding
// a grant on today's prod may delegate prod itself, but not every environment.
func TestRuleDelegationAllEnvironmentsIgnoresEnvironmentGrants(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		exceptProd := whereIn(domain.AxisAll, nil, domain.AxisAll)
		exceptProd.EnvNames = []string{"prod"}
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapManageMembers, Org: orgA, Where: exceptProd})
		execRaw(t, db, fmt.Sprintf("INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_frank_mm_prod', '%s', 'manage-members', 'org_a', '%s', '%s', %s)", frank, prjA1, envProd, ts))
		execRaw(t, db, fmt.Sprintf("INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_frank_read', '%s', 'read', 'org_a', '%s', NULL, %s)", frank, prjA1, ts))
		seedOrigins(t, db)
		delegate := func(w domain.Where) error {
			_, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), service.RuleSpec{Target: gina, Capability: domain.CapRead, Org: orgA, Where: w})
			return err
		}
		onlyProd := whereIn(domain.AxisOnly, nil, domain.AxisAll)
		onlyProd.EnvNames = []string{"prod"}
		if err := delegate(onlyProd); err != nil {
			t.Fatalf("delegating today's prod under the grant: %v", err)
		}
		if err := delegate(exceptProd); err != nil {
			t.Fatalf("delegating the rule's own reach: %v", err)
		}
		if err := delegate(whereIn(domain.AxisAll, nil, domain.AxisAll)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("delegating every environment = %v, want the uniform not found", err)
		}
	})
}
