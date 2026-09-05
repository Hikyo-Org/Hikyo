package store

import (
	"context"
	"io"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

// PreparationDB has no runtime repository or driver accessor. Its only data
// output is the separately authenticated export/inspection protocol.
type PreparationDB struct {
	db        *DB
	authority upgrade.PreparationAdmission
	config    Config
}

func OpenPreparation(ctx context.Context, cfg Config, authority upgrade.PreparationAdmission) (*PreparationDB, error) {
	if err := authority.Check(ctx, upgradeConfig(cfg)); err != nil {
		return nil, err
	}
	db, err := openConfigured(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &PreparationDB{db: db, authority: authority, config: cfg}, nil
}
func (p *PreparationDB) Close() error { return p.db.Close() }
func (p *PreparationDB) InspectUpgradeSource(ctx context.Context, manifest releaseidentity.MigrationManifest) (upgrade.InstalledSource, error) {
	if err := p.authority.Check(ctx, upgradeConfig(p.config)); err != nil {
		return upgrade.InstalledSource{}, err
	}
	return p.db.inspectUpgradeSource(ctx, manifest)
}
func (p *PreparationDB) ExportUpgrade(ctx context.Context, w io.Writer, workDir string, request UpgradeExportRequest) (Manifest, error) {
	if err := p.authority.Check(ctx, upgradeConfig(p.config)); err != nil {
		return Manifest{}, err
	}
	if request.Plan.Digest() != p.authority.PlanDigest() {
		return Manifest{}, upgrade.ErrConflict
	}
	request.preparation = p.authority
	return ExportUpgrade(ctx, p.db, w, workDir, request)
}
func (db *DB) ExportUpgrade(ctx context.Context, w io.Writer, workDir string, request UpgradeExportRequest) (Manifest, error) {
	return ExportUpgrade(ctx, db, w, workDir, request)
}
func upgradeConfig(cfg Config) upgrade.Config {
	return upgrade.Config{Engine: releaseidentity.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}
}
