package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// SSH certificate request-path store (#155). Every method is proof-carrying:
// it verifies its proof against its registered store operation and binds the
// tenant chain (org, project, environment) from the verified proof, never from
// a caller argument. The sweeper's revocation SQL is the proof-free SSHRuntime
// (ssh_runtime.go).

// SSHCAKey is one CA signing key's public record. There is no private field:
// the sealed key is reachable only through ActiveCAKeyCiphertext.
type SSHCAKey struct {
	ID          string
	CAID        string
	Algorithm   string
	PublicKey   string
	Fingerprint string
	Origin      string
	State       string
	CreatedAt   string
	RetiringAt  string
	RetireAfter string
	RetiredAt   string
}

// SSHCA is a CA and its keys, newest first.
type SSHCA struct {
	ID                   string
	Name                 string
	State                string
	AuthorityPrincipalID string
	CreatedAt            string
	Keys                 []SSHCAKey
}

// SSHCACreate is a new CA row.
type SSHCACreate struct {
	ID                   string
	Name                 string
	AuthorityPrincipalID string
	At                   time.Time
}

// SSHCAKeyCreate is a new active key. Ciphertext is already sealed.
type SSHCAKeyCreate struct {
	ID          string
	CAID        string
	Algorithm   string
	PublicKey   string
	Fingerprint string
	Origin      string
	Ciphertext  []byte
	At          time.Time
}

// SSHActiveKey is what signing needs: the active key's identity and its
// sealed private material.
type SSHActiveKey struct {
	KeyID      string
	Algorithm  string
	PublicKey  string
	Ciphertext []byte
}

// SSHProfile is a profile's full policy, requesters included.
type SSHProfile struct {
	ID                string
	CAID              string
	Name              string
	Principals        []string
	ForceCommand      string
	SourceAddresses   []string
	Extensions        []string
	KeyAlgorithms     []string
	DefaultTTLSeconds int64
	MaxTTLSeconds     int64
	State             string
	Requesters        []string
	CreatedAt         string
	UpdatedAt         string
}

// SSHProfileWrite creates or replaces a profile (requesters included).
type SSHProfileWrite struct {
	ID                string
	CAID              string
	Name              string
	Principals        []string
	ForceCommand      string
	SourceAddresses   []string
	Extensions        []string
	KeyAlgorithms     []string
	DefaultTTLSeconds int64
	MaxTTLSeconds     int64
	Enabled           bool
	Requesters        []string
	At                time.Time
}

// SSHCertificate is an issued certificate's public record, joined with the
// signing key's and CA's current state so reads can derive trust and KRL
// membership without a second query.
type SSHCertificate struct {
	ID                   string
	CAID                 string
	CAKeyID              string
	ProfileID            string
	Serial               int64
	KeyID                string
	Principals           []string
	PublicKeyFingerprint string
	KeyAlgorithm         string
	KeyOrigin            string
	ValidAfter           string
	ValidBefore          string
	RequesterPrincipalID string
	RequesterClass       string
	State                string
	RevokedAt            string
	RevocationReason     string
	CreatedAt            string
	CAKeyState           string
	CAKeyRetireAfter     string
	CAState              string
}

// SSHCertificateCreate is one issuance's durable record.
type SSHCertificateCreate struct {
	ID                   string
	CAID                 string
	CAKeyID              string
	ProfileID            string
	Serial               int64
	KeyID                string
	Principals           []string
	PublicKeyFingerprint string
	KeyAlgorithm         string
	KeyOrigin            string
	ValidAfter           time.Time
	ValidBefore          time.Time
	RequesterPrincipalID string
	RequesterClass       string
	At                   time.Time
}

// SSHRevokedSerial is one KRL entry: the signing key and the serial.
type SSHRevokedSerial struct {
	CAKeyPublicKey string
	Serial         int64
}

// SSHRevokedCertificate names a certificate a bulk revocation changed, for the
// caller's per-certificate audit rows.
type SSHRevokedCertificate struct {
	ID                   string
	Serial               int64
	RequesterPrincipalID string
}

// SSHReader is the read side.
type SSHReader interface {
	GetCA(ctx context.Context, p authz.Proof, caID string) (SSHCA, error)
	ListCAs(ctx context.Context, p authz.Proof) ([]SSHCA, error)
	GetProfile(ctx context.Context, p authz.Proof, profileID string) (SSHProfile, error)
	ListProfiles(ctx context.Context, p authz.Proof) ([]SSHProfile, error)
	GetCertificate(ctx context.Context, p authz.Proof, certID string) (SSHCertificate, error)
	ListCertificates(ctx context.Context, p authz.Proof, limit int) ([]SSHCertificate, error)
	RevokedSerials(ctx context.Context, p authz.Proof, caID string, now time.Time, limit int) ([]SSHRevokedSerial, error)
	ListCAKeysForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error)
}

