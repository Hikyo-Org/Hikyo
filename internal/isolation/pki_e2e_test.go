package isolation

import (
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// pkiClock is the test's controllable clock: renewal windows, expiry and the
// issuing deadline are all time-driven.
type pkiClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *pkiClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *pkiClock) Advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

func pkiSvc(t *testing.T, db *store.DB, clock *pkiClock) *service.PKI {
	t.Helper()
	return &service.PKI{
		DB: db, Keyring: probeKeyring(t, db), Runtime: store.NewPKIRuntime(db),
		IssuingDeadline: time.Minute, Now: clock.Now,
	}
}

func pkiGrant(t *testing.T, db *store.DB, id string, principal domain.PrincipalID, capability domain.Capability, env domain.EnvID) {
	t.Helper()
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('`+id+`','`+string(principal)+`','`+string(capability)+`','org_a','prj_a1','`+string(env)+`',`+ts+`)`)
}

func pkiWebPolicy() pki.Policy {
	return pki.Policy{
		AllowedIssuers: []string{"issuing"},
		DNSPatterns:    []string{"*.svc.example.com"},
		IPRanges:       []string{"10.0.0.0/8"},
		KeyAlgorithms:  []pki.KeyAlgorithm{pki.ECDSAP256, pki.Ed25519},
		KeyUsages:      []pki.KeyUsage{pki.UsageDigitalSignature},
		ExtKeyUsages:   []pki.ExtKeyUsage{pki.ExtUsageServerAuth, pki.ExtUsageClientAuth},
		MaxTTL:         72 * time.Hour, DefaultTTL: 24 * time.Hour, RenewWindow: 8 * time.Hour,
		AllowCSR: true, AllowGeneratedKey: true,
	}
}

func pkiCSR(t *testing.T) (string, any) {
	t.Helper()
	key, err := pki.GenerateKey(pki.ECDSAP256)
	if err != nil {
		t.Fatal(err)
	}
	der, err := pki.CreateCSR(key, pki.Subject{CommonName: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	return pki.CSRPEM(der), key.Public()
}

func parseLeaf(t *testing.T, view service.CertificateView) *x509.Certificate {
	t.Helper()
	certs, _, err := pki.ParseCertificates([]byte(view.CertificatePEM))
	if err != nil || len(certs) != 1 {
		t.Fatalf("certificate PEM: %v (%d)", err, len(certs))
	}
	return certs[0]
}

func verifyLeafChain(t *testing.T, view service.CertificateView, rootPEM string, dns string) {
	t.Helper()
	leaf := parseLeaf(t, view)
	roots, intermediates := x509.NewCertPool(), x509.NewCertPool()
	rootCerts, _, err := pki.ParseCertificates([]byte(rootPEM))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rootCerts {
		roots.AddCert(c)
	}
	chain, _, err := pki.ParseCertificates([]byte(view.ChainPEM))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chain {
		intermediates.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: intermediates, DNSName: dns, CurrentTime: leaf.NotBefore.Add(2 * time.Minute),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("leaf %s does not chain to the root: %v", view.Serial, err)
	}
}

func crlSerials(t *testing.T, der []byte) map[string]int {
	t.Helper()
	crl, err := x509.ParseRevocationList(der)
	if err != nil {
		t.Fatalf("parse CRL: %v", err)
	}
	out := map[string]int{}
	for _, entry := range crl.RevokedCertificateEntries {
		out[pki.SerialHex(entry.SerialNumber)] = entry.ReasonCode
	}
	return out
}

func sweepPKI(t *testing.T, svc *service.PKI) {
	t.Helper()
	if _, err := svc.RunPKISweep(t.Context()); err != nil {
		t.Fatalf("pki sweep: %v", err)
	}
}

// TestPKILifecycle drives the whole #154 lifecycle through the real service,
// runtime and store on both engines: CA setup (evaluation root, Hikyo-signed
// intermediate, offline-signed intermediate), profile refusal and
// narrowing-only updates, CSR and generated-key issuance (human and machine),
// renewal, revocation, the CRL, the unknown state, overlap rotation, the
// restore hold, and issuer compromise.
func TestPKILifecycle(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
		svc := pkiSvc(t, db, clock)
		op := service.LocalPrincipal(root)
		env := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
		human := service.LocalPrincipal(alice)
		pkiGrant(t, db, "g_pki_alice_issue", alice, domain.CapIssueCertificate, envA1)

		// --- CA setup ------------------------------------------------------
		rootCA, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "eval-root", CommonName: "Hikyo Eval Root", TTL: 365 * 24 * time.Hour})
		if err != nil {
			t.Fatalf("create root: %v", err)
		}
		if rootCA.Issuer.State != "active" || rootCA.Issuer.Kind != "root" || rootCA.Issuer.CertificatePEM == "" {
			t.Fatalf("root: %+v", rootCA.Issuer)
		}
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "eval-root", CommonName: "again"}); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("duplicate issuer name: %v", err)
		}
		if _, err := svc.CreateIssuer(ctx, service.LocalPrincipal(nobody), service.PKIIssuerRequest{Mode: "root", Name: "rogue", CommonName: "x"}); err == nil {
			t.Fatal("a principal without instance-config created a CA")
		}
		issuing, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "intermediate", Name: "issuing", CommonName: "Hikyo Issuing CA", ParentName: "eval-root", TTL: 90 * 24 * time.Hour, CRLDistributionURL: "https://pki.example.com/issuing.crl"})
		if err != nil {
			t.Fatalf("create intermediate: %v", err)
		}
		if issuing.Issuer.ParentID != rootCA.Issuer.ID || !strings.Contains(issuing.Issuer.ChainPEM, "CERTIFICATE") {
			t.Fatalf("intermediate chain: %+v", issuing.Issuer)
		}

		// Offline root: Hikyo generates the key and a CSR; the operator signs
		// it elsewhere and installs the certificate.
		offline, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "intermediate", Name: "offline", CommonName: "Hikyo Offline-Signed CA"})
		if err != nil {
			t.Fatalf("create pending intermediate: %v", err)
		}
		if offline.Issuer.State != "pending" || offline.Issuer.CSRPEM == "" || offline.Issuer.CertificatePEM != "" {
			t.Fatalf("pending intermediate: %+v", offline.Issuer)
		}
		offlineRootKey, _ := pki.GenerateKey(pki.ECDSAP384)
		offlineRootDER, err := pki.CreateRoot(offlineRootKey, pki.Subject{CommonName: "Air-Gapped Root"}, clock.Now(), 5*365*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		offlineRootCert, _ := x509.ParseCertificate(offlineRootDER)
		csr, err := pki.ParseCSR([]byte(offline.Issuer.CSRPEM))
		if err != nil {
			t.Fatal(err)
		}
		// A certificate for the wrong key is refused.
		wrongKey, _ := pki.GenerateKey(pki.ECDSAP256)
		wrongDER, _ := pki.SignIntermediate(pki.Parent{Certificate: offlineRootCert, Signer: offlineRootKey}, wrongKey.Public(), pki.Subject{CommonName: "wrong"}, clock.Now(), 365*24*time.Hour)
		if _, err := svc.InstallIssuerCertificate(ctx, op, "offline", pki.CertificatePEM(wrongDER), pki.CertificatePEM(offlineRootDER)); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("install of a certificate for another key: %v", err)
		}
		signedDER, err := pki.SignIntermediate(pki.Parent{Certificate: offlineRootCert, Signer: offlineRootKey}, csr.PublicKey, pki.Subject{CommonName: "Hikyo Offline-Signed CA"}, clock.Now(), 365*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		installed, err := svc.InstallIssuerCertificate(ctx, op, "offline", pki.CertificatePEM(signedDER), pki.CertificatePEM(offlineRootDER))
		if err != nil {
			t.Fatalf("install offline-signed certificate: %v", err)
		}
		if installed.State != "active" {
			t.Fatalf("installed state = %s", installed.State)
		}

		// --- Profiles --------------------------------------------------------
		bad := pkiWebPolicy()
		bad.AllowedIssuers = []string{"does-not-exist"}
		if _, err := svc.CreateProfile(ctx, op, "web", bad); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("profile naming a missing issuer: %v", err)
		}
		if _, err := svc.CreateProfile(ctx, op, "web", pkiWebPolicy()); err != nil {
			t.Fatalf("create profile: %v", err)
		}
		widened := pkiWebPolicy()
		widened.DNSPatterns = append(widened.DNSPatterns, "*.example.org")
		if _, err := svc.UpdateProfile(ctx, op, "web", widened, 0); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("widening update: %v", err)
		}
		narrowed := pkiWebPolicy()
		narrowed.IPRanges = nil
		if _, err := svc.UpdateProfile(ctx, op, "web", narrowed, 0); err != nil {
			t.Fatalf("narrowing update: %v", err)
		}
		reenable := narrowed
		reenable.IPRanges = []string{"10.1.0.0/16"}
		if _, err := svc.UpdateProfile(ctx, op, "web", reenable, 0); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("re-adding a removed range must be refused as widening: %v", err)
		}

		csrPEM, csrKey := pkiCSR(t)
		request := service.CertificateIssueRequest{Profile: "web", CSRPEM: csrPEM, DNSNames: []string{"api.svc.example.com"}, CommonName: "api.svc.example.com"}
		if _, err := svc.IssueCertificate(ctx, human, env, request); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("issuance through an unbound profile: %v", err)
		}
		if _, err := svc.BindProfile(ctx, op, "web", "org_a", "prj_a1", ""); err != nil {
			t.Fatalf("bind: %v", err)
		}
		if _, err := svc.BindProfile(ctx, op, "web", "org_a", "prj_a1", ""); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("duplicate bind: %v", err)
		}
		if _, err := svc.BindProfile(ctx, op, "web", "org_a", "prj_missing", ""); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("bind to a missing project: %v", err)
		}

		// --- Issuance ----------------------------------------------------------
		issued, err := svc.IssueCertificate(ctx, human, env, request)
		if err != nil {
			t.Fatalf("CSR issuance: %v", err)
		}
		if issued.PrivateKeyPEM != nil || issued.Certificate.State != "issued" || issued.Certificate.KeySource != "csr" {
			t.Fatalf("CSR issuance result: %+v", issued.Certificate)
		}
		leaf := parseLeaf(t, issued.Certificate)
		if !pki.PublicKeysEqual(leaf.PublicKey, csrKey) || leaf.IsCA || len(leaf.CRLDistributionPoints) != 1 {
			t.Fatalf("leaf shape: key-match=%v ca=%v crldp=%v", pki.PublicKeysEqual(leaf.PublicKey, csrKey), leaf.IsCA, leaf.CRLDistributionPoints)
		}
		verifyLeafChain(t, issued.Certificate, rootCA.Issuer.CertificatePEM, "api.svc.example.com")

		refusals := map[string]service.CertificateIssueRequest{
			"outside pattern":  {Profile: "web", CSRPEM: csrPEM, DNSNames: []string{"api.evil.example.com"}},
			"removed ip range": {Profile: "web", CSRPEM: csrPEM, IPAddresses: []string{"10.0.0.1"}},
			"too long":         {Profile: "web", CSRPEM: csrPEM, DNSNames: []string{"a.svc.example.com"}, TTL: 100 * time.Hour},
			"wrong issuer":     {Profile: "web", CSRPEM: csrPEM, DNSNames: []string{"a.svc.example.com"}, Issuer: "offline"},
			"both methods":     {Profile: "web", CSRPEM: csrPEM, GenerateKey: true, DNSNames: []string{"a.svc.example.com"}},
		}
		for name, req := range refusals {
			if _, err := svc.IssueCertificate(ctx, human, env, req); !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("%s: want a profile refusal, got %v", name, err)
			}
		}
		if _, err := svc.IssueCertificate(ctx, service.LocalPrincipal(reader), env, request); err == nil {
			t.Fatal("a principal without issue-certificate issued a certificate")
		}

		// Machine issuance: the profile's machine_issuance opt-in is the second,
		// live condition. The web profile lacks it, and enabling it would be a
		// widening, so a dedicated profile carries it.
		ident := identitySvc(db)
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_pki_alice_mi','usr_alice','manage-identities','org_a','prj_a1',NULL,`+ts+`)`)
		sa, err := ident.CreateServiceAccount(ctx, human, prjScope(), "pki-workload", domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		minted, err := ident.MintCredential(ctx, human, prjScope(), sa.ID, service.MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		pkiGrant(t, db, "g_pki_sa_issue", sa.Principal, domain.CapIssueCertificate, envA1)
		pkiGrant(t, db, "g_pki_sa_read", sa.Principal, domain.CapRead, envA1)
		workload := service.Bearer(minted.Value)
		generated := service.CertificateIssueRequest{Profile: "web", GenerateKey: true, KeyAlgorithm: "ed25519", URIs: nil, DNSNames: []string{"worker.svc.example.com"}}
		if _, err := svc.IssueCertificate(ctx, workload, env, generated); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("machine issuance without the opt-in must be the uniform not-found: %v", err)
		}
		machinePolicy := pkiWebPolicy()
		machinePolicy.MachineIssuance, machinePolicy.AllowCSR = true, false
		if _, err := svc.CreateProfile(ctx, op, "workload", machinePolicy); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.BindProfile(ctx, op, "workload", "org_a", "prj_a1", "env_a1"); err != nil {
			t.Fatal(err)
		}
		generated.Profile = "workload"
		machineCert, err := svc.IssueCertificate(ctx, workload, env, generated)
		if err != nil {
			t.Fatalf("machine generated-key issuance: %v", err)
		}
		key, err := pki.ParsePrivateKey(machineCert.PrivateKeyPEM)
		if err != nil {
			t.Fatalf("generated key: %v", err)
		}
		if !pki.PublicKeysEqual(key.Public(), parseLeaf(t, machineCert.Certificate).PublicKey) || machineCert.Certificate.PrincipalClass != string(domain.ClassWorkload) {
			t.Fatal("the generated key does not match the certificate")
		}
		keyBody := strings.Split(string(machineCert.PrivateKeyPEM), "\n")[1]
		for _, table := range []string{"pki_certificates", "pki_issuers", "audit_tenant_events", "audit_instance_events"} {
			if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE CAST(%s AS TEXT) LIKE '%%%s%%'", table, map[string]string{
				"pki_certificates": "sans", "pki_issuers": "chain_pem", "audit_tenant_events": "payload", "audit_instance_events": "payload",
			}[table], keyBody[:32])); got != 0 {
				t.Fatalf("generated private key material found in %s", table)
			}
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='pki.certificate_key_disclosed'"); got != 1 {
			t.Fatalf("key disclosure events = %d, want 1", got)
		}
		// A human generated-key issuance through a CSR-less profile works too.
		if _, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "workload", GenerateKey: true, DNSNames: []string{"h.svc.example.com"}}); err != nil {
			t.Fatalf("human generated-key issuance: %v", err)
		}

		// --- Renewal ----------------------------------------------------------
		if _, err := svc.RenewCertificate(ctx, human, env, issued.Certificate.ID); !errors.Is(err, service.ErrPKIRenewWindow) {
			t.Fatalf("renewal outside the window: %v", err)
		}
		clock.Advance(20 * time.Hour) // default ttl 24h, window 8h
		renewed, err := svc.RenewCertificate(ctx, human, env, issued.Certificate.ID)
		if err != nil {
			t.Fatalf("renew: %v", err)
		}
		if renewed.RenewedFrom != issued.Certificate.ID || renewed.KeyFingerprint != issued.Certificate.KeyFingerprint || renewed.Serial == issued.Certificate.Serial {
			t.Fatalf("renewal: %+v", renewed)
		}
		again, err := svc.RenewCertificate(ctx, human, env, issued.Certificate.ID)
		if err != nil || again.ID != renewed.ID {
			t.Fatalf("second renewal must return the successor: %v %s != %s", err, again.ID, renewed.ID)
		}
		prior, err := svc.ShowCertificate(ctx, human, env, issued.Certificate.ID)
		if err != nil || prior.State != "renewed" || prior.RenewedBy != renewed.ID {
			t.Fatalf("predecessor after renewal: %v %+v", err, prior)
		}

		// --- Revocation and the CRL -------------------------------------------------
		if _, err := svc.RevokeCertificate(ctx, human, env, renewed.ID, "ca-compromise"); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("a caller may not choose ca-compromise: %v", err)
		}
		// A worker snapshots entries, then another transaction revokes with an
		// older clock before publication. The captured sequence must leave
		// that issuer due even though the CRL timestamp is newer.
		sweepPKI(t, svc)
		staleDER, err := svc.IssuerCRL(ctx, op, "issuing", 0)
		if err != nil {
			t.Fatal(err)
		}
		candidates, err := svc.Runtime.DueCRLs(ctx, clock.Now().Add(48*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		var staleCandidate store.PKICRLCandidate
		for _, candidate := range candidates {
			if candidate.IssuerID == issuing.Issuer.ID {
				staleCandidate = candidate
			}
		}
		if staleCandidate.IssuerID == "" {
			t.Fatal("missing candidate")
		}
		snapshotEntries, err := svc.Runtime.RevokedEntries(ctx, staleCandidate.IssuerID, clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		revoked, err := svc.RevokeCertificate(ctx, human, env, renewed.ID, "key-compromise")
		if err != nil || revoked.State != "revoked" || revoked.RevocationReason != "key-compromise" {
			t.Fatalf("revoke: %v %+v", err, revoked)
		}
		if again, err := svc.RevokeCertificate(ctx, human, env, renewed.ID, "superseded"); err != nil || again.RevocationReason != "key-compromise" {
			t.Fatalf("revoking a revoked certificate must be an idempotent success: %v %+v", err, again)
		}

		published, err := svc.Runtime.PublishCRL(ctx, staleCandidate, staleDER, staleCandidate.CRLNumber+1, len(snapshotEntries), clock.Now().Add(time.Minute), clock.Now().Add(24*time.Hour))
		if err != nil || !published {
			t.Fatalf("publish older snapshot: %v %v", published, err)
		}
		pending, err := svc.Runtime.DueCRLs(ctx, clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, candidate := range pending {
			if candidate.IssuerID == issuing.Issuer.ID {
				found = true
				if candidate.RevocationSeq != staleCandidate.RevocationSeq+1 {
					t.Fatalf("revocation sequence=%d", candidate.RevocationSeq)
				}
			}
		}
		if !found {
			t.Fatal("concurrent revocation lost behind newer CRL timestamp")
		}
		sweepPKI(t, svc)
		pending, err = svc.Runtime.DueCRLs(ctx, clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range pending {
			if candidate.IssuerID == issuing.Issuer.ID {
				t.Fatal("caught-up CRL still due")
			}
		}

		// A crash between reserving a serial and recording the leaf: the row
		// stays issuing, the worker moves it to unknown, and the CRL lists it.
		execRaw(t, db, `INSERT INTO pki_certificates (id,org_id,project_id,environment_id,profile_id,profile_name,issuer_id,serial,state,key_source,key_algorithm,key_fingerprint,sans,not_before,not_after,principal_id,principal_class,issuing_deadline,created_at,updated_at) VALUES ('cert_crash','org_a','prj_a1','env_a1','pkip_x','web','`+issuing.Issuer.ID+`','abcdef0123','issuing','csr','ecdsa-p256','sha256:x','{"dns":[],"ip":[],"uri":[]}',`+pkiStamp(db, clock.Now())+`,`+pkiStamp(db, clock.Now().Add(24*time.Hour))+`,'usr_alice','human',`+pkiStamp(db, clock.Now().Add(-time.Second))+`,`+pkiStamp(db, clock.Now())+`,`+pkiStamp(db, clock.Now())+`)`)
		sweepPKI(t, svc)
		crashed, err := svc.ShowCertificate(ctx, human, env, "cert_crash")
		if err != nil || crashed.State != "unknown" {
			t.Fatalf("stale issuing row: %v %+v", err, crashed)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='pki.certificate_transition_outcome' AND outcome='unknown'"); got != 1 {
			t.Fatalf("unknown outcome events = %d, want 1", got)
		}
		crl, err := svc.IssuerCRL(ctx, op, "issuing", 0)
		if err != nil {
			t.Fatalf("issuer CRL: %v", err)
		}
		serials := crlSerials(t, crl)
		if code, ok := serials[renewed.Serial]; !ok || code != 1 {
			t.Fatalf("CRL lacks the revoked serial with keyCompromise: %v", serials)
		}
		if _, ok := serials["abcdef0123"]; !ok {
			t.Fatalf("CRL lacks the unknown serial: %v", serials)
		}
		if _, ok := serials[issued.Certificate.Serial]; ok {
			t.Fatal("CRL lists a certificate that was renewed, not revoked")
		}
		if envCRL, err := svc.CertificateCRL(ctx, human, env, renewed.ID); err != nil || string(envCRL) != string(crl) {
			t.Fatalf("environment CRL read: %v", err)
		}
		if _, err := svc.PublishIssuerCRL(ctx, op, "issuing", 0); err != nil {
			t.Fatalf("forced CRL publish: %v", err)
		}
		// An unknown-outcome leaf has no certificate DER; revoking it must
		// still satisfy the table's DER-presence constraint.
		if revokedUnknown, err := svc.RevokeCertificate(ctx, human, env, "cert_crash", "unspecified"); err != nil || revokedUnknown.State != "revoked" {
			t.Fatalf("revoke unknown certificate: %v %+v", err, revokedUnknown)
		}

		// --- Overlap rotation -------------------------------------------------------
		shortLived, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "web", CSRPEM: csrPEM, DNSNames: []string{"short.svc.example.com"}, TTL: 10 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		rotated, err := svc.RotateIssuer(ctx, op, "issuing", service.PKIIssuerRequest{})
		if err != nil {
			t.Fatalf("rotate: %v", err)
		}
		if rotated.Issuer.Version != 2 || rotated.Issuer.State != "active" || rotated.Issuer.KeyFingerprint == issuing.Issuer.KeyFingerprint {
			t.Fatalf("rotated version: %+v", rotated.Issuer)
		}
		versions, err := svc.ShowIssuer(ctx, op, "issuing")
		if err != nil || len(versions) != 2 || versions[0].State != "retiring" {
			t.Fatalf("after rotation: %v %+v", err, versions)
		}
		fresh, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "web", CSRPEM: csrPEM, DNSNames: []string{"new.svc.example.com"}})
		if err != nil || fresh.Certificate.IssuerVersion != 2 {
			t.Fatalf("issuance after rotation must use version 2: %v %+v", err, fresh.Certificate)
		}
		verifyLeafChain(t, fresh.Certificate, rootCA.Issuer.CertificatePEM, "new.svc.example.com")
		clock.Advance(4 * time.Hour) // short-lived cert (10h) enters its 8h window
		migrated, err := svc.RenewCertificate(ctx, human, env, shortLived.Certificate.ID)
		if err != nil || migrated.IssuerVersion != 2 {
			t.Fatalf("renewal must move to the active version: %v %+v", err, migrated)
		}
		if _, err := svc.RetireIssuer(ctx, op, "issuing", 1); !errors.Is(err, service.ErrPKIIssuerLive) {
			t.Fatalf("retiring a version with live leaves: %v", err)
		}
		clock.Advance(80 * time.Hour) // every version-1 leaf is now past not_after
		sweepPKI(t, svc)
		if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_certificates WHERE state='expired'"); got == 0 {
			t.Fatal("the worker expired nothing")
		}
		retired, err := svc.RetireIssuer(ctx, op, "issuing", 1)
		if err != nil || retired.State != "retired" {
			t.Fatalf("retire: %v %+v", err, retired)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_issuers WHERE id='"+issuing.Issuer.ID+"' AND encrypted_private_key IS NULL AND dek_version IS NULL"); got != 1 {
			t.Fatal("retirement did not destroy the key")
		}

		// --- Restore hold -------------------------------------------------------------
		execRaw(t, db, `UPDATE pki_issuers SET restore_hold=1`)
		if _, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "web", CSRPEM: csrPEM, DNSNames: []string{"held.svc.example.com"}}); !errors.Is(err, service.ErrPKIIssuerHeld) {
			t.Fatalf("issuance from a held issuer: %v", err)
		}
		// A restore hold retains stored CRL reads but forbids signing a fresh
		// snapshot until the operator reconciles post-backup revocations.
		beforeHoldDER, err := svc.IssuerCRL(ctx, op, "issuing", 2)
		if err != nil {
			t.Fatal(err)
		}
		clock.Advance(24 * time.Hour)
		sweepPKI(t, svc)
		heldDER, err := svc.IssuerCRL(ctx, op, "issuing", 2)
		if err != nil {
			t.Fatalf("held issuer CRL: %v", err)
		}
		heldCRL, err := x509.ParseRevocationList(heldDER)
		if err != nil {
			t.Fatal(err)
		}
		beforeHoldCRL, err := x509.ParseRevocationList(beforeHoldDER)
		if err != nil {
			t.Fatal(err)
		}
		if heldCRL.Number.Cmp(beforeHoldCRL.Number) != 0 {
			t.Fatal("held issuer CRL was refreshed by the worker")
		}
		if _, err := svc.PublishIssuerCRL(ctx, op, "issuing", 2); !errors.Is(err, service.ErrPKIIssuerHeld) {
			t.Fatalf("manual held issuer CRL publication did not refuse: %v", err)
		}
		if _, err := svc.ReleaseIssuerHold(ctx, op, "issuing"); err != nil {
			t.Fatalf("release hold: %v", err)
		}
		if _, err := svc.PublishIssuerCRL(ctx, op, "issuing", 2); err != nil {
			t.Fatalf("CRL publication after revocation reconciliation: %v", err)
		}
		afterHold, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "web", CSRPEM: csrPEM, DNSNames: []string{"held.svc.example.com"}})
		if err != nil {
			t.Fatalf("issuance after reconcile: %v", err)
		}

		// --- Concurrent renewal: one successor -----------------------------------------
		clock.Advance(20 * time.Hour)
		var wg sync.WaitGroup
		results := make([]string, 4)
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				view, err := svc.RenewCertificate(ctx, human, env, afterHold.Certificate.ID)
				if err == nil {
					results[i] = view.ID
				}
			}()
		}
		wg.Wait()
		if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_certificates WHERE renewed_from='"+afterHold.Certificate.ID+"' AND state='issued'"); got != 1 {
			t.Fatalf("concurrent renewals produced %d issued successors, want exactly 1 (%v)", got, results)
		}

		// --- Compromise ---------------------------------------------------------------
		// An in-flight leaf (no DER yet) is swept into the cascade too.
		execRaw(t, db, `INSERT INTO pki_certificates (id,org_id,project_id,environment_id,profile_id,profile_name,issuer_id,serial,state,key_source,key_algorithm,key_fingerprint,sans,not_before,not_after,principal_id,principal_class,issuing_deadline,created_at,updated_at) VALUES ('cert_inflight','org_a','prj_a1','env_a1','pkip_x','web','`+rotated.Issuer.ID+`','abcdef0124','issuing','csr','ecdsa-p256','sha256:y','{"dns":[],"ip":[],"uri":[]}',`+pkiStamp(db, clock.Now())+`,`+pkiStamp(db, clock.Now().Add(24*time.Hour))+`,'usr_alice','human',`+pkiStamp(db, clock.Now().Add(time.Hour))+`,`+pkiStamp(db, clock.Now())+`,`+pkiStamp(db, clock.Now())+`)`)
		compromised, err := svc.RevokeIssuer(ctx, op, "issuing", 2)
		if err != nil || compromised.State != "revoked" {
			t.Fatalf("revoke issuer: %v %+v", err, compromised)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_certificates WHERE issuer_id='"+rotated.Issuer.ID+"' AND state IN ('issued','renewed')"); got != 0 {
			t.Fatalf("%d leaves of a compromised issuer are still live", got)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_certificates WHERE issuer_id='"+rotated.Issuer.ID+"' AND revocation_reason='ca-compromise'"); got == 0 {
			t.Fatal("issuer compromise revoked nothing")
		}
		if _, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "web", CSRPEM: csrPEM, DNSNames: []string{"x.svc.example.com"}}); !errors.Is(err, service.ErrPKINoActiveIssuer) {
			t.Fatalf("issuance from a revoked issuer: %v", err)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_certificates WHERE id='cert_inflight' AND state='revoked'"); got != 1 {
			t.Fatal("issuer compromise left an in-flight leaf unrevoked")
		}
		// A pending version (CSR only, no certificate) can be revoked.
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "intermediate", Name: "abandoned", CommonName: "Abandoned CA"}); err != nil {
			t.Fatalf("create pending intermediate: %v", err)
		}
		if abandoned, err := svc.RevokeIssuer(ctx, op, "abandoned", 1); err != nil || abandoned.State != "revoked" {
			t.Fatalf("revoke pending issuer: %v %+v", err, abandoned)
		}

		// Inventory reads and profile teardown.
		if _, err := svc.ListIssuers(ctx, op); err != nil {
			t.Fatal(err)
		}
		profiles, err := svc.ListProfiles(ctx, op)
		if err != nil || len(profiles) != 2 {
			t.Fatalf("profiles: %v %d", err, len(profiles))
		}
		if _, err := svc.UnbindProfile(ctx, op, "workload", profiles[1].Bindings[0].ID); err != nil {
			t.Fatalf("unbind: %v", err)
		}
		if err := svc.DeleteProfile(ctx, op, "workload"); err != nil {
			t.Fatalf("delete profile: %v", err)
		}
		if list, err := svc.ListCertificates(ctx, human, env); err != nil || len(list) == 0 {
			t.Fatalf("list certificates: %v %d", err, len(list))
		}
	})
}

