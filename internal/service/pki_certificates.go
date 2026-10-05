package service

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	wencrypto "github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// CertificateIssueRequest is one leaf request against a bound profile. Exactly
// one of CSRPEM or GenerateKey is set.
type CertificateIssueRequest struct {
	Profile      string
	Issuer       string
	CSRPEM       string
	GenerateKey  bool
	KeyAlgorithm string
	CommonName   string
	DNSNames     []string
	IPAddresses  []string
	URIs         []string
	TTL          time.Duration
}

// CertificateView is an issuance record's public surface: metadata and the
// public certificate. There is no private-key field.
type CertificateView struct {
	ID               string
	EnvironmentID    string
	ProfileName      string
	IssuerID         string
	IssuerName       string
	IssuerVersion    int64
	Serial           string
	State            string
	KeySource        string
	KeyAlgorithm     string
	KeyFingerprint   string
	CommonName       string
	DNSNames         []string
	IPAddresses      []string
	URIs             []string
	NotBefore        time.Time
	NotAfter         time.Time
	CertificatePEM   string
	ChainPEM         string
	PrincipalID      string
	PrincipalClass   string
	RenewedFrom      string
	RenewedBy        string
	RevokedAt        *time.Time
	RevocationReason string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CertificateIssueResult carries the issued certificate and, for a
// server-generated key only, the private key: display-once, never stored,
// and the caller's to zero after writing it out.
type CertificateIssueResult struct {
	Certificate   CertificateView
	PrivateKeyPEM []byte
}

type certificateSANs struct {
	DNS []string `json:"dns"`
	IP  []string `json:"ip"`
	URI []string `json:"uri"`
}

func encodeSANs(resolved pki.Resolved) (string, error) {
	sans := certificateSANs{DNS: nonNil(resolved.DNSNames), IP: []string{}, URI: []string{}}
	for _, ip := range resolved.IPAddresses {
		sans.IP = append(sans.IP, ip.String())
	}
	for _, uri := range resolved.URIs {
		sans.URI = append(sans.URI, uri.String())
	}
	body, err := json.Marshal(sans)
	return string(body), err
}

func decodeCertificateSANs(cert store.PKICertificate) (certificateSANs, error) {
	var sans certificateSANs
	if err := json.Unmarshal([]byte(cert.SANs), &sans); err != nil {
		return certificateSANs{}, fmt.Errorf("service: certificate %s SAN metadata: %w", cert.ID, err)
	}
	return sans, nil
}

// pkiCertificatePublicView recovers corrupt redundant SAN metadata only from
// the signed leaf bound to this record's issuer, serial and public key. Reads
// and fail-safe revocation can still return accurate names; issuance/renewal
// use the strict metadata decoder so recovery cannot hide settlement errors.
func pkiCertificatePublicView(cert store.PKICertificate, issuer store.PKIIssuer) (CertificateView, error) {
	sans, metadataErr := decodeCertificateSANs(cert)
	if metadataErr == nil {
		return certificateViewWithSANs(cert, issuer, sans), nil
	}
	leaf, err := x509.ParseCertificate(cert.CertificateDER)
	if err != nil {
		return CertificateView{}, fmt.Errorf("%w; signed SAN recovery: %v", metadataErr, err)
	}
	parent, err := x509.ParseCertificate(issuer.CertificateDER)
	if err != nil {
		return CertificateView{}, fmt.Errorf("%w; SAN recovery issuer: %v", metadataErr, err)
	}
	if err := leaf.CheckSignatureFrom(parent); err != nil {
		return CertificateView{}, fmt.Errorf("%w; SAN recovery signature: %v", metadataErr, err)
	}
	serial, err := pki.ParseSerialHex(cert.Serial)
	if err != nil || leaf.SerialNumber.Cmp(serial) != 0 {
		return CertificateView{}, fmt.Errorf("%w; SAN recovery serial mismatch", metadataErr)
	}
	fingerprint, err := pki.KeyFingerprint(leaf.PublicKey)
	if err != nil || fingerprint != cert.KeyFingerprint {
		return CertificateView{}, fmt.Errorf("%w; SAN recovery public key mismatch", metadataErr)
	}
	sans = certificateSANs{DNS: leaf.DNSNames}
	for _, ip := range leaf.IPAddresses {
		sans.IP = append(sans.IP, ip.String())
	}
	for _, uri := range leaf.URIs {
		sans.URI = append(sans.URI, uri.String())
	}
	return certificateViewWithSANs(cert, issuer, sans), nil
}

func pkiCertificateView(cert store.PKICertificate, issuer store.PKIIssuer) (CertificateView, error) {
	sans, err := decodeCertificateSANs(cert)
	if err != nil {
		return CertificateView{}, err
	}
	return certificateViewWithSANs(cert, issuer, sans), nil
}

func certificateViewWithSANs(cert store.PKICertificate, issuer store.PKIIssuer, sans certificateSANs) CertificateView {
	view := CertificateView{
		ID: cert.ID, EnvironmentID: cert.EnvironmentID, ProfileName: cert.ProfileName, IssuerID: cert.IssuerID,
		IssuerName: issuer.Name, IssuerVersion: issuer.Version, Serial: cert.Serial, State: cert.State,
		KeySource: cert.KeySource, KeyAlgorithm: cert.KeyAlgorithm, KeyFingerprint: cert.KeyFingerprint,
		CommonName: cert.CommonName, DNSNames: nonNil(sans.DNS), IPAddresses: nonNil(sans.IP), URIs: nonNil(sans.URI),
		NotBefore: cert.NotBefore, NotAfter: cert.NotAfter, PrincipalID: cert.PrincipalID,
		PrincipalClass: cert.PrincipalClass, RenewedFrom: cert.RenewedFrom, RenewedBy: cert.RenewedBy,
		RevokedAt: optionalTime(cert.RevokedAt), RevocationReason: cert.RevocationReason,
		CreatedAt: cert.CreatedAt, UpdatedAt: cert.UpdatedAt,
	}
	if len(cert.CertificateDER) > 0 {
		view.CertificatePEM = pki.CertificatePEM(cert.CertificateDER)
	}
	if len(issuer.CertificateDER) > 0 {
		view.ChainPEM = pki.CertificatePEM(issuer.CertificateDER) + issuer.ChainPEM
	}
	return view
}

// pkiCallerGate is the per-caller-class issuance condition beyond the op
// formula (ADR D4): a machine needs the profile's machine_issuance opt-in (a
// withdrawn opt-in answers the uniform not-found response); a human issuing
// with a server-generated key consumes the env-bound mint reauthentication
// ceremony when consumeCeremony is set (the dynamic lease mint precedent).
func (s *PKI) pkiCallerGate(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope, policy pki.Policy, generated, consumeCeremony bool, now time.Time) error {
	if domain.IsServiceAccountKind(caller.Class) {
		if !policy.MachineIssuance {
			return domain.ErrNotFound
		}
		return nil
	}
	if !generated || !consumeCeremony || skipsCeremony(caller) {
		return nil
	}
	if s.Auth == nil {
		return errors.New("service: PKI has no reauthentication seam wired")
	}
	intent, err := NewMintReauthIntent(string(scope.Env), nil)
	if err != nil {
		return err
	}
	if err := s.Auth.ConsumeReauthWindow(ctx, az, caller.SessionID, intent, now); err != nil {
		switch {
		case errors.Is(err, ErrNoReauthWindow), errors.Is(err, ErrReauthWindowExpired),
			errors.Is(err, ErrReauthUnitMismatch), errors.Is(err, ErrReauthWindowSpent):
			return fmt.Errorf("%w (certificate key generation)", ErrReauthRequired)
		default:
			return err
		}
	}
	return nil
}

// selectIssuer picks the signing version: the requested issuer (which the
// profile must allow) or the first allowed issuer with an active version.
// A restore hold on that version returns ErrPKIIssuerHeld without trying
// later candidates. No active candidate returns ErrPKINoActiveIssuer;
// lookup errors other than not-found are propagated.
func selectIssuer(ctx context.Context, r store.Repos, proof authz.Proof, policy pki.Policy, requested string) (store.PKIIssuer, error) {
	candidates := policy.AllowedIssuers
	if requested != "" {
		if !slices.Contains(policy.AllowedIssuers, requested) {
			return store.PKIIssuer{}, fmt.Errorf("%w: issuer %q is not allowed by the profile", domain.ErrInvalid, requested)
		}
		candidates = []string{requested}
	}
	for _, name := range candidates {
		issuer, err := r.PKI().ActiveIssuerPublic(ctx, proof, name)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return store.PKIIssuer{}, err
		}
		if issuer.RestoreHold {
			return store.PKIIssuer{}, ErrPKIIssuerHeld
		}
		return issuer, nil
	}
	return store.PKIIssuer{}, ErrPKINoActiveIssuer
}

