package domain

import (
	"slices"
	"testing"
)

// envRefs names environments whose ids double as their names, in one project.
func envRefs(p ProjectID, names ...string) []EnvRef {
	out := make([]EnvRef, 0, len(names))
	for _, n := range names {
		out = append(out, EnvRef{Project: p, ID: EnvID(n), Name: n})
	}
	return out
}

func TestRuleReachesFailsClosed(t *testing.T) {
	const org, p1, p2 = OrgID("org"), ProjectID("p1"), ProjectID("p2")
	dev := Scope{Org: org, Project: p1, Env: "dev"}
	prod := Scope{Org: org, Project: p1, Env: "prod"}
	db := &RuleKey{ID: "k1", Folder: "db"}
	stripe := &RuleKey{ID: "k2", Folder: "stripe"}
	newInDB := &RuleKey{Folder: "db"}

	reveal := Rule{ID: "r", Capability: CapReveal, Org: org, Where: Where{
		Projects: []ProjectID{p1}, EnvMode: AxisAll, EnvNames: []string{"prod"},
		KeyMode: AxisOnly, Keys: map[ProjectID][]RuleKeyItem{p1: {{Folder: "db", IsFolder: true}}},
	}}
	reveal.Where = reveal.Where.BindEnvs(append(envRefs(p1, "dev", "prod"), envRefs(p2, "dev", "prod")...))
	if err := reveal.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		c    Capability
		at   Level
		s    Scope
		key  *RuleKey
		want bool
	}{
		"covered key":               {CapReveal, LevelEnv, dev, db, true},
		"key created in the folder": {CapReveal, LevelEnv, dev, newInDB, true},
		"other folder":              {CapReveal, LevelEnv, dev, stripe, false},
		"no key named":              {CapReveal, LevelEnv, dev, nil, false},
		"excepted env":              {CapReveal, LevelEnv, prod, db, false},
		"other capability":          {CapEdit, LevelEnv, dev, db, false},
		"project atom":              {CapReveal, LevelProject, Scope{Org: org, Project: p1}, db, false},
		"org atom":                  {CapReveal, LevelOrg, Scope{Org: org}, db, false},
		"other project":             {CapReveal, LevelEnv, Scope{Org: org, Project: p2, Env: "dev"}, db, false},
		"other org":                 {CapReveal, LevelEnv, Scope{Org: "x", Project: p1, Env: "dev"}, db, false},
		"env atom without an env":   {CapReveal, LevelEnv, Scope{Org: org, Project: p1}, db, false},
	} {
		if got := reveal.Reaches(tc.c, tc.at, tc.s, tc.key); got != tc.want {
			t.Errorf("%s: Reaches = %v, want %v", name, got, tc.want)
		}
	}

	members := Rule{ID: "m", Capability: CapManageMembers, Org: org, Where: Where{Projects: []ProjectID{p1}, EnvMode: AxisAll, KeyMode: AxisAll}}
	if members.Reaches(CapManageMembers, LevelProject, Scope{Org: org, Project: p1}, nil) {
		t.Fatal("manage-members on a rule satisfied an atom")
	}
	whole := Rule{ID: "w", Capability: CapDefinitionsEdit, Org: org, Where: Where{Projects: []ProjectID{p1}, EnvMode: AxisAll, KeyMode: AxisAll}}
	if !whole.Reaches(CapDefinitionsEdit, LevelProject, Scope{Org: org, Project: p1}, nil) {
		t.Fatal("an unnarrowed rule did not reach its project atom")
	}
}

