package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	sqlite "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Keyring persistence: rows hold wrapped-key ciphertext envelopes only.
// Absent keys map to crypto.ErrNoKey; uniqueness conflicts (two writers
// minting one scope's key, or two first boots racing) map to
// crypto.ErrKeyExists so the keyring can converge on the winner.

// KeyReader is the read side of keyring persistence.
type KeyReader interface {
	// ActiveMasterWrappers returns every active master wrapper (one per
	// root epoch; two while a root rotation is dual-wrapped; empty at
	// first boot), newest epoch first.
	ActiveMasterWrappers(ctx context.Context, pf authz.Proof) ([]crypto.WrappedKey, error)
	ActiveTier3(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) (crypto.WrappedKey, error)
	// Tier3Versions returns every still-openable version of one scope's key
	// (active + retiring, newest first) so the keyring can open ciphertext a
	// reencrypt has not yet moved off a superseded DEK version. Empty when the
	// scope has no key yet.
	Tier3Versions(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) ([]crypto.WrappedKey, error)
	// AllOpenableTier3 returns every still-openable tier-3 key across every
	// scope (active + retiring), for rotate-master-key to re-wrap.
	AllOpenableTier3(ctx context.Context, pf authz.Proof) ([]crypto.WrappedKey, error)
	// AssertActiveDEKVersion is the writer fence: it confirms (and, on postgres,
	// FOR SHARE-locks) that the DEK version a ciphertext was sealed under is
	// still active for its scope, refusing with ErrStaleDEK otherwise. Called
	// inside a ciphertext write's own transaction so a stale sealer cannot land a
	// write under a version reencrypt is about to retire.
	AssertActiveDEKVersion(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string, version uint32) error
}

