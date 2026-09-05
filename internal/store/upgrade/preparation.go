package upgrade

import (
	"context"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/jackc/pgx/v5"
)

// PreparationAdmission permits only a local source inspection and encrypted
// backup export while its migration session remains owned. It never authorizes
// runtime repositories, credentials or network workers.
type PreparationAdmission struct{ state *preparationState }
type preparationState struct {
	session *Session
	source  InstalledSource
	plan    upgradecompat.Plan
}

func (a PreparationAdmission) Valid() bool {
	return a.state != nil && a.state.session != nil && a.state.session.active
}

func (s *Session) PrepareExport(ctx context.Context, plan upgradecompat.Plan) (PreparationAdmission, error) {
	if err := s.check(); err != nil {
		return PreparationAdmission{}, err
	}
	if !plan.Valid() {
		return PreparationAdmission{}, ErrConflict
	}
	manifest, err := plan.SourceManifest(s.engine)
	if err != nil {
		return PreparationAdmission{}, err
	}
	catalog, err := inspectCatalog(ctx, s.conn, s.engine)
	if err != nil {
		return PreparationAdmission{}, err
	}
	source, err := inspectSource(catalog, manifest, func(q string, args ...any) scanner { return s.conn.QueryRowContext(ctx, q, args...) })
	if err != nil {
		return PreparationAdmission{}, err
	}
	if source.Source != plan.Source() || source.SchemaDigest != plan.SourceSchemaDigest() {
		return PreparationAdmission{}, ErrConflict
	}
	if source.Ledger != nil {
		state := source.Ledger
		healthy := state.Pending.Phase == Healthy && !state.Maintenance && !state.Pending.Invalidated
		restored := state.Pending.Phase == RestoreRequired && state.Maintenance && state.Pending.Invalidated
		if !healthy && !restored {
			return PreparationAdmission{}, ErrConflict
		}
	}
	return PreparationAdmission{state: &preparationState{session: s, source: source, plan: plan}}, nil
}
func (a PreparationAdmission) Check(ctx context.Context, cfg Config) error {
	if !a.Valid() {
		return ErrConflict
	}
	s := a.state.session
	if err := s.check(); err != nil {
		return err
	}
	if cfg.Engine != s.engine {
		return ErrConflict
	}
	if cfg.Engine == releaseidentity.SQLite {
		path, err := canonicalSQLite(cfg.Path)
		if err != nil {
			return err
		}
		if path != s.path {
			return ErrConflict
		}
	} else if cfg.DSN != s.dsn {
		return ErrConflict
	}
	manifest, err := a.state.plan.SourceManifest(s.engine)
	if err != nil {
		return err
	}
	catalog, err := inspectCatalog(ctx, s.conn, s.engine)
	if err != nil {
		return err
	}
	source, err := inspectSource(catalog, manifest, func(q string, args ...any) scanner { return s.conn.QueryRowContext(ctx, q, args...) })
	if err != nil {
		return err
	}
	if source.Source != a.state.source.Source || source.InstanceID != a.state.source.InstanceID || source.RestoreEpoch != a.state.source.RestoreEpoch || source.MigrationDigest != a.state.source.MigrationDigest || source.SchemaDigest != a.state.source.SchemaDigest {
		return ErrConflict
	}
	if (source.Ledger == nil) != (a.state.source.Ledger == nil) {
		return ErrConflict
	}
	if source.Ledger != nil && !equalRecord(*source.Ledger, *a.state.source.Ledger) {
		return ErrConflict
	}
	return nil
}
func (a PreparationAdmission) PlanDigest() releaseidentity.Digest {
	if !a.Valid() {
		return ""
	}
	return a.state.plan.Digest()
}
func (a PreparationAdmission) GuardPostgres(ctx context.Context, tx pgx.Tx) error {
	if !a.Valid() || a.state.session.engine != releaseidentity.Postgres || tx == nil {
		return ErrConflict
	}
	if a.state.source.Ledger == nil {
		return nil
	}
	current, err := scanState(tx.QueryRow(ctx, snapshotSQL+" FOR SHARE OF c"))
	if err != nil {
		return err
	}
	if !equalRecord(current, *a.state.source.Ledger) {
		return ErrConflict
	}
	return checkInstanceEpoch(current, func(q string) scanner { return tx.QueryRow(ctx, q) })
}