// leafPlan is everything decided in the INTENT transaction and needed to sign
// and settle outside it.
type leafPlan struct {
	certID         string
	serial         string
	issuer         store.PKIIssuer
	sealedKey      []byte
	resolved       pki.Resolved
	policy         pki.Policy
	notBefore      time.Time
	notAfter       time.Time
	publicKey      crypto.PublicKey
	principal      domain.PrincipalID
	class          string
	profileID      string
	profileName    string
	profileVersion int64
	renewedFrom    string
}

func (s *PKI) reserveLeaf(ctx context.Context, r store.Repos, proof authz.Proof, caller authz.Identity, profile store.PKIProfile, policy pki.Policy, resolved pki.Resolved, publicKey crypto.PublicKey, requestedIssuer, keySource, renewedFrom, kind string, now time.Time) (leafPlan, error) {
	issuer, err := selectIssuer(ctx, r, proof, policy, requestedIssuer)
	if err != nil {
		return leafPlan{}, err
	}
	issuerCert, err := x509.ParseCertificate(issuer.CertificateDER)
	if err != nil {
		return leafPlan{}, err
	}
	notBefore, notAfter, err := pki.LeafWindow(issuerCert, now, resolved.TTL)
	if err != nil {
		return leafPlan{}, pkiInvalid(err)
	}
	signingIssuer, sealed, _, err := r.PKI().IssuerForSigning(ctx, proof, issuer.ID)
	if err != nil {
		return leafPlan{}, err
	}
	serial, err := pki.NewSerial()
	if err != nil {
		return leafPlan{}, err
	}
	fingerprint, err := pki.KeyFingerprint(publicKey)
	if err != nil {
		return leafPlan{}, err
	}
	sans, err := encodeSANs(resolved)
	if err != nil {
		return leafPlan{}, err
	}
	certID, err := newID("cert")
	if err != nil {
		return leafPlan{}, err
	}
	plan := leafPlan{
		certID: certID, serial: pki.SerialHex(serial), issuer: signingIssuer, sealedKey: sealed,
		resolved: resolved, policy: policy, notBefore: notBefore, notAfter: notAfter, publicKey: publicKey,
		principal: caller.Principal, class: string(caller.Class), profileID: profile.ID,
		profileName: profile.Name, profileVersion: profile.RowVersion, renewedFrom: renewedFrom,
	}
	if err := r.PKI().CreateCertificate(ctx, proof, store.PKICertificateCreate{
		ID: certID, ProfileID: profile.ID, ProfileName: profile.Name, IssuerID: issuer.ID, Serial: plan.serial,
		KeySource: keySource, KeyAlgorithm: string(resolved.KeyAlgorithm), KeyFingerprint: fingerprint,
		CommonName: resolved.CommonName, SANs: sans, NotBefore: notBefore, NotAfter: notAfter,
		PrincipalID: string(caller.Principal), PrincipalClass: plan.class, RenewedFrom: renewedFrom,
		IssuingDeadline: now.Add(s.issuingDeadline()), At: now,
	}); err != nil {
		return leafPlan{}, err
	}
	intent, err := newAuditEvent(ctx, audit.EventPKICertificateTransitionIntent, caller.Principal,
		audit.Object{Type: "pki-certificate", ID: certID}, audit.OutcomeIntent, "",
		audit.Payload{"kind": kind, "serial": plan.serial, "issuer": issuer.Name, "profile": profile.Name})
	if err != nil {
		return leafPlan{}, err
	}
	return plan, r.Audit().InsertTenant(ctx, proof, intent)
}

