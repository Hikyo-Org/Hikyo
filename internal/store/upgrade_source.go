package store

import (
	"context"
	"database/sql"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/jackc/pgx/v5"
)

// InspectUpgradeSource measures the source inside one read-only transaction.
// It is a preparation/restore inspection, not an admission or trust decision.
func (db *DB) InspectUpgradeSource(ctx context.Context, manifest releaseidentity.MigrationManifest) (upgrade.InstalledSource, error) {
	if db.engine == EnginePostgres {
		tx, err := db.BeginPostgres(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return upgrade.InstalledSource{}, err
		}
		defer tx.Rollback(ctx)
		out, err := upgrade.InspectPostgresSource(ctx, tx, manifest)
		if err != nil {
			return upgrade.InstalledSource{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return upgrade.InstalledSource{}, err
		}
		return out, nil
	}
	tx, err := db.BeginSQLite(ctx, true)
	if err != nil {
		return upgrade.InstalledSource{}, err
	}
	defer tx.Rollback()
	out, err := upgrade.InspectSQLiteSource(ctx, tx, manifest)
	if err != nil {
		return upgrade.InstalledSource{}, err
	}
	if err := tx.Commit(); err != nil {
		return upgrade.InstalledSource{}, err
	}
	return out, nil
}
func (db *DB) inspectUpgradeSource(ctx context.Context, manifest releaseidentity.MigrationManifest) (upgrade.InstalledSource, error) {
	if db.engine == EnginePostgres {
		tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return upgrade.InstalledSource{}, err
		}
		defer tx.Rollback(ctx)
		inspected, err := upgrade.InspectPostgresSource(ctx, tx, manifest)
		if err != nil {
			return upgrade.InstalledSource{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return upgrade.InstalledSource{}, err
		}
		return inspected, nil
	}
	tx, err := db.sqRead.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return upgrade.InstalledSource{}, err
	}
	defer tx.Rollback()
	inspected, err := upgrade.InspectSQLiteSource(ctx, tx, manifest)
	if err != nil {
		return upgrade.InstalledSource{}, err
	}
	if err := tx.Commit(); err != nil {
		return upgrade.InstalledSource{}, err
	}
	return inspected, nil
}
