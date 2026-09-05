package upgrade

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/jackc/pgx/v5"
)

const controlDDL = `CREATE TABLE upgrade_control (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
 format INTEGER NOT NULL CHECK (format = 1),
 floor_json TEXT NOT NULL,
 release_root_digest TEXT NOT NULL,
 instance_id TEXT NOT NULL,
 trust_domain TEXT NOT NULL CHECK (trust_domain IN ('production','local-development')),
 applied_json TEXT NOT NULL,
 migration_digest TEXT NOT NULL,
 schema_digest TEXT NOT NULL,
 restore_epoch BIGINT NOT NULL CHECK (restore_epoch >= 0),
 incarnation TEXT NOT NULL,
 generation BIGINT NOT NULL CHECK (generation > 0),
 maintenance INTEGER NOT NULL CHECK (maintenance IN (0,1))
)`

const pendingDDL = `CREATE TABLE upgrade_pending (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1) REFERENCES upgrade_control(singleton),
 operation_json TEXT NOT NULL
)`

const snapshotSQL = `SELECT c.format, c.floor_json, c.release_root_digest, c.instance_id, c.trust_domain, c.applied_json, c.migration_digest,c.schema_digest,
 c.restore_epoch, c.incarnation, c.generation, c.maintenance, p.operation_json
 FROM upgrade_control c LEFT JOIN upgrade_pending p ON p.singleton=c.singleton WHERE c.singleton=1`

type SQLSnapshot interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type PGSnapshot interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type scanner interface{ Scan(...any) error }

// ReadSQLiteSnapshot reads an existing snapshot, never opens a new connection.
// The caller retains its snapshot transaction for the complete archive export.
func ReadSQLiteSnapshot(ctx context.Context, q SQLSnapshot) (State, error) {
	return scanState(q.QueryRowContext(ctx, snapshotSQL))
}

// ReadPostgresSnapshot is the equivalent seam for the native pgx export tx.
func ReadPostgresSnapshot(ctx context.Context, q PGSnapshot) (State, error) {
	return scanState(q.QueryRow(ctx, snapshotSQL))
}

func scanState(row scanner) (State, error) {
	var s State
	var format, maintenance int
	var applied, incarnation, floor string
	var pending sql.NullString
	err := row.Scan(&format, &floor, &s.ReleaseRootDigest, &s.InstanceID, &s.TrustDomain, &applied, &s.MigrationDigest, &s.SchemaDigest, &s.RestoreEpoch, &incarnation, &s.Generation, &maintenance, &pending)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return State{}, ErrAbsent
	}
	if err != nil {
		return State{}, fmt.Errorf("upgrade: read control: %w", err)
	}
	if format != 1 || (maintenance != 0 && maintenance != 1) || !pending.Valid {
		return State{}, ErrCorrupt
	}
	s.Maintenance = maintenance == 1
	if err := decode(floor, &s.Floor); err != nil {
		return State{}, err
	}
	if err := s.RecoveryIncarnation.UnmarshalText([]byte(incarnation)); err != nil {
		return State{}, ErrCorrupt
	}
	if err := decode(applied, &s.Applied); err != nil {
		return State{}, err
	}
	s.Pending = new(Operation)
	if err := decode(pending.String, s.Pending); err != nil {
		return State{}, err
	}
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	return s, nil
}

func decode(raw string, dest any) error {
	if len(raw) > 16384 {
		return ErrCorrupt
	}
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil {
		return fmt.Errorf("%w: JSON", ErrCorrupt)
	}
	if err := d.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return ErrCorrupt
	}
	// Records are written canonically by this package. Reject duplicate keys,
	// absent fields, null/default substitutions and alternate shapes on read.
	canonical, err := json.Marshal(dest)
	if err != nil || string(canonical) != raw {
		return ErrCorrupt
	}
	return nil
}

func (s *Session) Read(ctx context.Context) (State, error) {
	if err := s.check(); err != nil {
		return State{}, err
	}
	return ReadSQLiteSnapshot(ctx, s.conn)
}

// transaction is always on the connection owning migration exclusion. SQLite
// BEGIN IMMEDIATE takes writer exclusion before reading the CAS source. The
// PostgreSQL row lock will also conflict with F3's runtime FOR SHARE locks.
func (s *Session) transaction(ctx context.Context, fn func() error) (err error) {
	if err := s.check(); err != nil {
		return err
	}
	begin := "BEGIN"
	if s.engine == releaseidentity.SQLite {
		begin = "BEGIN IMMEDIATE"
	}
	if _, err := s.conn.ExecContext(ctx, begin); err != nil {
		return err
	}
	committed := false
	defer func() {
		if err != nil && !committed {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, rollback := s.conn.ExecContext(cleanup, "ROLLBACK")
			err = errors.Join(err, rollback)
		}
	}()
	if err = fn(); err != nil {
		return err
	}
	if s.beforeCommit != nil {
		if err = s.beforeCommit(); err != nil {
			return err
		}
	}
	_, err = s.conn.ExecContext(ctx, "COMMIT")
	committed = err == nil
	if err == nil && s.afterCommit != nil {
		err = s.afterCommit()
	}
	return err
}

