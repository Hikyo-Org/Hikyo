package isolation

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// Environment selectors bind by name (#1447, member-access-rules ADR D3): an
// environment renamed or created to a name changes what rules reach. A gain
// (renamed out of an except, renamed or created into an only) needs a
// confirmation naming exactly who gains, from a member manager, and is
// audited; a narrowing, or a new environment an `all` rule does not except,
// needs none.

const (
	envMgr   = domain.PrincipalID("usr_envmgr")   // the custodian's environment authority plus manage-members at org_a
	envOnly  = domain.PrincipalID("usr_envonly")  // See, only prod, over prj_a1 and prj_a2
	envExcpt = domain.PrincipalID("usr_envexcpt") // See, all except prod, over prj_a1 and prj_a2
	envPin   = domain.PrincipalID("usr_envpin")   // Pin, only pinned, over prj_a2
	envMM    = domain.PrincipalID("usr_envmm")    // manage-members, only pinned, over prj_a2
)

// reachesEnv reports whether p's rules, a person's or a machine's, as the chokepoint reads them, reach
// See in env.
func reachesEnv(t *testing.T, db *store.DB, p domain.PrincipalID, env domain.Scope) bool {
	t.Helper()
	resolver := authn.NewSQLite(db.SQLiteRead())
	if db.Engine() == store.EnginePostgres {
		resolver = authn.NewPG(db.PG())
	}
	rules, err := resolver.Rules(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := resolver.MachineRules(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	rules = append(rules, machine...)
	return slices.ContainsFunc(rules, func(r domain.Rule) bool { return r.Reaches(domain.CapRead, domain.LevelEnv, env, nil) })
}

func TestEnvironmentNamesWidenRules(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		for _, p := range []domain.PrincipalID{envMgr, envOnly, envExcpt} {
			execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+string(p)+"', 'human', "+ts+")")
		}
		for _, c := range []string{"definitions-edit", "manage-members", "read", "edit", "publish"} {
			execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_em_"+c+"', 'usr_envmgr', '"+c+"', 'org_a', NULL, NULL, "+ts+")")
		}
		rules := &service.Rules{DB: db}
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db), Auth: authWithWindow(db)}
		byName := func(p domain.PrincipalID, mode domain.AxisMode) {
			t.Helper()
			if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: p, Capability: domain.CapRead, Org: orgA,
				Where: domain.Where{Projects: []domain.ProjectID{prjA1, prjA2}, EnvMode: mode, EnvNames: []string{"prod"}, KeyMode: domain.AxisAll}}); err != nil {
				t.Fatalf("rule for %s: %v", p, err)
			}
		}
		confirmed := func(env domain.EnvID, from, to string) int64 {
			return queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.env_widening_confirmed' AND object_id = '"+string(env)+
				"' AND payload LIKE '%\"from_name\":\""+from+"\"%' AND payload LIKE '%\"to_name\":\""+to+"\"%'")
		}
		refusedFor := func(t *testing.T, err error, want ...domain.PrincipalID) *service.EnvWideningError {
			t.Helper()
			var widening *service.EnvWideningError
			if !errors.As(err, &widening) || !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("got %v, want an environment widening refusal", err)
			}
			if want != nil && !slices.Equal(widening.Principals, want) {
				t.Fatalf("widening names %v, want %v", widening.Principals, want)
			}
			return widening
		}
		prodA1 := scopeEnv(orgA, prjA1, envProd)
		byName(envOnly, domain.AxisOnly)

		t.Run("a_name_no_environment_could_carry_is_refused", func(t *testing.T) {
			for _, name := range []string{"prod ", "pro\u202ed"} {
				_, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: envExcpt, Capability: domain.CapRead, Org: orgA,
					Where: domain.Where{Projects: []domain.ProjectID{prjA1}, EnvMode: domain.AxisAll, EnvNames: []string{name}, KeyMode: domain.AxisAll}})
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("rule excepting %q: %v, want invalid", name, err)
				}
			}
		})

		t.Run("a_rule_without_names_keeps_hidden_exceptions", func(t *testing.T) {
			// A client before revision 22 sees an exception only as the
			// environments carrying it, so a name no environment of a project
			// carries is hidden from it there and must not be dropped silently.
			as := service.LocalPrincipal(orgAdmin)
			spec := func(mode domain.AxisMode, names []string, projects ...domain.ProjectID) service.RuleSpec {
				return service.RuleSpec{Target: envExcpt, Capability: domain.CapEdit, Org: orgA,
					Where: domain.Where{Projects: projects, EnvMode: mode, EnvNames: names, KeyMode: domain.AxisAll}}
			}
			refused := func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, service.ErrRuleUnmatchedNamesDropped) {
					t.Fatalf("got %v, want the hidden exception kept", err)
				}
			}
			replace := func(held string, add service.RuleSpec) ([]service.RuleView, error) {
				return rules.Replace(ctx, as, service.ReplaceRulesSpec{Org: orgA, Target: envExcpt, Revoke: []string{held}, Create: []service.RuleSpec{add}})
			}
			// "ghost" is nowhere; prod is only in the first project, so a rule
			// over the second hides it, and so does one over both once the
			// edit keeps only the second.
			for _, c := range []struct {
				name     string
				projects []domain.ProjectID
				resend   []domain.ProjectID
			}{{"ghost", []domain.ProjectID{prjA1}, []domain.ProjectID{prjA1}}, {"prod", []domain.ProjectID{prjA2}, []domain.ProjectID{prjA2}}, {"prod", []domain.ProjectID{prjA1, prjA2}, []domain.ProjectID{prjA2}}} {
				held, err := rules.Create(ctx, as, spec(domain.AxisAll, []string{c.name}, c.projects...))
				if err != nil {
					t.Fatal(err)
				}
				_, err = replace(held.ID, spec(domain.AxisAll, nil, c.resend...))
				refused(t, err)
				// Adding a permission beside it is a plain create: refused too.
				add := spec(domain.AxisAll, nil, c.resend...)
				add.Capability = domain.CapPublish
				_, err = rules.Create(ctx, as, add)
				refused(t, err)
				views, err := replace(held.ID, spec(domain.AxisAll, []string{}, c.resend...))
				if err != nil {
					t.Fatalf("dropping %s by name explicitly: %v", c.name, err)
				}
				if err := rules.Revoke(ctx, as, orgA, views[0].ID); err != nil {
					t.Fatal(err)
				}
			}
			// Dropping a name from an only list narrows: no refusal.
			held, err := rules.Create(ctx, as, spec(domain.AxisOnly, []string{"ghost"}, prjA1))
			if err != nil {
				t.Fatal(err)
			}
			old := spec(domain.AxisOnly, nil, prjA1)
			old.Where.Envs = map[domain.ProjectID][]domain.EnvID{prjA1: {envProd}}
			views, err := replace(held.ID, old)
			if err != nil {
				t.Fatalf("replacing an only list without names: %v", err)
			}
			if err := rules.Revoke(ctx, as, orgA, views[0].ID); err != nil {
				t.Fatal(err)
			}
		})

		t.Run("a_narrowing_rename_needs_no_confirmation", func(t *testing.T) {
			if !reachesEnv(t, db, envOnly, prodA1) {
				t.Fatal("the only-prod rule does not reach prod before the rename")
			}
			if _, err := envs.RenameConfirming(ctx, service.LocalPrincipal(envMgr), prodA1, "production", nil, nil); err != nil {
				t.Fatalf("narrowing rename: %v", err)
			}
			if reachesEnv(t, db, envOnly, prodA1) {
				t.Fatal("the only-prod rule still reaches the environment renamed away from prod")
			}
			if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.env_widening_confirmed'"); n != 0 {
				t.Fatalf("a narrowing rename recorded %d confirmations", n)
			}
		})

		t.Run("a_rename_into_an_only_list_needs_confirmation", func(t *testing.T) {
			_, err := envs.RenameConfirming(ctx, service.LocalPrincipal(envMgr), prodA1, "prod", nil, nil)
			_ = refusedFor(t, err, envOnly)
			if reachesEnv(t, db, envOnly, prodA1) {
				t.Fatal("a refused rename still widened the rule")
			}
			if _, err := envs.RenameConfirming(ctx, service.LocalPrincipal(envMgr), prodA1, "prod", nil, []domain.PrincipalID{envOnly}); err != nil {
				t.Fatalf("confirmed rename: %v", err)
			}
			if !reachesEnv(t, db, envOnly, prodA1) || confirmed(envProd, "production", "prod") != 1 {
				t.Fatal("the confirmed rename did not widen the rule with one audited confirmation")
			}
		})

		byName(envExcpt, domain.AxisAll)
		var prodA2 domain.Scope

		t.Run("a_created_environment_in_an_only_list_needs_confirmation", func(t *testing.T) {
			project := domain.Scope{Org: orgA, Project: prjA2}
			// The all-except-prod rule does not gain the new prod, so only the
			// only-prod rule's holder is named.
			_, err := envs.CreateConfirming(ctx, service.LocalPrincipal(envMgr), project, "prod", nil, nil)
			_ = refusedFor(t, err, envOnly)
			_, err = envs.CreateConfirming(ctx, service.LocalPrincipal(envMgr), project, "prod", nil, []domain.PrincipalID{envOnly, envExcpt})
			_ = refusedFor(t, err, envOnly)
			// A caller who may not manage members may not confirm, and learns
			// only how many gain.
			undisclosed := refusedFor(t, func() error {
				_, err := envs.CreateConfirming(ctx, service.LocalPrincipal(custodian), project, "prod", nil, []domain.PrincipalID{envOnly})
				return err
			}())
			if undisclosed.Count != 1 || len(undisclosed.Principals) != 0 || len(undisclosed.Gains) != 0 {
				t.Fatalf("refusal to a non-manager discloses %+v", undisclosed)
			}
			if n := queryInt(t, db, "SELECT COUNT(*) FROM environments WHERE project_id = 'prj_a2' AND name = 'prod'"); n != 0 {
				t.Fatalf("a refused create wrote %d environments", n)
			}
			env, err := envs.CreateConfirming(ctx, service.LocalPrincipal(envMgr), project, "prod", nil, []domain.PrincipalID{envOnly})
			if err != nil {
				t.Fatalf("confirmed create: %v", err)
			}
			prodA2 = scopeEnv(orgA, prjA2, domain.EnvID(env.ID))
			if !reachesEnv(t, db, envOnly, prodA2) || reachesEnv(t, db, envExcpt, prodA2) || confirmed(domain.EnvID(env.ID), "", "prod") != 1 {
				t.Fatal("the confirmed create did not give exactly the only-prod rule the new prod, audited")
			}
			// The except names every prod: the first project's stays excepted too.
			if reachesEnv(t, db, envExcpt, prodA1) {
				t.Fatal("the all-except-prod rule reaches the first project's prod")
			}
			// The steps below need the prod just created, so they run under it
			// and a focused -run of either still creates it first.
			t.Run("ids_without_names_must_be_every_environment_their_names_match", func(t *testing.T) {
				// A client before revision 22 sends ids alone. prod is in both
				// projects now, so naming only the first project's would widen.
				spec := service.RuleSpec{Target: envExcpt, Capability: domain.CapEdit, Org: orgA,
					Where: domain.Where{Projects: []domain.ProjectID{prjA1, prjA2}, EnvMode: domain.AxisOnly,
						Envs: map[domain.ProjectID][]domain.EnvID{prjA1: {envProd}}, KeyMode: domain.AxisAll}}
				if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), spec); !errors.Is(err, service.ErrRuleEnvItemsStale) {
					t.Fatalf("ids naming one prod of two: %v, want stale items", err)
				}
			})
			t.Run("a_rename_out_of_an_except_list_needs_confirmation", func(t *testing.T) {
				_, err := envs.RenameConfirming(ctx, service.LocalPrincipal(envMgr), prodA2, "staging", nil, nil)
				_ = refusedFor(t, err, envExcpt)
				if _, err := envs.RenameConfirming(ctx, service.LocalPrincipal(envMgr), prodA2, "staging", nil, []domain.PrincipalID{envExcpt}); err != nil {
					t.Fatalf("confirmed rename: %v", err)
				}
				if !reachesEnv(t, db, envExcpt, prodA2) || reachesEnv(t, db, envOnly, prodA2) || confirmed(prodA2.Env, "prod", "staging") != 1 {
					t.Fatal("renaming out of the except list did not move the reach, audited")
				}
			})
		})

		t.Run("a_rule_listed_above_the_item_bound_is_refused", func(t *testing.T) {
			// Two projects list 1 + n items each: 250 names list 502 items,
			// above MaxRuleItems.
			names := make([]string, 250)
			for i := range names {
				names[i] = fmt.Sprintf("n%03d", i)
			}
			spec := service.RuleSpec{Target: envExcpt, Capability: domain.CapEdit, Org: orgA,
				Where: domain.Where{Projects: []domain.ProjectID{prjA1, prjA2}, EnvMode: domain.AxisAll, EnvNames: names, KeyMode: domain.AxisAll}}
			if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), spec); !errors.Is(err, service.ErrRuleTooLarge) {
				t.Fatalf("502 listed items: %v, want too large", err)
			}
		})

		t.Run("a_created_environment_an_all_rule_does_not_except_needs_none", func(t *testing.T) {
			env, err := envs.CreateConfirming(ctx, service.LocalPrincipal(envMgr), domain.Scope{Org: orgA, Project: prjA2}, "qa", nil, nil)
			if err != nil {
				t.Fatalf("create qa: %v", err)
			}
			qa := scopeEnv(orgA, prjA2, domain.EnvID(env.ID))
			if !reachesEnv(t, db, envExcpt, qa) || reachesEnv(t, db, envOnly, qa) {
				t.Fatal("qa is not reached by exactly the all-except-prod rule")
			}
		})

		t.Run("every_environment_shaped_capability_is_counted", func(t *testing.T) {
			// Pin and manage-members (counted by its selector reach as Edit)
			// gain through an only list like See does.
			for _, held := range []struct {
				p domain.PrincipalID
				c domain.Capability
			}{{envPin, domain.CapPin}, {envMM, domain.CapManageMembers}} {
				execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+string(held.p)+"', 'human', "+ts+")")
				if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: held.p, Capability: held.c, Org: orgA,
					Where: domain.Where{Projects: []domain.ProjectID{prjA2}, EnvMode: domain.AxisOnly, EnvNames: []string{"pinned"}, KeyMode: domain.AxisAll}}); err != nil {
					t.Fatalf("%s rule: %v", held.c, err)
				}
			}
			project := domain.Scope{Org: orgA, Project: prjA2}
			_, err := envs.CreateConfirming(ctx, service.LocalPrincipal(envMgr), project, "pinned", nil, nil)
			_ = refusedFor(t, err, envMM, envPin)
			if _, err := envs.CreateConfirming(ctx, service.LocalPrincipal(envMgr), project, "pinned", nil, []domain.PrincipalID{envPin, envMM}); err != nil {
				t.Fatalf("confirmed create: %v", err)
			}
		})

		t.Run("a_cloned_environment_in_an_only_list_needs_confirmation", func(t *testing.T) {
			if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: envOnly, Capability: domain.CapRead, Org: orgA,
				Where: domain.Where{Projects: []domain.ProjectID{prjA1, prjA2}, EnvMode: domain.AxisOnly, EnvNames: []string{"uat"}, KeyMode: domain.AxisAll}}); err != nil {
				t.Fatalf("only-uat rule: %v", err)
			}
			project := domain.Scope{Org: orgA, Project: prjA1}
			_, _, err := envs.CloneConfirming(ctx, service.LocalPrincipal(envMgr), project, "uat", string(envProd), nil, nil)
			_ = refusedFor(t, err, envOnly)
			if n := queryInt(t, db, "SELECT COUNT(*) FROM environments WHERE project_id = 'prj_a1' AND name = 'uat'"); n != 0 {
				t.Fatalf("a refused clone wrote %d environments", n)
			}
			env, _, err := envs.CloneConfirming(ctx, service.LocalPrincipal(envMgr), project, "uat", string(envProd), nil, []domain.PrincipalID{envOnly})
			if err != nil {
				t.Fatalf("confirmed clone: %v", err)
			}
			if !reachesEnv(t, db, envOnly, scopeEnv(orgA, prjA1, domain.EnvID(env.ID))) || confirmed(domain.EnvID(env.ID), "", "uat") != 1 {
				t.Fatal("the confirmed clone did not give the only-uat rule the new environment, audited")
			}
		})
	})
}

