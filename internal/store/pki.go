package store

import (
	"context"
	"crypto/x509"
	"fmt"
	"sort"
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
	RevocationSeq      int64
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
	PublishCRL(ctx context.Context, p authz.Proof, issuerID string, der []byte, previousNumber, number, revocationSeq int64, thisUpdate, nextUpdate time.Time) (bool, error)
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

	return r.db.pkiStoreQueries().pkiListIssuers(ctx)
}

func (r pkiQueries) GetIssuer(ctx context.Context, p authz.Proof, id string) (PKIIssuer, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersGet, r.tok); err != nil {
		return PKIIssuer{}, err
	}

	return r.db.pkiStoreQueries().pkiGetIssuer(ctx, id)
}

// IssuerKey returns the sealed key and DEK version after verifying the proof.
// An absent issuer or destroyed key returns ErrNotFound; proof and database
// errors are propagated.
func (r pkiQueries) IssuerKey(ctx context.Context, p authz.Proof, id string) ([]byte, uint32, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersKey, r.tok); err != nil {
		return nil, 0, err
	}
	return r.issuerKey(ctx, id)
}

func (r pkiQueries) issuerKey(ctx context.Context, id string) ([]byte, uint32, error) {
	key, err := r.db.pkiStoreQueries().pkiIssuerKey(ctx, id)
	return key.ciphertext, key.version, err
}

func (r pkiQueries) CreateIssuer(ctx context.Context, p authz.Proof, m PKIIssuerCreate) error {
	if _, err := authz.Verify(p, authz.StorePKIIssuersCreate, r.tok); err != nil {
		return err
	}

	return r.db.pkiStoreQueries().pkiCreateIssuer(ctx, m)
}

// InstallIssuerCertificate activates a pending issuer only if its row version
// matches. It reports whether a row changed; a missing or changed issuer returns
// false without error, while proof and database failures return errors.
func (r pkiQueries) InstallIssuerCertificate(ctx context.Context, p authz.Proof, m PKIIssuerInstall) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersInstall, r.tok); err != nil {
		return false, err
	}

	return affectedOne(r.db.pkiStoreQueries().pkiInstallIssuer(ctx, m))
}

// TransitionIssuer changes state only when both from and rowVersion match,
// incrementing the row version. A missing or changed issuer returns false
// without error; proof and database failures return errors.
func (r pkiQueries) TransitionIssuer(ctx context.Context, p authz.Proof, id, from, to string, rowVersion int64, at time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersTransition, r.tok); err != nil {
		return false, err
	}

	return affectedOne(r.db.pkiStoreQueries().pkiTransitionIssuer(ctx, id, from, to, rowVersion, at))
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
	q := r.db.pkiStoreQueries()
	changed, err := affectedOne(q.pkiDestroyIssuerKey(ctx, id, from, to, rowVersion, at))
	if err == nil && changed && to == "revoked" {
		err = q.pkiRevokeParent(ctx, id)
	}
	return changed, err
}

// SetIssuerHold changes the restore hold only at the expected row version.
// It reports whether a row changed and propagates proof and database errors.
func (r pkiQueries) SetIssuerHold(ctx context.Context, p authz.Proof, id string, hold bool, rowVersion int64, at time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersHold, r.tok); err != nil {
		return false, err
	}

	var value int64
	if hold {
		value = 1
	}
	return affectedOne(r.db.pkiStoreQueries().pkiSetIssuerHold(ctx, id, value, rowVersion, at))
}

// CountLiveCertificates counts unexpired leaves, including revoked leaves
// that still require fresh CRLs, plus reserved/unknown issuances and unexpired
// child CA certificates in every state. Retirement must retain the signing
// key until no certificate depends on this issuer's revocation coverage.
func (r pkiQueries) CountLiveCertificates(ctx context.Context, p authz.Proof, issuerID string, now time.Time) (int64, error) {
	if _, err := authz.Verify(p, authz.StorePKICertificatesCountLive, r.tok); err != nil {
		return 0, err
	}

	return r.db.pkiStoreQueries().pkiCountLiveCertificates(ctx, issuerID, now)
}

// RevokeLiveCertificates revokes all issuing, issued, renewed, and unknown
// rows for an issuer, regardless of expiry, using the supplied reason. The
// compromise caller supplies ca-compromise. It returns the affected count
// and propagates proof and database errors.
func (r pkiQueries) RevokeLiveCertificates(ctx context.Context, p authz.Proof, issuerID, reason string, at time.Time) (int64, error) {
	if _, err := authz.Verify(p, authz.StorePKICertificatesRevokeLive, r.tok); err != nil {
		return 0, err
	}

	q := r.db.pkiStoreQueries()
	changed, err := q.pkiRevokeLiveCertificates(ctx, issuerID, reason, at)
	if err == nil && changed > 0 {
		err = q.pkiBumpIssuerRevocation(ctx, issuerID)
	}
	return changed, err
}

