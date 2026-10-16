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
	LastSuccessAt      time.Time
	LastArtifactName   string
	LastArtifactBytes  int64
	LastArtifactSHA256 string

	LastFailureAt     time.Time
	LastFailureReason string

	LastPruneAt             time.Time
	LastPruneAttemptAt      time.Time
	LastPruneAttemptID      string
	PruneFailureReason      string
	RetainedExpiredArchives int64

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
	SetExportSuccess(ctx context.Context, p authz.Proof, at time.Time, artifactName string, artifactBytes int64, digest string) error
	SetExportFailure(ctx context.Context, p authz.Proof, at time.Time, reason string) error
	SetPruneResult(ctx context.Context, p authz.Proof, at time.Time, attemptID, reason string, expired int64) error
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
		LastArtifactName: row.LastArtifactName, LastArtifactBytes: row.LastArtifactBytes, LastArtifactSHA256: row.LastArtifactSha256,
		LastPruneAttemptID: row.LastPruneAttemptID,
		LastFailureReason:  row.LastFailureReason,
		PruneFailureReason: row.PruneFailureReason, RetainedExpiredArchives: row.RetainedExpiredArchives,
		LastDrillOK: row.LastDrillOk == 1, LastDrillArchive: row.LastDrillArchive,
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
		{"last_prune_attempt_at", row.LastPruneAttemptAt, &out.LastPruneAttemptAt},
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

func (r sqliteBackupState) SetExportSuccess(ctx context.Context, p authz.Proof, at time.Time, artifactName string, artifactBytes int64, digest string) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetExportSuccess, r.tok); err != nil {
		return err
	}
	n, err := r.q.SetBackupExportSuccess(ctx, sqlitegen.SetBackupExportSuccessParams{
		LastSuccessAt: sqliteStamp(at), LastArtifactName: artifactName, LastArtifactBytes: artifactBytes, LastArtifactSha256: digest,
	})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (r sqliteBackupState) SetExportFailure(ctx context.Context, p authz.Proof, at time.Time, reason string) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetExportFailure, r.tok); err != nil {
		return err
	}
	return r.q.SetBackupExportFailure(ctx, sqlitegen.SetBackupExportFailureParams{
		LastFailureAt: sqliteStamp(at), LastFailureReason: reason,
	})
}

func (r sqliteBackupState) SetPruneResult(ctx context.Context, p authz.Proof, at time.Time, attemptID, reason string, expired int64) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetPruneResult, r.tok); err != nil {
		return err
	}
	// Preserve the observed high-water timestamp across clock rollback. A new
	// attempt must not erase the only durable evidence that time is uncertain.
	prior, err := r.Get(ctx, p)
	if err != nil {
		return err
	}
	if prior.LastPruneAttemptAt.After(at) {
		at = prior.LastPruneAttemptAt
	}
	n, err := r.q.SetBackupPruneResult(ctx, sqlitegen.SetBackupPruneResultParams{AttemptAt: sqliteStamp(at), AttemptID: attemptID, FailureReason: reason, RetainedExpiredArchives: expired})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
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
		LastSuccessAt: pgTimeToGo(row.LastSuccessAt), LastArtifactName: row.LastArtifactName, LastArtifactBytes: row.LastArtifactBytes, LastArtifactSHA256: row.LastArtifactSha256,
		LastPruneAttemptID: row.LastPruneAttemptID,
		LastFailureAt:      pgTimeToGo(row.LastFailureAt), LastFailureReason: row.LastFailureReason,
		PruneFailureReason: row.PruneFailureReason, RetainedExpiredArchives: row.RetainedExpiredArchives,
		LastPruneAt: pgTimeToGo(row.LastPruneAt), LastPruneAttemptAt: pgTimeToGo(row.LastPruneAttemptAt),
		LastDrillAt: pgTimeToGo(row.LastDrillAt), LastDrillOK: row.LastDrillOk, LastDrillArchive: row.LastDrillArchive,
		LastDrillElapsed:       time.Duration(row.LastDrillElapsedMs) * time.Millisecond,
		LastDrillBinaryVersion: row.LastDrillBinaryVersion, LastDrillSchemaVersion: row.LastDrillSchemaVersion,
	}, nil
}

func (r pgBackupState) SetExportSuccess(ctx context.Context, p authz.Proof, at time.Time, artifactName string, artifactBytes int64, digest string) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetExportSuccess, r.tok); err != nil {
		return err
	}
	n, err := r.q.SetBackupExportSuccess(ctx, pggen.SetBackupExportSuccessParams{
		LastSuccessAt: pgTimestamp(at), LastArtifactName: artifactName, LastArtifactBytes: artifactBytes, LastArtifactSha256: digest,
	})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (r pgBackupState) SetExportFailure(ctx context.Context, p authz.Proof, at time.Time, reason string) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetExportFailure, r.tok); err != nil {
		return err
	}
	return r.q.SetBackupExportFailure(ctx, pggen.SetBackupExportFailureParams{
		LastFailureAt: pgTimestamp(at), LastFailureReason: reason,
	})
}

func (r pgBackupState) SetPruneResult(ctx context.Context, p authz.Proof, at time.Time, attemptID, reason string, expired int64) error {
	if _, err := authz.Verify(p, authz.StoreBackupStateSetPruneResult, r.tok); err != nil {
		return err
	}
	// Preserve the observed high-water timestamp across clock rollback. A new
	// attempt must not erase the only durable evidence that time is uncertain.
	prior, err := r.Get(ctx, p)
	if err != nil {
		return err
	}
	if prior.LastPruneAttemptAt.After(at) {
		at = prior.LastPruneAttemptAt
	}
	n, err := r.q.SetBackupPruneResult(ctx, pggen.SetBackupPruneResultParams{AttemptAt: pgTimestamp(at), AttemptID: attemptID, FailureReason: reason, RetainedExpiredArchives: expired})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
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
