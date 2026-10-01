package lint

import (
	"strings"
	"testing"
)

func TestScopedMembershipPreservesBindingChain(t *testing.T) {
	rules := map[string]TableRule{"profiles": {Class: "instance"}, "bindings": {Class: "project", Chain: []string{"org_id", "project_id"}}}
	sql := "SELECT id,name FROM profiles WHERE name=sqlc.arg(name) AND id IN (SELECT profile_id FROM bindings WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND (environment_id IS NULL OR environment_id=sqlc.arg(chain_env))) ORDER BY name"
	if got := checkQuery("sqlite", Query{Name: "bound", SQL: sql}, rules); len(got) != 0 {
		t.Fatal(got)
	}
	cases := map[string]string{
		"missing chain":              strings.Replace(sql, "project_id=sqlc.arg(chain_project) AND ", "", 1),
		"literal chain":              strings.Replace(sql, "org_id=sqlc.arg(chain_org)", "org_id='other'", 1),
		"nullable chain":             strings.Replace(sql, "org_id=sqlc.arg(chain_org)", "(org_id IS NULL OR org_id=sqlc.arg(chain_org))", 1),
		"different nullable column":  strings.Replace(sql, "environment_id IS NULL", "project_id IS NULL", 1),
		"disjunction":                strings.Replace(sql, "org_id=sqlc.arg(chain_org) AND", "org_id=sqlc.arg(chain_org) OR", 1),
		"unbounded root disjunction": strings.Replace(sql, "name=sqlc.arg(name) AND", "name=sqlc.arg(name) OR", 1),
		"nested other read":          strings.Replace(sql, "SELECT profile_id", "SELECT (SELECT id FROM profiles)", 1),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if got := checkQuery("sqlite", Query{Name: "bound", SQL: bad}, rules); len(got) == 0 {
				t.Fatal("unconfined membership accepted")
			}
		})
	}
	rules["bindings"] = TableRule{Class: "environment", Chain: []string{"org_id", "project_id", "environment_id"}}
	if got := checkQuery("sqlite", Query{Name: "bound", SQL: sql}, rules); len(got) == 0 {
		t.Fatal("nullable owning-chain column accepted")
	}
}
