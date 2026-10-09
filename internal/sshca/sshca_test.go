package sshca

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func mustCA(t *testing.T, alg Algorithm) ssh.Signer {
	t.Helper()
	key, err := GenerateKey(alg)
	if err != nil {
		t.Fatal(err)
	}
	der, err := MarshalCAKey(key)
	if err != nil {
		t.Fatal(err)
	}
	signer, got, err := SignerFromCAKey(der)
	if err != nil {
		t.Fatal(err)
	}
	if got != alg {
		t.Fatalf("round-trip algorithm = %s, want %s", got, alg)
	}
	return signer
}

func testProfile() Profile {
	return Profile{
		Principals:      []string{"deploy", "ops"},
		SourceAddresses: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		Extensions:      []Extension{ExtPTY, ExtPortForwarding},
		KeyAlgorithms:   []Algorithm{AlgorithmEd25519, AlgorithmECDSAP256},
		DefaultTTL:      time.Hour,
		MaxTTL:          8 * time.Hour,
	}
}

func TestCAKeyRoundTripAndSignVerify(t *testing.T) {
	for _, alg := range Algorithms() {
		t.Run(string(alg), func(t *testing.T) {
			ca := mustCA(t, alg)
			user, err := GenerateKey(AlgorithmEd25519)
			if err != nil {
				t.Fatal(err)
			}
			pub, err := PublicKeyOf(user)
			if err != nil {
				t.Fatal(err)
			}
			res, err := Resolve(testProfile(), Request{Principals: []string{"deploy"}})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			cert, err := Sign(ca, pub, Template{Serial: 42, KeyID: "hikyo:test", Resolved: res, ValidAfter: now.Add(-ClockSkew), ValidBefore: now.Add(res.TTL)})
			if err != nil {
				t.Fatal(err)
			}
			if alg == AlgorithmRSA3072 && cert.Signature.Format != ssh.KeyAlgoRSASHA512 {
				t.Fatalf("RSA CA signed with %s, want rsa-sha2-512", cert.Signature.Format)
			}
			checker := ssh.CertChecker{
				// CheckCert verifies integrity here; the OpenSSH suite enforces addresses.
				SupportedCriticalOptions: []string{"source-address"},
				IsUserAuthority:          func(auth ssh.PublicKey) bool { return bytes.Equal(auth.Marshal(), ca.PublicKey().Marshal()) },
			}
			if err := checker.CheckCert("deploy", cert); err != nil {
				t.Fatalf("CheckCert: %v", err)
			}
			if err := checker.CheckCert("root", cert); err == nil {
				t.Fatal("certificate accepted for a principal it does not name")
			}
			if cert.CriticalOptions["source-address"] != "10.0.0.0/8" {
				t.Fatalf("source-address = %q", cert.CriticalOptions["source-address"])
			}
			if _, ok := cert.Extensions["permit-pty"]; !ok {
				t.Fatal("default extensions not applied")
			}
		})
	}
}

func TestResolveRefusesWidening(t *testing.T) {
	p := testProfile()
	cases := map[string]Request{
		"foreign principal":   {Principals: []string{"root"}},
		"wider source":        {Principals: []string{"ops"}, SourceAddresses: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}},
		"other family source": {Principals: []string{"ops"}, SourceAddresses: []netip.Prefix{netip.MustParsePrefix("::/0")}},
		"extra extension":     {Principals: []string{"ops"}, Extensions: []Extension{ExtAgentForwarding}, ExtensionsSet: true},
		"ttl over max":        {Principals: []string{"ops"}, TTL: 9 * time.Hour},
		"ttl under min":       {Principals: []string{"ops"}, TTL: time.Second},
		"ambiguous principal": {},
	}
	for name, req := range cases {
		if _, err := Resolve(p, req); !errors.Is(err, ErrConstraint) {
			t.Errorf("%s: err = %v, want ErrConstraint", name, err)
		}
	}
	got, err := Resolve(p, Request{Principals: []string{"ops"}, SourceAddresses: []netip.Prefix{netip.MustParsePrefix("10.1.2.0/24")}, ExtensionsSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Extensions) != 0 || got.SourceAddresses[0].String() != "10.1.2.0/24" || got.TTL != time.Hour {
		t.Fatalf("narrowing request resolved to %+v", got)
	}
	one := p
	one.Principals = []string{"deploy"}
	if got, err := Resolve(one, Request{}); err != nil || got.Principals[0] != "deploy" {
		t.Fatalf("single-principal default: %+v %v", got, err)
	}
}

