package upgrade

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// SeparatePostgresScratch checks database identity through both actual
// connections. Requiring different names rejects live-DSN aliases, including
// different users, connection options, and Unix/TCP routes to the same DB.
func SeparatePostgresScratch(ctx context.Context, source, scratch Config) (string, error) {
	if source.Engine != releaseidentity.Postgres || scratch.Engine != releaseidentity.Postgres {
		return "", ErrConflict
	}
	name := func(cfg Config) (string, error) {
		db, err := open(cfg, true)
		if err != nil {
			return "", errors.New("PostgreSQL database identity unavailable")
		}
		defer db.Close()
		var result string
		if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&result); err != nil {
			return "", errors.New("PostgreSQL database identity unavailable")
		}
		return result, nil
	}
	liveName, err := name(source)
	if err != nil {
		return "", err
	}
	scratchName, err := name(scratch)
	if err != nil {
		return "", err
	}
	if scratchName == liveName {
		return "", errors.New("scratch PostgreSQL database must have a different database name from the live source")
	}
	return scratchName, nil
}

// ResetOwnedScratch resets only a PostgreSQL scratch database already enrolled
// by the local coordinator. The caller must prove it is a separately configured
// database and persist ownership before the first restore. The complete schema
// must still equal its authenticated source; unknown objects stop cleanup.
// Migration exclusion covers the schema check and transactional reset.
func (s *Session) ResetOwnedScratch(ctx context.Context, schema releaseidentity.Digest) error {
	if s.engine != releaseidentity.Postgres || schema.Validate() != nil {
		return ErrConflict
	}
	return s.transaction(ctx, func() error {
		catalog, err := s.DomainCatalog(ctx)
		if err != nil {
			return err
		}
		if catalog.Digest() != schema {
			return ErrConflict
		}
		if _, err := s.conn.ExecContext(ctx, "DROP SCHEMA public CASCADE"); err != nil {
			return err
		}
		_, err = s.conn.ExecContext(ctx, "CREATE SCHEMA public")
		return err
	})
}
