package upgrade

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sync"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"
)

// ApplyMigrations executes source-owned embedded SQL on the session's physical
// connection, only after its durable schema-write-started marker. It does not
// authenticate the caller's release claims or mark health; the application gate
// owns those checks. An embed.FS prevents checked SQL bytes changing between
// inventory validation and execution. No Go/global migrations are admitted.
func (s *Session) ApplyMigrations(ctx context.Context, expected State, source embed.FS, directory string) error {
	if expected.Pending == nil || expected.Pending.Kind != UpgradeOperation {
		return ErrConflict
	}
	if err := s.check(); err != nil {
		return err
	}
	if err := expected.Validate(); err != nil {
		return err
	}
	if expected.Pending.Phase != SchemaWriteStarted || expected.Pending.Invalidated {
		return ErrConflict
	}
	if _, err := s.Resume(ctx, expected); err != nil {
		return err
	}
	manifest, err := releaseidentity.BuildMigrationManifest(source, directory, s.engine)
	if err != nil {
		return err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return err
	}
	if digest != expected.Pending.TargetMigrationDigest {
		return errors.New("upgrade: embedded migration digest differs from prepared target")
	}
	if err := s.checkMigrationHistory(ctx, expected, manifest); err != nil {
		return err
	}
	migrations, err := fs.Sub(source, directory)
	if err != nil {
		return err
	}
	return s.applyEmbedded(ctx, migrations)
}

func (s *Session) applyEmbedded(ctx context.Context, migrations fs.FS) error {
	return s.applyEmbeddedThrough(ctx, migrations, 0)
}

func (s *Session) applyEmbeddedThrough(ctx context.Context, migrations fs.FS, version int64) error {
	err := s.conn.Raw(func(value any) error {
		var borrowed driver.Conn
		var valid func() bool
		switch s.engine {
		case releaseidentity.Postgres:
			pg, ok := value.(*stdlib.Conn)
			if !ok {
				return errors.New("upgrade: unexpected PostgreSQL driver")
			}
			borrowed = &migrationPostgresConn{Conn: pg}
			valid = func() bool { return !pg.Conn().IsClosed() }
		case releaseidentity.SQLite:
			sq, ok := value.(migrationSQLiteDriver)
			if !ok {
				return errors.New("upgrade: SQLite driver lacks required context and validity interfaces")
			}
			borrowed = &migrationSQLiteConn{migrationSQLiteDriver: sq}
			valid = sq.IsValid
		default:
			return errors.New("upgrade: unknown migration engine")
		}
		// Private fixture seam wraps the real driver, never replaces goose or
		// the owned connection. Production sessions leave it nil.
		if s.wrapMigrationDriver != nil {
			borrowed = s.wrapMigrationDriver(borrowed)
		}
		if err := runBorrowedMigrationsThrough(ctx, borrowed, s.engine, migrations, version); err != nil {
			// Returning exactly ErrBadConn makes database/sql discard the owned physical
			// connection. The caller retains the durable post-write marker regardless.
			if !valid() {
				return driver.ErrBadConn
			}
			return err
		}
		if !valid() {
			return driver.ErrBadConn
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("upgrade: apply migrations: %w", err)
	}
	return s.check()
}

// The inner pool owns one logical lease. It cannot reconnect if cancellation,
// driver validity or a migration discards that lease; only the outer session
// owns the physical connection. Everything is destroyed inside Conn.Raw.
type migrationConnector struct {
	mu     sync.Mutex
	conn   driver.Conn
	active bool
	used   bool
}

func (c *migrationConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || c.used {
		return nil, driver.ErrBadConn
	}
	c.used = true
	return c.conn, nil
}
func (*migrationConnector) Driver() driver.Driver { return migrationDriver{} }

type migrationDriver struct{}

func (migrationDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("upgrade: migration lease cannot open a physical connection")
}

type migrationPostgresConn struct{ *stdlib.Conn }

func (*migrationPostgresConn) Close() error { return nil }

// Preserve the maintained SQLite driver's context and cancellation handling.
// The wrapper changes only logical Close ownership, not SQL behavior.
type migrationSQLiteDriver interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.SessionResetter
	driver.Validator
}
type migrationSQLiteConn struct{ migrationSQLiteDriver }

func (*migrationSQLiteConn) Close() error { return nil }

func runBorrowedMigrations(ctx context.Context, borrowed driver.Conn, engine releaseidentity.Engine, migrations fs.FS) (err error) {
	return runBorrowedMigrationsThrough(ctx, borrowed, engine, migrations, 0)
}

func runBorrowedMigrationsThrough(ctx context.Context, borrowed driver.Conn, engine releaseidentity.Engine, migrations fs.FS, version int64) (err error) {
	connector := &migrationConnector{conn: borrowed, active: true}
	inner := sql.OpenDB(connector)
	inner.SetMaxOpenConns(1)
	inner.SetMaxIdleConns(1)
	defer func() {
		err = errors.Join(err, inner.Close())
		connector.mu.Lock()
		connector.active = false
		connector.mu.Unlock()
	}()
	dialect := database.DialectSQLite3
	options := []goose.ProviderOption{goose.WithDisableGlobalRegistry(true)}
	if engine == releaseidentity.Postgres {
		dialect = database.DialectPostgres
		locker, lockErr := lock.NewPostgresSessionLocker()
		if lockErr != nil {
			return lockErr
		}
		// The real maintained locker is reentrant on this exact backend. Goose's
		// unlock releases its own acquisition, leaving the outer session lock held.
		options = append(options, goose.WithSessionLocker(locker))
	}
	provider, err := goose.NewProvider(dialect, inner, migrations, options...)
	if err != nil {
		return err
	}
	if version > 0 {
		_, err = provider.UpTo(ctx, version)
	} else {
		_, err = provider.Up(ctx)
	}
	return err
}