func (s *Session) compare(ctx context.Context, expected State) error {
	query := snapshotSQL
	if s.engine == releaseidentity.Postgres {
		query += " FOR UPDATE OF c"
	}
	current, err := scanState(s.conn.QueryRowContext(ctx, query))
	if err != nil {
		return err
	}
	if !equalRecord(current, expected) {
		return ErrConflict
	}
	return nil
}

func (s *Session) persist(ctx context.Context, state State, insert bool) error {
	if err := state.Validate(); err != nil {
		return err
	}
	applied, err := json.Marshal(state.Applied)
	if err != nil {
		return err
	}
	pending, err := json.Marshal(state.Pending)
	if err != nil {
		return err
	}
	incarnation, _ := state.RecoveryIncarnation.MarshalText()
	maintenance := 0
	if state.Maintenance {
		maintenance = 1
	}
	query := `UPDATE upgrade_control SET instance_id=$1,applied_json=$2,migration_digest=$3,restore_epoch=$4,incarnation=$5,generation=$6,maintenance=$7,trust_domain=$8,floor_json=$9,release_root_digest=$10,schema_digest=$11 WHERE singleton=1`
	if insert {
		query = `INSERT INTO upgrade_control(singleton,format,instance_id,applied_json,migration_digest,restore_epoch,incarnation,generation,maintenance,trust_domain,floor_json,release_root_digest,schema_digest) VALUES(1,1,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	}
	result, err := s.conn.ExecContext(ctx, query, state.InstanceID, string(applied), string(state.MigrationDigest), state.RestoreEpoch, string(incarnation), state.Generation, maintenance, string(state.TrustDomain), encodeFloor(state.Floor), string(state.ReleaseRootDigest), string(state.SchemaDigest))
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return ErrConflict
	}
	query = `UPDATE upgrade_pending SET operation_json=$1 WHERE singleton=1`
	if insert {
		query = `INSERT INTO upgrade_pending(singleton,operation_json) VALUES(1,$1)`
	}
	result, err = s.conn.ExecContext(ctx, query, string(pending))
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return ErrConflict
	}
	return nil
}

// Prepare starts a new immutable operation from a completed release. It does not
// verify the release/route/backup proof, and grants no runtime capability.
func (s *Session) Prepare(ctx context.Context, expected State, operation Operation) (State, error) {
	if operation.Kind == "" {
		operation.Kind = UpgradeOperation
	}
	if operation.Kind != UpgradeOperation {
		return State{}, ErrConflict
	}
	next := expected
	generation, err := nextGeneration(expected.Generation)
	if expected.Pending != nil && expected.Pending.Phase == Healthy && expected.Maintenance {
		previous := expected.Pending
		if operation.RouteSource != previous.RouteSource || operation.RouteDigest != previous.RouteDigest || operation.RouteLength != previous.RouteLength || operation.Hop != previous.Hop+1 || operation.BackupID != previous.BackupID {
			return State{}, ErrConflict
		}
		generation, err = expected.Generation, nil
	} else if operation.Hop != 0 {
		return State{}, ErrConflict
	}
	if err != nil {
		return State{}, err
	}
	if expected.Pending == nil || expected.Pending.Phase != Healthy || operation.Source != expected.Applied || operation.SourceSchemaDigest != expected.SchemaDigest || operation.SourceMigrationDigest != expected.MigrationDigest || operation.Generation != generation || operation.RecoveryIncarnation != expected.RecoveryIncarnation || operation.Phase != Prepared || operation.Invalidated {
		return State{}, ErrConflict
	}
	next.Pending = &operation
	next.Generation, next.Maintenance = generation, true
	err = s.transaction(ctx, func() error {
		if err := s.compare(ctx, expected); err != nil {
			return err
		}
		if err := s.accept(ctx, &expected, &next); err != nil {
			return err
		}
		return s.persist(ctx, next, false)
	})
	if err != nil {
		return State{}, err
	}
	return next, nil
}

// PrepareAfterRestore begins a NEW operation after independent restored-source
// verification and new backup proof in F5. It is never same-operation resume.
// Schema-write ambiguity is not resolved here: the caller must authenticate and
// compare the actual restored schema before using this storage primitive.
func (s *Session) PrepareAfterRestore(ctx context.Context, expected State, operation Operation) (State, error) {
	if operation.Kind == "" {
		operation.Kind = UpgradeOperation
	}
	if operation.Kind != UpgradeOperation {
		return State{}, ErrConflict
	}
	if expected.Pending == nil || !expected.Pending.Invalidated || expected.Pending.Phase != RestoreRequired || !expected.Maintenance {
		return State{}, ErrConflict
	}
	generation, err := nextGeneration(expected.Generation)
	if err != nil {
		return State{}, err
	}
	if operation.Source != expected.Applied || operation.SourceSchemaDigest != expected.SchemaDigest || operation.SourceMigrationDigest != expected.MigrationDigest || operation.Generation != generation || operation.RecoveryIncarnation != expected.RecoveryIncarnation || operation.Phase != Prepared || operation.Hop != 0 || operation.Invalidated || operation.BackupID == "" || operation.BackupID == expected.Pending.BackupID {
		return State{}, ErrConflict
	}
	next := expected
	next.Pending = &operation
	next.Generation = generation
	err = s.transaction(ctx, func() error {
		if err := s.compare(ctx, expected); err != nil {
			return err
		}
		if err := s.accept(ctx, &expected, &next); err != nil {
			return err
		}
		return s.persist(ctx, next, false)
	})
	if err != nil {
		return State{}, err
	}
	return next, nil
}

// Advance commits each boundary separately. Even a commit acknowledgement
// failure returns no result: callers must reread and conservatively treat an
// uncertain write boundary as post-write. Healthy is only F5's post-health act.
func (s *Session) Advance(ctx context.Context, expected State, phase Phase) (State, error) {
	if expected.Pending == nil || expected.Pending.Invalidated {
		return State{}, ErrConflict
	}
	from := expected.Pending.Phase
	legal := (from == Prepared && phase == SchemaWriteStarted) || (from == SchemaWriteStarted && phase == SchemaApplied) || (from == SchemaApplied && phase == Healthy) || ((from == Prepared || from == SchemaWriteStarted || from == SchemaApplied) && phase == RestoreRequired)
	if !legal {
		return State{}, ErrConflict
	}
	next := expected
	pending := *expected.Pending
	pending.Phase = phase
	next.Pending = &pending
	if phase == Healthy {
		next.Applied = Source{Release: pending.Target}
		next.MigrationDigest = pending.TargetMigrationDigest
		next.SchemaDigest = pending.TargetSchemaDigest
		next.Maintenance = pending.Hop+1 < pending.RouteLength
	}
	err := s.transaction(ctx, func() error {
		if err := s.compare(ctx, expected); err != nil {
			return err
		}
		return s.persist(ctx, next, false)
	})
	if err != nil {
		return State{}, err
	}
	return next, nil
}

// Resume only reconstructs the same operation. It does not advance or clear
// maintenance and cannot resume an operation invalidated by restore.
func (s *Session) Resume(ctx context.Context, expected State) (State, error) {
	current, err := s.Read(ctx)
	if err != nil {
		return State{}, err
	}
	if !equalRecord(current, expected) || current.Pending.Invalidated || current.Pending.Phase == RestoreRequired {
		return State{}, ErrConflict
	}
	return current, nil
}

// Compare persisted representations: time.Time's process-local monotonic clock
// and location pointer are not durable authority and disappear on JSON decode.
func equalRecord[T State | *AttestationUse](left, right T) bool {
	a, err := json.Marshal(left)
	if err != nil {
		return false
	}
	b, err := json.Marshal(right)
	return err == nil && bytes.Equal(a, b)
}

// RefreshTrust preserves newly authenticated trust floors on an exact healthy
// restart. It never changes release/schema identity, generation or maintenance,
// consumes no backup evidence and does not authorize a different release root.
func (s *Session) RefreshTrust(ctx context.Context, expected State, floor releaseidentity.SnapshotFloor, root releaseidentity.Digest) (State, error) {
	if expected.Pending == nil || expected.Pending.Phase != Healthy || expected.Pending.Invalidated || root != expected.ReleaseRootDigest {
		return State{}, ErrConflict
	}
	if err := expected.Floor.Advance(floor); err != nil {
		return State{}, err
	}
	next := expected
	pending := *expected.Pending
	pending.Acceptance.Floor = floor
	next.Pending = &pending
	next.Floor = floor
	err := s.transaction(ctx, func() error {
		if err := s.compare(ctx, expected); err != nil {
			return err
		}
		return s.persist(ctx, next, false)
	})
	if err != nil {
		return State{}, err
	}
	return next, nil
}
