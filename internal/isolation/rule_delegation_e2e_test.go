package isolation

import (
	"errors"
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