// pkiStamp spells a timestamp literal for raw fixture SQL on either engine.
func pkiStamp(db *store.DB, at time.Time) string {
	stamp := store.CanonTime(at).Format("2006-01-02T15:04:05.000000Z")
	if db.Engine() == store.EnginePostgres {
		return "'" + stamp + "'::timestamptz"
	}
	return "'" + stamp + "'"
}

// runPKILifecycle emits every pki.* audit type through the real service,
// runtime and store for the registry-emitter closure check (#154).
func runPKILifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := t.Context()
	clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
	svc := pkiSvc(t, db, clock)
	op := service.LocalPrincipal(root)
	env := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_pki_audit_issue','usr_alice','issue-certificate','org_a','prj_a1','env_a1',`+ts+`)`)
	if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "issuing", CommonName: "Audit Root"}); err != nil {
		t.Fatalf("pki create issuer: %v", err)
	}
	if _, err := svc.ListIssuers(ctx, op); err != nil {
		t.Fatal(err)
	}
	policy := pkiWebPolicy()
	policy.AllowCSR = false
	if _, err := svc.CreateProfile(ctx, op, "audit", policy); err != nil {
		t.Fatalf("pki create profile: %v", err)
	}
	if _, err := svc.BindProfile(ctx, op, "audit", "org_a", "prj_a1", "env_a1"); err != nil {
		t.Fatal(err)
	}
	issued, err := svc.IssueCertificate(ctx, service.LocalPrincipal(alice), env, service.CertificateIssueRequest{Profile: "audit", GenerateKey: true, DNSNames: []string{"audit.svc.example.com"}})
	if err != nil {
		t.Fatalf("pki issue: %v", err)
	}
	if _, err := svc.RevokeCertificate(ctx, service.LocalPrincipal(alice), env, issued.Certificate.ID, "superseded"); err != nil {
		t.Fatal(err)
	}
	sweepPKI(t, svc)
	for _, check := range []struct{ table, typ string }{
		{"audit_instance_events", "pki.inventory_read"}, {"audit_instance_events", "pki.issuer"},
		{"audit_instance_events", "pki.profile"}, {"audit_instance_events", "pki.crl_published"},
		{"audit_tenant_events", "pki.certificate_transition_intent"}, {"audit_tenant_events", "pki.certificate_transition_outcome"},
		{"audit_tenant_events", "pki.certificate_key_disclosed"},
	} {
		if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE type='%s'", check.table, check.typ)); got == 0 {
			t.Errorf("pki audit lifecycle did not emit %s", check.typ)
		}
	}
}
