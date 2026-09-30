package lint

import (
	"strings"
	"testing"
)

func TestNarrowConditionsNeverSatisfyChain(t *testing.T) {
	rules := map[string]TableRule{"items": {Class: "project", Chain: []string{"org_id", "project_id"}}}
	prefix := "SELECT id FROM items WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND "
	for _, predicate := range []string{
		"state NOT IN ('revoked','expired')",
		"id IN (sqlc.slice('ids'))",
		"id=ANY(sqlc.arg(ids)::text[])",
		"deleted_at IS NULL",
		"(state='active' OR (state='retiring' AND valid_before>sqlc.arg(now)))",
		"(CAST(sqlc.arg(chain_env) AS TEXT)='' OR environment_id=sqlc.arg(chain_env))",
	} {
		engine := "sqlite"
		if strings.Contains(predicate, "::text[]") {
			engine = "postgres"
		}
		if got := checkQuery(engine, Query{Name: "narrow", SQL: prefix + predicate}, rules); len(got) != 0 {
			t.Errorf("%s: %v", predicate, got)
		}
	}
	for _, sql := range []string{
		"SELECT id FROM items WHERE org_id=sqlc.arg(chain_org) AND (project_id IS NULL OR project_id=sqlc.arg(chain_project))",
		"SELECT id FROM items WHERE org_id=sqlc.arg(chain_org) AND project_id IN (sqlc.slice('projects'))",
		prefix + "state='active' OR state='revoked'",
		prefix + "id IN (SELECT id FROM another_tenant)",
		prefix + "(state='active' OR evil(sqlc.arg(id)))",
		prefix + "(state='active' OR project_id=sqlc.arg(chain_project))",
	} {
		if got := checkQuery("sqlite", Query{Name: "bad", SQL: sql}, rules); len(got) == 0 {
			t.Errorf("unproved predicate accepted: %s", sql)
		}
	}
}
