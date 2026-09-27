package service

import (
	"context"
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	wencrypto "github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// PKI owns the private-PKI surface (#154, docs/adr/pki.md): instance CA
// issuers (sealed, non-exportable, versioned for overlap rotation), closed
// narrowing-only certificate profiles, and environment-scoped certificate
// issuance, renewal and revocation. CA private keys never cross this type's
// boundary: views carry certificates and fingerprints only.
type PKI struct {
	DB      *store.DB
	Auth    *Auth
	Keyring *wencrypto.Keyring
	Budget  *Budget
	Runtime *store.PKIRuntime
	// IssuingDeadline bounds the window between reserving a serial and
	// recording the signed leaf. A row still `issuing` past it becomes
	// `unknown` and is published on the CRL.
	IssuingDeadline time.Duration
	Now             func() time.Time
}

func (s *PKI) now() time.Time { return nowOr(s.Now) }

func (s *PKI) issuingDeadline() time.Duration {
	if s.IssuingDeadline > 0 {
		return s.IssuingDeadline
	}
	return 2 * time.Minute
}

var (
	ErrPKIIssuerNotFound = fmt.Errorf("%w: PKI issuer not found", domain.ErrNotFound)
	ErrPKIIssuerExists   = fmt.Errorf("%w: a PKI issuer with this name already exists; rotate it instead", domain.ErrConflict)
	ErrPKIIssuerState    = fmt.Errorf("%w: the PKI issuer version is not in the required lifecycle state", domain.ErrConflict)
	ErrPKIIssuerRace     = fmt.Errorf("%w: the PKI issuer changed underneath this write", domain.ErrConflict)
	ErrPKIIssuerLive     = fmt.Errorf("%w: the PKI issuer version still has unexpired certificates requiring CRL coverage; wait for them to expire", domain.ErrConflict)
	ErrPKIIssuerHeld     = fmt.Errorf("%w: the PKI issuer is held after a restore; release its hold (`hikyo pki issuer release-hold`) before issuing", domain.ErrConflict)
	ErrPKINoActiveIssuer = fmt.Errorf("%w: none of the profile's issuers has an active version", domain.ErrConflict)
	ErrPKIProfileExists  = fmt.Errorf("%w: a certificate profile with this name already exists", domain.ErrConflict)
	ErrPKIProfileRace    = fmt.Errorf("%w: the certificate profile changed underneath this write", domain.ErrConflict)
	ErrPKIBindingExists  = fmt.Errorf("%w: the profile is already bound to this scope", domain.ErrConflict)
	ErrPKIBindingTarget  = fmt.Errorf("%w: the organization, project or environment does not exist", domain.ErrInvalid)
	ErrPKIRenewWindow    = fmt.Errorf("%w: the certificate is not inside its profile's renewal window", domain.ErrConflict)
	ErrPKICertificateNot = fmt.Errorf("%w: the certificate is not in a state that allows this", domain.ErrConflict)
	ErrPKISigning        = fmt.Errorf("%w: the certificate was not issued; the issuer changed while signing", domain.ErrConflict)
)

func pkiIssuerKeyAAD(id string) wencrypto.InstanceFieldAAD {
	return wencrypto.InstanceFieldAAD{OwnerTable: "pki_issuers", OwnerRowID: id, FieldTag: "private_key"}
}

// pkiInvalid wraps a pki package refusal as a domain invalid-request error so
// the wire maps it to 400 with its reason.
func pkiInvalid(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrNotFound):
		return err
	}
	return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
}

// ---- Issuer views -------------------------------------------------------------