func TestRuleValidateRefusesWideningShapes(t *testing.T) {
	base := func(c Capability, w Where) Rule { return Rule{ID: "r", Capability: c, Org: "org", Where: w} }
	keys := map[ProjectID][]RuleKeyItem{"p": {{KeyID: "k"}}}
	for name, r := range map[string]Rule{
		"pin by key":                 base(CapPin, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisOnly, Keys: keys}),
		"settings by env":            base(CapProjectSettings, Where{Projects: []ProjectID{"p"}, EnvMode: AxisOnly, EnvNames: []string{"e"}, KeyMode: AxisAll}),
		"settings except env":        base(CapProjectSettings, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, EnvNames: []string{"e"}, KeyMode: AxisAll}),
		"duplicate env name":         base(CapEdit, Where{Projects: []ProjectID{"p"}, EnvMode: AxisOnly, EnvNames: []string{"e", "e"}, KeyMode: AxisAll}),
		"empty env name":             base(CapEdit, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, EnvNames: []string{""}, KeyMode: AxisAll}),
		"all projects only no names": base(CapEdit, Where{AllProjects: true, EnvMode: AxisOnly, KeyMode: AxisAll}),
		"manage-projects":            base(CapManageProjects, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisAll}),
		"instance atom":              base(CapInstanceConfig, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisAll}),
		"empty only envs":            base(CapEdit, Where{Projects: []ProjectID{"p"}, EnvMode: AxisOnly, KeyMode: AxisAll}),
		"empty only keys":            base(CapEdit, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisOnly}),
		"no projects":                base(CapEdit, Where{EnvMode: AxisAll, KeyMode: AxisAll}),
		"item outside project":       base(CapEdit, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisAll, Keys: map[ProjectID][]RuleKeyItem{"q": {{KeyID: "k"}}}}),
		"unknown mode":               base(CapEdit, Where{Projects: []ProjectID{"p"}, EnvMode: "some", KeyMode: AxisAll}),
		"no org":                     {ID: "r", Capability: CapEdit, Where: Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisAll}},
	} {
		if r.Validate() == nil {
			t.Errorf("%s: validated", name)
		}
	}
}

func TestRuleValidateBoundsItems(t *testing.T) {
	envs := make([]string, MaxRuleItems)
	for i := range envs {
		envs[i] = "e" + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/676))
	}
	r := Rule{ID: "r", Capability: CapEdit, Org: "org", Where: Where{Projects: []ProjectID{"p"}, EnvMode: AxisOnly, EnvNames: envs, KeyMode: AxisAll}}
	if err := r.Validate(); err == nil {
		t.Fatal("an oversized rule validated")
	}
	all := Rule{ID: "r", Capability: CapEdit, Org: "org", Where: Where{AllProjects: true, EnvMode: AxisOnly, EnvNames: append(envs, "one-more"), KeyMode: AxisAll}}
	if err := all.Validate(); err == nil {
		t.Fatal("an oversized all-projects rule validated")
	}
}

// Folder paths are hierarchical: an except of a folder covers its subfolders,
// an only-pick of a folder does not (both err towards denial), and excepting
// the root folder excepts only root keys.
func TestRuleFolderSelectorsAreHierarchicalOnlyForExcepts(t *testing.T) {
	const org, p = OrgID("org"), ProjectID("p")
	dev := Scope{Org: org, Project: p, Env: "dev"}
	rule := func(mode AxisMode, folder string) Rule {
		return Rule{ID: "r", Capability: CapEdit, Org: org, Where: Where{
			Projects: []ProjectID{p}, EnvMode: AxisAll, KeyMode: mode,
			Keys: map[ProjectID][]RuleKeyItem{p: {{Folder: folder, IsFolder: true}}},
		}}
	}
	for name, tc := range map[string]struct {
		r      Rule
		folder string
		want   bool
	}{
		"except db excludes db":             {rule(AxisAll, "db"), "db", false},
		"except db excludes db/sub":         {rule(AxisAll, "db"), "db/sub", false},
		"except db excludes db/sub/deeper":  {rule(AxisAll, "db"), "db/sub/deeper", false},
		"except db keeps dbx":               {rule(AxisAll, "db"), "dbx", true},
		"except db keeps the root":          {rule(AxisAll, "db"), "", true},
		"only db admits db":                 {rule(AxisOnly, "db"), "db", true},
		"only db does not admit db/sub":     {rule(AxisOnly, "db"), "db/sub", false},
		"except root excludes root keys":    {rule(AxisAll, ""), "", false},
		"except root keeps foldered keys":   {rule(AxisAll, ""), "db", true},
		"only root admits only root keys":   {rule(AxisOnly, ""), "", true},
		"only root does not admit a folder": {rule(AxisOnly, ""), "db", false},
	} {
		if err := tc.r.Validate(); err != nil {
			t.Fatal(err)
		}
		if got := tc.r.Reaches(CapEdit, LevelEnv, dev, &RuleKey{ID: "k", Folder: tc.folder}); got != tc.want {
			t.Errorf("%s: Reaches = %v, want %v", name, got, tc.want)
		}
	}
}