// KeyRepo is the transactional keyring repository. InsertTier3 and
// InsertMaster are always preceded by AcquireHierarchyGeneration in the same
// transaction — the fence that will serialize key creation against master
// rotation (encryption-model ADR § Rotation; the rotation operations land later).
type KeyRepo interface {
	InsertInitialProjectDEK(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error
	KeyReader
	AcquireHierarchyGeneration(ctx context.Context, pf authz.Proof) error
	InsertMaster(ctx context.Context, pf authz.Proof, k crypto.WrappedKey) error
	InsertTier3(ctx context.Context, pf authz.Proof, k crypto.WrappedKey) error
	// RotateTokenKey retires the active root token key and installs its
	// successor, both inside the hierarchy-generation fence.
	//
	// It is ONE store method rather than the three calls it performs, and that
	// is a rule, not a convenience: `keys.AcquireHierarchyGeneration` and
	// `keys.InsertTier3` are bound to the boot mint site, and a store method is
	// grant-evaluated or site-bound, never both (invariant 6). A rotation is
	// grant-evaluated -- an operator holding `rotate-dek` asks for it -- so it
	// reaches the same rows through a door of its own.
	RotateTokenKey(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error
	// RotateScanningKey retires the active scanning-fingerprint key and installs
	// its successor, the exact twin of RotateTokenKey for the sixth rotation
	// operation (#74). Dropping the dismissal rows is the service's, in the same
	// transaction (StoreScanningDismissalsDeleteAll) — this method owns only the
	// key swap, so the two concerns stay separately authorized.
	RotateScanningKey(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error
	// RotateDEK appends a new DEK version for one project or the instance scope
	// and demotes the previous active version to retiring — no longer written,
	// still openable until reencrypt walks its ciphertext. It runs inside the
	// hierarchy fence (serializing against master rotation) and the scope fence
	// (against writers and reencrypt); a concurrent rotation that already moved
	// the active version returns ErrRotationSuperseded.
	RotateDEK(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error
	// RotateMasterKey installs a new master, re-wraps every tier-3 key under it,
	// and retires the old master — all inside the hierarchy fence. It refuses
	// (crypto.ErrMasterRotationBlocked) while the root is dual-wrapped, and
	// returns ErrRotationSuperseded if a concurrent master rotation already moved
	// the active master.
	RotateMasterKey(ctx context.Context, pf authz.Proof, newMaster crypto.WrappedKey, rewrapped []crypto.WrappedKey) error
	// RetireRetiringTier3 retires every 'retiring' version of one scope, inside
	// the scope fence — reencrypt's completion, once the walk has moved every
	// ciphertext onto the active version. Returns how many versions retired.
	RetireRetiringTier3(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) (int64, error)
	// RootKeyRotatePrepare commits the second master wrapper (same version, new
	// epoch) of the dual-wrapped transition, inside the hierarchy fence. It
	// refuses (crypto.ErrRootRotationBlocked) unless exactly one active wrapper
	// exists — a prepare while one is already pending is the four-way matrix the
	// ADR refuses.
	RootKeyRotatePrepare(ctx context.Context, pf authz.Proof, newWrapper crypto.WrappedKey) error
	// RootKeyRotateFinalize retires the old-epoch wrapper and returns the new
	// active epoch. It refuses (crypto.ErrNotDualWrapped) when the master is not
	// dual-wrapped, and ErrRotationSuperseded when a concurrent finalize won.
	RootKeyRotateFinalize(ctx context.Context, pf authz.Proof) (uint32, error)
	InsertScopeGeneration(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) error
}

// ErrRotationSuperseded reports a tier-3 key rotation (token key or DEK) losing
// the compare-and-swap: the active version moved between prepare and commit
// because a concurrent rotation committed first. The caller refuses loudly;
// retrying mints a fresh successor against the new predecessor.
var ErrRotationSuperseded = errors.New("store: key rotation superseded by a concurrent rotation")

// ErrStaleDEK reports the writer fence refusing a ciphertext write: the DEK
// version it was sealed under is no longer active for its scope — a sealer built
// before a rotate-dek. The caller re-fetches a fresh sealer and retries; the
// service maps it to a conflict.
var ErrStaleDEK = errors.New("store: ciphertext sealed under a non-active DEK version; re-fetch the sealer and retry")

func scopeGenerationKey(p crypto.Purpose, orgID, projectID string) string {
	return fmt.Sprintf("tier3:%s:%s:%s", p, orgID, projectID)
}

func dbVersion(field string, v int64) (uint32, error) {
	if v < 0 || v > math.MaxUint32 {
		return 0, fmt.Errorf("store: %s %d out of range", field, v)
	}
	return uint32(v), nil
}

func parsePurpose(s string) (crypto.Purpose, error) {
	switch p := crypto.Purpose(s); p {
	case crypto.PurposeProject, crypto.PurposeInstance, crypto.PurposeToken, crypto.PurposeScanning:
		return p, nil
	default:
		return "", fmt.Errorf("store: unknown key purpose %q", s)
	}
}

// --- sqlite ---

type sqliteKeys struct {
	q   *sqlitegen.Queries
	tok *authz.TxToken
}

func (r sqliteRepos) Keys() KeyRepo { return sqliteKeys{q: sqlitegen.New(r.db), tok: r.tok} }

// sqliteUniqueViolation matches exactly the uniqueness extended codes —
// mirroring postgres 23505. A broader SQLITE_CONSTRAINT match would turn
// CHECK/FK/NOT NULL bugs into a silent "key already exists" retry path.
func sqliteUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) &&
		(se.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE || se.Code() == sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY)
}

func (k sqliteKeys) ActiveMasterWrappers(ctx context.Context, pf authz.Proof) ([]crypto.WrappedKey, error) {
	if _, err := authz.Verify(pf, authz.StoreKeysActiveMasterWrappers, k.tok); err != nil {
		return nil, err
	}
	rows, err := k.q.GetActiveMasterKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]crypto.WrappedKey, 0, len(rows))
	for _, row := range rows {
		version, err := dbVersion("key version", row.Version)
		if err != nil {
			return nil, err
		}
		epoch, err := dbVersion("root key epoch", row.RootKeyEpoch)
		if err != nil {
			return nil, err
		}
		created, err := time.Parse(timeFormat, row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("store: master key created_at %q: %w", row.CreatedAt, err)
		}
		out = append(out, crypto.WrappedKey{
			Version:      version,
			RootKeyEpoch: epoch,
			Blob:         row.Blob,
			CreatedAt:    created.UTC(),
		})
	}
	return out, nil
}

