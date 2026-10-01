package lint

import (
	"strings"
	"testing"
)

func TestJoinedReadRequiresEveryAliasChain(t *testing.T) {
	rules := map[string]TableRule{
		"providers": {Class: "project", Chain: []string{"org_id", "project_id"}},
		"leases":    {Class: "environment", Chain: []string{"org_id", "project_id", "environment_id"}},
	}
	query := "SELECT p.kind FROM providers p JOIN leases l ON l.provider_id=p.id AND l.org_id=p.org_id AND l.project_id=p.project_id WHERE l.id=sqlc.arg(id) AND l.org_id=sqlc.arg(chain_org) AND l.project_id=sqlc.arg(chain_project) AND l.environment_id=sqlc.arg(chain_env) AND l.state='active' ORDER BY l.id LIMIT 1"
	if got := checkQuery("sqlite", Query{Name: "joined", SQL: query}, rules); len(got) != 0 {
		t.Fatalf("confined join refused: %v", got)
	}
	cases := map[string]string{
		"missing project equality": strings.Replace(query, " AND l.project_id=p.project_id", "", 1),
		"missing environment":      strings.Replace(query, " AND l.environment_id=sqlc.arg(chain_env)", "", 1),
		"literal chain":            strings.Replace(query, "l.org_id=sqlc.arg(chain_org)", "l.org_id='other'", 1),
		"range chain":              strings.Replace(query, "l.org_id=sqlc.arg(chain_org)", "l.org_id>sqlc.arg(chain_org)", 1),
		"cross column equality":    strings.Replace(query, "l.project_id=p.project_id", "l.project_id=p.org_id", 1),
		"outer join":               strings.Replace(query, "JOIN leases", "LEFT JOIN leases", 1),
		"unqualified scope":        strings.Replace(query, "l.org_id=sqlc.arg(chain_org)", "org_id=sqlc.arg(chain_org)", 1),
		"disjunction":              strings.Replace(query, " AND l.state='active'", " OR l.state='active'", 1),
		"unknown alias":            strings.Replace(query, "l.org_id=p.org_id", "l.org_id=x.org_id", 1),
		"on disjunction":           strings.Replace(query, "l.provider_id=p.id AND", "l.provider_id=p.id OR", 1),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if got := checkQuery("sqlite", Query{Name: "joined", SQL: bad}, rules); len(got) == 0 {
				t.Fatal("unconfined join accepted")
			}
		})
	}
}
