package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// Transit request-path store (#156, transit ADR). Every method is proof-carrying:
// it verifies its proof against its registered store operation and binds the
// tenant chain from the verified proof, never from an argument. Environment-
// scoped methods also bind the proof's environment, so a proof for one
// environment cannot touch a sibling's keys. The scheduler reaches the two
// maintenance doors (automatic rotation and the deletion purge) under scoped
// system authority, whose chain is resolved inside the transaction.

// ErrTransitVersionLimit means rotation requires trimming retained versions first.
var ErrTransitVersionLimit = errors.New("store: transit retained version limit exceeded")

// TransitKeyRecord is a key's metadata surface. There is no material here and
// no method on this surface ever returns material: version material is read
// only through TransitVersion for the operation that needs it.
type TransitKeyRecord struct {
	ID                string
	EnvironmentID     string
	Name              string
	Algorithm         string
	Custody           string
	AllowedOperations []string
	State             string
	LatestVersion     uint32
	MinEncryptVersion uint32
	MinDecryptVersion uint32
	// MinAvailableVersion is the trim floor (see TransitRepo.FenceTrim).
	MinAvailableVersion       uint32
	CompromisedThroughVersion uint32
	RotationPeriodSeconds     int64
	DeletionAfter             string
	CreatedBy                 string
	CreatedAt                 string
	UpdatedAt                 string
}

// TransitVersionRecord is one version's public metadata.
type TransitVersionRecord struct {
	ID           string
	Version      uint32
	PublicKey    []byte
	HasMaterial  bool
	ExternalHeld bool
	CreatedAt    string
}

// TransitVersionMaterial is what a custody provider needs for one operation:
// the sealed material (software) or the opaque reference (external).
type TransitVersionMaterial struct {
	ID          string
	Version     uint32
	Sealed      []byte
	ExternalRef string
	PublicKey   []byte
}

// TransitCaller is one per-key caller entry. It only ever narrows crypto-use.
type TransitCaller struct {
	PrincipalID string
	Operations  []string
}

// TransitVersionCreate is one version row. The service has already sealed the
// material (software) or obtained the reference (external).
type TransitVersionCreate struct {
	ID          string
	Version     uint32
	Sealed      []byte
	ExternalRef string
	PublicKey   []byte
	At          time.Time
}

// TransitKeyCreate is a key plus its first version and caller entries.
type TransitKeyCreate struct {
	ID                    string
	Name                  string
	Algorithm             string
	Custody               string
	AllowedOperations     []string
	RotationPeriodSeconds int64
	CreatedBy             string
	Callers               []TransitCaller
	First                 TransitVersionCreate
	At                    time.Time
}

// TransitKeyConfig changes the mutable policy of a key. Callers nil leaves the
// caller entries untouched; a non-nil empty slice clears them.
type TransitKeyConfig struct {
	KeyID                 string
	MinEncryptVersion     uint32
	MinDecryptVersion     uint32
	RotationPeriodSeconds int64
	Callers               *[]TransitCaller
	At                    time.Time
}

// TransitStateChange moves a key between lifecycle states. The update applies
// only when the key is in one of From; otherwise it is ErrConflict (or
// ErrNotFound when the key is gone).
type TransitStateChange struct {
	KeyID         string
	From          []string
	To            string
	DeletionAfter time.Time // set exactly when To is pending-deletion
	At            time.Time
}

// TransitDueKey is one key the scheduler must act on, with its full chain.
type TransitDueKey struct {
	OrgID, ProjectID, EnvironmentID string
	TransitKeyRecord
	LatestCreatedAt time.Time
}

// TransitReader is the read side.
type TransitReader interface {
	ListKeys(ctx context.Context, p authz.Proof) ([]TransitKeyRecord, error)
	GetKey(ctx context.Context, p authz.Proof, name string) (TransitKeyRecord, error)
	ListVersions(ctx context.Context, p authz.Proof, keyID string) ([]TransitVersionRecord, error)
	ListCallers(ctx context.Context, p authz.Proof, keyID string) ([]TransitCaller, error)
}

