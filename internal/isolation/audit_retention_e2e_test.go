package isolation

import (
	"context"
	"errors"
	"fmt"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func seedAuditExpiry(t *testing.T, db *store.DB, trail, id, typ, correlation string, at time.Time) {
	t.Helper()
	table := "audit_" + trail + "_events"
	columns, values := "", ""
	if trail == "tenant" {
		columns = ",scope_class,org_id"
		values = ",'org','org_a'"
	}
	corr := "NULL"
	if correlation != "" {
		corr = "'" + correlation + "'"
	}
	asserted := "0"
	if db.Engine() == store.EnginePostgres {
		asserted = "false"
	}
	execRaw(t, db, fmt.Sprintf("INSERT INTO %s (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_class,outcome,origin,payload,correlation_id%s) VALUES ('%s','%s',1,'%s',%s,'%s','system','success','system','{}',%s%s)", table, columns, id, typ, audit.FormatTime(at), asserted, audit.FormatTime(at), corr, values))
	// PostgreSQL stamps insertion time itself. Aging is a fixture-only write.
	execRaw(t, db, fmt.Sprintf("UPDATE %s SET recorded_at='%s' WHERE id='%s'", table, audit.FormatTime(at), id))
}

func TestAuditRetentionExpiry(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := store.CanonTime(time.Now())
		svc := &service.Retention{DB: db, Now: func() time.Time { return now }}
		for _, trail := range []string{"tenant", "instance"} {
			seedAuditExpiry(t, db, trail, trail+"-access-old", "grant.denied", "", now.Add(-91*24*time.Hour))
			seedAuditExpiry(t, db, trail, trail+"-access-boundary", "grant.denied", "", now.Add(-90*24*time.Hour))
			seedAuditExpiry(t, db, trail, trail+"-security-keep", "auth.login", "", now.Add(-100*24*time.Hour))
			seedAuditExpiry(t, db, trail, trail+"-security-old", "auth.login", "", now.Add(-366*24*time.Hour))
		}
		seedAuditExpiry(t, db, "tenant", "unit-keep", "identity.delivery_fetched", "", now.Add(-91*24*time.Hour))
		seedAuditExpiry(t, db, "tenant", "key-new", "identity.disclosure", "unit-keep", now.Add(-89*24*time.Hour))
		seedAuditExpiry(t, db, "tenant", "unit-delete", "identity.delivery_fetched", "", now.Add(-91*24*time.Hour))
		seedAuditExpiry(t, db, "tenant", "key-expired", "identity.disclosure", "unit-delete", now.Add(-91*24*time.Hour))
		if _, err := svc.Sweep(t.Context()); err != nil {
			t.Fatal(err)
		}
		for _, trail := range []string{"tenant", "instance"} {
			if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_"+trail+"_events WHERE id IN ('"+trail+"-access-old','"+trail+"-security-old')"); n != 0 {
				t.Fatalf("%s expired rows=%d", trail, n)
			}
			if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_"+trail+"_events WHERE id IN ('"+trail+"-access-boundary','"+trail+"-security-keep')"); n != 2 {
				t.Fatalf("%s retained rows=%d", trail, n)
			}
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE correlation_id='unit-keep' OR id='unit-keep'"); n != 2 {
			t.Fatalf("split retained unit: %d", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE correlation_id='unit-delete' OR id='unit-delete'"); n != 0 {
			t.Fatalf("expired unit: %d", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='retention.audit_pruned'"); n != 5 {
			t.Fatalf("category receipts=%d want5", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='retention.audit_policy_changed'"); n != 1 {
			t.Fatalf("initial policy receipts=%d", n)
		}
		svc.AuditPolicy = store.AuditRetentionPolicy{AccessDays: 30, SecurityDays: 90}
		if _, err := svc.Sweep(t.Context()); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE correlation_id='unit-keep' OR id='unit-keep'"); n != 0 {
			t.Fatalf("shorter policy did not expire entire unit: %d", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='retention.audit_policy_changed'"); n != 2 {
			t.Fatalf("policy receipts=%d", n)
		}
	})
}

func TestAuditRetentionReceiptFailureRollsBack(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := store.CanonTime(time.Now())
		svc := &service.Retention{DB: db, Now: func() time.Time { return now }}
		if _, err := svc.Sweep(t.Context()); err != nil {
			t.Fatal(err)
		}
		seedAuditExpiry(t, db, "tenant", "rollback-expired", "grant.denied", "", now.Add(-91*24*time.Hour))
		if db.Engine() == store.EngineSQLite {
			execRaw(t, db, `CREATE TRIGGER refuse_prune_receipt BEFORE INSERT ON audit_instance_events WHEN NEW.type='retention.audit_pruned' BEGIN SELECT RAISE(ABORT, 'receipt refused'); END`)
		} else {
			execRaw(t, db, `CREATE FUNCTION refuse_prune_receipt() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN IF NEW.type='retention.audit_pruned' THEN RAISE EXCEPTION 'receipt refused'; END IF; RETURN NEW; END; $$; CREATE TRIGGER refuse_prune_receipt BEFORE INSERT ON audit_instance_events FOR EACH ROW EXECUTE FUNCTION refuse_prune_receipt()`)
		}
		if _, err := svc.Sweep(t.Context()); err == nil {
			t.Fatal("receipt failure accepted")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE id='rollback-expired'"); n != 1 {
			t.Fatal("deletion escaped rollback")
		}
	})
}

func TestAuditRetentionRejectsUnprovedDelete(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		err := tx.Write(t.Context(), db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			_, err := r.Retention().PruneAudit(ctx, nil, time.Now(), time.Now())
			return err
		})
		if err == nil {
			t.Fatal("proof-free pruning allowed")
		}
	})
}

