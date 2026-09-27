package pki

import (
	"crypto/x509"
	"errors"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func basePolicy() Policy {
	return Policy{
		AllowedIssuers:    []string{"internal"},
		DNSPatterns:       []string{"*.svc.example.com", "api.example.com"},
		IPRanges:          []string{"10.0.0.0/8"},
		URIPatterns:       []string{"spiffe://example.com/ns/*"},
		KeyAlgorithms:     []KeyAlgorithm{ECDSAP256, Ed25519, RSA2048},
		KeyUsages:         []KeyUsage{UsageDigitalSignature, UsageKeyEncipherment},
		ExtKeyUsages:      []ExtKeyUsage{ExtUsageServerAuth, ExtUsageClientAuth},
		MaxTTL:            72 * time.Hour,
		DefaultTTL:        24 * time.Hour,
		RenewWindow:       8 * time.Hour,
		AllowCSR:          true,
		AllowGeneratedKey: true,
	}.Normalize()
}

func TestPolicyValidate(t *testing.T) {
	if err := basePolicy().Validate(); err != nil {
		t.Fatalf("base policy invalid: %v", err)
	}
	cases := map[string]func(*Policy){
		"no issuers":         func(p *Policy) { p.AllowedIssuers = nil },
		"bad issuer":         func(p *Policy) { p.AllowedIssuers = []string{"Bad Name"} },
		"no names":           func(p *Policy) { p.DNSPatterns, p.IPRanges, p.URIPatterns = nil, nil, nil },
		"regex pattern":      func(p *Policy) { p.DNSPatterns = []string{"^.*$"} },
		"inner wildcard":     func(p *Policy) { p.DNSPatterns = []string{"a.*.example.com"} },
		"bare tld wildcard":  func(p *Policy) { p.DNSPatterns = []string{"*.com"} },
		"bad cidr":           func(p *Policy) { p.IPRanges = []string{"10.0.0.0"} },
		"uri inner star":     func(p *Policy) { p.URIPatterns = []string{"spiffe://x/*/y"} },
		"uri no host":        func(p *Policy) { p.URIPatterns = []string{"spiffe:///*"} },
		"no algorithm":       func(p *Policy) { p.KeyAlgorithms = nil },
		"unknown algorithm":  func(p *Policy) { p.KeyAlgorithms = []KeyAlgorithm{"dsa"} },
		"unknown usage":      func(p *Policy) { p.KeyUsages = []KeyUsage{"cert-sign"} },
		"unknown ext usage":  func(p *Policy) { p.ExtKeyUsages = []ExtKeyUsage{"any"} },
		"ttl too long":       func(p *Policy) { p.MaxTTL = MaxLeafTTL + time.Hour },
		"default over max":   func(p *Policy) { p.DefaultTTL = p.MaxTTL + time.Hour },
		"window equals max":  func(p *Policy) { p.RenewWindow = p.MaxTTL },
		"no delivery method": func(p *Policy) { p.AllowCSR, p.AllowGeneratedKey = false, false },
		"control in org":     func(p *Policy) { p.Organization = "a\nb" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := basePolicy()
			mutate(&p)
			if err := p.Normalize().Validate(); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("want ErrInvalidPolicy, got %v", err)
			}
		})
	}
}

func mustKey(t *testing.T, algorithm KeyAlgorithm) any {
	t.Helper()
	key, err := GenerateKey(algorithm)
	if err != nil {
		t.Fatal(err)
	}
	return key.Public()
}

