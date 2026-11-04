package pki

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mldsaChain is root -> intermediate -> leaf, every key ML-DSA-65. The
// intermediate goes through the pending flow: its CSR is signed by the root
// as an offline operator would, and VerifyCA installs it.
type mldsaChain struct {
	root, issuing Parent
	leafKey       crypto.Signer
	leafDER       []byte
	leaf          *x509.Certificate
}

func newMLDSAChain(t testing.TB) mldsaChain { return newMLDSAChainAt(t, epoch) }

// newMLDSAChainAt builds the chain with every certificate dated from now.
func newMLDSAChainAt(t testing.TB, now time.Time) mldsaChain {
	t.Helper()
	rootKey, err := GenerateKey(MLDSA65)
	if err != nil {
		t.Fatal(err)
	}
	rootDER, err := CreateRoot(rootKey, Subject{CommonName: "Hikyo ML-DSA Test Root"}, now, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rootCert, err := VerifyCA(rootDER, rootKey.Public(), nil, now)
	if err != nil {
		t.Fatalf("an ML-DSA-65 root is not a usable CA: %v", err)
	}
	root := Parent{Certificate: rootCert, Signer: rootKey}

	issuerKey, err := GenerateKey(MLDSA65)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := CreateCSR(issuerKey, Subject{CommonName: "Hikyo ML-DSA Issuing CA"})
	if err != nil {
		t.Fatalf("ML-DSA CSR: %v", err)
	}
	csr, err := ParseCSR([]byte(CSRPEM(csrDER)))
	if err != nil {
		t.Fatalf("ML-DSA CSR does not parse: %v", err)
	}
	issuerDER, err := SignIntermediate(root, csr.PublicKey, Subject{CommonName: "Hikyo ML-DSA Issuing CA"}, now, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	issuerCert, err := VerifyCA(issuerDER, issuerKey.Public(), []*x509.Certificate{rootCert}, now)
	if err != nil {
		t.Fatalf("ML-DSA intermediate does not install: %v", err)
	}
	issuing := Parent{Certificate: issuerCert, Signer: issuerKey}

	leafKey, err := GenerateKey(MLDSA65)
	if err != nil {
		t.Fatal(err)
	}
	p := basePolicy()
	p.KeyAlgorithms = []KeyAlgorithm{MLDSA65}
	p.KeyUsages = []KeyUsage{UsageDigitalSignature}
	p = p.Normalize()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	resolved, err := p.Check(Request{DNSNames: []string{"web.svc.example.com"}, CommonName: "web.svc.example.com", PublicKey: leafKey.Public()})
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := NewSerial()
	notBefore, notAfter, err := LeafWindow(issuerCert, now, resolved.TTL)
	if err != nil {
		t.Fatal(err)
	}
	leafDER, err := SignLeaf(issuing, resolved, Leaf{
		Serial: serial, PublicKey: leafKey.Public(), NotBefore: notBefore, NotAfter: notAfter,
		KeyUsages: p.KeyUsages, ExtKeyUsages: p.ExtKeyUsages,
	})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}
	return mldsaChain{root: root, issuing: issuing, leafKey: leafKey, leafDER: leafDER, leaf: leaf}
}

func TestMLDSAKeyClassification(t *testing.T) {
	key, err := GenerateKey(MLDSA65)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := AlgorithmOf(key.Public()); err != nil || got != MLDSA65 {
		t.Fatalf("AlgorithmOf = %q, %v", got, err)
	}
	if parsed, err := ParseKeyAlgorithm("mldsa65"); err != nil || parsed != MLDSA65 {
		t.Fatalf("ParseKeyAlgorithm = %q, %v", parsed, err)
	}
	for _, spelling := range []string{"MLDSA65", "mldsa44", "mldsa87", "ml-dsa-65", "mldsa65 ", ""} {
		if _, err := ParseKeyAlgorithm(spelling); !errors.Is(err, ErrUnknownAlgorithm) {
			t.Errorf("ParseKeyAlgorithm(%q) = %v, want ErrUnknownAlgorithm", spelling, err)
		}
	}
	if err := CheckCAAlgorithm(MLDSA65); err != nil {
		t.Fatalf("mldsa65 must be a CA algorithm: %v", err)
	}
	// ML-DSA-44 and -87 parse in Go but DEC-02 selects only -65.
	for _, params := range []mldsa.Parameters{mldsa.MLDSA44(), mldsa.MLDSA87()} {
		other, err := mldsa.GenerateKey(params)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AlgorithmOf(other.Public()); !errors.Is(err, ErrUnsupportedKey) {
			t.Errorf("%s accepted: %v", params, err)
		}
		pkcs8, err := MarshalPrivateKey(other)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParsePrivateKey(PrivateKeyPEM(pkcs8)); !errors.Is(err, ErrUnsupportedKey) {
			t.Errorf("%s private key import accepted: %v", params, err)
		}
		csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "x"}}, other)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseCSR(csrDER); !errors.Is(err, ErrInvalidCSR) {
			t.Errorf("%s CSR accepted: %v", params, err)
		}
	}
}

