package store

import (
	"context"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
)

// Private-PKI request-path store (#154, docs/adr/pki.md). Every method is
// proof-carrying: it verifies its proof against its registered store operation
// before touching a row. Issuer and profile methods run under instance-class
// operations (empty chain). Certificate methods run under environment-class
// operations and bind org/project/environment from the verified proof, never
// from a caller-supplied id. The worker's sweeps are the proof-free
// PKIRuntime (pki_runtime.go), the DynamicRuntime shape.

// PKIIssuer is one CA key version's public surface. It has no private-key
// field: the sealed key is read only by IssuerKey, for the in-process signer.
type PKIIssuer struct {
	ID                 string
	Name               string
	Version            int64
	Kind               string
	Origin             string
	ParentID           string
	State              string
	KeyAlgorithm       string
	KeyFingerprint     string
	KeyPresent         bool
	CertificateDER     []byte
	CSRDER             []byte
	ChainPEM           string
	SubjectCN          string
	SubjectOrg         string
	NotBefore          time.Time
	NotAfter           time.Time
	CRLDistributionURL string
	RestoreHold        bool
	IssuedCount        int64
	CRLDER             []byte
	CRLNumber          int64
	CRLThisUpdate      time.Time
	CRLNextUpdate      time.Time
	RowVersion         int64
	CreatedBy          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// PKIIssuerCreate is a new issuer version. EncryptedPrivateKey is sealed by
// the service; the store never sees key plaintext.
type PKIIssuerCreate struct {
	ID                  string
	Name                string
	Version             int64
	Kind                string
	Origin              string
	ParentID            string
	State               string
	KeyAlgorithm        string
	KeyFingerprint      string
	EncryptedPrivateKey []byte
	DEKVersion          uint32
	CertificateDER      []byte
	CSRDER              []byte
	ChainPEM            string
	SubjectCN           string
	SubjectOrg          string
	NotBefore           time.Time
	NotAfter            time.Time
	CRLDistributionURL  string
	CreatedBy           string
	At                  time.Time
}

// PKIIssuerInstall activates a pending version with its externally signed
// certificate.
type PKIIssuerInstall struct {
	ID             string
	RowVersion     int64
	CertificateDER []byte
	ChainPEM       string
	NotBefore      time.Time
	NotAfter       time.Time
	At             time.Time
}

// PKIProfile is a stored profile. Policy is the canonical JSON the service
// encodes; the store does not interpret it.
type PKIProfile struct {
	ID         string
	Name       string
	Policy     string
	RowVersion int64
	CreatedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PKIProfileBinding is one explicit reach of a profile into a project, or one
// environment of it (EnvironmentID empty = the whole project).
type PKIProfileBinding struct {
	ID            string
	ProfileID     string
	OrgID         string
	ProjectID     string
	EnvironmentID string
	CreatedBy     string
	CreatedAt     time.Time
}

// PKICertificate is an issuance record. There is no private-key field, ever.
type PKICertificate struct {
	ID               string
	EnvironmentID    string
	ProfileID        string
	ProfileName      string
	IssuerID         string
	Serial           string
	State            string
	KeySource        string
	KeyAlgorithm     string
	KeyFingerprint   string
	CommonName       string
	SANs             string
	NotBefore        time.Time
	NotAfter         time.Time
	CertificateDER   []byte
	PrincipalID      string
	PrincipalClass   string
	RenewedFrom      string
	RenewedBy        string
	RevokedAt        time.Time
	RevocationReason string
	IssuingDeadline  time.Time
	RowVersion       int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// PKICertificateCreate reserves a serial in an `issuing` row (the INTENT).
type PKICertificateCreate struct {
	ID              string
	ProfileID       string
	ProfileName     string
	IssuerID        string
	Serial          string
	KeySource       string
	KeyAlgorithm    string
	KeyFingerprint  string
	CommonName      string
	SANs            string
	NotBefore       time.Time
	NotAfter        time.Time
	PrincipalID     string
	PrincipalClass  string
	RenewedFrom     string
	IssuingDeadline time.Time
	At              time.Time
}

// PKIReader is the audit-free read side.
type PKIReader interface {
	GetCertificate(ctx context.Context, p authz.Proof, id string) (PKICertificate, error)
	ListCertificates(ctx context.Context, p authz.Proof) ([]PKICertificate, error)
	BoundProfiles(ctx context.Context, p authz.Proof) ([]PKIProfile, error)
	IssuerPublic(ctx context.Context, p authz.Proof, id string) (PKIIssuer, error)
	ActiveIssuerPublic(ctx context.Context, p authz.Proof, name string) (PKIIssuer, error)
}

// PKIRepo is the write side plus the reads.
type PKIRepo interface {
	PKIReader
	// Issuers (instance operations).
	ListIssuers(ctx context.Context, p authz.Proof) ([]PKIIssuer, error)
	GetIssuer(ctx context.Context, p authz.Proof, id string) (PKIIssuer, error)
	// IssuerKey returns the sealed private key and its DEK version for the
	// in-process signer. It is the ONLY method that returns key ciphertext.
	IssuerKey(ctx context.Context, p authz.Proof, id string) ([]byte, uint32, error)
	CreateIssuer(ctx context.Context, p authz.Proof, m PKIIssuerCreate) error
	InstallIssuerCertificate(ctx context.Context, p authz.Proof, m PKIIssuerInstall) (bool, error)
	TransitionIssuer(ctx context.Context, p authz.Proof, id, from, to string, rowVersion int64, at time.Time) (bool, error)
	DestroyIssuerKey(ctx context.Context, p authz.Proof, id, from, to string, rowVersion int64, at time.Time) (bool, error)
	SetIssuerHold(ctx context.Context, p authz.Proof, id string, hold bool, rowVersion int64, at time.Time) (bool, error)
	CountLiveCertificates(ctx context.Context, p authz.Proof, issuerID string, now time.Time) (int64, error)
	RevokeLiveCertificates(ctx context.Context, p authz.Proof, issuerID, reason string, at time.Time) (int64, error)
	RevokedEntries(ctx context.Context, p authz.Proof, issuerID string, now time.Time) ([]PKIRevokedEntry, error)
	PublishCRL(ctx context.Context, p authz.Proof, issuerID string, der []byte, previousNumber, number int64, thisUpdate, nextUpdate time.Time) (bool, error)
	// Profiles and bindings (instance operations).
	ListProfiles(ctx context.Context, p authz.Proof) ([]PKIProfile, error)
	GetProfile(ctx context.Context, p authz.Proof, name string) (PKIProfile, error)
	CreateProfile(ctx context.Context, p authz.Proof, m PKIProfile) error
	UpdateProfile(ctx context.Context, p authz.Proof, id, policy string, rowVersion int64, at time.Time) (bool, error)
	DeleteProfile(ctx context.Context, p authz.Proof, id string) (bool, error)
	ListBindings(ctx context.Context, p authz.Proof, profileID string) ([]PKIProfileBinding, error)
	CreateBinding(ctx context.Context, p authz.Proof, m PKIProfileBinding) error
	DeleteBinding(ctx context.Context, p authz.Proof, profileID, bindingID string) (bool, error)
	// Certificates (environment operations).
	BoundProfile(ctx context.Context, p authz.Proof, name string) (PKIProfile, error)
	FenceIssuance(ctx context.Context, p authz.Proof, issuerID string) (bool, error)
	CreateCertificate(ctx context.Context, p authz.Proof, m PKICertificateCreate) error
	FinishCertificate(ctx context.Context, p authz.Proof, id string, der []byte, at time.Time) (bool, error)
	FailCertificate(ctx context.Context, p authz.Proof, id string, at time.Time) (bool, error)
	ClaimRenewal(ctx context.Context, p authz.Proof, id, successorID string, at time.Time) (bool, error)
	CompleteRenewal(ctx context.Context, p authz.Proof, id, successorID string, at time.Time) (bool, error)
	ReleaseRenewal(ctx context.Context, p authz.Proof, id, successorID string, at time.Time) (bool, error)
	RevokeCertificate(ctx context.Context, p authz.Proof, id, reason string, at time.Time) (bool, error)
	IssuerForSigning(ctx context.Context, p authz.Proof, id string) (PKIIssuer, []byte, uint32, error)
}

// PKIRevokedEntry is one serial a CRL lists.
type PKIRevokedEntry struct {
	Serial    string
	RevokedAt time.Time
	Reason    string
}

type pkiQueries struct {
	db  adapterDB
	tok *authz.TxToken
}

func (r sqliteRepos) PKI() PKIRepo       { return pkiQueries{db: sqliteAdoptDB{db: r.db}, tok: r.tok} }
func (r pgRepos) PKI() PKIRepo           { return pkiQueries{db: pgAdoptDB{db: r.db}, tok: r.tok} }
func (r sqliteReadRepos) PKI() PKIReader { return r.r.PKI() }
func (r pgReadRepos) PKI() PKIReader     { return r.r.PKI() }

const pkiIssuerColumns = `id,name,version,kind,origin,COALESCE(parent_id,''),state,key_algorithm,key_fingerprint,CASE WHEN encrypted_private_key IS NULL THEN 0 ELSE 1 END,certificate_der,csr_der,chain_pem,subject_cn,subject_org,not_before,not_after,crl_distribution_url,restore_hold,issued_count,crl_der,crl_number,crl_this_update,crl_next_update,row_version,created_by,created_at,updated_at`

type rowScanner interface{ Scan(...any) error }

func pkiTime(t adapterStoredTime) (time.Time, error) {
	parsed, err := t.Time()
	if err != nil || parsed == nil {
		return time.Time{}, err
	}
	return *parsed, nil
}

func pkiTimes(out []*time.Time, in []adapterStoredTime) error {
	for i := range in {
		t, err := pkiTime(in[i])
		if err != nil {
			return err
		}
		*out[i] = t
	}
	return nil
}

func scanPKIIssuer(row rowScanner) (PKIIssuer, error) {
	var out PKIIssuer
	var keyPresent, hold int
	stamps := make([]adapterStoredTime, 6)
	err := row.Scan(&out.ID, &out.Name, &out.Version, &out.Kind, &out.Origin, &out.ParentID, &out.State,
		&out.KeyAlgorithm, &out.KeyFingerprint, &keyPresent, &out.CertificateDER, &out.CSRDER, &out.ChainPEM,
		&out.SubjectCN, &out.SubjectOrg, &stamps[0], &stamps[1], &out.CRLDistributionURL, &hold,
		&out.IssuedCount, &out.CRLDER, &out.CRLNumber, &stamps[2], &stamps[3], &out.RowVersion,
		&out.CreatedBy, &stamps[4], &stamps[5])
	if isNoRows(err) {
		return PKIIssuer{}, ErrNotFound
	}
	if err != nil {
		return PKIIssuer{}, err
	}
	out.KeyPresent, out.RestoreHold = keyPresent == 1, hold == 1
	return out, pkiTimes([]*time.Time{&out.NotBefore, &out.NotAfter, &out.CRLThisUpdate, &out.CRLNextUpdate, &out.CreatedAt, &out.UpdatedAt}, stamps)
}

func collectPKI[T any](rows adapterTargetRows, err error, scan func(rowScanner) (T, error)) ([]T, error) {
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	var out []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// closeRows releases a result set on every return path. database/sql rows
// return an error from Close and pgx rows do not.
func closeRows(rows adapterTargetRows) {
	switch r := rows.(type) {
	case interface{ Close() error }:
		_ = r.Close()
	case interface{ Close() }:
		r.Close()
	}
}

func stampOrNil(db adapterDB, t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return db.Stamp(t)
}

func affectedOne(n int64, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// --- Issuers -----------------------------------------------------------------

func (r pkiQueries) ListIssuers(ctx context.Context, p authz.Proof) ([]PKIIssuer, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersList, r.tok); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+pkiIssuerColumns+` FROM pki_issuers ORDER BY name, version`)
	return collectPKI(rows, err, scanPKIIssuer)
}

func (r pkiQueries) GetIssuer(ctx context.Context, p authz.Proof, id string) (PKIIssuer, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersGet, r.tok); err != nil {
		return PKIIssuer{}, err
	}
	return scanPKIIssuer(r.db.QueryRow(ctx, r.db.SQL(`SELECT `+pkiIssuerColumns+` FROM pki_issuers WHERE id=?`), id))
}

func (r pkiQueries) IssuerKey(ctx context.Context, p authz.Proof, id string) ([]byte, uint32, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersKey, r.tok); err != nil {
		return nil, 0, err
	}
	return r.issuerKey(ctx, id)
}

func (r pkiQueries) issuerKey(ctx context.Context, id string) ([]byte, uint32, error) {
	var ct []byte
	var version int64
	err := r.db.QueryRow(ctx, r.db.SQL(`SELECT encrypted_private_key, dek_version FROM pki_issuers WHERE id=? AND encrypted_private_key IS NOT NULL AND dek_version IS NOT NULL`), id).Scan(&ct, &version)
	if isNoRows(err) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	return ct, uint32(version), nil
}

func (r pkiQueries) CreateIssuer(ctx context.Context, p authz.Proof, m PKIIssuerCreate) error {
	if _, err := authz.Verify(p, authz.StorePKIIssuersCreate, r.tok); err != nil {
		return err
	}
	var parent any
	if m.ParentID != "" {
		parent = m.ParentID
	}
	stamp := r.db.Stamp(m.At)
	query := r.db.SQL(`INSERT INTO pki_issuers (id,name,version,kind,origin,parent_id,state,key_algorithm,key_fingerprint,encrypted_private_key,dek_version,certificate_der,csr_der,chain_pem,subject_cn,subject_org,not_before,not_after,crl_distribution_url,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	_, err := r.db.Exec(ctx, query, m.ID, m.Name, m.Version, m.Kind, m.Origin, parent, m.State, m.KeyAlgorithm,
		m.KeyFingerprint, m.EncryptedPrivateKey, int64(m.DEKVersion), nilIfEmpty(m.CertificateDER), nilIfEmpty(m.CSRDER),
		m.ChainPEM, m.SubjectCN, m.SubjectOrg, stampOrNil(r.db, m.NotBefore), stampOrNil(r.db, m.NotAfter),
		m.CRLDistributionURL, m.CreatedBy, stamp, stamp)
	return err
}

func nilIfEmpty(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func (r pkiQueries) InstallIssuerCertificate(ctx context.Context, p authz.Proof, m PKIIssuerInstall) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersInstall, r.tok); err != nil {
		return false, err
	}
	query := r.db.SQL(`UPDATE pki_issuers SET state='active', certificate_der=?, chain_pem=?, not_before=?, not_after=?, row_version=row_version+1, updated_at=? WHERE id=? AND state='pending' AND row_version=?`)
	return affectedOne(r.db.Exec(ctx, query, m.CertificateDER, m.ChainPEM, r.db.Stamp(m.NotBefore), r.db.Stamp(m.NotAfter), r.db.Stamp(m.At), m.ID, m.RowVersion))
}

func (r pkiQueries) TransitionIssuer(ctx context.Context, p authz.Proof, id, from, to string, rowVersion int64, at time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersTransition, r.tok); err != nil {
		return false, err
	}
	query := r.db.SQL(`UPDATE pki_issuers SET state=?, row_version=row_version+1, updated_at=? WHERE id=? AND state=? AND row_version=?`)
	return affectedOne(r.db.Exec(ctx, query, to, r.db.Stamp(at), id, from, rowVersion))
}

// DestroyIssuerKey is the terminal transition: the sealed key is overwritten
// with NULL in the same statement that moves the state, so a retired or
// revoked version can never sign again (the CHECK constraint pins the pair).
func (r pkiQueries) DestroyIssuerKey(ctx context.Context, p authz.Proof, id, from, to string, rowVersion int64, at time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersDestroyKey, r.tok); err != nil {
		return false, err
	}
	if to != "retired" && to != "revoked" {
		return false, fmt.Errorf("store: %q is not a key-destroying state", to)
	}
	query := r.db.SQL(`UPDATE pki_issuers SET state=?, encrypted_private_key=NULL, dek_version=NULL, row_version=row_version+1, updated_at=? WHERE id=? AND state=? AND row_version=?`)
	return affectedOne(r.db.Exec(ctx, query, to, r.db.Stamp(at), id, from, rowVersion))
}

func (r pkiQueries) SetIssuerHold(ctx context.Context, p authz.Proof, id string, hold bool, rowVersion int64, at time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersHold, r.tok); err != nil {
		return false, err
	}
	value := 0
	if hold {
		value = 1
	}
	query := r.db.SQL(`UPDATE pki_issuers SET restore_hold=?, row_version=row_version+1, updated_at=? WHERE id=? AND row_version=?`)
	return affectedOne(r.db.Exec(ctx, query, value, r.db.Stamp(at), id, rowVersion))
}

// CountLiveCertificates counts leaves of one issuer version that relying
// parties may still accept: issued, renewed or unknown, not yet expired.
func (r pkiQueries) CountLiveCertificates(ctx context.Context, p authz.Proof, issuerID string, now time.Time) (int64, error) {
	if _, err := authz.Verify(p, authz.StorePKICertificatesCountLive, r.tok); err != nil {
		return 0, err
	}
	var n int64
	err := r.db.QueryRow(ctx, r.db.SQL(`SELECT COUNT(*) FROM pki_certificates WHERE issuer_id=? AND state IN ('issuing','issued','renewed','unknown') AND not_after>?`), issuerID, r.db.Stamp(now)).Scan(&n)
	return n, err
}

// RevokeLiveCertificates is the compromised-issuer cascade: every live leaf of
// the version is revoked with the system-only ca-compromise reason.
func (r pkiQueries) RevokeLiveCertificates(ctx context.Context, p authz.Proof, issuerID, reason string, at time.Time) (int64, error) {
	if _, err := authz.Verify(p, authz.StorePKICertificatesRevokeLive, r.tok); err != nil {
		return 0, err
	}
	stamp := r.db.Stamp(at)
	query := r.db.SQL(`UPDATE pki_certificates SET state='revoked', revoked_at=?, revocation_reason=?, row_version=row_version+1, updated_at=? WHERE issuer_id=? AND state IN ('issuing','issued','renewed','unknown')`)
	return r.db.Exec(ctx, query, stamp, reason, stamp, issuerID)
}

func (r pkiQueries) RevokedEntries(ctx context.Context, p authz.Proof, issuerID string, now time.Time) ([]PKIRevokedEntry, error) {
	if _, err := authz.Verify(p, authz.StorePKICertificatesRevokedEntries, r.tok); err != nil {
		return nil, err
	}
	return revokedEntries(ctx, r.db, issuerID, now)
}

func revokedEntries(ctx context.Context, db adapterDB, issuerID string, now time.Time) ([]PKIRevokedEntry, error) {
	rows, err := db.Query(ctx, db.SQL(`SELECT serial, COALESCE(revoked_at, updated_at), COALESCE(revocation_reason, 'unspecified') FROM pki_certificates WHERE issuer_id=? AND state IN ('revoked','unknown') AND not_after>? ORDER BY serial`), issuerID, db.Stamp(now))
	return collectPKI(rows, err, func(row rowScanner) (PKIRevokedEntry, error) {
		var out PKIRevokedEntry
		var at adapterStoredTime
		if err := row.Scan(&out.Serial, &at, &out.Reason); err != nil {
			return out, err
		}
		var parseErr error
		out.RevokedAt, parseErr = pkiTime(at)
		return out, parseErr
	})
}

func (r pkiQueries) PublishCRL(ctx context.Context, p authz.Proof, issuerID string, der []byte, previousNumber, number int64, thisUpdate, nextUpdate time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersPublishCRL, r.tok); err != nil {
		return false, err
	}
	return publishCRL(ctx, r.db, issuerID, der, previousNumber, number, thisUpdate, nextUpdate)
}

// publishCRL stores a CRL under a CAS on the prior CRL number, so two nodes
// racing to republish cannot move the number backwards.
func publishCRL(ctx context.Context, db adapterDB, issuerID string, der []byte, previousNumber, number int64, thisUpdate, nextUpdate time.Time) (bool, error) {
	query := db.SQL(`UPDATE pki_issuers SET crl_der=?, crl_number=?, crl_this_update=?, crl_next_update=? WHERE id=? AND crl_number=? AND state IN ('active','retiring')`)
	return affectedOne(db.Exec(ctx, query, der, number, db.Stamp(thisUpdate), db.Stamp(nextUpdate), issuerID, previousNumber))
}

// --- Profiles and bindings ----------------------------------------------------

const pkiProfileColumns = `id,name,policy,row_version,created_by,created_at,updated_at`

func scanPKIProfile(row rowScanner) (PKIProfile, error) {
	var out PKIProfile
	stamps := make([]adapterStoredTime, 2)
	err := row.Scan(&out.ID, &out.Name, &out.Policy, &out.RowVersion, &out.CreatedBy, &stamps[0], &stamps[1])
	if isNoRows(err) {
		return PKIProfile{}, ErrNotFound
	}
	if err != nil {
		return PKIProfile{}, err
	}
	return out, pkiTimes([]*time.Time{&out.CreatedAt, &out.UpdatedAt}, stamps)
}

func (r pkiQueries) ListProfiles(ctx context.Context, p authz.Proof) ([]PKIProfile, error) {
	if _, err := authz.Verify(p, authz.StorePKIProfilesList, r.tok); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+pkiProfileColumns+` FROM pki_profiles ORDER BY name`)
	return collectPKI(rows, err, scanPKIProfile)
}

func (r pkiQueries) GetProfile(ctx context.Context, p authz.Proof, name string) (PKIProfile, error) {
	if _, err := authz.Verify(p, authz.StorePKIProfilesGet, r.tok); err != nil {
		return PKIProfile{}, err
	}
	return scanPKIProfile(r.db.QueryRow(ctx, r.db.SQL(`SELECT `+pkiProfileColumns+` FROM pki_profiles WHERE name=?`), name))
}

func (r pkiQueries) CreateProfile(ctx context.Context, p authz.Proof, m PKIProfile) error {
	if _, err := authz.Verify(p, authz.StorePKIProfilesCreate, r.tok); err != nil {
		return err
	}
	stamp := r.db.Stamp(m.CreatedAt)
	_, err := r.db.Exec(ctx, r.db.SQL(`INSERT INTO pki_profiles (id,name,policy,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?)`),
		m.ID, m.Name, m.Policy, m.CreatedBy, stamp, stamp)
	return err
}

func (r pkiQueries) UpdateProfile(ctx context.Context, p authz.Proof, id, policy string, rowVersion int64, at time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIProfilesUpdate, r.tok); err != nil {
		return false, err
	}
	return affectedOne(r.db.Exec(ctx, r.db.SQL(`UPDATE pki_profiles SET policy=?, row_version=row_version+1, updated_at=? WHERE id=? AND row_version=?`),
		policy, r.db.Stamp(at), id, rowVersion))
}

func (r pkiQueries) DeleteProfile(ctx context.Context, p authz.Proof, id string) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIProfilesDelete, r.tok); err != nil {
		return false, err
	}
	if _, err := r.db.Exec(ctx, r.db.SQL(`DELETE FROM pki_profile_bindings WHERE profile_id=?`), id); err != nil {
		return false, err
	}
	return affectedOne(r.db.Exec(ctx, r.db.SQL(`DELETE FROM pki_profiles WHERE id=?`), id))
}

const pkiBindingColumns = `id,profile_id,org_id,project_id,COALESCE(environment_id,''),created_by,created_at`

func scanPKIBinding(row rowScanner) (PKIProfileBinding, error) {
	var out PKIProfileBinding
	var created adapterStoredTime
	if err := row.Scan(&out.ID, &out.ProfileID, &out.OrgID, &out.ProjectID, &out.EnvironmentID, &out.CreatedBy, &created); err != nil {
		if isNoRows(err) {
			return out, ErrNotFound
		}
		return out, err
	}
	var err error
	out.CreatedAt, err = pkiTime(created)
	return out, err
}

func (r pkiQueries) ListBindings(ctx context.Context, p authz.Proof, profileID string) ([]PKIProfileBinding, error) {
	if _, err := authz.Verify(p, authz.StorePKIBindingsList, r.tok); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, r.db.SQL(`SELECT `+pkiBindingColumns+` FROM pki_profile_bindings WHERE profile_id=? ORDER BY org_id, project_id, environment_id`), profileID)
	return collectPKI(rows, err, scanPKIBinding)
}

// CreateBinding records an operator's explicit binding. The ids are the
// target the instance operator named (instance operations carry no tenant
// chain); the foreign keys refuse a project or environment that does not
// exist, and the unique index refuses a duplicate.
func (r pkiQueries) CreateBinding(ctx context.Context, p authz.Proof, m PKIProfileBinding) error {
	if _, err := authz.Verify(p, authz.StorePKIBindingsCreate, r.tok); err != nil {
		return err
	}
	var env any
	if m.EnvironmentID != "" {
		env = m.EnvironmentID
	}
	_, err := r.db.Exec(ctx, r.db.SQL(`INSERT INTO pki_profile_bindings (id,profile_id,org_id,project_id,environment_id,created_by,created_at) VALUES (?,?,?,?,?,?,?)`),
		m.ID, m.ProfileID, m.OrgID, m.ProjectID, env, m.CreatedBy, r.db.Stamp(m.CreatedAt))
	return err
}

func (r pkiQueries) DeleteBinding(ctx context.Context, p authz.Proof, profileID, bindingID string) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIBindingsDelete, r.tok); err != nil {
		return false, err
	}
	return affectedOne(r.db.Exec(ctx, r.db.SQL(`DELETE FROM pki_profile_bindings WHERE id=? AND profile_id=?`), bindingID, profileID))
}

// BoundProfile resolves a profile by name only if a binding reaches the
// proof's environment: the whole project, or exactly this environment. An
// unbound profile is indistinguishable from a missing one.
func (r pkiQueries) BoundProfile(ctx context.Context, p authz.Proof, name string) (PKIProfile, error) {
	chain, err := authz.Verify(p, authz.StorePKIProfilesBound, r.tok)
	if err != nil {
		return PKIProfile{}, err
	}
	query := r.db.SQL(`SELECT ` + pkiProfileColumns + ` FROM pki_profiles WHERE name=? AND id IN (SELECT profile_id FROM pki_profile_bindings WHERE org_id=? AND project_id=? AND (environment_id IS NULL OR environment_id=?))`)
	return scanPKIProfile(r.db.QueryRow(ctx, query, name, chain.Org, chain.Project, chain.Env))
}

func (r pkiQueries) BoundProfiles(ctx context.Context, p authz.Proof) ([]PKIProfile, error) {
	chain, err := authz.Verify(p, authz.StorePKIProfilesBoundList, r.tok)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(`SELECT ` + pkiProfileColumns + ` FROM pki_profiles WHERE id IN (SELECT profile_id FROM pki_profile_bindings WHERE org_id=? AND project_id=? AND (environment_id IS NULL OR environment_id=?)) ORDER BY name`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project, chain.Env)
	return collectPKI(rows, err, scanPKIProfile)
}

// --- Certificates -------------------------------------------------------------

const pkiCertificateColumns = `id,environment_id,profile_id,profile_name,issuer_id,serial,state,key_source,key_algorithm,key_fingerprint,common_name,sans,not_before,not_after,certificate_der,principal_id,principal_class,COALESCE(renewed_from,''),COALESCE(renewed_by,''),revoked_at,COALESCE(revocation_reason,''),issuing_deadline,row_version,created_at,updated_at`

func scanPKICertificate(row rowScanner) (PKICertificate, error) {
	var out PKICertificate
	stamps := make([]adapterStoredTime, 6)
	err := row.Scan(&out.ID, &out.EnvironmentID, &out.ProfileID, &out.ProfileName, &out.IssuerID, &out.Serial,
		&out.State, &out.KeySource, &out.KeyAlgorithm, &out.KeyFingerprint, &out.CommonName, &out.SANs,
		&stamps[0], &stamps[1], &out.CertificateDER, &out.PrincipalID, &out.PrincipalClass, &out.RenewedFrom,
		&out.RenewedBy, &stamps[2], &out.RevocationReason, &stamps[3], &out.RowVersion, &stamps[4], &stamps[5])
	if isNoRows(err) {
		return PKICertificate{}, ErrNotFound
	}
	if err != nil {
		return PKICertificate{}, err
	}
	return out, pkiTimes([]*time.Time{&out.NotBefore, &out.NotAfter, &out.RevokedAt, &out.IssuingDeadline, &out.CreatedAt, &out.UpdatedAt}, stamps)
}

func (r pkiQueries) GetCertificate(ctx context.Context, p authz.Proof, id string) (PKICertificate, error) {
	chain, err := authz.Verify(p, authz.StorePKICertificatesGet, r.tok)
	if err != nil {
		return PKICertificate{}, err
	}
	query := r.db.SQL(`SELECT ` + pkiCertificateColumns + ` FROM pki_certificates WHERE id=? AND org_id=? AND project_id=? AND environment_id=?`)
	return scanPKICertificate(r.db.QueryRow(ctx, query, id, chain.Org, chain.Project, chain.Env))
}

func (r pkiQueries) ListCertificates(ctx context.Context, p authz.Proof) ([]PKICertificate, error) {
	chain, err := authz.Verify(p, authz.StorePKICertificatesList, r.tok)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(`SELECT ` + pkiCertificateColumns + ` FROM pki_certificates WHERE org_id=? AND project_id=? AND environment_id=? ORDER BY created_at DESC, id DESC LIMIT 500`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project, chain.Env)
	return collectPKI(rows, err, scanPKICertificate)
}

// IssuerPublic reads an issuer's public surface from an environment operation
// (the certificate response carries the issuing chain; the CRL route serves
// the stored CRL). It never returns key material.
func (r pkiQueries) IssuerPublic(ctx context.Context, p authz.Proof, id string) (PKIIssuer, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersPublic, r.tok); err != nil {
		return PKIIssuer{}, err
	}
	return scanPKIIssuer(r.db.QueryRow(ctx, r.db.SQL(`SELECT `+pkiIssuerColumns+` FROM pki_issuers WHERE id=?`), id))
}

// ActiveIssuerPublic resolves the signing version of a named issuer.
func (r pkiQueries) ActiveIssuerPublic(ctx context.Context, p authz.Proof, name string) (PKIIssuer, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersActivePublic, r.tok); err != nil {
		return PKIIssuer{}, err
	}
	return scanPKIIssuer(r.db.QueryRow(ctx, r.db.SQL(`SELECT `+pkiIssuerColumns+` FROM pki_issuers WHERE name=? AND state='active'`), name))
}

// IssuerForSigning returns the issuer's public row plus its sealed key, for
// the leaf signer. The caller must still FenceIssuance before committing.
func (r pkiQueries) IssuerForSigning(ctx context.Context, p authz.Proof, id string) (PKIIssuer, []byte, uint32, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersSigning, r.tok); err != nil {
		return PKIIssuer{}, nil, 0, err
	}
	issuer, err := scanPKIIssuer(r.db.QueryRow(ctx, r.db.SQL(`SELECT `+pkiIssuerColumns+` FROM pki_issuers WHERE id=? AND state='active'`), id))
	if err != nil {
		return PKIIssuer{}, nil, 0, err
	}
	ct, version, err := r.issuerKey(ctx, id)
	return issuer, ct, version, err
}

// FenceIssuance is the cross-node issuance fence (ADR D6): a write to the
// issuer row that succeeds only while the version is active and not held. A
// concurrent retire, revoke, rotation or restore hold makes it touch zero
// rows (and on PostgreSQL SERIALIZABLE aborts the loser of a race).
func (r pkiQueries) FenceIssuance(ctx context.Context, p authz.Proof, issuerID string) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersFence, r.tok); err != nil {
		return false, err
	}
	return affectedOne(r.db.Exec(ctx, r.db.SQL(`UPDATE pki_issuers SET issued_count=issued_count+1 WHERE id=? AND state='active' AND restore_hold=0`), issuerID))
}

func (r pkiQueries) CreateCertificate(ctx context.Context, p authz.Proof, m PKICertificateCreate) error {
	chain, err := authz.Verify(p, authz.StorePKICertificatesCreate, r.tok)
	if err != nil {
		return err
	}
	var renewedFrom any
	if m.RenewedFrom != "" {
		renewedFrom = m.RenewedFrom
	}
	stamp := r.db.Stamp(m.At)
	query := r.db.SQL(`INSERT INTO pki_certificates (id,org_id,project_id,environment_id,profile_id,profile_name,issuer_id,serial,state,key_source,key_algorithm,key_fingerprint,common_name,sans,not_before,not_after,principal_id,principal_class,renewed_from,issuing_deadline,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,'issuing',?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	_, err = r.db.Exec(ctx, query, m.ID, chain.Org, chain.Project, chain.Env, m.ProfileID, m.ProfileName, m.IssuerID,
		m.Serial, m.KeySource, m.KeyAlgorithm, m.KeyFingerprint, m.CommonName, m.SANs, r.db.Stamp(m.NotBefore),
		r.db.Stamp(m.NotAfter), m.PrincipalID, m.PrincipalClass, renewedFrom, r.db.Stamp(m.IssuingDeadline), stamp, stamp)
	return err
}

func (r pkiQueries) certificateCAS(ctx context.Context, p authz.Proof, op authz.StoreOp, set, where string, args ...any) (bool, error) {
	chain, err := authz.Verify(p, op, r.tok)
	if err != nil {
		return false, err
	}
	query := r.db.SQL(`UPDATE pki_certificates SET ` + set + `, row_version=row_version+1 WHERE ` + where + ` AND org_id=? AND project_id=? AND environment_id=?`)
	return affectedOne(r.db.Exec(ctx, query, append(args, chain.Org, chain.Project, chain.Env)...))
}

func (r pkiQueries) FinishCertificate(ctx context.Context, p authz.Proof, id string, der []byte, at time.Time) (bool, error) {
	return r.certificateCAS(ctx, p, authz.StorePKICertificatesFinish, `state='issued', certificate_der=?, updated_at=?`,
		`id=? AND state='issuing'`, der, r.db.Stamp(at), id)
}

func (r pkiQueries) FailCertificate(ctx context.Context, p authz.Proof, id string, at time.Time) (bool, error) {
	return r.certificateCAS(ctx, p, authz.StorePKICertificatesFail, `state='failed', updated_at=?`,
		`id=? AND state='issuing'`, r.db.Stamp(at), id)
}

// ClaimRenewal is the single-winner renewal claim: only an issued certificate
// with no renewal in flight can be claimed, so two nodes renewing at once
// produce one successor.
func (r pkiQueries) ClaimRenewal(ctx context.Context, p authz.Proof, id, successorID string, at time.Time) (bool, error) {
	return r.certificateCAS(ctx, p, authz.StorePKICertificatesClaimRenewal, `renewed_by=?, updated_at=?`,
		`id=? AND state='issued' AND renewed_by IS NULL`, successorID, r.db.Stamp(at), id)
}

func (r pkiQueries) CompleteRenewal(ctx context.Context, p authz.Proof, id, successorID string, at time.Time) (bool, error) {
	return r.certificateCAS(ctx, p, authz.StorePKICertificatesCompleteRenewal, `state='renewed', updated_at=?`,
		`id=? AND state='issued' AND renewed_by=?`, r.db.Stamp(at), id, successorID)
}

func (r pkiQueries) ReleaseRenewal(ctx context.Context, p authz.Proof, id, successorID string, at time.Time) (bool, error) {
	return r.certificateCAS(ctx, p, authz.StorePKICertificatesReleaseRenewal, `renewed_by=NULL, updated_at=?`,
		`id=? AND renewed_by=?`, r.db.Stamp(at), id, successorID)
}

func (r pkiQueries) RevokeCertificate(ctx context.Context, p authz.Proof, id, reason string, at time.Time) (bool, error) {
	stamp := r.db.Stamp(at)
	return r.certificateCAS(ctx, p, authz.StorePKICertificatesRevoke, `state='revoked', revoked_at=?, revocation_reason=?, updated_at=?`,
		`id=? AND state IN ('issued','renewed','unknown')`, stamp, reason, stamp, id)
}
