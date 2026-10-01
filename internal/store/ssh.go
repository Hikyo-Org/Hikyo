package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
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
	ActiveKeyLastExpiry(ctx context.Context, p authz.Proof, caID string, now time.Time) (*time.Time, error)
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
	queries sshStoreQueries
	tok     *authz.TxToken
}

func (r sqliteRepos) SSH() SSHRepo {
	return sshQueries{queries: sqliteSSHStoreQueries{queries: sqlitegen.New(r.db)}, tok: r.tok}
}
func (r pgRepos) SSH() SSHRepo {
	return sshQueries{queries: pgSSHStoreQueries{queries: pggen.New(r.db)}, tok: r.tok}
}

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
	_, err = r.queries.sshCreateCA(ctx, chain, m)
	return mapSSHUnique(err)
}

func (r sshQueries) InsertCAKey(ctx context.Context, p authz.Proof, m SSHCAKeyCreate) error {
	chain, err := r.envChain(p, authz.StoreSSHInsertCAKey)
	if err != nil {
		return err
	}
	rows, err := r.queries.sshInsertCAKey(ctx, chain, m)
	if err != nil {
		return mapSSHUnique(err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (r sshQueries) keysFor(ctx context.Context, chain domain.Scope, caIDs []string) (map[string][]SSHCAKey, error) {
	out := map[string][]SSHCAKey{}
	if len(caIDs) == 0 {
		return out, nil
	}
	keys, err := r.queries.sshCAKeys(ctx, chain)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range caIDs {
		want[id] = true
	}
	for _, key := range keys {
		if want[key.CAID] {
			out[key.CAID] = append(out[key.CAID], key)
		}
	}
	return out, nil
}

func (r sshQueries) GetCA(ctx context.Context, p authz.Proof, caID string) (SSHCA, error) {
	chain, err := r.envChain(p, authz.StoreSSHGetCA)
	if err != nil {
		return SSHCA{}, err
	}
	ca, err := r.queries.sshGetCA(ctx, chain, caID)
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
	out, err := r.queries.sshListCAs(ctx, chain)
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
	out, err := r.queries.sshActiveCAKey(ctx, chain, caID)
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
	keyID, err := r.queries.sshActiveCAKeyID(ctx, chain, caID)
	if isNoRows(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	rows, err := r.queries.sshRetireActiveCAKey(ctx, chain, keyID, at, retireAfter)
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
	rows, err := r.queries.sshRetireCAKey(ctx, chain, keyID, caID, at)
	if err != nil {
		return err
	}
	if rows != 1 {
		n, err := r.queries.sshCAKeyExists(ctx, chain, keyID, caID)
		if err != nil {
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
	rows, err := r.queries.sshDeleteCA(ctx, chain, caID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	_, err = r.queries.sshRetireAllCAKeys(ctx, chain, caID, at)
	return err
}

func (r sshQueries) CountLiveProfilesForCA(ctx context.Context, p authz.Proof, caID string) (int, error) {
	chain, err := r.envChain(p, authz.StoreSSHCountLiveProfilesForCA)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.sshCountLiveProfiles(ctx, chain, caID)
	return int(n), err
}

// ActiveKeyLastExpiry returns the latest valid_before among the live issued
// certificates the CA's active key signed, or nil when there are none. It
// reads the certificate rows, not profile bounds, so certificates of a
// deleted profile or issued under a since-lowered max_ttl are covered.
func (r sshQueries) ActiveKeyLastExpiry(ctx context.Context, p authz.Proof, caID string, now time.Time) (*time.Time, error) {
	chain, err := r.envChain(p, authz.StoreSSHActiveKeyLastExpiry)
	if err != nil {
		return nil, err
	}
	return r.queries.sshActiveKeyLastExpiry(ctx, chain, caID, now)
}

// ---- Profiles ---------------------------------------------------------------

func (r sshQueries) requestersFor(ctx context.Context, chain domain.Scope) (map[string][]string, error) {
	return r.queries.sshRequesters(ctx, chain)
}

func (r sshQueries) GetProfile(ctx context.Context, p authz.Proof, profileID string) (SSHProfile, error) {
	chain, err := r.envChain(p, authz.StoreSSHGetProfile)
	if err != nil {
		return SSHProfile{}, err
	}
	profile, err := r.queries.sshGetProfile(ctx, chain, profileID)
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
	out, err := r.queries.sshListProfiles(ctx, chain)
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
	// The CA must be live in this same environment; the SELECT binds it.
	rows, err := r.queries.sshCreateProfile(ctx, chain, m, lists)
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
	rows, err := r.queries.sshUpdateProfile(ctx, chain, m, lists)
	if err != nil {
		return mapSSHUnique(err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return r.replaceRequesters(ctx, chain, m.ID, m.Requesters, m.At)
}

func (r sshQueries) replaceRequesters(ctx context.Context, chain domain.Scope, profileID string, requesters []string, at time.Time) error {
	if _, err := r.queries.sshDeleteRequesters(ctx, chain, profileID); err != nil {
		return err
	}
	for _, principal := range requesters {
		if _, err := r.queries.sshInsertRequester(ctx, chain, profileID, principal, at); err != nil {
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
	rows, err := r.queries.sshDeleteProfile(ctx, chain, profileID, at)
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
	n, err := r.queries.sshIsRequester(ctx, chain, profileID, principalID)
	return n == 1, err
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
	rows, err := r.queries.sshInsertCertificate(ctx, chain, m, principals)
	if err != nil {
		return mapSSHUnique(err)
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}

func (r sshQueries) GetCertificate(ctx context.Context, p authz.Proof, certID string) (SSHCertificate, error) {
	chain, err := r.envChain(p, authz.StoreSSHGetCertificate)
	if err != nil {
		return SSHCertificate{}, err
	}
	return r.queries.sshGetCertificate(ctx, chain, certID)
}

func (r sshQueries) ListCertificates(ctx context.Context, p authz.Proof, limit int) ([]SSHCertificate, error) {
	chain, err := r.envChain(p, authz.StoreSSHListCertificates)
	if err != nil {
		return nil, err
	}
	return r.queries.sshListCertificates(ctx, chain, limit)
}

func (r sshQueries) RevokeCertificate(ctx context.Context, p authz.Proof, certID, reason string, at time.Time) (bool, error) {
	chain, err := r.envChain(p, authz.StoreSSHRevokeCertificate)
	if err != nil {
		return false, err
	}
	rows, err := r.queries.sshRevokeCertificate(ctx, chain, certID, reason, at)
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
	rows, err := r.queries.sshProfileRevocationCandidates(ctx, chain, profileID, at)
	if err != nil {
		return nil, err
	}
	var candidates []SSHRevokedCertificate
	for _, c := range rows {
		if all || !keep[c.RequesterPrincipalID] {
			candidates = append(candidates, c)
		}
	}
	var out []SSHRevokedCertificate
	for _, c := range candidates {
		n, err := r.queries.sshRevokeCertificate(ctx, chain, c.ID, reason, at)
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
	return r.queries.sshRevokedSerials(ctx, chain, caID, now, limit)
}

// ---- Reencrypt ---------------------------------------------------------------

func (r sshQueries) ListCAKeysForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error) {
	chain, err := authz.Verify(p, authz.StoreSSHListCAKeysForReencrypt, r.tok)
	if err != nil {
		return nil, err
	}
	return r.queries.sshListKeysForReencrypt(ctx, chain, cursor, limit)
}

func (r sshQueries) ReencryptCAKey(ctx context.Context, p authz.Proof, id string, newCiphertext, oldCiphertext []byte) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreSSHReencryptCAKey, r.tok)
	if err != nil {
		return false, err
	}
	rows, err := r.queries.sshReencryptCAKey(ctx, chain, id, newCiphertext, oldCiphertext)
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
	live, err := r.queries.sshCountLiveCAs(ctx, chain)
	if err != nil {
		return err
	}
	if live > 0 {
		return ErrSSHLiveCA
	}
	return r.queries.sshPurgeEnvironment(ctx, chain)
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
