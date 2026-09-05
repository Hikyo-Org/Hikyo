package upgrade

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/gofrs/flock"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3/lock"
	_ "modernc.org/sqlite"
)

type Config struct {
	Engine releaseidentity.Engine
	Path   string
	DSN    string
}

// Session is confined to WithLock's callback. It owns migration exclusion,
// not release trust or tenant admission. Callers must not use it concurrently.
type Session struct {
	conn   *sql.Conn
	engine releaseidentity.Engine
	active bool
	path   string
	file   os.FileInfo
	// Test-only fault points are inaccessible to application callers.
	beforeCommit func() error
	afterCommit  func() error
}

func (s *Session) check() error {
	if s == nil || !s.active || s.conn == nil {
		return errors.New("upgrade: inactive migration session")
	}
	if s.engine == releaseidentity.SQLite {
		info, err := checkedFile(s.path)
		if err != nil {
			return err
		}
		if !os.SameFile(info, s.file) {
			return errors.New("upgrade: SQLite database identity changed")
		}
	}
	return nil
}

// WithLock uses the existing migration lock namespace. PostgreSQL's private
// pool owns one reserved physical connection for the complete callback; errors
// and cancellation close that connection rather than returning lock ownership
// to a reusable runtime pool.
func WithLock(ctx context.Context, cfg Config, fn func(*Session) error) (err error) {
	if err := cfg.Engine.Validate(); err != nil {
		return err
	}
	if fn == nil {
		return errors.New("upgrade: missing locked operation")
	}
	var fileLock *flock.Flock
	if cfg.Engine == releaseidentity.SQLite {
		cfg.Path, err = canonicalSQLite(cfg.Path)
		if err != nil {
			return err
		}
		fileLock = flock.New(cfg.Path + ".lock")
		locked, lockErr := fileLock.TryLockContext(ctx, 25*time.Millisecond)
		if lockErr != nil {
			return lockErr
		}
		if !locked {
			return errors.New("upgrade: migration lock held")
		}
		defer func() { err = errors.Join(err, fileLock.Unlock()) }()
		if _, statErr := os.Stat(cfg.Path); statErr == nil {
			if _, err := checkedFile(cfg.Path); err != nil {
				return err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	db, err := open(cfg, false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()
	session := &Session{conn: conn, engine: cfg.Engine, active: true, path: cfg.Path}
	defer func() { session.active = false }()
	if cfg.Engine == releaseidentity.SQLite {
		session.file, err = checkedFile(cfg.Path)
		if err != nil {
			return err
		}
	} else {
		locker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return err
		}
		if err := locker.SessionLock(ctx, conn); err != nil {
			return err
		}
		defer func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = errors.Join(err, locker.SessionUnlock(unlockCtx, conn))
		}()
	}
	return fn(session)
}

func open(cfg Config, readonly bool) (*sql.DB, error) {
	driver, dsn := "pgx", cfg.DSN
	if cfg.Engine == releaseidentity.Postgres && dsn == "" {
		return nil, errors.New("upgrade: PostgreSQL DSN required")
	}
	if cfg.Engine == releaseidentity.SQLite {
		driver = "sqlite"
		q := url.Values{}
		q.Add("_pragma", "foreign_keys(1)")
		q.Add("_pragma", "busy_timeout(5000)")
		if readonly {
			q.Set("mode", "ro")
		} else {
			q.Add("_pragma", "synchronous(FULL)")
		}
		dsn = (&url.URL{Scheme: "file", Path: cfg.Path, RawQuery: q.Encode()}).String()
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	return db, nil
}

func canonicalSQLite(path string) (string, error) {
	if path == "" || path == ":memory:" {
		return "", errors.New("upgrade: SQLite requires a durable file")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
}

func checkedFile(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("upgrade: SQLite target is not a regular file")
	}
	if err := requireSingleLink(path, info); err != nil {
		return nil, fmt.Errorf("upgrade: SQLite file identity: %w", err)
	}
	return info, nil
}