// TransitRepo is the write side plus the reads.
type TransitRepo interface {
	TransitReader
	// GetKeyForUse is GetKey for the operation path: on postgres it takes a
	// shared row lock, so a concurrent disable, compromise or deletion waits
	// for the operation's commit or is seen by it, never interleaved.
	GetKeyForUse(ctx context.Context, p authz.Proof, name string) (TransitKeyRecord, error)
	// VersionMaterial returns one version's material for the operation that
	// needs it. It is the ONLY method that returns material.
	VersionMaterial(ctx context.Context, p authz.Proof, keyID string, version uint32) (TransitVersionMaterial, error)
	CountKeys(ctx context.Context, p authz.Proof) (int, error)
	CreateKey(ctx context.Context, p authz.Proof, m TransitKeyCreate) (TransitKeyRecord, error)
	// AppendVersion adds version expectLatest+1 under a compare-and-swap on the
	// key row: a concurrent rotation that won first makes this ErrConflict.
	AppendVersion(ctx context.Context, p authz.Proof, keyID string, expectLatest uint32, maxVersions int, v TransitVersionCreate) error
	Configure(ctx context.Context, p authz.Proof, m TransitKeyConfig) error
	ChangeState(ctx context.Context, p authz.Proof, m TransitStateChange) error
	// Compromise marks every version up to the current latest compromised.
	Compromise(ctx context.Context, p authz.Proof, keyID string, at time.Time) (uint32, error)
	// FenceTrim raises the trim floor (min_available_version) to the current
	// min_decrypt_version and returns it. From then on Configure refuses a
	// min_decrypt_version below the floor, so the versions under it can be
	// destroyed at an external provider outside any transaction.
	FenceTrim(ctx context.Context, p authz.Proof, keyID string, at time.Time) (uint32, error)
	// Trim deletes versions below through, bounded by the durable trim floor.
	Trim(ctx context.Context, p authz.Proof, keyID string, through uint32) (int64, error)
	ListVersionsForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error)
	ReencryptVersion(ctx context.Context, p authz.Proof, id string, newCiphertext, oldCiphertext []byte) (bool, error)
	// SelectDeletionDue and SelectRotationDue are the scheduler's
	// installation-wide reads (system authority), keyset-paged by key id: after
	// is the last id of the previous page ("" for the first).
	SelectDeletionDue(ctx context.Context, p authz.Proof, now time.Time, after string, limit int) ([]TransitDueKey, error)
	SelectRotationDue(ctx context.Context, p authz.Proof, after string, limit int) ([]TransitDueKey, error)
	// DestroyVersions fences the due purge and lists material to destroy at an external
	// provider; Destroy then erases every version's material and tombstones the
	// key, guarded by pending-deletion and the elapsed delay.
	DestroyVersions(ctx context.Context, p authz.Proof, keyID string, now time.Time) ([]TransitVersionMaterial, error)
	Destroy(ctx context.Context, p authz.Proof, keyID string, now time.Time) (int64, error)
}

type transitQueries struct {
	db  adapterDB
	tok *authz.TxToken
}

func (r sqliteRepos) Transit() TransitRepo {
	return transitQueries{db: sqliteAdoptDB{db: r.db}, tok: r.tok}
}
func (r pgRepos) Transit() TransitRepo {
	return transitQueries{db: pgAdoptDB{db: r.db}, tok: r.tok}
}

func (s sqliteReadRepos) Transit() TransitReader { return s.r.Transit() }
func (p pgReadRepos) Transit() TransitReader     { return p.r.Transit() }

// encodeTransitOps stores an operation set canonically: sorted, deduplicated,
// comma-separated. The service validates the vocabulary; the store only keeps
// the encoding canonical so equal sets are equal strings.
func encodeTransitOps(ops []string) string {
	out := slices.Clone(ops)
	slices.Sort(out)
	return strings.Join(slices.Compact(out), ",")
}

