package isolation

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// All-projects member access rules (#1446; member-access-rules ADR, owner
// decisions A, S1 and R2 of 2026-11-25). Each refusal has a positive control.

const (
	orgAP      = domain.OrgID("org_ap")
	apAdmin    = domain.PrincipalID("usr_apadmin")    // manage-members + manage-projects at org_ap
	apPrjAdmin = domain.PrincipalID("usr_apprjadmin") // manage-members + read in each project, never org
	apHolder   = domain.PrincipalID("usr_apholder")
	apMachine  = domain.PrincipalID("mch_ap")
	apInstance = domain.PrincipalID("usr_apinstance") // manage-members at instance scope
	apPrjOnly  = domain.PrincipalID("usr_apprjonly")  // manage-projects at org_ap, no member management
	apRuleMgr  = domain.PrincipalID("usr_aprulemgr")  // manage-members and read through project-bound rules only
)

type allProjectsFixture struct {
	rules    *service.Rules
	projects *service.Projects
}

func seedAllProjectsFixture(t *testing.T, db *store.DB) allProjectsFixture {
	t.Helper()
	execRaw(t, db, `INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ('org_ap', 'org-ap', TRUE, '{}', `+ts+`)`)
	for _, p := range []domain.PrincipalID{apAdmin, apPrjAdmin, apHolder, apInstance, apPrjOnly, apRuleMgr} {
		execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+string(p)+"', 'human', "+ts+")")
	}
	execRaw(t, db, "INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_ap', 'machine', 'automation', "+ts+")")
	for _, cap := range []string{"manage-members", "manage-projects"} {
		execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_ap_"+cap+"', 'usr_apadmin', '"+cap+"', 'org_ap', NULL, NULL, "+ts+")")
	}
	execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_ap_instance', 'usr_apinstance', 'manage-members', NULL, NULL, NULL, "+ts+")")
	execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_ap_prjonly', 'usr_apprjonly', 'manage-projects', 'org_ap', NULL, NULL, "+ts+")")
	seedOrigins(t, db)
	return allProjectsFixture{rules: &service.Rules{DB: db}, projects: &service.Projects{DB: db}}
}

func (f allProjectsFixture) newProject(t *testing.T, name string) domain.ProjectID {
	t.Helper()
	p, err := f.projects.Create(t.Context(), service.LocalPrincipal(apAdmin), orgAP, name)
	if err != nil {
		t.Fatalf("create project %s: %v", name, err)
	}
	return domain.ProjectID(p.ID)
}

func allProjectsWhere(keyMode domain.AxisMode, folders ...string) domain.Where {
	w := domain.Where{AllProjects: true, EnvMode: domain.AxisAll, KeyMode: keyMode}
	for _, path := range folders {
		if w.Keys == nil {
			w.Keys = map[domain.ProjectID][]domain.RuleKeyItem{}
		}
		w.Keys[domain.AnyProject] = append(w.Keys[domain.AnyProject], folderItem(path))
	}
	return w
}

// orgProjectIDs is the org's current projects, read as a client would before
// creating an all-projects rule (owner decision R2).
func orgProjectIDs(t *testing.T, db *store.DB, org domain.OrgID) []domain.ProjectID {
	t.Helper()
	var out []domain.ProjectID
	for id := range strings.SplitSeq(queryStrings(t, db, "SELECT id || ',' FROM projects WHERE org_id = '"+string(org)+"' ORDER BY id"), ",") {
		if id != "" {
			out = append(out, domain.ProjectID(id))
		}
	}
	return out
}

// claimAll is spec claiming the org's current projects, as a client does.
func claimAll(t *testing.T, db *store.DB, spec service.RuleSpec) service.RuleSpec {
	t.Helper()
	spec.ClaimedProjects = orgProjectIDs(t, db, spec.Org)
	return spec
}

func ruleCount(t *testing.T, db *store.DB, id string) int64 {
	t.Helper()
	return queryInt(t, db, "SELECT COUNT(*) FROM rules WHERE id = '"+id+"'")
}

func TestAllProjectsRules(t *testing.T) {
	forEngines(t, runAllProjectsRules)
}