// signLeaf signs the planned leaf outside any transaction. The issuer key is
// unsealed for this one signature.
func (s *PKI) signLeaf(plan leafPlan) ([]byte, error) {
	issuerCert, err := x509.ParseCertificate(plan.issuer.CertificateDER)
	if err != nil {
		return nil, err
	}
	signer, err := s.openIssuerKey(plan.issuer.ID, plan.sealedKey)
	if err != nil {
		return nil, err
	}
	serial, err := pki.ParseSerialHex(plan.serial)
	if err != nil {
		return nil, err
	}
	var crlPoints []string
	if plan.issuer.CRLDistributionURL != "" {
		crlPoints = []string{plan.issuer.CRLDistributionURL}
	}
	return pki.SignLeaf(pki.Parent{Certificate: issuerCert, Signer: signer}, plan.resolved, pki.Leaf{
		Serial: serial, PublicKey: plan.publicKey, NotBefore: plan.notBefore, NotAfter: plan.notAfter,
		Organization: plan.policy.Organization, KeyUsages: plan.policy.KeyUsages, ExtKeyUsages: plan.policy.ExtKeyUsages,
		CRLDistributionPoints: crlPoints,
	})
}

func recordCertificateOutcome(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, certID, kind, serial, issuer, state, reason string, outcome audit.Outcome) error {
	payload := audit.Payload{"kind": kind, "serial": serial, "issuer": issuer, "state": state}
	if reason != "" {
		payload["reason"] = reason
	}
	event, err := newAuditEvent(ctx, audit.EventPKICertificateTransitionOutcome, principal,
		audit.Object{Type: "pki-certificate", ID: certID}, outcome, "", payload)
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, proof, event)
}

