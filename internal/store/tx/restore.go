package tx

// Restore, bound to the transaction that loads it (#76).
//
// These two wrappers exist for one reason and it is a correctness reason, not
// a tidiness one: the credential-epoch bump a restore performs MUST commit in
// the same act as the data it invalidates. A restore that committed the rows
// first and advanced the epoch second would leave a window — one crash wide —
// in which a reconstructed instance is reachable with every pre-restore
// bearer credential, session and single-use artifact still live. That is
// precisely the failure the whole restore checklist exists to prevent.
//
// They live here because this is the package whose job is binding an
// authorizer to a transaction. Doing it in the service layer would mean
// handing a pgx type upward, which the architecture forbids; doing it in
// internal/store would mean a raw write to a class=authn table outside the
// enumerated resolution surface.

import (
	"context"
	"database/sql"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// RestoreFn runs inside the restore's own transaction, against the restored
// state, before anything is published or committed.
type RestoreFn func(ctx context.Context, az *authz.TxAuthorizer) error

// RestoreSQLite reconstructs a sqlite datastore at path from archive, runs fn
// against the staged file, and only then publishes it under its final name.
func RestoreSQLite(ctx context.Context, archive io.Reader, path string, fn RestoreFn) (store.Manifest, error) {
	return store.RestoreSQLite(ctx, archive, path, func(ctx context.Context, sqltx *sql.Tx) error {
		tok := authz.NewTxToken()
		defer tok.Invalidate()
		if fn == nil {
			return errors.New("restore requires credential invalidation")
		}
		if err := fn(ctx, authz.NewTxAuthorizer(authn.NewSQLite(sqltx), tok)); err != nil {
			return err
		}
		return upgrade.ReconcileSQLiteRestoreIfPresent(ctx, sqltx)
	})
}

// RestoreUpgradeSQLite verifies the exact source before credential and ledger
// invalidation run in the staging transaction, before atomic publication.
func RestoreUpgradeSQLite(ctx context.Context, archive io.Reader, path string, plan upgradecompat.Plan, fn RestoreFn) (store.Manifest, error) {
	return store.RestoreUpgradeSQLite(ctx, archive, path, plan, func(ctx context.Context, sqltx *sql.Tx) error {
		tok := authz.NewTxToken()
		defer tok.Invalidate()
		if fn == nil {
			return errors.New("restore requires credential invalidation")
		}
		if err := fn(ctx, authz.NewTxAuthorizer(authn.NewSQLite(sqltx), tok)); err != nil {
			return err
		}
		return upgrade.ReconcileSQLiteRestoreIfPresent(ctx, sqltx)
	})
}

// Reconcile runs one per-principal reconciliation in its own write
// transaction. It exists so the service layer never has to reach for Write
// with a raw closure for something this security-relevant.
func Reconcile(ctx context.Context, db *store.DB, fn RestoreFn) error {
	return Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return fn(ctx, az)
	})
}

// RestoreUpgradeDestinationPostgres imports only the archive authenticated by
// the destination capability, with credential and incarnation invalidation in
// the same physical transaction. It never admits a runtime database.
func RestoreUpgradeDestinationPostgres(ctx context.Context, destination *store.RestoreDestination, fn RestoreFn) (store.Manifest, error) {
	return destination.RestorePostgres(ctx, func(ctx context.Context, pgtx pgx.Tx) error {
		tok := authz.NewTxToken()
		defer tok.Invalidate()
		if fn == nil {
			return errors.New("restore requires credential invalidation")
		}
		if err := fn(ctx, authz.NewTxAuthorizer(authn.NewPG(pgtx), tok)); err != nil {
			return err
		}
		return upgrade.ReconcilePostgresRestoreIfPresent(ctx, pgtx)
	})
}

// RestoreDataDestinationPostgres restores ordinary v1 data under the verified
// source schema and performs invalidation before the one atomic commit.
func RestoreDataDestinationPostgres(ctx context.Context, destination *store.DataRestoreDestination, archive io.Reader, fn RestoreFn) (store.Manifest, error) {
	return destination.RestorePostgres(ctx, archive, func(ctx context.Context, pgtx pgx.Tx) error {
		tok := authz.NewTxToken()
		defer tok.Invalidate()
		if fn == nil {
			return errors.New("restore requires credential invalidation")
		}
		if err := fn(ctx, authz.NewTxAuthorizer(authn.NewPG(pgtx), tok)); err != nil {
			return err
		}
		return upgrade.ReconcilePostgresRestoreIfPresent(ctx, pgtx)
	})
}

// RestoreDataSQLite validates the actual v1 source in the private staging
// transaction before credential invalidation and atomic file publication.
func RestoreDataSQLite(ctx context.Context, archive io.Reader, path string, plan upgradecompat.Plan, fn RestoreFn) (store.Manifest, error) {
	return store.RestoreDataSQLite(ctx, archive, path, plan, func(ctx context.Context, sqltx *sql.Tx) error {
		tok := authz.NewTxToken()
		defer tok.Invalidate()
		if fn == nil {
			return errors.New("restore requires credential invalidation")
		}
		if err := fn(ctx, authz.NewTxAuthorizer(authn.NewSQLite(sqltx), tok)); err != nil {
			return err
		}
		return upgrade.ReconcileSQLiteRestoreIfPresent(ctx, sqltx)
	})
}
