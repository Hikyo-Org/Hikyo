package upgrade

import (
	"context"
	"database/sql"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/jackc/pgx/v5"
)

type SQLSnapshotQueries interface {
	SQLSnapshot
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type PGSnapshotQueries interface {
	PGSnapshot
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

const appliedSQL = `SELECT version_id,is_applied FROM goose_db_version ORDER BY id`

type migrationRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

// VerifySQLiteSnapshot checks the exact applied set on the caller's snapshot.
// The caller authenticates manifest bytes independently; this function does not
// turn a supplied manifest into release authority.
func VerifySQLiteSnapshot(ctx context.Context, q SQLSnapshotQueries, manifest releaseidentity.MigrationManifest) (State, error) {
	state, err := ReadSQLiteSnapshot(ctx, q)
	if err != nil {
		return State{}, err
	}
	if manifest.Engine != releaseidentity.SQLite {
		return State{}, ErrConflict
	}
	rows, err := q.QueryContext(ctx, appliedSQL)
	if err != nil {
		return State{}, err
	}
	defer rows.Close()
	if err := verifyApplied(rows, state, manifest); err != nil {
		return State{}, err
	}
	if err := rows.Close(); err != nil {
		return State{}, err
	}
	catalog, err := DomainCatalogSQLite(ctx, q)
	if err != nil {
		return State{}, err
	}
	if catalog.Digest() != state.SchemaDigest {
		return State{}, ErrConflict
	}
	if err := checkInstanceEpoch(state, func(query string) scanner { return q.QueryRowContext(ctx, query) }); err != nil {
		return State{}, err
	}
	return state, nil
}

func VerifyPostgresSnapshot(ctx context.Context, q PGSnapshotQueries, manifest releaseidentity.MigrationManifest) (State, error) {
	state, err := ReadPostgresSnapshot(ctx, q)
	if err != nil {
		return State{}, err
	}
	if manifest.Engine != releaseidentity.Postgres {
		return State{}, ErrConflict
	}
	rows, err := q.Query(ctx, appliedSQL)
	if err != nil {
		return State{}, err
	}
	defer rows.Close()
	if err := verifyApplied(rows, state, manifest); err != nil {
		return State{}, err
	}
	rows.Close()
	catalog, err := DomainCatalogPostgres(ctx, q)
	if err != nil {
		return State{}, err
	}
	if catalog.Digest() != state.SchemaDigest {
		return State{}, ErrConflict
	}
	if err := checkInstanceEpoch(state, func(query string) scanner { return q.QueryRow(ctx, query) }); err != nil {
		return State{}, err
	}
	return state, nil
}

func verifyApplied(rows migrationRows, state State, manifest releaseidentity.MigrationManifest) error {
	digest, err := manifest.Digest()
	if err != nil {
		return err
	}
	if state.MigrationDigest != digest {
		return ErrConflict
	}
	position := 0
	for rows.Next() {
		var version int64
		var applied bool
		if err := rows.Scan(&version, &applied); err != nil {
			return err
		}
		if !applied || position > len(manifest.Entries) {
			return ErrConflict
		}
		if position == 0 {
			if version != 0 {
				return ErrConflict
			}
		} else if version != int64(manifest.Entries[position-1].Version) {
			return ErrConflict
		}
		position++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if position != len(manifest.Entries)+1 {
		return ErrConflict
	}
	return nil
}
