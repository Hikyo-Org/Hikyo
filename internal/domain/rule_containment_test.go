package domain

import "testing"

func TestRuleSelectorContainment(t *testing.T) {
	const project ProjectID = "project"
	selector := func(mode AxisMode, items ...RuleKeyItem) Where {
		return Where{Projects: []ProjectID{project}, EnvMode: AxisAll, KeyMode: mode, Keys: map[ProjectID][]RuleKeyItem{project: items}}
	}
	folder := func(path string) RuleKeyItem { return RuleKeyItem{IsFolder: true, Folder: path} }
	key := func(id string) RuleKeyItem { return RuleKeyItem{KeyID: id} }
	folders := map[string]string{"db-key": "db", "child-key": "db/child", "other-key": "other"}
	for _, tc := range []struct {
		name         string
		outer, inner Where
		want         bool
	}{
		{"root exception does not cover a named folder exception", selector(AxisAll, folder("db")), selector(AxisAll, folder("")), false},
		{"root exception allows a named folder", selector(AxisAll, folder("")), selector(AxisOnly, folder("db")), true},
		{"root exception does not exclude child key", selector(AxisAll, folder("")), selector(AxisOnly, key("child-key")), true},
		{"folder contains resolved key", selector(AxisOnly, folder("db")), selector(AxisOnly, key("db-key")), true},
		{"only folder does not contain subtree", selector(AxisOnly, folder("db")), selector(AxisOnly, key("child-key")), false},
		{"unresolved key refuses", selector(AxisOnly, folder("db")), selector(AxisOnly, key("missing")), false},
		{"except folder removes subtree", selector(AxisAll, folder("db")), selector(AxisOnly, key("child-key")), false},
		{"except key prevents delegating full folder", selector(AxisAll, key("db-key")), selector(AxisOnly, folder("db")), false},
		{"except subtree contained by except ancestor", selector(AxisAll, folder("db/child")), selector(AxisAll, folder("db")), true},
		{"except ancestor not contained by except subtree", selector(AxisAll, folder("db")), selector(AxisAll, folder("db/child")), false},
		{"finite folders cannot contain all future folders", selector(AxisOnly, folder("db")), selector(AxisAll), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.outer.ContainsWhereInProject(tc.inner, project, folders); got != tc.want {
				t.Fatalf("contains = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("future environments", func(t *testing.T) {
		outer := selector(AxisAll)
		outer.EnvMode = AxisOnly
		outer.Envs = map[ProjectID][]EnvID{project: {"dev"}}
		if outer.ContainsWhereInProject(selector(AxisAll), project, folders) {
			t.Fatal("finite environment selection contains all future environments")
		}
	})
}

func TestRuleSelectorUnionContainment(t *testing.T) {
	const project ProjectID = "project"
	all := Where{Projects: []ProjectID{project}, EnvMode: AxisAll, KeyMode: AxisAll}
	only := func(env EnvID, folder string) Where {
		return Where{Projects: []ProjectID{project}, EnvMode: AxisOnly, Envs: map[ProjectID][]EnvID{project: {env}}, KeyMode: AxisOnly, Keys: map[ProjectID][]RuleKeyItem{project: {{IsFolder: true, Folder: folder}}}}
	}
	t.Run("root exception cannot replace named folder exception", func(t *testing.T) {
		outer, target := all, all
		outer.Keys = map[ProjectID][]RuleKeyItem{project: {{IsFolder: true, Folder: "db"}}}
		target.Keys = map[ProjectID][]RuleKeyItem{project: {{IsFolder: true, Folder: ""}}}
		if WhereUnionContainsInProject([]Where{outer}, target, project, nil) {
			t.Fatal("root-only exception incorrectly removes db subtree from target")
		}
	})
	t.Run("root exception leaves named folders reachable", func(t *testing.T) {
		outer := all
		outer.Keys = map[ProjectID][]RuleKeyItem{project: {{IsFolder: true, Folder: ""}}}
		target := only("dev", "db")
		if !WhereUnionContainsInProject([]Where{outer}, target, project, nil) {
			t.Fatal("root-only exception incorrectly excludes named folder")
		}
	})
	t.Run("complementary environment exceptions include future environments", func(t *testing.T) {
		a, b := all, all
		a.Envs = map[ProjectID][]EnvID{project: {"dev"}}
		b.EnvMode = AxisOnly
		b.Envs = map[ProjectID][]EnvID{project: {"dev"}}
		if !WhereUnionContainsInProject([]Where{a, b}, all, project, nil) {
			t.Fatal("complementary env rules fail to cover universe")
		}
	})
	t.Run("complementary key exceptions include future folders", func(t *testing.T) {
		a, b := all, all
		a.Keys = map[ProjectID][]RuleKeyItem{project: {{KeyID: "key-a"}}}
		b.KeyMode = AxisOnly
		b.Keys = map[ProjectID][]RuleKeyItem{project: {{KeyID: "key-a"}}}
		if !WhereUnionContainsInProject([]Where{a, b}, all, project, map[string]string{"key-a": "db"}) {
			t.Fatal("complementary key rules fail to cover universe")
		}
	})
	t.Run("axes must not be independently unioned", func(t *testing.T) {
		target := only("dev", "db")
		target.Envs[project] = []EnvID{"dev", "prod"}
		target.Keys[project] = append(target.Keys[project], RuleKeyItem{IsFolder: true, Folder: "payments"})
		if WhereUnionContainsInProject([]Where{only("dev", "db"), only("prod", "payments")}, target, project, nil) {
			t.Fatal("diagonal rules incorrectly cover cross product")
		}
	})
	t.Run("exact folder cannot fill excluded descendants", func(t *testing.T) {
		a, b := all, all
		a.Keys = map[ProjectID][]RuleKeyItem{project: {{IsFolder: true, Folder: "db"}}}
		b.KeyMode = AxisOnly
		b.Keys = map[ProjectID][]RuleKeyItem{project: {{IsFolder: true, Folder: "db"}}}
		if WhereUnionContainsInProject([]Where{a, b}, all, project, nil) {
			t.Fatal("only db folder fills descendants excluded by except db")
		}
	})
}

// Containment is a security implication of the runtime Reaches predicate.
// Compare those separate implementations over selector combinations so root,
// descendant, key-id and exception semantics cannot diverge silently.
func TestRuleContainmentAgreesWithRuntimeReach(t *testing.T) {
	const project ProjectID = "project"
	folders := map[string]string{"root-key": "", "db-key": "db", "child-key": "db/child", "other-key": "other"}
	keys := []RuleKey{{ID: "root-key", Folder: ""}, {ID: "db-key", Folder: "db"}, {ID: "child-key", Folder: "db/child"}, {ID: "other-key", Folder: "other"}, {ID: "future-key", Folder: "db/future"}, {ID: "future-root", Folder: ""}, {ID: "future-folder", Folder: "future"}}
	items := []RuleKeyItem{{IsFolder: true, Folder: ""}, {IsFolder: true, Folder: "db"}, {IsFolder: true, Folder: "db/child"}, {KeyID: "root-key"}, {KeyID: "db-key"}}
	selections := [][]RuleKeyItem{nil}
	for _, item := range items {
		selections = append(selections, []RuleKeyItem{item})
	}
	selections = append(selections, []RuleKeyItem{items[0], items[1]})
	var selectors []Where
	for _, mode := range []AxisMode{AxisAll, AxisOnly} {
		for _, selected := range selections {
			if mode == AxisOnly && len(selected) == 0 {
				continue
			}
			for _, envMode := range []AxisMode{AxisAll, AxisOnly} {
				selectors = append(selectors, Where{Projects: []ProjectID{project}, EnvMode: envMode, Envs: map[ProjectID][]EnvID{project: {"dev"}}, KeyMode: mode, Keys: map[ProjectID][]RuleKeyItem{project: selected}})
			}
		}
	}
	reaches := func(where Where, env EnvID, key RuleKey) bool {
		return (Rule{Org: "org", Capability: CapEdit, Where: where}).Reaches(CapEdit, LevelEnv, Scope{Org: "org", Project: project, Env: env}, &key)
	}
	for i, outer := range selectors {
		for j, inner := range selectors {
			if outer.ContainsWhereInProject(inner, project, folders) {
				for _, env := range []EnvID{"dev", "prod", "future"} {
					for _, key := range keys {
						if reaches(inner, env, key) && !reaches(outer, env, key) {
							t.Fatalf("pair %d/%d widened runtime reach at %s/%+v", i, j, env, key)
						}
					}
				}
			}
			for k, other := range selectors {
				if !WhereUnionContainsInProject([]Where{outer, other}, inner, project, folders) {
					continue
				}
				for _, env := range []EnvID{"dev", "prod", "future"} {
					for _, key := range keys {
						if reaches(inner, env, key) && !reaches(outer, env, key) && !reaches(other, env, key) {
							t.Fatalf("union %d/%d/%d widened runtime reach at %s/%+v", i, k, j, env, key)
						}
					}
				}
			}
		}
	}
}