func TestPolicyCheck(t *testing.T) {
	p := basePolicy()
	pub := mustKey(t, ECDSAP256)
	ok := []Request{
		{DNSNames: []string{"web.svc.example.com"}, PublicKey: pub},
		{DNSNames: []string{"API.example.com."}, CommonName: "api.example.com", PublicKey: pub},
		{IPAddresses: []string{"10.1.2.3"}, PublicKey: pub, Generated: true},
		{URIs: []string{"spiffe://example.com/ns/prod/sa/web"}, PublicKey: pub, TTL: 72 * time.Hour},
	}
	for _, req := range ok {
		if _, err := p.Check(req); err != nil {
			t.Errorf("%+v: unexpected refusal %v", req, err)
		}
	}
	refused := map[string]Request{
		"two labels deep":      {DNSNames: []string{"a.b.svc.example.com"}, PublicKey: pub},
		"suffix only":          {DNSNames: []string{"svc.example.com"}, PublicKey: pub},
		"lookalike suffix":     {DNSNames: []string{"x.svc.example.com.evil.io"}, PublicKey: pub},
		"wildcard name":        {DNSNames: []string{"*.svc.example.com"}, PublicKey: pub},
		"ip outside":           {IPAddresses: []string{"192.168.1.1"}, PublicKey: pub},
		"uri outside":          {URIs: []string{"spiffe://example.com/other"}, PublicKey: pub},
		"uri exact prefix":     {URIs: []string{"spiffe://example.com/ns/"}, PublicKey: pub},
		"uri dot segment":      {URIs: []string{"spiffe://example.com/ns/../admin"}, PublicKey: pub},
		"uri encoded dots":     {URIs: []string{"spiffe://example.com/ns/%2e%2e/admin"}, PublicKey: pub},
		"uri query":            {URIs: []string{"spiffe://example.com/ns/x?y"}, PublicKey: pub},
		"uri fragment":         {URIs: []string{"spiffe://example.com/ns/x#y"}, PublicKey: pub},
		"uri alternate form":   {URIs: []string{"SPIFFE://example.com/ns/x"}, PublicKey: pub},
		"no sans":              {CommonName: "api.example.com", PublicKey: pub},
		"cn not in sans":       {DNSNames: []string{"api.example.com"}, CommonName: "other.example.com", PublicKey: pub},
		"ttl too long":         {DNSNames: []string{"api.example.com"}, TTL: 73 * time.Hour, PublicKey: pub},
		"ttl too short":        {DNSNames: []string{"api.example.com"}, TTL: time.Minute, PublicKey: pub},
		"algorithm not listed": {DNSNames: []string{"api.example.com"}, PublicKey: mustKey(t, ECDSAP384)},
	}
	for name, req := range refused {
		t.Run(name, func(t *testing.T) {
			if _, err := p.Check(req); !errors.Is(err, ErrRefused) {
				t.Fatalf("want ErrRefused, got %v", err)
			}
		})
	}
	csrOnly := p
	csrOnly.AllowGeneratedKey = false
	if _, err := csrOnly.Check(Request{DNSNames: []string{"api.example.com"}, PublicKey: pub, Generated: true}); !errors.Is(err, ErrRefused) {
		t.Fatalf("generated key on a CSR-only profile: %v", err)
	}
	wild := p
	wild.AllowWildcardNames = true
	if _, err := wild.Check(Request{DNSNames: []string{"*.svc.example.com"}, PublicKey: pub}); err != nil {
		t.Fatalf("wildcard name on a wildcard profile: %v", err)
	}
}

func TestNarrows(t *testing.T) {
	base := basePolicy()
	narrowing := map[string]func(*Policy){
		"identical":            func(*Policy) {},
		"exact under wildcard": func(p *Policy) { p.DNSPatterns = []string{"web.svc.example.com"} },
		"drop a pattern":       func(p *Policy) { p.DNSPatterns = []string{"api.example.com"} },
		"smaller cidr":         func(p *Policy) { p.IPRanges = []string{"10.1.0.0/16"} },
		"deeper uri prefix":    func(p *Policy) { p.URIPatterns = []string{"spiffe://example.com/ns/prod/*"} },
		"exact uri":            func(p *Policy) { p.URIPatterns = []string{"spiffe://example.com/ns/prod/sa/web"} },
		"fewer algorithms":     func(p *Policy) { p.KeyAlgorithms = []KeyAlgorithm{ECDSAP256} },
		"shorter ttl":          func(p *Policy) { p.MaxTTL, p.DefaultTTL, p.RenewWindow = 48*time.Hour, 12*time.Hour, 4*time.Hour },
		"disable csr":          func(p *Policy) { p.AllowCSR = false },
		"lower default ttl":    func(p *Policy) { p.DefaultTTL = time.Hour },
	}
	for name, mutate := range narrowing {
		t.Run("narrow/"+name, func(t *testing.T) {
			next := base
			mutate(&next)
			if err := Narrows(base, next); err != nil {
				t.Fatalf("want narrowing, got %v", err)
			}
		})
	}
	widening := map[string]func(*Policy){
		"new issuer":            func(p *Policy) { p.AllowedIssuers = append(p.AllowedIssuers, "other") },
		"new suffix":            func(p *Policy) { p.DNSPatterns = append(p.DNSPatterns, "*.example.org") },
		"deeper wildcard":       func(p *Policy) { p.DNSPatterns = []string{"*.a.svc.example.com"} },
		"wildcard over exact":   func(p *Policy) { p.DNSPatterns = []string{"*.example.com"} },
		"bigger cidr":           func(p *Policy) { p.IPRanges = []string{"0.0.0.0/0"} },
		"v6 range":              func(p *Policy) { p.IPRanges = []string{"::/0"} },
		"shallower uri":         func(p *Policy) { p.URIPatterns = []string{"spiffe://example.com/*"} },
		"new algorithm":         func(p *Policy) { p.KeyAlgorithms = append(p.KeyAlgorithms, RSA4096) },
		"new usage":             func(p *Policy) { p.KeyUsages = append(p.KeyUsages, UsageKeyAgreement) },
		"longer ttl":            func(p *Policy) { p.MaxTTL = 96 * time.Hour },
		"longer renewal window": func(p *Policy) { p.RenewWindow = 9 * time.Hour },
		"enable wildcard":       func(p *Policy) { p.AllowWildcardNames = true },
		"enable machines":       func(p *Policy) { p.MachineIssuance = true },
		"change organization":   func(p *Policy) { p.Organization = "Other" },
	}
	for name, mutate := range widening {
		t.Run("widen/"+name, func(t *testing.T) {
			next := base
			mutate(&next)
			if err := Narrows(base, next); !errors.Is(err, ErrWidening) {
				t.Fatalf("want ErrWidening, got %v", err)
			}
		})
	}
}

