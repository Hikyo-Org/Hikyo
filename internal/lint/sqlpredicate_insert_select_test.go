package lint

import (
	"strings"
	"testing"
)

func TestInsertSelectRetainsSourceAndTargetChainProof(t *testing.T) {
	rules := map[string]TableRule{"certificates": {Class: "environment", Chain: []string{"org_id", "project_id"}}, "keys": {Class: "environment", Chain: []string{"org_id", "project_id"}}, "profiles": {Class: "environment", Chain: []string{"org_id", "project_id"}}}
	sql := "INSERT INTO certificates (id,org_id,project_id,environment_id,key_id,profile_id) SELECT sqlc.arg(id),k.org_id,k.project_id,k.environment_id,k.id,p.id FROM keys k JOIN profiles p ON p.org_id=k.org_id AND p.project_id=k.project_id AND p.environment_id=k.environment_id WHERE k.id=sqlc.arg(key_id) AND p.id=sqlc.arg(profile_id) AND k.state='active' AND p.state='enabled' AND k.org_id=sqlc.arg(chain_org) AND k.project_id=sqlc.arg(chain_project) AND k.environment_id=sqlc.arg(chain_env)"
	if got := checkQuery("sqlite", Query{Name: "insert", SQL: sql}, rules); len(got) != 0 {
		t.Fatal(got)
	}
	flat := "INSERT INTO certificates (id,org_id,project_id) SELECT sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project) FROM keys WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project)"
	if got := checkQuery("sqlite", Query{Name: "flat", SQL: flat}, rules); len(got) != 0 {
		t.Fatal(got)
	}
	if got := checkQuery("sqlite", Query{Name: "wrong source parameter", SQL: strings.Replace(flat, "WHERE org_id=sqlc.arg(chain_org)", "WHERE org_id=sqlc.arg(other_org)", 1)}, rules); len(got) == 0 {
		t.Fatal("unrelated source scope parameter accepted")
	}
	for name, bad := range map[string]string{
		"wrong target column":         strings.Replace(sql, "sqlc.arg(id),k.org_id", "sqlc.arg(id),k.id", 1),
		"literal target scope":        strings.Replace(sql, "sqlc.arg(id),k.org_id", "sqlc.arg(id),'other'", 1),
		"missing source scope":        strings.Replace(sql, " AND k.project_id=sqlc.arg(chain_project)", "", 1),
		"missing joined source scope": strings.Replace(sql, " AND p.project_id=k.project_id", "", 1),
		"missing target chain":        strings.Replace(sql, "id,org_id,project_id", "id,org_id,unrelated", 1),
		"disjunctive source":          strings.Replace(sql, " AND k.org_id=sqlc.arg(chain_org)", " OR k.org_id=sqlc.arg(chain_org)", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if got := checkQuery("sqlite", Query{Name: "bad", SQL: bad}, rules); len(got) == 0 {
				t.Fatal("unproved insert accepted")
			}
		})
	}
}