// PKIIssuerView is an issuer version's public surface. It has no private-key
// field: the key is sealed, non-exportable, and never leaves the service.
type PKIIssuerView struct {
	ID                 string
	Name               string
	Version            int64
	Kind               string
	Origin             string
	ParentID           string
	State              string
	KeyAlgorithm       string
	KeyFingerprint     string
	CertificatePEM     string
	CSRPEM             string
	ChainPEM           string
	SubjectCN          string
	SubjectOrg         string
	NotBefore          *time.Time
	NotAfter           *time.Time
	CRLDistributionURL string
	RestoreHold        bool
	IssuedCount        int64
	CRLNumber          int64
	CRLThisUpdate      *time.Time
	CRLNextUpdate      *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func pkiIssuerView(issuer store.PKIIssuer) PKIIssuerView {
	view := PKIIssuerView{
		ID: issuer.ID, Name: issuer.Name, Version: issuer.Version, Kind: issuer.Kind, Origin: issuer.Origin,
		ParentID: issuer.ParentID, State: issuer.State, KeyAlgorithm: issuer.KeyAlgorithm,
		KeyFingerprint: issuer.KeyFingerprint, ChainPEM: issuer.ChainPEM, SubjectCN: issuer.SubjectCN,
		SubjectOrg: issuer.SubjectOrg, NotBefore: optionalTime(issuer.NotBefore), NotAfter: optionalTime(issuer.NotAfter),
		CRLDistributionURL: issuer.CRLDistributionURL, RestoreHold: issuer.RestoreHold, IssuedCount: issuer.IssuedCount,
		CRLNumber: issuer.CRLNumber, CRLThisUpdate: optionalTime(issuer.CRLThisUpdate),
		CRLNextUpdate: optionalTime(issuer.CRLNextUpdate), CreatedAt: issuer.CreatedAt, UpdatedAt: issuer.UpdatedAt,
	}
	if len(issuer.CertificateDER) > 0 {
		view.CertificatePEM = pki.CertificatePEM(issuer.CertificateDER)
	}
	if len(issuer.CSRDER) > 0 {
		view.CSRPEM = pki.CSRPEM(issuer.CSRDER)
	}
	return view
}

// authorizeInstance is the cheap pre-pass before CPU-heavy key generation, so
// an unauthorized caller cannot use an issuer endpoint as a CPU oracle (the
// SAML SP key rotate precedent). The write transaction authorizes again.
func (s *PKI) authorizeInstance(ctx context.Context, actor Actor, op authz.Operation) error {
	return tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, s.now())
		if err != nil {
			return err
		}
		_, err = az.Authorize(ctx, caller, op, domain.Scope{})
		return err
	})
}

func recordPKIIssuerEvent(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, issuer store.PKIIssuer, action, state, priorFingerprint string, revoked int64) error {
	payload := audit.Payload{
		"action": action, "name": issuer.Name, "version": issuer.Version, "kind": issuer.Kind,
		"state": state, "key_fingerprint": issuer.KeyFingerprint,
	}
	if priorFingerprint != "" {
		payload["prior_key_fingerprint"] = priorFingerprint
	}
	if action == "revoke" {
		payload["certificates_revoked"] = revoked
	}
	event, err := domainEvent(ctx, audit.EventPKIIssuer, principal, audit.Object{Type: "pki-issuer", ID: issuer.ID}, payload)
	if err != nil {
		return err
	}
	return r.Audit().InsertInstance(ctx, proof, event)
}

func recordPKIInventoryRead(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, object, query string, rows int) error {
	event, err := domainEvent(ctx, audit.EventPKIInventoryRead, principal, audit.Object{Type: "pki-" + object},
		audit.Payload{"object": object, "query": query, "row_count": rows})
	if err != nil {
		return err
	}
	return r.Audit().InsertInstance(ctx, proof, event)
}

// ListIssuers returns every issuer version, ordered by name then version.
func (s *PKI) ListIssuers(ctx context.Context, actor Actor) ([]PKIIssuerView, error) {
	var out []PKIIssuerView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		out = nil
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerInspect, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		issuers, err := r.PKI().ListIssuers(ctx, proof)
		if err != nil {
			return err
		}
		for _, issuer := range issuers {
			out = append(out, pkiIssuerView(issuer))
		}
		return recordPKIInventoryRead(ctx, r, proof, caller.Principal, "issuer", "list", len(out))
	})
	return out, err
}

// ShowIssuer returns every version of one named issuer.
func (s *PKI) ShowIssuer(ctx context.Context, actor Actor, name string) ([]PKIIssuerView, error) {
	var out []PKIIssuerView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		out = nil
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerInspect, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		versions, err := issuerVersions(ctx, r, proof, name)
		if err != nil {
			return err
		}
		for _, issuer := range versions {
			out = append(out, pkiIssuerView(issuer))
		}
		return recordPKIInventoryRead(ctx, r, proof, caller.Principal, "issuer", "show", len(out))
	})
	return out, err
}

// IssuerCRL returns the stored DER CRL of one issuer version. A nonpositive
// version selects the newest version with a published CRL, regardless of
// lifecycle state. Missing issuers, versions, or CRLs return a not-found error.
// The read is authorized and audited and touches no key material.
func (s *PKI) IssuerCRL(ctx context.Context, actor Actor, name string, version int64) ([]byte, error) {
	var out []byte
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerInspect, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		versions, err := issuerVersions(ctx, r, proof, name)
		if err != nil {
			return err
		}
		issuer, err := pickVersion(versions, version, func(i store.PKIIssuer) bool { return len(i.CRLDER) > 0 })
		if err != nil {
			return err
		}
		if len(issuer.CRLDER) == 0 {
			return fmt.Errorf("%w: no CRL has been published for this issuer version yet", domain.ErrNotFound)
		}
		out = issuer.CRLDER
		return recordPKIInventoryRead(ctx, r, proof, caller.Principal, "crl", "show", 1)
	})
	return out, err
}

