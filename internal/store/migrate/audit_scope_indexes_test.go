package migrate

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestAuditScopeIndexesSQLite(t *testing.T) {
	testAuditScopeIndexes(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "audit.db")})
}

func TestAuditScopeIndexesPostgres(t *testing.T) {
	testAuditScopeIndexes(t, postgresTestConfig(t, "audit_indexes"))
}

func testAuditScopeIndexes(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 52); err != nil {
		t.Fatal(err)
	}
	db := migrationFixtureSQL(t, cfg)
	// Sparse scopes within a busy org expose scans of unrelated audit events.
	_, err := db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (
	    SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 10000
	) INSERT INTO audit_tenant_events
	    (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,
	     actor_class,scope_class,org_id,project_id,env_id,outcome,origin,payload)
	SELECT 'evt_' || i, 'grant.denied', 1, '2026-01-01T00:00:00Z', FALSE,
	    '2026-01-01T00:00:00Z', 'unauthenticated', 'env', 'org_test',
	    'project_' || (i % 100), 'env_' || (i % 1000), 'denied', 'api', '{}'
	FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE audit_tenant_events"); err != nil {
		t.Fatal(err)
	}
	if n := countUpgrade(t, cfg, "SELECT COUNT(*) FROM audit_tenant_events"); n != 10000 {
		t.Fatalf("migration changed audit count: %d", n)
	}
	for _, scope := range []struct{ name, predicate string }{
		{"project", "org_id = 'org_test' AND project_id = 'project_1'"},
		{"env", "org_id = 'org_test' AND project_id = 'project_1' AND env_id = 'env_1'"},
	} {
		for _, query := range []string{
			"SELECT seq, payload FROM audit_tenant_events WHERE " + scope.predicate +
				" AND seq > 0 AND recorded_at >= '2025-01-01T00:00:00Z' AND recorded_at <= '2027-01-01T00:00:00Z' ORDER BY seq LIMIT 10",
			"SELECT COALESCE(MAX(seq), 0) FROM audit_tenant_events WHERE " + scope.predicate,
		} {
			prefix := "EXPLAIN "
			if cfg.Engine == store.EngineSQLite {
				prefix = "EXPLAIN QUERY PLAN "
			}
			rows, err := db.QueryContext(ctx, prefix+query)
			if err != nil {
				t.Fatal(err)
			}
			var plan strings.Builder
			for rows.Next() {
				var detail string
				if cfg.Engine == store.EngineSQLite {
					var id, parent, unused int
					err = rows.Scan(&id, &parent, &unused, &detail)
				} else {
					err = rows.Scan(&detail)
				}
				if err != nil {
					rows.Close()
					t.Fatal(err)
				}
				fmt.Fprintln(&plan, detail)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				t.Fatal(err)
			}
			want := "audit_tenant_events_" + scope.name + "_seq"
			if !strings.Contains(plan.String(), want) || strings.Contains(plan.String(), "Sort") || strings.Contains(plan.String(), "TEMP B-TREE") {
				t.Fatalf("query must use ordered scope index %s:\n%s", want, plan.String())
			}
		}
	}
}