// SSHRepo is the write side plus the reads.
type SSHRepo interface {
	SSHReader
	CreateCA(ctx context.Context, p authz.Proof, m SSHCACreate) error
	InsertCAKey(ctx context.Context, p authz.Proof, m SSHCAKeyCreate) error
	ActiveCAKey(ctx context.Context, p authz.Proof, caID string) (SSHActiveKey, error)
	RetireActiveCAKey(ctx context.Context, p authz.Proof, caID string, retireAfter, at time.Time) (string, error)
	RetireCAKey(ctx context.Context, p authz.Proof, caID, keyID string, at time.Time) error
	DeleteCA(ctx context.Context, p authz.Proof, caID string, at time.Time) error
	CountLiveProfilesForCA(ctx context.Context, p authz.Proof, caID string) (int, error)
	MaxProfileTTLForCA(ctx context.Context, p authz.Proof, caID string) (int64, error)
	CreateProfile(ctx context.Context, p authz.Proof, m SSHProfileWrite) error
	UpdateProfile(ctx context.Context, p authz.Proof, m SSHProfileWrite) error
	DeleteProfile(ctx context.Context, p authz.Proof, profileID string, at time.Time) error
	IsRequester(ctx context.Context, p authz.Proof, profileID, principalID string) (bool, error)
	InsertCertificate(ctx context.Context, p authz.Proof, m SSHCertificateCreate) error
	RevokeCertificate(ctx context.Context, p authz.Proof, certID, reason string, at time.Time) (bool, error)
	RevokeProfileCertificates(ctx context.Context, p authz.Proof, profileID, reason string, keepRequesters []string, all bool, at time.Time) ([]SSHRevokedCertificate, error)
	ReencryptCAKey(ctx context.Context, p authz.Proof, id string, newCiphertext, oldCiphertext []byte) (bool, error)
	PurgeEnvironment(ctx context.Context, p authz.Proof) error
}

type sshQueries struct {
	db  adapterDB
	tok *authz.TxToken
}

func (r sqliteRepos) SSH() SSHRepo { return sshQueries{db: sqliteAdoptDB{db: r.db}, tok: r.tok} }
func (r pgRepos) SSH() SSHRepo     { return sshQueries{db: pgAdoptDB{db: r.db}, tok: r.tok} }

// ErrSSHNoActiveKey reports a CA with no signing key (it was deleted).
var ErrSSHNoActiveKey = errors.New("store: ssh CA has no active signing key")

// envChain verifies a proof and requires it to be environment-scoped: every
// SSH table is environment class.
func (r sshQueries) envChain(p authz.Proof, op authz.StoreOp) (domain.Scope, error) {
	chain, err := authz.Verify(p, op, r.tok)
	if err != nil {
		return domain.Scope{}, err
	}
	if chain.Env == "" {
		return domain.Scope{}, ErrNotFound
	}
	return chain, nil
}