func runAllProjectsRules(t *testing.T, db *store.DB) {
	f := seedAllProjectsFixture(t, db)
	ctx := t.Context()
	read := func(w domain.Where) service.RuleSpec {
		return claimAll(t, db, service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP, Where: w})
	}

	t.Run("an_org_without_projects_refuses_the_rule", func(t *testing.T) {
		_, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), read(allProjectsWhere(domain.AxisAll)))
		if !errors.Is(err, service.ErrRuleAllProjectsNoProject) || !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("create in an empty org = %v, want ErrRuleAllProjectsNoProject", err)
		}
		// An instance-scope member manager is told why too.
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apInstance), read(allProjectsWhere(domain.AxisAll))); !errors.Is(err, service.ErrRuleAllProjectsNoProject) {
			t.Fatalf("instance manager's create in an empty org = %v, want ErrRuleAllProjectsNoProject", err)
		}
		// Someone without member management there learns nothing.
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apHolder), read(allProjectsWhere(domain.AxisAll))); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("an outsider's create in an empty org = %v, want not found", err)
		}
	})

	p1, p2 := f.newProject(t, "one"), f.newProject(t, "two")

	// R2: the client's project list must be exactly the org's current
	// projects, compared under the create transaction's project lock.
	t.Run("a_stale_project_list_is_refused", func(t *testing.T) {
		stale := func(t *testing.T, claimed []domain.ProjectID, names ...string) {
			t.Helper()
			spec := service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP, Where: allProjectsWhere(domain.AxisAll), ClaimedProjects: claimed}
			_, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), spec)
			var detail interface{ SafeDetail() string }
			if !errors.Is(err, service.ErrRuleAllProjectsStale) || !errors.Is(err, domain.ErrConflict) || !errors.As(err, &detail) {
				t.Fatalf("claim %v = %v, want ErrRuleAllProjectsStale with a detail", claimed, err)
			}
			for _, n := range names {
				if !strings.Contains(detail.SafeDetail(), n) {
					t.Fatalf("refusal %q does not name %s", detail.SafeDetail(), n)
				}
			}
		}
		t.Run("missing_a_project", func(t *testing.T) { stale(t, []domain.ProjectID{p1}, "missing "+string(p2)) })
		t.Run("listing_none", func(t *testing.T) { stale(t, nil, string(p1), string(p2)) })
		t.Run("an_extra_project", func(t *testing.T) {
			stale(t, []domain.ProjectID{p1, p2, "prj_elsewhere"}, "not in the organization prj_elsewhere")
		})
		t.Run("a_deleted_project", func(t *testing.T) {
			gone := f.newProject(t, "gone")
			if err := f.projects.Delete(ctx, service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: gone}); err != nil {
				t.Fatal(err)
			}
			stale(t, []domain.ProjectID{p1, p2, gone}, string(gone))
		})
		t.Run("a_duplicate", func(t *testing.T) { stale(t, []domain.ProjectID{p1, p2, p2}, "listed twice "+string(p2)) })
		t.Run("a_project_added_after_listing", func(t *testing.T) {
			listed := orgProjectIDs(t, db, orgAP)
			late := f.newProject(t, "late")
			stale(t, listed, "missing "+string(late))
			if err := f.projects.Delete(ctx, service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: late}); err != nil {
				t.Fatal(err)
			}
		})
		if n := queryInt(t, db, "SELECT COUNT(*) FROM rules WHERE org_id = 'org_ap'"); n != 0 {
			t.Fatalf("%d rules written by refused creates", n)
		}
	})

	t.Run("environment_names_are_held_and_single_keys_are_refused", func(t *testing.T) {
		// ADR D3 (#1447) lifts restriction A: an all-projects rule names
		// environments by name, matched in every project.
		w := allProjectsWhere(domain.AxisAll)
		w.EnvMode = domain.AxisOnly
		w.EnvNames = []string{"prod"}
		named := read(w)
		view, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), named)
		if err != nil {
			t.Fatalf("environment names on an all-projects rule = %v, want held", err)
		}
		if err := f.rules.Revoke(ctx, service.LocalPrincipal(apAdmin), orgAP, view.ID); err != nil {
			t.Fatal(err)
		}
		w = allProjectsWhere(domain.AxisOnly)
		w.Keys = map[domain.ProjectID][]domain.RuleKeyItem{domain.AnyProject: {{KeyID: "key_x"}}}
		spec := read(w)
		spec.Capability = domain.CapEdit
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), spec); !errors.Is(err, domain.ErrRuleAllProjectsKeys) {
			t.Fatalf("single-key item = %v, want ErrRuleAllProjectsKeys", err)
		}
		// Named projects alongside all-projects are refused too.
		w = allProjectsWhere(domain.AxisAll)
		w.Projects = []domain.ProjectID{p1}
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), read(w)); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("projects with all-projects = %v, want invalid", err)
		}
	})

	t.Run("machine_rules_are_refused", func(t *testing.T) {
		spec := read(allProjectsWhere(domain.AxisAll))
		spec.Target = apMachine
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), spec); !errors.Is(err, service.ErrMachineScope) {
			t.Fatalf("machine all-projects rule = %v, want ErrMachineScope", err)
		}
	})

	t.Run("project_scoped_management_does_not_cover_future_projects", func(t *testing.T) {
		for _, p := range []domain.ProjectID{p1, p2} {
			for _, cap := range []string{"manage-members", "read"} {
				execRaw(t, db, fmt.Sprintf("INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_appa_%s_%s', 'usr_apprjadmin', '%s', 'org_ap', '%s', NULL, %s)", cap, p, cap, p, ts))
			}
		}
		seedOrigins(t, db)
		_, err := f.rules.Create(ctx, service.LocalPrincipal(apPrjAdmin), read(allProjectsWhere(domain.AxisAll)))
		if !errors.Is(err, service.ErrRuleAllProjectsAuthority) {
			t.Fatalf("project-scoped manager over every current project = %v, want ErrRuleAllProjectsAuthority", err)
		}
		// Positive control: the same manager may create the explicit rule.
		spec := read(domain.Where{Projects: []domain.ProjectID{p1, p2}, EnvMode: domain.AxisAll, KeyMode: domain.AxisAll})
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apPrjAdmin), spec); err != nil {
			t.Fatalf("explicit rule over the same projects: %v", err)
		}
	})

	// An item-less all-projects rule: every read must survive the absence of
	// rule_items rows (formerly inner joins) and of Projects[0].
	view, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), read(allProjectsWhere(domain.AxisAll)))
	if err != nil {
		t.Fatalf("create all-projects rule: %v", err)
	}
	if !view.Where.AllProjects || !slices.Equal(view.Covers, []domain.ProjectID{p1, p2}) && !slices.Equal(view.Covers, []domain.ProjectID{p2, p1}) {
		t.Fatalf("created view %+v, want all-projects covering both projects", view)
	}
	folderRule, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), claimAll(t, db, service.RuleSpec{Target: apHolder, Capability: domain.CapEdit, Org: orgAP,
		Where: allProjectsWhere(domain.AxisAll, "secret")}))
	if err != nil {
		t.Fatalf("create all-projects folder-except rule: %v", err)
	}
	// The exact current list is accepted and stored as the mode only: no
	// project item, and the folder item names no project.
	for _, id := range []string{view.ID, folderRule.ID} {
		if mode := queryString(t, db, "SELECT project_mode FROM rules WHERE id = '"+id+"'"); mode != "all" {
			t.Fatalf("rule %s stored project_mode %q", id, mode)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM rule_items WHERE rule_id = '"+id+"' AND (axis = 'project' OR project_id IS NOT NULL)"); n != 0 {
			t.Fatalf("rule %s stored %d per-project items", id, n)
		}
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM rule_items WHERE rule_id = '"+folderRule.ID+"' AND axis = 'folder'"); n != 1 {
		t.Fatalf("folder rule stored %d folder items, want 1", n)
	}

	t.Run("listings_expand_at_read_time", func(t *testing.T) {
		org, err := f.rules.List(ctx, service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP})
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, v := range org {
			if v.ID == view.ID || v.ID == folderRule.ID {
				found++
				if !v.Where.AllProjects || len(v.Covers) != 2 || v.OtherProjects {
					t.Fatalf("org listing of %s = %+v, want both projects and other_projects false", v.ID, v)
				}
			}
			if v.ID == folderRule.ID && len(v.Where.Keys[domain.AnyProject]) != 1 {
				t.Fatalf("folder rule listed with keys %+v", v.Where.Keys)
			}
		}
		if found != 2 {
			t.Fatalf("org listing found %d of the 2 all-projects rules", found)
		}
		project, err := f.rules.List(ctx, service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: p1})
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(project, func(v service.RuleView) bool { return v.ID == view.ID })
		if i < 0 || !slices.Equal(project[i].Covers, []domain.ProjectID{p1}) || !project[i].OtherProjects {
			t.Fatalf("project listing %+v, want the rule covering only p1 with other_projects", project)
		}
	})

	t.Run("project_scoped_management_cannot_revoke", func(t *testing.T) {
		err := f.rules.Revoke(ctx, service.LocalPrincipal(apPrjAdmin), orgAP, view.ID)
		if !errors.Is(err, service.ErrRuleAllProjectsAuthority) {
			t.Fatalf("project-scoped manager's revoke = %v, want ErrRuleAllProjectsAuthority", err)
		}
		if ruleCount(t, db, view.ID) != 1 {
			t.Fatal("a refused revoke removed the rule")
		}
	})

	// D7 also binds rule-delegated management: Manage-access rules naming every
	// current project do not name the future-project witness.
	t.Run("rule_delegated_management_does_not_cover_future_projects", func(t *testing.T) {
		every := domain.Where{Projects: []domain.ProjectID{p1, p2}, EnvMode: domain.AxisAll, KeyMode: domain.AxisAll}
		for _, c := range []domain.Capability{domain.CapManageMembers, domain.CapRead} {
			if _, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), service.RuleSpec{Target: apRuleMgr, Capability: c, Org: orgAP, Where: every}); err != nil {
				t.Fatalf("give the rule manager %s over every project: %v", c, err)
			}
		}
		_, err := f.rules.Create(ctx, service.LocalPrincipal(apRuleMgr), read(allProjectsWhere(domain.AxisAll)))
		if !errors.Is(err, service.ErrRuleAllProjectsAuthority) {
			t.Fatalf("rule-delegated manager's all-projects create = %v, want ErrRuleAllProjectsAuthority", err)
		}
		if err := f.rules.Revoke(ctx, service.LocalPrincipal(apRuleMgr), orgAP, view.ID); !errors.Is(err, service.ErrRuleAllProjectsAuthority) {
			t.Fatalf("rule-delegated manager's all-projects revoke = %v, want ErrRuleAllProjectsAuthority", err)
		}
		if ruleCount(t, db, view.ID) != 1 {
			t.Fatal("a refused revoke removed the rule")
		}
		// Positive control: the same delegated manager creates and revokes the explicit rule.
		explicit, err := f.rules.Create(ctx, service.LocalPrincipal(apRuleMgr), read(every))
		if err != nil {
			t.Fatalf("rule-delegated manager's explicit rule over the same projects: %v", err)
		}
		if err := f.rules.Revoke(ctx, service.LocalPrincipal(apRuleMgr), orgAP, explicit.ID); err != nil {
			t.Fatalf("rule-delegated manager revokes the explicit rule: %v", err)
		}
	})

	t.Run("a_project_created_later_is_reached", func(t *testing.T) {
		p3 := f.newProject(t, "three")
		got, err := f.projects.List(ctx, service.LocalPrincipal(apHolder), orgAP)
		if err != nil {
			t.Fatalf("holder lists projects: %v", err)
		}
		if !slices.ContainsFunc(got, func(p service.Project) bool { return p.ID == string(p3) }) {
			t.Fatalf("holder's project list %+v misses the later project", got)
		}
		if _, err := f.projects.Get(ctx, service.LocalPrincipal(apHolder), domain.Scope{Org: orgAP, Project: p3}); err != nil {
			t.Fatalf("holder reads the later project: %v", err)
		}
		// Negative control: no rule, no reach.
		if _, err := f.projects.Get(ctx, service.LocalPrincipal(apPrjAdmin), domain.Scope{Org: orgAP, Project: p3}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("project-scoped admin reads the later project = %v, want not found", err)
		}
		// Pruning on deletion never revokes an all-projects rule.
		if err := f.projects.Delete(ctx, service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: p3}); err != nil {
			t.Fatalf("delete the later project: %v", err)
		}
		// Project deletion leaves grants to their own revocation; drop the
		// project-scoped admin's so only the rules are in play.
		execRaw(t, db, "DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id = 'usr_apprjadmin')")
		execRaw(t, db, "DELETE FROM grants WHERE principal_id = 'usr_apprjadmin'")
		if err := f.projects.Delete(ctx, service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: p1}); err != nil {
			t.Fatalf("delete p1: %v", err)
		}
		if ruleCount(t, db, view.ID) != 1 || ruleCount(t, db, folderRule.ID) != 1 {
			t.Fatal("project deletion pruned an all-projects rule")
		}
	})

	t.Run("the_last_project_is_kept_while_rules_need_it", func(t *testing.T) {
		err := f.projects.Delete(ctx, service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: p2})
		if !errors.Is(err, service.ErrLastProjectUnderAllProjectsRules) || !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("delete the last project = %v, want ErrLastProjectUnderAllProjectsRules", err)
		}
		var detail interface{ SafeDetail() string }
		if !errors.As(err, &detail) || !strings.Contains(detail.SafeDetail(), view.ID) || !strings.Contains(detail.SafeDetail(), folderRule.ID) {
			t.Fatalf("refusal %v does not name both blocking rules", err)
		}
		// Rule ids are membership information: a project manager who does not
		// manage members is refused without them.
		err = f.projects.Delete(ctx, service.LocalPrincipal(apPrjOnly), domain.Scope{Org: orgAP, Project: p2})
		if !errors.Is(err, service.ErrLastProjectUnderAllProjectsRules) || !errors.As(err, &detail) {
			t.Fatalf("project manager's delete of the last project = %v, want ErrLastProjectUnderAllProjectsRules", err)
		}
		if strings.Contains(detail.SafeDetail(), view.ID) || strings.Contains(detail.SafeDetail(), folderRule.ID) {
			t.Fatalf("refusal %q named rules to a caller who cannot list them", detail.SafeDetail())
		}
		for _, id := range []string{view.ID, folderRule.ID} {
			if err := f.rules.Revoke(ctx, service.LocalPrincipal(apAdmin), orgAP, id); err != nil {
				t.Fatalf("revoke %s: %v", id, err)
			}
		}
		if err := f.projects.Delete(ctx, service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: p2}); err != nil {
			t.Fatalf("delete the last project once the rules are gone: %v", err)
		}
	})
}

