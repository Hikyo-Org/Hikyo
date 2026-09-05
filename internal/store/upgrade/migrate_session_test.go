package upgrade

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/pressly/goose/v3/lock"
)

//go:embed testdata/session-*/*.sql
var sessionMigrations embed.FS

func prepareSessionMigration(t *testing.T, s *Session, directory string) State {
	t.Helper()
	manifest, err := releaseidentity.BuildMigrationManifest(sessionMigrations, directory, s.engine)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	empty := emptyManifest(s.engine)
	op := operation(Source{Genesis: FreshGenesis}, empty)
	op.TargetMigrationDigest = digest
	state, err := s.Bootstrap(t.Context(), empty, op, Production)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestSessionMigrationsRequireDurableBoundaryAndExactBytes(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			state := prepareSessionMigration(t, s, "testdata/session-success")
			if err := s.ApplyMigrations(t.Context(), state, sessionMigrations, "testdata/session-success"); !errors.Is(err, ErrConflict) {
				return fmt.Errorf("prepared phase admitted: %v", err)
			}
			var err error
			state, err = s.Advance(t.Context(), state, SchemaWriteStarted)
			if err != nil {
				return err
			}
			if err := s.ApplyMigrations(t.Context(), state, sessionMigrations, "testdata/session-pg-cancel"); err == nil {
				return errors.New("different embedded SQL admitted")
			}
			var count int
			q := "SELECT count(*) FROM sqlite_schema WHERE name='goose_db_version'"
			if cfg.Engine == releaseidentity.Postgres {
				q = "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='goose_db_version'"
			}
			if err := s.conn.QueryRowContext(t.Context(), q).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return errors.New("refused migration initialized goose")
			}
			// A temporary table exists only on this physical connection. The actual
			// migration must read it, proving goose neither pooled nor reconnected.
			if _, err := s.conn.ExecContext(t.Context(), "CREATE TEMP TABLE migration_connection_marker(value TEXT NOT NULL); INSERT INTO migration_connection_marker VALUES ('owned-connection')"); err != nil {
				return err
			}
			if err := s.ApplyMigrations(t.Context(), state, sessionMigrations, "testdata/session-success"); err != nil {
				return err
			}
			var value string
			if err := s.conn.QueryRowContext(t.Context(), "SELECT value FROM migration_session_probe").Scan(&value); err != nil {
				return err
			}
			if value != "owned-connection" {
				return errors.New("migration ran on a different connection")
			}
			current, err := s.Read(t.Context())
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, state) {
				return errors.New("SQL execution advanced control phase without verification/health")
			}
			if cfg.Engine == releaseidentity.Postgres {
				other, err := open(cfg, false)
				if err != nil {
					return err
				}
				defer other.Close()
				var acquired bool
				if err := other.QueryRowContext(t.Context(), "SELECT pg_try_advisory_lock($1)", lock.DefaultLockID).Scan(&acquired); err != nil {
					return err
				}
				if acquired {
					return errors.New("goose cleanup released outer migration lock")
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestSessionMigrationCancellationKeepsPostWriteMarker(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		var expected State
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			directory := "testdata/session-sqlite-cancel"
			if cfg.Engine == releaseidentity.Postgres {
				directory = "testdata/session-pg-cancel"
			}
			state := prepareSessionMigration(t, s, directory)
			var err error
			expected, err = s.Advance(t.Context(), state, SchemaWriteStarted)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			finished := make(chan error, 1)
			go func() { finished <- s.ApplyMigrations(ctx, expected, sessionMigrations, directory) }()
			other, err := open(cfg, false)
			if err != nil {
				cancel()
				<-finished
				return err
			}
			defer other.Close()
			query := "SELECT count(*) FROM sqlite_schema WHERE name='migration_partial_probe'"
			if cfg.Engine == releaseidentity.Postgres {
				query = "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='migration_partial_probe'"
			}
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			poll := time.NewTicker(10 * time.Millisecond)
			defer poll.Stop()
			for {
				var count int
				if err := other.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
					cancel()
					<-finished
					return err
				}
				if count == 1 {
					break
				}
				select {
				case err := <-finished:
					return fmt.Errorf("migration ended before cancellation barrier: %v", err)
				case <-deadline.C:
					cancel()
					<-finished
					return errors.New("migration never committed its first statement")
				case <-poll.C:
				}
			}
			cancel()
			if err := <-finished; err == nil {
				return errors.New("cancelled migration reported success")
			}
			return nil
		})
		// A cancelled driver can discard the connection, making outer lock cleanup
		// fail too. Read the authoritative persisted outcome through a new session.
		if err != nil && cfg.Engine == releaseidentity.SQLite && !errors.Is(err, sql.ErrConnDone) {
			t.Fatal(err)
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			current, err := s.Read(t.Context())
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, expected) {
				return errors.New("cancelled migration advanced or cleared pending state")
			}
			var count int
			if err := s.conn.QueryRowContext(t.Context(), "SELECT count(*) FROM migration_partial_probe").Scan(&count); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestMigrationConnectorCannotOpenOrReopenPhysicalConnection(t *testing.T) {
	c := &migrationConnector{active: true}
	if _, err := c.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Connect(t.Context()); !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("second lease: %v", err)
	}
	if _, err := c.Driver().Open("ignored"); err == nil {
		t.Fatal("physical connection opening was allowed")
	}
}

func TestSessionMigrationBackendTerminationCannotReconnect(t *testing.T) {
	cfg := testConfig(t, releaseidentity.Postgres)
	var expected State
	err := WithLock(t.Context(), cfg, func(s *Session) error {
		state := prepareSessionMigration(t, s, "testdata/session-pg-cancel")
		var err error
		expected, err = s.Advance(t.Context(), state, SchemaWriteStarted)
		if err != nil {
			return err
		}
		var backend int
		if err := s.conn.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&backend); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		finished := make(chan error, 1)
		go func() { finished <- s.ApplyMigrations(ctx, expected, sessionMigrations, "testdata/session-pg-cancel") }()
		other, err := open(cfg, false)
		if err != nil {
			cancel()
			<-finished
			return err
		}
		defer other.Close()
		poll := time.NewTicker(10 * time.Millisecond)
		defer poll.Stop()
		for {
			var sleeping bool
			if err := other.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='PgSleep')", backend).Scan(&sleeping); err != nil {
				cancel()
				<-finished
				return err
			}
			if sleeping {
				break
			}
			select {
			case err := <-finished:
				return fmt.Errorf("migration ended before backend barrier: %v", err)
			case <-ctx.Done():
				<-finished
				return ctx.Err()
			case <-poll.C:
			}
		}
		var killed bool
		if err := other.QueryRowContext(ctx, "SELECT pg_terminate_backend($1)", backend).Scan(&killed); err != nil {
			cancel()
			<-finished
			return err
		}
		if !killed {
			cancel()
			<-finished
			return errors.New("test backend termination refused")
		}
		if err := <-finished; err == nil {
			return errors.New("terminated migration reported success")
		}
		var after int
		if err := s.conn.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&after); err == nil {
			return errors.New("terminated owner reconnected")
		}
		return nil
	})
	if err == nil {
		t.Fatal("terminated outer session did not report lost ownership")
	}
	err = WithLock(t.Context(), cfg, func(s *Session) error {
		current, err := s.Read(t.Context())
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, expected) {
			return errors.New("terminated backend advanced pending state")
		}
		var count int
		return s.conn.QueryRowContext(t.Context(), "SELECT count(*) FROM migration_partial_probe").Scan(&count)
	})
	if err != nil {
		t.Fatal(err)
	}
}