// TestMLDSAPrivateKeyIsSeedOnly proves D2: Hikyo stores and accepts only the
// seed-only PKCS#8 form (RFC 9881 seed CHOICE), 32 bytes of key material.
func TestMLDSAPrivateKeyIsSeedOnly(t *testing.T) {
	key, err := GenerateKey(MLDSA65)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := MarshalPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Version    int
		Algorithm  pkix.AlgorithmIdentifier
		PrivateKey []byte
	}
	if _, err := asn1.Unmarshal(pkcs8, &info); err != nil {
		t.Fatal(err)
	}
	if !info.Algorithm.Algorithm.Equal(asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 18}) || len(info.Algorithm.Parameters.FullBytes) != 0 {
		t.Fatalf("algorithm identifier %v params %x", info.Algorithm.Algorithm, info.Algorithm.Parameters.FullBytes)
	}
	if want := append([]byte{0x80, 32}, key.(*mldsa.PrivateKey).Bytes()...); !bytes.Equal(info.PrivateKey, want) || len(info.PrivateKey) != 34 {
		t.Fatalf("private key field is %d bytes, want the 34-byte seed-only encoding", len(info.PrivateKey))
	}
	// Expanded (and seed-plus-expanded) forms are refused, not converted.
	for name, field := range map[string][]byte{
		"expanded":           append([]byte{0x04, 0x82, 0x0f, 0xc0}, make([]byte, 4032)...),
		"seed plus expanded": append([]byte{0x30, 0x82, 0x0f, 0xe8, 0x04, 0x20}, make([]byte, 32+4+4032)...),
		"short seed":         append([]byte{0x80, 31}, make([]byte, 31)...),
	} {
		forged := info
		forged.PrivateKey = field
		der, err := asn1.Marshal(forged)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParsePrivateKey(PrivateKeyPEM(der)); err == nil {
			t.Errorf("%s PKCS#8 accepted", name)
		}
		if _, err := UnmarshalPrivateKey(der); err == nil {
			t.Errorf("%s PKCS#8 unsealed", name)
		}
	}
	// The sealed round trip yields the same key and the same fingerprint.
	parsed, err := UnmarshalPrivateKey(pkcs8)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := KeyFingerprint(key.Public())
	b, _ := KeyFingerprint(parsed.Public())
	if a != b || !PublicKeysEqual(key.Public(), parsed.Public()) {
		t.Fatal("round trip changed the key")
	}
}