// RevokedEntries returns unexpired revoked and unknown serials, ordered by
// serial, for the issuer's CRL. Missing revocation times and reasons fall back
// to the update time and unspecified reason. Proof, database, and timestamp
// errors are propagated.
func (r pkiQueries) RevokedEntries(ctx context.Context, p authz.Proof, issuerID string, now time.Time) ([]PKIRevokedEntry, error) {
	if _, err := authz.Verify(p, authz.StorePKICertificatesRevokedEntries, r.tok); err != nil {
		return nil, err
	}
	return revokedEntries(ctx, r.db, issuerID, now)
}

func revokedEntries(ctx context.Context, db adapterDB, issuerID string, now time.Time) ([]PKIRevokedEntry, error) {
	q := db.pkiStoreQueries()
	entries, err := q.pkiRevokedEntries(ctx, issuerID, now)
	if err != nil {
		return nil, err
	}
	children, err := q.pkiRevokedChildren(ctx, issuerID, now)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		cert, err := x509.ParseCertificate(child.der)
		if err != nil {
			return nil, fmt.Errorf("store: revoked child issuer certificate: %w", err)
		}
		if !cert.IsCA || cert.SerialNumber.Sign() <= 0 {
			return nil, fmt.Errorf("store: revoked child issuer certificate has invalid CA serial")
		}
		entries = append(entries, PKIRevokedEntry{Serial: cert.SerialNumber.Text(16), RevokedAt: child.updatedAt, Reason: "ca-compromise"})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Serial < entries[j].Serial })
	return entries, nil
}

// PublishCRL stores DER and update times only for an unheld active or retiring
// issuer whose CRL number equals previousNumber. It returns false without error when
// no row matches; proof and database errors are propagated. The caller must
// choose a number greater than previousNumber and capture revocationSeq before
// reading the entries included in this CRL.
func (r pkiQueries) PublishCRL(ctx context.Context, p authz.Proof, issuerID string, der []byte, previousNumber, number, revocationSeq int64, thisUpdate, nextUpdate time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersPublishCRL, r.tok); err != nil {
		return false, err
	}
	return publishCRL(ctx, r.db, issuerID, der, previousNumber, number, revocationSeq, thisUpdate, nextUpdate)
}

// publishCRL stores a CRL for an unheld active or retiring issuer under a CAS on
// the prior CRL number. A restore hold rejects even pre-hold candidates. The
// caller must supply an increasing number; this helper does not enforce it.
func publishCRL(ctx context.Context, db adapterDB, issuerID string, der []byte, previousNumber, number, revocationSeq int64, thisUpdate, nextUpdate time.Time) (bool, error) {
	return affectedOne(db.pkiStoreQueries().pkiPublishCRL(ctx, issuerID, der, previousNumber, number, revocationSeq, thisUpdate, nextUpdate))
}

// --- Profiles and bindings ----------------------------------------------------

func (r pkiQueries) ListProfiles(ctx context.Context, p authz.Proof) ([]PKIProfile, error) {
	if _, err := authz.Verify(p, authz.StorePKIProfilesList, r.tok); err != nil {
		return nil, err
	}

	return r.db.pkiStoreQueries().pkiListProfiles(ctx)
}

func (r pkiQueries) GetProfile(ctx context.Context, p authz.Proof, name string) (PKIProfile, error) {
	if _, err := authz.Verify(p, authz.StorePKIProfilesGet, r.tok); err != nil {
		return PKIProfile{}, err
	}

	return r.db.pkiStoreQueries().pkiGetProfile(ctx, name)
}

func (r pkiQueries) CreateProfile(ctx context.Context, p authz.Proof, m PKIProfile) error {
	if _, err := authz.Verify(p, authz.StorePKIProfilesCreate, r.tok); err != nil {
		return err
	}

	return r.db.pkiStoreQueries().pkiCreateProfile(ctx, m)
}

// UpdateProfile replaces stored policy JSON at the expected row version and
// reports whether a row changed. The caller must validate and check narrowing;
// proof and database errors are propagated.
func (r pkiQueries) UpdateProfile(ctx context.Context, p authz.Proof, id, policy string, rowVersion int64, at time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIProfilesUpdate, r.tok); err != nil {
		return false, err
	}

	return affectedOne(r.db.pkiStoreQueries().pkiUpdateProfile(ctx, id, policy, rowVersion, at))
}

