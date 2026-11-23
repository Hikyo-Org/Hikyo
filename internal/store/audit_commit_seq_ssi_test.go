package store

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// The deferred commit_seq assigners must look their row up by primary key only
// (#1351). Once the table is analyzed, a `commit_seq IS NULL` predicate is
// planned as a scan of the commit_seq unique index's NULL tail, which holds
// every org's in-flight rows and so takes SIRead locks that conflict across
// orgs under SERIALIZABLE.
func TestAuditCommitSeqAssignmentTakesNoCommitSeqIndexSIReadLock(t *testing.T) {
	cfg := ownedAdmissionConfig(t, EnginePostgres)
	db, err := admittedStoreFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	conn, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	cases := []struct{ table, extra, values string }{
		{"audit_tenant_events", ",scope_class,org_id", ",'org','org_a'"},
		{"audit_instance_events", "", ""},
	}
	for _, c := range cases {
		t.Run(c.table, func(t *testing.T) {
			insert := func(id string) string {
				return fmt.Sprintf(`INSERT INTO %s(id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_class,outcome,origin,payload%s)
SELECT %s,'t',1,now(),FALSE,now(),'system','success','system','{}'%s`, c.table, c.extra, id, c.values)
			}
			// Enough committed rows, all with commit_seq assigned, that the
			// planner estimates `commit_seq IS NULL` as nearly empty.
			if _, err := conn.Exec(t.Context(), insert(`'seed_'||g`)+` FROM generate_series(1,200000) g`); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(t.Context(), "VACUUM ANALYZE "+c.table); err != nil {
				t.Fatal(err)
			}
			// Positive control: the pre-fix predicate shape must still be planned
			// on the commit_seq unique index, or this test could pass vacuously.
			var plan strings.Builder
			planRows, err := conn.Query(t.Context(), "EXPLAIN UPDATE "+c.table+" SET commit_seq = 1 WHERE seq = 100 AND commit_seq IS NULL")
			if err != nil {
				t.Fatal(err)
			}
			for planRows.Next() {
				var line string
				if err = planRows.Scan(&line); err != nil {
					t.Fatal(err)
				}
				plan.WriteString(line + "\n")
			}
			planRows.Close()
			if err = planRows.Err(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(plan.String(), c.table+"_commit_seq_unique") {
				t.Fatalf("old predicate no longer plans on %s_commit_seq_unique; raise the seed size:\n%s", c.table, plan.String())
			}
			tx, err := conn.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err = tx.Exec(t.Context(), insert(`'probe'`)); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(t.Context(), "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
				t.Fatal(err)
			}
			rows, err := tx.Query(t.Context(), `SELECT l.locktype, l.relation::regclass::text FROM pg_locks l
WHERE l.mode = 'SIReadLock' AND l.pid = pg_backend_pid()
  AND l.relation IN ($1::regclass, $2::regclass, $3::regclass)`, c.table+"_commit_seq_unique", c.table, c.table+"_pkey")
			var pkeyLocks int
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var locktype, relation string
				if err = rows.Scan(&locktype, &relation); err != nil {
					t.Fatal(err)
				}
				if relation == c.table+"_pkey" {
					pkeyLocks++
					continue
				}
				if relation != c.table || locktype == "relation" {
					t.Errorf("commit_seq assignment holds %s SIReadLock on %s", locktype, relation)
				}
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			if pkeyLocks == 0 {
				t.Error("no SIRead lock on the primary key: SSI lock tracking is not live")
			}
			// Checked after reading pg_locks so this read adds no locks to the evidence.
			var assigned bool
			if err = tx.QueryRow(t.Context(), "SELECT commit_seq IS NOT NULL FROM "+c.table+" WHERE id = 'probe'").Scan(&assigned); err != nil || !assigned {
				t.Fatalf("probe row has no commit_seq after the assigner ran: %v", err)
			}
		})
	}
}