func TestMLDSAChainValidatesIndependently(t *testing.T) {
	c := newMLDSAChain(t)
	roots, inter := x509.NewCertPool(), x509.NewCertPool()
	roots.AddCert(c.root.Certificate)
	inter.AddCert(c.issuing.Certificate)
	chains, err := c.leaf.Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: inter, CurrentTime: epoch, DNSName: "web.svc.example.com",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	if err != nil {
		t.Fatalf("crypto/x509 does not verify the chain: %v", err)
	}
	if len(chains) != 1 || len(chains[0]) != 3 {
		t.Fatalf("unexpected chains %d", len(chains))
	}
	if err := CheckPath(chains[0]); err != nil {
		t.Fatalf("a pure ML-DSA-65 chain failed the path check: %v", err)
	}
	for i, cert := range chains[0] {
		if cert.SignatureAlgorithm != x509.MLDSA65 {
			t.Errorf("certificate %d signed with %v", i, cert.SignatureAlgorithm)
		}
	}
	// A second implementation path: verify each TBS with crypto/mldsa directly,
	// not through x509.
	for i, pair := range [][2]*x509.Certificate{{c.leaf, c.issuing.Certificate}, {c.issuing.Certificate, c.root.Certificate}, {c.root.Certificate, c.root.Certificate}} {
		subject, issuer := pair[0], pair[1]
		pub, ok := issuer.PublicKey.(*mldsa.PublicKey)
		if !ok {
			t.Fatalf("link %d: issuer key is %T", i, issuer.PublicKey)
		}
		if err := mldsa.Verify(pub, subject.RawTBSCertificate, subject.Signature, nil); err != nil {
			t.Errorf("link %d: %v", i, err)
		}
	}
	if c.leaf.KeyUsage != x509.KeyUsageDigitalSignature {
		t.Errorf("leaf key usage %v; ML-DSA admits digital signature only here", c.leaf.KeyUsage)
	}
	if !bytes.Equal(c.leaf.AuthorityKeyId, c.issuing.Certificate.SubjectKeyId) {
		t.Error("the leaf does not name its issuer's key identifier")
	}
	t.Logf("DER sizes: root %d, intermediate %d, leaf %d; PEM chain (leaf+intermediate+root) %d",
		len(c.root.Certificate.Raw), len(c.issuing.Certificate.Raw), len(c.leaf.Raw),
		len(CertificatesPEM([][]byte{c.leaf.Raw, c.issuing.Certificate.Raw, c.root.Certificate.Raw})))
}

func TestMLDSATamperedAndWrongKeyRefusals(t *testing.T) {
	c := newMLDSAChain(t)
	roots, inter := x509.NewCertPool(), x509.NewCertPool()
	roots.AddCert(c.root.Certificate)
	inter.AddCert(c.issuing.Certificate)
	opts := x509.VerifyOptions{Roots: roots, Intermediates: inter, CurrentTime: epoch, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}

	tampered := bytes.Clone(c.leaf.Raw)
	tampered[len(tampered)-3] ^= 0x01 // inside the signature BIT STRING
	leaf, err := x509.ParseCertificate(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(opts); err == nil {
		t.Fatal("a tampered leaf signature verified")
	}
	body := bytes.Clone(c.leaf.Raw)
	body[bytes.Index(body, []byte("web.svc.example.com"))] ^= 0x01
	if leaf, err := x509.ParseCertificate(body); err == nil {
		if _, err := leaf.Verify(opts); err == nil {
			t.Fatal("a tampered leaf body verified")
		}
	}

	// A different ML-DSA-65 key is not the issuer's key.
	other, _ := GenerateKey(MLDSA65)
	if _, err := VerifyCA(c.issuing.Certificate.Raw, other.Public(), []*x509.Certificate{c.root.Certificate}, epoch); err == nil {
		t.Fatal("VerifyCA accepted a certificate for a different key")
	}
	// An intermediate without its chain, or chained to an unrelated ML-DSA root, fails.
	if _, err := VerifyCA(c.issuing.Certificate.Raw, c.issuing.Signer.Public(), nil, epoch); err == nil {
		t.Fatal("VerifyCA accepted an intermediate without its chain")
	}
	strangerKey, _ := GenerateKey(MLDSA65)
	strangerDER, err := CreateRoot(strangerKey, Subject{CommonName: "Stranger"}, epoch, 24*time.Hour*30)
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := VerifyCA(strangerDER, strangerKey.Public(), nil, epoch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCA(c.issuing.Certificate.Raw, c.issuing.Signer.Public(), []*x509.Certificate{stranger}, epoch); err == nil {
		t.Fatal("VerifyCA accepted an unrelated ML-DSA root")
	}
	// A tampered self-signature on a PQ root.
	badRoot := bytes.Clone(c.root.Certificate.Raw)
	badRoot[len(badRoot)-3] ^= 0x01
	if _, err := VerifyCA(badRoot, c.root.Signer.Public(), nil, epoch); err == nil {
		t.Fatal("a root with a bad self-signature was installed")
	}

	// CSR: tampered signature and a public key that does not match the signer.
	csrKey, _ := GenerateKey(MLDSA65)
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "svc"}}, csrKey)
	if err != nil {
		t.Fatal(err)
	}
	bad := bytes.Clone(csrDER)
	bad[len(bad)-3] ^= 0x01
	if _, err := ParseCSR(bad); !errors.Is(err, ErrInvalidCSR) {
		t.Fatalf("tampered CSR signature: %v", err)
	}
	var req struct {
		Raw       asn1.RawContent
		Info      asn1.RawValue
		Algorithm pkix.AlgorithmIdentifier
		Signature asn1.BitString
	}
	if _, err := asn1.Unmarshal(csrDER, &req); err != nil {
		t.Fatal(err)
	}
	forged, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		t.Fatal(err)
	}
	// Swap in another key's SPKI under the original signature.
	otherSPKI, _ := x509.MarshalPKIXPublicKey(other.Public())
	swapped := bytes.Replace(csrDER, forged.RawSubjectPublicKeyInfo, otherSPKI, 1)
	if _, err := ParseCSR(swapped); !errors.Is(err, ErrInvalidCSR) {
		t.Fatalf("CSR with a swapped public key: %v", err)
	}
}

