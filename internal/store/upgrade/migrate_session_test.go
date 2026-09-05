package upgrade

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	bundlefixture "github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/jackc/pgx/v5"
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

// A successful ownership probe can race backend termination. The transaction's
// shared drain lock, not that probe, must prevent the next owner from entering.
func TestOperatorTransactionDrainsAfterBackendTermination(t *testing.T) {
	cfg := testConfig(t, releaseidentity.Postgres)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	native, err := pgx.Connect(ctx, cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close(context.Background())
	admin, err := pgx.Connect(ctx, cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	var next <-chan error
	reached := false
	err = WithLock(ctx, cfg, func(s *Session) error {
		var backend int
		if err := s.conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&backend); err != nil {
			return err
		}
		transaction, err := native.Begin(ctx)
		if err != nil {
			return err
		}
		defer transaction.Rollback(context.Background())
		if err := s.guardPostgresOperator(ctx, transaction); err != nil {
			return err
		}
		// This is the exact gap after a successful pre-commit owner check.
		if err := s.checkPostgresOwner(ctx); err != nil {
			return err
		}
		var killed bool
		if err := admin.QueryRow(ctx, "SELECT pg_terminate_backend($1)", backend).Scan(&killed); err != nil {
			return err
		}
		if !killed {
			return errors.New("backend termination refused")
		}
		if err := s.checkPostgresOwner(ctx); err == nil {
			return errors.New("terminated owner retained authority")
		}
		done := make(chan error, 1)
		next = done
		go func() { done <- WithLock(ctx, cfg, func(*Session) error { return nil }) }()
		// Wait for an actual blocked exclusive lock request, not a timing guess.
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var waiting bool
			if err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks
    WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
    AND classid=($1::bigint >> 32)::oid AND objid=($1::bigint & 4294967295)::oid
    AND objsubid=1 AND mode='ExclusiveLock' AND NOT granted)`, operatorDrainLock).Scan(&waiting); err != nil {
				return err
			}
			if waiting {
				break
			}
			select {
			case err := <-done:
				next = nil
				return fmt.Errorf("next owner entered before transaction settled: %v", err)
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
		if err := transaction.Rollback(ctx); err != nil {
			return err
		}
		if err := <-done; err != nil {
			return err
		}
		next = nil
		reached = true
		return nil
	})
	if next != nil {
		cancel()
		<-next
	}
	if err == nil {
		t.Fatal("terminated session reported success")
	}
	if !reached {
		t.Fatalf("custody regression did not reach settlement: %v", err)
	}
}

// commitObservedDriver preserves every maintained driver interface. The only
// interception is after a real successful transaction COMMIT has returned.
type observedMigrationConnection interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.SessionResetter
}
type commitObservedDriver struct {
	observedMigrationConnection
	after func()
}

// Preserve SQLite's optional Validator without pretending PostgreSQL's driver
// implements that interface. Native connection validity remains independently
// checked by the existing production migration runner.
type commitObservedSQLiteDriver struct {
	*commitObservedDriver
	driver.Validator
}

func (d *commitObservedDriver) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	transaction, err := d.observedMigrationConnection.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &commitObservedTransaction{Tx: transaction, after: d.after}, nil
}

type commitObservedTransaction struct {
	driver.Tx
	after func()
}

func (t *commitObservedTransaction) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	t.after()
	return nil
}

type migrationCommitProcessConfig struct {
	Store     Config
	State     State
	Directory string
	Pinned    releasetrust.PinnedTrust
	Version   int64
	Marker    string
}

func TestMigrationCommitCrashChild(t *testing.T) {
	path := os.Getenv("HIKYO_MIGRATION_COMMIT_PROCESS_CONFIG")
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read private child configuration")
	}
	var cfg migrationCommitProcessConfig
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal("decode private child configuration")
	}
	bundle, err := upgradebundle.Load(t.Context(), cfg.Directory, cfg.Pinned, cfg.State.Floor)
	if err != nil {
		t.Fatal("authenticate child release bundle")
	}
	plan, err := bundle.Plan(upgradecompat.InstalledSource{Identity: cfg.State.Pending.RouteSource, Migrations: emptyManifest(cfg.Store.Engine), SchemaSHA256: cfg.State.Pending.SourceSchemaDigest}, cfg.State.Pending.Target)
	if err != nil || plan.Digest() != cfg.State.Pending.RouteDigest {
		t.Fatal("child route differs from signed release")
	}
	err = WithLock(t.Context(), cfg.Store, func(session *Session) error {
		if _, err := session.Resume(t.Context(), cfg.State); err != nil {
			return err
		}
		session.wrapMigrationDriver = func(original driver.Conn) driver.Conn {
			maintained, ok := original.(observedMigrationConnection)
			if !ok {
				t.Fatal("fixture driver lacks maintained interfaces")
			}
			wrapped := &commitObservedDriver{observedMigrationConnection: maintained, after: func() {
				observed, err := open(cfg.Store, true)
				if err != nil {
					t.Fatal("open commit observation")
				}
				var version int64
				err = observed.QueryRowContext(t.Context(), "SELECT COALESCE(MAX(version_id),0) FROM goose_db_version WHERE is_applied").Scan(&version)
				closeErr := observed.Close()
				if err != nil || closeErr != nil {
					t.Fatal("read committed goose history:", errors.Join(err, closeErr))
				}
				if version != cfg.Version {
					return
				}
				if err := os.WriteFile(cfg.Marker, []byte("committed"), 0600); err != nil {
					t.Fatal("write commit marker")
				}
				<-t.Context().Done()
			}}
			if validator, ok := original.(driver.Validator); ok {
				return &commitObservedSQLiteDriver{commitObservedDriver: wrapped, Validator: validator}
			}
			return wrapped
		}
		return session.ApplyMigrations(t.Context(), cfg.State, sessionMigrations, "testdata/session-commits")
	})
	if err != nil {
		t.Fatal("migration child failed before checkpoint:", err)
	}
	t.Fatal("migration child returned before kill")
}

func TestMigrationProcessCrashAfterIndividualCommits(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		for _, version := range []int64{1, 2} {
			t.Run(fmt.Sprintf("commit-%d", version), func(t *testing.T) {
				cfg := testConfig(t, cfg.Engine)
				empty, catalog, err := BuildScratchSchema(t.Context(), testConfig(t, cfg.Engine), sessionMigrations, "testdata/session-commits")
				if err != nil {
					t.Fatal(err)
				}
				manifest, err := releaseidentity.BuildMigrationManifest(sessionMigrations, "testdata/session-commits", cfg.Engine)
				if err != nil {
					t.Fatal(err)
				}
				source := upgradecompat.InstalledSource{Identity: Source{Genesis: FreshGenesis}, Migrations: emptyManifest(cfg.Engine), SchemaSHA256: empty.Digest()}
				fixture := bundlefixture.Write(t, source, []bundlefixture.Target{{Version: "1.0.1", Sequence: 1, Commit: strings.Repeat("c", 40), Migrations: manifest, SchemaSHA256: catalog.Digest()}})
				step := fixture.Plan.Steps()[0]
				sourceDigest, _ := source.Migrations.Digest()
				targetDigest, _ := manifest.Digest()
				operation := Operation{Kind: UpgradeOperation, Source: source.Identity, RouteSource: source.Identity, Target: step.Target, SourceMigrationDigest: sourceDigest, TargetMigrationDigest: targetDigest, SourceSchemaDigest: source.SchemaSHA256, TargetSchemaDigest: step.TargetSchemaSHA256, RouteDigest: fixture.Plan.Digest(), RouteLength: 1, Generation: 1, BackupID: "fresh-bootstrap", Phase: Prepared, Acceptance: Acceptance{Floor: fixture.Bundle.Snapshot().Floor(), ReleaseRootDigest: releaseidentity.Hash(fixture.Pinned.Root)}}
				var state State
				err = WithLock(t.Context(), cfg, func(session *Session) error {
					var err error
					state, err = session.Bootstrap(t.Context(), source.Migrations, operation, Production)
					if err != nil {
						return err
					}
					state, err = session.Advance(t.Context(), state, SchemaWriteStarted)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				directory := t.TempDir()
				marker := filepath.Join(directory, "committed")
				child := migrationCommitProcessConfig{Store: cfg, State: state, Directory: fixture.Directory, Pinned: fixture.Pinned, Version: version, Marker: marker}
				raw, err := json.Marshal(child)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(directory, "child.json")
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMigrationCommitCrashChild$", "-test.timeout=25s")
				command.Env = append(os.Environ(), "HIKYO_MIGRATION_COMMIT_PROCESS_CONFIG="+path)
				log, err := os.OpenFile(filepath.Join(directory, "child.log"), os.O_CREATE|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer log.Close()
				command.Stdout, command.Stderr = log, log
				if err := command.Start(); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- command.Wait() }()
				defer command.Process.Kill()
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				waiting := true
				for waiting {
					select {
					case err := <-done:
						output, _ := os.ReadFile(filepath.Join(directory, "child.log"))
						safe := string(output)
						if cfg.DSN != "" {
							safe = strings.ReplaceAll(safe, cfg.DSN, "[test DSN]")
						}
						t.Fatalf("child exited before commit checkpoint: %v: %s", err, safe)
					case <-ctx.Done():
						command.Process.Kill()
						<-done
						t.Fatal("child commit checkpoint deadline")
					case <-ticker.C:
						if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
							continue
						} else if err != nil {
							t.Fatal(err)
						}
						if err := command.Process.Kill(); err != nil {
							t.Fatal(err)
						}
						var exit *exec.ExitError
						if err := <-done; !errors.As(err, &exit) || exit.Success() {
							t.Fatal("expected SIGKILL termination")
						}
						waiting = false
					}
				}
				after, err := InspectControl(t.Context(), cfg)
				if err != nil || !reflect.DeepEqual(state, after) {
					t.Fatal("migration commit crash changed durable phase", err)
				}
				observed, err := open(cfg, true)
				if err != nil {
					t.Fatal(err)
				}
				var actual, count int64
				err = observed.QueryRowContext(t.Context(), "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied").Scan(&actual)
				if err == nil {
					err = observed.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM migration_commit_probe").Scan(&count)
				}
				observed.Close()
				if err != nil || actual != version || count != version {
					t.Fatalf("crash did not preserve exactly committed prefix: version=%d count=%d error=%v", actual, count, err)
				}
				err = WithLock(t.Context(), cfg, func(session *Session) error {
					if err := session.ApplyMigrations(t.Context(), state, sessionMigrations, "testdata/session-commits"); err != nil {
						return err
					}
					actual, err := session.DomainCatalog(t.Context())
					if err != nil {
						return err
					}
					if actual.Digest() != catalog.Digest() {
						return errors.New("resume differs from exact signed target catalog")
					}
					var count int
					if err := session.conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM migration_commit_probe").Scan(&count); err != nil {
						return err
					}
					if count != 3 {
						return errors.New("resume duplicated or lost committed migration effects")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	})
}
