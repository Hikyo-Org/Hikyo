package upgrade

import (
	"context"
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
	session  *Session
	expected State
	phase    Phase
}

func (r *candidateKeys) check(ctx context.Context) error {
	if r == nil || r.session == nil || r.expected.Pending == nil || r.expected.Pending.Phase != r.phase {
		return ErrConflict
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