// TestClassicalRootOverMLDSAIntermediateIsNotPQ is DEC-19: the chain verifies
// as X.509, and Hikyo must not install or sign it as a PQ chain.
func TestClassicalRootOverMLDSAIntermediateIsNotPQ(t *testing.T) {
	classical := newRoot(t)
	pqKey, _ := GenerateKey(MLDSA65)

	if _, err := SignIntermediate(classical, pqKey.Public(), Subject{CommonName: "PQ under classical"}, epoch, 30*24*time.Hour); !errors.Is(err, ErrInvalidCA) {
		t.Fatalf("Hikyo signed an ML-DSA intermediate under a classical root: %v", err)
	}
	// Build it the way an offline operator with a classical root could.
	template := caTemplate(pkix.Name{CommonName: "PQ under classical"}, bigInt(77), epoch, 30*24*time.Hour, 0)
	der, err := x509.CreateCertificate(rand.Reader, template, classical.Certificate, pqKey.Public(), classical.Signer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCA(der, pqKey.Public(), []*x509.Certificate{classical.Certificate}, epoch); !errors.Is(err, ErrInvalidCA) {
		t.Fatalf("VerifyCA accepted a classical root over an ML-DSA intermediate: %v", err)
	}
	// Plain X.509 does accept it, which is exactly why Hikyo must refuse it.
	cert, _ := x509.ParseCertificate(der)
	roots := x509.NewCertPool()
	roots.AddCert(classical.Certificate)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: epoch, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		t.Fatalf("test premise: crypto/x509 should accept the mixed chain: %v", err)
	}
	if err := CheckPath([]*x509.Certificate{cert, classical.Certificate}); err == nil {
		t.Fatal("CheckPath labelled a mixed chain post-quantum")
	}

	// The reverse: a classical intermediate under an ML-DSA root.
	c := newMLDSAChain(t)
	classicalKey, _ := GenerateKey(ECDSAP256)
	if _, err := SignIntermediate(c.root, classicalKey.Public(), Subject{CommonName: "classical under PQ"}, epoch, 30*24*time.Hour); !errors.Is(err, ErrInvalidCA) {
		t.Fatalf("Hikyo signed a classical intermediate under an ML-DSA root: %v", err)
	}
	// And a classical intermediate under an ML-DSA root built by hand.
	template = caTemplate(pkix.Name{CommonName: "classical under PQ"}, bigInt(78), epoch, 30*24*time.Hour, 0)
	der, err = x509.CreateCertificate(rand.Reader, template, c.root.Certificate, classicalKey.Public(), c.root.Signer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCA(der, classicalKey.Public(), []*x509.Certificate{c.root.Certificate}, epoch); !errors.Is(err, ErrInvalidCA) {
		t.Fatalf("VerifyCA accepted a classical intermediate under an ML-DSA root: %v", err)
	}
	// A classical chain is unaffected, including a P-521 root outside the leaf set.
	p521, _ := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	rootDER, err := CreateRoot(p521, Subject{CommonName: "P-521 root"}, epoch, 24*time.Hour*30)
	if err != nil {
		t.Fatal(err)
	}
	rootCert, _ := x509.ParseCertificate(rootDER)
	key, _ := GenerateKey(ECDSAP384)
	template = caTemplate(pkix.Name{CommonName: "intermediate"}, bigInt(79), epoch, 24*time.Hour*10, 0)
	der, _ = x509.CreateCertificate(rand.Reader, template, rootCert, key.Public(), p521)
	if _, err := VerifyCA(der, key.Public(), []*x509.Certificate{rootCert}, epoch); err != nil {
		t.Fatalf("a classical chain behind a P-521 root regressed: %v", err)
	}
}

func TestMLDSALeafUnderClassicalIssuerIsAllowedButNotAPQChain(t *testing.T) {
	// The leaf key and the issuing path are separate dimensions (ADR D5, D3).
	classical := newRoot(t)
	leafKey, _ := GenerateKey(MLDSA65)
	p := basePolicy()
	p.KeyAlgorithms = []KeyAlgorithm{MLDSA65}
	p.KeyUsages = []KeyUsage{UsageDigitalSignature}
	p = p.Normalize()
	resolved, err := p.Check(Request{DNSNames: []string{"api.example.com"}, PublicKey: leafKey.Public()})
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := NewSerial()
	nb, na, _ := LeafWindow(classical.Certificate, epoch, resolved.TTL)
	der, err := SignLeaf(classical, resolved, Leaf{Serial: serial, PublicKey: leafKey.Public(), NotBefore: nb, NotAfter: na, KeyUsages: p.KeyUsages, ExtKeyUsages: p.ExtKeyUsages})
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	if leaf.SignatureAlgorithm == x509.MLDSA65 {
		t.Fatal("a classical issuer produced an ML-DSA signature")
	}
	if err := CheckPath([]*x509.Certificate{leaf, classical.Certificate}); err == nil {
		t.Fatal("CheckPath accepted an ML-DSA leaf under a classical issuer as a PQ path")
	}
	// A profile that lists only classical key algorithms refuses an ML-DSA key.
	classicalOnly := basePolicy()
	if _, err := classicalOnly.Check(Request{DNSNames: []string{"api.example.com"}, PublicKey: leafKey.Public()}); !errors.Is(err, ErrRefused) {
		t.Fatalf("a classical profile admitted an ML-DSA key: %v", err)
	}
	// Adding the algorithm is widening.
	widened := classicalOnly
	widened.KeyAlgorithms = append(append([]KeyAlgorithm{}, classicalOnly.KeyAlgorithms...), MLDSA65)
	if err := Narrows(classicalOnly, widened); !errors.Is(err, ErrWidening) {
		t.Fatalf("adding mldsa65 to a profile is not a narrowing: %v", err)
	}
}

func TestMLDSACRLSignedAndVerifiedIndependently(t *testing.T) {
	c := newMLDSAChain(t)
	entries := []RevokedEntry{{Serial: c.leaf.SerialNumber, RevokedAt: epoch, Reason: ReasonKeyCompromise}}
	der, err := CreateCRL(c.issuing, entries, CRLNumber(NextCRLNumber(0, epoch)), epoch)
	if err != nil {
		t.Fatal(err)
	}
	crl, err := x509.ParseRevocationList(der)
	if err != nil {
		t.Fatal(err)
	}
	if crl.SignatureAlgorithm != x509.MLDSA65 {
		t.Fatalf("CRL signed with %v; D7 says the issuer algorithm", crl.SignatureAlgorithm)
	}
	if err := crl.CheckSignatureFrom(c.issuing.Certificate); err != nil {
		t.Fatalf("CRL signature: %v", err)
	}
	pub := c.issuing.Certificate.PublicKey.(*mldsa.PublicKey)
	if err := mldsa.Verify(pub, crl.RawTBSRevocationList, crl.Signature, nil); err != nil {
		t.Fatalf("direct ML-DSA verification of the CRL: %v", err)
	}
	if err := crl.CheckSignatureFrom(c.root.Certificate); err == nil {
		t.Fatal("the CRL verified under the root key")
	}
	tampered := bytes.Clone(der)
	tampered[len(tampered)-3] ^= 0x01
	if bad, err := x509.ParseRevocationList(tampered); err == nil && bad.CheckSignatureFrom(c.issuing.Certificate) == nil {
		t.Fatal("a tampered CRL verified")
	}
	if len(crl.RevokedCertificateEntries) != 1 || crl.RevokedCertificateEntries[0].ReasonCode != 1 {
		t.Fatalf("entries %+v", crl.RevokedCertificateEntries)
	}
}

func TestMLDSAPKCS8PEMIssuerImportRoundTrip(t *testing.T) {
	key, _ := GenerateKey(MLDSA65)
	pkcs8, _ := MarshalPrivateKey(key)
	pem := PrivateKeyPEM(pkcs8)
	if len(pem) > 16384 {
		t.Fatalf("an ML-DSA-65 key PEM is %d bytes; the 16384 cap would refuse it", len(pem))
	}
	t.Logf("PKCS#8 DER %d bytes, PEM %d bytes", len(pkcs8), len(pem))
	if _, err := ParsePrivateKey(pem); err != nil {
		t.Fatal(err)
	}
	// PKCS#1 and SEC1 blocks cannot carry ML-DSA; they stay refused.
	if _, err := ParsePrivateKey(bytes.ReplaceAll(pem, []byte("PRIVATE KEY"), []byte("EC PRIVATE KEY"))); err == nil {
		t.Fatal("an ML-DSA key parsed as SEC1")
	}
}

// TestMLDSARelyingPartyWithoutMLDSARefuses runs a real TLS client built
// against the Go FIPS 140-3 module v1.0.0, which has no ML-DSA, so it is a
// maintained relying party that predates post-quantum signatures. It must
// refuse the ML-DSA chain and send no application byte; the same program built
// normally is the positive control.
func TestMLDSARelyingPartyWithoutMLDSARefuses(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool on PATH")
	}
	goroot, err := exec.CommandContext(t.Context(), goTool, "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	if snapshots, _ := filepath.Glob(filepath.Join(strings.TrimSpace(string(goroot)), "lib", "fips140", "v1.0.0*.zip")); len(snapshots) == 0 {
		t.Skip("this toolchain does not ship the FIPS 140-3 v1.0.0 module snapshot")
	}
	dir := t.TempDir()
	build := func(name string, fips bool) string {
		t.Helper()
		out := filepath.Join(dir, name)
		cmd := exec.CommandContext(t.Context(), goTool, "build", "-o", out, "./testdata/fipsclient")
		cmd.Env = os.Environ()
		if fips {
			cmd.Env = append(cmd.Env, "GOFIPS140=v1.0.0")
		}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, output)
		}
		return out
	}
	fipsClient, plainClient := build("fips-client", true), build("plain-client", false)

	rootFile := filepath.Join(dir, "root.pem")
	// Dated from the real clock: the client binary uses the system time.
	live := newMLDSAChainAt(t, time.Now().UTC())
	realPair, err := tls.X509KeyPair([]byte(CertificatesPEM([][]byte{live.leaf.Raw, live.issuing.Certificate.Raw})), mustPrivateKeyPEM(t, live.leafKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootFile, []byte(CertificatePEM(live.root.Certificate.Raw)), 0o600); err != nil {
		t.Fatal(err)
	}
	addr, appBytes, stop := serveTLS(t, realPair)
	defer stop()
	run := func(binary string) (string, error) {
		out, err := exec.CommandContext(t.Context(), binary, addr, "web.svc.example.com", rootFile).CombinedOutput()
		return string(out), err
	}
	if out, err := run(plainClient); err != nil || !strings.Contains(out, "ACCEPTED: pong") {
		t.Fatalf("control: a client with ML-DSA must accept the chain: %v %s", err, out)
	}
	if appBytes() != "ping" {
		t.Fatalf("control: server saw %q", appBytes())
	}
	out, err := run(fipsClient)
	if err == nil || !strings.Contains(out, "REFUSED") {
		t.Fatalf("a relying party without ML-DSA accepted the chain: %v %s", err, out)
	}
	if appBytes() != "ping" { // unchanged since the control: nothing new arrived
		t.Fatalf("the refusing client leaked application bytes: server saw %q", appBytes())
	}
	t.Logf("relying party without ML-DSA: %s", strings.TrimSpace(out))
}

