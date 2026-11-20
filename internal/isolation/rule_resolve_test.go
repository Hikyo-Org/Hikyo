package isolation

import (
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// "Who can...?" (member-access-rules ADR D10) at the server: the answer comes
// from the chokepoint's own evaluation, so a key-narrowed rule reaches only
// its keys, an unnamed key leaves it key-limited, and Reveal without See is
// reported apart.
func TestRuleResolve(t *testing.T) {
	forEngines(t, runRuleResolve)
}

func runRuleResolve(t *testing.T, db *store.DB) {
	f := seedRulesFixture(t, db)
	envMode, envs := onlyEnvs(envA1)
	f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapRead, Org: orgA,
		Where: whereIn(envMode, envs, domain.AxisAll)})
	carolReveal := f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapReveal, Org: orgA,
		Where: whereIn(envMode, envs, domain.AxisOnly, folderItem("db"))})
	daveReveal := f.create(t, orgAdmin, service.RuleSpec{Target: dave, Capability: domain.CapReveal, Org: orgA,
		Where: whereIn(domain.AxisAll, nil, domain.AxisAll, folderItem("db"))})
	dev := scopeEnv(orgA, prjA1, envA1)

	state := func(views []service.AccessView, p domain.PrincipalID) service.AccessView {
		i := slices.IndexFunc(views, func(v service.AccessView) bool { return v.Principal == p })
		if i < 0 {
			t.Fatalf("%s missing from the answer", p)
		}
		return views[i]
	}
	resolve := func(key string) []service.AccessView {
		t.Helper()
		views, err := f.rules.Resolve(t.Context(), service.LocalPrincipal(orgAdmin), dev, domain.CapReveal, key)
		if err != nil {
			t.Fatalf("resolve key %q: %v", key, err)
		}
		return views
	}

	views := resolve(f.dbKeyID)
	if v := state(views, carol); v.State != authz.AccessCan || !slices.Equal(v.Rules, []string{carolReveal}) {
		t.Fatalf("carol on db key = %+v, want can through %s", v, carolReveal)
	}
	if v := state(views, dave); v.State != authz.AccessExcepted || len(v.Excepts) != 1 || v.Excepts[0].RuleID != daveReveal ||
		len(v.Excepts[0].Keys) != 1 || v.Excepts[0].Keys[0].Folder != "db" {
		t.Fatalf("dave on db key = %+v, want excepted by folder db of %s", v, daveReveal)
	}
	if v := state(views, custodian); v.State != authz.AccessCan || len(v.Grants) == 0 {
		t.Fatalf("custodian = %+v, want can through a grant", v)
	}

	views = resolve(f.strKeyID)
	if v := state(views, carol); v.State != authz.AccessNone {
		t.Fatalf("carol on stripe key = %+v, want none (only-folder db)", v)
	}
	if v := state(views, dave); v.State != authz.AccessNeedsSee {
		t.Fatalf("dave on stripe key = %+v, want needs-see (Reveal without See)", v)
	}

	views = resolve("")
	if v := state(views, carol); v.State != authz.AccessExcepted || !v.Excepts[0].KeyLimited {
		t.Fatalf("carol without a key = %+v, want key-limited", v)
	}

	// The answer discloses membership: a principal without manage-members is
	// refused as if the environment did not exist.
	if _, err := f.rules.Resolve(t.Context(), service.LocalPrincipal(carol), dev, domain.CapReveal, ""); err == nil {
		t.Fatal("a principal without manage-members resolved access")
	}
}
