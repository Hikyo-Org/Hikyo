package pki

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// benchChain is root -> intermediate -> leaf with one algorithm throughout,
// for the DEC-17 baseline. No threshold is implied by anything measured here.
type benchChain struct {
	algorithm            KeyAlgorithm
	root, issuing        Parent
	leafKey              crypto.Signer
	leaf                 *x509.Certificate
	csrDER, issuerCSRDER []byte
}

func newBenchChain(tb testing.TB, algorithm KeyAlgorithm) benchChain {
	tb.Helper()
	now := time.Now().UTC()
	rootKey, _ := GenerateKey(algorithm)
	rootDER, err := CreateRoot(rootKey, Subject{CommonName: "Bench Root"}, now, 365*24*time.Hour)
	if err != nil {
		tb.Fatal(err)
	}
	rootCert, _ := x509.ParseCertificate(rootDER)
	root := Parent{Certificate: rootCert, Signer: rootKey}
	issuerKey, _ := GenerateKey(algorithm)
	issuerCSR, _ := CreateCSR(issuerKey, Subject{CommonName: "Bench Issuing"})
	issuerDER, err := SignIntermediate(root, issuerKey.Public(), Subject{CommonName: "Bench Issuing"}, now, 90*24*time.Hour)
	if err != nil {
		tb.Fatal(err)
	}
	issuerCert, _ := x509.ParseCertificate(issuerDER)
	issuing := Parent{Certificate: issuerCert, Signer: issuerKey}
	leafKey, _ := GenerateKey(algorithm)
	leafCSR, _ := CreateCSR(leafKey, Subject{CommonName: "web.svc.example.com"})
	policy := basePolicy()
	policy.KeyAlgorithms = []KeyAlgorithm{algorithm}
	policy.KeyUsages = []KeyUsage{UsageDigitalSignature}
	policy = policy.Normalize()
	resolved, err := policy.Check(Request{DNSNames: []string{"web.svc.example.com"}, PublicKey: leafKey.Public()})
	if err != nil {
		tb.Fatal(err)
	}
	serial, _ := NewSerial()
	nb, na, _ := LeafWindow(issuerCert, now, resolved.TTL)
	leafDER, err := SignLeaf(issuing, resolved, Leaf{Serial: serial, PublicKey: leafKey.Public(), NotBefore: nb, NotAfter: na, KeyUsages: policy.KeyUsages, ExtKeyUsages: policy.ExtKeyUsages})
	if err != nil {
		tb.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(leafDER)
	return benchChain{algorithm: algorithm, root: root, issuing: issuing, leafKey: leafKey, leaf: leaf, csrDER: leafCSR, issuerCSRDER: issuerCSR}
}

func (c benchChain) crl(tb testing.TB, entries int) []byte {
	tb.Helper()
	list := make([]RevokedEntry, entries)
	for i := range list {
		serial, _ := NewSerial()
		list[i] = RevokedEntry{Serial: serial, RevokedAt: time.Now().UTC(), Reason: ReasonKeyCompromise}
	}
	der, err := CreateCRL(c.issuing, list, CRLNumber(1), time.Now().UTC())
	if err != nil {
		tb.Fatal(err)
	}
	return der
}

type countingConn struct {
	net.Conn
	read, written *atomic.Int64
}

func (c countingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.read.Add(int64(n))
	return n, err
}

func (c countingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.written.Add(int64(n))
	return n, err
}

// handshake runs one TLS 1.3 handshake over an in-memory pipe with the hybrid
// group and returns the bytes the client sent and received.
func (c benchChain) handshake(tb testing.TB) (sent, received int64) {
	tb.Helper()
	pair := tls.Certificate{
		Certificate: [][]byte{c.leaf.Raw, c.issuing.Certificate.Raw},
		PrivateKey:  c.leafKey,
	}
	roots := x509.NewCertPool()
	roots.AddCert(c.root.Certificate)
	serverRaw, clientRaw := net.Pipe()
	var read, written atomic.Int64
	server := tls.Server(serverRaw, &tls.Config{
		Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519MLKEM768}, SessionTicketsDisabled: true,
	})
	client := tls.Client(countingConn{Conn: clientRaw, read: &read, written: &written}, &tls.Config{
		RootCAs: roots, ServerName: "web.svc.example.com", MinVersion: tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519MLKEM768}, ClientSessionCache: nil,
	})
	done := make(chan error, 1)
	go func() { done <- server.Handshake() }()
	if err := client.Handshake(); err != nil {
		tb.Fatal(err)
	}
	if err := <-done; err != nil {
		tb.Fatal(err)
	}
	// Close the pipes, not the TLS conns: Close would send close_notify into a
	// synchronous pipe nobody reads and wait out its 5 second deadline.
	_ = clientRaw.Close()
	_ = serverRaw.Close()
	return written.Load(), read.Load()
}