// decodeTransitOps splits the stored operation set, returning an empty slice
// for an empty encoding.
func decodeTransitOps(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// envChain verifies the proof and requires an environment-scoped chain.
func (r transitQueries) envChain(p authz.Proof, op authz.StoreOp) (domain.Scope, error) {
	chain, err := authz.Verify(p, op, r.tok)
	if err != nil {
		return domain.Scope{}, err
	}
	if chain.Env == "" {
		return domain.Scope{}, ErrNotFound
	}
	return chain, nil
}

// ListKeys returns non-destroyed keys in the proof's environment, ordered by
// name. Proof, query, and row decoding errors propagate.
func (r transitQueries) ListKeys(ctx context.Context, p authz.Proof) ([]TransitKeyRecord, error) {
	chain, err := r.envChain(p, authz.StoreTransitKeysList)
	if err != nil {
		return nil, err
	}

	return r.db.transitStoreQueries().transitListKeys(ctx, chain)
}

// getKey loads a non-destroyed key by name under op, taking a shared row lock
// on PostgreSQL when lock is true. Missing keys return ErrNotFound; proof and
// query errors propagate.
func (r transitQueries) getKey(ctx context.Context, p authz.Proof, op authz.StoreOp, name string, lock bool) (TransitKeyRecord, error) {
	chain, err := r.envChain(p, op)
	if err != nil {
		return TransitKeyRecord{}, err
	}

	if lock {
		return r.db.transitStoreQueries().transitGetKeyForUse(ctx, chain, name)
	}
	return r.db.transitStoreQueries().transitGetKey(ctx, chain, name)
}

// GetKey returns a non-destroyed key in the proof's environment, or ErrNotFound.
// Proof and query errors propagate.
func (r transitQueries) GetKey(ctx context.Context, p authz.Proof, name string) (TransitKeyRecord, error) {
	return r.getKey(ctx, p, authz.StoreTransitKeysGet, name, false)
}

// GetKeyForUse loads a non-destroyed key for a cryptographic operation, holding
// a shared row lock on PostgreSQL. Missing keys return ErrNotFound; proof and
// query errors propagate.
func (r transitQueries) GetKeyForUse(ctx context.Context, p authz.Proof, name string) (TransitKeyRecord, error) {
	return r.getKey(ctx, p, authz.StoreTransitKeysGetForUse, name, true)
}

// CountKeys counts non-destroyed keys in the proof's environment, locking the
// environment on PostgreSQL to serialize key admission. Proof and query errors
// propagate.
func (r transitQueries) CountKeys(ctx context.Context, p authz.Proof) (int, error) {
	chain, err := r.envChain(p, authz.StoreTransitKeysCount)
	if err != nil {
		return 0, err
	}

	// Serialize admission before counting, so concurrent writers cannot take the last slot.
	q := r.db.transitStoreQueries()
	if _, err := q.transitAdmissionLock(ctx, chain); err != nil {
		return 0, err
	}
	n, err := q.transitCountKeys(ctx, chain)
	return int(n), err
}

// ListVersions returns version metadata ordered by version within the proof's
// environment. No matching versions yields an empty result; proof and store
// errors propagate.
func (r transitQueries) ListVersions(ctx context.Context, p authz.Proof, keyID string) ([]TransitVersionRecord, error) {
	chain, err := r.envChain(p, authz.StoreTransitVersionsList)
	if err != nil {
		return nil, err
	}

	return r.db.transitStoreQueries().transitListVersions(ctx, chain, keyID)
}

// ListCallers returns per-key restrictions ordered by principal ID within the
// proof's environment. An empty list imposes no caller restriction. Proof and
// store errors propagate.
func (r transitQueries) ListCallers(ctx context.Context, p authz.Proof, keyID string) ([]TransitCaller, error) {
	chain, err := r.envChain(p, authz.StoreTransitCallersList)
	if err != nil {
		return nil, err
	}

	return r.db.transitStoreQueries().transitListCallers(ctx, chain, keyID)
}

// VersionMaterial loads sealed material or an external reference and public
// metadata for one version in the proof's environment. Missing rows return
// ErrNotFound; proof and query errors propagate.
func (r transitQueries) VersionMaterial(ctx context.Context, p authz.Proof, keyID string, version uint32) (TransitVersionMaterial, error) {
	chain, err := r.envChain(p, authz.StoreTransitVersionMaterial)
	if err != nil {
		return TransitVersionMaterial{}, err
	}

	return r.db.transitStoreQueries().transitVersionMaterial(ctx, chain, keyID, version)
}

// insertVersion stores a version in the current transaction, writing absent
// material, references, and public keys as NULL. Database errors propagate.
func (r transitQueries) insertVersion(ctx context.Context, chain domain.Scope, keyID string, v TransitVersionCreate) error {
	return r.db.transitStoreQueries().transitInsertVersion(ctx, chain, keyID, v)
}

// replaceCallers replaces all caller entries for the key in the current
// transaction. An empty list clears them; database errors propagate.
func (r transitQueries) replaceCallers(ctx context.Context, chain domain.Scope, keyID string, callers []TransitCaller) error {
	q := r.db.transitStoreQueries()
	if err := q.transitDeleteCallers(ctx, chain, keyID); err != nil {
		return err
	}
	for _, caller := range callers {
		if err := q.transitInsertCaller(ctx, chain, keyID, caller); err != nil {
			return err
		}
	}
	return nil
}

// CreateKey inserts an active key, version 1, and its caller entries in the
// proof's environment, returning metadata. Proof and database errors propagate
// so the enclosing transaction can roll back.
func (r transitQueries) CreateKey(ctx context.Context, p authz.Proof, m TransitKeyCreate) (TransitKeyRecord, error) {
	chain, err := r.envChain(p, authz.StoreTransitKeysCreate)
	if err != nil {
		return TransitKeyRecord{}, err
	}

	q := r.db.transitStoreQueries()
	if err := q.transitInsertKey(ctx, chain, m); err != nil {
		return TransitKeyRecord{}, err
	}
	first := m.First
	first.Version = 1
	if err := r.insertVersion(ctx, chain, m.ID, first); err != nil {
		return TransitKeyRecord{}, err
	}
	if err := r.replaceCallers(ctx, chain, m.ID, m.Callers); err != nil {
		return TransitKeyRecord{}, err
	}
	return q.transitKeyByID(ctx, chain, m.ID)
}

// missingOrConflict classifies a guarded update that touched no row.
func (r transitQueries) missingOrConflict(ctx context.Context, chain domain.Scope, keyID string) error {
	n, err := r.db.transitStoreQueries().transitCountExistingKey(ctx, chain, keyID)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return ErrConflict
}

// AppendVersion adds expectLatest+1 to an active, retired, or disabled key.
// A stale latest version or disallowed state returns ErrConflict; an absent
// or destroyed key returns ErrNotFound. Retaining maxVersions rows returns
// ErrTransitVersionLimit. Proof and database errors propagate; the caller must
// roll back the transaction on error.
func (r transitQueries) AppendVersion(ctx context.Context, p authz.Proof, keyID string, expectLatest uint32, maxVersions int, v TransitVersionCreate) error {
	chain, err := r.envChain(p, authz.StoreTransitVersionsAppend)
	if err != nil {
		return err
	}

	if v.Version != expectLatest+1 {
		return errors.New("store: transit version must be latest+1")
	}
	q := r.db.transitStoreQueries()
	rows, err := q.transitAppendVersion(ctx, chain, keyID, expectLatest, v)
	if err != nil {
		return err
	}
	if rows != 1 {
		return r.missingOrConflict(ctx, chain, keyID)
	}
	// The successful CAS owns the key write lock; count retained rows, not the decrypt window.
	retained, err := q.transitCountVersions(ctx, chain, keyID)
	if err != nil {
		return err
	}
	if retained >= int64(maxVersions) {
		return ErrTransitVersionLimit
	}
	return r.insertVersion(ctx, chain, keyID, v)
}

// Configure updates policy and optionally replaces caller entries. A missing
// or destroyed key returns ErrNotFound; pending deletion or a version outside
// the stored bounds returns ErrConflict. Proof and database errors propagate.
func (r transitQueries) Configure(ctx context.Context, p authz.Proof, m TransitKeyConfig) error {
	chain, err := r.envChain(p, authz.StoreTransitKeysConfigure)
	if err != nil {
		return err
	}

	rows, err := r.db.transitStoreQueries().transitConfigure(ctx, chain, m)
	if err != nil {
		return err
	}
	if rows != 1 {
		return r.missingOrConflict(ctx, chain, m.KeyID)
	}
	if m.Callers != nil {
		return r.replaceCallers(ctx, chain, m.KeyID, *m.Callers)
	}
	return nil
}

// ChangeState applies a transition from one of m.From, setting a deletion
// deadline only for pending deletion. A pending deletion cannot be canceled
// after its deadline or purge start. Empty source states and a destroyed target
// are invalid. A guarded update matching no row returns ErrNotFound for an
// absent or destroyed key, otherwise ErrConflict. Proof and database errors
// propagate.
func (r transitQueries) ChangeState(ctx context.Context, p authz.Proof, m TransitStateChange) error {
	chain, err := r.envChain(p, authz.StoreTransitKeysChangeState)
	if err != nil {
		return err
	}

	if len(m.From) == 0 || m.To == "destroyed" {
		return errors.New("store: transit state change requires source states and a non-terminal target")
	}
	// An elapsed deletion deadline or committed purge fence forbids revival.
	rows, err := r.db.transitStoreQueries().transitChangeState(ctx, chain, m)
	if err != nil {
		return err
	}
	if rows != 1 {
		return r.missingOrConflict(ctx, chain, m.KeyID)
	}
	return nil
}

// Compromise marks all versions through the current latest as compromised and
// returns that version. Missing or destroyed keys return ErrNotFound; proof
// and database errors propagate.
func (r transitQueries) Compromise(ctx context.Context, p authz.Proof, keyID string, at time.Time) (uint32, error) {
	chain, err := r.envChain(p, authz.StoreTransitKeysCompromise)
	if err != nil {
		return 0, err
	}

	q := r.db.transitStoreQueries()
	rows, err := q.transitCompromise(ctx, chain, keyID, at)
	if err != nil {
		return 0, err
	}
	if rows != 1 {
		return 0, ErrNotFound
	}
	through, err := q.transitCompromisedThrough(ctx, chain, keyID)
	return uint32(through), err
}

// FenceTrim persists and returns the current minimum decrypt version as the
// trim floor. Pending deletion returns ErrConflict; missing or destroyed keys
// return ErrNotFound. Proof and database errors propagate.
func (r transitQueries) FenceTrim(ctx context.Context, p authz.Proof, keyID string, at time.Time) (uint32, error) {
	chain, err := r.envChain(p, authz.StoreTransitVersionsFenceTrim)
	if err != nil {
		return 0, err
	}

	q := r.db.transitStoreQueries()
	rows, err := q.transitFenceTrim(ctx, chain, keyID, at)
	if err != nil {
		return 0, err
	}
	if rows != 1 {
		return 0, r.missingOrConflict(ctx, chain, keyID)
	}
	floor, err := q.transitTrimFloor(ctx, chain, keyID)
	return uint32(floor), err
}

// Trim deletes version rows strictly below through and returns the deleted
// count. through must be positive and at most the durable trim floor. Invalid
// bounds or pending deletion return ErrConflict; missing or destroyed keys
// return ErrNotFound. Proof and database errors propagate.
func (r transitQueries) Trim(ctx context.Context, p authz.Proof, keyID string, through uint32) (int64, error) {
	chain, err := r.envChain(p, authz.StoreTransitVersionsTrim)
	if err != nil {
		return 0, err
	}

	q := r.db.transitStoreQueries()
	floor, err := q.transitAvailableTrimFloor(ctx, chain, keyID)
	if err != nil {
		if isNoRows(err) {
			return 0, r.missingOrConflict(ctx, chain, keyID)
		}
		return 0, err
	}
	// Delete only versions this attempt processed, even if another trim fenced a higher floor.
	if through < 1 || int64(through) > floor {
		return 0, ErrConflict
	}
	return q.transitTrimVersions(ctx, chain, keyID, through)
}

// ListVersionsForReencrypt pages a project's sealed transit material for the
// reencrypt walk. It is project-scoped (the walk spans every environment).
func (r transitQueries) ListVersionsForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error) {
	chain, err := authz.Verify(p, authz.StoreTransitVersionsListForReencrypt, r.tok)
	if err != nil {
		return nil, err
	}

	return r.db.transitStoreQueries().transitListReencrypt(ctx, chain, cursor, limit)
}