// Trigger retention after the first committed export page has crossed the
// sink boundary. A later page must refuse a now-incomplete snapshot.
type auditRetentionSink struct {
	prune  func() error
	called bool
}

func (w *auditRetentionSink) Write(p []byte) (int, error) {
	if !w.called {
		w.called = true
		if err := w.prune(); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}
func TestAuditRetentionConcurrentExportFailsExplicitly(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := store.CanonTime(time.Now())
		for i := 0; i < 3; i++ {
			seedAuditExpiry(t, db, "tenant", fmt.Sprintf("export-expired-%d", i), "grant.denied", "", now.Add(-91*24*time.Hour))
		}
		svc := &service.Retention{DB: db, Now: func() time.Time { return now }}
		sink := &auditRetentionSink{prune: func() error { _, err := svc.Sweep(t.Context()); return err }}
		err := (&service.Audits{DB: db}).Export(t.Context(), alice, domain.Scope{Org: orgA}, store.AuditFilter{}, 1, sink)
		if !errors.Is(err, store.ErrAuditRetentionChanged) {
			t.Fatalf("export error=%v", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='audit.export_completed' AND outcome='failure'"); n != 1 {
			t.Fatalf("export failure receipts=%d", n)
		}
	})
}

func TestAuditRetentionDrainsMultipleBatches(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := store.CanonTime(time.Now())
		for i := 0; i < 205; i++ {
			seedAuditExpiry(t, db, "tenant", fmt.Sprintf("batch-expired-%d", i), "grant.denied", "", now.Add(-91*24*time.Hour))
		}
		svc := &service.Retention{DB: db, Now: func() time.Time { return now }}
		if _, err := svc.Sweep(t.Context()); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE id LIKE 'batch-expired-%'"); n != 0 {
			t.Fatalf("backlog=%d", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='retention.audit_pruned'"); n != 3 {
			t.Fatalf("batch receipts=%d", n)
		}
	})
}

func TestAuditRetentionPolicyFailurePreventsDeletion(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := store.CanonTime(time.Now())
		seedAuditExpiry(t, db, "tenant", "policy-failure-expired", "grant.denied", "", now.Add(-91*24*time.Hour))
		if db.Engine() == store.EngineSQLite {
			execRaw(t, db, `CREATE TRIGGER refuse_policy_receipt BEFORE INSERT ON audit_instance_events WHEN NEW.type='retention.audit_policy_changed' BEGIN SELECT RAISE(ABORT, 'policy receipt refused'); END`)
		} else {
			execRaw(t, db, `CREATE FUNCTION refuse_policy_receipt() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN IF NEW.type='retention.audit_policy_changed' THEN RAISE EXCEPTION 'policy receipt refused'; END IF; RETURN NEW; END; $$; CREATE TRIGGER refuse_policy_receipt BEFORE INSERT ON audit_instance_events FOR EACH ROW EXECUTE FUNCTION refuse_policy_receipt()`)
		}
		svc := &service.Retention{DB: db, Now: func() time.Time { return now }}
		if _, err := svc.Sweep(t.Context()); err == nil {
			t.Fatal("policy receipt failure accepted")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE id='policy-failure-expired'"); n != 1 {
			t.Fatal("deletion began without durable policy receipt")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_retention_policy"); n != 0 {
			t.Fatal("policy survived receipt rollback")
		}
	})
}

func TestAuditRetentionSnapshotWaitsForPruneReceiptCommit(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := store.CanonTime(time.Now())
		seedAuditExpiry(t, db, "tenant", "pending-prune-victim", "grant.denied", "", now.Add(-91*24*time.Hour))
		// Hold an audited deletion after its receipt has acquired recorded_at but
		// before commit. This is exactly the old timestamp-only guard's blind spot.
		var commit func() error
		if db.Engine() == store.EnginePostgres {
			pending, err := db.PG().Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer pending.Rollback(context.Background())
			_, err = pending.Exec(t.Context(), `DELETE FROM audit_tenant_events WHERE id='pending-prune-victim'; INSERT INTO audit_instance_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_class,outcome,origin,payload) VALUES ('held-prune-receipt','retention.audit_pruned',1,clock_timestamp(),false,clock_timestamp(),'system','success','system','{}')`)
			if err != nil {
				t.Fatal(err)
			}
			commit = func() error { return pending.Commit(t.Context()) }
		} else {
			pending, err := db.BeginSQLite(t.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			defer pending.Rollback()
			_, err = pending.ExecContext(t.Context(), fmt.Sprintf(`DELETE FROM audit_tenant_events WHERE id='pending-prune-victim'; INSERT INTO audit_instance_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_class,outcome,origin,payload) VALUES ('held-prune-receipt','retention.audit_pruned',1,'%s',0,'%s','system','success','system','{}')`, audit.FormatTime(now), audit.FormatTime(now)))
			if err != nil {
				t.Fatal(err)
			}
			commit = pending.Commit
		}
		type result struct {
			at  time.Time
			err error
		}
		done := make(chan result, 1)
		go func() { at, err := db.AuditExportSnapshotTime(t.Context()); done <- result{at, err} }()
		select {
		case got := <-done:
			t.Fatalf("snapshot escaped uncommitted prune: %+v", got)
		case <-time.After(100 * time.Millisecond):
		}
		if err := commit(); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-done:
			if got.err != nil {
				t.Fatal(got.err)
			}
			if !got.at.After(now) {
				t.Fatalf("cutoff=%s before receipt %s", got.at, now)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("snapshot remained blocked after prune commit")
		}
	})
}