// The D9 key-move census sees all-projects rules: moving a key out of the
// folder an all-projects rule excepts widens that rule's holder.
func TestAllProjectsRulesMoveWidening(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		f.create(t, orgAdmin, claimAll(t, db, service.RuleSpec{Target: hank, Capability: domain.CapEdit, Org: orgA,
			Where: allProjectsWhere(domain.AxisAll, "stripe")}))
		grantCustodianMemberManagement(t, db)
		to := "db"
		_, err := f.keys.UpdateMetadata(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), f.strKeyID,
			service.KeyMetadataUpdate{FolderPath: &to}, nil)
		var widening *service.MoveWideningError
		if !errors.As(err, &widening) || !slices.Contains(widening.Principals, hank) {
			t.Fatalf("move out of the excepted folder = %v, want a widening naming hank", err)
		}
		// Positive control: confirming the named set lets the move through.
		if _, err := f.keys.UpdateMetadata(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), f.strKeyID,
			service.KeyMetadataUpdate{FolderPath: &to, ConfirmWidening: widening.Principals}, nil); err != nil {
			t.Fatalf("confirmed move: %v", err)
		}
	})
}

// "Who can...?" (rule.resolve, #1474) sees an all-projects rule: it reaches a
// key outside its excepted folder and not one inside it.
func TestAllProjectsRulesResolve(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		id := f.create(t, orgAdmin, claimAll(t, db, service.RuleSpec{Target: hank, Capability: domain.CapEdit, Org: orgA,
			Where: allProjectsWhere(domain.AxisAll, "stripe")}))
		resolveIn := func(env domain.Scope, key string) (service.AccessView, bool) {
			t.Helper()
			views, err := f.rules.Resolve(t.Context(), service.LocalPrincipal(orgAdmin), env, domain.CapEdit, key)
			if err != nil {
				t.Fatalf("resolve key %q: %v", key, err)
			}
			i := slices.IndexFunc(views, func(v service.AccessView) bool { return v.Principal == hank })
			if i < 0 {
				return service.AccessView{}, false
			}
			return views[i], true
		}
		resolve := func(key string) (service.AccessView, bool) {
			t.Helper()
			return resolveIn(scopeEnv(orgA, prjA1, envA1), key)
		}
		if v, ok := resolve(f.dbKeyID); !ok || v.State != authz.AccessCan || !slices.Equal(v.Rules, []string{id}) {
			t.Fatalf("hank on the db key = %+v (listed %v), want can through %s", v, ok, id)
		}
		if v, ok := resolve(f.strKeyID); !ok || v.State != authz.AccessExcepted || len(v.Excepts) != 1 || v.Excepts[0].RuleID != id ||
			len(v.Excepts[0].Keys) != 1 || v.Excepts[0].Keys[0].Folder != "stripe" {
			t.Fatalf("hank on the excepted stripe key = %+v (listed %v), want excepted by %s's stripe folder", v, ok, id)
		}
		// No key named: the folder except leaves the rule key-limited.
		if v, ok := resolve(""); !ok || v.State != authz.AccessExcepted || len(v.Excepts) != 1 || !v.Excepts[0].KeyLimited ||
			len(v.Excepts[0].Keys) != 0 || len(v.Excepts[0].Envs) != 0 {
			t.Fatalf("hank with no key named = %+v (listed %v), want key-limited by %s", v, ok, id)
		}
		// The same rule reaches the org's other project.
		if v, ok := resolveIn(scopeEnv(orgA, prjA2, envA2), ""); !ok || !slices.ContainsFunc(v.Excepts, func(e authz.AccessExcept) bool { return e.RuleID == id && e.KeyLimited }) {
			t.Fatalf("hank in the second project = %+v (listed %v), want %s named there too", v, ok, id)
		}
	})
}

