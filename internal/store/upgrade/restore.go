package upgrade

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"io"

	"github.com/jackc/pgx/v5"
)

// ReconcileSQLiteRestore runs inside the staged database restore transaction,
// after the existing strongest-credential-epoch increment and before publication.
// It neither starts nor commits a transaction and refuses absent/partial state.
func ReconcileSQLiteRestore(ctx context.Context, tx *sql.Tx) (State, error) {
	if tx == nil {
		return State{}, ErrCorrupt
	}
	return reconcileRestore(ctx, func(query string, args ...any) scanner { return tx.QueryRowContext(ctx, query, args...) }, func(query string, args ...any) (int64, error) {
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, err
		}
		return result.RowsAffected()
	}, rand.Reader)
}

// ReconcilePostgresRestore is bound to the actual row-import pgx transaction.
// Caller must roll back on every error, including entropy and counter failure.
func ReconcilePostgresRestore(ctx context.Context, tx pgx.Tx) (State, error) {
	if tx == nil {
		return State{}, ErrCorrupt
	}
	return reconcileRestore(ctx, func(query string, args ...any) scanner { return tx.QueryRow(ctx, query, args...) }, func(query string, args ...any) (int64, error) {
		result, err := tx.Exec(ctx, query, args...)
		return result.RowsAffected(), err
	}, rand.Reader)
}

func reconcileRestore(ctx context.Context, row func(string, ...any) scanner, exec func(string, ...any) (int64, error), entropy io.Reader) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	old, err := scanState(row(snapshotSQL))
	if err != nil {
		return State{}, err
	}
	var credential, restored int64
	if err := row(`SELECT credential_epoch,restore_epoch FROM auth_instance_state WHERE id=1`).Scan(&credential, &restored); err != nil {
		return State{}, err
	}
	if credential <= old.RestoreEpoch || credential != restored {
		return State{}, errors.New("upgrade: restore requires the existing strongest credential-epoch advance")
	}
	next := old
	next.Generation, err = nextGeneration(old.Generation)
	if err != nil {
		return State{}, err
	}
	if _, err := io.ReadFull(entropy, next.RecoveryIncarnation[:]); err != nil {
		return State{}, err
	}
	if next.RecoveryIncarnation == (Incarnation{}) || next.RecoveryIncarnation == old.RecoveryIncarnation {
		return State{}, ErrCorrupt
	}
	next.RestoreEpoch = credential
	next.Maintenance = true
	pending := *old.Pending
	pending.Invalidated = true
	pending.Phase = RestoreRequired
	next.Pending = &pending
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	incarnation, _ := next.RecoveryIncarnation.MarshalText()
	previous, _ := old.RecoveryIncarnation.MarshalText()
	// The old incarnation is in the predicate even when numeric counters repeat.
	n, err := exec(`UPDATE upgrade_control SET restore_epoch=$1,incarnation=$2,generation=$3,maintenance=1 WHERE singleton=1 AND incarnation=$4 AND generation=$5 AND restore_epoch=$6`, next.RestoreEpoch, string(incarnation), next.Generation, string(previous), old.Generation, old.RestoreEpoch)
	if err != nil {
		return State{}, err
	}
	if n != 1 {
		return State{}, ErrConflict
	}
	raw, err := json.Marshal(next.Pending)
	if err != nil {
		return State{}, err
	}
	n, err = exec(`UPDATE upgrade_pending SET operation_json=$1 WHERE singleton=1`, string(raw))
	if err != nil {
		return State{}, err
	}
	if n != 1 {
		return State{}, ErrConflict
	}
	return next, nil
}

// ReconcileSQLiteRestoreIfPresent also invalidates ledger state carried by an
// ordinary archive. The absence of a public upgrade receipt never exempts an
// existing ledger from restore invalidation. Legacy archives have no ledger.
func ReconcileSQLiteRestoreIfPresent(ctx context.Context, tx *sql.Tx) error {
	if tx == nil {
		return ErrCorrupt
	}
	catalog, err := inspectCatalog(ctx, tx, releaseidentity.SQLite)
	if err != nil {
		return err
	}
	if !controlPresent(catalog) {
		return nil
	}
	if _, err := withoutControl(catalog); err != nil {
		return err
	}
	_, err = ReconcileSQLiteRestore(ctx, tx)
	return err
}

func ReconcilePostgresRestoreIfPresent(ctx context.Context, tx pgx.Tx) error {
	if tx == nil {
		return ErrCorrupt
	}
	catalog, err := inspectCatalogWith(ctx, func(ctx context.Context, query string, args ...any) (catalogRows, error) {
		return tx.Query(ctx, query, args...)
	}, releaseidentity.Postgres)
	if err != nil {
		return err
	}
	if !controlPresent(catalog) {
		return nil
	}
	if _, err := withoutControl(catalog); err != nil {
		return err
	}
	_, err = ReconcilePostgresRestore(ctx, tx)
	return err
}