// FuzzNarrowsSound checks the property Narrows exists for: when it accepts a
// new DNS pattern set, every name the new set admits, the old set admits too.
func FuzzNarrowsSound(f *testing.F) {
	f.Add("*.svc.example.com", "web.svc.example.com", "web.svc.example.com")
	f.Add("*.example.com", "*.example.com", "a.example.com")
	f.Add("a.example.com", "*.example.com", "b.example.com")
	f.Fuzz(func(t *testing.T, older, newer, name string) {
		oldPolicy := basePolicy()
		oldPolicy.DNSPatterns = []string{older}
		newPolicy := oldPolicy
		newPolicy.DNSPatterns = []string{newer}
		oldPolicy, newPolicy = oldPolicy.Normalize(), newPolicy.Normalize()
		if oldPolicy.Validate() != nil || newPolicy.Validate() != nil {
			return
		}
		if Narrows(oldPolicy, newPolicy) != nil {
			return
		}
		pub := mustKey(t, ECDSAP256)
		if _, err := newPolicy.Check(Request{DNSNames: []string{name}, PublicKey: pub}); err != nil {
			return
		}
		if _, err := oldPolicy.Check(Request{DNSNames: []string{name}, PublicKey: pub}); err != nil {
			t.Fatalf("narrowed %q -> %q admits %q but the original refuses it: %v", older, newer, name, err)
		}
	})
}

