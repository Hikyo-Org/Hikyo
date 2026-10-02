package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type authorityProbeModule struct {
	fanoutModule
	calls int
}

func (m *authorityProbeModule) Sync(context.Context, adapter.SyncRequest, adapter.Journal) (adapter.SyncResult, error) {
	m.calls++
	return adapter.SyncResult{}, nil
}

func TestAdapterProductionAuthorityTimeoutRetriesButRevocationTerminates(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedDeadlineTargets(t, db)
		runtime := generatedAdapterRuntime(db)
		var release func()
		if db.Engine() == store.EngineSQLite {
			// Block the actual authorizer's admitted reader, not the durable
			// outbox writer. tx.Read's production deadline releases this wait.
			db.SQLiteRead().SetMaxOpenConns(1)
			conn, err := db.SQLiteRead().Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			release = func() { _ = conn.Close() }
		} else {
			lock, err := db.PG().Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := lock.Exec(t.Context(), `LOCK TABLE grants IN ACCESS EXCLUSIVE MODE`); err != nil {
				t.Fatal(err)
			}
			release = func() { _ = lock.Rollback(t.Context()) }
		}
		t.Cleanup(release)
		module := &authorityProbeModule{}
		worker := adapter.Worker{Store: runtime, Loader: deadlineRuntimeLoader{module: module}, ID: "authority_worker", Jitter: func(d time.Duration) time.Duration { return d }}
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
			t.Fatalf("authority datastore timeout = %v, %v", worked, err)
		}
		release()
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE id='deadline_a' AND state='queued' AND attempt_count=1`); got != 1 || module.calls != 0 {
			t.Fatalf("authority timeout discarded the job or executed provider work: queued=%d calls=%d", got, module.calls)
		}
		// An actual revoked grant remains a terminal refusal, rather than an
		// infrastructure retry disguised as authorization success.
		execRaw(t, db, `DELETE FROM grants WHERE id='deadline_manage_a'`)
		execRaw(t, db, `UPDATE adapter_outbox SET next_attempt_at='2000-01-01T00:00:00.000000Z' WHERE id='deadline_a'`)
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
			t.Fatalf("revoked authority = %v, %v", worked, err)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE id='deadline_a' AND state='failed' AND attempt_count=2`); got != 1 || module.calls != 0 {
			t.Fatalf("revocation was not terminal: failed=%d calls=%d", got, module.calls)
		}
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil || module.calls != 1 {
			t.Fatalf("fresh authorized tenant = %v, %v; calls=%d", worked, err, module.calls)
		}
	})
}

func TestAdapterPostgresBlockedRetryHonorsParentShutdown(t *testing.T) {
	db := seededDB(t, openPostgres)
	seedDeadlineTargets(t, db)
	runtime := generatedAdapterRuntime(db)
	job, ok, err := runtime.ClaimDue(t.Context(), "blocked_retry", time.Now().UTC(), time.Now().UTC().Add(adapter.LeaseTime))
	if err != nil || !ok {
		t.Fatalf("ClaimDue() = %v, %v", ok, err)
	}
	lock, err := db.PG().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(t.Context())
	if _, err := lock.Exec(t.Context(), `SELECT id FROM adapter_outbox WHERE id='deadline_a' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := time.Now()
	err = runtime.Retry(ctx, job, time.Now().Add(adapter.RetryFloor), 1, nil, nil, context.DeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 3*time.Second {
		t.Fatalf("blocked actual durable retry did not cancel: elapsed=%s err=%v", time.Since(started), err)
	}
	if err := lock.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE id='deadline_a' AND state='running' AND lease_owner='blocked_retry'`); got != 1 {
		t.Fatal("canceled settlement altered another owner's durable claim")
	}
}
