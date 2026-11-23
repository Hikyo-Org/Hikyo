package isolation

import (
	"errors"
	"slices"
	"strconv"
	"strings"
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
	// custodian holds only org-scope grants: the project listings do not show
	// it, so the answer does not either (see TestRuleResolveIsolation).
	if i := slices.IndexFunc(views, func(v service.AccessView) bool { return v.Principal == custodian }); i >= 0 {
		t.Fatalf("custodian (org grants only) listed in a project answer: %+v", views[i])
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

// The answer shows no more than a project member manager's own listings: the
// project's grant lines and the project rules they may manage. Sibling-project
// members and rules, instance operators, org-only grant holders and rules the
// caller cannot manage appear neither as principals nor in any content.
func TestRuleResolveIsolation(t *testing.T) {
	forEngines(t, runRuleResolveIsolation)
}

func runRuleResolveIsolation(t *testing.T, db *store.DB) {
	f := seedRulesFixture(t, db)
	ctx := t.Context()
	dev := scopeEnv(orgA, prjA1, envA1)
	prod := scopeEnv(orgA, prjA1, envProd)
	devOnly, devEnvs := onlyEnvs(envA1)
	grants := grantSvc(db)

	// Sibling project A2: a member by grant (erin) and a read rule (gina).
	if _, err := grants.Create(ctx, service.LocalPrincipal(orgAdmin), service.GrantSpec{
		Target: erin, Capability: domain.CapRead, Scope: scopeProject(orgA, prjA2)}); err != nil {
		t.Fatalf("sibling grant: %v", err)
	}
	siblingRule := f.create(t, orgAdmin, service.RuleSpec{Target: gina, Capability: domain.CapRead, Org: orgA,
		Where: domain.Where{Projects: []domain.ProjectID{prjA2}, EnvMode: domain.AxisAll, KeyMode: domain.AxisAll}})
	// A project member by grant who also holds an instance-scope grant.
	if _, err := grants.Create(ctx, service.LocalPrincipal(orgAdmin), service.GrantSpec{
		Target: ivan, Capability: domain.CapRead, Scope: scopeProject(orgA, prjA1)}); err != nil {
		t.Fatalf("project grant: %v", err)
	}
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_iv_inst', 'usr_ivan', 'read', NULL, NULL, NULL, `+ts+`)`)
	seedOrigins(t, db)
	// carol: a read rule and an all-keys reveal rule in project A1.
	carolRead := f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapRead, Org: orgA,
		Where: whereIn(devOnly, devEnvs, domain.AxisAll)})
	carolReveal := f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapReveal, Org: orgA,
		Where: whereIn(devOnly, devEnvs, domain.AxisAll)})
	// hank: only a reveal rule.
	hankReveal := f.create(t, orgAdmin, service.RuleSpec{Target: hank, Capability: domain.CapReveal, Org: orgA,
		Where: whereIn(domain.AxisAll, nil, domain.AxisAll)})
	// dave: reveal limited to the db folder, with a read rule.
	f.create(t, orgAdmin, service.RuleSpec{Target: dave, Capability: domain.CapRead, Org: orgA,
		Where: whereIn(domain.AxisAll, nil, domain.AxisAll)})
	daveReveal := f.create(t, orgAdmin, service.RuleSpec{Target: dave, Capability: domain.CapReveal, Org: orgA,
		Where: whereIn(domain.AxisAll, nil, domain.AxisOnly, folderItem("db"))})
	// A machine holding Reveal while the project's machine-reveal opt-in is off
	// (written raw: the grant API refuses it), and an unclassified machine.
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_mw_rev', 'mch_workload', 'reveal', 'org_a', 'prj_a1', NULL, `+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_nc_read', 'mch_unclassed', 'read', 'org_a', 'prj_a1', NULL, `+ts+`)`)
	seedOrigins(t, db)

	resolve := func(by domain.PrincipalID, scope domain.Scope, capability domain.Capability, key string) []service.AccessView {
		t.Helper()
		views, err := f.rules.Resolve(ctx, service.LocalPrincipal(by), scope, capability, key)
		if err != nil {
			t.Fatalf("resolve %s %s: %v", by, capability, err)
		}
		return views
	}
	find := func(views []service.AccessView, p domain.PrincipalID) (service.AccessView, bool) {
		i := slices.IndexFunc(views, func(v service.AccessView) bool { return v.Principal == p })
		if i < 0 {
			return service.AccessView{}, false
		}
		return views[i], true
	}
	mustBeAbsent := func(views []service.AccessView, who ...domain.PrincipalID) {
		t.Helper()
		for _, p := range who {
			if v, ok := find(views, p); ok {
				t.Fatalf("%s must not be listed: %+v", p, v)
			}
		}
	}
	mustFind := func(views []service.AccessView, p domain.PrincipalID) service.AccessView {
		t.Helper()
		v, ok := find(views, p)
		if !ok {
			t.Fatalf("%s missing from %+v", p, views)
		}
		return v
	}
	mentions := func(views []service.AccessView, id string) bool {
		for _, v := range views {
			if slices.Contains(v.Rules, id) {
				return true
			}
			for _, e := range v.Excepts {
				if e.RuleID == id {
					return true
				}
			}
		}
		return false
	}

	t.Run("project_member_manager_sees_only_the_project_view", func(t *testing.T) {
		views := resolve(prjAdmin, dev, domain.CapRead, "")
		mustBeAbsent(views, erin, gina, custodian, alice, root, orgAdmin, mchNoCls)
		for _, p := range []domain.PrincipalID{prjAdmin, carol, dave, hank, ivan, mchWork} {
			mustFind(views, p)
		}
		if mentions(views, siblingRule) {
			t.Fatalf("a sibling-project rule is named: %+v", views)
		}
		if v := mustFind(views, carol); !slices.Equal(v.Rules, []string{carolRead}) || v.State != authz.AccessCan {
			t.Fatalf("carol = %+v, want can through only %s", v, carolRead)
		}
		// Instance holder: listed because it holds a project grant, and named
		// by the scope of what reaches.
		if v := mustFind(views, ivan); v.State != authz.AccessCan || len(v.Grants) != 2 {
			t.Fatalf("ivan = %+v, want can through the project and instance grants", v)
		}
	})

	t.Run("rules_are_named_from_the_project_only", func(t *testing.T) {
		views := resolve(prjAdmin, dev, domain.CapReveal, f.dbKeyID)
		if v := mustFind(views, carol); v.State != authz.AccessCan || !slices.Equal(v.Rules, []string{carolReveal}) {
			t.Fatalf("carol = %+v, want can through %s", v, carolReveal)
		}
		if v := mustFind(views, dave); v.State != authz.AccessNeedsSee && v.State != authz.AccessCan || !slices.Equal(v.Rules, []string{daveReveal}) {
			t.Fatalf("dave = %+v, want %s", v, daveReveal)
		}
		mustBeAbsent(views, erin, gina)
		if mentions(views, siblingRule) {
			t.Fatalf("a sibling-project rule is named: %+v", views)
		}
	})

	t.Run("org_manager_view_is_still_the_project_view", func(t *testing.T) {
		views := resolve(orgAdmin, dev, domain.CapReveal, f.dbKeyID)
		if v := mustFind(views, hank); v.State != authz.AccessNeedsSee || !slices.Equal(v.Rules, []string{hankReveal}) {
			t.Fatalf("hank = %+v, want needs-see through %s", v, hankReveal)
		}
		if v := mustFind(views, dave); !slices.Equal(v.Rules, []string{daveReveal}) {
			t.Fatalf("dave on the db key = %+v, want %s", v, daveReveal)
		}
		mustBeAbsent(views, erin, gina, root)
		if mentions(views, siblingRule) {
			t.Fatalf("a sibling-project rule is named: %+v", views)
		}
	})

	t.Run("machine_reveal_withdrawn_is_none", func(t *testing.T) {
		views := resolve(orgAdmin, dev, domain.CapReveal, "")
		if v := mustFind(views, mchWork); v.State != authz.AccessNone || v.Class != domain.ClassWorkload || len(v.Grants) != 0 {
			t.Fatalf("workload with the opt-in off = %+v, want none", v)
		}
		mustBeAbsent(views, mchNoCls)
	})

	t.Run("environment_excepts", func(t *testing.T) {
		// Read on every environment but dev and prod, and Edit on all but dev.
		allBut := func(envs ...domain.EnvID) domain.Where {
			return whereIn(domain.AxisAll, map[domain.ProjectID][]domain.EnvID{prjA1: envs}, domain.AxisAll)
		}
		both := f.create(t, orgAdmin, service.RuleSpec{Target: erin, Capability: domain.CapEdit, Org: orgA, Where: allBut(envA1, envProd)})
		v := mustFind(resolve(orgAdmin, dev, domain.CapEdit, ""), erin)
		if v.State != authz.AccessExcepted || len(v.Excepts) != 1 || v.Excepts[0].RuleID != both ||
			!slices.Equal(v.Excepts[0].Envs, []domain.EnvID{envA1}) {
			t.Fatalf("erin on dev = %+v, want excepted by the dev except of %s alone", v, both)
		}
		if v := mustFind(resolve(orgAdmin, scopeEnv(orgA, prjA1, envProd), domain.CapEdit, ""), erin); v.State != authz.AccessExcepted ||
			!slices.Equal(v.Excepts[0].Envs, []domain.EnvID{envProd}) {
			t.Fatalf("erin on prod = %+v, want excepted by the prod except", v)
		}
	})

	t.Run("manage_access_selector_path", func(t *testing.T) {
		rule := f.create(t, orgAdmin, service.RuleSpec{Target: hank, Capability: domain.CapManageMembers, Org: orgA,
			Where: whereIn(devOnly, devEnvs, domain.AxisAll)})
		if v := mustFind(resolve(orgAdmin, dev, domain.CapManageMembers, ""), hank); v.State != authz.AccessCan || !slices.Equal(v.Rules, []string{rule}) {
			t.Fatalf("hank on dev = %+v, want can through %s", v, rule)
		}
		if v := mustFind(resolve(orgAdmin, prod, domain.CapManageMembers, ""), hank); v.State != authz.AccessNone {
			t.Fatalf("hank on prod = %+v, want none", v)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		for _, bad := range []domain.Capability{"no-such-permission", domain.CapManageProjects} {
			if _, err := f.rules.Resolve(ctx, service.LocalPrincipal(orgAdmin), dev, bad, ""); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("permission %q = %v, want invalid", bad, err)
			}
		}
		_, missing := f.rules.Resolve(ctx, service.LocalPrincipal(orgAdmin), dev, domain.CapReveal, "key_does_not_exist")
		_, sibling := f.rules.Resolve(ctx, service.LocalPrincipal(orgAdmin), dev, domain.CapReveal, keyA2)
		assertUniformNotFound(t, missing, sibling)
	})

	t.Run("audited_as_a_membership_read", func(t *testing.T) {
		const q = `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.membership_read'`
		before := queryInt(t, db, q)
		views := resolve(prjAdmin, dev, domain.CapRead, "")
		if got := queryInt(t, db, q); got != before+1 {
			t.Fatalf("membership_read events = %d, want %d", got, before+1)
		}
		payload := queryString(t, db, `SELECT payload FROM audit_tenant_events WHERE type = 'grant.membership_read' ORDER BY seq DESC LIMIT 1`)
		if !strings.Contains(payload, `"row_count":`+strconv.Itoa(len(views))) {
			t.Fatalf("payload %s does not record row_count %d", payload, len(views))
		}
	})

	t.Run("a_rule_delegated_manager_is_refused", func(t *testing.T) {
		if _, err := f.rules.Resolve(ctx, service.LocalPrincipal(hank), dev, domain.CapRead, ""); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("hank (manage-members through a rule only) = %v, want not found", err)
		}
	})
}