// Two concurrent deletions of an org's last two projects under an
// all-projects rule must leave one: the delete side locks the org's projects
// FOR UPDATE. Postgres only; sqlite serializes write transactions.
func TestAllProjectsRulesConcurrentLastProjectDeletes(t *testing.T) {
	db := seededDB(t, openPostgres)
	f := seedAllProjectsFixture(t, db)
	for round := range 3 {
		a, b := f.newProject(t, fmt.Sprintf("a%d", round)), f.newProject(t, fmt.Sprintf("b%d", round))
		rule, err := f.rules.Create(t.Context(), service.LocalPrincipal(apAdmin), claimAll(t, db, service.RuleSpec{Target: apHolder,
			Capability: domain.CapRead, Org: orgAP, Where: allProjectsWhere(domain.AxisAll)}))
		if err != nil {
			t.Fatalf("round %d: create rule: %v", round, err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		for i, p := range []domain.ProjectID{a, b} {
			wg.Go(func() {
				<-start
				errs[i] = f.projects.Delete(t.Context(), service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: p})
			})
		}
		close(start)
		wg.Wait()
		left := queryInt(t, db, "SELECT COUNT(*) FROM projects WHERE org_id = 'org_ap'")
		if left != 1 || ruleCount(t, db, rule.ID) != 1 {
			t.Fatalf("round %d: %d projects and the rule %d left, want exactly one project and the rule (errors %v)", round, left, ruleCount(t, db, rule.ID), errs)
		}
		if (errs[0] == nil) == (errs[1] == nil) {
			t.Fatalf("round %d: want exactly one deletion to succeed, got errors %v", round, errs)
		}
		for _, err := range errs {
			if err != nil && !errors.Is(err, service.ErrLastProjectUnderAllProjectsRules) {
				t.Fatalf("round %d: refusal %v, want ErrLastProjectUnderAllProjectsRules", round, err)
			}
		}
		if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(apAdmin), orgAP, rule.ID); err != nil {
			t.Fatal(err)
		}
		for _, p := range []domain.ProjectID{a, b} {
			_ = f.projects.Delete(t.Context(), service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: p})
		}
	}
}

