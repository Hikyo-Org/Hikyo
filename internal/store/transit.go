package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
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

func decodeTransitOps(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

const transitKeyColumns = `id,environment_id,name,algorithm,custody,allowed_operations,state,latest_version,min_encrypt_version,min_decrypt_version,min_available_version,compromised_through_version,rotation_period_seconds,deletion_after,created_by,created_at,updated_at`

func scanTransitKey(row interface{ Scan(...any) error }, extra ...any) (TransitKeyRecord, error) {
	var out TransitKeyRecord
	var ops string
	var latest, minEnc, minDec, minAvail, compromised int64
	var deletionAfter, createdAt, updatedAt adapterStoredTime
	dest := []any{&out.ID, &out.EnvironmentID, &out.Name, &out.Algorithm, &out.Custody, &ops, &out.State,
		&latest, &minEnc, &minDec, &minAvail, &compromised, &out.RotationPeriodSeconds, &deletionAfter, &out.CreatedBy, &createdAt, &updatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if isNoRows(err) {
			return TransitKeyRecord{}, ErrNotFound
		}
		return TransitKeyRecord{}, err
	}
	out.AllowedOperations = decodeTransitOps(ops)
	out.LatestVersion, out.MinEncryptVersion = uint32(latest), uint32(minEnc)
	out.MinDecryptVersion, out.CompromisedThroughVersion = uint32(minDec), uint32(compromised)
	out.MinAvailableVersion = uint32(minAvail)
	out.DeletionAfter, out.CreatedAt, out.UpdatedAt = deletionAfter.value, createdAt.value, updatedAt.value
	return out, nil
}

// envChain verifies the proof and requires an environment-scoped chain.
func (r transitQueries) envChain(p authz.Proof, op authz.StoreOp) (org, project, env string, err error) {
	chain, err := authz.Verify(p, op, r.tok)
	if err != nil {
		return "", "", "", err
	}
	if chain.Env == "" {
		return "", "", "", ErrNotFound
	}
	return string(chain.Org), string(chain.Project), string(chain.Env), nil
}

func (r transitQueries) ListKeys(ctx context.Context, p authz.Proof) ([]TransitKeyRecord, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitKeysList)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(`SELECT ` + transitKeyColumns + ` FROM transit_keys WHERE org_id=? AND project_id=? AND environment_id=? AND state<>'destroyed' ORDER BY name`)
	rows, err := r.db.Query(ctx, query, org, project, env)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []TransitKeyRecord
	for rows.Next() {
		k, err := scanTransitKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r transitQueries) getKey(ctx context.Context, p authz.Proof, op authz.StoreOp, name string, lock bool) (TransitKeyRecord, error) {
	org, project, env, err := r.envChain(p, op)
	if err != nil {
		return TransitKeyRecord{}, err
	}
	base := `SELECT ` + transitKeyColumns + ` FROM transit_keys WHERE org_id=? AND project_id=? AND environment_id=? AND name=? AND state<>'destroyed'`
	query := r.db.SQL(base)
	if lock {
		query = r.db.SQLPerEngine(base, rewriteAdapterPlaceholders(base)+` FOR SHARE`)
	}
	return scanTransitKey(r.db.QueryRow(ctx, query, org, project, env, name))
}

func (r transitQueries) GetKey(ctx context.Context, p authz.Proof, name string) (TransitKeyRecord, error) {
	return r.getKey(ctx, p, authz.StoreTransitKeysGet, name, false)
}

func (r transitQueries) GetKeyForUse(ctx context.Context, p authz.Proof, name string) (TransitKeyRecord, error) {
	return r.getKey(ctx, p, authz.StoreTransitKeysGetForUse, name, true)
}

func (r transitQueries) CountKeys(ctx context.Context, p authz.Proof) (int, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitKeysCount)
	if err != nil {
		return 0, err
	}
	// Serialize environment key admission before counting. PostgreSQL's
	// READ COMMITTED writers otherwise all observe the last free slot.
	base := `SELECT id FROM environments WHERE org_id=? AND project_id=? AND id=?`
	var environmentID string
	if err := r.db.QueryRow(ctx, r.db.SQLPerEngine(base, rewriteAdapterPlaceholders(base)+` FOR UPDATE`), org, project, env).Scan(&environmentID); err != nil {
		return 0, err
	}
	var n int
	err = r.db.QueryRow(ctx, r.db.SQL(`SELECT COUNT(*) FROM transit_keys WHERE org_id=? AND project_id=? AND environment_id=? AND state<>'destroyed'`), org, project, env).Scan(&n)
	return n, err
}