// settleLeaf is the OUTCOME transaction for issue and renew: re-authorize,
// re-apply the caller-class gate (without a second ceremony), fence on the
// issuer row, then record the signed leaf. A nil DER, failed binding lookup
// or caller gate, or lost issuer fence records `failed`. Authorization and
// transaction errors are returned and can leave the row `issuing` for the
// worker to mark `unknown`. The boolean reports issuance only when err is nil.
func (s *PKI) settleLeaf(ctx context.Context, actor Actor, op authz.Operation, scope domain.Scope, plan leafPlan, der []byte, kind string, generated bool, onSuccess, onFailure func(context.Context, store.Repos, authz.Proof) error) (CertificateView, bool, error) {
	var settled CertificateView
	var issued bool
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := store.CanonTime(s.now())
		caller, proof, err := authorize(ctx, az, actor, op, scope, now)
		if err != nil {
			return err
		}
		issued = der != nil
		if issued {
			currentProfile, err := r.PKI().BoundProfile(ctx, proof, plan.profileName)
			if err != nil || currentProfile.ID != plan.profileID || currentProfile.RowVersion != plan.profileVersion {
				issued = false
			} else if err := s.pkiCallerGate(ctx, az, caller, scope, plan.policy, generated, false, now); err != nil {
				issued = false
			}
		}
		if issued {
			if issued, err = r.PKI().FenceIssuance(ctx, proof, plan.issuer.ID); err != nil {
				return err
			}
		}
		state, outcome := "issued", audit.OutcomeSuccess
		if issued {
			finished, err := r.PKI().FinishCertificate(ctx, proof, plan.certID, der, now)
			if err != nil {
				return err
			}
			if !finished {
				return ErrPKISigning
			}
			if onSuccess != nil {
				if err := onSuccess(ctx, r, proof); err != nil {
					return err
				}
			}
		} else {
			state, outcome = "failed", audit.OutcomeFailure
			if _, err := r.PKI().FailCertificate(ctx, proof, plan.certID, now); err != nil {
				return err
			}
			if onFailure != nil {
				if err := onFailure(ctx, r, proof); err != nil {
					return err
				}
			}
		}
		if err := recordCertificateOutcome(ctx, r, proof, caller.Principal, plan.certID, kind, plan.serial, plan.issuer.Name, state, "", outcome); err != nil {
			return err
		}
		if issued && generated {
			event, err := domainEvent(ctx, audit.EventPKICertificateKeyDisclosed, caller.Principal,
				audit.Object{Type: "pki-certificate", ID: plan.certID},
				audit.Payload{"serial": plan.serial, "key_algorithm": string(plan.resolved.KeyAlgorithm), "principal_class": plan.class})
			if err != nil {
				return err
			}
			if err := r.Audit().InsertTenant(ctx, proof, event); err != nil {
				return err
			}
		}
		// Read the settled row in this SAME transaction, so the response
		// never depends on a fallible post-commit read that could lose a
		// display-once key after the certificate is already live.
		cert, err := r.PKI().GetCertificate(ctx, proof, plan.certID)
		if err != nil {
			return err
		}
		// Decode before commit: corrupt metadata must not turn a live
		// display-once issuance into an error that discards its private key.
		settled, err = pkiCertificateView(cert, plan.issuer)
		return err
	})
	return settled, issued, err
}