func (k sqliteKeys) ActiveTier3(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) (crypto.WrappedKey, error) {
	if _, err := authz.Verify(pf, authz.StoreKeysActiveTier3, k.tok); err != nil {
		return crypto.WrappedKey{}, err
	}
	row, err := k.q.GetActiveTier3Key(ctx, sqlitegen.GetActiveTier3KeyParams{
		Purpose: string(p), OrgID: orgID, ProjectID: projectID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return crypto.WrappedKey{}, crypto.ErrNoKey
	}
	if err != nil {
		return crypto.WrappedKey{}, err
	}
	return tier3FromSQLite(row)
}

func (k sqliteKeys) Tier3Versions(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) ([]crypto.WrappedKey, error) {
	if _, err := authz.Verify(pf, authz.StoreKeysTier3Versions, k.tok); err != nil {
		return nil, err
	}
	rows, err := k.q.GetTier3Versions(ctx, sqlitegen.GetTier3VersionsParams{
		Purpose: string(p), OrgID: orgID, ProjectID: projectID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]crypto.WrappedKey, 0, len(rows))
	for _, row := range rows {
		wk, err := tier3FromSQLite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, wk)
	}
	return out, nil
}

func tier3FromSQLite(row sqlitegen.Tier3Key) (crypto.WrappedKey, error) {
	purpose, err := parsePurpose(row.Purpose)
	if err != nil {
		return crypto.WrappedKey{}, err
	}
	version, err := dbVersion("key version", row.Version)
	if err != nil {
		return crypto.WrappedKey{}, err
	}
	masterVersion, err := dbVersion("master key version", row.MasterKeyVersion)
	if err != nil {
		return crypto.WrappedKey{}, err
	}
	created, err := time.Parse(timeFormat, row.CreatedAt)
	if err != nil {
		return crypto.WrappedKey{}, fmt.Errorf("store: key %s created_at %q: %w", row.ID, row.CreatedAt, err)
	}
	return crypto.WrappedKey{
		ID:               row.ID,
		Purpose:          purpose,
		OrgID:            row.OrgID,
		ProjectID:        row.ProjectID,
		Version:          version,
		MasterKeyVersion: masterVersion,
		Blob:             row.Blob,
		CreatedAt:        created.UTC(),
	}, nil
}

func (k sqliteKeys) AcquireHierarchyGeneration(ctx context.Context, pf authz.Proof) error {
	return acquireHierarchyGeneration(ctx, pf, k)
}