// A machine that gains reach through an environment name passes the machine
// widening gate (#1473, #1480) like a grant or rule widening.
func TestEnvironmentNameWideningGatesMachines(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		ctx := t.Context()
		execRaw(t, db, `UPDATE projects SET machine_reveal = TRUE WHERE id = 'prj_a1'`)
		// Environment create republishes, so it also needs read, edit and publish.
		for _, c := range []string{"definitions-edit", "read", "edit", "publish"} {
			execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_idr_"+c+"', 'usr_idrev', '"+c+"', 'org_a', 'prj_a1', NULL, "+ts+")")
		}
		identities := identitySvc(db)
		sa, err := identities.CreateServiceAccount(ctx, service.LocalPrincipal(identAdmin), prjScope(), "named-reader", domain.ClassAutomation)
		if err != nil {
			t.Fatal(err)
		}
		// Reveal without read reaches no plaintext; the rule's read on
		// `staging` reaches nothing until an environment is named so. The
		// reveal grant is project-wide, so a new staging is plaintext-reachable.
		// An automation credential may hold it at project depth under the
		// opt-in; seeded directly, as no fixture principal holds reveal there.
		execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_named_reveal', '"+string(sa.Principal)+"', 'reveal', 'org_a', 'prj_a1', NULL, "+ts+")")
		if _, err := (&service.Rules{DB: db, Auth: authWithWindow(db)}).Create(ctx, service.LocalPrincipal(identAdmin), service.RuleSpec{
			Target: sa.Principal, Capability: domain.CapRead, Org: orgA,
			Where: domain.Where{Projects: []domain.ProjectID{prjA1}, EnvMode: domain.AxisOnly, EnvNames: []string{"staging"}, KeyMode: domain.AxisAll}}); err != nil {
			t.Fatalf("named rule: %v", err)
		}
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db), Auth: authWithWindow(db)}
		dev := envScope(envA1)
		confirm := []domain.PrincipalID{sa.Principal}
		// Creating or cloning an environment the machine's rules make
		// plaintext-reachable fails closed: the gate asks for a window over the
		// new environment's id, which no session can hold before the
		// environment exists. Rename is the way (disclosed in the ADR, D3).
		// The caller may disclose project-wide, so only reauthentication is left.
		execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_idr_reveal_prj', 'usr_idrev', 'reveal', 'org_a', 'prj_a1', NULL, "+ts+")")
		var windowed []domain.EnvID
		for _, id := range strings.Fields(queryStrings(t, db, "SELECT id || ' ' FROM environments WHERE project_id = 'prj_a1' ORDER BY id")) {
			windowed = append(windowed, domain.EnvID(id))
		}
		windows := service.Bearer(sessionWithWindows(t, db, identRevatr, windowed...))
		if _, err := envs.CreateConfirming(ctx, windows, prjScope(), "staging", nil, confirm); !errors.Is(err, service.ErrReauthRequired) {
			t.Fatalf("create widening a machine = %v, want ErrReauthRequired", err)
		}
		if _, _, err := envs.CloneConfirming(ctx, windows, prjScope(), "staging", string(envA1), nil, confirm); !errors.Is(err, service.ErrReauthRequired) {
			t.Fatalf("clone widening a machine = %v, want ErrReauthRequired", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM environments WHERE project_id = 'prj_a1' AND name = 'staging'"); n != 0 {
			t.Fatalf("a refused create or clone wrote %d environments", n)
		}
		if _, err := envs.RenameConfirming(ctx, service.LocalPrincipal(identRevatr), dev, "staging", nil, confirm); !errors.Is(err, service.ErrReauthRequired) {
			t.Fatalf("confirmed rename widening a machine without a window = %v, want ErrReauthRequired", err)
		}
		if reachesEnv(t, db, sa.Principal, dev) {
			t.Fatal("a refused rename widened the machine")
		}
		if _, err := envs.RenameConfirming(ctx, windows, dev, "staging", nil, confirm); err != nil {
			t.Fatalf("rename under a live window: %v", err)
		}
		if !reachesEnv(t, db, sa.Principal, dev) {
			t.Fatal("the machine does not reach the renamed environment")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'identity.grant_widened' AND object_id = '"+string(sa.Principal)+"'"); n != 1 {
			t.Fatalf("identity.grant_widened rows for the machine = %d, want 1", n)
		}
		// Deleting the environment a machine's `all` rule excepts gains it the
		// project: it must be confirmed like a person, but makes no remaining
		// environment newly plaintext-reachable, so needs no window.
		qa, err := envs.CreateConfirming(ctx, service.LocalPrincipal(identRevatr), prjScope(), "qa", nil, nil)
		if err != nil {
			t.Fatalf("create qa: %v", err)
		}
		// The rule reaches every other environment in plaintext, so its
		// creator must be able to disclose them.
		execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_ida_reveal_prj', '"+string(identAdmin)+"', 'reveal', 'org_a', 'prj_a1', NULL, "+ts+")")
		seedOrigins(t, db)
		adminWindows := service.Bearer(sessionWithWindows(t, db, identAdmin, windowed...))
		if _, err := (&service.Rules{DB: db, Auth: authWithWindow(db)}).Create(ctx, adminWindows, service.RuleSpec{
			Target: sa.Principal, Capability: domain.CapRead, Org: orgA,
			Where: domain.Where{Projects: []domain.ProjectID{prjA1}, EnvMode: domain.AxisAll, EnvNames: []string{"qa"}, KeyMode: domain.AxisAll}}); err != nil {
			t.Fatalf("all-except-qa rule: %v", err)
		}
		qaScope := envScope(domain.EnvID(qa.ID))
		var widening *service.EnvWideningError
		if err := envs.DeleteConfirming(ctx, service.LocalPrincipal(identRevatr), qaScope, nil); !errors.As(err, &widening) ||
			!slices.Equal(widening.Principals, confirm) {
			t.Fatalf("unconfirmed delete widening a machine = %v, want a refusal naming it", err)
		}
		if err := envs.DeleteConfirming(ctx, service.LocalPrincipal(identRevatr), qaScope, confirm); err != nil {
			t.Fatalf("confirmed delete widening a machine without a window: %v", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.env_widening_confirmed' AND object_id = '"+qa.ID+"'"); n != 1 {
			t.Fatalf("delete confirmations for the machine = %d, want 1", n)
		}
	})
}

