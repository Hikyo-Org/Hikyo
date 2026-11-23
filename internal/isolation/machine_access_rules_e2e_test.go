package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Member access rules for machine principals. A workload or automation rule
// is honoured at the chokepoint, holds to its class's allowlist and depth,
// and goes through the machine widening gate like a machine grant. The reach
// the widening and mint gates diff counts rule-held authority too.

func TestMachineAccessRules(t *testing.T) {
	forEngines(t, runMachineAccessRules)
}

func runMachineAccessRules(t *testing.T, db *store.DB) {
	identityFixtures(t, db)
	ctx := t.Context()
	identities := identitySvc(db)
	grants := grantSvcWithAuth(db)
	rules := &service.Rules{DB: db, Auth: authWithWindow(db)}
	execRaw(t, db, `UPDATE projects SET machine_reveal = TRUE WHERE id = 'prj_a1'`)
	dev := scopeEnv(orgA, prjA1, envA1)

	newAccount := func(t *testing.T, name string, class domain.PrincipalClass) (principal domain.PrincipalID, saID, credential string) {
		t.Helper()
		sa, err := identities.CreateServiceAccount(ctx, service.LocalPrincipal(identAdmin), prjScope(), name, class)
		if err != nil {
			t.Fatal(err)
		}
		cred, err := identities.MintCredential(ctx, service.LocalPrincipal(identAdmin), prjScope(), sa.ID, service.MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		return sa.Principal, sa.ID, cred.Value
	}
	readOn := func(p domain.PrincipalID, envMode domain.AxisMode, envs ...domain.EnvID) service.RuleSpec {
		w := domain.Where{Projects: []domain.ProjectID{prjA1}, EnvMode: envMode, KeyMode: domain.AxisAll}
		if len(envs) > 0 {
			w.Envs = map[domain.ProjectID][]domain.EnvID{prjA1: envs}
		}
		return service.RuleSpec{Target: p, Capability: domain.CapRead, Org: orgA, Where: w}
	}

	t.Run("the_chokepoint_honours_a_machine_rule", func(t *testing.T) {
		ci, _, credential := newAccount(t, "rule-reader", domain.ClassAutomation)
		machine := service.Bearer(credential)
		if _, err := valueSvc(t, db).Get(ctx, machine, dev, "SHARED_KEY", false); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("read before the rule = %v, want not found", err)
		}
		view, err := rules.Create(ctx, service.LocalPrincipal(identAdmin), readOn(ci, domain.AxisAll))
		if err != nil {
			t.Fatalf("automation read rule: %v", err)
		}
		if _, err := valueSvc(t, db).Get(ctx, machine, dev, "SHARED_KEY", false); err != nil {
			t.Fatalf("read through the rule: %v", err)
		}
		if err := rules.Revoke(ctx, service.LocalPrincipal(identAdmin), orgA, view.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := valueSvc(t, db).Get(ctx, machine, dev, "SHARED_KEY", false); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("read after revoke = %v, want not found", err)
		}
	})

	t.Run("a_workload_rule_lists_its_environments", func(t *testing.T) {
		wl, _, _ := newAccount(t, "listed-reader", domain.ClassWorkload)
		if _, err := rules.Create(ctx, service.LocalPrincipal(identAdmin), readOn(wl, domain.AxisAll)); !errors.Is(err, service.ErrMachineScope) {
			t.Fatalf("workload rule on every environment = %v, want ErrMachineScope", err)
		}
		if _, err := rules.Create(ctx, service.LocalPrincipal(identAdmin), readOn(wl, domain.AxisAll, envProd)); !errors.Is(err, service.ErrMachineScope) {
			t.Fatalf("workload rule on every environment except one = %v, want ErrMachineScope", err)
		}
		if _, err := rules.Create(ctx, service.LocalPrincipal(identAdmin), readOn(wl, domain.AxisOnly, envA1)); err != nil {
			t.Fatalf("workload rule on a listed environment: %v", err)
		}
	})

	t.Run("a_widening_rule_needs_disclosure_and_reauthentication", func(t *testing.T) {
		wl, saID, _ := newAccount(t, "rule-widened", domain.ClassWorkload)
		// reveal without read reaches no plaintext, so this grant widens nothing.
		if _, err := grants.Create(ctx, service.LocalPrincipal(identRevatr), service.GrantSpec{
			Target: wl, Capability: domain.CapReveal, Scope: envScope(envA1)}); err != nil {
			t.Fatalf("reveal grant without read: %v", err)
		}
		spec := readOn(wl, domain.AxisOnly, envA1)
		if _, err := rules.Create(ctx, service.LocalPrincipal(identAdmin), spec); !errors.Is(err, service.ErrDisclosureAuthority) {
			t.Fatalf("widening rule without reveal = %v, want ErrDisclosureAuthority", err)
		}
		if _, err := rules.Create(ctx, service.LocalPrincipal(identRevatr), spec); !errors.Is(err, service.ErrReauthRequired) {
			t.Fatalf("widening rule without a window = %v, want ErrReauthRequired", err)
		}
		if _, err := (&service.Rules{DB: db}).Create(ctx, service.LocalPrincipal(identRevatr), spec); err == nil {
			t.Fatal("a widening rule passed with no reauthentication seam wired")
		}
		widenedBefore := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'identity.grant_widened'`)
		view, err := rules.Create(ctx, service.Bearer(sessionWithWindows(t, db, identRevatr, envA1)), spec)
		if err != nil {
			t.Fatalf("widening rule under a live window: %v", err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'identity.grant_widened' AND object_type = 'rule' AND object_id = '`+view.ID+`'`); n != 1 {
			t.Fatalf("identity.grant_widened rows for the rule = %d, want 1", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'identity.grant_widened'`); n != widenedBefore+1 {
			t.Fatalf("identity.grant_widened rows = %d, want %d", n, widenedBefore+1)
		}

		// A card edit that only replaces the rule widens nothing, so it needs
		// neither disclosure nor reauthentication.
		if _, err := rules.Replace(ctx, service.LocalPrincipal(identAdmin), service.ReplaceRulesSpec{
			Org: orgA, Target: wl, Revoke: []string{view.ID}, Create: []service.RuleSpec{spec}}); err != nil {
			t.Fatalf("replace that widens nothing: %v", err)
		}
		// The mint gate counts the rule-held read: minting for this account
		// now discloses env_a1's plaintext.
		if _, err := identities.MintCredential(ctx, service.LocalPrincipal(identAdmin), prjScope(), saID, service.MintRequest{}); !errors.Is(err, service.ErrDisclosureAuthority) {
			t.Fatalf("mint over rule-held reach without reveal = %v, want ErrDisclosureAuthority", err)
		}
	})

	t.Run("a_grant_widening_counts_rule_held_read", func(t *testing.T) {
		wl, _, _ := newAccount(t, "rule-then-grant", domain.ClassWorkload)
		if _, err := rules.Create(ctx, service.LocalPrincipal(identAdmin), readOn(wl, domain.AxisOnly, envA1)); err != nil {
			t.Fatalf("read rule with no reveal held: %v", err)
		}
		// Over grants alone this reveal reaches nothing; with the rule it
		// reaches env_a1's plaintext and must pass the gate.
		if _, err := grants.Create(ctx, service.LocalPrincipal(identRevatr), service.GrantSpec{
			Target: wl, Capability: domain.CapReveal, Scope: envScope(envA1)}); !errors.Is(err, service.ErrReauthRequired) {
			t.Fatalf("reveal grant over a rule-held read = %v, want ErrReauthRequired", err)
		}
	})

	t.Run("an_all_except_rule_reaches_a_later_environment", func(t *testing.T) {
		ci, _, credential := newAccount(t, "all-except", domain.ClassAutomation)
		machine := service.Bearer(credential)
		if _, err := rules.Create(ctx, service.LocalPrincipal(identAdmin), readOn(ci, domain.AxisAll, envProd)); err != nil {
			t.Fatalf("all-except rule: %v", err)
		}
		execRaw(t, db, `INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) `+
			`VALUES ('env_later', 'org_a', 'prj_a1', 'later', '', `+ts+`, 9)`)
		if _, err := valueSvc(t, db).Get(ctx, machine, scopeEnv(orgA, prjA1, "env_later"), "SHARED_KEY", false); err != nil {
			t.Fatalf("read in an environment created after the rule: %v", err)
		}
		if _, err := valueSvc(t, db).Get(ctx, machine, scopeEnv(orgA, prjA1, envProd), "SHARED_KEY", false); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("read in the excepted environment = %v, want not found", err)
		}
	})

	t.Run("deleting_a_service_account_removes_its_rules", func(t *testing.T) {
		ci, saID, _ := newAccount(t, "rule-deleted", domain.ClassAutomation)
		if _, err := rules.Create(ctx, service.LocalPrincipal(identAdmin), readOn(ci, domain.AxisAll)); err != nil {
			t.Fatal(err)
		}
		count := `SELECT COUNT(*) FROM rules WHERE principal_id = '` + string(ci) + `'`
		if n := queryInt(t, db, count); n != 1 {
			t.Fatalf("rules before delete = %d, want 1", n)
		}
		if err := identities.DeleteServiceAccount(ctx, service.LocalPrincipal(identAdmin), prjScope(), saID); err != nil {
			t.Fatalf("delete an account that holds a rule: %v", err)
		}
		if n := queryInt(t, db, count); n != 0 {
			t.Fatalf("rules after delete = %d, want 0", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM rule_items WHERE rule_id NOT IN (SELECT id FROM rules)`); n != 0 {
			t.Fatalf("orphan rule items = %d, want 0", n)
		}
	})

	t.Run("a_machine_without_a_service_account_row_holds_no_rule", func(t *testing.T) {
		execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_norow', 'machine', 'automation', `+ts+`)`)
		if _, err := rules.Create(ctx, service.LocalPrincipal(identAdmin), readOn("mch_norow", domain.AxisAll)); !errors.Is(err, service.ErrMachineProject) {
			t.Fatalf("rule for a machine with no service-account row = %v, want ErrMachineProject", err)
		}
	})
}