func issuerVersions(ctx context.Context, r store.Repos, proof authz.Proof, name string) ([]store.PKIIssuer, error) {
	all, err := r.PKI().ListIssuers(ctx, proof)
	if err != nil {
		return nil, err
	}
	var out []store.PKIIssuer
	for _, issuer := range all {
		if issuer.Name == name {
			out = append(out, issuer)
		}
	}
	if len(out) == 0 {
		return nil, ErrPKIIssuerNotFound
	}
	return out, nil
}

// pickVersion returns the named version, or with version zero the newest
// version that satisfies prefer (falling back to the newest).
func pickVersion(versions []store.PKIIssuer, version int64, prefer func(store.PKIIssuer) bool) (store.PKIIssuer, error) {
	if version > 0 {
		for _, issuer := range versions {
			if issuer.Version == version {
				return issuer, nil
			}
		}
		return store.PKIIssuer{}, ErrPKIIssuerNotFound
	}
	for i := len(versions) - 1; i >= 0; i-- {
		if prefer(versions[i]) {
			return versions[i], nil
		}
	}
	return versions[len(versions)-1], nil
}

func versionInState(versions []store.PKIIssuer, state string) (store.PKIIssuer, bool) {
	for _, issuer := range versions {
		if issuer.State == state {
			return issuer, true
		}
	}
	return store.PKIIssuer{}, false
}

// ---- Issuer creation and rotation ------------------------------------------------

// PKIIssuerRequest creates (or rotates to) an issuer version.
type PKIIssuerRequest struct {
	// Mode is root, intermediate or import. Rotation ignores it and follows
	// the issuer's own kind and origin.
	Mode               string
	Name               string
	CommonName         string
	Organization       string
	KeyAlgorithm       string
	TTL                time.Duration
	ParentName         string
	CRLDistributionURL string
	// Import material, protected input only. PrivateKeyPEM is zeroed by the
	// caller after the call returns.
	PrivateKeyPEM  []byte
	CertificatePEM string
	ChainPEM       string
}

// PKIIssuerResult is a created version. CSRPEM is set for a pending
// intermediate, for the operator's offline root to sign.
type PKIIssuerResult struct {
	Issuer PKIIssuerView
}

// pkiKeyMaterial is a generated or imported CA key, sealed once so a
// retried write transaction reuses the same ciphertext.
type pkiKeyMaterial struct {
	id          string
	key         crypto.Signer
	algorithm   pki.KeyAlgorithm
	fingerprint string
	sealed      []byte
	dekVersion  uint32
}

// sealIssuerKey seals a CA key under the instance DEK.
//
// fence:delegated: returns the sealed key and the instance DEK version it
// was sealed under; createIssuerVersion fences on that version
// (fenceInstanceVersion) inside the write transaction before the row lands.
func (s *PKI) sealIssuerKey(key crypto.Signer) (pkiKeyMaterial, error) {
	id, err := newID("pkii")
	if err != nil {
		return pkiKeyMaterial{}, err
	}
	algorithm, err := pki.AlgorithmOf(key.Public())
	if err != nil {
		return pkiKeyMaterial{}, pkiInvalid(err)
	}
	fingerprint, err := pki.KeyFingerprint(key.Public())
	if err != nil {
		return pkiKeyMaterial{}, err
	}
	pkcs8, err := pki.MarshalPrivateKey(key)
	if err != nil {
		return pkiKeyMaterial{}, err
	}
	defer wencrypto.Zero(pkcs8)
	sealer := s.Keyring.ForInstance()
	sealed, err := sealer.SealField(pkiIssuerKeyAAD(id), pkcs8)
	if err != nil {
		return pkiKeyMaterial{}, err
	}
	return pkiKeyMaterial{id: id, key: key, algorithm: algorithm, fingerprint: fingerprint, sealed: sealed, dekVersion: sealer.Version()}, nil
}

// openIssuerKey unseals a CA key for exactly one signing act. The PKCS#8
// buffer is zeroed before return; the caller owns the parsed key's lifetime
// (encryption-model ADR: best-effort memory hygiene, no enclave).
// Decryption and PKCS#8 parsing errors are returned to the caller.
func (s *PKI) openIssuerKey(id string, sealed []byte) (crypto.Signer, error) {
	pkcs8, err := s.Keyring.ForInstance().OpenField(pkiIssuerKeyAAD(id), sealed)
	if err != nil {
		return nil, err
	}
	defer wencrypto.Zero(pkcs8)
	return pki.UnmarshalPrivateKey(pkcs8)
}