// ReencryptVersion replaces sealed material only if it still matches
// oldCiphertext, returning whether a row changed. Missing or changed rows
// return false without error; proof and database errors propagate.
func (r transitQueries) ReencryptVersion(ctx context.Context, p authz.Proof, id string, newCiphertext, oldCiphertext []byte) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreTransitVersionsReencrypt, r.tok)
	if err != nil {
		return false, err
	}

	return affectedOne(r.db.transitStoreQueries().transitReencrypt(ctx, chain, id, newCiphertext, oldCiphertext))
}

// SelectDeletionDue returns installation-wide pending deletions due at or
// before now, ordered by key ID after the exclusive cursor, up to limit rows.
// Use an empty after for the first page. Proof, query, and scan errors propagate.
func (r transitQueries) SelectDeletionDue(ctx context.Context, p authz.Proof, now time.Time, after string, limit int) ([]TransitDueKey, error) {
	if _, err := authz.Verify(p, authz.StoreTransitSelectDeletionDue, r.tok); err != nil {
		return nil, err
	}

	return r.db.transitStoreQueries().transitSelectDeletionDue(ctx, now, after, limit)
}

// SelectRotationDue returns active keys with a positive rotation period,
// ordered by key ID after the exclusive cursor, up to limit rows. Use an empty
// after for the first page. The caller must check whether the period has elapsed
// since LatestCreatedAt. Proof, query, and scan errors propagate.
func (r transitQueries) SelectRotationDue(ctx context.Context, p authz.Proof, after string, limit int) ([]TransitDueKey, error) {
	if _, err := authz.Verify(p, authz.StoreTransitSelectRotationDue, r.tok); err != nil {
		return nil, err
	}

	return r.db.transitStoreQueries().transitSelectRotationDue(ctx, after, limit)
}

