package jwkssource

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// RFC 8032 section 7.1, test vector 1 public key.
const testEd25519JWKX = "11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"

func TestParseKeySourceRejectsImpossibleCombinations(t *testing.T) {
	valid := `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"` + testEd25519JWKX + `","kid":"test","use":"sig"}]}`
	empty := ""
	for _, tc := range []struct {
		name     string
		mode     domain.JWKSMode
		document *string
	}{
		{name: "unknown mode", mode: domain.JWKSMode("other")},
		{name: "discovery with document", mode: domain.JWKSDiscovery, document: &valid},
		{name: "discovery with empty document", mode: domain.JWKSDiscovery, document: &empty},
		{name: "static without document", mode: domain.JWKSStatic},
		{name: "static with empty document", mode: domain.JWKSStatic, document: &empty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseKeySource(tc.mode, tc.document); !errors.Is(err, ErrKeySource) {
				t.Fatalf("ParseKeySource() error = %v, want ErrKeySource", err)
			}
		})
	}
}

func TestStaticKeySourceCanonicalizesBeforeUse(t *testing.T) {
	rawA := "{\n  \"keys\": [ { \"use\": \"sig\", \"x\": \"" + testEd25519JWKX + "\", \"kid\": \"test\", \"crv\": \"Ed25519\", \"kty\": \"OKP\" } ]\n}"
	rawB := `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"test","x":"` + testEd25519JWKX + `","use":"sig"}]}`

	sourceA, err := ParseKeySource(domain.JWKSStatic, &rawA)
	if err != nil {
		t.Fatal(err)
	}
	sourceB, err := ParseKeySource(domain.JWKSStatic, &rawB)
	if err != nil {
		t.Fatal(err)
	}
	documentA, ok := sourceA.CanonicalJWKS()
	if !ok {
		t.Fatal("static source reported no canonical JWKS")
	}
	documentB, ok := sourceB.CanonicalJWKS()
	if !ok {
		t.Fatal("static source reported no canonical JWKS")
	}
	if documentA != documentB {
		t.Fatalf("equivalent JWKS documents canonicalized differently:\n%s\n%s", documentA, documentB)
	}
	if strings.ContainsAny(documentA, "\n\t") || strings.Contains(documentA, "  ") {
		t.Fatalf("canonical JWKS still contains formatting whitespace: %q", documentA)
	}
	if sourceA.Mode() != domain.JWKSStatic {
		t.Fatalf("static source mode = %q", sourceA.Mode())
	}
	if _, ok := RemoteDiscovery().CanonicalJWKS(); ok {
		t.Fatal("remote discovery carried a static JWKS")
	}
}

func TestStaticKeySourcePreservesJWKSAdmissionPolicy(t *testing.T) {
	noSigningKeys := `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"` + testEd25519JWKX + `","use":"enc"}]}`
	if _, err := ParseKeySource(domain.JWKSStatic, &noSigningKeys); !errors.Is(err, ErrKeySource) {
		t.Fatalf("non-signing JWKS error = %v, want ErrKeySource", err)
	}

	tooLarge := `{"keys":[]}` + strings.Repeat(" ", MaxJWKSBytes)
	if _, err := ParseKeySource(domain.JWKSStatic, &tooLarge); !errors.Is(err, ErrKeySource) {
		t.Fatalf("oversize JWKS error = %v, want ErrKeySource", err)
	}
}

func TestStaticKeySourceRejectsLowOrderEd25519Key(t *testing.T) {
	raw := `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","kid":"test","use":"sig"}]}`
	if _, err := ParseKeySource(domain.JWKSStatic, &raw); !errors.Is(err, ErrKeySource) {
		t.Fatalf("low-order Ed25519 JWKS error = %v, want ErrKeySource", err)
	}
}

func jwksOf(t *testing.T, keys ...jose.JSONWebKey) []byte {
	t.Helper()
	body, err := json.Marshal(jose.JSONWebKeySet{Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func mldsa65JWK(t *testing.T) jose.JSONWebKey {
	t.Helper()
	priv, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	return jose.JSONWebKey{Key: priv.PublicKey(), KeyID: "pq", Algorithm: string(jose.MLDSA65), Use: "sig"}
}

// Federation key loading accepts RSA, ECDSA and Ed25519 only (#1191). Widening
// this set for ML-DSA (#1180) must widen oidcfed's algorithm allowlist with it.
func TestParseJWKSLoadsRSAECDSAAndEd25519Keys(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := jwksOf(t,
		jose.JSONWebKey{Key: &rsaKey.PublicKey, KeyID: "rsa", Use: "sig"},
		jose.JSONWebKey{Key: &ecKey.PublicKey, KeyID: "ec", Use: "sig"},
		jose.JSONWebKey{Key: edPub, KeyID: "ed", Use: "sig"},
	)
	keys, kids, err := ParseJWKS(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || !kids["rsa"] || !kids["ec"] || !kids["ed"] {
		t.Fatalf("ParseJWKS() = %d keys, kids %v, want rsa, ec and ed", len(keys), kids)
	}
	doc := string(body)
	if _, err := ParseKeySource(domain.JWKSStatic, &doc); err != nil {
		t.Fatalf("static source refused supported key types: %v", err)
	}
}

// A key of a type federation does not accept is skipped like any other unusable
// key; the document is refused only when nothing usable remains.
func TestParseJWKSRefusesMLDSAKey(t *testing.T) {
	pq := mldsa65JWK(t)

	only := jwksOf(t, pq)
	if _, _, err := ParseJWKS(only); err == nil || !strings.Contains(err.Error(), "RSA, ECDSA or Ed25519") {
		t.Fatalf("ML-DSA-only JWKS error = %v, want a key-type refusal", err)
	}
	doc := string(only)
	if _, err := ParseKeySource(domain.JWKSStatic, &doc); !errors.Is(err, ErrKeySource) {
		t.Fatalf("static ML-DSA-only JWKS error = %v, want ErrKeySource", err)
	}

	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys, kids, err := ParseJWKS(jwksOf(t, pq, jose.JSONWebKey{Key: edPub, KeyID: "ed", Use: "sig"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || kids["pq"] || !kids["ed"] {
		t.Fatalf("mixed JWKS = %d keys, kids %v, want only the Ed25519 key", len(keys), kids)
	}
	if _, ok := keys[0].(ed25519.PublicKey); !ok {
		t.Fatalf("mixed JWKS loaded %T, want ed25519.PublicKey", keys[0])
	}
}
