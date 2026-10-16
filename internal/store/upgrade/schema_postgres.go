package upgrade

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// Canonical rendering must match the published catalog, not operator session
// preferences. Settings are restored before returning to caller queries.
// Failed reads roll back their private scope, including after cancellation.
const postgresCatalogSettings = `SELECT
 pg_catalog.set_config('TimeZone','UTC',true),
 pg_catalog.set_config('DateStyle','ISO, MDY',true),
 pg_catalog.set_config('IntervalStyle','postgres',true),
 pg_catalog.set_config('extra_float_digits','1',true),
 pg_catalog.set_config('bytea_output','hex',true),
 pg_catalog.set_config('search_path','pg_catalog, public, pg_temp',true),
 pg_catalog.set_config('quote_all_identifiers','off',true),
 pg_catalog.set_config('standard_conforming_strings','on',true)`

const postgresCatalogOriginalSettings = `SELECT
 pg_catalog.current_setting('TimeZone'), pg_catalog.current_setting('DateStyle'),
 pg_catalog.current_setting('IntervalStyle'), pg_catalog.current_setting('extra_float_digits'),
 pg_catalog.current_setting('bytea_output'), pg_catalog.current_setting('search_path'),
 pg_catalog.current_setting('quote_all_identifiers'), pg_catalog.current_setting('standard_conforming_strings')`

const postgresCatalogRestoreSettings = `SELECT
 pg_catalog.set_config('TimeZone',$1,true), pg_catalog.set_config('DateStyle',$2,true),
 pg_catalog.set_config('IntervalStyle',$3,true), pg_catalog.set_config('extra_float_digits',$4,true),
 pg_catalog.set_config('bytea_output',$5,true), pg_catalog.set_config('search_path',$6,true),
 pg_catalog.set_config('quote_all_identifiers',$7,true), pg_catalog.set_config('standard_conforming_strings',$8,true)`

// This intentionally includes the store admission and recovery wrappers, which
// own transactions without exposing the full pgx.Tx connection capability.
type postgresCatalogTransaction interface {
	PGSnapshotQueries
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type catalogReader func(context.Context, catalogQuery, releaseidentity.Engine) (Catalog, error)

func inspectSQLPostgresCatalog(ctx context.Context, q queryer, read catalogReader) (catalog Catalog, err error) {
	switch q := q.(type) {
	case *sql.Conn:
		err = q.Raw(func(value any) error {
			driver, ok := value.(*stdlib.Conn)
			if !ok {
				return errors.New("upgrade: unexpected PostgreSQL catalog driver")
			}
			catalog, err = inspectPostgresCatalog(ctx, driver.Conn(), read)
			return err
		})
		return catalog, err
	case *sql.Tx:
		query := sqlCatalogQuery(q)
		return withPostgresCatalogScope(ctx, query, true, func(context.Context) error { return q.Rollback() }, read)
	default:
		return Catalog{}, errors.New("upgrade: PostgreSQL catalog requires a reserved connection or transaction")
	}
}

func inspectPostgresCatalog(ctx context.Context, q PGSnapshotQueries, read catalogReader) (Catalog, error) {
	query := func(ctx context.Context, sql string, args ...any) (catalogRows, error) {
		return q.Query(ctx, sql, args...)
	}
	switch q := q.(type) {
	case *pgx.Conn:
		status := q.PgConn().TxStatus()
		if status != 'I' && status != 'T' {
			return Catalog{}, errors.New("upgrade: PostgreSQL catalog requires a usable transaction")
		}
		return withPostgresCatalogScope(ctx, query, status == 'T', q.Close, read)
	case postgresCatalogTransaction:
		return withPostgresCatalogScope(ctx, query, true, q.Rollback, read)
	default:
		return Catalog{}, errors.New("upgrade: PostgreSQL catalog requires a reserved connection or transaction")
	}
}

// A savepoint preserves a caller's snapshot, pending writes, and prior SET LOCAL
// values. Outside a transaction, own a read-only repeatable snapshot instead.
// ROLLBACK TO also recovers a failed query without aborting the caller's work.
func withPostgresCatalogScope(ctx context.Context, query catalogQuery, nested bool, invalidate func(context.Context) error, read catalogReader) (catalog Catalog, err error) {
	command := func(ctx context.Context, sql string, args ...any) error {
		rows, err := query(ctx, sql, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
		}
		rows.Close()
		return rows.Err()
	}
	begin := "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY"
	if nested {
		begin = "SAVEPOINT hikyo_catalog_rendering"
	}
	if err := ctx.Err(); err != nil {
		return Catalog{}, err
	}
	if err := command(ctx, begin); err != nil {
		if !nested {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = errors.Join(err, invalidate(cleanup))
		}
		return Catalog{}, err
	}
	var original [8]string
	var originalArgs [8]any
	for i := range original {
		originalArgs[i] = &original[i]
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var cleanupErr error
		if nested && err == nil {
			args := make([]any, len(original))
			for i := range original {
				args[i] = original[i]
			}
			cleanupErr = command(cleanup, postgresCatalogRestoreSettings, args...)
			if cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
			}
		}
		if !nested {
			cleanupErr = command(cleanup, "ROLLBACK")
		} else {
			if err != nil {
				cleanupErr = command(cleanup, "ROLLBACK TO SAVEPOINT hikyo_catalog_rendering")
			}
			if cleanupErr == nil {
				cleanupErr = command(cleanup, "RELEASE SAVEPOINT hikyo_catalog_rendering")
			}
		}
		if cleanupErr != nil {
			cleanupErr = errors.Join(cleanupErr, invalidate(cleanup))
			err = errors.Join(err, fmt.Errorf("upgrade: restore catalog session settings: %w", cleanupErr))
		}
		if err != nil {
			catalog = Catalog{}
		}
	}()
	if nested {
		rows, captureErr := query(ctx, postgresCatalogOriginalSettings)
		if captureErr != nil {
			return Catalog{}, captureErr
		}
		if !rows.Next() {
			captureErr = errors.Join(errors.New("upgrade: missing catalog session settings"), rows.Err())
		} else {
			captureErr = rows.Scan(originalArgs[:]...)
		}
		rows.Close()
		captureErr = errors.Join(captureErr, rows.Err())
		if captureErr != nil {
			return Catalog{}, captureErr
		}
	}
	if err := command(ctx, postgresCatalogSettings); err != nil {
		return Catalog{}, err
	}
	return read(ctx, query, releaseidentity.Postgres)
}
