package upgrade

import (
	"context"
	"database/sql"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/jackc/pgx/v5"
)

// PreparePostgresRestoreControlSchema installs ONLY empty storage tables inside
// the same transaction that will validate/import the authenticated archive and
// reconcile its authority. It requires the exact independently verified source
// catalog and migration manifest. It never overwrites populated control state.
func PreparePostgresRestoreControlSchema(ctx context.Context, tx pgx.Tx, manifest releaseidentity.MigrationManifest, schema releaseidentity.Digest) error {
	if tx == nil || manifest.Engine != releaseidentity.Postgres {
		return ErrCorrupt
	}
	catalog, err := inspectCatalogWith(ctx, func(ctx context.Context, q string, args ...any) (catalogRows, error) {
		return tx.Query(ctx, q, args...)
	}, releaseidentity.Postgres)
	if err != nil {
		return err
	}
	return prepareRestoreSchema(catalog, manifest, schema, func(q string) scanner { return tx.QueryRow(ctx, q) }, func(q string) error { _, err := tx.Exec(ctx, q); return err })
}

func PrepareSQLiteRestoreControlSchema(ctx context.Context, tx *sql.Tx, manifest releaseidentity.MigrationManifest, schema releaseidentity.Digest) error {
	if tx == nil || manifest.Engine != releaseidentity.SQLite {
		return ErrCorrupt
	}
	catalog, err := inspectCatalog(ctx, tx, releaseidentity.SQLite)
	if err != nil {
		return err
	}
	return prepareRestoreSchema(catalog, manifest, schema, func(q string) scanner { return tx.QueryRowContext(ctx, q) }, func(q string) error { _, err := tx.ExecContext(ctx, q); return err })
}

func prepareRestoreSchema(catalog Catalog, manifest releaseidentity.MigrationManifest, schema releaseidentity.Digest, row func(string) scanner, exec func(string) error) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	if err := schema.Validate(); err != nil {
		return err
	}
	exists := controlPresent(catalog)
	domain, err := domainCatalog(catalog)
	if err != nil {
		return err
	}
	if domain.Digest() != schema || !appliedMatches(domain.Applied, manifest) {
		return ErrConflict
	}
	if exists {
		// Constant table vocabulary: no archive-provided SQL identifier enters.
		for _, query := range []string{`SELECT count(*) FROM upgrade_control`, `SELECT count(*) FROM upgrade_pending`, `SELECT count(*) FROM upgrade_nonces`} {
			var count int64
			if err := row(query).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return ErrConflict
			}
		}
		return nil
	}
	for _, ddl := range []string{controlDDL, pendingDDL, nonceDDL} {
		if err := exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