// DestroyVersions persists the purge fence for a pending deletion due at or
// before now, then lists its external references in version order. A key that
// is absent or not due returns ErrConflict; proof and database errors propagate.
// The enclosing transaction must commit before external destruction begins.
func (r transitQueries) DestroyVersions(ctx context.Context, p authz.Proof, keyID string, now time.Time) ([]TransitVersionMaterial, error) {
	chain, err := r.envChain(p, authz.StoreTransitDestroyVersions)
	if err != nil {
		return nil, err
	}

	q := r.db.transitStoreQueries()
	// Commit the destructive fence before crossing the external provider boundary.
	changed, err := q.transitFencePurge(ctx, chain, keyID, now)
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrConflict
	}
	return q.transitExternalVersions(ctx, chain, keyID)
}

// Destroy tombstones a pending deletion due at or before now, clears all
// version material and caller entries, and returns the number of version rows
// updated. Version metadata and public keys remain. A key that is absent or
// not due returns ErrConflict; proof and database errors propagate.
func (r transitQueries) Destroy(ctx context.Context, p authz.Proof, keyID string, now time.Time) (int64, error) {
	chain, err := r.envChain(p, authz.StoreTransitDestroy)
	if err != nil {
		return 0, err
	}

	q := r.db.transitStoreQueries()
	rows, err := q.transitDestroy(ctx, chain, keyID, now)
	if err != nil {
		return 0, err
	}
	if rows != 1 {
		return 0, ErrConflict
	}
	erased, err := q.transitEraseMaterial(ctx, chain, keyID)
	if err != nil {
		return 0, err
	}
	err = q.transitDeleteCallers(ctx, chain, keyID)
	return erased, err
}

