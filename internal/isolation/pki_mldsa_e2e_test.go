package isolation

import (
	"crypto"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
)

func pkiPQSvc(t *testing.T, db *store.DB, clock *pkiClock) *service.PKI {
	t.Helper()
	svc := pkiSvc(t, db, clock)
	svc.MLDSAEnabled = true
	return svc
}

func pkiPQPolicy(issuer string) pki.Policy {
	return pki.Policy{
		AllowedIssuers: []string{issuer},
		DNSPatterns:    []string{"*.svc.example.com"},
		KeyAlgorithms:  []pki.KeyAlgorithm{pki.MLDSA65},
		KeyUsages:      []pki.KeyUsage{pki.UsageDigitalSignature},
		ExtKeyUsages:   []pki.ExtKeyUsage{pki.ExtUsageServerAuth, pki.ExtUsageClientAuth},
		MaxTTL:         72 * time.Hour, DefaultTTL: 24 * time.Hour, RenewWindow: 8 * time.Hour,
		AllowCSR: true, AllowGeneratedKey: true,
	}
}

func mldsaCSR(t *testing.T) (string, *mldsa.PrivateKey) {
	t.Helper()
	key, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "ignored"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pki.CSRPEM(der), key
}

func chainOf(t *testing.T, view service.CertificateView, rootPEM string) []*x509.Certificate {
	t.Helper()
	leaf := parseLeaf(t, view)
	roots, intermediates := poolOf(t, rootPEM), poolOf(t, view.ChainPEM)
	chains, err := leaf.Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: intermediates, CurrentTime: leaf.NotBefore.Add(2 * time.Minute),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		t.Fatalf("leaf %s does not chain: %v", view.Serial, err)
	}
	return chains[0]
}