func newRoot(t *testing.T) Parent {
	t.Helper()
	key, err := GenerateKey(ECDSAP256)
	if err != nil {
		t.Fatal(err)
	}
	der, err := CreateRoot(key, Subject{CommonName: "Hikyo Test Root"}, epoch, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := VerifyCA(der, key.Public(), nil, epoch)
	if err != nil {
		t.Fatalf("root does not verify as a CA: %v", err)
	}
	return Parent{Certificate: cert, Signer: key}
}

func TestIssueChainAndCRL(t *testing.T) {
	root := newRoot(t)
	intermediateKey, err := GenerateKey(ECDSAP384)
	if err != nil {
		t.Fatal(err)
	}
	intermediateDER, err := SignIntermediate(root, intermediateKey.Public(), Subject{CommonName: "Hikyo Issuing CA"}, epoch, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	intermediate, err := VerifyCA(intermediateDER, intermediateKey.Public(), []*x509.Certificate{root.Certificate}, epoch)
	if err != nil {
		t.Fatalf("intermediate: %v", err)
	}
	if !intermediate.MaxPathLenZero {
		t.Fatal("a Hikyo intermediate must not be able to sign further CAs")
	}
	if _, err := VerifyCA(intermediateDER, root.Signer.Public(), []*x509.Certificate{root.Certificate}, epoch); err == nil {
		t.Fatal("VerifyCA accepted a certificate whose key is not the issuer key")
	}
	if _, err := VerifyCA(intermediateDER, intermediateKey.Public(), nil, epoch); err == nil {
		t.Fatal("VerifyCA accepted an intermediate without its chain")
	}
	issuing := Parent{Certificate: intermediate, Signer: intermediateKey}
	if _, err := SignIntermediate(issuing, intermediateKey.Public(), Subject{CommonName: "x"}, epoch, 30*24*time.Hour); err == nil {
		t.Fatal("an intermediate signed a further CA")
	}

	leafKey, _ := GenerateKey(RSA2048)
	p := basePolicy()
	resolved, err := p.Check(Request{DNSNames: []string{"web.svc.example.com"}, CommonName: "web.svc.example.com", PublicKey: leafKey.Public()})
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := NewSerial()
	notBefore, notAfter, err := LeafWindow(intermediate, epoch, resolved.TTL)
	if err != nil {
		t.Fatal(err)
	}
	leafDER, err := SignLeaf(issuing, resolved, Leaf{
		Serial: serial, PublicKey: leafKey.Public(), NotBefore: notBefore, NotAfter: notAfter,
		Organization: "Example", KeyUsages: p.KeyUsages, ExtKeyUsages: p.ExtKeyUsages,
		CRLDistributionPoints: []string{"https://pki.example.com/internal.crl"},
	})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}
	if leaf.IsCA || leaf.KeyUsage&x509.KeyUsageKeyEncipherment == 0 || leaf.Subject.Organization[0] != "Example" {
		t.Fatalf("unexpected leaf shape: ca=%v usage=%v subject=%v", leaf.IsCA, leaf.KeyUsage, leaf.Subject)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root.Certificate)
	inter := x509.NewCertPool()
	inter.AddCert(intermediate)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: inter, CurrentTime: epoch, DNSName: "web.svc.example.com",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("leaf does not chain: %v", err)
	}
	if _, _, err := LeafWindow(intermediate, epoch, 91*24*time.Hour); !errors.Is(err, ErrIssuerExpiry) {
		t.Fatalf("a leaf outliving its issuer: %v", err)
	}

	number := NextCRLNumber(0, epoch)
	crlDER, err := CreateCRL(issuing, []RevokedEntry{{Serial: serial, RevokedAt: epoch, Reason: ReasonKeyCompromise}}, bigInt(number), epoch)
	if err != nil {
		t.Fatal(err)
	}
	crl, err := x509.ParseRevocationList(crlDER)
	if err != nil {
		t.Fatal(err)
	}
	if err := crl.CheckSignatureFrom(intermediate); err != nil {
		t.Fatalf("crl signature: %v", err)
	}
	if len(crl.RevokedCertificateEntries) != 1 || crl.RevokedCertificateEntries[0].SerialNumber.Cmp(serial) != 0 ||
		crl.RevokedCertificateEntries[0].ReasonCode != 1 {
		t.Fatalf("unexpected CRL entries: %+v", crl.RevokedCertificateEntries)
	}
	if NextCRLNumber(number+10, epoch) != number+11 || NextCRLNumber(3, epoch) != epoch.Unix() {
		t.Fatal("CRL number is not monotonic")
	}
}

func TestParseCSRChecksPossession(t *testing.T) {
	key, _ := GenerateKey(ECDSAP256)
	der, err := CreateCSR(key, Subject{CommonName: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCSR([]byte(CSRPEM(der))); err != nil {
		t.Fatalf("valid CSR refused: %v", err)
	}
	tampered := append([]byte(nil), der...)
	tampered[len(tampered)-5] ^= 0xff
	if _, err := ParseCSR(tampered); !errors.Is(err, ErrInvalidCSR) {
		t.Fatalf("tampered CSR accepted: %v", err)
	}
	if _, err := ParseCSR([]byte(CertificatePEM(der))); !errors.Is(err, ErrInvalidCSR) {
		t.Fatalf("wrong PEM type accepted: %v", err)
	}
}

func TestPrivateKeyRoundTripAndRefusals(t *testing.T) {
	for _, algorithm := range LeafKeyAlgorithms {
		if strings.HasPrefix(string(algorithm), "rsa-") && algorithm != RSA2048 {
			continue // slow; the RSA path is covered by rsa-2048
		}
		key, err := GenerateKey(algorithm)
		if err != nil {
			t.Fatal(err)
		}
		pkcs8, err := MarshalPrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParsePrivateKey(PrivateKeyPEM(pkcs8))
		if err != nil {
			t.Fatalf("%s: %v", algorithm, err)
		}
		if got, _ := AlgorithmOf(parsed.Public()); got != algorithm {
			t.Fatalf("round trip changed algorithm %s -> %s", algorithm, got)
		}
	}
	if _, err := ParsePrivateKey([]byte("-----BEGIN ENCRYPTED PRIVATE KEY-----\nAA==\n-----END ENCRYPTED PRIVATE KEY-----\n")); err == nil {
		t.Fatal("encrypted key accepted")
	}
	if _, err := ParseRequestedReason("ca-compromise"); err == nil {
		t.Fatal("a caller may not request ca-compromise")
	}
}