// validateCRLURL trims an optional HTTP(S) distribution URL, rejecting a
// missing host, userinfo, or fragment with domain.ErrInvalid. It never fetches
// the URL; an empty value disables the distribution point.
func validateCRLURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("%w: crl_distribution_url must be an http(s) URL without userinfo or fragment", domain.ErrInvalid)
	}
	return u.String(), nil
}

// prepareIssuerKey generates or parses the CA key for a create or rotation,
// before any transaction (generation is CPU-heavy; the caller has already
// passed the cheap authorization pre-pass).
func (s *PKI) prepareIssuerKey(req PKIIssuerRequest, imported bool) (pkiKeyMaterial, error) {
	var key crypto.Signer
	var err error
	if imported {
		if len(req.PrivateKeyPEM) == 0 {
			return pkiKeyMaterial{}, fmt.Errorf("%w: an imported issuer needs its private key as protected input", domain.ErrInvalid)
		}
		key, err = pki.ParsePrivateKey(req.PrivateKeyPEM)
		if err != nil {
			return pkiKeyMaterial{}, pkiInvalid(err)
		}
		algorithm, err := pki.AlgorithmOf(key.Public())
		if err != nil {
			return pkiKeyMaterial{}, pkiInvalid(err)
		}
		if err := pki.CheckCAAlgorithm(algorithm); err != nil {
			return pkiKeyMaterial{}, pkiInvalid(err)
		}
	} else {
		algorithm := pki.ECDSAP256
		if req.KeyAlgorithm != "" {
			algorithm, err = pki.ParseKeyAlgorithm(req.KeyAlgorithm)
			if err != nil {
				return pkiKeyMaterial{}, pkiInvalid(err)
			}
		}
		if err := pki.CheckCAAlgorithm(algorithm); err != nil {
			return pkiKeyMaterial{}, pkiInvalid(err)
		}
		key, err = pki.GenerateKey(algorithm)
		if err != nil {
			return pkiKeyMaterial{}, err
		}
	}
	return s.sealIssuerKey(key)
}

// CreateIssuer creates version 1 of a new named issuer: an evaluation root, a
// Hikyo-signed intermediate (ParentName set), a pending intermediate awaiting
// an offline-signed certificate (no ParentName), or an imported CA.
func (s *PKI) CreateIssuer(ctx context.Context, actor Actor, req PKIIssuerRequest) (PKIIssuerResult, error) {
	if err := pki.ValidateName(req.Name); err != nil {
		return PKIIssuerResult{}, fmt.Errorf("%w: issuer name: %v", domain.ErrInvalid, err)
	}
	switch req.Mode {
	case "root", "intermediate", "import":
	default:
		return PKIIssuerResult{}, fmt.Errorf("%w: mode must be root, intermediate or import", domain.ErrInvalid)
	}
	crlURL, err := validateCRLURL(req.CRLDistributionURL)
	if err != nil {
		return PKIIssuerResult{}, err
	}
	req.CRLDistributionURL = crlURL
	if err := s.authorizeInstance(ctx, actor, authz.OpPKIIssuerCreate); err != nil {
		return PKIIssuerResult{}, err
	}
	material, err := s.prepareIssuerKey(req, req.Mode == "import")
	if err != nil {
		return PKIIssuerResult{}, err
	}
	var out PKIIssuerResult
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerCreate, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		if _, err := issuerVersions(ctx, r, proof, req.Name); err == nil {
			return ErrPKIIssuerExists
		} else if !errors.Is(err, ErrPKIIssuerNotFound) {
			return err
		}
		kind := req.Mode
		if kind == "import" {
			kind = ""
		}
		created, err := s.createIssuerVersion(ctx, r, proof, caller.Principal, req, material, kind, 1)
		if err != nil {
			return err
		}
		out = PKIIssuerResult{Issuer: pkiIssuerView(created)}
		return recordPKIIssuerEvent(ctx, r, proof, caller.Principal, created, "create", created.State, "", 0)
	})
	return out, err
}