func mustPrivateKeyPEM(t testing.TB, key crypto.Signer) []byte {
	t.Helper()
	pkcs8, err := MarshalPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return PrivateKeyPEM(pkcs8)
}

// TestMLDSAAnchorMustBeARealSelfSignedRoot covers the two ways a chain can end
// early or end badly: a list that stops below the real (classical) root so an
// ML-DSA intermediate is mistaken for the anchor, and a trust anchor whose
// self-signature was damaged.
func TestMLDSAAnchorMustBeARealSelfSignedRoot(t *testing.T) {
	now := epoch
	classical := newRoot(t) // the real root
	mk := func(parent Parent, name string, pathLen int) (Parent, []byte) {
		key, err := GenerateKey(MLDSA65)
		if err != nil {
			t.Fatal(err)
		}
		template := caTemplate(pkix.Name{CommonName: name}, bigInt(int64(100+pathLen)), now, 30*24*time.Hour, pathLen)
		der, err := x509.CreateCertificate(rand.Reader, template, parent.Certificate, key.Public(), parent.Signer)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return Parent{Certificate: cert, Signer: key}, der
	}
	// Real path: classical root R, ML-DSA Y, ML-DSA X, issuer I (all ML-DSA below R).
	y, _ := mk(classical, "Y", 2)
	x, _ := mk(y, "X", 1)
	issuerKey, _ := GenerateKey(MLDSA65)
	template := caTemplate(pkix.Name{CommonName: "I"}, bigInt(7), now, 24*time.Hour*10, 0)
	issuerDER, err := x509.CreateCertificate(rand.Reader, template, x.Certificate, issuerKey.Public(), x.Signer)
	if err != nil {
		t.Fatal(err)
	}

	// Attack 1: present only [X] as if it were the root. Every certificate in the
	// list is ML-DSA-65, so CheckPath alone would pass.
	if err := CheckPath([]*x509.Certificate{mustParse(t, issuerDER), x.Certificate}); err != nil {
		t.Fatalf("test premise: the truncated list should look pure: %v", err)
	}
	for name, chain := range map[string][]*x509.Certificate{
		"stops at X":         {x.Certificate},
		"stops at Y":         {x.Certificate, y.Certificate},
		"classical root too": {x.Certificate, y.Certificate, classical.Certificate},
	} {
		_, err := VerifyCA(issuerDER, issuerKey.Public(), chain, now)
		if name == "classical root too" {
			// The honest full path contains a classical root: a mixed path.
			if !errors.Is(err, ErrInvalidCA) {
				t.Errorf("%s: want ErrInvalidCA, got %v", name, err)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidCA) {
			t.Errorf("%s: a truncated ML-DSA chain was installed: %v", name, err)
		}
	}

	// Attack 2: the real ML-DSA root with a tampered self-signature, supplied as
	// the chain anchor. x509 Verify skips anchor signatures.
	c := newMLDSAChainAt(t, now)
	damaged := bytes.Clone(c.root.Certificate.Raw)
	damaged[len(damaged)-3] ^= 0x01
	damagedRoot, err := x509.ParseCertificate(damaged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCA(c.issuing.Certificate.Raw, c.issuing.Signer.Public(), []*x509.Certificate{damagedRoot}, now); !errors.Is(err, ErrInvalidCA) {
		t.Fatalf("an intermediate was installed under a root with a damaged self-signature: %v", err)
	}
	// Control: the intact chain installs.
	if _, err := VerifyCA(c.issuing.Certificate.Raw, c.issuing.Signer.Public(), []*x509.Certificate{c.root.Certificate}, now); err != nil {
		t.Fatalf("control: %v", err)
	}
	// CheckPQPath is the same rule for a relying party's verified chain.
	if err := CheckPQPath([]*x509.Certificate{c.leaf, c.issuing.Certificate}); err == nil {
		t.Fatal("a verified chain without its anchor was labelled a PQ path")
	}
	if err := CheckPQPath([]*x509.Certificate{c.leaf, c.issuing.Certificate, c.root.Certificate}); err != nil {
		t.Fatalf("control: %v", err)
	}
}

func mustParse(t testing.TB, der []byte) *x509.Certificate {
	t.Helper()
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// serveTLS accepts TLS 1.3 connections on loopback, reads four application bytes
// after a successful handshake, answers "pong", and reports every byte it read.
func serveTLS(t testing.TB, pair tls.Certificate) (addr string, appBytes func() string, stop func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got string
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
				conn := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13})
				if conn.Handshake() != nil {
					return
				}
				buf := make([]byte, 4)
				n, _ := io.ReadFull(conn, buf)
				mu.Lock()
				got += string(buf[:n])
				mu.Unlock()
				_, _ = conn.Write([]byte("pong"))
			}()
		}
	}()
	return listener.Addr().String(), func() string { mu.Lock(); defer mu.Unlock(); return got }, func() { _ = listener.Close() }
}