func (r transitQueries) ListVersions(ctx context.Context, p authz.Proof, keyID string) ([]TransitVersionRecord, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitVersionsList)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(`SELECT id,version,public_key,CASE WHEN material_ciphertext IS NULL THEN 0 ELSE 1 END,CASE WHEN external_ref IS NULL THEN 0 ELSE 1 END,created_at FROM transit_key_versions WHERE org_id=? AND project_id=? AND environment_id=? AND key_id=? ORDER BY version`)
	rows, err := r.db.Query(ctx, query, org, project, env, keyID)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []TransitVersionRecord
	for rows.Next() {
		var v TransitVersionRecord
		var version int64
		var material, external int
		var createdAt adapterStoredTime
		if err := rows.Scan(&v.ID, &version, &v.PublicKey, &material, &external, &createdAt); err != nil {
			return nil, err
		}
		v.Version, v.HasMaterial, v.ExternalHeld, v.CreatedAt = uint32(version), material == 1, external == 1, createdAt.value
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r transitQueries) ListCallers(ctx context.Context, p authz.Proof, keyID string) ([]TransitCaller, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitCallersList)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(`SELECT principal_id,operations FROM transit_key_callers WHERE org_id=? AND project_id=? AND environment_id=? AND key_id=? ORDER BY principal_id`)
	rows, err := r.db.Query(ctx, query, org, project, env, keyID)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	out := []TransitCaller{}
	for rows.Next() {
		var c TransitCaller
		var ops string
		if err := rows.Scan(&c.PrincipalID, &ops); err != nil {
			return nil, err
		}
		c.Operations = decodeTransitOps(ops)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r transitQueries) VersionMaterial(ctx context.Context, p authz.Proof, keyID string, version uint32) (TransitVersionMaterial, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitVersionMaterial)
	if err != nil {
		return TransitVersionMaterial{}, err
	}
	query := r.db.SQL(`SELECT id,version,material_ciphertext,external_ref,public_key FROM transit_key_versions WHERE org_id=? AND project_id=? AND environment_id=? AND key_id=? AND version=?`)
	var out TransitVersionMaterial
	var v int64
	var ref *string
	if err := r.db.QueryRow(ctx, query, org, project, env, keyID, int64(version)).Scan(&out.ID, &v, &out.Sealed, &ref, &out.PublicKey); err != nil {
		if isNoRows(err) {
			return TransitVersionMaterial{}, ErrNotFound
		}
		return TransitVersionMaterial{}, err
	}
	out.Version = uint32(v)
	if ref != nil {
		out.ExternalRef = *ref
	}
	return out, nil
}

func (r transitQueries) insertVersion(ctx context.Context, org, project, env, keyID string, v TransitVersionCreate) error {
	var ref any
	if v.ExternalRef != "" {
		ref = v.ExternalRef
	}
	var sealed any
	if len(v.Sealed) > 0 {
		sealed = v.Sealed
	}
	var public any
	if len(v.PublicKey) > 0 {
		public = v.PublicKey
	}
	_, err := r.db.Exec(ctx, r.db.SQL(`INSERT INTO transit_key_versions (id,org_id,project_id,environment_id,key_id,version,material_ciphertext,external_ref,public_key,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`),
		v.ID, org, project, env, keyID, int64(v.Version), sealed, ref, public, r.db.Stamp(v.At))
	return err
}

func (r transitQueries) replaceCallers(ctx context.Context, org, project, env, keyID string, callers []TransitCaller) error {
	if _, err := r.db.Exec(ctx, r.db.SQL(`DELETE FROM transit_key_callers WHERE org_id=? AND project_id=? AND environment_id=? AND key_id=?`), org, project, env, keyID); err != nil {
		return err
	}
	for _, c := range callers {
		if _, err := r.db.Exec(ctx, r.db.SQL(`INSERT INTO transit_key_callers (org_id,project_id,environment_id,key_id,principal_id,operations) VALUES (?,?,?,?,?,?)`),
			org, project, env, keyID, c.PrincipalID, encodeTransitOps(c.Operations)); err != nil {
			return err
		}
	}
	return nil
}