// D7 with environment names (#1446, #1447): an all-projects rule also
// reaches projects created later, so its names must be held over the
// future-project witness too, by an org grant or another all-projects rule
// whose names contain them. Rules naming today's projects never do.
func TestAllProjectsRulesFutureProjectsHoldNames(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedAllProjectsFixture(t, db)
		ctx := t.Context()
		const mgr = domain.PrincipalID("usr_apnamed")
		execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('usr_apnamed', 'human', "+ts+")")
		p1, p2 := f.newProject(t, "one"), f.newProject(t, "two")
		named := func(where domain.Where, names ...string) domain.Where {
			where.EnvMode, where.EnvNames = domain.AxisOnly, names
			return where
		}
		give := func(c domain.Capability, where domain.Where) string {
			t.Helper()
			spec := service.RuleSpec{Target: mgr, Capability: c, Org: orgAP, Where: where}
			if where.AllProjects {
				spec = claimAll(t, db, spec)
			}
			view, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), spec)
			if err != nil {
				t.Fatalf("give %s: %v", c, err)
			}
			return view.ID
		}
		create := func(names ...string) error {
			_, err := f.rules.Create(ctx, service.LocalPrincipal(mgr), claimAll(t, db, service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP,
				Where: named(allProjectsWhere(domain.AxisAll), names...)}))
			return err
		}
		give(domain.CapManageMembers, allProjectsWhere(domain.AxisAll))
		// See on prod in every current project, by project-bound rule: the
		// current projects are held, the future-project witness is not.
		bound := give(domain.CapRead, named(domain.Where{Projects: []domain.ProjectID{p1, p2}, KeyMode: domain.AxisAll}, "prod"))
		if err := create("prod"); !errors.Is(err, service.ErrGrantorLacksCapability) {
			t.Fatalf("all-projects prod rule from project-bound prod = %v, want ErrGrantorLacksCapability", err)
		}
		// Positive control: the same rules hold prod in the current projects.
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(mgr), service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP,
			Where: named(domain.Where{Projects: []domain.ProjectID{p1, p2}, KeyMode: domain.AxisAll}, "prod")}); err != nil {
			t.Fatalf("project-bound prod rule from project-bound prod: %v", err)
		}
		if err := f.rules.Revoke(ctx, service.LocalPrincipal(apAdmin), orgAP, bound); err != nil {
			t.Fatal(err)
		}
		give(domain.CapRead, named(allProjectsWhere(domain.AxisAll), "prod"))
		if err := create("prod"); err != nil {
			t.Fatalf("all-projects prod rule from all-projects prod: %v", err)
		}
		if err := create("dev"); !errors.Is(err, service.ErrGrantorLacksCapability) {
			t.Fatalf("all-projects dev rule from all-projects prod = %v, want ErrGrantorLacksCapability", err)
		}
	})
}

