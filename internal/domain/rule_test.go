package domain

import "testing"

func TestRuleReachesFailsClosed(t *testing.T) {
	const org, p1, p2 = OrgID("org"), ProjectID("p1"), ProjectID("p2")
	dev := Scope{Org: org, Project: p1, Env: "dev"}
	prod := Scope{Org: org, Project: p1, Env: "prod"}
	db := &RuleKey{ID: "k1", Folder: "db"}
	stripe := &RuleKey{ID: "k2", Folder: "stripe"}
	newInDB := &RuleKey{Folder: "db"}

	reveal := Rule{ID: "r", Capability: CapReveal, Org: org, Where: Where{
		Projects: []ProjectID{p1}, EnvMode: AxisAll, Envs: map[ProjectID][]EnvID{p1: {"prod"}},
		KeyMode: AxisOnly, Keys: map[ProjectID][]RuleKeyItem{p1: {{Folder: "db", IsFolder: true}}},
	}}
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
		"read by key":          base(CapRead, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisAll, Keys: keys}),
		"pin by key":           base(CapPin, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisOnly, Keys: keys}),
		"settings by env":      base(CapProjectSettings, Where{Projects: []ProjectID{"p"}, EnvMode: AxisOnly, Envs: map[ProjectID][]EnvID{"p": {"e"}}, KeyMode: AxisAll}),
		"manage-projects":      base(CapManageProjects, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisAll}),
		"instance atom":        base(CapInstanceConfig, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisAll}),
		"empty only envs":      base(CapEdit, Where{Projects: []ProjectID{"p"}, EnvMode: AxisOnly, KeyMode: AxisAll}),
		"empty only keys":      base(CapEdit, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisOnly}),
		"no projects":          base(CapEdit, Where{EnvMode: AxisAll, KeyMode: AxisAll}),
		"item outside project": base(CapEdit, Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisAll, Keys: map[ProjectID][]RuleKeyItem{"q": {{KeyID: "k"}}}}),
		"unknown mode":         base(CapEdit, Where{Projects: []ProjectID{"p"}, EnvMode: "some", KeyMode: AxisAll}),
		"no org":               {ID: "r", Capability: CapEdit, Where: Where{Projects: []ProjectID{"p"}, EnvMode: AxisAll, KeyMode: AxisAll}},
	} {
		if r.Validate() == nil {
			t.Errorf("%s: validated", name)
		}
	}
}