// RotateIssuer creates version N+1 of a named issuer (ADR D3 overlap
// rotation). A root or Hikyo-signed intermediate activates at once and moves
// version N to `retiring` in the same transaction; an offline-signed
// intermediate becomes `pending` with a CSR, and version N stays active until
// the new certificate is installed; an imported issuer rotates to the newly
// imported material.
func (s *PKI) RotateIssuer(ctx context.Context, actor Actor, name string, req PKIIssuerRequest) (PKIIssuerResult, error) {
	crlURL, err := validateCRLURL(req.CRLDistributionURL)
	if err != nil {
		return PKIIssuerResult{}, err
	}
	req.CRLDistributionURL = crlURL
	if err := s.authorizeInstance(ctx, actor, authz.OpPKIIssuerRotate); err != nil {
		return PKIIssuerResult{}, err
	}
	// The key kind depends on the stored issuer's origin; read it before
	// generating (a cheap read under the same authorization).
	var latest store.PKIIssuer
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerRotate, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		versions, err := issuerVersions(ctx, r, proof, name)
		if err != nil {
			return err
		}
		latest = versions[len(versions)-1]
		return nil
	})
	if err != nil {
		return PKIIssuerResult{}, err
	}
	imported := latest.Origin == "imported"
	if !imported && req.KeyAlgorithm == "" {
		req.KeyAlgorithm = latest.KeyAlgorithm
	}
	material, err := s.prepareIssuerKey(req, imported)
	if err != nil {
		return PKIIssuerResult{}, err
	}
	var out PKIIssuerResult
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerRotate, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		versions, err := issuerVersions(ctx, r, proof, name)
		if err != nil {
			return err
		}
		current := versions[len(versions)-1]
		if current.ID != latest.ID {
			return ErrPKIIssuerRace
		}
		if _, pending := versionInState(versions, "pending"); pending {
			return fmt.Errorf("%w: a pending version is awaiting its certificate; install it or revoke it first", ErrPKIIssuerState)
		}
		spec := req
		spec.Name = current.Name
		if spec.CommonName == "" {
			spec.CommonName = current.SubjectCN
		}
		if spec.Organization == "" {
			spec.Organization = current.SubjectOrg
		}
		if spec.CRLDistributionURL == "" {
			spec.CRLDistributionURL = current.CRLDistributionURL
		}
		if spec.TTL == 0 && !current.NotAfter.IsZero() {
			spec.TTL = current.NotAfter.Sub(current.NotBefore).Round(24 * time.Hour)
		}
		kind := current.Kind
		if imported {
			kind = ""
		}
		if kind == "intermediate" && current.ParentID != "" {
			parent, err := r.PKI().GetIssuer(ctx, proof, current.ParentID)
			if err != nil {
				return err
			}
			spec.ParentName = parent.Name
		}
		created, err := s.createIssuerVersion(ctx, r, proof, caller.Principal, spec, material, kind, current.Version+1)
		if err != nil {
			return err
		}
		out = PKIIssuerResult{Issuer: pkiIssuerView(created)}
		prior := ""
		if active, ok := versionInState(versions, "active"); ok {
			prior = active.KeyFingerprint
		}
		return recordPKIIssuerEvent(ctx, r, proof, caller.Principal, created, "rotate", created.State, prior, 0)
	})
	return out, err
}