// The v1 selector bound counts names (#1447): an all-projects rule lists
// projects * (1 + folders + names) items.
func TestAllProjectsRulesBoundCountsNames(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedAllProjectsFixture(t, db)
		ctx := t.Context()
		spec := func(names ...string) service.RuleSpec {
			w := allProjectsWhere(domain.AxisAll)
			w.EnvMode, w.EnvNames = domain.AxisOnly, names
			return claimAll(t, db, service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP, Where: w})
		}
		// One name: 2 items per project, so 250 projects list 500 and 251 list 502.
		projects := domain.MaxRuleItems / 2
		seedBoundProjects(t, db, projects-1)
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), spec("prod")); err != nil {
			t.Fatalf("one-name rule over %d projects: %v", projects-1, err)
		}
		f.newProject(t, "at-the-bound")
		if _, err := f.projects.Create(ctx, service.LocalPrincipal(apAdmin), orgAP, "past-the-bound"); !errors.Is(err, service.ErrProjectAboveAllProjectsRules) {
			t.Fatalf("project %d under a one-name rule = %v, want ErrProjectAboveAllProjectsRules", projects+1, err)
		}
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), spec("prod", "dev")); !errors.Is(err, service.ErrRuleAllProjectsTooLarge) {
			t.Fatalf("two-name rule over %d projects = %v, want ErrRuleAllProjectsTooLarge", projects, err)
		}
	})
}

