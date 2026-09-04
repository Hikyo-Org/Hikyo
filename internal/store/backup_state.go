package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// BackupState is the single-row disaster-recovery health record (#145): the
// latest successful export, the latest failure, the latest prune and the
// latest restore drill. A zero time means "never". Nothing in it is secret
// material: archive names, byte counts, versions and reasons only.
type BackupState struct {
	LastSuccessAt     time.Time
	LastArtifactName  string
	LastArtifactBytes int64

	LastFailureAt     time.Time
	LastFailureReason string

	LastPruneAt time.Time

	LastDrillAt            time.Time
	LastDrillOK            bool
	LastDrillArchive       string
	LastDrillElapsed       time.Duration
	LastDrillBinaryVersion string
	LastDrillSchemaVersion int64
}

// BackupDrillRecord is what a completed restore drill writes.
type BackupDrillRecord struct {
	At            time.Time
	OK            bool
	Archive       string
	Elapsed       time.Duration
	BinaryVersion string
	SchemaVersion int64
}

// BackupStateReader exposes the persisted DR health row.
type BackupStateReader interface {
	Get(ctx context.Context, p authz.Proof) (BackupState, error)
}

// BackupStateRepo owns the DR health row's writes. Each write touches only
// its own columns, so a failed prune cannot erase the last successful export.
type BackupStateRepo interface {
	BackupStateReader
	SetExportSuccess(ctx context.Context, p authz.Proof, at time.Time, artifactName string, artifactBytes int64) error
	SetExportFailure(ctx context.Context, p authz.Proof, at time.Time, reason string) error
	SetPruneSuccess(ctx context.Context, p authz.Proof, at time.Time) error
	SetDrill(ctx context.Context, p authz.Proof, rec BackupDrillRecord) error
}

type sqliteBackupState struct {
	q   *sqlitegen.Queries
	tok *authz.TxToken
}

func (r sqliteBackupState) Get(ctx context.Context, p authz.Proof) (BackupState, error) {
	if _, err := authz.Verify(p, authz.StoreBackupStateGet, r.tok); err != nil {
		return BackupState{}, err
	}
	row, err := r.q.GetBackupState(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return BackupState{}, ErrNotFound
	}
	if err != nil {
		return BackupState{}, err
	}
	out := BackupState{
		LastArtifactName: row.LastArtifactName, LastArtifactBytes: row.LastArtifactBytes,
		LastFailureReason: row.LastFailureReason,
		LastDrillOK:       row.LastDrillOk == 1, LastDrillArchive: row.LastDrillArchive,
		LastDrillElapsed:       time.Duration(row.LastDrillElapsedMs) * time.Millisecond,
		LastDrillBinaryVersion: row.LastDrillBinaryVersion, LastDrillSchemaVersion: row.LastDrillSchemaVersion,
	}
	for _, col := range []struct {
		name string
		raw  sql.NullString
		dst  *time.Time
	}{
		{"last_success_at", row.LastSuccessAt, &out.LastSuccessAt},
		{"last_failure_at", row.LastFailureAt, &out.LastFailureAt},
		{"last_prune_at", row.LastPruneAt, &out.LastPruneAt},
		{"last_drill_at", row.LastDrillAt, &out.LastDrillAt},
	} {
		if !col.raw.Valid {
			continue
		}
		at, err := parseTime("backup state", col.name, col.raw.String)
		if err != nil {
			return BackupState{}, err
		}
		*col.dst = at
	}
	return out, nil
}

func sqliteStamp(at time.Time) sql.NullString {
	return sql.NullString{String: CanonTime(at).Format(timeFormat), Valid: true}
}

func (r sqliteBackupState) SetExportSuccess(ctx context.Context, p authz.Proof, at time.Time, artifactName string, artifactBytes int64) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetExportSuccess, r.tok); err != nil {
		return err
	}
	return r.q.SetBackupExportSuccess(ctx, sqlitegen.SetBackupExportSuccessParams{
		LastSuccessAt: sqliteStamp(at), LastArtifactName: artifactName, LastArtifactBytes: artifactBytes,
	})
}