// authorizeTenant is the cheap pre-pass before key generation.
func (s *PKI) authorizeTenant(ctx context.Context, actor Actor, op authz.Operation, scope domain.Scope) error {
	return tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		_, _, err := authorize(ctx, az, actor, op, scope, s.now())
		return err
	})
}

func envScopeRequired(scope domain.Scope, what string) error {
	if scope.Org == "" || scope.Project == "" || scope.Env == "" {
		return fmt.Errorf("%w: %s requires an environment scope", domain.ErrInvalid, what)
	}
	return nil
}

// IssueCertificate issues one leaf in an environment through a bound profile
// (ADR D6), using exactly one of a CSR or a generated key. Names come from req,
// not the CSR. A zero TTL uses the profile default. A generated private key
// is returned only on success, never stored, and must be zeroed by the caller.
// Validation, authorization, budget, storage, and signing errors reach the
// caller. Errors after reservation can leave a failed or unresolved issuance
// record; an error does not imply that no record was created.
func (s *PKI) IssueCertificate(ctx context.Context, actor Actor, scope domain.Scope, req CertificateIssueRequest) (CertificateIssueResult, error) {
	if err := envScopeRequired(scope, "certificate issue"); err != nil {
		return CertificateIssueResult{}, err
	}
	if req.Profile == "" {
		return CertificateIssueResult{}, fmt.Errorf("%w: certificate issue names a profile", domain.ErrInvalid)
	}
	if req.GenerateKey == (req.CSRPEM != "") {
		return CertificateIssueResult{}, fmt.Errorf("%w: supply exactly one of csr_pem or generate_key", domain.ErrInvalid)
	}
	release, err := chargeDefaultAtEntry(ctx, s.DB, s.Budget, actor, authz.OpCertificateIssue, authz.OpCertificateIssue, scope, s.now)
	if err != nil {
		return CertificateIssueResult{}, err
	}
	defer release()
	var publicKey crypto.PublicKey
	if req.CSRPEM != "" {
		csr, err := pki.ParseCSR([]byte(req.CSRPEM))
		if err != nil {
			return CertificateIssueResult{}, pkiInvalid(err)
		}
		publicKey = csr.PublicKey
	}
	var generatedKey crypto.Signer
	if req.GenerateKey {
		algorithm := pki.ECDSAP256
		if req.KeyAlgorithm != "" {
			if algorithm, err = pki.ParseKeyAlgorithm(req.KeyAlgorithm); err != nil {
				return CertificateIssueResult{}, pkiInvalid(err)
			}
		}
		if err := s.authorizeTenant(ctx, actor, authz.OpCertificateIssue, scope); err != nil {
			return CertificateIssueResult{}, err
		}
		if generatedKey, err = pki.GenerateKey(algorithm); err != nil {
			return CertificateIssueResult{}, err
		}
		publicKey = generatedKey.Public()
	}
	keySource := "csr"
	if req.GenerateKey {
		keySource = "generated"
	}

	// INTENT: authorize, gate, validate against the profile, reserve the
	// serial in an `issuing` row, record the intent, and read the SEALED
	// issuer key (opened after commit, for one signature).
	var plan leafPlan
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := store.CanonTime(s.now())
		caller, proof, err := authorize(ctx, az, actor, authz.OpCertificateIssue, scope, now)
		if err != nil {
			return err
		}
		profile, err := r.PKI().BoundProfile(ctx, proof, req.Profile)
		if err != nil {
			return err
		}
		policy, err := decodePKIPolicy(profile.Policy)
		if err != nil {
			return err
		}
		if err := s.pkiCallerGate(ctx, az, caller, scope, policy, req.GenerateKey, true, now); err != nil {
			return err
		}
		resolved, err := policy.Check(pki.Request{
			CommonName: req.CommonName, DNSNames: req.DNSNames, IPAddresses: req.IPAddresses, URIs: req.URIs,
			TTL: req.TTL, PublicKey: publicKey, Generated: req.GenerateKey,
		})
		if err != nil {
			return pkiInvalid(err)
		}
		plan, err = s.reserveLeaf(ctx, r, proof, caller, profile, policy, resolved, publicKey, req.Issuer, keySource, "", "issue", now)
		return err
	})
	if err != nil {
		return CertificateIssueResult{}, err
	}
	der, signErr := s.signLeaf(plan)
	if signErr != nil {
		der = nil
	}
	settled, issued, err := s.settleLeaf(ctx, actor, authz.OpCertificateIssue, scope, plan, der, "issue", req.GenerateKey, nil, nil)
	if err != nil {
		// The row stays `issuing`; the worker moves it to `unknown` and the
		// CRL publishes it. Never reported as success.
		return CertificateIssueResult{}, err
	}
	if signErr != nil {
		return CertificateIssueResult{}, signErr
	}
	if !issued {
		return CertificateIssueResult{}, ErrPKISigning
	}
	result := CertificateIssueResult{Certificate: settled}
	if generatedKey != nil {
		pkcs8, err := pki.MarshalPrivateKey(generatedKey)
		if err != nil {
			return CertificateIssueResult{}, err
		}
		result.PrivateKeyPEM = pki.PrivateKeyPEM(pkcs8)
		wencrypto.Zero(pkcs8)
	}
	return result, nil
}

