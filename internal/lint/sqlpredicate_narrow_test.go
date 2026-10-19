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

func TestTypedParameterNarrowingRetainsChainProof(t *testing.T) {
	rules := map[string]TableRule{"items": {Class: "project", Chain: []string{"org_id", "project_id"}}}
	prefix := "SELECT id FROM items WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND "
	for _, predicate := range []string{
		"CAST(sqlc.arg(max_ttl_seconds) AS BIGINT)>0",
		"CAST(sqlc.arg(limit) AS BIGINT) >= -1",
	} {
		if got := checkQuery("postgres", Query{Name: "typed_narrow", SQL: prefix + predicate}, rules); len(got) != 0 {
			t.Errorf("valid typed narrowing %q: %v", predicate, got)
		}
	}
	for _, sql := range []string{
		// A parameter-only predicate cannot stand in for a missing chain column.
		"SELECT id FROM items WHERE org_id=sqlc.arg(chain_org) AND CAST(sqlc.arg(chain_project) AS BIGINT)>0",
		"SELECT id FROM items WHERE org_id=sqlc.arg(chain_org) AND CAST(project_id AS BIGINT)=sqlc.arg(chain_project)",
		// This exception is closed over bind parameters, BIGINT and integers.
		prefix + "CAST(project_id AS BIGINT)>0",
		prefix + "CAST(evil(sqlc.arg(limit)) AS BIGINT)>0",
		prefix + "CAST((SELECT id FROM another_tenant) AS BIGINT)>0",
		prefix + "CAST(sqlc.arg(limit) AS TEXT)>0",
		prefix + "CAST(sqlc.arg(limit) AS BIGINT)>0 OR TRUE",
	} {
		if got := checkQuery("postgres", Query{Name: "bad_typed_narrow", SQL: sql}, rules); len(got) == 0 {
			t.Errorf("unproved typed predicate accepted: %s", sql)
		}
	}
}

func TestOccurrenceBatchPredicatesKeepCompleteChain(t *testing.T) {
	rules := map[string]TableRule{"secret_value_occurrences": {Class: "environment", Chain: []string{"org_id", "project_id"}}}
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			membership := "value_entry_id IN (sqlc.slice('value_entry_ids'))"
			if engine == "postgres" {
				membership = "value_entry_id = ANY(sqlc.arg(value_entry_ids)::text[])"
			}
			sql := "SELECT value_entry_id FROM secret_value_occurrences WHERE org_id=sqlc.arg(chain_org_id) AND project_id=sqlc.arg(chain_project_id) AND environment_id=sqlc.arg(chain_env_id) AND " + membership
			if got := checkQuery(engine, Query{Name: "occurrences", SQL: sql}, rules); len(got) != 0 {
				t.Fatal(got)
			}
			for name, bad := range map[string]string{
				"missing org":       strings.Replace(sql, "org_id=sqlc.arg(chain_org_id) AND ", "", 1),
				"missing project":   strings.Replace(sql, "project_id=sqlc.arg(chain_project_id) AND ", "", 1),
				"literal org":       strings.Replace(sql, "org_id=sqlc.arg(chain_org_id)", "org_id='foreign'", 1),
				"chain slice":       strings.Replace(sql, "project_id=sqlc.arg(chain_project_id)", "project_id IN (sqlc.slice('projects'))", 1),
				"chain array":       strings.Replace(sql, "project_id=sqlc.arg(chain_project_id)", "project_id=ANY(sqlc.arg(projects)::text[])", 1),
				"outer disjunction": sql + " OR TRUE",
				"nested read":       strings.Replace(sql, membership, "value_entry_id IN (SELECT value_entry_id FROM secret_value_occurrences)", 1),
			} {
				t.Run(name, func(t *testing.T) {
					if got := checkQuery(engine, Query{Name: "occurrences", SQL: bad}, rules); len(got) == 0 {
						t.Fatal("unproved batch accepted")
					}
				})
			}
		})
	}
}