func (r sqliteBackupState) SetExportFailure(ctx context.Context, p authz.Proof, at time.Time, reason string) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetExportFailure, r.tok); err != nil {
		return err
	}
	return r.q.SetBackupExportFailure(ctx, sqlitegen.SetBackupExportFailureParams{
		LastFailureAt: sqliteStamp(at), LastFailureReason: reason,
	})
}

func (r sqliteBackupState) SetPruneSuccess(ctx context.Context, p authz.Proof, at time.Time) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetPruneSuccess, r.tok); err != nil {
		return err
	}
	return r.q.SetBackupPruneSuccess(ctx, sqliteStamp(at))
}

func (r sqliteBackupState) SetDrill(ctx context.Context, p authz.Proof, rec BackupDrillRecord) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetDrill, r.tok); err != nil {
		return err
	}
	ok := boolInt(rec.OK)
	return r.q.SetBackupDrill(ctx, sqlitegen.SetBackupDrillParams{
		LastDrillAt: sqliteStamp(rec.At), LastDrillOk: ok, LastDrillArchive: rec.Archive,
		LastDrillElapsedMs: rec.Elapsed.Milliseconds(), LastDrillBinaryVersion: rec.BinaryVersion,
		LastDrillSchemaVersion: rec.SchemaVersion,
	})
}

type pgBackupState struct {
	q   *pggen.Queries
	tok *authz.TxToken
}

func pgTimeToGo(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time.UTC()
}

func (r pgBackupState) Get(ctx context.Context, p authz.Proof) (BackupState, error) {
	if _, err := authz.Verify(p, authz.StoreBackupStateGet, r.tok); err != nil {
		return BackupState{}, err
	}
	row, err := r.q.GetBackupState(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return BackupState{}, ErrNotFound
	}
	if err != nil {
		return BackupState{}, err
	}
	return BackupState{
		LastSuccessAt: pgTimeToGo(row.LastSuccessAt), LastArtifactName: row.LastArtifactName, LastArtifactBytes: row.LastArtifactBytes,
		LastFailureAt: pgTimeToGo(row.LastFailureAt), LastFailureReason: row.LastFailureReason,
		LastPruneAt: pgTimeToGo(row.LastPruneAt),
		LastDrillAt: pgTimeToGo(row.LastDrillAt), LastDrillOK: row.LastDrillOk, LastDrillArchive: row.LastDrillArchive,
		LastDrillElapsed:       time.Duration(row.LastDrillElapsedMs) * time.Millisecond,
		LastDrillBinaryVersion: row.LastDrillBinaryVersion, LastDrillSchemaVersion: row.LastDrillSchemaVersion,
	}, nil
}

func (r pgBackupState) SetExportSuccess(ctx context.Context, p authz.Proof, at time.Time, artifactName string, artifactBytes int64) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetExportSuccess, r.tok); err != nil {
		return err
	}
	return r.q.SetBackupExportSuccess(ctx, pggen.SetBackupExportSuccessParams{
		LastSuccessAt: pgTimestamp(at), LastArtifactName: artifactName, LastArtifactBytes: artifactBytes,
	})
}

func (r pgBackupState) SetExportFailure(ctx context.Context, p authz.Proof, at time.Time, reason string) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetExportFailure, r.tok); err != nil {
		return err
	}
	return r.q.SetBackupExportFailure(ctx, pggen.SetBackupExportFailureParams{
		LastFailureAt: pgTimestamp(at), LastFailureReason: reason,
	})
}

func (r pgBackupState) SetPruneSuccess(ctx context.Context, p authz.Proof, at time.Time) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetPruneSuccess, r.tok); err != nil {
		return err
	}
	return r.q.SetBackupPruneSuccess(ctx, pgTimestamp(at))
}

func (r pgBackupState) SetDrill(ctx context.Context, p authz.Proof, rec BackupDrillRecord) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetDrill, r.tok); err != nil {
		return err
	}
	return r.q.SetBackupDrill(ctx, pggen.SetBackupDrillParams{
		LastDrillAt: pgTimestamp(rec.At), LastDrillOk: rec.OK, LastDrillArchive: rec.Archive,
		LastDrillElapsedMs: rec.Elapsed.Milliseconds(), LastDrillBinaryVersion: rec.BinaryVersion,
		LastDrillSchemaVersion: rec.SchemaVersion,
	})
}