// createIssuerVersion builds and inserts one issuer version inside the write
// transaction. kind "" means imported material. When the new version is
// active, the name's current active version moves to `retiring` first (the
// partial unique index admits one active version per name).
func (s *PKI) createIssuerVersion(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, req PKIIssuerRequest, material pkiKeyMaterial, kind string, version int64) (store.PKIIssuer, error) {
	// Writer fence (invariant 7): refuse if rotate-dek --instance retired the
	// version the key was sealed under.
	if err := fenceInstanceVersion(ctx, r, proof, material.dekVersion); err != nil {
		return store.PKIIssuer{}, err
	}
	now := store.CanonTime(s.now())
	row := store.PKIIssuerCreate{
		ID: material.id, Name: req.Name, Version: version, Origin: "generated", State: "active",
		KeyAlgorithm: string(material.algorithm), KeyFingerprint: material.fingerprint,
		EncryptedPrivateKey: material.sealed, DEKVersion: material.dekVersion,
		CRLDistributionURL: req.CRLDistributionURL, CreatedBy: string(principal), At: now,
	}
	subject := pki.Subject{CommonName: req.CommonName, Organization: req.Organization}
	switch kind {
	case "root":
		ttl := req.TTL
		if ttl == 0 {
			ttl = 10 * 365 * 24 * time.Hour
		}
		der, err := pki.CreateRoot(material.key, subject, now, ttl)
		if err != nil {
			return store.PKIIssuer{}, pkiInvalid(err)
		}
		row.Kind, row.CertificateDER = "root", der
	case "intermediate":
		row.Kind = "intermediate"
		ttl := req.TTL
		if ttl == 0 {
			ttl = 365 * 24 * time.Hour
		}
		if req.ParentName == "" {
			csr, err := pki.CreateCSR(material.key, subject)
			if err != nil {
				return store.PKIIssuer{}, pkiInvalid(err)
			}
			row.State, row.CSRDER = "pending", csr
			break
		}
		parent, err := r.PKI().ActiveIssuerPublic(ctx, proof, req.ParentName)
		if errors.Is(err, domain.ErrNotFound) {
			return store.PKIIssuer{}, fmt.Errorf("%w: the parent issuer has no active version", ErrPKIIssuerState)
		}
		if err != nil {
			return store.PKIIssuer{}, err
		}
		if parent.RestoreHold {
			return store.PKIIssuer{}, ErrPKIIssuerHeld
		}
		signer, parentCert, err := s.issuerSigner(ctx, r, proof, parent)
		if err != nil {
			return store.PKIIssuer{}, err
		}
		der, err := pki.SignIntermediate(pki.Parent{Certificate: parentCert, Signer: signer}, material.key.Public(), subject, now, ttl)
		if err != nil {
			return store.PKIIssuer{}, pkiInvalid(err)
		}
		row.ParentID, row.CertificateDER = parent.ID, der
		row.ChainPEM = pki.CertificatePEM(parent.CertificateDER) + parent.ChainPEM
	case "":
		certs, ders, err := pki.ParseCertificates([]byte(req.CertificatePEM))
		if err != nil || len(certs) != 1 {
			return store.PKIIssuer{}, fmt.Errorf("%w: certificate_pem must hold exactly one certificate", domain.ErrInvalid)
		}
		chain, _, err := pki.ParseCertificates([]byte(req.ChainPEM))
		if err != nil {
			return store.PKIIssuer{}, pkiInvalid(err)
		}
		cert, err := pki.VerifyCA(ders[0], material.key.Public(), chain, now)
		if err != nil {
			return store.PKIIssuer{}, pkiInvalid(err)
		}
		row.Origin, row.CertificateDER, row.ChainPEM = "imported", ders[0], req.ChainPEM
		row.Kind = "intermediate"
		if cert.CheckSignatureFrom(cert) == nil && len(chain) == 0 {
			row.Kind = "root"
		}
	default:
		return store.PKIIssuer{}, fmt.Errorf("%w: unknown issuer kind %q", domain.ErrInvalid, kind)
	}
	if len(row.CertificateDER) > 0 {
		cert, err := x509.ParseCertificate(row.CertificateDER)
		if err != nil {
			return store.PKIIssuer{}, err
		}
		row.NotBefore, row.NotAfter = cert.NotBefore, cert.NotAfter
		row.SubjectCN = cert.Subject.CommonName
		if len(cert.Subject.Organization) > 0 {
			row.SubjectOrg = cert.Subject.Organization[0]
		}
	} else {
		row.SubjectCN, row.SubjectOrg = strings.TrimSpace(req.CommonName), strings.TrimSpace(req.Organization)
	}
	if row.State == "active" {
		if err := s.demoteActive(ctx, r, proof, req.Name, now); err != nil {
			return store.PKIIssuer{}, err
		}
	}
	if err := r.PKI().CreateIssuer(ctx, proof, row); err != nil {
		return store.PKIIssuer{}, err
	}
	return r.PKI().GetIssuer(ctx, proof, row.ID)
}

// demoteActive moves the name's current active version (if any) to
// `retiring` under a row-version CAS.
func (s *PKI) demoteActive(ctx context.Context, r store.Repos, proof authz.Proof, name string, now time.Time) error {
	all, err := r.PKI().ListIssuers(ctx, proof)
	if err != nil {
		return err
	}
	for _, issuer := range all {
		if issuer.Name != name || issuer.State != "active" {
			continue
		}
		moved, err := r.PKI().TransitionIssuer(ctx, proof, issuer.ID, "active", "retiring", issuer.RowVersion, now)
		if err != nil {
			return err
		}
		if !moved {
			return ErrPKIIssuerRace
		}
	}
	return nil
}

// issuerSigner opens an issuer version's key for one signing act.
func (s *PKI) issuerSigner(ctx context.Context, r store.Repos, proof authz.Proof, issuer store.PKIIssuer) (crypto.Signer, *x509.Certificate, error) {
	sealed, _, err := r.PKI().IssuerKey(ctx, proof, issuer.ID)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(issuer.CertificateDER)
	if err != nil {
		return nil, nil, err
	}
	signer, err := s.openIssuerKey(issuer.ID, sealed)
	return signer, cert, err
}