func (r transitQueries) CreateKey(ctx context.Context, p authz.Proof, m TransitKeyCreate) (TransitKeyRecord, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitKeysCreate)
	if err != nil {
		return TransitKeyRecord{}, err
	}
	stamp := r.db.Stamp(m.At)
	_, err = r.db.Exec(ctx, r.db.SQL(`INSERT INTO transit_keys (id,org_id,project_id,environment_id,name,algorithm,custody,allowed_operations,exportable,state,latest_version,min_encrypt_version,min_decrypt_version,compromised_through_version,rotation_period_seconds,deletion_after,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,0,'active',1,1,1,0,?,NULL,?,?,?)`),
		m.ID, org, project, env, m.Name, m.Algorithm, m.Custody, encodeTransitOps(m.AllowedOperations), m.RotationPeriodSeconds, m.CreatedBy, stamp, stamp)
	if err != nil {
		return TransitKeyRecord{}, err
	}
	first := m.First
	first.Version = 1
	if err := r.insertVersion(ctx, org, project, env, m.ID, first); err != nil {
		return TransitKeyRecord{}, err
	}
	if err := r.replaceCallers(ctx, org, project, env, m.ID, m.Callers); err != nil {
		return TransitKeyRecord{}, err
	}
	return scanTransitKey(r.db.QueryRow(ctx, r.db.SQL(`SELECT `+transitKeyColumns+` FROM transit_keys WHERE id=? AND org_id=? AND project_id=? AND environment_id=?`), m.ID, org, project, env))
}

// missingOrConflict classifies a guarded update that touched no row.
func (r transitQueries) missingOrConflict(ctx context.Context, org, project, env, keyID string) error {
	var n int
	if err := r.db.QueryRow(ctx, r.db.SQL(`SELECT COUNT(*) FROM transit_keys WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'destroyed'`), keyID, org, project, env).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return ErrConflict
}

func (r transitQueries) AppendVersion(ctx context.Context, p authz.Proof, keyID string, expectLatest uint32, maxVersions int, v TransitVersionCreate) error {
	org, project, env, err := r.envChain(p, authz.StoreTransitVersionsAppend)
	if err != nil {
		return err
	}
	if v.Version != expectLatest+1 {
		return errors.New("store: transit version must be latest+1")
	}
	// The key must be live and its latest must still be the one the caller
	// rotated from; a destroyed, disabled-for-deletion or concurrently rotated
	// key affects zero rows.
	rows, err := r.db.Exec(ctx, r.db.SQL(`UPDATE transit_keys SET latest_version=?,updated_at=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND latest_version=? AND state IN ('active','retired','disabled')`),
		int64(v.Version), r.db.Stamp(v.At), keyID, org, project, env, int64(expectLatest))
	if err != nil {
		return err
	}
	if rows != 1 {
		return r.missingOrConflict(ctx, org, project, env, keyID)
	}
	// The successful update owns the key write lock. Count retained rows,
	// not the configured decrypt window, which can advance without trimming.
	var retained int
	if err := r.db.QueryRow(ctx, r.db.SQL(`SELECT COUNT(*) FROM transit_key_versions WHERE org_id=? AND project_id=? AND environment_id=? AND key_id=?`), org, project, env, keyID).Scan(&retained); err != nil {
		return err
	}
	if retained >= maxVersions {
		return ErrTransitVersionLimit
	}
	return r.insertVersion(ctx, org, project, env, keyID, v)
}

func (r transitQueries) Configure(ctx context.Context, p authz.Proof, m TransitKeyConfig) error {
	org, project, env, err := r.envChain(p, authz.StoreTransitKeysConfigure)
	if err != nil {
		return err
	}
	rows, err := r.db.Exec(ctx, r.db.SQL(`UPDATE transit_keys SET min_encrypt_version=?,min_decrypt_version=?,rotation_period_seconds=?,updated_at=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state NOT IN ('destroyed','pending-deletion') AND ? <= latest_version AND ? >= min_available_version`),
		int64(m.MinEncryptVersion), int64(m.MinDecryptVersion), m.RotationPeriodSeconds, r.db.Stamp(m.At), m.KeyID, org, project, env, int64(m.MinEncryptVersion), int64(m.MinDecryptVersion))
	if err != nil {
		return err
	}
	if rows != 1 {
		return r.missingOrConflict(ctx, org, project, env, m.KeyID)
	}
	if m.Callers != nil {
		return r.replaceCallers(ctx, org, project, env, m.KeyID, *m.Callers)
	}
	return nil
}

