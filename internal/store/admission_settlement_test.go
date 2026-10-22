package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"modernc.org/sqlite"
)

func TestSQLiteSettlementPoolsAndErrors(t *testing.T) {
	cfg := ownedAdmissionConfig(t, EngineSQLite)
	db, err := admittedStoreFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, readonly := range []bool{false, true} {
		t.Run(fmt.Sprintf("read=%t/deadline", readonly), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			tx, err := db.BeginSQLite(ctx, readonly)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			<-ctx.Done()
			if err := tx.Commit(); !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, sql.ErrTxDone) {
				t.Fatalf("expired commit: %v", err)
			}
			if db.sqWrite.Stats().InUse != 0 || db.sqRead.Stats().InUse != 0 {
				t.Fatal("expired transaction retained a connection")
			}
			assertSQLiteHostLock(t, cfg.Path, true)
		})
		for _, commit := range []bool{false, true} {
			for _, canceled := range []bool{false, true} {
				t.Run(fmt.Sprintf("read=%t/commit=%t/canceled=%t", readonly, commit, canceled), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					tx, err := db.BeginSQLite(ctx, readonly)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback()
					pool, other := db.sqWrite, db.sqRead
					if readonly {
						pool, other = other, pool
					}
					if pool.Stats().InUse != 1 || other.Stats().InUse != 0 {
						t.Fatal("transaction did not retain its correct pool connection")
					}
					if canceled {
						cancel()
					}
					if commit {
						err = tx.Commit()
					} else {
						err = tx.Rollback()
					}
					if canceled {
						if commit && !errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrTxDone) {
							t.Fatalf("canceled commit: %v", err)
						}
						if !commit && err != nil && !errors.Is(err, sql.ErrTxDone) {
							t.Fatalf("canceled rollback: %v", err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
					if pool.Stats().InUse != 0 {
						t.Fatal("finalizer retained a pool connection")
					}
					if err := tx.Rollback(); !errors.Is(err, sql.ErrTxDone) {
						t.Fatalf("repeated rollback: %v", err)
					}
					if err := tx.Commit(); !errors.Is(err, sql.ErrTxDone) {
						t.Fatalf("repeated commit: %v", err)
					}
					assertSQLiteHostLock(t, cfg.Path, true)
				})
			}
		}
	}
}