// A rule create racing the deletion of the org's last project: either the
// rule lands and the project stays, or the project goes and the create is
// refused. Never a rule over an org without projects. Postgres only.
func TestAllProjectsRulesCreateRacesLastProjectDelete(t *testing.T) {
	db := seededDB(t, openPostgres)
	f := seedAllProjectsFixture(t, db)
	for round := range 3 {
		p := f.newProject(t, fmt.Sprintf("solo%d", round))
		spec := claimAll(t, db, service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP, Where: allProjectsWhere(domain.AxisAll)})
		var wg sync.WaitGroup
		start := make(chan struct{})
		var created service.RuleView
		var createErr, deleteErr error
		wg.Go(func() {
			<-start
			created, createErr = f.rules.Create(t.Context(), service.LocalPrincipal(apAdmin), spec)
		})
		wg.Go(func() {
			<-start
			deleteErr = f.projects.Delete(t.Context(), service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: p})
		})
		close(start)
		wg.Wait()
		projects := queryInt(t, db, "SELECT COUNT(*) FROM projects WHERE org_id = 'org_ap'")
		rules := queryInt(t, db, "SELECT COUNT(*) FROM rules WHERE org_id = 'org_ap'")
		if rules > 0 && projects == 0 {
			t.Fatalf("round %d: an all-projects rule outlived the org's last project (create %v, delete %v)", round, createErr, deleteErr)
		}
		if (createErr == nil) == (deleteErr == nil) {
			t.Fatalf("round %d: create %v and delete %v, want exactly one to succeed", round, createErr, deleteErr)
		}
		if createErr == nil {
			if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(apAdmin), orgAP, created.ID); err != nil {
				t.Fatal(err)
			}
			if err := f.projects.Delete(t.Context(), service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: p}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// seedBoundProjects inserts projects into org_ap until it has total, in one