// RenewCertificate re-signs a certificate's public key inside its profile's
// renewal window, against the CURRENT profile and the CURRENT active issuer
// version (overlap rotation). A second renewal of the same certificate returns
// its successor instead of signing again.
func (s *PKI) RenewCertificate(ctx context.Context, actor Actor, scope domain.Scope, certificateID string) (CertificateView, error) {
	if err := envScopeRequired(scope, "certificate renew"); err != nil {
		return CertificateView{}, err
	}
	release, err := chargeDefaultAtEntry(ctx, s.DB, s.Budget, actor, authz.OpCertificateRenew, authz.OpCertificateRenew, scope, s.now)
	if err != nil {
		return CertificateView{}, err
	}
	defer release()
	var (
		plan      leafPlan
		successor *CertificateView
	)
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		successor = nil
		now := store.CanonTime(s.now())
		caller, proof, err := authorize(ctx, az, actor, authz.OpCertificateRenew, scope, now)
		if err != nil {
			return err
		}
		current, err := r.PKI().GetCertificate(ctx, proof, certificateID)
		if err != nil {
			return err
		}
		if current.State == "renewed" && current.RenewedBy != "" {
			next, err := r.PKI().GetCertificate(ctx, proof, current.RenewedBy)
			if err != nil {
				return err
			}
			issuer, err := r.PKI().IssuerPublic(ctx, proof, next.IssuerID)
			if err != nil {
				return err
			}
			view, err := pkiCertificateView(next, issuer)
			if err != nil {
				return err
			}
			successor = &view
			return nil
		}
		if current.State != "issued" {
			return ErrPKICertificateNot
		}
		if current.RenewedBy != "" {
			return fmt.Errorf("%w: a renewal of this certificate is in flight", domain.ErrConflict)
		}
		profile, err := r.PKI().BoundProfile(ctx, proof, current.ProfileName)
		if err != nil {
			return err
		}
		policy, err := decodePKIPolicy(profile.Policy)
		if err != nil {
			return err
		}
		if now.Before(current.NotAfter.Add(-policy.RenewWindow)) || !now.Before(current.NotAfter) {
			return ErrPKIRenewWindow
		}
		if err := s.pkiCallerGate(ctx, az, caller, scope, policy, false, false, now); err != nil {
			return err
		}
		leaf, err := x509.ParseCertificate(current.CertificateDER)
		if err != nil {
			return err
		}
		sans, err := decodeCertificateSANs(current)
		if err != nil {
			return err
		}
		// X.509 and database timestamps may lose subsecond precision. Keep the
		// minimum valid lifetime, but never round above a non-minute policy cap.
		ttl := min(max(current.NotAfter.Sub(current.NotBefore)-time.Minute, pki.MinLeafTTL), policy.MaxTTL)
		resolved, err := policy.Check(pki.Request{
			CommonName: current.CommonName, DNSNames: sans.DNS, IPAddresses: sans.IP, URIs: sans.URI,
			TTL: ttl, PublicKey: leaf.PublicKey, Renewal: true,
		})
		if err != nil {
			return pkiInvalid(err)
		}
		previousIssuer, err := r.PKI().IssuerPublic(ctx, proof, current.IssuerID)
		if err != nil {
			return err
		}
		preferred := ""
		if slices.Contains(policy.AllowedIssuers, previousIssuer.Name) {
			preferred = previousIssuer.Name
		}
		plan, err = s.reserveLeaf(ctx, r, proof, caller, profile, policy, resolved, leaf.PublicKey, preferred, current.KeySource, current.ID, "renew", now)
		if err != nil {
			return err
		}
		claimed, err := r.PKI().ClaimRenewal(ctx, proof, current.ID, plan.certID, now)
		if err != nil {
			return err
		}
		if !claimed {
			return fmt.Errorf("%w: a renewal of this certificate is in flight", domain.ErrConflict)
		}
		return nil
	})
	if err != nil {
		return CertificateView{}, err
	}
	if successor != nil {
		return *successor, nil
	}
	der, signErr := s.signLeaf(plan)
	if signErr != nil {
		der = nil
	}
	settled, issued, err := s.settleLeaf(ctx, actor, authz.OpCertificateRenew, scope, plan, der, "renew", false,
		func(ctx context.Context, r store.Repos, proof authz.Proof) error {
			completed, err := r.PKI().CompleteRenewal(ctx, proof, certificateID, plan.certID, store.CanonTime(s.now()))
			if err != nil {
				return err
			}
			if !completed {
				return ErrPKICertificateNot
			}
			return nil
		},
		func(ctx context.Context, r store.Repos, proof authz.Proof) error {
			// Release the claim so the certificate can be renewed again.
			_, err := r.PKI().ReleaseRenewal(ctx, proof, certificateID, plan.certID, store.CanonTime(s.now()))
			return err
		})
	if err != nil {
		return CertificateView{}, err
	}
	if signErr != nil {
		return CertificateView{}, signErr
	}
	if !issued {
		return CertificateView{}, ErrPKISigning
	}
	return settled, nil
}