func (r transitQueries) ChangeState(ctx context.Context, p authz.Proof, m TransitStateChange) error {
	org, project, env, err := r.envChain(p, authz.StoreTransitKeysChangeState)
	if err != nil {
		return err
	}
	if len(m.From) == 0 || m.To == "destroyed" {
		return errors.New("store: transit state change requires source states and a non-terminal target")
	}
	var deletionAfter any
	if m.To == "pending-deletion" {
		deletionAfter = r.db.Stamp(m.DeletionAfter)
	}
	// A pending-deletion key whose delay has elapsed is committed to
	// destruction: the purge may already have destroyed external material, so
	// no transition may revive it (it stays pending-deletion until Destroy).
	args := []any{m.To, deletionAfter, r.db.Stamp(m.At), m.KeyID, org, project, env, r.db.Stamp(m.At)}
	for _, s := range m.From {
		args = append(args, s)
	}
	rows, err := r.db.Exec(ctx, r.db.SQL(`UPDATE transit_keys SET state=?,deletion_after=?,updated_at=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND (state<>'pending-deletion' OR (deletion_after>? AND purge_started=0)) AND state IN (`+strings.TrimSuffix(strings.Repeat("?,", len(m.From)), ",")+`)`), args...)
	if err != nil {
		return err
	}
	if rows != 1 {
		return r.missingOrConflict(ctx, org, project, env, m.KeyID)
	}
	return nil
}

func (r transitQueries) Compromise(ctx context.Context, p authz.Proof, keyID string, at time.Time) (uint32, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitKeysCompromise)
	if err != nil {
		return 0, err
	}
	rows, err := r.db.Exec(ctx, r.db.SQL(`UPDATE transit_keys SET compromised_through_version=latest_version,updated_at=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'destroyed'`),
		r.db.Stamp(at), keyID, org, project, env)
	if err != nil {
		return 0, err
	}
	if rows != 1 {
		return 0, ErrNotFound
	}
	var through int64
	err = r.db.QueryRow(ctx, r.db.SQL(`SELECT compromised_through_version FROM transit_keys WHERE id=? AND org_id=? AND project_id=? AND environment_id=?`), keyID, org, project, env).Scan(&through)
	return uint32(through), err
}

func (r transitQueries) FenceTrim(ctx context.Context, p authz.Proof, keyID string, at time.Time) (uint32, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitVersionsFenceTrim)
	if err != nil {
		return 0, err
	}
	rows, err := r.db.Exec(ctx, r.db.SQL(`UPDATE transit_keys SET min_available_version=min_decrypt_version,updated_at=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state NOT IN ('destroyed','pending-deletion')`),
		r.db.Stamp(at), keyID, org, project, env)
	if err != nil {
		return 0, err
	}
	if rows != 1 {
		return 0, r.missingOrConflict(ctx, org, project, env, keyID)
	}
	var floor int64
	err = r.db.QueryRow(ctx, r.db.SQL(`SELECT min_available_version FROM transit_keys WHERE id=? AND org_id=? AND project_id=? AND environment_id=?`), keyID, org, project, env).Scan(&floor)
	return uint32(floor), err
}

func (r transitQueries) Trim(ctx context.Context, p authz.Proof, keyID string, through uint32) (int64, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitVersionsTrim)
	if err != nil {
		return 0, err
	}
	var floor int64
	if err := r.db.QueryRow(ctx, r.db.SQL(`SELECT min_available_version FROM transit_keys WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state NOT IN ('destroyed','pending-deletion')`), keyID, org, project, env).Scan(&floor); err != nil {
		if isNoRows(err) {
			return 0, r.missingOrConflict(ctx, org, project, env, keyID)
		}
		return 0, err
	}
	// A concurrent trim may have fenced a higher floor but not destroyed its
	// external material yet. Delete only versions this attempt processed.
	if through < 1 || int64(through) > floor {
		return 0, ErrConflict
	}
	return r.db.Exec(ctx, r.db.SQL(`DELETE FROM transit_key_versions WHERE org_id=? AND project_id=? AND environment_id=? AND key_id=? AND version<?`), org, project, env, keyID, int64(through))
}