// statement: the v1 bound tests need hundreds.
func seedBoundProjects(t *testing.T, db *store.DB, total int) {
	t.Helper()
	have := queryInt(t, db, "SELECT COUNT(*) FROM projects WHERE org_id = 'org_ap'")
	var rows []string
	for i := int(have); i < total; i++ {
		rows = append(rows, fmt.Sprintf("('prj_bound_%04d', 'org_ap', 'bound %d', %s)", i, i, ts))
	}
	if len(rows) > 0 {
		execRaw(t, db, "INSERT INTO projects (id, org_id, name, created_at) VALUES "+strings.Join(rows, ", "))
	}
}

// An all-projects rule is listed with its folders repeated under every
// current project, and that listing stays within the v1 selector bound
// (domain.MaxRuleItems, projects and items together): a rule create or a
// project create that would pass it is refused, never truncated (#1446).
func TestAllProjectsRulesBound(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedAllProjectsFixture(t, db)
		ctx := t.Context()
		spec := func(folders ...string) service.RuleSpec {
			return claimAll(t, db, service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP, Where: allProjectsWhere(domain.AxisAll, folders...)})
		}
		half := domain.MaxRuleItems / 2
		seedBoundProjects(t, db, half-1)

		atBound, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), spec("secret"))
		if err != nil {
			t.Fatalf("all-projects rule below the bound (%d projects, 1 folder): %v", half-1, err)
		}
		// Exactly the bound through project creation: half the projects, each
		// listing the one folder item.
		f.newProject(t, "at-the-bound")
		// Past it: two folders under every project.
		_, err = f.rules.Create(ctx, service.LocalPrincipal(apAdmin), spec("secret", "stripe"))
		var detail interface{ SafeDetail() string }
		if !errors.Is(err, service.ErrRuleAllProjectsTooLarge) || !errors.Is(err, domain.ErrLimitExceeded) || !errors.As(err, &detail) {
			t.Fatalf("all-projects rule past the bound = %v, want ErrRuleAllProjectsTooLarge with a detail", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM rules WHERE org_id = 'org_ap'"); n != 1 {
			t.Fatalf("%d rules after the refused create, want 1", n)
		}

		// One more project would list the rule past the bound.
		before := queryInt(t, db, "SELECT COUNT(*) FROM projects WHERE org_id = 'org_ap'")
		_, err = f.projects.Create(ctx, service.LocalPrincipal(apAdmin), orgAP, "one-too-many")
		if !errors.Is(err, service.ErrProjectAboveAllProjectsRules) || !errors.Is(err, domain.ErrConflict) || !errors.As(err, &detail) ||
			!strings.Contains(detail.SafeDetail(), atBound.ID) {
			t.Fatalf("project past the bound = %v, want ErrProjectAboveAllProjectsRules naming %s", err, atBound.ID)
		}
		// A project manager without member management learns no rule id.
		_, err = f.projects.Create(ctx, service.LocalPrincipal(apPrjOnly), orgAP, "one-too-many")
		if !errors.Is(err, service.ErrProjectAboveAllProjectsRules) || !errors.As(err, &detail) || strings.Contains(detail.SafeDetail(), atBound.ID) {
			t.Fatalf("project manager's refusal = %v, want ErrProjectAboveAllProjectsRules without the rule id", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM projects WHERE org_id = 'org_ap'"); n != before {
			t.Fatalf("refused project creates left %d projects, want %d", n, before)
		}

		// Positive control: without the folder the rule fits, so the project is created.
		if err := f.rules.Revoke(ctx, service.LocalPrincipal(apAdmin), orgAP, atBound.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), spec()); err != nil {
			t.Fatalf("folder-less all-projects rule over %d projects: %v", half, err)
		}
		f.newProject(t, "fits")

		// A folder-less rule is bounded by the project count alone.
		seedBoundProjects(t, db, domain.MaxRuleItems)
		if _, err := f.rules.Create(ctx, service.LocalPrincipal(apAdmin), spec()); err != nil {
			t.Fatalf("folder-less all-projects rule over %d projects: %v", domain.MaxRuleItems, err)
		}
		if _, err := f.projects.Create(ctx, service.LocalPrincipal(apAdmin), orgAP, "past-the-bound"); !errors.Is(err, service.ErrProjectAboveAllProjectsRules) {
			t.Fatalf("project %d under folder-less rules = %v, want ErrProjectAboveAllProjectsRules", domain.MaxRuleItems+1, err)
		}
	})
}