func TestValidateProfile(t *testing.T) {
	if err := ValidateProfile(testProfile()); err != nil {
		t.Fatal(err)
	}
	bad := []func(*Profile){
		func(p *Profile) { p.Principals = nil },
		func(p *Profile) { p.Principals = []string{"a b"} },
		func(p *Profile) { p.Principals = []string{"a,b"} },
		func(p *Profile) { p.Principals = []string{"x", "x"} },
		func(p *Profile) { p.KeyAlgorithms = nil },
		func(p *Profile) { p.KeyAlgorithms = []Algorithm{"dsa"} },
		func(p *Profile) { p.Extensions = []Extension{"no-touch-required"} },
		func(p *Profile) { p.MaxTTL = MaxTTL + time.Second },
		func(p *Profile) { p.DefaultTTL = p.MaxTTL + time.Second },
		func(p *Profile) { p.ForceCommand = "a\nb" },
	}
	for i, mutate := range bad {
		p := testProfile()
		mutate(&p)
		if err := ValidateProfile(p); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestParsePublicKeyClosedSet(t *testing.T) {
	ed, _ := GenerateKey(AlgorithmEd25519)
	pub, _ := PublicKeyOf(ed)
	if _, alg, err := ParsePublicKey(AuthorizedKey(pub, "me@host")); err != nil || alg != AlgorithmEd25519 {
		t.Fatalf("ed25519: %v %v", alg, err)
	}
	small, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	smallPub, _ := ssh.NewPublicKey(&small.PublicKey)
	if _, _, err := ParsePublicKey(AuthorizedKey(smallPub, "")); !errors.Is(err, ErrUnsupportedKey) {
		t.Fatalf("2048-bit RSA accepted: %v", err)
	}
	two := AuthorizedKey(pub, "") + "\n" + AuthorizedKey(pub, "")
	if _, _, err := ParsePublicKey(two); err == nil {
		t.Fatal("two keys accepted")
	}
	ca := mustCA(t, AlgorithmEd25519)
	res, _ := Resolve(testProfile(), Request{Principals: []string{"ops"}})
	cert, err := Sign(ca, pub, Template{Serial: 1, Resolved: res, ValidAfter: time.Now(), ValidBefore: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParsePublicKey(AuthorizedKey(cert, "")); !errors.Is(err, ErrUnsupportedKey) {
		t.Fatalf("certificate accepted as a key: %v", err)
	}
}

func TestParseImportedKey(t *testing.T) {
	key, _ := GenerateKey(AlgorithmEd25519)
	block, err := ssh.MarshalPrivateKey(key, "ca")
	if err != nil {
		t.Fatal(err)
	}
	if _, alg, err := ParseImportedKey(pem.EncodeToMemory(block)); err != nil || alg != AlgorithmEd25519 {
		t.Fatalf("openssh import: %v %v", alg, err)
	}
	enc, err := ssh.MarshalPrivateKeyWithPassphrase(key, "ca", []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseImportedKey(pem.EncodeToMemory(enc)); !errors.Is(err, ErrEncryptedKey) {
		t.Fatalf("encrypted import: %v", err)
	}
	if _, _, err := ParseImportedKey([]byte("not a key")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestAlgorithmOfPrivateRejectsInconsistentEd25519PublicHalf(t *testing.T) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	key[ed25519.SeedSize] ^= 0xff
	if _, _, err := algorithmOfPrivate(key); !errors.Is(err, ErrUnsupportedKey) {
		t.Fatalf("algorithmOfPrivate() error = %v, want ErrUnsupportedKey", err)
	}
}

func TestUserPrivateKeyIsOpenSSHFormat(t *testing.T) {
	key, _ := GenerateKey(AlgorithmECDSAP256)
	text, err := MarshalUserPrivateKey(key, "hikyo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Fatalf("unexpected encoding: %.40q", text)
	}
	if _, err := ssh.ParsePrivateKey([]byte(text)); err != nil {
		t.Fatal(err)
	}
}

func TestNewSerialRange(t *testing.T) {
	for range 1000 {
		s, err := NewSerial()
		if err != nil {
			t.Fatal(err)
		}
		if s == 0 || s>>63 != 0 {
			t.Fatalf("serial %d out of range", s)
		}
	}
}

// TestKRLEncodingKAT pins the byte layout against PROTOCOL.krl by decoding it
// back by hand.
func TestKRLEncodingKAT(t *testing.T) {
	ed := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32))
	caPub, _ := ssh.NewPublicKey(ed.Public())
	out, err := EncodeKRL(KRL{
		Version: 9, GeneratedAt: time.Unix(1700000000, 0), Comment: "hikyo",
		Sections: []KRLSection{{CAKey: caPub, Serials: []uint64{5, 3, 5, 0}}, {CAKey: caPub}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(out)
	u32 := func() uint32 { var b [4]byte; r.Read(b[:]); return binary.BigEndian.Uint32(b[:]) }
	u64 := func() uint64 { var b [8]byte; r.Read(b[:]); return binary.BigEndian.Uint64(b[:]) }
	str := func() []byte { n := u32(); b := make([]byte, n); r.Read(b); return b }
	if u64() != krlMagic || u32() != 1 || u64() != 9 || u64() != 1700000000 || u64() != 0 {
		t.Fatal("header mismatch")
	}
	if len(str()) != 0 || string(str()) != "hikyo" {
		t.Fatal("reserved/comment mismatch")
	}
	typ, _ := r.ReadByte()
	if typ != krlSectionCerts {
		t.Fatalf("section type %d", typ)
	}
	body := bytes.NewReader(str())
	if r.Len() != 0 {
		t.Fatal("empty section was not dropped")
	}
	r = body
	if !bytes.Equal(str(), caPub.Marshal()) || len(str()) != 0 {
		t.Fatal("ca key mismatch")
	}
	sub, _ := r.ReadByte()
	if sub != krlCertSectionSerials {
		t.Fatalf("subsection %d", sub)
	}
	serials := bytes.NewReader(str())
	r = serials
	if u64() != 3 || u64() != 5 || r.Len() != 0 {
		t.Fatal("serials not sorted, de-duplicated and zero-free")
	}
}

func TestKRLBound(t *testing.T) {
	ed, _ := GenerateKey(AlgorithmEd25519)
	pub, _ := PublicKeyOf(ed)
	serials := make([]uint64, MaxKRLSerials+1)
	for i := range serials {
		serials[i] = uint64(i + 1)
	}
	if _, err := EncodeKRL(KRL{Sections: []KRLSection{{CAKey: pub, Serials: serials}}}); !errors.Is(err, ErrKRLTooLarge) {
		t.Fatalf("err = %v, want ErrKRLTooLarge", err)
	}
}

func FuzzValidPrincipal(f *testing.F) {
	for _, s := range []string{"deploy", "a b", "a,b", "", "\x00", "ünï"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !ValidPrincipal(s) {
			return
		}
		if strings.ContainsAny(s, ", \t\r\n\x00") || len(s) > MaxPrincipalLength {
			t.Fatalf("accepted %q", s)
		}
	})
}

func FuzzParseSourceAddresses(f *testing.F) {
	for _, s := range []string{"10.0.0.0/8", "::1", "1.2.3.4", "fe80::1%eth0", "x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := ParseSourceAddresses([]string{s})
		if err != nil {
			return
		}
		if len(got) != 1 || got[0] != got[0].Masked() || strings.ContainsAny(got[0].String(), ", ") {
			t.Fatalf("%q parsed to %v", s, got)
		}
	})
}
