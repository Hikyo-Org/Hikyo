package upgrade

import (
	"context"
	"embed"
	"io/fs"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/jackc/pgx/v5"
)

// RestoreDestinationAdmission permits only importing the authenticated source
// into a matching PostgreSQL schema under this migration session. The importer
// must additionally lock and verify all destination tables are empty before COPY.
type RestoreDestinationAdmission struct {
	session *Session
	archive *backupreceipt.AuthenticatedArchive
	plan    upgradecompat.Plan
}

func (s *Session) ValidateRestoreDestination(ctx context.Context, archive *backupreceipt.AuthenticatedArchive, plan upgradecompat.Plan) (RestoreDestinationAdmission, error) {
	if err := s.check(); err != nil {
		return RestoreDestinationAdmission{}, err
	}
	if s.engine != releaseidentity.Postgres || !archive.Valid() || !plan.Valid() || archive.PlanDigest() != plan.Digest() || archive.Snapshot().Engine != s.engine {
		return RestoreDestinationAdmission{}, ErrConflict
	}
	a := RestoreDestinationAdmission{session: s, archive: archive, plan: plan}
	catalog, err := s.DomainCatalog(ctx)
	if err != nil {
		return RestoreDestinationAdmission{}, err
	}
	if err := a.checkCatalog(catalog); err != nil {
		return RestoreDestinationAdmission{}, err
	}
	return a, nil
}
func (a RestoreDestinationAdmission) CheckOwner(ctx context.Context) error {
	if a.session == nil || (a.archive != nil && !a.archive.Valid()) || !a.plan.Valid() {
		return ErrConflict
	}
	return a.session.checkPostgresOwner(ctx)
}
func (a RestoreDestinationAdmission) Check(ctx context.Context, cfg Config, archive *backupreceipt.AuthenticatedArchive, plan upgradecompat.Plan) error {
	if err := a.CheckOwner(ctx); err != nil {
		return err
	}
	if cfg.Engine != releaseidentity.Postgres || cfg.DSN != a.session.dsn || archive != a.archive || plan.Digest() != a.plan.Digest() {
		return ErrConflict
	}
	catalog, err := a.session.DomainCatalog(ctx)
	if err != nil {
		return err
	}
	return a.checkCatalog(catalog)
}
func (a RestoreDestinationAdmission) GuardPostgres(ctx context.Context, tx pgx.Tx) error {
	if err := a.CheckOwner(ctx); err != nil {
		return err
	}
	if tx == nil {
		return ErrConflict
	}
	if err := a.session.guardPostgresOperator(ctx, tx); err != nil {
		return err
	}
	catalog, err := DomainCatalogPostgres(ctx, tx)
	if err != nil {
		return err
	}
	return a.checkCatalog(catalog)
}
func (a RestoreDestinationAdmission) checkCatalog(catalog Catalog) error {
	manifest, err := a.plan.SourceManifest(releaseidentity.Postgres)
	if err != nil {
		return err
	}
	if catalog.Digest() != a.plan.SourceSchemaDigest() || !appliedMatches(catalog.Applied, manifest) {
		return ErrConflict
	}
	return nil
}

// DataRestoreDestinationAdmission is a separate v1 data-import authority. It
// asserts no ciphertext receipt or operator attestation and cannot admit serving.
type DataRestoreDestinationAdmission struct{ destination RestoreDestinationAdmission }

func (s *Session) ValidateDataRestoreDestination(ctx context.Context, plan upgradecompat.Plan) (DataRestoreDestinationAdmission, error) {
	if err := s.check(); err != nil {
		return DataRestoreDestinationAdmission{}, err
	}
	if s.engine != releaseidentity.Postgres || !plan.Valid() || plan.Source().Genesis == releaseidentity.FreshGenesisV1 {
		return DataRestoreDestinationAdmission{}, ErrConflict
	}
	a := RestoreDestinationAdmission{session: s, plan: plan}
	catalog, err := s.DomainCatalog(ctx)
	if err != nil {
		return DataRestoreDestinationAdmission{}, err
	}
	if err := a.checkCatalog(catalog); err != nil {
		return DataRestoreDestinationAdmission{}, err
	}
	return DataRestoreDestinationAdmission{destination: a}, nil
}
func (a DataRestoreDestinationAdmission) Check(ctx context.Context, cfg Config, plan upgradecompat.Plan) error {
	return a.destination.Check(ctx, cfg, nil, plan)
}
func (a DataRestoreDestinationAdmission) CheckOwner(ctx context.Context) error {
	return a.destination.CheckOwner(ctx)
}
func (a DataRestoreDestinationAdmission) GuardPostgres(ctx context.Context, tx pgx.Tx) error {
	return a.destination.GuardPostgres(ctx, tx)
}

// ApplyRestoreSchema performs the only pre-import schema initialization. Fresh
// catalog inspection and exact embedded source-prefix execution share the owned
// physical migration session; an earlier caller preflight is never authority.
func (s *Session) ApplyRestoreSchema(ctx context.Context, plan upgradecompat.Plan, source embed.FS, directory string) error {
	if err := s.check(); err != nil {
		return err
	}
	if s.engine != releaseidentity.Postgres || !plan.Valid() {
		return ErrConflict
	}
	expected, err := plan.SourceManifest(s.engine)
	if err != nil || len(expected.Entries) == 0 {
		return ErrConflict
	}
	embedded, err := releaseidentity.BuildMigrationManifest(source, directory, s.engine)
	if err != nil {
		return err
	}
	if len(embedded.Entries) < len(expected.Entries) || !slices.Equal(embedded.Entries[:len(expected.Entries)], expected.Entries) {
		return ErrConflict
	}
	catalog, err := inspectCatalog(ctx, s.conn, s.engine)
	if err != nil {
		return err
	}
	if len(catalog.Objects) != 1 || catalog.Objects[0] != `["schema", "public"]` || len(catalog.Applied) != 0 {
		return ErrConflict
	}
	migrations, err := fs.Sub(source, directory)
	if err != nil {
		return err
	}
	last := expected.Entries[len(expected.Entries)-1].Version
	if err := s.applyEmbeddedThrough(ctx, migrations, int64(last)); err != nil {
		return err
	}
	actual, err := s.DomainCatalog(ctx)
	if err != nil {
		return err
	}
	if actual.Digest() != plan.SourceSchemaDigest() || !appliedMatches(actual.Applied, expected) {
		return ErrConflict
	}
	return nil
}