// InstallIssuerCertificate activates a pending version with the certificate
// an offline root signed for its CSR. The certificate must carry the pending
// key, be a CA with certificate and CRL signing, and verify against the
// supplied chain (the offline root last).
func (s *PKI) InstallIssuerCertificate(ctx context.Context, actor Actor, name, certificatePEM, chainPEM string) (PKIIssuerView, error) {
	var out PKIIssuerView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerInstall, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		versions, err := issuerVersions(ctx, r, proof, name)
		if err != nil {
			return err
		}
		pending, ok := versionInState(versions, "pending")
		if !ok {
			return fmt.Errorf("%w: no pending version awaits a certificate", ErrPKIIssuerState)
		}
		csr, err := x509.ParseCertificateRequest(pending.CSRDER)
		if err != nil {
			return err
		}
		certs, ders, err := pki.ParseCertificates([]byte(certificatePEM))
		if err != nil || len(certs) != 1 {
			return fmt.Errorf("%w: certificate_pem must hold exactly one certificate", domain.ErrInvalid)
		}
		chain, _, err := pki.ParseCertificates([]byte(chainPEM))
		if err != nil {
			return pkiInvalid(err)
		}
		if len(chain) == 0 {
			return fmt.Errorf("%w: chain_pem must hold the signing chain, ending with the offline root", domain.ErrInvalid)
		}
		now := store.CanonTime(s.now())
		cert, err := pki.VerifyCA(ders[0], csr.PublicKey, chain, now)
		if err != nil {
			return pkiInvalid(err)
		}
		prior := ""
		if active, ok := versionInState(versions, "active"); ok {
			prior = active.KeyFingerprint
		}
		if err := s.demoteActive(ctx, r, proof, name, now); err != nil {
			return err
		}
		installed, err := r.PKI().InstallIssuerCertificate(ctx, proof, store.PKIIssuerInstall{
			ID: pending.ID, RowVersion: pending.RowVersion, CertificateDER: ders[0], ChainPEM: chainPEM,
			NotBefore: cert.NotBefore, NotAfter: cert.NotAfter, At: now,
		})
		if err != nil {
			return err
		}
		if !installed {
			return ErrPKIIssuerRace
		}
		updated, err := r.PKI().GetIssuer(ctx, proof, pending.ID)
		if err != nil {
			return err
		}
		out = pkiIssuerView(updated)
		return recordPKIIssuerEvent(ctx, r, proof, caller.Principal, updated, "install", "active", prior, 0)
	})
	return out, err
}

// RetireIssuer ends an overlap: the version stops signing CRLs and its key is
// destroyed. It refuses while unexpired leaves or child CAs need CRL coverage.
func (s *PKI) RetireIssuer(ctx context.Context, actor Actor, name string, version int64) (PKIIssuerView, error) {
	var out PKIIssuerView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerRetire, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		versions, err := issuerVersions(ctx, r, proof, name)
		if err != nil {
			return err
		}
		target, err := pickVersion(versions, version, func(i store.PKIIssuer) bool { return i.State == "retiring" })
		if err != nil {
			return err
		}
		if target.State != "active" && target.State != "retiring" {
			return ErrPKIIssuerState
		}
		now := store.CanonTime(s.now())
		live, err := r.PKI().CountLiveCertificates(ctx, proof, target.ID, now)
		if err != nil {
			return err
		}
		if live > 0 {
			return ErrPKIIssuerLive
		}
		moved, err := r.PKI().DestroyIssuerKey(ctx, proof, target.ID, target.State, "retired", target.RowVersion, now)
		if err != nil {
			return err
		}
		if !moved {
			return ErrPKIIssuerRace
		}
		updated, err := r.PKI().GetIssuer(ctx, proof, target.ID)
		if err != nil {
			return err
		}
		out = pkiIssuerView(updated)
		return recordPKIIssuerEvent(ctx, r, proof, caller.Principal, updated, "retire", "retired", "", 0)
	})
	return out, err
}

// RevokeIssuer is the compromise response (ADR D3): terminal, key destroyed,
// every live leaf of the version revoked with reason ca-compromise.
func (s *PKI) RevokeIssuer(ctx context.Context, actor Actor, name string, version int64) (PKIIssuerView, error) {
	if version <= 0 {
		return PKIIssuerView{}, fmt.Errorf("%w: issuer revoke names the exact version", domain.ErrInvalid)
	}
	var out PKIIssuerView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerRevoke, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		versions, err := issuerVersions(ctx, r, proof, name)
		if err != nil {
			return err
		}
		target, err := pickVersion(versions, version, func(store.PKIIssuer) bool { return true })
		if err != nil {
			return err
		}
		if !slices.Contains([]string{"pending", "active", "retiring"}, target.State) {
			return ErrPKIIssuerState
		}
		now := store.CanonTime(s.now())
		revoked, err := r.PKI().RevokeLiveCertificates(ctx, proof, target.ID, string(pki.ReasonCACompromise), now)
		if err != nil {
			return err
		}
		moved, err := r.PKI().DestroyIssuerKey(ctx, proof, target.ID, target.State, "revoked", target.RowVersion, now)
		if err != nil {
			return err
		}
		if !moved {
			return ErrPKIIssuerRace
		}
		updated, err := r.PKI().GetIssuer(ctx, proof, target.ID)
		if err != nil {
			return err
		}
		out = pkiIssuerView(updated)
		return recordPKIIssuerEvent(ctx, r, proof, caller.Principal, updated, "revoke", "revoked", "", revoked)
	})
	return out, err
}