func (k sqliteKeys) InsertMaster(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error {
	return insertMaster(ctx, pf, key, k)
}

func (k sqliteKeys) InsertTier3(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error {
	return insertTier3(ctx, pf, key, k)
}

func (k sqliteKeys) RotateTokenKey(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error {
	return rotateRootScopedTier3(ctx, pf, key, k, authz.StoreKeysRotateTokenKey, crypto.PurposeToken)
}

func (k sqliteKeys) AllOpenableTier3(ctx context.Context, pf authz.Proof) ([]crypto.WrappedKey, error) {
	if _, err := authz.Verify(pf, authz.StoreKeysAllOpenableTier3, k.tok); err != nil {
		return nil, err
	}
	rows, err := k.q.AllOpenableTier3(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]crypto.WrappedKey, 0, len(rows))
	for _, row := range rows {
		wk, err := tier3FromSQLite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, wk)
	}
	return out, nil
}

func (k sqliteKeys) RotateMasterKey(ctx context.Context, pf authz.Proof, newMaster crypto.WrappedKey, rewrapped []crypto.WrappedKey) error {
	return rotateMasterKey(ctx, pf, newMaster, rewrapped, k)
}

func (k sqliteKeys) AssertActiveDEKVersion(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string, version uint32) error {
	return assertActiveDEKVersion(ctx, pf, p, orgID, projectID, version, k)
}

func (k sqliteKeys) RotateScanningKey(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error {
	return rotateRootScopedTier3(ctx, pf, key, k, authz.StoreKeysRotateScanningKey, crypto.PurposeScanning)
}

func (k sqliteKeys) RotateDEK(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error {
	return rotateDEK(ctx, pf, key, k)
}

func (k sqliteKeys) RetireRetiringTier3(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) (int64, error) {
	return retireRetiringTier3(ctx, pf, p, orgID, projectID, k)
}

func (k sqliteKeys) RootKeyRotatePrepare(ctx context.Context, pf authz.Proof, newWrapper crypto.WrappedKey) error {
	return rootKeyRotatePrepare(ctx, pf, newWrapper, k)
}

func (k sqliteKeys) RootKeyRotateFinalize(ctx context.Context, pf authz.Proof) (uint32, error) {
	return rootKeyRotateFinalize(ctx, pf, k)
}

func (k sqliteKeys) InsertScopeGeneration(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) error {
	return insertScopeGeneration(ctx, pf, p, orgID, projectID, k)
}

// --- postgres ---

type pgKeys struct {
	q   *pggen.Queries
	tok *authz.TxToken
}

func (r pgRepos) Keys() KeyRepo { return pgKeys{q: pggen.New(r.db), tok: r.tok} }

func pgUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (k pgKeys) ActiveMasterWrappers(ctx context.Context, pf authz.Proof) ([]crypto.WrappedKey, error) {
	if _, err := authz.Verify(pf, authz.StoreKeysActiveMasterWrappers, k.tok); err != nil {
		return nil, err
	}
	rows, err := k.q.GetActiveMasterKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]crypto.WrappedKey, 0, len(rows))
	for _, row := range rows {
		version, err := dbVersion("key version", row.Version)
		if err != nil {
			return nil, err
		}
		epoch, err := dbVersion("root key epoch", row.RootKeyEpoch)
		if err != nil {
			return nil, err
		}
		if !row.CreatedAt.Valid {
			return nil, errors.New("store: master key: null created_at")
		}
		out = append(out, crypto.WrappedKey{
			Version:      version,
			RootKeyEpoch: epoch,
			Blob:         row.Blob,
			CreatedAt:    row.CreatedAt.Time.UTC(),
		})
	}
	return out, nil
}

func (k pgKeys) ActiveTier3(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) (crypto.WrappedKey, error) {
	if _, err := authz.Verify(pf, authz.StoreKeysActiveTier3, k.tok); err != nil {
		return crypto.WrappedKey{}, err
	}
	row, err := k.q.GetActiveTier3Key(ctx, pggen.GetActiveTier3KeyParams{
		Purpose: string(p), OrgID: orgID, ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return crypto.WrappedKey{}, crypto.ErrNoKey
	}
	if err != nil {
		return crypto.WrappedKey{}, err
	}
	return tier3FromPG(row)
}

func (k pgKeys) Tier3Versions(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) ([]crypto.WrappedKey, error) {
	if _, err := authz.Verify(pf, authz.StoreKeysTier3Versions, k.tok); err != nil {
		return nil, err
	}
	rows, err := k.q.GetTier3Versions(ctx, pggen.GetTier3VersionsParams{
		Purpose: string(p), OrgID: orgID, ProjectID: projectID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]crypto.WrappedKey, 0, len(rows))
	for _, row := range rows {
		wk, err := tier3FromPG(row)
		if err != nil {
			return nil, err
		}
		out = append(out, wk)
	}
	return out, nil
}

func tier3FromPG(row pggen.Tier3Key) (crypto.WrappedKey, error) {
	purpose, err := parsePurpose(row.Purpose)
	if err != nil {
		return crypto.WrappedKey{}, err
	}
	version, err := dbVersion("key version", row.Version)
	if err != nil {
		return crypto.WrappedKey{}, err
	}
	masterVersion, err := dbVersion("master key version", row.MasterKeyVersion)
	if err != nil {
		return crypto.WrappedKey{}, err
	}
	if !row.CreatedAt.Valid {
		return crypto.WrappedKey{}, fmt.Errorf("store: key %s: null created_at", row.ID)
	}
	return crypto.WrappedKey{
		ID:               row.ID,
		Purpose:          purpose,
		OrgID:            row.OrgID,
		ProjectID:        row.ProjectID,
		Version:          version,
		MasterKeyVersion: masterVersion,
		Blob:             row.Blob,
		CreatedAt:        row.CreatedAt.Time.UTC(),
	}, nil
}

func (k pgKeys) AcquireHierarchyGeneration(ctx context.Context, pf authz.Proof) error {
	return acquireHierarchyGeneration(ctx, pf, k)
}

func (k pgKeys) InsertMaster(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error {
	return insertMaster(ctx, pf, key, k)
}

func (k pgKeys) InsertTier3(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error {
	return insertTier3(ctx, pf, key, k)
}

func (k pgKeys) RotateTokenKey(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error {
	return rotateRootScopedTier3(ctx, pf, key, k, authz.StoreKeysRotateTokenKey, crypto.PurposeToken)
}

func (k pgKeys) AllOpenableTier3(ctx context.Context, pf authz.Proof) ([]crypto.WrappedKey, error) {
	if _, err := authz.Verify(pf, authz.StoreKeysAllOpenableTier3, k.tok); err != nil {
		return nil, err
	}
	rows, err := k.q.AllOpenableTier3(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]crypto.WrappedKey, 0, len(rows))
	for _, row := range rows {
		wk, err := tier3FromPG(row)
		if err != nil {
			return nil, err
		}
		out = append(out, wk)
	}
	return out, nil
}

func (k pgKeys) RotateMasterKey(ctx context.Context, pf authz.Proof, newMaster crypto.WrappedKey, rewrapped []crypto.WrappedKey) error {
	return rotateMasterKey(ctx, pf, newMaster, rewrapped, k)
}

func (k pgKeys) AssertActiveDEKVersion(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string, version uint32) error {
	return assertActiveDEKVersion(ctx, pf, p, orgID, projectID, version, k)
}

func (k pgKeys) RotateScanningKey(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error {
	return rotateRootScopedTier3(ctx, pf, key, k, authz.StoreKeysRotateScanningKey, crypto.PurposeScanning)
}

func (k pgKeys) RotateDEK(ctx context.Context, pf authz.Proof, key crypto.WrappedKey) error {
	return rotateDEK(ctx, pf, key, k)
}

func (k pgKeys) RetireRetiringTier3(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) (int64, error) {
	return retireRetiringTier3(ctx, pf, p, orgID, projectID, k)
}

func (k pgKeys) RootKeyRotatePrepare(ctx context.Context, pf authz.Proof, newWrapper crypto.WrappedKey) error {
	return rootKeyRotatePrepare(ctx, pf, newWrapper, k)
}

func (k pgKeys) RootKeyRotateFinalize(ctx context.Context, pf authz.Proof) (uint32, error) {
	return rootKeyRotateFinalize(ctx, pf, k)
}

func (k pgKeys) InsertScopeGeneration(ctx context.Context, pf authz.Proof, p crypto.Purpose, orgID, projectID string) error {
	return insertScopeGeneration(ctx, pf, p, orgID, projectID, k)
}

func (k sqliteKeys) InsertInitialProjectDEK(ctx context.Context, p authz.Proof, key crypto.WrappedKey) error {
	return insertInitialProjectDEK(ctx, p, key, k)
}
func (k pgKeys) InsertInitialProjectDEK(ctx context.Context, p authz.Proof, key crypto.WrappedKey) error {
	return insertInitialProjectDEK(ctx, p, key, k)
}
