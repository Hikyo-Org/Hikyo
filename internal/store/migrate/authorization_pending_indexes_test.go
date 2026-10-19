package migrate

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestAuthorizationPendingIndexesSQLite(t *testing.T) {
	testAuthorizationPendingIndexes(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "indexes.db")})
}

func TestAuthorizationPendingIndexesPostgres(t *testing.T) {
	testAuthorizationPendingIndexes(t, postgresTestConfig(t, "authorization_pending_indexes"))
}

// Read the actual complete sqlc statement, including the temporary-grant arm
// and privacy/restore predicates. Only fixture arguments are substituted.
func indexFixtureQuery(t *testing.T, engine store.Engine, file, name string, args map[string]string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "queries", string(engine), file+".sql"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(contents), "-- name: "+name+" :many\n")
	if !ok {
		t.Fatalf("missing query %s", name)
	}
	query, _, ok := strings.Cut(rest, ";")
	if !ok {
		t.Fatal("unterminated query")
	}
	for name, value := range args {
		query = strings.ReplaceAll(query, "sqlc.arg("+name+")", value)
	}
	if strings.Contains(query, "sqlc.arg(") {
		t.Fatalf("unbound fixture argument: %s", query)
	}
	return query
}

func indexFixturePlan(t *testing.T, db *sql.DB, engine store.Engine, query string) string {
	t.Helper()
	prefix := "EXPLAIN (ANALYZE, BUFFERS) "
	if engine == store.EngineSQLite {
		prefix = "EXPLAIN QUERY PLAN "
	}
	rows, err := db.QueryContext(t.Context(), prefix+query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var detail string
		if engine == store.EngineSQLite {
			var id, parent, unused int
			err = rows.Scan(&id, &parent, &unused, &detail)
		} else {
			err = rows.Scan(&detail)
		}
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}

func testAuthorizationPendingIndexes(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 76); err != nil {
		t.Fatal(err)
	}
	db := migrationFixtureSQL(t, cfg)
	exec := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	// Respect service caps: 1,000 unique grants per org, 100 pending
	// changes and 100 keys per project. Within each project one owner has
	// 60 drafts, the paged owner has 10, and 30 other owners have one each.
	exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i < 999)
 INSERT INTO principals(id,kind,created_at) SELECT 'principal_' || i, 'human', '2026-01-01T00:00:00Z' FROM n`)
	exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i < 9)
 INSERT INTO orgs(id,name,active,metadata,created_at) SELECT 'org_' || i,'indexes_org_' || i,TRUE,'{}','2026-01-01T00:00:00Z' FROM n`)
	exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i < 9999)
 INSERT INTO grants(id,principal_id,capability,org_id,created_at)
 SELECT 'grant_' || i,'principal_' || (i % 1000),'read','org_' || (i / 1000),'2026-01-01T00:00:00Z' FROM n`)
	exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i < 99)
 INSERT INTO projects(id,org_id,name,created_at) SELECT 'project_' || i,'org_' || (i / 10),'indexes_project_' || i,'2026-01-01T00:00:00Z' FROM n`)
	exec(`INSERT INTO environments(id,org_id,project_id,name,note,display_order,created_at)
 SELECT 'env_' || substr(id,9),org_id,id,'indexes','',0,'2026-01-01T00:00:00Z' FROM projects`)
	exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i < 99)
 INSERT INTO keys(id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at)
 SELECT 'key_' || p.id || '_' || (10000+n.i),p.org_id,p.id,'KEY_' || n.i,'','config','',FALSE,'','{}','none','none','2026-01-01T00:00:00Z' FROM projects p CROSS JOIN n`)
	exec(`INSERT INTO pending_changes(id,org_id,project_id,environment_id,key_id,owner_id,operation,staged_from_revision,staged_from_entry,created_at)
 SELECT 'pending_' || k.id,k.org_id,k.project_id,e.id,k.id,
 CASE WHEN CAST(substr(k.name,5) AS INTEGER) % 10 = 0 THEN 'principal_1'
      WHEN CAST(substr(k.name,5) AS INTEGER) % 10 BETWEEN 1 AND 3
      THEN 'principal_' || (2 + (CAST(substr(k.name,5) AS INTEGER) / 10) * 3 + CAST(substr(k.name,5) AS INTEGER) % 10 - 1)
      ELSE 'principal_0' END,'unset',0,'','2026-01-01T00:00:00Z'
 FROM keys k JOIN environments e ON e.org_id=k.org_id AND e.project_id=k.project_id`)
	for _, bounded := range []struct {
		query string
		want  int
	}{
		{"SELECT MAX(n) FROM (SELECT COUNT(*) AS n FROM grants GROUP BY org_id) AS counts", 1000},
		{"SELECT COUNT(*) FROM (SELECT principal_id,capability,org_id,project_id,env_id FROM grants GROUP BY principal_id,capability,org_id,project_id,env_id) AS unique_grants", 10000},
		{"SELECT MAX(n) FROM (SELECT COUNT(*) AS n FROM pending_changes GROUP BY org_id,project_id) AS counts", 100},
		{"SELECT MAX(n) FROM (SELECT COUNT(*) AS n FROM keys GROUP BY org_id,project_id) AS counts", 100},
	} {
		var got int
		if err := db.QueryRowContext(ctx, bounded.query).Scan(&got); err != nil || got != bounded.want {
			t.Fatalf("bounded fixture count=%d want=%d err=%v", got, bounded.want, err)
		}
	}
	grants := indexFixtureQuery(t, cfg.Engine, "authn", "ListGrantsForPrincipal", map[string]string{"principal_id": "'principal_1'", "now": "'2026-01-02T00:00:00Z'"})
	page := func(after string) string {
		return indexFixtureQuery(t, cfg.Engine, "revisions", "ListPendingChangesForOwnerInEnvironmentPage", map[string]string{
			"chain_org_id": "'org_0'", "chain_project_id": "'project_0'", "chain_env_id": "'env_0'", "owner_id": "'principal_1'", "after_key_id": "'" + after + "'", "page_limit": "3",
		})
	}
	for _, table := range []string{"grants", "principals", "pending_changes"} {
		exec("ANALYZE " + table)
	}
	for _, q := range []struct{ name, sql string }{{"grants", grants}, {"pending", page("")}} {
		t.Logf("before %s (10000 grants, 10000 drafts across 100 capped projects, 32 owners per project):\n%s", q.name, indexFixturePlan(t, db, cfg.Engine, q.sql))
	}
	if err := Run(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if max, err := MaxVersion(ctx, cfg); err != nil || max < 77 {
		t.Fatalf("embedded migration max=%d err=%v", max, err)
	}
	for _, table := range []string{"grants", "pending_changes"} {
		exec("ANALYZE " + table)
	}
	// Index installation and key order are migration invariants. Planner choice
	// is measured separately: PostgreSQL can retain the existing unique index
	// for a capped 100-draft project without invalidating the new access path.
	for _, index := range []struct{ name, table, columns string }{
		{"grants_principal", "grants", "(principal_id)"},
		{"pending_changes_owner_key", "pending_changes", "(org_id, project_id, environment_id, owner_id, key_id)"},
	} {
		query := fmt.Sprintf("SELECT sql FROM sqlite_schema WHERE type='index' AND name='%s' AND tbl_name='%s'", index.name, index.table)
		if cfg.Engine == store.EnginePostgres {
			query = fmt.Sprintf("SELECT indexdef FROM pg_indexes WHERE schemaname=current_schema() AND indexname='%s' AND tablename='%s'", index.name, index.table)
		}
		var definition string
		if err := db.QueryRowContext(ctx, query).Scan(&definition); err != nil {
			t.Fatal(err)
		}
		definition = strings.Join(strings.Fields(definition), " ")
		if !strings.HasPrefix(definition, "CREATE INDEX "+index.name+" ON ") || !strings.HasSuffix(definition, index.columns) {
			t.Fatalf("unexpected index definition: %s", definition)
		}
	}
	for _, q := range []struct{ name, sql, index string }{{"grants", grants, "grants_principal"}, {"pending", page(""), "pending_changes_owner_key"}} {
		plan := indexFixturePlan(t, db, cfg.Engine, q.sql)
		t.Logf("after %s:\n%s", q.name, plan)
		if cfg.Engine == store.EnginePostgres && q.name == "pending" {
			continue // Retain the natural plan; catalog and exact pagination are asserted.
		}
		if !regexp.MustCompile(`\b`+q.index+`\b`).MatchString(plan) || (q.name == "pending" && (strings.Contains(plan, "Sort") || strings.Contains(plan, "TEMP B-TREE") || !strings.Contains(plan, "owner_id") || !strings.Contains(plan, "key_id"))) {
			t.Fatalf("expected %s:\n%s", q.index, plan)
		}
	}
	var grantCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+grants+") AS resolved").Scan(&grantCount); err != nil || grantCount != 10 {
		t.Fatalf("resolved grants=%d, err=%v", grantCount, err)
	}
	// Walk all sparse-owner rows with an exclusive cursor, verifying exact
	// expected keys rather than just a count (duplicates and skips both fail).
	after, seen := "", 0
	for {
		rows, err := db.QueryContext(ctx, page(after))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			key := fmt.Sprint(values[4])
			want := fmt.Sprintf("key_project_0_%d", 10000+seen*10)
			if key != want || fmt.Sprint(values[5]) != "principal_1" {
				t.Fatalf("page row %d: %v, want %s for principal_1", seen, values, want)
			}
			after = key
			seen++
			count++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if count == 0 {
			break
		}
	}
	if seen != 10 {
		t.Fatalf("paged %d drafts, want 10", seen)
	}
	// The original uniqueness constraint still rejects a duplicate cell/owner.
	_, err := db.ExecContext(ctx, `INSERT INTO pending_changes(id,org_id,project_id,environment_id,key_id,owner_id,operation,staged_from_revision,staged_from_entry,created_at) SELECT 'duplicate',org_id,project_id,environment_id,key_id,owner_id,operation,staged_from_revision,staged_from_entry,created_at FROM pending_changes WHERE id='pending_key_project_0_10000'`)
	if err == nil {
		t.Fatal("duplicate owner/key accepted")
	}
	for _, predicate := range []string{"privacy_state='erased'", "reconciled_epoch=-1"} {
		exec("UPDATE principals SET " + predicate + " WHERE id='principal_1'")
		rows, err := db.QueryContext(ctx, grants)
		if err != nil {
			t.Fatal(err)
		}
		if rows.Next() {
			t.Fatal("inactive/unreconciled principal retained grants")
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		exec("UPDATE principals SET privacy_state='active', reconciled_epoch=0 WHERE id='principal_1'")
	}
}