// RevokeCertificate revokes a leaf with a caller-chosen RFC 5280 reason. It
// never re-checks that the profile still permits the names: revocation is the
// fail-safe direction. Revoking a revoked certificate returns it unchanged.
// If neither SAN metadata nor its signed leaf can render the response, the
// authorized revocation still commits; the returned error says it is revoked.
func (s *PKI) RevokeCertificate(ctx context.Context, actor Actor, scope domain.Scope, certificateID, reason string) (CertificateView, error) {
	if err := envScopeRequired(scope, "certificate revoke"); err != nil {
		return CertificateView{}, err
	}
	parsed, err := pki.ParseRequestedReason(reason)
	if err != nil {
		return CertificateView{}, pkiInvalid(err)
	}
	var out CertificateView
	var viewErr error
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		viewErr = nil
		now := store.CanonTime(s.now())
		caller, proof, err := authorize(ctx, az, actor, authz.OpCertificateRevoke, scope, now)
		if err != nil {
			return err
		}
		current, err := r.PKI().GetCertificate(ctx, proof, certificateID)
		if err != nil {
			return err
		}
		issuer, err := r.PKI().IssuerPublic(ctx, proof, current.IssuerID)
		if err != nil {
			return err
		}
		if current.State == "revoked" {
			out, viewErr = pkiCertificatePublicView(current, issuer)
			return nil
		}
		revoked, err := r.PKI().RevokeCertificate(ctx, proof, certificateID, string(parsed), now)
		if err != nil {
			return err
		}
		if !revoked {
			return ErrPKICertificateNot
		}
		if err := recordCertificateOutcome(ctx, r, proof, caller.Principal, certificateID, "revoke", current.Serial, issuer.Name, "revoked", string(parsed), audit.OutcomeSuccess); err != nil {
			return err
		}
		updated, err := r.PKI().GetCertificate(ctx, proof, certificateID)
		if err != nil {
			return err
		}
		// Display corruption must not undo fail-safe revocation or its audit.
		out, viewErr = pkiCertificatePublicView(updated, issuer)
		return nil
	})
	if err != nil {
		return CertificateView{}, err
	}
	if viewErr != nil {
		return CertificateView{}, fmt.Errorf("service: certificate is revoked, but response metadata unavailable: %w", viewErr)
	}
	return out, nil
}