// TestPKIMLDSALifecycle drives the ML-DSA-65 private PKI (#1133) through the
// real service, runtime and store on both engines: a disposable offline
// ML-DSA root signs an intermediate through the CSR and install flow, leaves
// are issued by CSR and by generated key, renewed, revoked, listed on a CRL
// signed by the issuer algorithm, the issuer is rotated with overlap, and
// real crypto/tls peers authenticate with the result. Every certificate and
// CRL is checked again by crypto/x509 and crypto/mldsa directly, not through
// the issuance path.
func TestPKIMLDSALifecycle(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
		svc := pkiPQSvc(t, db, clock)
		held := pkiSvc(t, db, clock) // the production wiring: ML-DSA not enabled
		op := service.LocalPrincipal(root)
		human := service.LocalPrincipal(alice)
		env := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
		pkiGrant(t, db, "g_pqpki_alice_issue", alice, domain.CapIssueCertificate, envA1)
		count := func(q string) int64 { return queryInt(t, db, q) }

		// --- The default wiring refuses every ML-DSA entry point ------------------
		pqKey, err := mldsa.GenerateKey(mldsa.MLDSA65())
		if err != nil {
			t.Fatal(err)
		}
		pqKeyPKCS8, err := pki.MarshalPrivateKey(pqKey)
		if err != nil {
			t.Fatal(err)
		}
		importRootDER, err := pki.CreateRoot(pqKey, pki.Subject{CommonName: "Imported PQ Root"}, clock.Now(), 365*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		heldRefusals := map[string]func() error{
			"generate root": func() error {
				_, err := held.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "held-root", CommonName: "x", KeyAlgorithm: "mldsa65"})
				return err
			},
			"import key": func() error {
				_, err := held.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "import", Name: "held-import", PrivateKeyPEM: pki.PrivateKeyPEM(pqKeyPKCS8), CertificatePEM: pki.CertificatePEM(importRootDER)})
				return err
			},
			"profile": func() error {
				_, err := held.CreateProfile(ctx, op, "held-profile", pkiPQPolicy("whatever"))
				return err
			},
			// The generated-key gate runs before generation and before any profile
			// lookup, so no profile or issuer needs to exist to reach it.
			"generated leaf key": func() error {
				_, err := held.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "none", GenerateKey: true, KeyAlgorithm: "mldsa65", DNSNames: []string{"held.svc.example.com"}})
				return err
			},
		}
		for name, run := range heldRefusals {
			if err := run(); !errors.Is(err, service.ErrPKIAlgorithmHeld) || !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("default wiring, %s: %v", name, err)
			}
		}
		if n := count(`SELECT COUNT(*) FROM pki_issuers`) + count(`SELECT COUNT(*) FROM pki_profiles`); n != 0 {
			t.Fatalf("a refused ML-DSA request left %d rows", n)
		}

		// --- Roots, mixed-chain refusals, offline intermediate -----------------------
		classicRoot, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "classic-root", CommonName: "Classic Root"})
		if err != nil {
			t.Fatal(err)
		}
		pqRoot, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "pq-eval-root", CommonName: "PQ Evaluation Root", KeyAlgorithm: "mldsa65", TTL: 365 * 24 * time.Hour})
		if err != nil {
			t.Fatalf("ML-DSA evaluation root: %v", err)
		}
		if pqRoot.Issuer.KeyAlgorithm != "mldsa65" || pqRoot.Issuer.State != "active" {
			t.Fatalf("pq root: %+v", pqRoot.Issuer)
		}
		mixed := map[string]service.PKIIssuerRequest{
			"ML-DSA intermediate under a classical parent":  {Mode: "intermediate", Name: "mixed-a", CommonName: "x", KeyAlgorithm: "mldsa65", ParentName: "classic-root"},
			"classical intermediate under an ML-DSA parent": {Mode: "intermediate", Name: "mixed-b", CommonName: "x", ParentName: "pq-eval-root"},
			"ML-DSA-44": {Mode: "root", Name: "mixed-c", CommonName: "x", KeyAlgorithm: "mldsa44"},
			"ML-DSA-87": {Mode: "root", Name: "mixed-d", CommonName: "x", KeyAlgorithm: "mldsa87"},
			"uppercase": {Mode: "root", Name: "mixed-e", CommonName: "x", KeyAlgorithm: "MLDSA65"},
		}
		for name, req := range mixed {
			if _, err := svc.CreateIssuer(ctx, op, req); !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("%s: %v", name, err)
			}
		}
		if n := count(`SELECT COUNT(*) FROM pki_issuers WHERE name LIKE 'mixed-%'`); n != 0 {
			t.Fatalf("refused issuers left %d rows", n)
		}
		issuing, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "intermediate", Name: "pq-issuing", CommonName: "PQ Issuing CA", KeyAlgorithm: "mldsa65", ParentName: "pq-eval-root", TTL: 90 * 24 * time.Hour})
		if err != nil {
			t.Fatalf("Hikyo-signed ML-DSA intermediate: %v", err)
		}
		if issuing.Issuer.KeyAlgorithm != "mldsa65" || issuing.Issuer.ParentID != pqRoot.Issuer.ID {
			t.Fatalf("pq intermediate: %+v", issuing.Issuer)
		}

		// Offline root: Hikyo makes the key and a CSR; the operator's offline
		// ML-DSA root signs it elsewhere; install activates it.
		offline, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "intermediate", Name: "pq-offline", CommonName: "PQ Offline-Signed CA", KeyAlgorithm: "mldsa65"})
		if err != nil {
			t.Fatal(err)
		}
		if offline.Issuer.State != "pending" || !strings.Contains(offline.Issuer.CSRPEM, "CERTIFICATE REQUEST") {
			t.Fatalf("pending: %+v", offline.Issuer)
		}
		offlineRootKey, _ := pki.GenerateKey(pki.MLDSA65)
		offlineRootDER, err := pki.CreateRoot(offlineRootKey, pki.Subject{CommonName: "Air-Gapped PQ Root"}, clock.Now(), 5*365*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		offlineRoot, _ := x509.ParseCertificate(offlineRootDER)
		offlineParent := pki.Parent{Certificate: offlineRoot, Signer: offlineRootKey}
		csr, err := pki.ParseCSR([]byte(offline.Issuer.CSRPEM))
		if err != nil {
			t.Fatal(err)
		}
		// Refusals: a certificate for another key, and a classical root signing
		// the post-quantum CSR (valid X.509, not a PQ-authenticated chain).
		otherKey, _ := pki.GenerateKey(pki.MLDSA65)
		wrongDER, _ := pki.SignIntermediate(offlineParent, otherKey.Public(), pki.Subject{CommonName: "wrong"}, clock.Now(), 365*24*time.Hour)
		if _, err := svc.InstallIssuerCertificate(ctx, op, "pq-offline", pki.CertificatePEM(wrongDER), pki.CertificatePEM(offlineRootDER)); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("install of a certificate for another key: %v", err)
		}
		classicalRootKey, _ := pki.GenerateKey(pki.ECDSAP384)
		classicalRootDER, _ := pki.CreateRoot(classicalRootKey, pki.Subject{CommonName: "Classical Offline Root"}, clock.Now(), 5*365*24*time.Hour)
		classicalRoot, _ := x509.ParseCertificate(classicalRootDER)
		template := &x509.Certificate{
			SerialNumber: bigSerial(t), Subject: pkix.Name{CommonName: "PQ Offline-Signed CA"},
			NotBefore: clock.Now().Add(-time.Minute), NotAfter: clock.Now().Add(365 * 24 * time.Hour),
			KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true, MaxPathLenZero: true,
		}
		mixedDER, err := x509.CreateCertificate(rand.Reader, template, classicalRoot, csr.PublicKey, classicalRootKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.InstallIssuerCertificate(ctx, op, "pq-offline", pki.CertificatePEM(mixedDER), pki.CertificatePEM(classicalRootDER)); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("install under a classical root: %v", err)
		}
		if got := count(`SELECT COUNT(*) FROM pki_issuers WHERE name='pq-offline' AND state='pending'`); got != 1 {
			t.Fatal("a refused install changed the pending version")
		}
		signedDER, err := pki.SignIntermediate(offlineParent, csr.PublicKey, pki.Subject{CommonName: "PQ Offline-Signed CA"}, clock.Now(), 365*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		installed, err := svc.InstallIssuerCertificate(ctx, op, "pq-offline", pki.CertificatePEM(signedDER), pki.CertificatePEM(offlineRootDER))
		if err != nil || installed.State != "active" || installed.KeyAlgorithm != "mldsa65" {
			t.Fatalf("install: %v %+v", err, installed)
		}

		// Import: an existing ML-DSA CA key and certificate as protected input.
		imported, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "import", Name: "pq-imported", PrivateKeyPEM: pki.PrivateKeyPEM(pqKeyPKCS8), CertificatePEM: pki.CertificatePEM(importRootDER)})
		if err != nil || imported.Issuer.KeyAlgorithm != "mldsa65" || imported.Issuer.Kind != "root" {
			t.Fatalf("import: %v %+v", err, imported.Issuer)
		}
		other44, _ := mldsa.GenerateKey(mldsa.MLDSA44())
		pkcs844, _ := pki.MarshalPrivateKey(other44)
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "import", Name: "pq-import-44", PrivateKeyPEM: pki.PrivateKeyPEM(pkcs844), CertificatePEM: pki.CertificatePEM(importRootDER)}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("ML-DSA-44 import: %v", err)
		}
		// Import refusals that the pki unit tests also cover, here through the service on
		// both engines: a chain that stops below the real classical root, a trust anchor
		// whose self-signature is damaged, and an expanded (not seed-only) PKCS#8 key.
		classicalR := newClassicalRoot(t, clock.Now())
		y := newMLDSACA(t, classicalR, "Y", 2, clock.Now())
		x := newMLDSACA(t, y, "X", 1, clock.Now())
		leafCAKey, _ := pki.GenerateKey(pki.MLDSA65)
		leafCADER := signCA(t, x, leafCAKey.Public(), "I", 0, clock.Now())
		leafCAPKCS8, _ := pki.MarshalPrivateKey(leafCAKey)
		for name, chain := range map[string]string{
			"chain stopping at X (real root is classical)": pki.CertificatePEM(x.Certificate.Raw),
			"chain stopping at Y":                          pki.CertificatePEM(x.Certificate.Raw) + pki.CertificatePEM(y.Certificate.Raw),
		} {
			if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "import", Name: "pq-trunc", PrivateKeyPEM: pki.PrivateKeyPEM(leafCAPKCS8), CertificatePEM: pki.CertificatePEM(leafCADER), ChainPEM: chain}); !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("import with a %s: %v", name, err)
			}
		}
		damaged := append([]byte(nil), offlineRootDER...)
		damaged[len(damaged)-3] ^= 0x01
		underOffline := signCA(t, pki.Parent{Certificate: offlineRoot, Signer: offlineRootKey}, leafCAKey.Public(), "Under offline root", 0, clock.Now())
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "import", Name: "pq-damaged-anchor", PrivateKeyPEM: pki.PrivateKeyPEM(leafCAPKCS8), CertificatePEM: pki.CertificatePEM(underOffline), ChainPEM: pki.CertificatePEM(damaged)}); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("import under a root with a damaged self-signature: %v", err)
		}
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "import", Name: "pq-control", PrivateKeyPEM: pki.PrivateKeyPEM(leafCAPKCS8), CertificatePEM: pki.CertificatePEM(underOffline), ChainPEM: pki.CertificatePEM(offlineRootDER)}); err != nil {
			t.Errorf("control: the same import under the intact root must succeed: %v", err)
		}
		// Seed plus expanded, built from the seed of the key that matches importRootDER,
		// so the only thing wrong with the import is the encoding: a parser that ignored
		// the seed-only rule would take the seed and accept it. Zero-filled expanded bytes
		// are never read by an accepting parser; a rejecting one refuses the form itself.
		type pkcs8Info struct {
			Version    int
			Algorithm  pkix.AlgorithmIdentifier
			PrivateKey []byte
		}
		var matching pkcs8Info
		if _, err := asn1.Unmarshal(pqKeyPKCS8, &matching); err != nil || len(matching.PrivateKey) != 34 {
			t.Fatalf("matching key: %v (%d bytes)", err, len(matching.PrivateKey))
		}
		pair, err := asn1.Marshal(struct {
			Seed     []byte
			Expanded []byte
		}{matching.PrivateKey[2:], make([]byte, 4032)})
		if err != nil {
			t.Fatal(err)
		}
		matching.PrivateKey = pair
		forged, err := asn1.Marshal(matching)
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "import", Name: "pq-expanded", PrivateKeyPEM: pki.PrivateKeyPEM(forged), CertificatePEM: pki.CertificatePEM(importRootDER)})
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "private key") {
			t.Errorf("seed-plus-expanded PKCS#8 import must be refused as an unparseable private key: %v", err)
		}
		if n := count(`SELECT COUNT(*) FROM pki_issuers WHERE name IN ('pq-trunc','pq-damaged-anchor','pq-expanded')`); n != 0 {
			t.Fatalf("refused imports left %d rows", n)
		}
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "import", Name: "pq-import-bad", PrivateKeyPEM: pki.PrivateKeyPEM(pqKeyPKCS8), CertificatePEM: pki.CertificatePEM(offlineRootDER)}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("import of a key that does not match the certificate: %v", err)
		}
		// Sealed keys are PKCS#8 ciphertext: no plaintext key and no seed in the rows.
		if n := count(`SELECT COUNT(*) FROM pki_issuers WHERE key_algorithm='mldsa65' AND encrypted_private_key IS NOT NULL`); n < 4 {
			t.Fatalf("expected sealed ML-DSA issuer keys, found %d", n)
		}

		// --- Profile, binding, issuance ------------------------------------------------
		if _, err := svc.CreateProfile(ctx, op, "pq-web", pkiPQPolicy("pq-issuing")); err != nil {
			t.Fatalf("ML-DSA profile: %v", err)
		}
		classicProfile := pkiWebPolicy()
		classicProfile.AllowedIssuers = []string{"pq-issuing"}
		if _, err := svc.CreateProfile(ctx, op, "classic-web", classicProfile); err != nil {
			t.Fatal(err)
		}
		// Adding the algorithm to the classical profile is a widening; the
		// issuer-name reference cannot smuggle it in.
		widened := classicProfile
		widened.KeyAlgorithms = append(append([]pki.KeyAlgorithm{}, classicProfile.KeyAlgorithms...), pki.MLDSA65)
		if _, err := svc.UpdateProfile(ctx, op, "classic-web", widened, 0); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("adding mldsa65 to a classical profile: %v", err)
		}
		for _, name := range []string{"pq-web", "classic-web"} {
			if _, err := svc.BindProfile(ctx, op, name, "org_a", "prj_a1", ""); err != nil {
				t.Fatal(err)
			}
		}
		csrPEM, csrKey := mldsaCSR(t)
		request := service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: csrPEM, DNSNames: []string{"web.svc.example.com"}, CommonName: "web.svc.example.com"}
		issued, err := svc.IssueCertificate(ctx, human, env, request)
		if err != nil {
			t.Fatalf("ML-DSA CSR issuance: %v", err)
		}
		leaf := parseLeaf(t, issued.Certificate)
		if issued.Certificate.KeyAlgorithm != "mldsa65" || !pki.PublicKeysEqual(leaf.PublicKey, csrKey.Public()) || leaf.SignatureAlgorithm != x509.MLDSA65 {
			t.Fatalf("issued leaf: algorithm=%s sig=%v", issued.Certificate.KeyAlgorithm, leaf.SignatureAlgorithm)
		}
		chain := chainOf(t, issued.Certificate, pqRoot.Issuer.CertificatePEM)
		if err := pki.CheckPQPath(chain); err != nil || len(chain) != 3 {
			t.Fatalf("path: %v (%d certificates)", err, len(chain))
		}
		// Independent verification of the leaf signature, not through x509.
		if err := mldsa.Verify(chain[1].PublicKey.(*mldsa.PublicKey), leaf.RawTBSCertificate, leaf.Signature, nil); err != nil {
			t.Fatalf("direct ML-DSA verification of the leaf: %v", err)
		}
		// A classical CSR is refused by the ML-DSA-only profile; an ML-DSA CSR is
		// refused by the classical profile.
		classicalCSR, _ := pkiCSR(t)
		if _, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: classicalCSR, DNSNames: []string{"a.svc.example.com"}}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("classical key under the ML-DSA profile: %v", err)
		}
		if _, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "classic-web", CSRPEM: csrPEM, DNSNames: []string{"a.svc.example.com"}}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("ML-DSA key under the classical profile: %v", err)
		}
		// The leaf key and the path are separate dimensions: a classical leaf key
		// under the ML-DSA issuer is signed with ML-DSA but is not ML-DSA end to end.
		mixedLeaf, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "classic-web", CSRPEM: classicalCSR, DNSNames: []string{"mixed.svc.example.com"}})
		if err != nil {
			t.Fatalf("classical leaf under an ML-DSA issuer: %v", err)
		}
		if got := parseLeaf(t, mixedLeaf.Certificate); got.SignatureAlgorithm != x509.MLDSA65 || mixedLeaf.Certificate.KeyAlgorithm != "ecdsa-p256" {
			t.Fatalf("mixed leaf: key %s signature %v", mixedLeaf.Certificate.KeyAlgorithm, got.SignatureAlgorithm)
		}
		if err := pki.CheckPQPath(chainOf(t, mixedLeaf.Certificate, pqRoot.Issuer.CertificatePEM)); err == nil {
			t.Fatal("a classical leaf key under an ML-DSA issuer was labelled a PQ path")
		}
		// Tampered proof of possession and a profile-violating name.
		tamperedCSR := []byte(csrPEM)
		tamperedCSR[len(tamperedCSR)-40] ^= 0x01
		if _, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: string(tamperedCSR), DNSNames: []string{"a.svc.example.com"}}); err == nil {
			t.Fatal("a CSR with a damaged signature was accepted")
		}
		if _, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: csrPEM, DNSNames: []string{"a.evil.example.org"}}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("name outside the profile: %v", err)
		}
		if _, err := svc.IssueCertificate(ctx, service.LocalPrincipal(reader), env, request); err == nil {
			t.Fatal("a principal without issue-certificate issued a certificate")
		}

		// Display-once generated keys for the two TLS identities.
		serverCert, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", GenerateKey: true, KeyAlgorithm: "mldsa65", DNSNames: []string{"web.svc.example.com"}, CommonName: "web.svc.example.com"})
		if err != nil {
			t.Fatalf("generated ML-DSA key: %v", err)
		}
		clientCert, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", GenerateKey: true, KeyAlgorithm: "mldsa65", DNSNames: []string{"client.svc.example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		generated, err := pki.ParsePrivateKey(serverCert.PrivateKeyPEM)
		if err != nil || !pki.PublicKeysEqual(generated.Public(), parseLeaf(t, serverCert.Certificate).PublicKey) {
			t.Fatalf("generated key: %v", err)
		}
		keyBody := strings.Split(string(serverCert.PrivateKeyPEM), "\n")[1]
		for table, column := range map[string]string{"pki_certificates": "sans", "pki_issuers": "chain_pem", "audit_tenant_events": "payload", "audit_instance_events": "payload"} {
			if n := count("SELECT COUNT(*) FROM " + table + " WHERE CAST(" + column + " AS TEXT) LIKE '%" + keyBody[:24] + "%'"); n != 0 {
				t.Fatalf("generated private key material found in %s", table)
			}
		}
		if strings.Contains(chainShow(t, svc, human, env, serverCert.Certificate.ID), "PRIVATE KEY") {
			t.Fatal("a later read returned the private key")
		}

		// --- Real TLS: separate evidence for chain signatures and the group ---------
		serverPeer := tlsPeer{certPEM: serverCert.Certificate.CertificatePEM + serverCert.Certificate.ChainPEM, keyPEM: string(serverCert.PrivateKeyPEM)}
		clientPeer := tlsPeer{certPEM: clientCert.Certificate.CertificatePEM + clientCert.Certificate.ChainPEM, keyPEM: string(clientCert.PrivateKeyPEM)}
		rootPEM := pqRoot.Issuer.CertificatePEM

		strict := runMLDSATLS(t, serverPeer, clientPeer, rootPEM, tlsOptions{
			serverProfile: tlspolicy.ProfileStrict, clientProfile: tlspolicy.ProfileStrict,
			serverRequirePQChain: true, clientRequirePQChain: true,
		})
		if strict.clientErr != nil || strict.serverErr != nil || strict.appBytes != "ping" {
			t.Fatalf("strict mutual TLS: client=%v server=%v app=%q", strict.clientErr, strict.serverErr, strict.appBytes)
		}
		// 1. The negotiated key exchange, from the connection state.
		if strict.client.Version != tls.VersionTLS13 || strict.client.CurveID != tls.X25519MLKEM768 || strict.server.CurveID != tls.X25519MLKEM768 {
			t.Fatalf("key exchange: version %x client group %v server group %v", strict.client.Version, strict.client.CurveID, strict.server.CurveID)
		}
		// 2. The authentication, separately: the client offered ML-DSA-65, the
		// server's leaf is an ML-DSA-65 key (so CertificateVerify can only be
		// ML-DSA-65) and every certificate of both verified chains is signed
		// with ML-DSA-65.
		if !offeredMLDSA65(strict.offeredSignatureScheme) {
			t.Fatalf("the client did not offer ML-DSA-65: %v", strict.offeredSignatureScheme)
		}
		for name, state := range map[string]tls.ConnectionState{"client sees server": strict.client, "server sees client": strict.server} {
			if _, ok := state.PeerCertificates[0].PublicKey.(*mldsa.PublicKey); !ok {
				t.Fatalf("%s: leaf key is %T", name, state.PeerCertificates[0].PublicKey)
			}
			if err := pki.CheckPQPath(state.VerifiedChains[0]); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for i, cert := range state.VerifiedChains[0] {
				if cert.SignatureAlgorithm != x509.MLDSA65 {
					t.Errorf("%s: certificate %d signed with %v", name, i, cert.SignatureAlgorithm)
				}
			}
		}
		// The two dimensions are independent: the same ML-DSA identities work
		// over a classical group, and the strict group works with the same chain.
		classicalGroup := runMLDSATLS(t, serverPeer, clientPeer, rootPEM, tlsOptions{clientCurves: []tls.CurveID{tls.X25519}})
		if classicalGroup.clientErr != nil || classicalGroup.client.CurveID != tls.X25519 || classicalGroup.client.PeerCertificates[0].SignatureAlgorithm != x509.MLDSA65 {
			t.Fatalf("ML-DSA chain over X25519: %v group=%v", classicalGroup.clientErr, classicalGroup.client.CurveID)
		}
		if refused := runMLDSATLS(t, serverPeer, clientPeer, rootPEM, tlsOptions{serverProfile: tlspolicy.ProfileStrict, clientCurves: []tls.CurveID{tls.X25519}}); refused.clientErr == nil || refused.appBytes != "" {
			t.Fatalf("a classical-group client reached a strict server: %v app=%q", refused.clientErr, refused.appBytes)
		}

		// --- Relying parties that must refuse ---------------------------------------
		// (a) A root that did not sign this path cannot anchor it. This is only
		// UnknownAuthority: crypto/x509 would accept a classical root that really
		// signed an ML-DSA intermediate (see TestRealClassicalAnchorOverMLDSAPath).
		classicalOnly := runMLDSATLS(t, serverPeer, clientPeer, rootPEM, tlsOptions{clientRoots: poolOf(t, classicRoot.Issuer.CertificatePEM)})
		var unknownAuthority x509.UnknownAuthorityError
		if !errors.As(classicalOnly.clientErr, &unknownAuthority) || classicalOnly.appBytes != "" {
			t.Fatalf("classical trust anchor: %v app=%q", classicalOnly.clientErr, classicalOnly.appBytes)
		}
		// (b) A relying party that requires a PQ-authenticated chain refuses a
		// classical chain, even over the hybrid group, before any application byte.
		// A fully classical path needs a classical issuer under the classical root.
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "intermediate", Name: "classic-issuing", CommonName: "Classic Issuing", ParentName: "classic-root"}); err != nil {
			t.Fatal(err)
		}
		classicalPolicy := pkiWebPolicy()
		classicalPolicy.AllowedIssuers = []string{"classic-issuing"}
		if _, err := svc.CreateProfile(ctx, op, "classic-path", classicalPolicy); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.BindProfile(ctx, op, "classic-path", "org_a", "prj_a1", ""); err != nil {
			t.Fatal(err)
		}
		classicalServer, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "classic-path", GenerateKey: true, KeyAlgorithm: "ecdsa-p256", DNSNames: []string{"web.svc.example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		classicalClient, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "classic-path", GenerateKey: true, KeyAlgorithm: "ecdsa-p256", DNSNames: []string{"client.svc.example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		classicalRootsPEM := classicRoot.Issuer.CertificatePEM
		cServer := tlsPeer{certPEM: classicalServer.Certificate.CertificatePEM + classicalServer.Certificate.ChainPEM, keyPEM: string(classicalServer.PrivateKeyPEM)}
		cClient := tlsPeer{certPEM: classicalClient.Certificate.CertificatePEM + classicalClient.Certificate.ChainPEM, keyPEM: string(classicalClient.PrivateKeyPEM)}
		unchanged := runMLDSATLS(t, cServer, cClient, classicalRootsPEM, tlsOptions{serverProfile: tlspolicy.ProfileStrict, clientProfile: tlspolicy.ProfileStrict})
		if unchanged.clientErr != nil || unchanged.client.CurveID != tls.X25519MLKEM768 || unchanged.appBytes != "ping" {
			t.Fatalf("the classical path regressed under the strict group: %v", unchanged.clientErr)
		}
		pqRequired := runMLDSATLS(t, cServer, cClient, classicalRootsPEM, tlsOptions{
			serverProfile: tlspolicy.ProfileStrict, clientProfile: tlspolicy.ProfileStrict, clientRequirePQChain: true,
		})
		if pqRequired.clientErr == nil || pqRequired.appBytes != "" || !strings.Contains(pqRequired.clientErr.Error(), "post-quantum") {
			t.Fatalf("a PQ-requiring relying party accepted a classical chain: %v app=%q", pqRequired.clientErr, pqRequired.appBytes)
		}

		// --- Renewal -----------------------------------------------------------------
		clock.Advance(20 * time.Hour)
		renewed, err := svc.RenewCertificate(ctx, human, env, issued.Certificate.ID)
		if err != nil {
			t.Fatalf("renew: %v", err)
		}
		renewedLeaf := parseLeaf(t, renewed)
		if renewed.KeyFingerprint != issued.Certificate.KeyFingerprint || renewed.Serial == issued.Certificate.Serial || renewedLeaf.SignatureAlgorithm != x509.MLDSA65 {
			t.Fatalf("renewal: %+v", renewed)
		}
		if again, err := svc.RenewCertificate(ctx, human, env, issued.Certificate.ID); err != nil || again.ID != renewed.ID {
			t.Fatalf("second renewal must return the successor: %v", err)
		}

		// --- Revocation and the CRL signed by the issuer algorithm ------------------------
		if revoked, err := svc.RevokeCertificate(ctx, human, env, renewed.ID, "key-compromise"); err != nil || revoked.State != "revoked" {
			t.Fatalf("revoke: %v", err)
		}
		sweepPKI(t, svc)
		crlDER, err := svc.IssuerCRL(ctx, op, "pq-issuing", 0)
		if err != nil {
			t.Fatal(err)
		}
		crl, err := x509.ParseRevocationList(crlDER)
		if err != nil {
			t.Fatal(err)
		}
		issuerPEM := issuing.Issuer.CertificatePEM
		issuerCerts, _, _ := pki.ParseCertificates([]byte(issuerPEM))
		if crl.SignatureAlgorithm != x509.MLDSA65 || crl.CheckSignatureFrom(issuerCerts[0]) != nil {
			t.Fatalf("CRL: algorithm %v, signature %v", crl.SignatureAlgorithm, crl.CheckSignatureFrom(issuerCerts[0]))
		}
		if err := mldsa.Verify(issuerCerts[0].PublicKey.(*mldsa.PublicKey), crl.RawTBSRevocationList, crl.Signature, nil); err != nil {
			t.Fatalf("direct ML-DSA verification of the CRL: %v", err)
		}
		if code, ok := crlSerials(t, crlDER)[renewed.Serial]; !ok || code != 1 {
			t.Fatalf("the CRL lacks the revoked serial: %v", crlSerials(t, crlDER))
		}
		// A revoked identity is refused by a relying party that checks the CRL.
		if !serialRevoked(crl, renewedLeaf) {
			t.Fatal("the independent CRL check does not flag the revoked leaf")
		}

		// --- Overlap rotation, with the classical/PQ line fixed --------------------------
		shortLived, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: csrPEM, DNSNames: []string{"short.svc.example.com"}, TTL: 10 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		before := count(`SELECT COUNT(*) FROM pki_issuers`)
		for name, run := range map[string]func() error{
			"PQ issuer rotated to a classical key": func() error {
				_, err := svc.RotateIssuer(ctx, op, "pq-issuing", service.PKIIssuerRequest{KeyAlgorithm: "ecdsa-p256"})
				return err
			},
			"classical issuer rotated to ML-DSA": func() error {
				_, err := svc.RotateIssuer(ctx, op, "classic-issuing", service.PKIIssuerRequest{KeyAlgorithm: "mldsa65"})
				return err
			},
			"PQ issuer rotated to ML-DSA-87": func() error {
				_, err := svc.RotateIssuer(ctx, op, "pq-issuing", service.PKIIssuerRequest{KeyAlgorithm: "mldsa87"})
				return err
			},
		} {
			if err := run(); !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("%s: %v", name, err)
			}
		}
		if after := count(`SELECT COUNT(*) FROM pki_issuers`); after != before {
			t.Fatalf("a refused rotation left %d new rows", after-before)
		}
		rotated, err := svc.RotateIssuer(ctx, op, "pq-issuing", service.PKIIssuerRequest{})
		if err != nil {
			t.Fatalf("rotate: %v", err)
		}
		if rotated.Issuer.Version != 2 || rotated.Issuer.KeyAlgorithm != "mldsa65" || rotated.Issuer.State != "active" || rotated.Issuer.KeyFingerprint == issuing.Issuer.KeyFingerprint {
			t.Fatalf("rotated: %+v", rotated.Issuer)
		}
		fresh, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: csrPEM, DNSNames: []string{"new.svc.example.com"}})
		if err != nil || fresh.Certificate.IssuerVersion != 2 {
			t.Fatalf("issuance after rotation: %v %+v", err, fresh.Certificate)
		}
		if err := pki.CheckPQPath(chainOf(t, fresh.Certificate, rootPEM)); err != nil {
			t.Fatal(err)
		}
		// Prior-issuer CRL: version 1 is retiring but still signs its CRL, and a
		// revocation of one of its leaves lands there.
		if _, err := svc.RevokeCertificate(ctx, human, env, shortLived.Certificate.ID, "superseded"); err != nil {
			t.Fatal(err)
		}
		sweepPKI(t, svc)
		priorDER, err := svc.IssuerCRL(ctx, op, "pq-issuing", 1)
		if err != nil {
			t.Fatal(err)
		}
		prior, _ := x509.ParseRevocationList(priorDER)
		if _, ok := crlSerials(t, priorDER)[shortLived.Certificate.Serial]; !ok || prior.CheckSignatureFrom(issuerCerts[0]) != nil || prior.SignatureAlgorithm != x509.MLDSA65 {
			t.Fatal("the retiring ML-DSA issuer does not publish its CRL")
		}
		clock.Advance(4 * time.Hour)
		if _, err := svc.RetireIssuer(ctx, op, "pq-issuing", 1); !errors.Is(err, service.ErrPKIIssuerLive) {
			t.Fatalf("retiring a version with live leaves: %v", err)
		}

		// --- Instance DEK rotation and re-encryption of the sealed ML-DSA keys ---------
		sealedBefore := queryHexKey(t, db, rotated.Issuer.ID)
		kr := probeKeyring(t, db)
		rotation := &service.Rotation{DB: db, Keyring: kr, RootKey: probeRootSource{db: db}}
		if _, err := rotation.RotateDEK(ctx, op, service.DEKScope{Instance: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := (&service.Reencrypt{DB: db, Keyring: kr, ChunkPause: -1}).ReencryptInstance(ctx, op); err != nil {
			t.Fatalf("reencrypt with ML-DSA issuer keys present: %v", err)
		}
		if queryHexKey(t, db, rotated.Issuer.ID) == sealedBefore {
			t.Fatal("instance re-encryption left the ML-DSA issuer key under its old envelope")
		}
		afterDEK, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: csrPEM, DNSNames: []string{"dek.svc.example.com"}})
		if err != nil {
			t.Fatalf("the re-sealed ML-DSA key no longer signs: %v", err)
		}
		if err := pki.CheckPQPath(chainOf(t, afterDEK.Certificate, rootPEM)); err != nil || afterDEK.Certificate.IssuerVersion != 2 {
			t.Fatalf("re-sealed key signs under another identity: %v", err)
		}

		// --- A fresh service struct over the same *store.DB (NOT a process restart: ML-DSA restart is NOTRUN) ---
		restarted := pkiPQSvc(t, db, clock)
		restartedCert, err := restarted.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: csrPEM, DNSNames: []string{"restart.svc.example.com"}})
		if err != nil || restartedCert.Certificate.IssuerVersion != 2 {
			t.Fatalf("restarted service: %v", err)
		}

		// --- Concurrent issue, rotate and revoke -------------------------------------------------
		var wg sync.WaitGroup
		outcomes := make([]error, 6)
		views := make([]service.CertificateView, 6)
		// The rotation and the revocation start once one issuance has succeeded (or
		// all four have finished), so the race always has a success to check and the
		// remaining issuances can overlap the rotation. Overlap is observed in
		// instrumented runs (PostgreSQL showed "issuer changed while signing"
		// conflicts), not guaranteed or asserted by this test.
		firstDone := make(chan struct{})
		var once sync.Once
		var issueWG sync.WaitGroup
		for i := range 4 {
			wg.Add(1)
			issueWG.Add(1)
			go func() {
				defer wg.Done()
				defer issueWG.Done()
				res, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: csrPEM, DNSNames: []string{"race" + string(rune('a'+i)) + ".svc.example.com"}})
				views[i], outcomes[i] = res.Certificate, err
				if err == nil {
					once.Do(func() { close(firstDone) })
				}
			}()
		}
		go func() { issueWG.Wait(); once.Do(func() { close(firstDone) }) }()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-firstDone
			_, outcomes[4] = svc.RotateIssuer(ctx, op, "pq-issuing", service.PKIIssuerRequest{})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-firstDone
			_, outcomes[5] = restarted.RevokeCertificate(ctx, human, env, restartedCert.Certificate.ID, "superseded")
		}()
		wg.Wait()
		succeeded := 0
		for i := range 4 {
			if outcomes[i] == nil {
				succeeded++
			}
		}
		if succeeded == 0 {
			t.Fatalf("no concurrent issuance succeeded, so the race checked nothing: %v", outcomes[:4])
		}
		for i, err := range outcomes {
			switch {
			case err == nil:
			case errors.Is(err, service.ErrPKISigning), errors.Is(err, service.ErrPKIIssuerRace), errors.Is(err, service.ErrPKINoActiveIssuer):
			case strings.Contains(err.Error(), "SQLSTATE 40001"):
				// PostgreSQL SERIALIZABLE under real contention may exhaust the
				// bounded retries; the operation did not happen.
			default:
				t.Errorf("concurrent operation %d failed unexpectedly: %v", i, err)
			}
		}
		for i := range 4 {
			if outcomes[i] != nil {
				continue
			}
			if err := pki.CheckPQPath(chainOf(t, views[i], rootPEM)); err != nil {
				t.Errorf("race leaf %d: %v", i, err)
			}
		}
		if n := count(`SELECT COUNT(*) FROM pki_issuers WHERE name='pq-issuing' AND state='active'`); n != 1 {
			t.Fatalf("%d active versions of pq-issuing after the race, want exactly 1", n)
		}

		// --- Switching the algorithm off, with ML-DSA rows already present ------------------------------
		// The gate refuses NEW ML-DSA keys; it does not stop everything that touches an
		// existing ML-DSA issuer. What is refused, then what is NOT (registered in the
		// handoff as known limits; a restore is one way such rows exist with the gate off).
		if _, err := held.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: csrPEM, DNSNames: []string{"off.svc.example.com"}}); !errors.Is(err, service.ErrPKIAlgorithmHeld) {
			t.Fatalf("ML-DSA leaf with the algorithm switched off: %v", err)
		}
		if _, err := held.RotateIssuer(ctx, op, "pq-issuing", service.PKIIssuerRequest{}); !errors.Is(err, service.ErrPKIAlgorithmHeld) {
			t.Fatalf("rotation with the algorithm switched off: %v", err)
		}
		if _, err := held.PublishIssuerCRL(ctx, op, "pq-issuing", 1); err != nil {
			t.Fatalf("an existing ML-DSA issuer must keep publishing its CRL when the algorithm is switched off: %v", err)
		}
		// KNOWN LIMIT 1: a classical leaf is still newly issued, with an ML-DSA signature,
		// by an existing ML-DSA issuer that a classical profile names.
		offLeaf, err := held.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "classic-web", CSRPEM: classicalCSR, DNSNames: []string{"off-classical.svc.example.com"}})
		if err != nil || parseLeaf(t, offLeaf.Certificate).SignatureAlgorithm != x509.MLDSA65 {
			t.Fatalf("known limit 1 changed: classical leaf issued by an existing ML-DSA issuer with the gate off: %v", err)
		}
		// KNOWN LIMIT 2: ...and renewed the same way.
		clock.Advance(20 * time.Hour)
		offRenewed, err := held.RenewCertificate(ctx, human, env, offLeaf.Certificate.ID)
		if err != nil || parseLeaf(t, offRenewed).SignatureAlgorithm != x509.MLDSA65 {
			t.Fatalf("known limit 2 changed: classical leaf renewed by an existing ML-DSA issuer with the gate off: %v", err)
		}
		// KNOWN LIMIT 3: a classical profile may name an existing ML-DSA issuer.
		namingPolicy := pkiWebPolicy()
		namingPolicy.AllowedIssuers = []string{"pq-issuing"}
		if _, err := held.CreateProfile(ctx, op, "classic-names-pq", namingPolicy); err != nil {
			t.Fatalf("known limit 3 changed: classical profile naming an ML-DSA issuer with the gate off: %v", err)
		}
		// KNOWN LIMIT 4: a pending ML-DSA version created while the gate was on is
		// activated by install with the gate off (install has no algorithm check).
		pending, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "intermediate", Name: "pq-pending-off", CommonName: "PQ Pending", KeyAlgorithm: "mldsa65"})
		if err != nil {
			t.Fatal(err)
		}
		pendingCSR, err := pki.ParseCSR([]byte(pending.Issuer.CSRPEM))
		if err != nil {
			t.Fatal(err)
		}
		pendingDER, err := pki.SignIntermediate(offlineParent, pendingCSR.PublicKey, pki.Subject{CommonName: "PQ Pending"}, clock.Now(), 24*time.Hour*30)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := held.InstallIssuerCertificate(ctx, op, "pq-pending-off", pki.CertificatePEM(pendingDER), pki.CertificatePEM(offlineRootDER)); err != nil || got.State != "active" {
			t.Fatalf("known limit 4 changed: install with the gate off: %v %+v", err, got)
		}
		// KNOWN LIMIT 5: a stored mldsa65 profile is returned by the service as is.
		// Through the frozen API that value is outside the closed response enum.
		if shown, err := held.ShowProfile(ctx, op, "pq-web"); err != nil || len(shown.Policy.KeyAlgorithms) != 1 || shown.Policy.KeyAlgorithms[0] != pki.MLDSA65 {
			t.Fatalf("known limit 5 changed: stored ML-DSA profile read with the gate off: %v", err)
		}

		// --- Restore hold ------------------------------------------------------------------------
		execRaw(t, db, `UPDATE pki_issuers SET restore_hold=1`)
		if _, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "pq-web", CSRPEM: csrPEM, DNSNames: []string{"held.svc.example.com"}}); !errors.Is(err, service.ErrPKIIssuerHeld) {
			t.Fatalf("issuance from a held ML-DSA issuer: %v", err)
		}
		if _, err := svc.PublishIssuerCRL(ctx, op, "pq-issuing", 1); !errors.Is(err, service.ErrPKIIssuerHeld) {
			t.Fatalf("CRL publication from a held ML-DSA issuer: %v", err)
		}
		if _, err := svc.IssuerCRL(ctx, op, "pq-issuing", 1); err != nil {
			t.Fatalf("a held issuer's stored CRL must stay readable: %v", err)
		}
		if _, err := svc.ReleaseIssuerHold(ctx, op, "pq-issuing"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.PublishIssuerCRL(ctx, op, "pq-issuing", 1); err != nil {
			t.Fatalf("CRL publication after release: %v", err)
		}

		// --- Response sizes: the list returns up to 500 rows, each with its PEMs (P3) -------------------
		// The CLI reads at most 8 MiB of a response (internal/cli/client.go), so the
		// per-row size decides how many ML-DSA rows `cert list` can show. Measured here,
		// registered in the handoff; no bound was changed.
		rows, err := svc.ListCertificates(ctx, human, env)
		if err != nil {
			t.Fatal(err)
		}
		var pqRow, classicalRow int
		for _, row := range rows {
			size := len(row.CertificatePEM) + len(row.ChainPEM)
			switch {
			case row.KeyAlgorithm == "mldsa65" && size > pqRow:
				pqRow = size
			case row.KeyAlgorithm == "ecdsa-p256" && row.IssuerName == "pq-issuing" && size > classicalRow:
				classicalRow = size
			}
		}
		if pqRow == 0 || classicalRow == 0 {
			t.Fatalf("no sample rows: pq=%d classical-under-pq=%d", pqRow, classicalRow)
		}
		t.Logf("list row PEM bytes: ML-DSA leaf under ML-DSA issuer %d (8 MiB admits about %d rows); ECDSA leaf under ML-DSA issuer %d (about %d rows)",
			pqRow, (8<<20)/pqRow, classicalRow, (8<<20)/classicalRow)

		// --- Retirement: expire the leaves, retire the old version, the key is destroyed --------------------
		clock.Advance(100 * time.Hour)
		sweepPKI(t, svc)
		retired, err := svc.RetireIssuer(ctx, op, "pq-issuing", 1)
		if err != nil || retired.State != "retired" {
			t.Fatalf("retire the expired ML-DSA version: %v %+v", err, retired)
		}
		if n := count(`SELECT COUNT(*) FROM pki_issuers WHERE id='` + issuing.Issuer.ID + `' AND encrypted_private_key IS NULL AND dek_version IS NULL`); n != 1 {
			t.Fatal("retirement did not destroy the ML-DSA issuer key")
		}
		if _, err := svc.IssuerCRL(ctx, op, "pq-issuing", 1); err != nil {
			t.Fatalf("the retired version's last CRL must stay readable: %v", err)
		}

		// --- Compromise ----------------------------------------------------------------------------------
		active, _ := svc.ShowIssuer(ctx, op, "pq-issuing")
		last := active[len(active)-1]
		if compromised, err := svc.RevokeIssuer(ctx, op, "pq-issuing", last.Version); err != nil || compromised.State != "revoked" {
			t.Fatalf("revoke issuer: %v %+v", err, compromised)
		}
		if n := count(`SELECT COUNT(*) FROM pki_certificates WHERE issuer_id='` + last.ID + `' AND state IN ('issued','renewed')`); n != 0 {
			t.Fatalf("%d leaves of a compromised ML-DSA issuer are still live", n)
		}
		if n := count(`SELECT COUNT(*) FROM pki_issuers WHERE id='` + last.ID + `' AND encrypted_private_key IS NULL`); n != 1 {
			t.Fatal("issuer compromise did not destroy the ML-DSA key")
		}
	})
}

