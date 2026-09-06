package upgrade

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/stdlib"
)

// CandidateKeys exposes only existing encrypted key wrappers while the exact
// candidate remains schema-applied under migration exclusion. It cannot mint
// keys, query tenant values, or construct a runtime datastore. Each call checks
// the durable operation again; the reader becomes unusable after the session
// exits or the phase/generation changes.
func (s *Session) CandidateKeys(ctx context.Context, expected State) (*candidateKeys, error) {
	return s.existingKeys(ctx, expected, SchemaApplied)
}

// HealthyKeys exposes the same read-only inventory during an exact healthy
// restart, before runtime admission or keyring initialization. Maintenance and
// intermediate route hops cannot obtain this capability.
func (s *Session) HealthyKeys(ctx context.Context, expected State) (*candidateKeys, error) {
	return s.existingKeys(ctx, expected, Healthy)
}

func (s *Session) existingKeys(ctx context.Context, expected State, phase Phase) (*candidateKeys, error) {
	// Keep the capability's authority snapshot independent of caller mutation.
	if expected.Pending != nil {
		pending := *expected.Pending
		if pending.Acceptance.Attestation != nil {
			attestation := *pending.Acceptance.Attestation
			pending.Acceptance.Attestation = &attestation
		}
		expected.Pending = &pending
	}
	reader := &candidateKeys{session: s, expected: expected, phase: phase}
	if err := reader.check(ctx); err != nil {
		return nil, err
	}
	return reader, nil
}

type candidateKeys struct {
	session          *Session
	expected         State
	phase            Phase
	operatorRecovery bool
}

func (r *candidateKeys) check(ctx context.Context) error {
	if r == nil || r.session == nil || r.expected.Pending == nil || r.expected.Pending.Phase != r.phase {
		return ErrConflict
	}
	if r.operatorRecovery {
		if err := r.session.check(); err != nil {
			return err
		}
		current, err := r.session.Read(ctx)
		if err != nil {
			return err
		}
		if !equalRecord(current, r.expected) {
			return ErrConflict
		}
		return nil
	}
	switch r.phase {
	case SchemaApplied:
		if !r.expected.Maintenance {
			return ErrConflict
		}
	case Healthy:
		if r.expected.Maintenance {
			return ErrConflict
		}
	default:
		return ErrConflict
	}
	if _, err := r.session.Resume(ctx, r.expected); err != nil {
		return err
	}
	return nil
}