// TestMLDSASizeBaseline prints the DEC-17 size baseline. It asserts only the
// facts the request caps depend on (PEM sizes against the OpenAPI and CLI
// bounds); the numbers themselves are evidence, not thresholds.
func TestMLDSASizeBaseline(t *testing.T) {
	const (
		maxKeyPEM   = 16384  // PkiIssuerCreateRequest.private_key_pem
		maxCertPEM  = 65536  // certificate_pem, csr_pem
		maxChainPEM = 262144 // chain_pem
	)
	t.Logf("%-28s %10s %10s", "object", "ecdsa-p256", "mldsa65")
	classical, pq := newBenchChain(t, ECDSAP256), newBenchChain(t, MLDSA65)
	row := func(name string, a, b int) { t.Logf("%-28s %10d %10d", name, a, b) }
	row("root certificate DER", len(classical.root.Certificate.Raw), len(pq.root.Certificate.Raw))
	row("intermediate certificate DER", len(classical.issuing.Certificate.Raw), len(pq.issuing.Certificate.Raw))
	row("leaf certificate DER", len(classical.leaf.Raw), len(pq.leaf.Raw))
	row("leaf CSR DER", len(classical.csrDER), len(pq.csrDER))
	row("leaf certificate PEM", len(CertificatePEM(classical.leaf.Raw)), len(CertificatePEM(pq.leaf.Raw)))
	row("leaf CSR PEM", len(CSRPEM(classical.csrDER)), len(CSRPEM(pq.csrDER)))
	chainPEM := func(c benchChain) int {
		return len(CertificatesPEM([][]byte{c.leaf.Raw, c.issuing.Certificate.Raw, c.root.Certificate.Raw}))
	}
	row("3-certificate chain PEM", chainPEM(classical), chainPEM(pq))
	keyPEM := func(c benchChain) int { return len(mustPrivateKeyPEM(t, c.leafKey)) }
	row("generated leaf key PEM", keyPEM(classical), keyPEM(pq))
	for _, n := range []int{0, 100, 1000, 10000} {
		row(fmt.Sprintf("CRL DER, %d entries", n), len(classical.crl(t, n)), len(pq.crl(t, n)))
	}
	cSent, cRecv := classical.handshake(t)
	pSent, pRecv := pq.handshake(t)
	row("handshake bytes client->server", int(cSent), int(pSent))
	row("handshake bytes server->client", int(cRecv), int(pRecv))

	switch {
	case len(CertificatePEM(pq.leaf.Raw)) > maxCertPEM, len(CSRPEM(pq.csrDER)) > maxCertPEM:
		t.Fatal("an ML-DSA-65 leaf or CSR exceeds the 65536-character certificate and CSR cap")
	case chainPEM(pq) > maxChainPEM:
		t.Fatal("an ML-DSA-65 chain exceeds the 262144-character chain cap")
	case keyPEM(pq) > maxKeyPEM:
		t.Fatal("an ML-DSA-65 seed-only key exceeds the 16384-character key cap")
	}
	// The remaining headroom, as chains of this shape that fit the chain cap.
	t.Logf("a 262144-character chain cap admits about %d ML-DSA-65 certificates", maxChainPEM/(len(CertificatePEM(pq.leaf.Raw))))
}

func BenchmarkSignLeaf(b *testing.B) {
	for _, algorithm := range []KeyAlgorithm{ECDSAP256, MLDSA65} {
		c := newBenchChain(b, algorithm)
		policy := basePolicy()
		policy.KeyAlgorithms = []KeyAlgorithm{algorithm}
		policy.KeyUsages = []KeyUsage{UsageDigitalSignature}
		policy = policy.Normalize()
		now := time.Now().UTC()
		resolved, _ := policy.Check(Request{DNSNames: []string{"web.svc.example.com"}, PublicKey: c.leafKey.Public()})
		b.Run(string(algorithm), func(b *testing.B) {
			for b.Loop() {
				serial, _ := NewSerial()
				nb, na, _ := LeafWindow(c.issuing.Certificate, now, resolved.TTL)
				if _, err := SignLeaf(c.issuing, resolved, Leaf{Serial: serial, PublicKey: c.leafKey.Public(), NotBefore: nb, NotAfter: na, KeyUsages: policy.KeyUsages, ExtKeyUsages: policy.ExtKeyUsages}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkVerifyChain(b *testing.B) {
	for _, algorithm := range []KeyAlgorithm{ECDSAP256, MLDSA65} {
		c := newBenchChain(b, algorithm)
		roots, intermediates := x509.NewCertPool(), x509.NewCertPool()
		roots.AddCert(c.root.Certificate)
		intermediates.AddCert(c.issuing.Certificate)
		b.Run(string(algorithm), func(b *testing.B) {
			for b.Loop() {
				if _, err := c.leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: "web.svc.example.com", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParseCSR(b *testing.B) {
	for _, algorithm := range []KeyAlgorithm{ECDSAP256, MLDSA65} {
		c := newBenchChain(b, algorithm)
		b.Run(string(algorithm), func(b *testing.B) {
			for b.Loop() {
				if _, err := ParseCSR(c.csrDER); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCreateCRL(b *testing.B) {
	for _, algorithm := range []KeyAlgorithm{ECDSAP256, MLDSA65} {
		c := newBenchChain(b, algorithm)
		for _, n := range []int{0, 1000} {
			b.Run(fmt.Sprintf("%s/%d-entries", algorithm, n), func(b *testing.B) {
				list := make([]RevokedEntry, n)
				for i := range list {
					serial, _ := NewSerial()
					list[i] = RevokedEntry{Serial: serial, RevokedAt: time.Now().UTC(), Reason: ReasonKeyCompromise}
				}
				for b.Loop() {
					if _, err := CreateCRL(c.issuing, list, CRLNumber(1), time.Now().UTC()); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkVerifyCRL(b *testing.B) {
	for _, algorithm := range []KeyAlgorithm{ECDSAP256, MLDSA65} {
		c := newBenchChain(b, algorithm)
		der := c.crl(b, 1000)
		b.Run(string(algorithm)+"/1000-entries", func(b *testing.B) {
			for b.Loop() {
				crl, err := x509.ParseRevocationList(der)
				if err != nil {
					b.Fatal(err)
				}
				if err := crl.CheckSignatureFrom(c.issuing.Certificate); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTLSHandshake(b *testing.B) {
	for _, algorithm := range []KeyAlgorithm{ECDSAP256, MLDSA65} {
		c := newBenchChain(b, algorithm)
		b.Run(string(algorithm), func(b *testing.B) {
			for b.Loop() {
				c.handshake(b)
			}
		})
	}
}