// ListCertificates returns up to 500 certificates in the authorized
// environment, newest first, with public issuer chains. It requires a complete
// environment scope and propagates authorization and storage errors.
func (s *PKI) ListCertificates(ctx context.Context, actor Actor, scope domain.Scope) ([]CertificateView, error) {
	if err := envScopeRequired(scope, "certificate list"); err != nil {
		return nil, err
	}
	var out []CertificateView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		out = nil
		_, proof, err := authorize(ctx, az, actor, authz.OpCertificateInspect, scope, s.now())
		if err != nil {
			return err
		}
		rows, err := r.PKI().ListCertificates(ctx, proof)
		if err != nil {
			return err
		}
		issuers := map[string]store.PKIIssuer{}
		for _, row := range rows {
			issuer, ok := issuers[row.IssuerID]
			if !ok {
				if issuer, err = r.PKI().IssuerPublic(ctx, proof, row.IssuerID); err != nil {
					return err
				}
				issuers[row.IssuerID] = issuer
			}
			view, err := pkiCertificatePublicView(row, issuer)
			if err != nil {
				return err
			}
			out = append(out, view)
		}
		return nil
	})
	return out, err
}

// ShowCertificate returns one certificate and its public issuer chain within
// the authorized environment. Missing or out-of-scope records return not found;
// scope validation, authorization, and storage errors are propagated.
func (s *PKI) ShowCertificate(ctx context.Context, actor Actor, scope domain.Scope, certificateID string) (CertificateView, error) {
	if err := envScopeRequired(scope, "certificate show"); err != nil {
		return CertificateView{}, err
	}
	var out CertificateView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpCertificateInspect, scope, s.now())
		if err != nil {
			return err
		}
		cert, err := r.PKI().GetCertificate(ctx, proof, certificateID)
		if err != nil {
			return err
		}
		issuer, err := r.PKI().IssuerPublic(ctx, proof, cert.IssuerID)
		if err != nil {
			return err
		}
		out, err = pkiCertificatePublicView(cert, issuer)
		return err
	})
	return out, err
}

// CertificateCRL returns the CRL published by the issuer version that signed
// the certificate: the list a relying party checks that leaf against.
func (s *PKI) CertificateCRL(ctx context.Context, actor Actor, scope domain.Scope, certificateID string) ([]byte, error) {
	if err := envScopeRequired(scope, "certificate crl"); err != nil {
		return nil, err
	}
	var out []byte
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpCertificateInspect, scope, s.now())
		if err != nil {
			return err
		}
		cert, err := r.PKI().GetCertificate(ctx, proof, certificateID)
		if err != nil {
			return err
		}
		issuer, err := r.PKI().IssuerPublic(ctx, proof, cert.IssuerID)
		if err != nil {
			return err
		}
		if len(issuer.CRLDER) == 0 {
			return fmt.Errorf("%w: the issuer has not published a CRL yet", domain.ErrNotFound)
		}
		out = issuer.CRLDER
		return nil
	})
	return out, err
}

// PKIBoundProfileView is what an environment principal may see of a profile
// bound to its environment: the policy it issues under, never the bindings.
type PKIBoundProfileView struct {
	Name   string
	Policy pki.Policy
}

// BoundProfiles returns policies bound to the authorized environment or its
// whole project, without exposing bindings. It requires a complete environment
// scope and propagates authorization, storage, and policy decoding errors.
func (s *PKI) BoundProfiles(ctx context.Context, actor Actor, scope domain.Scope) ([]PKIBoundProfileView, error) {
	if err := envScopeRequired(scope, "certificate profile list"); err != nil {
		return nil, err
	}
	var out []PKIBoundProfileView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		out = nil
		_, proof, err := authorize(ctx, az, actor, authz.OpCertificateInspect, scope, s.now())
		if err != nil {
			return err
		}
		profiles, err := r.PKI().BoundProfiles(ctx, proof)
		if err != nil {
			return err
		}
		for _, profile := range profiles {
			policy, err := decodePKIPolicy(profile.Policy)
			if err != nil {
				return err
			}
			out = append(out, PKIBoundProfileView{Name: profile.Name, Policy: policy})
		}
		return nil
	})
	return out, err
}