// ListVersionsForReencrypt pages a project's sealed transit material for the
// reencrypt walk. It is project-scoped (the walk spans every environment).
func (r transitQueries) ListVersionsForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error) {
	chain, err := authz.Verify(p, authz.StoreTransitVersionsListForReencrypt, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, r.db.SQL(`SELECT id,environment_id,key_id,material_ciphertext FROM transit_key_versions WHERE org_id=? AND project_id=? AND id>? AND material_ciphertext IS NOT NULL ORDER BY id LIMIT ?`),
		chain.Org, chain.Project, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []ReencryptFieldRow
	for rows.Next() {
		var row ReencryptFieldRow
		if err := rows.Scan(&row.ID, &row.EnvironmentID, &row.KeyID, &row.Ciphertext); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r transitQueries) ReencryptVersion(ctx context.Context, p authz.Proof, id string, newCiphertext, oldCiphertext []byte) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreTransitVersionsReencrypt, r.tok)
	if err != nil {
		return false, err
	}
	rows, err := r.db.Exec(ctx, r.db.SQL(`UPDATE transit_key_versions SET material_ciphertext=? WHERE org_id=? AND project_id=? AND id=? AND material_ciphertext=?`),
		newCiphertext, chain.Org, chain.Project, id, oldCiphertext)
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

const transitDueColumns = `k.org_id,k.project_id,k.environment_id,k.id,k.environment_id,k.name,k.algorithm,k.custody,k.allowed_operations,k.state,k.latest_version,k.min_encrypt_version,k.min_decrypt_version,k.min_available_version,k.compromised_through_version,k.rotation_period_seconds,k.deletion_after,k.created_by,k.created_at,k.updated_at`

func (r transitQueries) scanDue(rows adapterTargetRows, withLatest bool) ([]TransitDueKey, error) {
	defer closeAdapterRows(rows)
	var out []TransitDueKey
	for rows.Next() {
		var d TransitDueKey
		var latest adapterStoredTime
		prefix := []any{&d.OrgID, &d.ProjectID, &d.EnvironmentID}
		scanner := prefixScanner{rows: rows, prefix: prefix}
		var extra []any
		if withLatest {
			extra = []any{&latest}
		}
		k, err := scanTransitKey(scanner, extra...)
		if err != nil {
			return nil, err
		}
		d.TransitKeyRecord = k
		if withLatest {
			t, err := latest.Time()
			if err != nil {
				return nil, err
			}
			if t != nil {
				d.LatestCreatedAt = *t
			}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// prefixScanner lets scanTransitKey decode a row that carries chain columns in
// front of the key columns.
type prefixScanner struct {
	rows   interface{ Scan(...any) error }
	prefix []any
}

func (s prefixScanner) Scan(dest ...any) error {
	return s.rows.Scan(append(slices.Clone(s.prefix), dest...)...)
}

func (r transitQueries) SelectDeletionDue(ctx context.Context, p authz.Proof, now time.Time, after string, limit int) ([]TransitDueKey, error) {
	if _, err := authz.Verify(p, authz.StoreTransitSelectDeletionDue, r.tok); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, r.db.SQL(`SELECT `+transitDueColumns+` FROM transit_keys k WHERE k.state='pending-deletion' AND k.deletion_after<=? AND k.id>? ORDER BY k.id LIMIT ?`), r.db.Stamp(now), after, limit)
	if err != nil {
		return nil, err
	}
	return r.scanDue(rows, false)
}

func (r transitQueries) SelectRotationDue(ctx context.Context, p authz.Proof, after string, limit int) ([]TransitDueKey, error) {
	if _, err := authz.Verify(p, authz.StoreTransitSelectRotationDue, r.tok); err != nil {
		return nil, err
	}
	// Candidates only: whether the period has elapsed is decided by the caller
	// against the latest version's creation time, because interval arithmetic
	// differs between the engines.
	rows, err := r.db.Query(ctx, r.db.SQL(`SELECT `+transitDueColumns+`,v.created_at FROM transit_keys k JOIN transit_key_versions v ON v.key_id=k.id AND v.org_id=k.org_id AND v.version=k.latest_version WHERE k.state='active' AND k.rotation_period_seconds>0 AND k.id>? ORDER BY k.id LIMIT ?`), after, limit)
	if err != nil {
		return nil, err
	}
	return r.scanDue(rows, true)
}

func (r transitQueries) DestroyVersions(ctx context.Context, p authz.Proof, keyID string, now time.Time) ([]TransitVersionMaterial, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitDestroyVersions)
	if err != nil {
		return nil, err
	}
	// Persist the destructive boundary before calling the external provider.
	// Even a cancellation carrying an earlier request timestamp cannot revive
	// this key after the fence commits.
	changed, err := r.db.Exec(ctx, r.db.SQL(`UPDATE transit_keys SET purge_started=1 WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='pending-deletion' AND deletion_after<=?`), keyID, org, project, env, r.db.Stamp(now))
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrConflict
	}
	rows, err := r.db.Query(ctx, r.db.SQL(`SELECT id,version,external_ref FROM transit_key_versions WHERE org_id=? AND project_id=? AND environment_id=? AND key_id=? AND external_ref IS NOT NULL ORDER BY version`), org, project, env, keyID)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []TransitVersionMaterial
	for rows.Next() {
		var m TransitVersionMaterial
		var v int64
		if err := rows.Scan(&m.ID, &v, &m.ExternalRef); err != nil {
			return nil, err
		}
		m.Version = uint32(v)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r transitQueries) Destroy(ctx context.Context, p authz.Proof, keyID string, now time.Time) (int64, error) {
	org, project, env, err := r.envChain(p, authz.StoreTransitDestroy)
	if err != nil {
		return 0, err
	}
	rows, err := r.db.Exec(ctx, r.db.SQL(`UPDATE transit_keys SET state='destroyed',deletion_after=NULL,updated_at=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='pending-deletion' AND deletion_after<=?`),
		r.db.Stamp(now), keyID, org, project, env, r.db.Stamp(now))
	if err != nil {
		return 0, err
	}
	if rows != 1 {
		return 0, ErrConflict
	}
	erased, err := r.db.Exec(ctx, r.db.SQL(`UPDATE transit_key_versions SET material_ciphertext=NULL,external_ref=NULL WHERE org_id=? AND project_id=? AND environment_id=? AND key_id=?`), org, project, env, keyID)
	if err != nil {
		return 0, err
	}
	_, err = r.db.Exec(ctx, r.db.SQL(`DELETE FROM transit_key_callers WHERE org_id=? AND project_id=? AND environment_id=? AND key_id=?`), org, project, env, keyID)
	return erased, err
}

// TransitRuntime serves the label-free transit gauges at scrape time. It reads
// counts only, never a row's content.
type TransitRuntime struct {
	db *DB
}

func NewTransitRuntime(db *DB) *TransitRuntime { return &TransitRuntime{db: db} }

// TransitGauges are the label-free transit counts.
type TransitGauges struct {
	Live, RotationDue, PendingDeletion int64
}

func (r *TransitRuntime) Gauges(ctx context.Context, now time.Time) (TransitGauges, error) {
	var g TransitGauges
	err := dbRead(ctx, r.db, func(db adapterDB) error {
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM transit_keys WHERE state<>'destroyed'`).Scan(&g.Live); err != nil {
			return err
		}
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM transit_keys WHERE state='pending-deletion'`).Scan(&g.PendingDeletion); err != nil {
			return err
		}
		rows, err := db.Query(ctx, `SELECT k.rotation_period_seconds,v.created_at FROM transit_keys k JOIN transit_key_versions v ON v.key_id=k.id AND v.org_id=k.org_id AND v.version=k.latest_version WHERE k.state='active' AND k.rotation_period_seconds>0`)
		if err != nil {
			return err
		}
		defer closeAdapterRows(rows)
		for rows.Next() {
			var period int64
			var created adapterStoredTime
			if err := rows.Scan(&period, &created); err != nil {
				return err
			}
			t, err := created.Time()
			if err != nil {
				return err
			}
			if t != nil && !t.Add(time.Duration(period)*time.Second).After(now) {
				g.RotationDue++
			}
		}
		return rows.Err()
	})
	return g, err
}
