package lint

import (
	"strings"
	"testing"
)

func TestTenantStateLiteralsNeverSupplyChainBindings(t *testing.T) {
	rules := map[string]TableRule{
		"items": {Class: "project", Chain: []string{"org_id", "project_id"}},
	}
	for _, predicate := range []string{"state = 'issued'", "state = 'it''s-issued'", "attempt_count > 0", "enabled = TRUE"} {
		t.Run(predicate, func(t *testing.T) {
			q := Query{Name: "ScopedState", SQL: "SELECT id FROM items WHERE org_id = sqlc.arg(chain_org) AND project_id = sqlc.arg(chain_project) AND " + predicate}
			if findings := checkQuery("sqlite", q, rules); len(findings) != 0 {
				t.Fatalf("bound chain with narrowing literal refused: %v", findings)
			}
		})
	}
	for _, predicate := range []string{"org_id = 'tenant-a' AND project_id = ?", "org_id = ? AND project_id = 1", "org_id = 'tenant-a' OR state = 'issued'", "org_id = ? AND state = 'issued'"} {
		t.Run(predicate, func(t *testing.T) {
			q := Query{Name: "LiteralChain", SQL: "SELECT id FROM items WHERE " + predicate}
			if findings := checkQuery("sqlite", q, rules); len(findings) == 0 {
				t.Fatal("literal, missing or disjunctive chain escaped guard")
			}
		})
	}
	q := Query{Name: "MissingChain", SQL: "SELECT id FROM items WHERE org_id = ? AND state = 'issued'"}
	if findings := checkQuery("sqlite", q, rules); len(findings) != 1 || !strings.Contains(findings[0], "project_id") {
		t.Fatalf("state literal hid missing project binding: %v", findings)
	}
}
