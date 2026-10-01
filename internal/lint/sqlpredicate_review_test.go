package lint

import "testing"

func TestSQLPredicateRejectsReviewedScopeBypasses(t *testing.T) {
	rules := map[string]TableRule{
		"environments": {Class: "environment", Chain: []string{"org_id", "project_id"}},
		"profiles":     {Class: "instance"},
		"bindings":     {Class: "project", Chain: []string{"org_id", "project_id"}},
		"events":       {Class: "org", Chain: []string{"org_id"}},
	}
	for name, sql := range map[string]string{
		"union before membership": "SELECT secret FROM environments UNION SELECT id FROM profiles WHERE id IN (SELECT profile_id FROM bindings WHERE org_id=? AND project_id=?)",
		"literal where and limit": "SELECT ' WHERE org_id = ? AND project_id = ? AND name = ' FROM environments WHERE name = ' LIMIT '",
		"joined literal where":    "SELECT ' WHERE e.org_id = ? AND e.project_id = ? AND e.name = ' FROM environments e WHERE e.name = ' LIMIT '",
		"scalar insert":           "INSERT INTO environments (id,org_id,project_id,name) VALUES (?,?,?,(SELECT name FROM bindings LIMIT 1))",
		"scalar update":           "UPDATE environments SET name=(SELECT name FROM bindings LIMIT 1) WHERE org_id=? AND project_id=?",
		"update from":             "UPDATE environments SET name=b.name FROM bindings b WHERE org_id=? AND project_id=?",
		"delete using":            "DELETE FROM environments USING bindings WHERE org_id=? AND project_id=?",
		"unbound source project":  "INSERT INTO environments (id,org_id,project_id) SELECT e.id,e.org_id,e.project_id FROM events e WHERE e.org_id=sqlc.arg(chain_org)",
	} {
		t.Run(name, func(t *testing.T) {
			if got := checkQuery("sqlite", Query{Name: "bypass", SQL: sql}, rules); len(got) == 0 {
				t.Fatal("unproved access accepted")
			}
		})
	}
	if got := checkQuery("sqlite", Query{Name: "selected-column", SQL: "UPDATE environments SET selected_repository_ids=? WHERE org_id=? AND project_id=?"}, rules); len(got) != 0 {
		t.Fatal("column name mistaken for a nested SELECT: ", got)
	}
}