func TestSQLiteConcurrentSettlementJoinsNativeCommit(t *testing.T) {
	cfg := ownedAdmissionConfig(t, EngineSQLite)
	db, err := admittedStoreFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	entered, release := sqliteSettlementGate(t, db, true)
	defer release()
	tx, err := db.BeginSQLite(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { release(); _ = tx.Rollback() }()
	if _, err := tx.ExecContext(t.Context(), "CREATE TABLE settlement_probe(value TEXT)"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	committed := make(chan error, 1)
	go func() { committed <- tx.Commit() }()
	// Install the join before waiting for the hook, including timeout paths.
	defer func() {
		release()
		select {
		case err := <-committed:
			if err != nil {
				t.Errorf("native commit: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("commit did not return after native settlement")
		}
	}()
	awaitSettlementSignal(t, entered)
	rolledBack := make(chan error, 1)
	go func() { rolledBack <- tx.Rollback() }()
	defer func() {
		release()
		select {
		case err := <-rolledBack:
			if !errors.Is(err, sql.ErrTxDone) {
				t.Errorf("losing rollback: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("rollback did not join native commit")
		}
	}()
	select {
	case err := <-rolledBack:
		rolledBack <- err
		t.Fatalf("losing rollback returned during native commit: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	assertSQLiteHostLock(t, cfg.Path, false)
}

func TestSQLiteBeginFailuresReleaseOwnership(t *testing.T) {
	for _, failure := range []string{"acquire", "begin", "check"} {
		t.Run(failure, func(t *testing.T) {
			cfg := ownedAdmissionConfig(t, EngineSQLite)
			db, err := admittedStoreFixture(t, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			switch failure {
			case "acquire":
				if err := db.sqWrite.Close(); err != nil {
					t.Fatal(err)
				}
			case "begin":
				// Occupy SQLite's native transaction without a sql.Tx owner so the
				// next BeginTx fails on this same single-writer connection.
				if _, err := db.sqWrite.ExecContext(t.Context(), "BEGIN"); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := db.sqWrite.ExecContext(t.Context(), "ROLLBACK"); err != nil {
						t.Error(err)
					}
				}()
			case "check":
				// Preserve a valid host/file admission but make the snapshot query
				// fail inside the newly begun transaction.
				if _, err := db.sqWrite.ExecContext(t.Context(), "ALTER TABLE upgrade_control RENAME TO unavailable_control"); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if tx, err := db.BeginSQLite(ctx, false); err == nil {
				_ = tx.Rollback()
				t.Fatal("injected begin failure was accepted")
			}
			if db.sqWrite.Stats().InUse != 0 {
				t.Fatal("failed begin leaked its connection")
			}
			assertSQLiteHostLock(t, cfg.Path, true)
		})
	}
}

// Gate the actual SQLite operation, after database/sql has marked the Tx done.
// A second sql.Tx finalizer alone cannot join this operation.
func TestSQLiteCanceledSettlementRetainsAdmission(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprintf("commit=%t", commit), func(t *testing.T) {
			cfg := ownedAdmissionConfig(t, EngineSQLite)
			db, err := admittedStoreFixture(t, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			entered, release := sqliteSettlementGate(t, db, false)
			defer release()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			tx, err := db.BeginSQLite(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { cancel(); release(); _ = tx.Rollback() }()
			if _, err := tx.ExecContext(ctx, "CREATE TABLE settlement_probe(value TEXT)"); err != nil {
				release()
				_ = tx.Rollback()
				t.Fatal(err)
			}
			cancel()
			awaitSettlementSignal(t, entered)
			results := make(chan error, 8)
			for range cap(results) {
				go func() {
					if commit {
						results <- tx.Commit()
					} else {
						results <- tx.Rollback()
					}
				}()
			}
			// Always release the native hook and join every finalizer on failure.
			defer func() {
				release()
				for range cap(results) {
					select {
					case err := <-results:
						if !errors.Is(err, sql.ErrTxDone) {
							t.Errorf("cancel-winning finalizer: %v", err)
						}
					case <-time.After(5 * time.Second):
						t.Error("finalizer did not join native settlement")
					}
				}
				if got := db.sqWrite.Stats().InUse; got != 0 {
					t.Errorf("settled write connections in use: %d", got)
				}
				assertSQLiteHostLock(t, cfg.Path, true)
			}()
			select {
			case err := <-results:
				results <- err // Preserve the result for the unconditional join.
				t.Fatalf("finalizer returned during native rollback: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			assertSQLiteHostLock(t, cfg.Path, false)
		})
	}
}

func sqliteSettlementGate(t *testing.T, db *DB, commit bool) (<-chan struct{}, func()) {
	t.Helper()
	conn, err := db.sqWrite.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	entered, released := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	gate := func() {
		enterOnce.Do(func() { close(entered) })
		<-released
	}
	if err := conn.Raw(func(raw any) error {
		hooks, ok := raw.(sqlite.HookRegisterer)
		if !ok {
			return errors.New("SQLite connection has no settlement hooks")
		}
		if commit {
			hooks.RegisterCommitHook(func() int32 { gate(); return 0 })
		} else {
			hooks.RegisterRollbackHook(gate)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return entered, func() { releaseOnce.Do(func() { close(released) }) }
}

func awaitSettlementSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("native settlement gate was not reached")
	}
}

func assertSQLiteHostLock(t *testing.T, path string, available bool) {
	t.Helper()
	lock := flock.New(path + ".lock")
	held, err := lock.TryLock()
	if err != nil {
		t.Fatal(err)
	}
	if held {
		if err := lock.Unlock(); err != nil {
			t.Fatal(err)
		}
	}
	if held != available {
		t.Errorf("exclusive host lock available = %t, want %t", held, available)
	}
}