// A project create racing an all-projects rule create that reaches the bound
// exactly: at most one commits, and no rule is left listed past the bound.
// Postgres only; sqlite serializes write transactions.
func TestAllProjectsRulesCreateRacesProjectCreate(t *testing.T) {
	db := seededDB(t, openPostgres)
	f := seedAllProjectsFixture(t, db)
	half := domain.MaxRuleItems / 2

	// Two project creates under a rule one project short of the bound: the
	// first reaches it exactly, the second would pass it.
	seedBoundProjects(t, db, half-1)
	rule, err := f.rules.Create(t.Context(), service.LocalPrincipal(apAdmin), claimAll(t, db, service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP, Where: allProjectsWhere(domain.AxisAll, "secret")}))
	if err != nil {
		t.Fatal(err)
	}
	for round := range 3 {
		var wg sync.WaitGroup
		start := make(chan struct{})
		var made [2]service.Project
		var errs [2]error
		for i := range 2 {
			wg.Go(func() {
				<-start
				made[i], errs[i] = f.projects.Create(t.Context(), service.LocalPrincipal(apAdmin), orgAP, fmt.Sprintf("pair%d-%d", round, i))
			})
		}
		close(start)
		wg.Wait()
		won := slices.IndexFunc(errs[:], func(err error) bool { return err == nil })
		if won < 0 || !errors.Is(errs[1-won], service.ErrProjectAboveAllProjectsRules) {
			t.Fatalf("round %d: project creates %v, want one to reach the bound and the other refused", round, errs)
		}
		if err := f.projects.Delete(t.Context(), service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: domain.ProjectID(made[won].ID)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(apAdmin), orgAP, rule.ID); err != nil {
		t.Fatal(err)
	}

	// A project create racing a rule create that reaches the bound exactly.
	seedBoundProjects(t, db, half)
	for round := range 3 {
		spec := claimAll(t, db, service.RuleSpec{Target: apHolder, Capability: domain.CapRead, Org: orgAP, Where: allProjectsWhere(domain.AxisAll, "secret")})
		var wg sync.WaitGroup
		start := make(chan struct{})
		var created service.RuleView
		var project service.Project
		var ruleErr, projectErr error
		wg.Go(func() {
			<-start
			created, ruleErr = f.rules.Create(t.Context(), service.LocalPrincipal(apAdmin), spec)
		})
		wg.Go(func() {
			<-start
			project, projectErr = f.projects.Create(t.Context(), service.LocalPrincipal(apAdmin), orgAP, fmt.Sprintf("race%d", round))
		})
		close(start)
		wg.Wait()
		if ruleErr == nil && projectErr == nil {
			t.Fatalf("round %d: both the rule and the project committed, listing the rule past the bound", round)
		}
		// The loser is refused by name: a rule create that lost lists a stale
		// project set, a project create that lost would pass the bound.
		if !errors.Is(ruleErr, service.ErrRuleAllProjectsStale) && !errors.Is(projectErr, service.ErrProjectAboveAllProjectsRules) {
			t.Fatalf("round %d: rule %v and project %v, want one to commit and the other refused by name", round, ruleErr, projectErr)
		}
		if ruleErr == nil {
			if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(apAdmin), orgAP, created.ID); err != nil {
				t.Fatal(err)
			}
		} else if err := f.projects.Delete(t.Context(), service.LocalPrincipal(apAdmin), domain.Scope{Org: orgAP, Project: domain.ProjectID(project.ID)}); err != nil {
			t.Fatal(err)
		}
	}
}

// The wire (owner decision R2): a create with project_mode all lists the org's
// current projects with its folder items repeated under each; a stale list is
// a 409 naming the difference; a rule reads back expanded the same way, and
// what is read back is itself a valid create request. The last project's
// refused deletion names the rule in the 409 body.
func TestAllProjectsRulesWire(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := newAccessWireEnv(t, db)
		const (
			holder = "usr_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f81"
			second = "prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f82"
		)
		execRaw(t, db, fmt.Sprintf(`INSERT INTO principals (id, kind, created_at) VALUES ('%s', 'human', %s)`, holder, ts))
		org := api.PathPrefix + "/orgs/" + e.org
		all := apigen.RuleProjectModeAll
		secret := "secret"
		body := func(projects ...apigen.ID) apigen.CreateRuleRequest {
			w := apigen.RuleWhere{ProjectMode: &all, Projects: projects,
				Environments: apigen.RuleEnvironmentAxis{Mode: apigen.RuleAxisModeAll, Items: []apigen.RuleEnvironmentItem{}},
				Keys:         apigen.RuleKeyAxis{Mode: apigen.RuleAxisModeAll, Items: []apigen.RuleKeyItem{}}}
			for _, p := range projects {
				w.Keys.Items = append(w.Keys.Items, apigen.RuleKeyItem{Project: p, Folder: &secret})
			}
			return apigen.CreateRuleRequest{Principal: holder, Capability: apigen.RuleCapabilityEdit, Where: w}
		}

		// A project created after the client listed the org's projects.
		execRaw(t, db, fmt.Sprintf(`INSERT INTO projects (id, org_id, name, created_at) VALUES ('%s', '%s', 'second', %s)`, second, e.org, ts))
		code, raw := e.call(t, http.MethodPost, org+"/rules", body(e.project))
		var refusal apigen.Error
		if code != http.StatusConflict || json.Unmarshal(raw, &refusal) != nil || refusal.Error.Detail == nil || !strings.Contains(*refusal.Error.Detail, second) {
			t.Fatalf("stale list = %d %s, want 409 naming %s", code, raw, second)
		}
		// Folder items that differ between the listed projects are refused on shape.
		uneven := body(e.project, second)
		uneven.Where.Keys.Items = uneven.Where.Keys.Items[:1]
		if code, raw := e.call(t, http.MethodPost, org+"/rules", uneven); code != http.StatusBadRequest {
			t.Fatalf("uneven folder items = %d %s, want 400", code, raw)
		}

		code, raw = e.call(t, http.MethodPost, org+"/rules", body(second, e.project))
		var created apigen.Rule
		if code != http.StatusCreated || json.Unmarshal(raw, &created) != nil {
			t.Fatalf("create = %d %s", code, raw)
		}
		code, raw = e.call(t, http.MethodGet, org+"/rules", nil)
		var list apigen.RuleList
		if code != http.StatusOK || json.Unmarshal(raw, &list) != nil {
			t.Fatalf("list = %d %s", code, raw)
		}
		i := slices.IndexFunc(list.Items, func(r apigen.Rule) bool { return r.Id == created.Id })
		if i < 0 {
			t.Fatalf("list %s misses the rule", raw)
		}
		w := list.Items[i].Where
		projects := slices.Sorted(slices.Values(w.Projects))
		if w.ProjectMode == nil || *w.ProjectMode != apigen.RuleProjectModeAll || !slices.Equal(projects, slices.Sorted(slices.Values([]apigen.ID{e.project, second}))) ||
			len(w.Keys.Items) != 2 || slices.ContainsFunc(w.Keys.Items, func(k apigen.RuleKeyItem) bool { return k.Folder == nil || *k.Folder != secret }) {
			t.Fatalf("wire where %s, want project_mode all expanded over both projects with the folder repeated", raw)
		}
		// Round trip: the read-back where is a valid create request.
		again := apigen.CreateRuleRequest{Principal: holder, Capability: apigen.RuleCapabilityRevealHistory, Where: w}
		if code, raw := e.call(t, http.MethodPost, org+"/rules", again); code != http.StatusCreated {
			t.Fatalf("read-back where as a create = %d %s", code, raw)
		}
		// The replace endpoint reads the same spelling, stale lists included.
		replace := func(where apigen.RuleWhere) (int, []byte) {
			create := []apigen.CreateRuleRequest{{Principal: holder, Capability: apigen.RuleCapabilityRead, Where: where}}
			return e.call(t, http.MethodPost, org+"/rules/replace", apigen.ReplaceRulesRequest{Principal: holder, Create: &create})
		}
		stale := body(e.project).Where
		if code, raw := replace(stale); code != http.StatusConflict || !bytes.Contains(raw, []byte(second)) {
			t.Fatalf("replace with a stale list = %d %s, want 409 naming %s", code, raw, second)
		}
		if code, raw := replace(w); code != http.StatusOK || !bytes.Contains(raw, []byte(`"project_mode":"all"`)) {
			t.Fatalf("replace with the current list = %d %s", code, raw)
		}

		if code, raw := e.call(t, http.MethodDelete, org+"/projects/"+second, nil); code != http.StatusNoContent {
			t.Fatalf("delete the second project = %d %s", code, raw)
		}
		code, raw = e.call(t, http.MethodDelete, org+"/projects/"+e.project, nil)
		var gone apigen.Error
		if code != http.StatusConflict || json.Unmarshal(raw, &gone) != nil || gone.Error.Detail == nil || !strings.Contains(*gone.Error.Detail, created.Id) {
			t.Fatalf("delete the last project = %d %s, want 409 naming %s", code, raw, created.Id)
		}
	})
}