// ReleaseIssuerHold lifts the restore hold on every version of a named issuer,
// after the operator has re-applied revocations made since the backup.
func (s *PKI) ReleaseIssuerHold(ctx context.Context, actor Actor, name string) ([]PKIIssuerView, error) {
	var out []PKIIssuerView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		out = nil
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerReleaseHold, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		versions, err := issuerVersions(ctx, r, proof, name)
		if err != nil {
			return err
		}
		now := store.CanonTime(s.now())
		cleared := 0
		for _, issuer := range versions {
			if issuer.RestoreHold {
				moved, err := r.PKI().SetIssuerHold(ctx, proof, issuer.ID, false, issuer.RowVersion, now)
				if err != nil {
					return err
				}
				if !moved {
					return ErrPKIIssuerRace
				}
				updated, err := r.PKI().GetIssuer(ctx, proof, issuer.ID)
				if err != nil {
					return err
				}
				if err := recordPKIIssuerEvent(ctx, r, proof, caller.Principal, updated, "release-hold", updated.State, "", 0); err != nil {
					return err
				}
				issuer = updated
				cleared++
			}
			out = append(out, pkiIssuerView(issuer))
		}
		if cleared == 0 {
			return fmt.Errorf("%w: no version of this issuer is held", ErrPKIIssuerState)
		}
		return nil
	})
	return out, err
}

// PublishIssuerCRL signs and stores a fresh CRL for one version now, instead
// of waiting for the worker (after revocations an operator wants out at once).
func (s *PKI) PublishIssuerCRL(ctx context.Context, actor Actor, name string, version int64) (PKIIssuerView, error) {
	var out PKIIssuerView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIIssuerPublishCRL, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		versions, err := issuerVersions(ctx, r, proof, name)
		if err != nil {
			return err
		}
		target, err := pickVersion(versions, version, func(i store.PKIIssuer) bool { return i.State == "active" })
		if err != nil {
			return err
		}
		if target.State != "active" && target.State != "retiring" {
			return ErrPKIIssuerState
		}
		now := store.CanonTime(s.now())
		entries, err := r.PKI().RevokedEntries(ctx, proof, target.ID, now)
		if err != nil {
			return err
		}
		signer, cert, err := s.issuerSigner(ctx, r, proof, target)
		if err != nil {
			return err
		}
		number := pki.NextCRLNumber(target.CRLNumber, now)
		der, err := signCRL(pki.Parent{Certificate: cert, Signer: signer}, entries, number, now)
		if err != nil {
			return err
		}
		published, err := r.PKI().PublishCRL(ctx, proof, target.ID, der, target.CRLNumber, number, target.RevocationSeq, now, now.Add(pki.CRLValidity))
		if err != nil {
			return err
		}
		if !published {
			return ErrPKIIssuerRace
		}
		event, err := domainEvent(ctx, audit.EventPKICRLPublished, caller.Principal, audit.Object{Type: "pki-issuer", ID: target.ID},
			audit.Payload{"issuer": target.Name, "version": target.Version, "crl_number": number, "entries": len(entries)})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertInstance(ctx, proof, event); err != nil {
			return err
		}
		updated, err := r.PKI().GetIssuer(ctx, proof, target.ID)
		out = pkiIssuerView(updated)
		return err
	})
	return out, err
}

func signCRL(parent pki.Parent, entries []store.PKIRevokedEntry, number int64, now time.Time) ([]byte, error) {
	list := make([]pki.RevokedEntry, 0, len(entries))
	for _, entry := range entries {
		serial, err := pki.ParseSerialHex(entry.Serial)
		if err != nil {
			return nil, err
		}
		list = append(list, pki.RevokedEntry{Serial: serial, RevokedAt: entry.RevokedAt, Reason: pki.RevocationReason(entry.Reason)})
	}
	return pki.CreateCRL(parent, list, pki.CRLNumber(number), now)
}
