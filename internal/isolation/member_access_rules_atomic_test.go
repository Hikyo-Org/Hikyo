package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestMemberAccessRuleAtomicReplacement(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		mode, envs := onlyEnvs(envA1)
		old := service.RuleSpec{Target: carol, Capability: domain.CapReveal, Org: orgA, Where: whereIn(mode, envs, domain.AxisOnly, folderItem("db"))}
		f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapRead, Org: orgA, Where: whereIn(mode, envs, domain.AxisAll)})
		id := f.create(t, orgAdmin, old)
		replacement := old
		replacement.Where = whereIn(mode, envs, domain.AxisOnly, folderItem("stripe"))
		// A stale removal must leave the original capability and selector intact.
		_, err := f.rules.Replace(t.Context(), service.LocalPrincipal(orgAdmin), service.ReplaceRulesSpec{Org: orgA, Target: carol, Revoke: []string{id, "rul_missing"}, Create: []service.RuleSpec{replacement}})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("stale replacement=%v", err)
		}
		dev := scopeEnv(orgA, prjA1, envA1)
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "DB_PASSWORD", true); err != nil {
			t.Fatalf("failed replacement changed original reach: %v", err)
		}
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "STRIPE_KEY", true); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("failed replacement widened reach: %v", err)
		}
		rows, err := f.rules.Replace(t.Context(), service.LocalPrincipal(orgAdmin), service.ReplaceRulesSpec{Org: orgA, Target: carol, Revoke: []string{id}, Create: []service.RuleSpec{replacement}})
		if err != nil || len(rows) != 1 {
			t.Fatalf("atomic replacement rows=%v err=%v", rows, err)
		}
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "STRIPE_KEY", true); err != nil {
			t.Fatalf("replacement positive control: %v", err)
		}
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "DB_PASSWORD", true); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("replacement retained original reach: %v", err)
		}
		// A denied addition refuses the batch without deleting its old row.
		unowned := replacement
		unowned.Where.Projects = []domain.ProjectID{prjB1}
		_, err = f.rules.Replace(t.Context(), service.LocalPrincipal(carol), service.ReplaceRulesSpec{Org: orgA, Target: carol, Revoke: []string{rows[0].ID}, Create: []service.RuleSpec{unowned}})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unauthorized batch=%v", err)
		}
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "STRIPE_KEY", true); err != nil {
			t.Fatalf("denied batch removed original rule: %v", err)
		}
		invalidReference := replacement
		invalidReference.Where = whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: "key_missing"})
		_, err = f.rules.Replace(t.Context(), service.LocalPrincipal(orgAdmin), service.ReplaceRulesSpec{Org: orgA, Target: carol, Revoke: []string{rows[0].ID}, Create: []service.RuleSpec{invalidReference}})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid reference batch=%v", err)
		}
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "STRIPE_KEY", true); err != nil {
			t.Fatalf("invalid reference removed original: %v", err)
		}
		// Invalid additions must roll back all requested removals.
		bad := old
		bad.Target = dave
		_, err = f.rules.Replace(t.Context(), service.LocalPrincipal(orgAdmin), service.ReplaceRulesSpec{Org: orgA, Target: carol, Revoke: []string{rows[0].ID}, Create: []service.RuleSpec{bad}})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("cross-person replacement=%v", err)
		}
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "STRIPE_KEY", true); err != nil {
			t.Fatalf("invalid batch removed rule: %v", err)
		}
	})
}

func TestMemberAccessRuleAtomicSelfChange(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		where := whereIn(domain.AxisAll, nil, domain.AxisAll)
		first := f.create(t, orgAdmin, service.RuleSpec{Target: orgAdmin, Capability: domain.CapRead, Org: orgA, Where: where})
		second := f.create(t, orgAdmin, service.RuleSpec{Target: orgAdmin, Capability: domain.CapEdit, Org: orgA, Where: where})
		rows, err := f.rules.Replace(t.Context(), service.LocalPrincipal(orgAdmin), service.ReplaceRulesSpec{Org: orgA, Target: orgAdmin, Revoke: []string{first, second}, Create: []service.RuleSpec{
			{Target: orgAdmin, Capability: domain.CapRead, Org: orgA, Where: where},
			{Target: orgAdmin, Capability: domain.CapPublish, Org: orgA, Where: where},
		}})
		if err != nil || len(rows) != 2 {
			t.Fatalf("self batch=%v err=%v", rows, err)
		}
		listed, err := f.rules.List(t.Context(), service.LocalPrincipal(orgAdmin), scopeProject(orgA, prjA1))
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range listed {
			if row.ID == first || row.ID == second {
				t.Fatalf("old self part survived: %s", row.ID)
			}
		}
	})
}