// DeleteProfile removes a profile's bindings and then the profile in the
// caller's transaction, reporting whether the profile existed. Proof and
// database errors are propagated.
func (r pkiQueries) DeleteProfile(ctx context.Context, p authz.Proof, id string) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIProfilesDelete, r.tok); err != nil {
		return false, err
	}

	q := r.db.pkiStoreQueries()
	if err := q.pkiDeleteProfileBindings(ctx, id); err != nil {
		return false, err
	}
	return affectedOne(q.pkiDeleteProfile(ctx, id))
}

func (r pkiQueries) ListBindings(ctx context.Context, p authz.Proof, profileID string) ([]PKIProfileBinding, error) {
	if _, err := authz.Verify(p, authz.StorePKIBindingsList, r.tok); err != nil {
		return nil, err
	}

	return r.db.pkiStoreQueries().pkiListBindings(ctx, profileID)
}

// CreateBinding records an operator's explicit binding. The ids are the
// target the instance operator named (instance operations carry no tenant
// chain); the foreign keys refuse a project or environment that does not
// exist, and the unique index refuses a duplicate.
func (r pkiQueries) CreateBinding(ctx context.Context, p authz.Proof, m PKIProfileBinding) error {
	if _, err := authz.Verify(p, authz.StorePKIBindingsCreate, r.tok); err != nil {
		return err
	}

	return r.db.pkiStoreQueries().pkiCreateBinding(ctx, m)
}

func (r pkiQueries) DeleteBinding(ctx context.Context, p authz.Proof, profileID, bindingID string) (bool, error) {
	if _, err := authz.Verify(p, authz.StorePKIBindingsDelete, r.tok); err != nil {
		return false, err
	}

	return affectedOne(r.db.pkiStoreQueries().pkiDeleteBinding(ctx, bindingID, profileID))
}

// BoundProfile resolves a profile by name only if a binding reaches the
// proof's environment: the whole project, or exactly this environment. An
// unbound profile is indistinguishable from a missing one.
func (r pkiQueries) BoundProfile(ctx context.Context, p authz.Proof, name string) (PKIProfile, error) {
	chain, err := authz.Verify(p, authz.StorePKIProfilesBound, r.tok)
	if err != nil {
		return PKIProfile{}, err
	}

	return r.db.pkiStoreQueries().pkiBoundProfile(ctx, chain, name)
}

// BoundProfiles lists profiles bound to the proof's environment or whole
// project, ordered by name. Proof, database, and timestamp errors are returned.
func (r pkiQueries) BoundProfiles(ctx context.Context, p authz.Proof) ([]PKIProfile, error) {
	chain, err := authz.Verify(p, authz.StorePKIProfilesBoundList, r.tok)
	if err != nil {
		return nil, err
	}

	return r.db.pkiStoreQueries().pkiBoundProfiles(ctx, chain)
}

// --- Certificates -------------------------------------------------------------

func (r pkiQueries) GetCertificate(ctx context.Context, p authz.Proof, id string) (PKICertificate, error) {
	chain, err := authz.Verify(p, authz.StorePKICertificatesGet, r.tok)
	if err != nil {
		return PKICertificate{}, err
	}

	return r.db.pkiStoreQueries().pkiGetCertificate(ctx, chain, id)
}

// ListCertificates returns at most 500 rows in the proof's environment, ordered
// by creation time and ID descending. Proof, database, and timestamp errors
// are propagated.
func (r pkiQueries) ListCertificates(ctx context.Context, p authz.Proof) ([]PKICertificate, error) {
	chain, err := authz.Verify(p, authz.StorePKICertificatesList, r.tok)
	if err != nil {
		return nil, err
	}

	return r.db.pkiStoreQueries().pkiListCertificates(ctx, chain)
}

// IssuerPublic reads an issuer's public surface from an environment operation
// (the certificate response carries the issuing chain; the CRL route serves
// the stored CRL). It never returns key material.
func (r pkiQueries) IssuerPublic(ctx context.Context, p authz.Proof, id string) (PKIIssuer, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersPublic, r.tok); err != nil {
		return PKIIssuer{}, err
	}

	return r.db.pkiStoreQueries().pkiGetIssuer(ctx, id)
}

