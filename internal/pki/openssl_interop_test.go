package pki

import (
	"bytes"
	"crypto/x509"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMLDSAOpenSSLInterop is the second-implementation check (DEC-09): an
// offline ML-DSA-65 root made and used only by OpenSSL signs a Hikyo-made
// intermediate CSR, Hikyo installs it, and OpenSSL then validates Hikyo's
// leaf, its CRL, and an OpenSSL-made leaf CSR. It is the exact flow in
// docs/operations/pki-ml-dsa.md. It skips when no OpenSSL with ML-DSA-65 is on
// PATH (OpenSSL 3.5 or later), so hosted CI without one does not run it.
func TestMLDSAOpenSSLInterop(t *testing.T) {
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("no openssl on PATH")
	}
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), openssl, args...)
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("openssl %s: %v\n%s", strings.Join(args, " "), err, out.String())
		}
		return out.String()
	}
	if listing := exec.CommandContext(t.Context(), openssl, "list", "-signature-algorithms"); func() bool {
		out, err := listing.CombinedOutput()
		return err != nil || !strings.Contains(string(out), "ML-DSA-65")
	}() {
		t.Skip("this openssl has no ML-DSA-65")
	}
	version := strings.TrimSpace(run(".", "version"))
	t.Logf("second implementation: %s", version)

	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("root.cnf", "[req]\ndistinguished_name=dn\nx509_extensions=v3_root\nprompt=no\n[dn]\nCN=Offline PQ Root\n[v3_root]\nbasicConstraints=critical,CA:TRUE,pathlen:1\nkeyUsage=critical,keyCertSign,cRLSign\nsubjectKeyIdentifier=hash\n")
	write("issuing.cnf", "basicConstraints=critical,CA:TRUE,pathlen:0\nkeyUsage=critical,keyCertSign,cRLSign\nsubjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid\n")

	// The offline root lives only in OpenSSL.
	run(dir, "genpkey", "-algorithm", "ML-DSA-65", "-out", "root.key")
	run(dir, "req", "-x509", "-new", "-key", "root.key", "-days", "3650", "-config", "root.cnf", "-out", "root.pem")

	// Hikyo generates the intermediate key and a CSR; the root signs it offline.
	issuerKey, err := GenerateKey(MLDSA65)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := CreateCSR(issuerKey, Subject{CommonName: "Hikyo PQ Issuing CA"})
	if err != nil {
		t.Fatal(err)
	}
	write("issuing.csr", CSRPEM(csrDER))
	run(dir, "req", "-in", "issuing.csr", "-noout", "-verify")
	run(dir, "x509", "-req", "-in", "issuing.csr", "-CA", "root.pem", "-CAkey", "root.key", "-CAcreateserial",
		"-days", "365", "-extfile", "issuing.cnf", "-out", "issuing.pem")

	rootPEM, _ := os.ReadFile(filepath.Join(dir, "root.pem"))
	issuingPEM, _ := os.ReadFile(filepath.Join(dir, "issuing.pem"))
	roots, _, err := ParseCertificates(rootPEM)
	if err != nil || len(roots) != 1 {
		t.Fatalf("root: %v", err)
	}
	_, issuingDERs, err := ParseCertificates(issuingPEM)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	issuing, err := VerifyCA(issuingDERs[0], issuerKey.Public(), roots, now)
	if err != nil {
		t.Fatalf("Hikyo does not accept the OpenSSL-signed intermediate: %v", err)
	}
	if err := CheckPQPath([]*x509.Certificate{issuing, roots[0]}); err != nil {
		t.Fatal(err)
	}

	// An OpenSSL-made leaf CSR (its own key) goes through Hikyo's CSR parser.
	run(dir, "req", "-new", "-newkey", "ML-DSA-65", "-nodes", "-keyout", "leaf.key", "-subj", "/CN=web.svc.example.com", "-out", "leaf.csr")
	csrPEM, _ := os.ReadFile(filepath.Join(dir, "leaf.csr"))
	csr, err := ParseCSR(csrPEM)
	if err != nil {
		t.Fatalf("Hikyo cannot read an OpenSSL ML-DSA-65 CSR: %v", err)
	}
	policy := basePolicy()
	policy.KeyAlgorithms = []KeyAlgorithm{MLDSA65}
	policy.KeyUsages = []KeyUsage{UsageDigitalSignature}
	policy = policy.Normalize()
	resolved, err := policy.Check(Request{DNSNames: []string{"web.svc.example.com"}, PublicKey: csr.PublicKey})
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := NewSerial()
	notBefore, notAfter, err := LeafWindow(issuing, now, resolved.TTL)
	if err != nil {
		t.Fatal(err)
	}
	issuer := Parent{Certificate: issuing, Signer: issuerKey}
	leafDER, err := SignLeaf(issuer, resolved, Leaf{Serial: serial, PublicKey: csr.PublicKey, NotBefore: notBefore, NotAfter: notAfter, KeyUsages: policy.KeyUsages, ExtKeyUsages: policy.ExtKeyUsages})
	if err != nil {
		t.Fatal(err)
	}
	write("leaf.pem", CertificatePEM(leafDER))

	// OpenSSL validates the whole path Hikyo produced.
	if out := run(dir, "verify", "-CAfile", "root.pem", "-untrusted", "issuing.pem", "-purpose", "sslserver", "-verify_hostname", "web.svc.example.com", "leaf.pem"); !strings.Contains(out, "OK") {
		t.Fatalf("openssl verify: %s", out)
	}
	// A tampered leaf does not validate.
	tampered := bytes.Clone(leafDER)
	tampered[len(tampered)-3] ^= 0x01
	write("tampered.pem", CertificatePEM(tampered))
	if out, err := exec.CommandContext(t.Context(), openssl, "verify", "-CAfile", filepath.Join(dir, "root.pem"), "-untrusted", filepath.Join(dir, "issuing.pem"), filepath.Join(dir, "tampered.pem")).CombinedOutput(); err == nil {
		t.Fatalf("openssl accepted a tampered ML-DSA leaf: %s", out)
	}

	// The CRL, signed by the issuer algorithm, verifies and revokes in OpenSSL.
	crlDER, err := CreateCRL(issuer, []RevokedEntry{{Serial: serial, RevokedAt: now, Reason: ReasonKeyCompromise}}, CRLNumber(NextCRLNumber(0, now)), now)
	if err != nil {
		t.Fatal(err)
	}
	write("crl.pem", CRLPEM(crlDER))
	run(dir, "crl", "-in", "crl.pem", "-CAfile", "issuing.pem", "-noout", "-verify")
	cmd := exec.CommandContext(t.Context(), openssl, "verify", "-crl_check", "-CAfile", filepath.Join(dir, "root.pem"),
		"-untrusted", filepath.Join(dir, "issuing.pem"), "-CRLfile", filepath.Join(dir, "crl.pem"), filepath.Join(dir, "leaf.pem"))
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "revoked") {
		t.Fatalf("openssl did not honour the ML-DSA CRL: %v %s", err, out)
	}

	// Operator pitfall that D2 makes visible: OpenSSL writes seed plus expanded
	// PKCS#8 by default, which Hikyo refuses; seed-only is the importable form.
	run(dir, "genpkey", "-algorithm", "ML-DSA-65", "-out", "default.key")
	run(dir, "genpkey", "-algorithm", "ML-DSA-65", "-provparam", "ml-dsa.output_formats=seed-only", "-out", "seedonly.key")
	defaultKey, _ := os.ReadFile(filepath.Join(dir, "default.key"))
	seedOnly, _ := os.ReadFile(filepath.Join(dir, "seedonly.key"))
	if _, err := ParsePrivateKey(defaultKey); err == nil {
		t.Error("an OpenSSL default (seed plus expanded) ML-DSA key was importable; D2 says seed-only")
	}
	if _, err := ParsePrivateKey(seedOnly); err != nil {
		t.Errorf("an OpenSSL seed-only ML-DSA key is not importable: %v", err)
	}
}