func bigSerial(t *testing.T) *big.Int {
	t.Helper()
	n, err := pki.NewSerial()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func chainShow(t *testing.T, svc *service.PKI, actor service.Actor, env domain.Scope, id string) string {
	t.Helper()
	view, err := svc.ShowCertificate(t.Context(), actor, env, id)
	if err != nil {
		t.Fatal(err)
	}
	return view.CertificatePEM + view.ChainPEM
}

func serialRevoked(crl *x509.RevocationList, cert *x509.Certificate) bool {
	for _, entry := range crl.RevokedCertificateEntries {
		if entry.SerialNumber.Cmp(cert.SerialNumber) == 0 {
			return true
		}
	}
	return false
}

func queryHexKey(t *testing.T, db *store.DB, issuerID string) string {
	t.Helper()
	q := `SELECT hex(encrypted_private_key) FROM pki_issuers WHERE id='` + issuerID + `'`
	if db.Engine() == store.EnginePostgres {
		q = `SELECT encode(encrypted_private_key,'hex') FROM pki_issuers WHERE id='` + issuerID + `'`
	}
	return queryString(t, db, q)
}

// newClassicalRoot is a self-signed ECDSA root, the real root of the truncation test.
func newClassicalRoot(t *testing.T, now time.Time) pki.Parent {
	t.Helper()
	key, err := pki.GenerateKey(pki.ECDSAP384)
	if err != nil {
		t.Fatal(err)
	}
	der, err := pki.CreateRoot(key, pki.Subject{CommonName: "Real Classical Root"}, now, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return pki.Parent{Certificate: cert, Signer: key}
}

// signCA signs a CA certificate for public under parent, the way an offline
// operator with a plain X.509 tool could (no Hikyo rule applied).
func signCA(t *testing.T, parent pki.Parent, public crypto.PublicKey, name string, pathLen int, now time.Time) []byte {
	t.Helper()
	serial, err := pki.NewSerial()
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: name},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true,
		MaxPathLen: pathLen, MaxPathLenZero: pathLen == 0,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent.Certificate, public, parent.Signer)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// newMLDSACA makes an ML-DSA CA under parent.
func newMLDSACA(t *testing.T, parent pki.Parent, name string, pathLen int, now time.Time) pki.Parent {
	t.Helper()
	key, err := pki.GenerateKey(pki.MLDSA65)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(signCA(t, parent, key.Public(), name, pathLen, now))
	if err != nil {
		t.Fatal(err)
	}
	return pki.Parent{Certificate: cert, Signer: key}
}
