package store

import (
	"context"
	"io"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/jackc/pgx/v5"
)

// RestoreDestination cannot serve repositories or expose a runtime database.
// Its only mutation imports the exact authenticated archive after the importer
// locks and checks every destination table in the same transaction as COPY.
type RestoreDestination struct {
	db        *DB
	authority upgrade.RestoreDestinationAdmission
	archive   *backupreceipt.AuthenticatedArchive
	plan      upgradecompat.Plan
	config    Config
}

func OpenRestoreDestination(ctx context.Context, cfg Config, authority upgrade.RestoreDestinationAdmission, archive *backupreceipt.AuthenticatedArchive, plan upgradecompat.Plan) (*RestoreDestination, error) {
	if err := authority.Check(ctx, upgradeConfig(cfg), archive, plan); err != nil {
		return nil, err
	}
	db, err := openConfigured(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &RestoreDestination{db: db, authority: authority, archive: archive, plan: plan, config: cfg}, nil
}
func (d *RestoreDestination) Close() error { return d.db.Close() }
func (d *RestoreDestination) RestorePostgres(ctx context.Context, mutate func(context.Context, pgx.Tx) error) (Manifest, error) {
	if d == nil {
		return Manifest{}, upgrade.ErrConflict
	}
	if err := d.authority.Check(ctx, upgradeConfig(d.config), d.archive, d.plan); err != nil {
		return Manifest{}, err
	}
	archive, err := d.archive.Open()
	if err != nil {
		return Manifest{}, err
	}
	return restorePostgresChecked(ctx, d.db, archive, d.plan, mutate, d.authority.GuardPostgres, d.authority.CheckOwner, true)
}

// DataRestoreDestination imports ordinary v1 data only. The caller supplies the
// already decrypted archive; this capability makes no upgrade-receipt claim.
// Restored serving still requires a new current-incarnation proof at the gate.
type DataRestoreDestination struct {
	db        *DB
	authority upgrade.DataRestoreDestinationAdmission
	plan      upgradecompat.Plan
	config    Config
}

func OpenDataRestoreDestination(ctx context.Context, cfg Config, authority upgrade.DataRestoreDestinationAdmission, plan upgradecompat.Plan) (*DataRestoreDestination, error) {
	if err := authority.Check(ctx, upgradeConfig(cfg), plan); err != nil {
		return nil, err
	}
	db, err := openConfigured(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &DataRestoreDestination{db: db, authority: authority, plan: plan, config: cfg}, nil
}
func (d *DataRestoreDestination) Close() error { return d.db.Close() }
func (d *DataRestoreDestination) RestorePostgres(ctx context.Context, archive io.Reader, mutate func(context.Context, pgx.Tx) error) (Manifest, error) {
	if d == nil {
		return Manifest{}, upgrade.ErrConflict
	}
	if err := d.authority.Check(ctx, upgradeConfig(d.config), d.plan); err != nil {
		return Manifest{}, err
	}
	return restorePostgresChecked(ctx, d.db, archive, d.plan, mutate, d.authority.GuardPostgres, d.authority.CheckOwner, false)
}