func encodeSSHList(in []string) (string, error) {
	if in == nil {
		in = []string{}
	}
	b, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeSSHList(s string) ([]string, error) {
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("store: malformed ssh list column: %w", err)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// ---- CAs --------------------------------------------------------------------

func (r sshQueries) CreateCA(ctx context.Context, p authz.Proof, m SSHCACreate) error {
	chain, err := r.envChain(p, authz.StoreSSHCreateCA)
	if err != nil {
		return err
	}
	query := r.db.SQL(`INSERT INTO ssh_cas (id,org_id,project_id,environment_id,name,state,authority_principal_id,created_at) VALUES (?,?,?,?,?, 'active', ?, ?)`)
	_, err = r.db.Exec(ctx, query, m.ID, chain.Org, chain.Project, string(chain.Env), m.Name, m.AuthorityPrincipalID, r.db.Stamp(m.At))
	return mapSSHUnique(err)
}

func (r sshQueries) InsertCAKey(ctx context.Context, p authz.Proof, m SSHCAKeyCreate) error {
	chain, err := r.envChain(p, authz.StoreSSHInsertCAKey)
	if err != nil {
		return err
	}
	query := r.db.SQL(`INSERT INTO ssh_ca_keys (id,org_id,project_id,environment_id,ca_id,algorithm,public_key,fingerprint,origin,private_key_ciphertext,state,created_at)
SELECT ?,org_id,project_id,environment_id,id,?,?,?,?,?, 'active', ? FROM ssh_cas WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='active'`)
	rows, err := r.db.Exec(ctx, query, m.ID, m.Algorithm, m.PublicKey, m.Fingerprint, m.Origin, m.Ciphertext, r.db.Stamp(m.At),
		m.CAID, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return mapSSHUnique(err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

const sshCAColumns = `id,name,state,authority_principal_id,created_at`
const sshCAKeyColumns = `id,ca_id,algorithm,public_key,fingerprint,origin,state,created_at,retiring_at,retire_after,retired_at`

func scanSSHCA(row interface{ Scan(...any) error }) (SSHCA, error) {
	var out SSHCA
	var created adapterStoredTime
	err := row.Scan(&out.ID, &out.Name, &out.State, &out.AuthorityPrincipalID, &created)
	if isNoRows(err) {
		return SSHCA{}, ErrNotFound
	}
	if err != nil {
		return SSHCA{}, err
	}
	out.CreatedAt = created.value
	return out, nil
}

func scanSSHCAKey(row interface{ Scan(...any) error }) (SSHCAKey, error) {
	var out SSHCAKey
	var created, retiring, retireAfter, retired adapterStoredTime
	if err := row.Scan(&out.ID, &out.CAID, &out.Algorithm, &out.PublicKey, &out.Fingerprint, &out.Origin, &out.State, &created, &retiring, &retireAfter, &retired); err != nil {
		return SSHCAKey{}, err
	}
	out.CreatedAt, out.RetiringAt, out.RetireAfter, out.RetiredAt = created.value, retiring.value, retireAfter.value, retired.value
	return out, nil
}

func (r sshQueries) keysFor(ctx context.Context, chain domain.Scope, caIDs []string) (map[string][]SSHCAKey, error) {
	out := map[string][]SSHCAKey{}
	if len(caIDs) == 0 {
		return out, nil
	}
	query := r.db.SQL(`SELECT ` + sshCAKeyColumns + ` FROM ssh_ca_keys WHERE org_id=? AND project_id=? AND environment_id=? ORDER BY created_at DESC, id DESC`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	want := map[string]bool{}
	for _, id := range caIDs {
		want[id] = true
	}
	for rows.Next() {
		key, err := scanSSHCAKey(rows)
		if err != nil {
			return nil, err
		}
		if want[key.CAID] {
			out[key.CAID] = append(out[key.CAID], key)
		}
	}
	return out, rows.Err()
}

func (r sshQueries) GetCA(ctx context.Context, p authz.Proof, caID string) (SSHCA, error) {
	chain, err := r.envChain(p, authz.StoreSSHGetCA)
	if err != nil {
		return SSHCA{}, err
	}
	query := r.db.SQL(`SELECT ` + sshCAColumns + ` FROM ssh_cas WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='active'`)
	ca, err := scanSSHCA(r.db.QueryRow(ctx, query, caID, chain.Org, chain.Project, string(chain.Env)))
	if err != nil {
		return SSHCA{}, err
	}
	keys, err := r.keysFor(ctx, chain, []string{ca.ID})
	if err != nil {
		return SSHCA{}, err
	}
	ca.Keys = keys[ca.ID]
	return ca, nil
}

func (r sshQueries) ListCAs(ctx context.Context, p authz.Proof) ([]SSHCA, error) {
	chain, err := r.envChain(p, authz.StoreSSHListCAs)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(`SELECT ` + sshCAColumns + ` FROM ssh_cas WHERE org_id=? AND project_id=? AND environment_id=? AND state='active' ORDER BY name, id`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return nil, err
	}
	var out []SSHCA
	for rows.Next() {
		ca, err := scanSSHCA(rows)
		if err != nil {
			closeAdapterRows(rows)
			return nil, err
		}
		out = append(out, ca)
	}
	err = rows.Err()
	closeAdapterRows(rows)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out))
	for _, ca := range out {
		ids = append(ids, ca.ID)
	}
	keys, err := r.keysFor(ctx, chain, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Keys = keys[out[i].ID]
	}
	return out, nil
}

func (r sshQueries) ActiveCAKey(ctx context.Context, p authz.Proof, caID string) (SSHActiveKey, error) {
	chain, err := r.envChain(p, authz.StoreSSHActiveCAKey)
	if err != nil {
		return SSHActiveKey{}, err
	}
	query := r.db.SQL(`SELECT k.id,k.algorithm,k.public_key,k.private_key_ciphertext FROM ssh_ca_keys k JOIN ssh_cas c ON c.id=k.ca_id AND c.org_id=k.org_id AND c.project_id=k.project_id AND c.environment_id=k.environment_id
WHERE k.ca_id=? AND k.org_id=? AND k.project_id=? AND k.environment_id=? AND k.state='active' AND c.state='active'`)
	var out SSHActiveKey
	err = r.db.QueryRow(ctx, query, caID, chain.Org, chain.Project, string(chain.Env)).Scan(&out.KeyID, &out.Algorithm, &out.PublicKey, &out.Ciphertext)
	if isNoRows(err) {
		return SSHActiveKey{}, ErrNotFound
	}
	if err != nil {
		return SSHActiveKey{}, err
	}
	if len(out.Ciphertext) == 0 {
		return SSHActiveKey{}, ErrSSHNoActiveKey
	}
	return out, nil
}

// RetireActiveCAKey moves the CA's active key to `retiring` (trusted until
// retireAfter, never signing again: its ciphertext is nulled in the same
// statement) and returns its id.
func (r sshQueries) RetireActiveCAKey(ctx context.Context, p authz.Proof, caID string, retireAfter, at time.Time) (string, error) {
	chain, err := r.envChain(p, authz.StoreSSHRetireActiveCAKey)
	if err != nil {
		return "", err
	}
	selectQuery := r.db.SQL(`SELECT id FROM ssh_ca_keys WHERE ca_id=? AND org_id=? AND project_id=? AND environment_id=? AND state='active'`)
	var keyID string
	if err := r.db.QueryRow(ctx, selectQuery, caID, chain.Org, chain.Project, string(chain.Env)).Scan(&keyID); err != nil {
		if isNoRows(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	query := r.db.SQL(`UPDATE ssh_ca_keys SET state='retiring',private_key_ciphertext=NULL,retiring_at=?,retire_after=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='active'`)
	rows, err := r.db.Exec(ctx, query, r.db.Stamp(at), r.db.Stamp(retireAfter), keyID, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return "", err
	}
	if rows != 1 {
		return "", ErrConflict
	}
	return keyID, nil
}

// RetireCAKey ends a retiring key's overlap now.
func (r sshQueries) RetireCAKey(ctx context.Context, p authz.Proof, caID, keyID string, at time.Time) error {
	chain, err := r.envChain(p, authz.StoreSSHRetireCAKey)
	if err != nil {
		return err
	}
	query := r.db.SQL(`UPDATE ssh_ca_keys SET state='retired',retired_at=? WHERE id=? AND ca_id=? AND org_id=? AND project_id=? AND environment_id=? AND state='retiring'`)
	rows, err := r.db.Exec(ctx, query, r.db.Stamp(at), keyID, caID, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return err
	}
	if rows != 1 {
		existsQuery := r.db.SQL(`SELECT COUNT(*) FROM ssh_ca_keys WHERE id=? AND ca_id=? AND org_id=? AND project_id=? AND environment_id=?`)
		var n int
		if err := r.db.QueryRow(ctx, existsQuery, keyID, caID, chain.Org, chain.Project, string(chain.Env)).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return ErrConflict
	}
	return nil
}

// DeleteCA tombstones a CA and retires every key it holds, destroying the
// active key's sealed material.
func (r sshQueries) DeleteCA(ctx context.Context, p authz.Proof, caID string, at time.Time) error {
	chain, err := r.envChain(p, authz.StoreSSHDeleteCA)
	if err != nil {
		return err
	}
	query := r.db.SQL(`UPDATE ssh_cas SET state='tombstoned' WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='active'`)
	rows, err := r.db.Exec(ctx, query, caID, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	keys := r.db.SQL(`UPDATE ssh_ca_keys SET state='retired',private_key_ciphertext=NULL,retired_at=? WHERE ca_id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'retired'`)
	_, err = r.db.Exec(ctx, keys, r.db.Stamp(at), caID, chain.Org, chain.Project, string(chain.Env))
	return err
}

func (r sshQueries) CountLiveProfilesForCA(ctx context.Context, p authz.Proof, caID string) (int, error) {
	chain, err := r.envChain(p, authz.StoreSSHCountLiveProfilesForCA)
	if err != nil {
		return 0, err
	}
	query := r.db.SQL(`SELECT COUNT(*) FROM ssh_profiles WHERE ca_id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'tombstoned'`)
	var n int
	err = r.db.QueryRow(ctx, query, caID, chain.Org, chain.Project, string(chain.Env)).Scan(&n)
	return n, err
}

func (r sshQueries) MaxProfileTTLForCA(ctx context.Context, p authz.Proof, caID string) (int64, error) {
	chain, err := r.envChain(p, authz.StoreSSHMaxProfileTTLForCA)
	if err != nil {
		return 0, err
	}
	query := r.db.SQL(`SELECT COALESCE(MAX(max_ttl_seconds),0) FROM ssh_profiles WHERE ca_id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'tombstoned'`)
	var n int64
	err = r.db.QueryRow(ctx, query, caID, chain.Org, chain.Project, string(chain.Env)).Scan(&n)
	return n, err
}

// ---- Profiles ---------------------------------------------------------------

const sshProfileColumns = `id,ca_id,name,principals,force_command,source_addresses,extensions,key_algorithms,default_ttl_seconds,max_ttl_seconds,state,created_at,updated_at`

func scanSSHProfile(row interface{ Scan(...any) error }) (SSHProfile, error) {
	var out SSHProfile
	var principals, sources, extensions, algorithms string
	var created, updated adapterStoredTime
	err := row.Scan(&out.ID, &out.CAID, &out.Name, &principals, &out.ForceCommand, &sources, &extensions, &algorithms, &out.DefaultTTLSeconds, &out.MaxTTLSeconds, &out.State, &created, &updated)
	if isNoRows(err) {
		return SSHProfile{}, ErrNotFound
	}
	if err != nil {
		return SSHProfile{}, err
	}
	for _, f := range []struct {
		src string
		dst *[]string
	}{{principals, &out.Principals}, {sources, &out.SourceAddresses}, {extensions, &out.Extensions}, {algorithms, &out.KeyAlgorithms}} {
		if *f.dst, err = decodeSSHList(f.src); err != nil {
			return SSHProfile{}, err
		}
	}
	out.CreatedAt, out.UpdatedAt = created.value, updated.value
	return out, nil
}

func (r sshQueries) requestersFor(ctx context.Context, chain domain.Scope) (map[string][]string, error) {
	query := r.db.SQL(`SELECT profile_id,principal_id FROM ssh_profile_requesters WHERE org_id=? AND project_id=? AND environment_id=? ORDER BY profile_id, principal_id`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	out := map[string][]string{}
	for rows.Next() {
		var profile, principal string
		if err := rows.Scan(&profile, &principal); err != nil {
			return nil, err
		}
		out[profile] = append(out[profile], principal)
	}
	return out, rows.Err()
}

func (r sshQueries) GetProfile(ctx context.Context, p authz.Proof, profileID string) (SSHProfile, error) {
	chain, err := r.envChain(p, authz.StoreSSHGetProfile)
	if err != nil {
		return SSHProfile{}, err
	}
	query := r.db.SQL(`SELECT ` + sshProfileColumns + ` FROM ssh_profiles WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'tombstoned'`)
	profile, err := scanSSHProfile(r.db.QueryRow(ctx, query, profileID, chain.Org, chain.Project, string(chain.Env)))
	if err != nil {
		return SSHProfile{}, err
	}
	requesters, err := r.requestersFor(ctx, chain)
	if err != nil {
		return SSHProfile{}, err
	}
	profile.Requesters = nonNilStrings(requesters[profile.ID])
	return profile, nil
}

func (r sshQueries) ListProfiles(ctx context.Context, p authz.Proof) ([]SSHProfile, error) {
	chain, err := r.envChain(p, authz.StoreSSHListProfiles)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(`SELECT ` + sshProfileColumns + ` FROM ssh_profiles WHERE org_id=? AND project_id=? AND environment_id=? AND state<>'tombstoned' ORDER BY name, id`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return nil, err
	}
	var out []SSHProfile
	for rows.Next() {
		profile, err := scanSSHProfile(rows)
		if err != nil {
			closeAdapterRows(rows)
			return nil, err
		}
		out = append(out, profile)
	}
	err = rows.Err()
	closeAdapterRows(rows)
	if err != nil {
		return nil, err
	}
	requesters, err := r.requestersFor(ctx, chain)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Requesters = nonNilStrings(requesters[out[i].ID])
	}
	return out, nil
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func profileState(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func (r sshQueries) encodeProfileLists(m SSHProfileWrite) ([4]string, error) {
	var out [4]string
	for i, list := range [][]string{m.Principals, m.SourceAddresses, m.Extensions, m.KeyAlgorithms} {
		enc, err := encodeSSHList(list)
		if err != nil {
			return out, err
		}
		out[i] = enc
	}
	return out, nil
}

func (r sshQueries) CreateProfile(ctx context.Context, p authz.Proof, m SSHProfileWrite) error {
	chain, err := r.envChain(p, authz.StoreSSHCreateProfile)
	if err != nil {
		return err
	}
	lists, err := r.encodeProfileLists(m)
	if err != nil {
		return err
	}
	stamp := r.db.Stamp(m.At)
	// The CA must be live in this same environment; the SELECT binds it.
	query := r.db.SQL(`INSERT INTO ssh_profiles (id,org_id,project_id,environment_id,ca_id,name,principals,force_command,source_addresses,extensions,key_algorithms,default_ttl_seconds,max_ttl_seconds,state,created_at,updated_at)
SELECT ?,org_id,project_id,environment_id,id,?,?,?,?,?,?,?,?,?,?,? FROM ssh_cas WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='active'`)
	rows, err := r.db.Exec(ctx, query, m.ID, m.Name, lists[0], m.ForceCommand, lists[1], lists[2], lists[3], m.DefaultTTLSeconds, m.MaxTTLSeconds, profileState(m.Enabled), stamp, stamp,
		m.CAID, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return mapSSHUnique(err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return r.replaceRequesters(ctx, chain, m.ID, m.Requesters, m.At)
}

func (r sshQueries) UpdateProfile(ctx context.Context, p authz.Proof, m SSHProfileWrite) error {
	chain, err := r.envChain(p, authz.StoreSSHUpdateProfile)
	if err != nil {
		return err
	}
	lists, err := r.encodeProfileLists(m)
	if err != nil {
		return err
	}
	// The CA is immutable on update (a profile never moves between CAs): the
	// predicate asserts the caller's CAID is the stored one.
	query := r.db.SQL(`UPDATE ssh_profiles SET name=?,principals=?,force_command=?,source_addresses=?,extensions=?,key_algorithms=?,default_ttl_seconds=?,max_ttl_seconds=?,state=?,updated_at=?
WHERE id=? AND ca_id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'tombstoned'`)
	rows, err := r.db.Exec(ctx, query, m.Name, lists[0], m.ForceCommand, lists[1], lists[2], lists[3], m.DefaultTTLSeconds, m.MaxTTLSeconds, profileState(m.Enabled), r.db.Stamp(m.At),
		m.ID, m.CAID, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return mapSSHUnique(err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return r.replaceRequesters(ctx, chain, m.ID, m.Requesters, m.At)
}

func (r sshQueries) replaceRequesters(ctx context.Context, chain domain.Scope, profileID string, requesters []string, at time.Time) error {
	del := r.db.SQL(`DELETE FROM ssh_profile_requesters WHERE profile_id=? AND org_id=? AND project_id=? AND environment_id=?`)
	if _, err := r.db.Exec(ctx, del, profileID, chain.Org, chain.Project, string(chain.Env)); err != nil {
		return err
	}
	ins := r.db.SQL(`INSERT INTO ssh_profile_requesters (org_id,project_id,environment_id,profile_id,principal_id,created_at) VALUES (?,?,?,?,?,?)`)
	for _, principal := range requesters {
		if _, err := r.db.Exec(ctx, ins, chain.Org, chain.Project, string(chain.Env), profileID, principal, r.db.Stamp(at)); err != nil {
			return err
		}
	}
	return nil
}

func (r sshQueries) DeleteProfile(ctx context.Context, p authz.Proof, profileID string, at time.Time) error {
	chain, err := r.envChain(p, authz.StoreSSHDeleteProfile)
	if err != nil {
		return err
	}
	query := r.db.SQL(`UPDATE ssh_profiles SET state='tombstoned',updated_at=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state<>'tombstoned'`)
	rows, err := r.db.Exec(ctx, query, r.db.Stamp(at), profileID, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	// Requester rows stay: the sweeper reads them as the requester's standing,
	// and a delete without revoke_issued leaves live certificates valid until
	// they expire. PurgeEnvironment removes the rows with the environment.
	return nil
}

func (r sshQueries) IsRequester(ctx context.Context, p authz.Proof, profileID, principalID string) (bool, error) {
	chain, err := r.envChain(p, authz.StoreSSHIsRequester)
	if err != nil {
		return false, err
	}
	query := r.db.SQL(`SELECT COUNT(*) FROM ssh_profile_requesters WHERE profile_id=? AND principal_id=? AND org_id=? AND project_id=? AND environment_id=?`)
	var n int
	if err := r.db.QueryRow(ctx, query, profileID, principalID, chain.Org, chain.Project, string(chain.Env)).Scan(&n); err != nil {
		return false, err
	}
	return n == 1, nil
}

// ---- Certificates -----------------------------------------------------------

func (r sshQueries) InsertCertificate(ctx context.Context, p authz.Proof, m SSHCertificateCreate) error {
	chain, err := r.envChain(p, authz.StoreSSHInsertCertificate)
	if err != nil {
		return err
	}
	principals, err := encodeSSHList(m.Principals)
	if err != nil {
		return err
	}
	// The row is written only while its signing key is still the CA's active
	// key and its profile still enabled, inside the issuing transaction: this
	// is the fence a node that lost a race with rotation or disable hits.
	query := r.db.SQL(`INSERT INTO ssh_certificates (id,org_id,project_id,environment_id,ca_id,ca_key_id,profile_id,serial,key_id,principals,public_key_fingerprint,key_algorithm,key_origin,valid_after,valid_before,requester_principal_id,requester_class,state,created_at)
SELECT ?,k.org_id,k.project_id,k.environment_id,k.ca_id,k.id,pr.id,?,?,?,?,?,?,?,?,?,?, 'issued', ?
FROM ssh_ca_keys k JOIN ssh_profiles pr ON pr.ca_id=k.ca_id AND pr.org_id=k.org_id AND pr.project_id=k.project_id AND pr.environment_id=k.environment_id
WHERE k.id=? AND k.ca_id=? AND k.state='active' AND pr.id=? AND pr.state='enabled' AND k.org_id=? AND k.project_id=? AND k.environment_id=?`)
	rows, err := r.db.Exec(ctx, query, m.ID, m.Serial, m.KeyID, principals, m.PublicKeyFingerprint, m.KeyAlgorithm, m.KeyOrigin,
		r.db.Stamp(m.ValidAfter), r.db.Stamp(m.ValidBefore), m.RequesterPrincipalID, m.RequesterClass, r.db.Stamp(m.At),
		m.CAKeyID, m.CAID, m.ProfileID, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return mapSSHUnique(err)
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}

const sshCertificateColumns = `c.id,c.ca_id,c.ca_key_id,c.profile_id,c.serial,c.key_id,c.principals,c.public_key_fingerprint,c.key_algorithm,c.key_origin,c.valid_after,c.valid_before,c.requester_principal_id,c.requester_class,c.state,c.revoked_at,c.revocation_reason,c.created_at,k.state,k.retire_after,a.state`

const sshCertificateFrom = ` FROM ssh_certificates c
JOIN ssh_ca_keys k ON k.id=c.ca_key_id AND k.org_id=c.org_id AND k.project_id=c.project_id AND k.environment_id=c.environment_id
JOIN ssh_cas a ON a.id=c.ca_id AND a.org_id=c.org_id AND a.project_id=c.project_id AND a.environment_id=c.environment_id`

func scanSSHCertificate(row interface{ Scan(...any) error }) (SSHCertificate, error) {
	var out SSHCertificate
	var principals string
	var reason *string
	var validAfter, validBefore, revokedAt, created, retireAfter adapterStoredTime
	err := row.Scan(&out.ID, &out.CAID, &out.CAKeyID, &out.ProfileID, &out.Serial, &out.KeyID, &principals, &out.PublicKeyFingerprint, &out.KeyAlgorithm, &out.KeyOrigin,
		&validAfter, &validBefore, &out.RequesterPrincipalID, &out.RequesterClass, &out.State, &revokedAt, &reason, &created, &out.CAKeyState, &retireAfter, &out.CAState)
	if isNoRows(err) {
		return SSHCertificate{}, ErrNotFound
	}
	if err != nil {
		return SSHCertificate{}, err
	}
	if out.Principals, err = decodeSSHList(principals); err != nil {
		return SSHCertificate{}, err
	}
	if reason != nil {
		out.RevocationReason = *reason
	}
	out.ValidAfter, out.ValidBefore, out.RevokedAt, out.CreatedAt, out.CAKeyRetireAfter = validAfter.value, validBefore.value, revokedAt.value, created.value, retireAfter.value
	return out, nil
}

func (r sshQueries) GetCertificate(ctx context.Context, p authz.Proof, certID string) (SSHCertificate, error) {
	chain, err := r.envChain(p, authz.StoreSSHGetCertificate)
	if err != nil {
		return SSHCertificate{}, err
	}
	query := r.db.SQL(`SELECT ` + sshCertificateColumns + sshCertificateFrom + ` WHERE c.id=? AND c.org_id=? AND c.project_id=? AND c.environment_id=?`)
	return scanSSHCertificate(r.db.QueryRow(ctx, query, certID, chain.Org, chain.Project, string(chain.Env)))
}

func (r sshQueries) ListCertificates(ctx context.Context, p authz.Proof, limit int) ([]SSHCertificate, error) {
	chain, err := r.envChain(p, authz.StoreSSHListCertificates)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(`SELECT ` + sshCertificateColumns + sshCertificateFrom + ` WHERE c.org_id=? AND c.project_id=? AND c.environment_id=? ORDER BY c.created_at DESC, c.id DESC LIMIT ?`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project, string(chain.Env), limit)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []SSHCertificate
	for rows.Next() {
		cert, err := scanSSHCertificate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cert)
	}
	return out, rows.Err()
}

func (r sshQueries) RevokeCertificate(ctx context.Context, p authz.Proof, certID, reason string, at time.Time) (bool, error) {
	chain, err := r.envChain(p, authz.StoreSSHRevokeCertificate)
	if err != nil {
		return false, err
	}
	query := r.db.SQL(`UPDATE ssh_certificates SET state='revoked',revoked_at=?,revocation_reason=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='issued'`)
	rows, err := r.db.Exec(ctx, query, r.db.Stamp(at), reason, certID, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

// RevokeProfileCertificates revokes the profile's live (issued, unexpired)
// certificates: every one when all is set, otherwise those whose requester
// is not in keepRequesters. It returns what it changed.
func (r sshQueries) RevokeProfileCertificates(ctx context.Context, p authz.Proof, profileID, reason string, keepRequesters []string, all bool, at time.Time) ([]SSHRevokedCertificate, error) {
	chain, err := r.envChain(p, authz.StoreSSHRevokeProfileCertificates)
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	for _, principal := range keepRequesters {
		keep[principal] = true
	}
	selectQuery := r.db.SQL(`SELECT id,serial,requester_principal_id FROM ssh_certificates WHERE profile_id=? AND org_id=? AND project_id=? AND environment_id=? AND state='issued' AND valid_before>? ORDER BY id`)
	rows, err := r.db.Query(ctx, selectQuery, profileID, chain.Org, chain.Project, string(chain.Env), r.db.Stamp(at))
	if err != nil {
		return nil, err
	}
	var candidates []SSHRevokedCertificate
	for rows.Next() {
		var c SSHRevokedCertificate
		if err := rows.Scan(&c.ID, &c.Serial, &c.RequesterPrincipalID); err != nil {
			closeAdapterRows(rows)
			return nil, err
		}
		if all || !keep[c.RequesterPrincipalID] {
			candidates = append(candidates, c)
		}
	}
	err = rows.Err()
	closeAdapterRows(rows)
	if err != nil {
		return nil, err
	}
	update := r.db.SQL(`UPDATE ssh_certificates SET state='revoked',revoked_at=?,revocation_reason=? WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='issued'`)
	var out []SSHRevokedCertificate
	for _, c := range candidates {
		n, err := r.db.Exec(ctx, update, r.db.Stamp(at), reason, c.ID, chain.Org, chain.Project, string(chain.Env))
		if err != nil {
			return nil, err
		}
		if n == 1 {
			out = append(out, c)
		}
	}
	return out, nil
}

// RevokedSerials lists the KRL entries of one CA: revoked, not-yet-expired
// certificates whose signing key hosts still trust (active, or retiring inside
// its overlap). limit bounds the read; the caller asks for one more than it
// accepts so an oversized KRL is detected rather than truncated.
func (r sshQueries) RevokedSerials(ctx context.Context, p authz.Proof, caID string, now time.Time, limit int) ([]SSHRevokedSerial, error) {
	chain, err := r.envChain(p, authz.StoreSSHRevokedSerials)
	if err != nil {
		return nil, err
	}
	stamp := r.db.Stamp(now)
	query := r.db.SQL(`SELECT k.public_key,c.serial FROM ssh_certificates c
JOIN ssh_ca_keys k ON k.id=c.ca_key_id AND k.org_id=c.org_id AND k.project_id=c.project_id AND k.environment_id=c.environment_id
WHERE c.ca_id=? AND c.org_id=? AND c.project_id=? AND c.environment_id=? AND c.state='revoked' AND c.valid_before>?
AND (k.state='active' OR (k.state='retiring' AND k.retire_after>?))
ORDER BY k.id, c.serial LIMIT ?`)
	rows, err := r.db.Query(ctx, query, caID, chain.Org, chain.Project, string(chain.Env), stamp, stamp, limit)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []SSHRevokedSerial
	for rows.Next() {
		var s SSHRevokedSerial
		if err := rows.Scan(&s.CAKeyPublicKey, &s.Serial); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- Reencrypt ---------------------------------------------------------------

func (r sshQueries) ListCAKeysForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error) {
	chain, err := authz.Verify(p, authz.StoreSSHListCAKeysForReencrypt, r.tok)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(`SELECT id, private_key_ciphertext FROM ssh_ca_keys WHERE org_id=? AND project_id=? AND id>? AND private_key_ciphertext IS NOT NULL ORDER BY id LIMIT ?`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []ReencryptFieldRow
	for rows.Next() {
		var id string
		var ct []byte
		if err := rows.Scan(&id, &ct); err != nil {
			return nil, err
		}
		out = append(out, ReencryptFieldRow{ID: id, Owner: id, Ciphertext: ct})
	}
	return out, rows.Err()
}

func (r sshQueries) ReencryptCAKey(ctx context.Context, p authz.Proof, id string, newCiphertext, oldCiphertext []byte) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreSSHReencryptCAKey, r.tok)
	if err != nil {
		return false, err
	}
	query := r.db.SQL(`UPDATE ssh_ca_keys SET private_key_ciphertext=? WHERE org_id=? AND project_id=? AND id=? AND private_key_ciphertext=?`)
	rows, err := r.db.Exec(ctx, query, newCiphertext, chain.Org, chain.Project, id, oldCiphertext)
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

// ErrSSHLiveCA refuses an environment delete while it still holds a live SSH
// CA: hosts may trust that CA's keys, and deleting the environment would
// silently drop its revocation list. Delete the CA first (which retires its
// keys and says so), then the environment.
var ErrSSHLiveCA = fmt.Errorf("%w: the environment still has a live SSH CA; delete it first", ErrConflict)

// PurgeEnvironment removes every SSH row of the proof's environment, as part
// of the environment delete, once no live CA remains.
func (r sshQueries) PurgeEnvironment(ctx context.Context, p authz.Proof) error {
	chain, err := r.envChain(p, authz.StoreSSHPurgeEnvironment)
	if err != nil {
		return err
	}
	env := string(chain.Env)
	var live int
	if err := r.db.QueryRow(ctx, r.db.SQL(`SELECT COUNT(*) FROM ssh_cas WHERE org_id=? AND project_id=? AND environment_id=? AND state='active'`), chain.Org, chain.Project, env).Scan(&live); err != nil {
		return err
	}
	if live > 0 {
		return ErrSSHLiveCA
	}
	for _, table := range []string{"ssh_certificates", "ssh_profile_requesters", "ssh_profiles", "ssh_ca_keys", "ssh_cas"} {
		if _, err := r.db.Exec(ctx, r.db.SQL(`DELETE FROM `+table+` WHERE org_id=? AND project_id=? AND environment_id=?`), chain.Org, chain.Project, env); err != nil {
			return err
		}
	}
	return nil
}

// mapSSHUnique turns a uniqueness violation (a live name taken, or the
// one-active-key index) into ErrConflict.
func mapSSHUnique(err error) error {
	if err == nil {
		return nil
	}
	if sqliteUniqueViolation(err) || pgUniqueViolation(err) {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}
