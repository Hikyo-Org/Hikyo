package upgrade

import (
	"context"
	"database/sql"
	"math"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/jackc/pgx/v5"
)

// RecoveryAdmission is confined to an authenticated, restored scratch target.
// It expires with the owning exclusion session or decrypted archive. It cannot
// be passed to runtime Open and does not authorize serving or background work.
type RecoveryAdmission struct{ state *recoveryState }
type recoveryKind uint8

const (
	authenticatedScratch recoveryKind = iota + 1
	restoredData
)

type recoveryState struct {
	kind        recoveryKind
	preparation PreparationAdmission
	archive     *backupreceipt.AuthenticatedArchive
	epoch       int64
}

func (s *Session) ScratchAdmission(ctx context.Context, archive *backupreceipt.AuthenticatedArchive, plan upgradecompat.Plan) (RecoveryAdmission, error) {
	if !archive.Valid() || !plan.Valid() || archive.PlanDigest() != plan.Digest() {
		return RecoveryAdmission{}, ErrConflict
	}
	prepared, err := s.PrepareExport(ctx, plan)
	if err != nil {
		return RecoveryAdmission{}, err
	}
	source := prepared.state.source
	archived := archive.Snapshot()
	if archived.Engine != s.engine || source.Source != archived.SourceIdentity || source.InstanceID != archived.InstanceID || source.SchemaDigest != archived.SourceSchemaSHA256 || source.MigrationDigest != archived.MigrationSHA256 || source.RestoreEpoch <= archived.RestoreEpoch {
		return RecoveryAdmission{}, ErrConflict
	}
	var credential, restored int64
	if err := s.conn.QueryRowContext(ctx, `SELECT credential_epoch,restore_epoch FROM auth_instance_state WHERE id=1`).Scan(&credential, &restored); err != nil {
		return RecoveryAdmission{}, err
	}
	if credential != restored || restored != source.RestoreEpoch {
		return RecoveryAdmission{}, ErrConflict
	}
	switch archived.Authority {
	case backupreceipt.LedgerAuthority:
		state := source.Ledger
		if state == nil || state.Pending == nil || state.Pending.Phase != RestoreRequired || !state.Pending.Invalidated || !state.Maintenance || archived.SourceGeneration == math.MaxInt64 || state.Generation != archived.SourceGeneration+1 {
			return RecoveryAdmission{}, ErrConflict
		}
		incarnation, err := state.RecoveryIncarnation.MarshalText()
		if err != nil || backupreceipt.Nonce(incarnation) == archived.RecoveryIncarnation {
			return RecoveryAdmission{}, ErrConflict
		}
	case backupreceipt.LegacyProposalAuthority:
		if source.Ledger != nil || source.Source.Genesis != LegacyGenesis {
			return RecoveryAdmission{}, ErrConflict
		}
	default:
		return RecoveryAdmission{}, ErrConflict
	}
	return RecoveryAdmission{state: &recoveryState{kind: authenticatedScratch, preparation: prepared, archive: archive, epoch: restored}}, nil
}
func (a RecoveryAdmission) Valid() bool {
	if a.state == nil || !a.state.preparation.Valid() {
		return false
	}
	switch a.state.kind {
	case authenticatedScratch:
		return a.state.archive.Valid()
	case restoredData:
		return a.state.archive == nil && a.state.epoch > 0
	default:
		return false
	}
}
func (a RecoveryAdmission) CheckOwner() error {
	if !a.Valid() {
		return ErrConflict
	}
	return a.state.preparation.state.session.check()
}

// CheckPostgresOwner rejects a lost physical migration owner before commit.
func (a RecoveryAdmission) CheckPostgresOwner(ctx context.Context) error {
	if err := a.CheckOwner(); err != nil {
		return err
	}
	return a.state.preparation.state.session.checkPostgresOwner(ctx)
}
func (a RecoveryAdmission) Check(ctx context.Context, cfg Config) error {
	if err := a.CheckOwner(); err != nil {
		return err
	}
	return a.state.preparation.Check(ctx, cfg)
}
func (a RecoveryAdmission) GuardSQLite(ctx context.Context, tx *sql.Tx) error {
	if err := a.CheckOwner(); err != nil {
		return err
	}
	if a.state.preparation.state.session.engine != releaseidentity.SQLite || tx == nil {
		return ErrConflict
	}
	return a.checkRows(func(q string) scanner { return tx.QueryRowContext(ctx, q) })
}
func (a RecoveryAdmission) GuardPostgres(ctx context.Context, tx pgx.Tx) error {
	if tx == nil {
		return ErrConflict
	}
	if err := a.CheckOwner(); err != nil {
		return err
	}
	if err := a.state.preparation.state.session.guardPostgresOperator(ctx, tx); err != nil {
		return err
	}
	if err := a.state.preparation.GuardPostgres(ctx, tx); err != nil {
		return err
	}
	return a.checkRows(func(q string) scanner { return tx.QueryRow(ctx, q) })
}
func (a RecoveryAdmission) checkRows(row func(string) scanner) error {
	source := a.state.preparation.state.source
	if source.Ledger != nil {
		state, err := scanState(row(snapshotSQL))
		if err != nil {
			return err
		}
		if !equalRecord(state, *source.Ledger) {
			return ErrConflict
		}
	}
	var instance string
	var credential, restored int64
	if err := row(`SELECT identity FROM instance_identity WHERE id=1`).Scan(&instance); err != nil {
		return err
	}
	if err := row(`SELECT credential_epoch,restore_epoch FROM auth_instance_state WHERE id=1`).Scan(&credential, &restored); err != nil {
		return err
	}
	if instance != source.InstanceID || credential != a.state.epoch || restored != a.state.epoch {
		return ErrConflict
	}
	return nil
}

// DataRecoveryAdmission admits only local inspection/reconciliation of an
// actually restored source under a verified route and the active session. It
// asserts no archive receipt and never permits ordinary runtime serving.
func (s *Session) DataRecoveryAdmission(ctx context.Context, plan upgradecompat.Plan) (RecoveryAdmission, error) {
	prepared, err := s.PrepareExport(ctx, plan)
	if err != nil {
		return RecoveryAdmission{}, err
	}
	var credential, restored int64
	if err := s.conn.QueryRowContext(ctx, `SELECT credential_epoch,restore_epoch FROM auth_instance_state WHERE id=1`).Scan(&credential, &restored); err != nil {
		return RecoveryAdmission{}, err
	}
	if restored <= 0 || credential != restored || prepared.state.source.RestoreEpoch != restored {
		return RecoveryAdmission{}, ErrConflict
	}
	return RecoveryAdmission{state: &recoveryState{kind: restoredData, preparation: prepared, epoch: restored}}, nil
}