func TestSeeIgnoresKeysButPublishDoesNot(t *testing.T) {
	w := Where{Projects: []ProjectID{"p"}, EnvMode: AxisOnly, EnvNames: []string{"dev"}, KeyMode: AxisOnly, Keys: map[ProjectID][]RuleKeyItem{"p": {{KeyID: "one"}}}}.BindEnvs(envRefs("p", "dev", "prod"))
	scope := Scope{Org: "org", Project: "p", Env: "dev"}
	read := Rule{Org: "org", Capability: CapRead, Where: w}
	publish := Rule{Org: "org", Capability: CapPublish, Where: w}
	for _, r := range []Rule{read, publish} {
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if !read.Reaches(CapRead, LevelEnv, scope, nil) || !read.Reaches(CapRead, LevelEnv, scope, &RuleKey{ID: "other"}) {
		t.Fatal("See must cover the environment catalogue")
	}
	if !publish.Reaches(CapPublish, LevelEnv, scope, &RuleKey{ID: "one"}) || publish.Reaches(CapPublish, LevelEnv, scope, &RuleKey{ID: "other"}) || publish.Reaches(CapPublish, LevelEnv, scope, nil) {
		t.Fatal("Publish must require the selected key")
	}
	scope.Env = "prod"
	if read.Reaches(CapRead, LevelEnv, scope, nil) {
		t.Fatal("key-independent See escaped its environment selector")
	}
}

func TestSelectedEnvironmentDefinitionsOnlyProjectForNamedKey(t *testing.T) {
	scope := Scope{Org: "org", Project: "project"}
	rule := Rule{Org: scope.Org, Capability: CapDefinitionsEdit, Where: Where{
		Projects: []ProjectID{scope.Project}, EnvMode: AxisOnly, EnvNames: []string{"staging"},
		KeyMode: AxisOnly, Keys: map[ProjectID][]RuleKeyItem{scope.Project: {{Folder: "db", IsFolder: true}}},
	}}
	unbound := rule
	rule.Where = rule.Where.BindEnvs(envRefs(scope.Project, "staging", "prod"))
	if unbound.Reaches(CapDefinitionsEdit, LevelProject, scope, &RuleKey{ID: "key", Folder: "db"}) {
		t.Fatal("an unbound name-narrowed rule reached an atom")
	}
	key := &RuleKey{ID: "key", Folder: "db"}
	if !rule.Reaches(CapDefinitionsEdit, LevelProject, scope, key) {
		t.Fatal("named-key shared catalogue admission")
	}
	if rule.Reaches(CapDefinitionsEdit, LevelProject, scope, nil) {
		t.Fatal("no broad schema admission")
	}
	scope.Env = "staging"
	if !rule.Reaches(CapDefinitionsEdit, LevelEnv, scope, key) {
		t.Fatal("selected destination")
	}
	scope.Env = "prod"
	if rule.Reaches(CapDefinitionsEdit, LevelEnv, scope, key) {
		t.Fatal("excluded destination")
	}
}

func TestCheckMachineRule(t *testing.T) {
	const org, p1, p2 = OrgID("org"), ProjectID("p1"), ProjectID("p2")
	where := func(envMode AxisMode, envs []string, projects ...ProjectID) Where {
		return Where{Projects: projects, EnvMode: envMode, KeyMode: AxisAll, EnvNames: envs}
	}
	folder := where(AxisAll, nil, p1)
	folder.Keys = map[ProjectID][]RuleKeyItem{p1: {{Folder: "db", IsFolder: true}}}
	for name, tc := range map[string]struct {
		class PrincipalClass
		cap   Capability
		where Where
		want  MachineRuleFault
	}{
		"automation edit on one project":    {ClassAutomation, CapEdit, where(AxisAll, nil, p1), MachineRuleOK},
		"automation read except an env":     {ClassAutomation, CapRead, where(AxisAll, []string{"prod"}, p1), MachineRuleOK},
		"workload read on listed envs":      {ClassWorkload, CapRead, where(AxisOnly, []string{"dev"}, p1), MachineRuleOK},
		"workload read on every env":        {ClassWorkload, CapRead, where(AxisAll, nil, p1), MachineRuleDepth},
		"workload read except an env":       {ClassWorkload, CapRead, where(AxisAll, []string{"prod"}, p1), MachineRuleDepth},
		"automation on two projects":        {ClassAutomation, CapRead, where(AxisAll, nil, p1, p2), MachineRuleDepth},
		"workload edit":                     {ClassWorkload, CapEdit, where(AxisOnly, []string{"dev"}, p1), MachineRuleCapability},
		"automation reveal":                 {ClassAutomation, CapReveal, where(AxisAll, nil, p1), MachineRuleCapability},
		"automation reveal-history":         {ClassAutomation, CapRevealHistory, where(AxisAll, nil, p1), MachineRuleCapability},
		"automation manage-members":         {ClassAutomation, CapManageMembers, where(AxisAll, nil, p1), MachineRuleCapability},
		"provisioning connection":           {ClassProvisioning, CapRead, where(AxisAll, nil, p1), MachineRuleCapability},
		"instance connection":               {ClassInstanceConn, CapRead, where(AxisAll, nil, p1), MachineRuleCapability},
		"developer":                         {ClassDeveloper, CapRead, where(AxisAll, nil, p1), MachineRuleCapability},
		"automation read narrowed by keys":  {ClassAutomation, CapRead, folder, MachineRuleKeys},
		"automation only-keys with no item": {ClassAutomation, CapEdit, Where{Projects: []ProjectID{p1}, EnvMode: AxisAll, KeyMode: AxisOnly}, MachineRuleKeys},
	} {
		r := Rule{Capability: tc.cap, Org: org, Where: tc.where}
		if got := CheckMachineRule(tc.class, r); got != tc.want {
			t.Errorf("%s: CheckMachineRule = %d, want %d", name, got, tc.want)
		}
	}
}

// An all-projects rule reaches every project's environments but never an
// organisation-level atom: org-scope checks are answered from organisation
// grants alone. The web scope inspector answers the same (ruleHolders in
// web/src/routes/accessRules/model.ts), which its unit test pins (#1446).
func TestAllProjectsRuleDoesNotReachOrgScope(t *testing.T) {
	const org = OrgID("org")
	all := Rule{ID: "a", Capability: CapEdit, Org: org, Where: Where{AllProjects: true, EnvMode: AxisAll, KeyMode: AxisAll}}
	if err := all.Validate(); err != nil {
		t.Fatal(err)
	}
	if !all.Reaches(CapEdit, LevelEnv, Scope{Org: org, Project: "any", Env: "dev"}, nil) {
		t.Fatal("an unnarrowed all-projects rule did not reach an environment of a project")
	}
	if !all.Reaches(CapEdit, LevelProject, Scope{Org: org, Project: "any"}, nil) {
		t.Fatal("an unnarrowed all-projects rule did not reach a project atom")
	}
	if all.Reaches(CapEdit, LevelOrg, Scope{Org: org}, nil) || all.Reaches(CapEdit, LevelEnv, Scope{Org: org}, nil) {
		t.Fatal("an all-projects rule reached the organisation scope")
	}
}

// Environment names match in every project a rule covers, including an
// environment that carries the name only later (ADR D3, #1447): "except prod"
// over two projects excludes both prod environments and a later one, and
// "only prod" admits a prod created later.
func TestRuleEnvironmentNamesMatchAcrossProjects(t *testing.T) {
	const org, pay, web = OrgID("org"), ProjectID("payments"), ProjectID("web")
	except := Rule{ID: "x", Capability: CapEdit, Org: org, Where: Where{Projects: []ProjectID{pay, web}, EnvMode: AxisAll, EnvNames: []string{"prod"}, KeyMode: AxisAll}}
	only := Rule{ID: "o", Capability: CapEdit, Org: org, Where: Where{AllProjects: true, EnvMode: AxisOnly, EnvNames: []string{"prod"}, KeyMode: AxisAll}}
	refs := []EnvRef{{pay, "e1", "prod"}, {pay, "e2", "dev"}, {web, "e3", "prod"}, {web, "e4", "dev"}}
	later := append(refs, EnvRef{"api", "e5", "prod"}, EnvRef{"api", "e6", "dev"})
	reach := func(r Rule, refs []EnvRef, p ProjectID, env EnvID) bool {
		r.Where = r.Where.BindEnvs(refs)
		return r.Validate() == nil && r.Reaches(CapEdit, LevelEnv, Scope{Org: org, Project: p, Env: env}, nil)
	}
	for name, tc := range map[string]struct {
		got, want bool
	}{
		"except: payments prod":    {reach(except, refs, pay, "e1"), false},
		"except: web prod":         {reach(except, refs, web, "e3"), false},
		"except: web dev":          {reach(except, refs, web, "e4"), true},
		"only: payments prod":      {reach(only, refs, pay, "e1"), true},
		"only: payments dev":       {reach(only, refs, pay, "e2"), false},
		"only: later project prod": {reach(only, later, "api", "e5"), true},
		"only: later project dev":  {reach(only, later, "api", "e6"), false},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: Reaches = %v, want %v", name, tc.got, tc.want)
		}
	}
	// A prod created later in web joins the except: the project-wide atom
	// holds while web has no prod and stops holding once it has one.
	webOnly := []EnvRef{{web, "e4", "dev"}}
	bound := except
	bound.Where = except.Where.BindEnvs(webOnly)
	if !bound.Reaches(CapEdit, LevelProject, Scope{Org: org, Project: web}, nil) {
		t.Fatal("except prod over a project without prod did not reach the project")
	}
	bound.Where = except.Where.BindEnvs(append(webOnly, EnvRef{web, "e7", "prod"}))
	if bound.Reaches(CapEdit, LevelEnv, Scope{Org: org, Project: web, Env: "e7"}, nil) || bound.Reaches(CapEdit, LevelProject, Scope{Org: org, Project: web}, nil) {
		t.Fatal("a prod created later escaped except prod")
	}
	// Deleting the excepted environment leaves the project without prod, so
	// the rule reaches the project-wide atom again, unconfirmed. Id binding
	// did the same: deletion pruned the except item and left "all, except
	// nothing" (ADR D3).
	bound.Where = except.Where.BindEnvs(refs)
	if bound.Reaches(CapEdit, LevelProject, Scope{Org: org, Project: web}, nil) {
		t.Fatal("except prod reached a project that has prod")
	}
	// Bound within payments alone, the rule knows no web environment, so it
	// must reach nothing in web: unconfined, web's prod would be unknown and
	// "all, except prod" would reach it. EnvName and WithEnvNames keep the
	// confinement.
	within := except
	within.Where = except.Where.BindEnvsWithin(pay, refs[:2])
	if !within.Reaches(CapEdit, LevelEnv, Scope{Org: org, Project: pay, Env: "e2"}, nil) || within.Reaches(CapEdit, LevelEnv, Scope{Org: org, Project: pay, Env: "e1"}, nil) {
		t.Fatal("a rule bound within payments lost its payments reach")
	}
	if within.Reaches(CapEdit, LevelEnv, Scope{Org: org, Project: web, Env: "e3"}, nil) || within.Reaches(CapEdit, LevelProject, Scope{Org: org, Project: web}, nil) {
		t.Fatal("a rule bound within payments reached web")
	}
	if within.Where = within.Where.WithEnvNames(nil); within.Reaches(CapEdit, LevelEnv, Scope{Org: org, Project: web, Env: "e4"}, nil) {
		t.Fatal("WithEnvNames dropped the confinement")
	}
	if _, ok := within.Where.EnvName(web, "e3"); ok {
		t.Fatal("EnvName answered for a project outside the binding")
	}
	bound.Where = except.Where.BindEnvs(slices.DeleteFunc(slices.Clone(refs), func(e EnvRef) bool { return e.ID == "e3" }))
	if !bound.Reaches(CapEdit, LevelProject, Scope{Org: org, Project: web}, nil) {
		t.Fatal("except prod did not reach web once its prod was deleted")
	}
}

// A rule's listed size counts each name once per project, as the environment
// item a v1 response lists there (#1447).
func TestRuleListedItemsCountNames(t *testing.T) {
	w := Where{Projects: []ProjectID{"a", "b"}, EnvNames: []string{"dev", "prod"},
		Keys: map[ProjectID][]RuleKeyItem{"a": {{Folder: "db", IsFolder: true}}}}
	if got := w.ListedItems(); got != 2*(1+2)+1 {
		t.Fatalf("ListedItems = %d, want 7", got)
	}
	if got := AllProjectsExpansion(3, 2, 2); got != 3*(1+2+2) {
		t.Fatalf("AllProjectsExpansion = %d, want 15", got)
	}
}
