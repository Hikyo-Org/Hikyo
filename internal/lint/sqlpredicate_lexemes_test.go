package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPredicateRejectsCommentAndIdentifierMaskBypasses(t *testing.T) {
	rules := map[string]TableRule{"environments": {Class: "environment", Chain: []string{"org_id", "project_id"}}}
	for name, body := range map[string]string{
		"line comment union":        "SELECT name FROM environments WHERE org_id=? AND project_id=? GROUP BY name --\nUNION SELECT secret FROM environments",
		"line comment upsert":       "INSERT INTO environments (id,org_id,project_id,name) VALUES (?,?,?,?) --\nON CONFLICT (id) DO UPDATE SET org_id=excluded.org_id",
		"indented literal union":    "SELECT name FROM environments WHERE org_id=? AND project_id=? AND name='a\n  -- ' UNION SELECT name FROM environments WHERE name='\n'",
		"indented literal upsert":   "INSERT INTO environments (id,org_id,project_id,name) VALUES (?,?,?,'a\n  -- ') ON CONFLICT (id) DO UPDATE SET org_id=excluded.org_id, name='\n'",
		"block comment union":       "SELECT name FROM environments WHERE org_id=? AND project_id=? GROUP BY name /* boundary */ UNION SELECT secret FROM environments",
		"dollar identifier":         "SELECT a$q$ FROM environments WHERE org_id=? AND project_id=? GROUP BY name UNION SELECT secret FROM environments WHERE name=$q$",
		"unicode dollar identifier": "SELECT é$q$ FROM environments WHERE org_id=? AND project_id=? GROUP BY name UNION SELECT secret FROM environments WHERE name=$q$",
		"backtick identifier":       "SELECT `a'` FROM environments WHERE name=' LIMIT '",
		"bracket identifier":        "SELECT [a'q$] FROM environments WHERE name=' LIMIT '",
	} {
		t.Run(name, func(t *testing.T) {
			// Exercise actual query parsing, including its whitespace flattening.
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "fixture.sql"), []byte("-- name: Bypass :one\n"+body+";\n"), 0600); err != nil {
				t.Fatal(err)
			}
			queries, err := ParseQueries(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(queries) != 1 {
				t.Fatal(queries)
			}
			for _, engine := range []string{"sqlite", "postgres"} {
				if got := checkQuery(engine, queries[0], rules); len(got) == 0 {
					t.Fatalf("%s masking bypass accepted", engine)
				}
			}
		})
	}
	for engine, sql := range map[string]string{
		"sqlite":   "SELECT '-- UNION /* marker */' AS name FROM environments WHERE org_id=? AND project_id=?",
		"postgres": "SELECT $$-- UNION /* marker */$$ AS name FROM environments WHERE org_id=$1 AND project_id=$2",
	} {
		if got := checkQuery(engine, Query{Name: "literal", SQL: sql}, rules); len(got) != 0 {
			t.Fatalf("%s harmless literal refused: %v", engine, got)
		}
	}
}