// ActiveIssuerPublic resolves the signing version of a named issuer.
func (r pkiQueries) ActiveIssuerPublic(ctx context.Context, p authz.Proof, name string) (PKIIssuer, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersActivePublic, r.tok); err != nil {
		return PKIIssuer{}, err
	}

	return r.db.pkiStoreQueries().pkiActiveIssuer(ctx, name)
}

// IssuerForSigning returns the issuer's public row plus its sealed key, for
// the leaf signer. The caller must still FenceIssuance before committing.
func (r pkiQueries) IssuerForSigning(ctx context.Context, p authz.Proof, id string) (PKIIssuer, []byte, uint32, error) {
	if _, err := authz.Verify(p, authz.StorePKIIssuersSigning, r.tok); err != nil {
		return PKIIssuer{}, nil, 0, err
	}

	issuer, err := r.db.pkiStoreQueries().pkiSigningIssuer(ctx, id)
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

	return affectedOne(r.db.pkiStoreQueries().pkiFenceIssuance(ctx, issuerID))
}

// CreateCertificate reserves an issuing row using the environment from the
// verified proof. It stores metadata and the deadline, without a certificate or
// private key, and propagates proof and database errors.
func (r pkiQueries) CreateCertificate(ctx context.Context, p authz.Proof, m PKICertificateCreate) error {
	chain, err := authz.Verify(p, authz.StorePKICertificatesCreate, r.tok)
	if err != nil {
		return err
	}

	return r.db.pkiStoreQueries().pkiCreateCertificate(ctx, chain, m)
}

// FinishCertificate stores the signed DER and marks an issuing row issued in
// the proof's environment. Missing rows or other states return false without
// error; proof and database failures return errors.
func (r pkiQueries) FinishCertificate(ctx context.Context, p authz.Proof, id string, der []byte, at time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StorePKICertificatesFinish, r.tok)
	if err != nil {
		return false, err
	}
	return affectedOne(r.db.pkiStoreQueries().pkiFinishCertificate(ctx, chain, id, der, at))
}

// FailCertificate marks an issuing row failed in the proof's environment.
// Missing rows or other states return false without error; proof and database
// failures return errors.
func (r pkiQueries) FailCertificate(ctx context.Context, p authz.Proof, id string, at time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StorePKICertificatesFail, r.tok)
	if err != nil {
		return false, err
	}
	return affectedOne(r.db.pkiStoreQueries().pkiFailCertificate(ctx, chain, id, at))
}

// ClaimRenewal is the single-winner renewal claim: only an issued certificate
// with no renewal in flight can be claimed, so two nodes renewing at once
// produce one successor.
func (r pkiQueries) ClaimRenewal(ctx context.Context, p authz.Proof, id, successorID string, at time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StorePKICertificatesClaimRenewal, r.tok)
	if err != nil {
		return false, err
	}
	return affectedOne(r.db.pkiStoreQueries().pkiClaimRenewal(ctx, chain, id, successorID, at))
}

// CompleteRenewal marks an issued predecessor renewed only when it names
// successorID in the proof's environment. It reports whether a row changed and
// propagates proof and database errors.
func (r pkiQueries) CompleteRenewal(ctx context.Context, p authz.Proof, id, successorID string, at time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StorePKICertificatesCompleteRenewal, r.tok)
	if err != nil {
		return false, err
	}
	return affectedOne(r.db.pkiStoreQueries().pkiCompleteRenewal(ctx, chain, id, successorID, at))
}

// ReleaseRenewal clears a matching successor claim in the proof's environment,
// regardless of certificate state. It reports whether a row changed and
// propagates proof and database errors.
func (r pkiQueries) ReleaseRenewal(ctx context.Context, p authz.Proof, id, successorID string, at time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StorePKICertificatesReleaseRenewal, r.tok)
	if err != nil {
		return false, err
	}
	return affectedOne(r.db.pkiStoreQueries().pkiReleaseRenewal(ctx, chain, id, successorID, at))
}

// RevokeCertificate marks an issued, renewed, or unknown row revoked in the
// proof's environment, recording the supplied reason and time. Missing rows or
// other states return false without error; proof and database errors propagate.
func (r pkiQueries) RevokeCertificate(ctx context.Context, p authz.Proof, id, reason string, at time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StorePKICertificatesRevoke, r.tok)
	if err != nil {
		return false, err
	}
	changed, err := affectedOne(r.db.pkiStoreQueries().pkiRevokeCertificate(ctx, chain, id, reason, at))
	if err == nil && changed {
		err = r.db.pkiStoreQueries().pkiBumpCertificateRevocation(ctx, id)
	}
	return changed, err
}