// TransitRuntime serves the label-free transit gauges at scrape time. It reads
// counts only, never a row's content.
type TransitRuntime struct {
	db *DB
}

// NewTransitRuntime creates a gauge reader over db without reading it.
func NewTransitRuntime(db *DB) *TransitRuntime { return &TransitRuntime{db: db} }

// TransitGauges are the label-free transit counts.
type TransitGauges struct {
	Live, RotationDue, PendingDeletion int64
}

// Gauges counts non-destroyed, rotation-due, and pending-deletion keys across
// the installation at now. Database and timestamp errors propagate; counts
// may be partial on error.
func (r *TransitRuntime) Gauges(ctx context.Context, now time.Time) (TransitGauges, error) {
	var g TransitGauges
	err := dbRead(ctx, r.db, func(db adapterDB) error {
		q := db.transitStoreQueries()
		var err error
		g.Live, err = q.transitCountLive(ctx)
		if err != nil {
			return err
		}
		g.PendingDeletion, err = q.transitCountPendingDeletion(ctx)
		if err != nil {
			return err
		}
		candidates, err := q.transitGaugeRotationCandidates(ctx)
		for _, candidate := range candidates {
			if candidate.createdAt != nil && !candidate.createdAt.Add(time.Duration(candidate.period)*time.Second).After(now) {
				g.RotationDue++
			}
		}
		return err
	})
	return g, err
}