// An old client's create of one environment id per project is bounded by the
// names those ids carry, not one per id: 30 projects' prod list 60 items.
func TestRuleCreateBoundCountsDerivedNames(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedAllProjectsFixture(t, db)
		have := int(queryInt(t, db, "SELECT COUNT(*) FROM projects WHERE org_id = 'org_ap'"))
		seedBoundProjects(t, db, have+30)
		w := domain.Where{EnvMode: domain.AxisOnly, Envs: map[domain.ProjectID][]domain.EnvID{}, KeyMode: domain.AxisAll}
		var rows []string
		for i := range 30 {
			project := domain.ProjectID(fmt.Sprintf("prj_bound_%04d", have+i))
			env := domain.EnvID("env_bound_" + string(project))
			w.Projects = append(w.Projects, project)
			w.Envs[project] = []domain.EnvID{env}
			rows = append(rows, fmt.Sprintf("('%s', 'org_ap', '%s', 'prod', '', %s, 0)", env, project, ts))
		}
		execRaw(t, db, "INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES "+strings.Join(rows, ", "))
		view, err := f.rules.Create(t.Context(), service.LocalPrincipal(apAdmin), service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP, Where: w})
		if err != nil {
			t.Fatalf("ids naming prod in 30 projects: %v", err)
		}
		if !slices.Equal(view.Where.EnvNames, []string{"prod"}) {
			t.Fatalf("stored names %v, want [prod]", view.Where.EnvNames)
		}
	})
}

// A definitions apply renaming or creating environments widens rules like the
// single operations, confirmed by the apply's ConfirmWidening (#1447).
func TestDefinitionsApplyEnvironmentNameWidening(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		const reader = domain.PrincipalID("usr_envplan")
		execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('usr_envplan', 'human', "+ts+")")
		execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_alice_mm', 'usr_alice', 'manage-members', 'org_a', NULL, NULL, "+ts+")")
		f := seedDefinitionsProject(t, db, "envnames", true)
		if _, err := (&service.Rules{DB: db}).Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: reader, Capability: domain.CapRead, Org: orgA,
			Where: domain.Where{Projects: []domain.ProjectID{f.project}, EnvMode: domain.AxisOnly, EnvNames: []string{"uat"}, KeyMode: domain.AxisAll}}); err != nil {
			t.Fatalf("only-uat rule: %v", err)
		}
		svc := definitionsService(t, db)
		bundle := parseDefinitions(t, exportDefinitions(t, svc, f))
		bundle.Environments[0].Name = "uat"
		plan := planDefinitions(t, svc, f, encodeDefinitions(t, bundle))
		var widening *service.EnvWideningError
		if _, err := svc.Apply(ctx, service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{}); !errors.As(err, &widening) || !slices.Equal(widening.Principals, []domain.PrincipalID{reader}) {
			t.Fatalf("unconfirmed widening apply = %v, want a refusal naming %s", err, reader)
		}
		if reachesEnv(t, db, reader, f.envScope()) {
			t.Fatal("a refused apply widened the rule")
		}
		if _, err := svc.Apply(ctx, service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{ConfirmWidening: []domain.PrincipalID{reader}}); err != nil {
			t.Fatalf("confirmed apply: %v", err)
		}
		if !reachesEnv(t, db, reader, f.envScope()) {
			t.Fatal("the confirmed apply did not give the only-uat rule the renamed environment")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.env_widening_confirmed' AND object_id = '"+string(f.env)+"'"); n != 1 {
			t.Fatalf("confirmations recorded = %d, want 1", n)
		}
	})
}

// reachesProject reports whether p's rules, as the chokepoint reads them,
// reach See at project level: every environment of it.
func reachesProject(t *testing.T, db *store.DB, p domain.PrincipalID, project domain.Scope) bool {
	t.Helper()
	resolver := authn.NewSQLite(db.SQLiteRead())
	if db.Engine() == store.EnginePostgres {
		resolver = authn.NewPG(db.PG())
	}
	rules, err := resolver.Rules(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	return slices.ContainsFunc(rules, func(r domain.Rule) bool { return r.Reaches(domain.CapRead, domain.LevelProject, project, nil) })
}

// Deleting the environment an `all, except` rule names fails closed (#1447,
// ADR owner decision 2026-11-27): the rule would reach the rest of the
// project, so the delete is refused naming who gains until a member manager
// confirms exactly them. The rule keeps the name, so a later environment of
// that name is excepted again without a confirmation.
func TestEnvironmentDeleteWideningExceptRules(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		for _, p := range []domain.PrincipalID{envMgr, envExcpt} {
			execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+string(p)+"', 'human', "+ts+")")
		}
		for _, c := range []string{"definitions-edit", "manage-members", "read", "edit", "publish"} {
			execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_em_"+c+"', 'usr_envmgr', '"+c+"', 'org_a', NULL, NULL, "+ts+")")
		}
		rules := &service.Rules{DB: db}
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db), Auth: authWithWindow(db)}
		if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: envExcpt, Capability: domain.CapRead, Org: orgA,
			Where: domain.Where{Projects: []domain.ProjectID{prjA1}, EnvMode: domain.AxisAll, EnvNames: []string{"prod"}, KeyMode: domain.AxisAll}}); err != nil {
			t.Fatalf("all-except-prod rule: %v", err)
		}
		project := domain.Scope{Org: orgA, Project: prjA1}
		prod := scopeEnv(orgA, prjA1, envProd)
		if reachesProject(t, db, envExcpt, project) || reachesEnv(t, db, envExcpt, prod) {
			t.Fatal("the all-except-prod rule reaches past its exception before the delete")
		}
		var widening *service.EnvWideningError
		err := envs.DeleteConfirming(ctx, service.LocalPrincipal(envMgr), prod, nil)
		if !errors.As(err, &widening) || !errors.Is(err, domain.ErrConflict) || !widening.Deleting ||
			!slices.Equal(widening.Principals, []domain.PrincipalID{envExcpt}) || !strings.Contains(widening.SafeDetail(), `deleting the environment "prod"`) {
			t.Fatalf("unconfirmed delete = %v, want a delete widening refusal naming %s", err, envExcpt)
		}
		// A caller who may not manage members may not confirm, and learns
		// only how many gain.
		err = envs.DeleteConfirming(ctx, service.LocalPrincipal(custodian), prod, []domain.PrincipalID{envExcpt})
		if !errors.As(err, &widening) || widening.Count != 1 || len(widening.Principals) != 0 || len(widening.Gains) != 0 {
			t.Fatalf("non-manager confirmation = %v (%+v), want a count-only refusal", err, widening)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM environments WHERE id = '"+string(envProd)+"'"); n != 1 || reachesProject(t, db, envExcpt, project) {
			t.Fatal("a refused delete removed the environment or widened the rule")
		}
		if err := envs.DeleteConfirming(ctx, service.LocalPrincipal(envMgr), prod, []domain.PrincipalID{envExcpt}); err != nil {
			t.Fatalf("confirmed delete: %v", err)
		}
		if !reachesProject(t, db, envExcpt, project) {
			t.Fatal("the confirmed delete did not widen the rule to the project")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.env_widening_confirmed' AND object_id = '"+string(envProd)+
			"' AND payload LIKE '%\"from_name\":\"prod\"%' AND payload LIKE '%\"to_name\":\"\"%'"); n != 1 {
			t.Fatalf("delete confirmations recorded = %d, want 1", n)
		}
		// The rule kept the name: a new prod is excepted again, a narrowing
		// that needs no confirmation.
		env, err := envs.CreateConfirming(ctx, service.LocalPrincipal(envMgr), project, "prod", nil, nil)
		if err != nil {
			t.Fatalf("re-creating prod: %v", err)
		}
		if reachesProject(t, db, envExcpt, project) || reachesEnv(t, db, envExcpt, scopeEnv(orgA, prjA1, domain.EnvID(env.ID))) {
			t.Fatal("the re-created prod is not excepted again")
		}
		// Deleting an environment no rule excepts needs none.
		if err := envs.Delete(ctx, service.LocalPrincipal(envMgr), scopeEnv(orgA, prjA1, "env_a1")); err != nil {
			t.Fatalf("deleting dev: %v", err)
		}
	})
}

