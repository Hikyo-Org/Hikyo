package app

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func TestOwnerRuntimeJoinsCanceledSQLiteSettlement(t *testing.T) {
	testOwnerRuntimeJoinsCanceledSQLiteSettlement(t, false)
}

func TestOwnerRuntimeCanceledSQLiteSettlementCleanupWithoutRollbackHook(t *testing.T) {
	testOwnerRuntimeJoinsCanceledSQLiteSettlement(t, true)
}

func testOwnerRuntimeJoinsCanceledSQLiteSettlement(t *testing.T, skipRollbackHook bool) {
	t.Helper()
	_, srv, _ := deploymentAdapterFixture(t, false)
	conn, err := srv.db.SQLiteWrite().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), "CREATE TABLE cleanup_probe(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	rollbackEntered, releaseRollback := make(chan struct{}), make(chan struct{})
	var releaseOnce, enterOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRollback) }) }
	defer release()
	if err := conn.Raw(func(raw any) error {
		hooks, ok := raw.(sqlite.HookRegisterer)
		if !ok {
			return errors.New("SQLite connection has no rollback hook")
		}
		if !skipRollbackHook {
			hooks.RegisterRollbackHook(func() { enterOnce.Do(func() { close(rollbackEntered) }); <-releaseRollback })
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	started, finalizing, workerReturned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	abort := make(chan struct{})
	srv.owner.current.graph.scheduler.Jobs = []ScheduledJob{{Name: "cleanup-settlement", Run: func(ctx context.Context) error {
		defer close(workerReturned)
		transaction, err := srv.db.BeginSQLite(ctx, false)
		if err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, "INSERT INTO cleanup_probe VALUES ('pending')"); err != nil {
			_ = transaction.Rollback()
			return err
		}
		close(started)
		<-ctx.Done()
		select {
		case <-rollbackEntered:
		case <-abort:
			_ = transaction.Rollback()
			return ctx.Err()
		}
		close(finalizing)
		err = transaction.Rollback()
		if !errors.Is(err, sql.ErrTxDone) {
			return err
		}
		return ctx.Err()
	}}}
	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() { served <- srv.ServeWithReady(ctx, nil) }()
	// On every path, abort the hook wait and unblock native rollback before
	// joining the actual server.
	// Fixture cleanup and TempDir removal must run after this join.
	defer func() {
		close(abort)
		cancel()
		release()
		if err := <-served; err != nil {
			t.Errorf("serve after settlement: %v", err)
		}
		if srv.db.SQLiteWrite().Stats().InUse != 0 || srv.db.SQLiteRead().Stats().InUse != 0 {
			t.Error("serve returned with SQLite connections still in use")
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	if skipRollbackHook {
		// Exercise the same deferred teardown as a failed assertion when the
		// native hook never signals rollbackEntered.
		return
	}
	cancel()
	select {
	case <-rollbackEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("rollback did not start")
	}
	select {
	case <-finalizing:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not attempt settlement")
	}
	select {
	case err := <-served:
		served <- err // The unconditional cleanup still joins this result.
		t.Fatalf("serve returned before SQLite rollback settled: %v, write pool: %+v", err, srv.db.SQLiteWrite().Stats())
	case <-workerReturned:
		t.Fatal("worker returned before SQLite rollback settled")
	case <-time.After(100 * time.Millisecond):
	}
}
