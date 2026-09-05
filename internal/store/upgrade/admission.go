package upgrade

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/gofrs/flock"
	"github.com/jackc/pgx/v5"
)

// Admission is immutable runtime authority. Its zero value never grants access.
// Only a locked session can mint it from an actually verified release node and
// the exact healthy installation. State claims alone cannot construct one.
type Admission struct{ state *admissionState }
type admissionState struct {
	expected State
	engine   releaseidentity.Engine
	path     string
	file     os.FileInfo
}

func (a Admission) Valid() bool { return a.state != nil }

// RecoveryIdentity returns detached public identity metadata from this exact
// admission. Domain callers use it only inside a guarded transaction, which
// refuses if a restore has changed the admitted incarnation.
func (a Admission) RecoveryIdentity() (instance, incarnation string, err error) {
	if !a.Valid() {
		return "", "", ErrConflict
	}
	incarnationBytes, err := a.state.expected.RecoveryIncarnation.MarshalText()
	if err != nil {
		return "", "", err
	}
	return a.state.expected.InstanceID, string(incarnationBytes), nil
}

// Admit does not replace build/trust-domain verification at the application
// gate. It binds that gate's real authenticated node to the actual database,
// complete migration inventory and schema under the same migration exclusion.
func (s *Session) Admit(ctx context.Context, expected State, node upgradecompat.VerifiedNode) (Admission, error) {
	if err := s.check(); err != nil {
		return Admission{}, err
	}
	if !node.Valid() || expected.Validate() != nil || expected.Maintenance || expected.Pending.Phase != Healthy || expected.Pending.Invalidated || expected.Applied != (Source{Release: node.Identity()}) {
		return Admission{}, ErrConflict
	}
	manifest, err := node.Manifest(s.engine)
	if err != nil {
		return Admission{}, err
	}
	digest, err := manifest.Digest()
	if err != nil || digest != expected.MigrationDigest {
		return Admission{}, ErrConflict
	}
	schema, err := node.SchemaDigest(s.engine)
	if err != nil || schema != expected.SchemaDigest {
		return Admission{}, ErrConflict
	}
	var admitted State
	err = s.transaction(ctx, func() error {
		current, err := s.Resume(ctx, expected)
		if err != nil {
			return err
		}
		catalog, err := s.DomainCatalog(ctx)
		if err != nil {
			return err
		}
		if catalog.Digest() != schema || !appliedMatches(catalog.Applied, manifest) {
			return ErrConflict
		}
		if err := checkInstanceEpoch(current, func(q string) scanner { return s.conn.QueryRowContext(ctx, q) }); err != nil {
			return err
		}
		admitted = current
		return nil
	})
	if err != nil {
		return Admission{}, err
	}
	return Admission{state: &admissionState{expected: admitted, engine: s.engine, path: s.path, file: s.file}}, nil
}

// CheckTarget refuses mismatched engine/file before runtime pool creation.
// PostgreSQL independently checks installation identity in each transaction.
func (a Admission) CheckTarget(engine releaseidentity.Engine, path string) error {
	if !a.Valid() || a.state.engine != engine {
		return ErrConflict
	}
	if engine == releaseidentity.SQLite {
		canonical, err := canonicalSQLite(path)
		if err != nil {
			return err
		}
		if canonical != a.state.path {
			return ErrConflict
		}
		info, err := checkedFile(canonical)
		if err != nil {
			return err
		}
		if !os.SameFile(info, a.state.file) {
			return ErrConflict
		}
	}
	return nil
}

// SQLiteGuard owns the shared side of the same host lock used by maintenance.
// Each transaction gets its own descriptor; one goroutine cannot release a
// different transaction's guard. Keep it until commit or rollback completes.
type SQLiteGuard struct {
	admission Admission
	lock      *flock.Flock
}

func (a Admission) LockSQLite(ctx context.Context) (*SQLiteGuard, error) {
	if !a.Valid() || a.state.engine != releaseidentity.SQLite {
		return nil, ErrConflict
	}
	lock := flock.New(a.state.path + ".lock")
	held, err := lock.TryRLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !held {
		return nil, ErrConflict
	}
	if err := a.CheckTarget(releaseidentity.SQLite, a.state.path); err != nil {
		return nil, errors.Join(err, lock.Unlock())
	}
	return &SQLiteGuard{admission: a, lock: lock}, nil
}
func (g *SQLiteGuard) Close() error {
	if g == nil || g.lock == nil {
		return nil
	}
	lock := g.lock
	g.lock = nil
	return lock.Unlock()
}
func (g *SQLiteGuard) Check(ctx context.Context, tx *sql.Tx) error {
	if g == nil || g.lock == nil || tx == nil {
		return ErrConflict
	}
	a := g.admission
	if err := a.CheckTarget(releaseidentity.SQLite, a.state.path); err != nil {
		return err
	}
	current, err := ReadSQLiteSnapshot(ctx, tx)
	if err != nil {
		return err
	}
	if err := a.matches(current); err != nil {
		return err
	}
	return checkInstanceEpoch(current, func(q string) scanner { return tx.QueryRowContext(ctx, q) })
}

// GuardPostgres acquires the shared row lock in the transaction that owns all
// subsequent domain SQL. FOR SHARE conflicts with maintenance's non-key update;
// a separate connection or KEY SHARE would not establish this boundary.
func (a Admission) GuardPostgres(ctx context.Context, tx pgx.Tx) error {
	if !a.Valid() || a.state.engine != releaseidentity.Postgres || tx == nil {
		return ErrConflict
	}
	current, err := scanState(tx.QueryRow(ctx, snapshotSQL+" FOR SHARE OF c"))
	if err != nil {
		return err
	}
	if err := a.matches(current); err != nil {
		return err
	}
	return checkInstanceEpoch(current, func(q string) scanner { return tx.QueryRow(ctx, q) })
}
func (a Admission) matches(current State) error {
	if !a.Valid() || current.Validate() != nil || current.Maintenance || current.Pending.Phase != Healthy || current.Pending.Invalidated {
		return ErrConflict
	}
	expected := a.state.expected
	if current.TrustDomain != expected.TrustDomain || current.ReleaseRootDigest != expected.ReleaseRootDigest || current.InstanceID != expected.InstanceID || current.Applied != expected.Applied || current.MigrationDigest != expected.MigrationDigest || current.SchemaDigest != expected.SchemaDigest || current.RestoreEpoch != expected.RestoreEpoch || current.RecoveryIncarnation != expected.RecoveryIncarnation || current.Generation != expected.Generation {
		return ErrConflict
	}
	return nil
}