// A definitions apply deleting the environment an `all, except` rule names
// fails closed like the single delete (#1447).
func TestDefinitionsApplyEnvironmentDeleteWidening(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		const reader = domain.PrincipalID("usr_envplan")
		execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('usr_envplan', 'human', "+ts+")")
		execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_alice_mm', 'usr_alice', 'manage-members', 'org_a', NULL, NULL, "+ts+")")
		f := seedDefinitionsProject(t, db, "envdel", true)
		execRaw(t, db, fmt.Sprintf("INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('env_def_envdel_prod', 'org_a', '%s', 'prod', '', %s, 1)", f.project, ts))
		if _, err := (&service.Rules{DB: db}).Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: reader, Capability: domain.CapRead, Org: orgA,
			Where: domain.Where{Projects: []domain.ProjectID{f.project}, EnvMode: domain.AxisAll, EnvNames: []string{"prod"}, KeyMode: domain.AxisAll}}); err != nil {
			t.Fatalf("all-except-prod rule: %v", err)
		}
		svc := definitionsService(t, db)
		bundle := parseDefinitions(t, exportDefinitions(t, svc, f))
		bundle.Environments = slices.DeleteFunc(bundle.Environments, func(e definitions.Environment) bool { return e.Name == "prod" })
		// A harmless rename beside it: the refusal names the delete.
		bundle.Environments[0].Name = "qa"
		plan := planDefinitions(t, svc, f, encodeDefinitions(t, bundle))
		project := domain.Scope{Org: orgA, Project: f.project}
		var widening *service.EnvWideningError
		if _, err := svc.Apply(ctx, service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true}); !errors.As(err, &widening) ||
			!widening.Deleting || !slices.Equal(widening.Principals, []domain.PrincipalID{reader}) {
			t.Fatalf("unconfirmed deleting apply = %v, want a delete refusal naming %s", err, reader)
		}
		if reachesProject(t, db, reader, project) {
			t.Fatal("a refused apply widened the rule")
		}
		if _, err := svc.Apply(ctx, service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true, ConfirmWidening: []domain.PrincipalID{reader}}); err != nil {
			t.Fatalf("confirmed apply: %v", err)
		}
		if !reachesProject(t, db, reader, project) {
			t.Fatal("the confirmed apply did not widen the rule to the project")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.env_widening_confirmed' AND object_id = 'env_def_envdel_prod'"); n != 1 {
			t.Fatalf("confirmations recorded = %d, want 1", n)
		}
		// The harmless rename caused nothing and records no confirmation.
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.env_widening_confirmed'"); n != 1 {
			t.Fatalf("confirmations recorded across the plan = %d, want only the delete's", n)
		}
	})
}

// A definitions plan whose changes widen only together (#1447): deleting
// prod or staging alone leaves the other excepted, so the refusal names the
// plan rather than a change, and the confirmation is recorded once, on a
// delete, not on the harmless rename beside them.
func TestDefinitionsApplyEnvironmentChangesWidenTogether(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		const reader = domain.PrincipalID("usr_envplan")
		execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('usr_envplan', 'human', "+ts+")")
		execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_alice_mm', 'usr_alice', 'manage-members', 'org_a', NULL, NULL, "+ts+")")
		f := seedDefinitionsProject(t, db, "envboth", true)
		for i, name := range []string{"prod", "staging"} {
			execRaw(t, db, fmt.Sprintf("INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('env_def_envboth_%s', 'org_a', '%s', '%s', '', %s, %d)", name, f.project, name, ts, i+1))
		}
		if _, err := (&service.Rules{DB: db}).Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: reader, Capability: domain.CapRead, Org: orgA,
			Where: domain.Where{Projects: []domain.ProjectID{f.project}, EnvMode: domain.AxisAll, EnvNames: []string{"prod", "staging"}, KeyMode: domain.AxisAll}}); err != nil {
			t.Fatalf("all-except-prod-and-staging rule: %v", err)
		}
		svc := definitionsService(t, db)
		bundle := parseDefinitions(t, exportDefinitions(t, svc, f))
		bundle.Environments = slices.DeleteFunc(bundle.Environments, func(e definitions.Environment) bool { return e.Name == "prod" || e.Name == "staging" })
		bundle.Environments[0].Name = "qa"
		plan := planDefinitions(t, svc, f, encodeDefinitions(t, bundle))
		var widening *service.EnvWideningError
		if _, err := svc.Apply(ctx, service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true}); !errors.As(err, &widening) ||
			!widening.Plan || widening.EnvName != "" || !slices.Equal(widening.Principals, []domain.PrincipalID{reader}) ||
			!strings.HasPrefix(widening.SafeDetail(), "this plan's changes widen access") {
			t.Fatalf("unconfirmed apply = %v, want a plan-level refusal naming %s", err, reader)
		}
		if _, err := svc.Apply(ctx, service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true, ConfirmWidening: []domain.PrincipalID{reader}}); err != nil {
			t.Fatalf("confirmed apply: %v", err)
		}
		if !reachesProject(t, db, reader, domain.Scope{Org: orgA, Project: f.project}) {
			t.Fatal("the confirmed apply did not widen the rule to the project")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.env_widening_confirmed'"); n != 1 {
			t.Fatalf("confirmations recorded = %d, want one for the plan", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.env_widening_confirmed' AND object_id IN ('env_def_envboth_prod', 'env_def_envboth_staging')"); n != 1 {
			t.Fatal("the plan's confirmation is not recorded on a delete")
		}
	})
}