// The wire of the listing bound: a project create that would list an
// all-projects rule past it is a 409 conflict whose detail names the rule (a
// limit_exceeded body is fixed, so it could not).
func TestAllProjectsRulesBoundWire(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := newAccessWireEnv(t, db)
		const holder = "usr_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f91"
		execRaw(t, db, fmt.Sprintf(`INSERT INTO principals (id, kind, created_at) VALUES ('%s', 'human', %s)`, holder, ts))
		var rows []string
		for i := 1; i < domain.MaxRuleItems; i++ {
			rows = append(rows, fmt.Sprintf("('prj_0193f0b4-1f2a-7c31-9c1e-%012d', '%s', 'wire bound %d', %s)", i, e.org, i, ts))
		}
		execRaw(t, db, "INSERT INTO projects (id, org_id, name, created_at) VALUES "+strings.Join(rows, ", "))
		org := api.PathPrefix + "/orgs/" + e.org
		all := apigen.RuleProjectModeAll
		var projects []apigen.ID
		for id := range strings.SplitSeq(queryStrings(t, db, "SELECT id || ',' FROM projects WHERE org_id = '"+e.org+"' ORDER BY id"), ",") {
			if id != "" {
				projects = append(projects, id)
			}
		}
		code, raw := e.call(t, http.MethodPost, org+"/rules", apigen.CreateRuleRequest{Principal: holder, Capability: apigen.RuleCapabilityRead, Where: apigen.RuleWhere{
			ProjectMode: &all, Projects: projects,
			Environments: apigen.RuleEnvironmentAxis{Mode: apigen.RuleAxisModeAll, Items: []apigen.RuleEnvironmentItem{}},
			Keys:         apigen.RuleKeyAxis{Mode: apigen.RuleAxisModeAll, Items: []apigen.RuleKeyItem{}},
		}})
		var created apigen.Rule
		if code != http.StatusCreated || json.Unmarshal(raw, &created) != nil {
			t.Fatalf("rule over %d projects = %d %s", len(projects), code, raw)
		}
		code, raw = e.call(t, http.MethodPost, org+"/projects", apigen.CreateProjectRequest{Name: "one-too-many"})
		var refusal apigen.Error
		if code != http.StatusConflict || json.Unmarshal(raw, &refusal) != nil || refusal.Error.Code != apigen.ErrorCodeConflict ||
			refusal.Error.Detail == nil || !strings.Contains(*refusal.Error.Detail, created.Id) {
			t.Fatalf("project past the bound = %d %s, want 409 conflict naming %s", code, raw, created.Id)
		}
	})
}