// TestRealClassicalAnchorOverMLDSAPath is the relying-party view of DEC-19. The
// anchor here is a classical root that really signed an ML-DSA intermediate,
// which signed an ML-DSA leaf. Go's crypto/x509 and crypto/tls accept that path
// (they check each link, not that the anchor is post-quantum), so a plain client
// that trusts the classical root authenticates and talks. Only a VerifyConnection
// walk with CheckPQPath refuses it, before any application byte. This is the test
// harness for that walk, not a production hop (no product relying party performs
// it yet).
func TestRealClassicalAnchorOverMLDSAPath(t *testing.T) {
	now := time.Now().UTC()
	rootKey, _ := GenerateKey(ECDSAP384)
	rootDER, err := CreateRoot(rootKey, Subject{CommonName: "Classical Anchor"}, now, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rootCert := mustParse(t, rootDER)
	issuerKey, _ := GenerateKey(MLDSA65)
	template := caTemplate(pkix.Name{CommonName: "PQ Issuing under classical"}, bigInt(11), now, 30*24*time.Hour, 0)
	issuerDER, err := x509.CreateCertificate(rand.Reader, template, rootCert, issuerKey.Public(), rootKey)
	if err != nil {
		t.Fatal(err)
	}
	issuerCert := mustParse(t, issuerDER)
	leafKey, _ := GenerateKey(MLDSA65)
	policy := basePolicy()
	policy.KeyAlgorithms = []KeyAlgorithm{MLDSA65}
	policy.KeyUsages = []KeyUsage{UsageDigitalSignature}
	policy = policy.Normalize()
	resolved, err := policy.Check(Request{DNSNames: []string{"web.svc.example.com"}, PublicKey: leafKey.Public()})
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := NewSerial()
	nb, na, _ := LeafWindow(issuerCert, now, resolved.TTL)
	leafDER, err := SignLeaf(Parent{Certificate: issuerCert, Signer: issuerKey}, resolved, Leaf{Serial: serial, PublicKey: leafKey.Public(), NotBefore: nb, NotAfter: na, KeyUsages: policy.KeyUsages, ExtKeyUsages: policy.ExtKeyUsages})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair([]byte(CertificatesPEM([][]byte{leafDER, issuerDER})), mustPrivateKeyPEM(t, leafKey))
	if err != nil {
		t.Fatal(err)
	}
	addr, appBytes, stop := serveTLS(t, pair)
	defer stop()
	roots := x509.NewCertPool()
	roots.AddCert(rootCert)
	dial := func(config *tls.Config) error {
		config.RootCAs, config.ServerName, config.MinVersion = roots, "web.svc.example.com", tls.VersionTLS13
		conn, err := tls.Dial("tcp", addr, config)
		if err != nil {
			return err
		}
		defer conn.Close()
		if _, err := conn.Write([]byte("ping")); err != nil {
			return err
		}
		reply := make([]byte, 4)
		_, err = io.ReadFull(conn, reply)
		return err
	}
	// 1. A plain client that trusts the classical root accepts the path.
	if err := dial(&tls.Config{}); err != nil {
		t.Fatalf("crypto/tls refused a valid path under a classical root: %v", err)
	}
	if appBytes() != "ping" {
		t.Fatalf("control: server saw %q", appBytes())
	}
	// 2. A PQ-requiring walk refuses it before any application byte.
	err = dial(&tls.Config{VerifyConnection: func(cs tls.ConnectionState) error { return CheckPQPath(cs.VerifiedChains[0]) }})
	if err == nil || !strings.Contains(err.Error(), "post-quantum") && !strings.Contains(err.Error(), "mixes") {
		t.Fatalf("a PQ-requiring relying party accepted a real classical anchor: %v", err)
	}
	if appBytes() != "ping" {
		t.Fatalf("the refusing client sent application bytes: server saw %q", appBytes())
	}
}