// The delete census across rule shapes (#1447): an exception still bound by
// another name keeps the rule from the project, the confirmation must be the
// exact set, an all-projects rule widens like a named-project one, and a
// definitions apply that deletes an excepted environment while renaming
// another to its name narrows.
func TestEnvironmentDeleteWideningShapes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		for _, p := range []domain.PrincipalID{envMgr, envExcpt, envOnly} {
			execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+string(p)+"', 'human', "+ts+")")
		}
		for _, c := range []string{"definitions-edit", "manage-members", "read", "edit", "publish"} {
			execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_em_"+c+"', 'usr_envmgr', '"+c+"', 'org_a', NULL, NULL, "+ts+")")
		}
		rules := &service.Rules{DB: db}
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db), Auth: authWithWindow(db)}
		as := service.LocalPrincipal(envMgr)
		except := func(p domain.PrincipalID, names ...string) {
			t.Helper()
			if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: p, Capability: domain.CapRead, Org: orgA,
				Where: domain.Where{Projects: []domain.ProjectID{prjA1}, EnvMode: domain.AxisAll, EnvNames: names, KeyMode: domain.AxisAll}}); err != nil {
				t.Fatalf("rule for %s: %v", p, err)
			}
		}
		refused := func(t *testing.T, err error, want ...domain.PrincipalID) {
			t.Helper()
			var widening *service.EnvWideningError
			if !errors.As(err, &widening) || !widening.Deleting || !slices.Equal(widening.Principals, want) {
				t.Fatalf("got %v, want a delete refusal naming %v", err, want)
			}
		}

		t.Run("exceptions_left_and_the_exact_set", func(t *testing.T) {
			except(envExcpt, "prod", "dev")
			except(envOnly, "dev")
			// dev still carries an exception of envExcpt's rule: no gain.
			if err := envs.Delete(ctx, as, scopeEnv(orgA, prjA1, envProd)); err != nil {
				t.Fatalf("deleting prod while dev is excepted too: %v", err)
			}
			dev := scopeEnv(orgA, prjA1, "env_a1")
			refused(t, envs.Delete(ctx, as, dev), envExcpt, envOnly)
			for _, wrong := range [][]domain.PrincipalID{{envExcpt}, {envExcpt, envOnly, envMgr}} {
				refused(t, envs.DeleteConfirming(ctx, as, dev, wrong), envExcpt, envOnly)
			}
			if err := envs.DeleteConfirming(ctx, as, dev, []domain.PrincipalID{envOnly, envExcpt, envOnly}); err != nil {
				t.Fatalf("exact confirmation, unordered and repeated: %v", err)
			}
			if !reachesProject(t, db, envExcpt, domain.Scope{Org: orgA, Project: prjA1}) {
				t.Fatal("the confirmed delete did not widen the rule")
			}
		})

		t.Run("an_all_projects_rule", func(t *testing.T) {
			f := seedAllProjectsFixture(t, db)
			for _, c := range []string{"definitions-edit", "manage-members", "read", "edit", "publish"} {
				execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_apm_"+c+"', 'usr_envmgr', '"+c+"', 'org_ap', NULL, NULL, "+ts+")")
			}
			seedOrigins(t, db)
			p1 := f.newProject(t, "one")
			project := domain.Scope{Org: orgAP, Project: p1}
			prod, err := envs.CreateConfirming(ctx, as, project, "prod", nil, nil)
			if err != nil {
				t.Fatalf("create prod: %v", err)
			}
			if _, err := envs.CreateConfirming(ctx, as, project, "dev", nil, nil); err != nil {
				t.Fatalf("create dev: %v", err)
			}
			w := allProjectsWhere(domain.AxisAll)
			w.EnvNames = []string{"prod"}
			if _, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), claimAll(t, db, service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP, Where: w})); err != nil {
				t.Fatalf("all-projects all-except-prod rule: %v", err)
			}
			prodScope := domain.Scope{Org: orgAP, Project: p1, Env: domain.EnvID(prod.ID)}
			refused(t, envs.Delete(ctx, as, prodScope), apHolder)
			if err := envs.DeleteConfirming(ctx, as, prodScope, []domain.PrincipalID{apHolder}); err != nil {
				t.Fatalf("confirmed delete: %v", err)
			}
			if !reachesProject(t, db, apHolder, project) {
				t.Fatal("the confirmed delete did not widen the all-projects rule")
			}
		})
	})
}

// A definitions apply deleting the excepted environment while renaming
// another to its name only narrows: no confirmation (#1447).
func TestDefinitionsApplyEnvironmentDeleteAndRenameNarrows(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		const reader = domain.PrincipalID("usr_envplan")
		execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('usr_envplan', 'human', "+ts+")")
		f := seedDefinitionsProject(t, db, "envswap", true)
		execRaw(t, db, fmt.Sprintf("INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('env_def_envswap_prod', 'org_a', '%s', 'prod', '', %s, 1)", f.project, ts))
		if _, err := (&service.Rules{DB: db}).Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: reader, Capability: domain.CapRead, Org: orgA,
			Where: domain.Where{Projects: []domain.ProjectID{f.project}, EnvMode: domain.AxisAll, EnvNames: []string{"prod"}, KeyMode: domain.AxisAll}}); err != nil {
			t.Fatalf("all-except-prod rule: %v", err)
		}
		svc := definitionsService(t, db)
		bundle := parseDefinitions(t, exportDefinitions(t, svc, f))
		bundle.Environments = slices.DeleteFunc(bundle.Environments, func(e definitions.Environment) bool { return e.Name == "prod" })
		bundle.Environments[0].Name = "prod"
		plan := planDefinitions(t, svc, f, encodeDefinitions(t, bundle))
		if _, err := svc.Apply(ctx, service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true}); err != nil {
			t.Fatalf("narrowing apply: %v", err)
		}
		if reachesProject(t, db, reader, domain.Scope{Org: orgA, Project: f.project}) || reachesEnv(t, db, reader, f.envScope()) {
			t.Fatal("the renamed prod is not excepted")
		}
	})
}