func (r *candidateKeys) ActiveMasterWrappers(ctx context.Context) ([]crypto.WrappedKey, error) {
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	var out []crypto.WrappedKey
	if r.session.engine == releaseidentity.SQLite {
		rows, err := sqlitegen.New(r.session.conn).GetActiveMasterKeys(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
			if err != nil {
				return nil, errors.New("upgrade: invalid candidate master timestamp")
			}
			key, err := candidateMaster(row.Version, row.RootKeyEpoch, row.Blob, created)
			if err != nil {
				return nil, err
			}
			out = append(out, key)
		}
		return out, nil
	}
	// Reuse the canonical PostgreSQL generated queries on the same maintained
	// pgx connection. The handle and generated query object stay inside Raw.
	err := r.session.conn.Raw(func(value any) error {
		conn, ok := value.(*stdlib.Conn)
		if !ok {
			return errors.New("upgrade: unexpected candidate PostgreSQL driver")
		}
		rows, err := pggen.New(conn.Conn()).GetActiveMasterKeys(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if !row.CreatedAt.Valid {
				return errors.New("upgrade: missing candidate master timestamp")
			}
			key, err := candidateMaster(row.Version, row.RootKeyEpoch, row.Blob, row.CreatedAt.Time)
			if err != nil {
				return err
			}
			out = append(out, key)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *candidateKeys) AllOpenableTier3(ctx context.Context) ([]crypto.WrappedKey, error) {
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	var out []crypto.WrappedKey
	if r.session.engine == releaseidentity.SQLite {
		rows, err := sqlitegen.New(r.session.conn).AllOpenableTier3(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
			if err != nil {
				return nil, errors.New("upgrade: invalid candidate tier-3 timestamp")
			}
			key, err := candidateTier3(row.ID, row.Purpose, row.OrgID, row.ProjectID, row.Version, row.MasterKeyVersion, row.Blob, created)
			if err != nil {
				return nil, err
			}
			out = append(out, key)
		}
		return out, nil
	}
	err := r.session.conn.Raw(func(value any) error {
		conn, ok := value.(*stdlib.Conn)
		if !ok {
			return errors.New("upgrade: unexpected candidate PostgreSQL driver")
		}
		rows, err := pggen.New(conn.Conn()).AllOpenableTier3(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if !row.CreatedAt.Valid {
				return errors.New("upgrade: missing candidate tier-3 timestamp")
			}
			key, err := candidateTier3(row.ID, row.Purpose, row.OrgID, row.ProjectID, row.Version, row.MasterKeyVersion, row.Blob, row.CreatedAt.Time)
			if err != nil {
				return err
			}
			out = append(out, key)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func candidateMaster(version, epoch int64, blob []byte, created time.Time) (crypto.WrappedKey, error) {
	if version < 0 || version > math.MaxUint32 || epoch < 0 || epoch > math.MaxUint32 {
		return crypto.WrappedKey{}, errors.New("upgrade: candidate master version out of range")
	}
	return crypto.WrappedKey{Version: uint32(version), RootKeyEpoch: uint32(epoch), Blob: blob, CreatedAt: created.UTC()}, nil
}
func candidateTier3(id, purpose, org, project string, version, master int64, blob []byte, created time.Time) (crypto.WrappedKey, error) {
	if version < 0 || version > math.MaxUint32 || master < 0 || master > math.MaxUint32 {
		return crypto.WrappedKey{}, errors.New("upgrade: candidate tier-3 version out of range")
	}
	p := crypto.Purpose(purpose)
	switch p {
	case crypto.PurposeInstance, crypto.PurposeToken, crypto.PurposeScanning, crypto.PurposeProject:
	default:
		return crypto.WrappedKey{}, errors.New("upgrade: unknown candidate key purpose")
	}
	return crypto.WrappedKey{ID: id, Purpose: p, OrgID: org, ProjectID: project, Version: uint32(version), MasterKeyVersion: uint32(master), Blob: blob, CreatedAt: created.UTC()}, nil
}

// Configuration reads the exact saved policy under the same session/phase fence
// as the key inventory. Nil means an unadopted (or pre-feature) datastore.
func (r *candidateKeys) Configuration(ctx context.Context) (*CandidateConfiguration, error) {
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	if r.operatorRecovery {
		return nil, ErrConflict
	}
	var tables int
	query := "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='self_config_binding'"
	if r.session.engine == releaseidentity.Postgres {
		query = "SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='self_config_binding'"
	}
	if err := r.session.conn.QueryRowContext(ctx, query).Scan(&tables); err != nil {
		return nil, err
	}
	if tables == 0 {
		return nil, nil
	}
	var owner, org, project, environment, snapshot string
	var revision, schemaVersion, generation int64
	read := func() error {
		return r.session.conn.QueryRowContext(ctx, "SELECT owner_instance_id,org_id,project_id,environment_id,desired_snapshot_id,desired_revision,schema_version,generation FROM self_config_binding WHERE id=1").Scan(&owner, &org, &project, &environment, &snapshot, &revision, &schemaVersion, &generation)
	}
	if err := read(); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if owner != r.expected.InstanceID || org == "" || project == "" || environment == "" || snapshot == "" || revision < 1 || schemaVersion < 1 || generation < 1 {
		return nil, ErrConflict
	}
	expectedOwner, expectedOrg, expectedProject, expectedEnvironment, expectedSnapshot, expectedRevision, expectedSchema, expectedGeneration := owner, org, project, environment, snapshot, revision, schemaVersion, generation
	q := "SELECT count(*) FROM snapshots WHERE org_id=$1 AND project_id=$2 AND environment_id=$3 AND id=$4 AND revision=$5 AND payload_present=TRUE"
	var present int
	if err := r.session.conn.QueryRowContext(ctx, q, org, project, environment, snapshot, revision).Scan(&present); err != nil {
		return nil, err
	}
	if present != 1 {
		return nil, ErrConflict
	}
	projection := &CandidateConfiguration{OrgID: org, ProjectID: project, SchemaVersion: int(schemaVersion)}
	rows, err := r.session.conn.QueryContext(ctx, "SELECT name,classification,declaration,required_mode,forbidden_mode,coalesce(group_id,''),folder_path FROM keys WHERE org_id=$1 AND project_id=$2 ORDER BY name", org, project)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key CandidateConfigurationKey
		if err := rows.Scan(&key.Name, &key.Classification, &key.Declaration, &key.RequiredMode, &key.ForbiddenMode, &key.GroupID, &key.FolderPath); err != nil {
			rows.Close()
			return nil, err
		}
		projection.Catalogue = append(projection.Catalogue, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = r.session.conn.QueryContext(ctx, "SELECT e.id,e.key_id,e.key_name,e.ciphertext FROM snapshot_entries e JOIN keys k ON k.org_id=e.org_id AND k.project_id=e.project_id AND k.id=e.key_id AND k.name=e.key_name AND k.classification=e.classification WHERE e.org_id=$1 AND e.project_id=$2 AND e.environment_id=$3 AND e.snapshot_id=$4 ORDER BY e.key_name", org, project, environment, snapshot)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		field := crypto.ExistingProjectField{AAD: crypto.ProjectFieldAAD{OrgID: org, ProjectID: project, EnvironmentID: environment, SnapshotID: snapshot, OwnerTable: "snapshot_entries", FieldTag: "snapshot_value"}}
		if err := rows.Scan(&field.AAD.OwnerRowID, &field.AAD.KeyID, &field.Name, &field.Ciphertext); err != nil {
			rows.Close()
			return nil, err
		}
		projection.Fields = append(projection.Fields, field)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// A mismatching catalogue join must not silently omit a persisted field.
	var count int
	if err := r.session.conn.QueryRowContext(ctx, "SELECT count(*) FROM snapshot_entries WHERE snapshot_id=$1", snapshot).Scan(&count); err != nil {
		return nil, err
	}
	if count != len(projection.Fields) {
		return nil, ErrConflict
	}
	if err := read(); err != nil {
		return nil, err
	}
	if owner != expectedOwner || org != expectedOrg || project != expectedProject || environment != expectedEnvironment || snapshot != expectedSnapshot || revision != expectedRevision || schemaVersion != expectedSchema || generation != expectedGeneration {
		return nil, ErrConflict
	}
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	return projection, nil
}