// A rename that gives a principal other keys of an environment it already
// reaches through another rule widens too (#1447): the census keeps each
// key-narrowed rule's key selector rather than counting the environment once.
func TestEnvironmentRenameWideningKeepsKeySelectors(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		for _, p := range []domain.PrincipalID{envMgr, envExcpt} {
			execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+string(p)+"', 'human', "+ts+")")
		}
		for _, c := range []string{"definitions-edit", "manage-members", "read", "edit", "publish"} {
			execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_em_"+c+"', 'usr_envmgr', '"+c+"', 'org_a', NULL, NULL, "+ts+")")
		}
		rules := &service.Rules{DB: db}
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db), Auth: authWithWindow(db)}
		edit := func(mode domain.AxisMode, names []string, folder string) {
			t.Helper()
			w := domain.Where{Projects: []domain.ProjectID{prjA1}, EnvMode: mode, EnvNames: names, KeyMode: domain.AxisOnly,
				Keys: map[domain.ProjectID][]domain.RuleKeyItem{prjA1: {folderItem(folder)}}}
			if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: envExcpt, Capability: domain.CapEdit, Org: orgA, Where: w}); err != nil {
				t.Fatalf("edit rule over %s: %v", folder, err)
			}
		}
		edit(domain.AxisAll, nil, "shared")
		edit(domain.AxisOnly, []string{"qa"}, "restricted")
		dev := scopeEnv(orgA, prjA1, "env_a1")
		var widening *service.EnvWideningError
		_, err := envs.RenameConfirming(ctx, service.LocalPrincipal(envMgr), dev, "qa", nil, nil)
		if !errors.As(err, &widening) || !slices.Equal(widening.Principals, []domain.PrincipalID{envExcpt}) {
			t.Fatalf("renaming dev into the restricted rule's qa = %v, want a refusal naming %s", err, envExcpt)
		}
		if _, err := envs.RenameConfirming(ctx, service.LocalPrincipal(envMgr), dev, "qa", nil, []domain.PrincipalID{envExcpt}); err != nil {
			t.Fatalf("confirmed rename: %v", err)
		}
		// The same selector held already in every environment gains nothing.
		edit(domain.AxisAll, nil, "restricted")
		if _, err := envs.RenameConfirming(ctx, service.LocalPrincipal(envMgr), dev, "dev", nil, nil); err != nil {
			t.Fatalf("renaming back: %v", err)
		}
		if _, err := envs.RenameConfirming(ctx, service.LocalPrincipal(envMgr), dev, "qa", nil, nil); err != nil {
			t.Fatalf("renaming into qa with the restricted keys held everywhere: %v, want no confirmation", err)
		}
	})
}

// Changes of one definitions plan that widen for different principals each
// record only their own gainers, and the refusal names the plan rather than
// one change; so does a plan whose key move and environment rename both widen
// (#1447).
func TestDefinitionsApplyIndependentWidenings(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		const excepting, onlyQA, mover = domain.PrincipalID("usr_envplan"), domain.PrincipalID("usr_envqa"), domain.PrincipalID("usr_envmove")
		for _, p := range []domain.PrincipalID{excepting, onlyQA, mover} {
			execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+string(p)+"', 'human', "+ts+")")
		}
		execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_alice_mm', 'usr_alice', 'manage-members', 'org_a', NULL, NULL, "+ts+")")
		rules := &service.Rules{DB: db}
		svc := definitionsService(t, db)
		read := func(target domain.PrincipalID, project domain.ProjectID, mode domain.AxisMode, names ...string) {
			t.Helper()
			if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: target, Capability: domain.CapRead, Org: orgA,
				Where: domain.Where{Projects: []domain.ProjectID{project}, EnvMode: mode, EnvNames: names, KeyMode: domain.AxisAll}}); err != nil {
				t.Fatalf("rule for %s: %v", target, err)
			}
		}
		renamed := func(f definitionsFixture, moveKey bool) service.PlanView {
			t.Helper()
			bundle := parseDefinitions(t, exportDefinitions(t, svc, f))
			for i := range bundle.Environments {
				bundle.Environments[i].Name = map[string]string{"prod": "production", "dev": "qa"}[bundle.Environments[i].Name]
			}
			if moveKey {
				for i := range bundle.Keys {
					bundle.Keys[i].FolderPath = "moved"
				}
			}
			return planDefinitions(t, svc, f, encodeDefinitions(t, bundle))
		}
		planRefused := func(t *testing.T, f definitionsFixture, plan service.PlanView, want ...domain.PrincipalID) {
			t.Helper()
			var widening *service.EnvWideningError
			if _, err := svc.Apply(ctx, service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true}); !errors.As(err, &widening) ||
				!widening.Plan || !slices.Equal(widening.Principals, want) {
				t.Fatalf("unconfirmed apply = %v, want a plan-level refusal naming %v", err, want)
			}
			if _, err := svc.Apply(ctx, service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true, ConfirmWidening: want}); err != nil {
				t.Fatalf("confirmed apply: %v", err)
			}
		}

		t.Run("two_renames", func(t *testing.T) {
			f := seedDefinitionsProject(t, db, "envtwo", true)
			execRaw(t, db, fmt.Sprintf("INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('env_def_envtwo_prod', 'org_a', '%s', 'prod', '', %s, 1)", f.project, ts))
			read(excepting, f.project, domain.AxisAll, "prod")
			read(onlyQA, f.project, domain.AxisOnly, "qa")
			planRefused(t, f, renamed(f, false), excepting, onlyQA)
			for env, own := range map[string][2]domain.PrincipalID{"env_def_envtwo_prod": {excepting, onlyQA}, string(f.env): {onlyQA, excepting}} {
				q := "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.env_widening_confirmed' AND object_id = '" + env + "'"
				if n := queryInt(t, db, q+" AND payload LIKE '%"+string(own[0])+"%' AND payload NOT LIKE '%"+string(own[1])+"%'"); n != 1 {
					t.Fatalf("confirmation on %s recording only %s = %d, want 1", env, own[0], n)
				}
			}
		})

		t.Run("a_move_beside_a_rename", func(t *testing.T) {
			f := seedDefinitionsProject(t, db, "envmove", true)
			execRaw(t, db, fmt.Sprintf("INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('env_def_envmove_prod', 'org_a', '%s', 'prod', '', %s, 1)", f.project, ts))
			read(excepting, f.project, domain.AxisAll, "prod")
			if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{Target: mover, Capability: domain.CapEdit, Org: orgA,
				Where: domain.Where{Projects: []domain.ProjectID{f.project}, EnvMode: domain.AxisAll, KeyMode: domain.AxisOnly,
					Keys: map[domain.ProjectID][]domain.RuleKeyItem{f.project: {folderItem("moved")}}}}); err != nil {
				t.Fatalf("moved-folder rule: %v", err)
			}
			planRefused(t, f, renamed(f, true), mover, excepting)
		})
	})
}
