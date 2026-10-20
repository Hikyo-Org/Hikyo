package pairing

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestDPoPRoundTripAndConstraints(t *testing.T) {
	private, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := PublicJWK(private)
	if err != nil {
		t.Fatal(err)
	}
	jkt, err := PublicKeyThumbprint(public)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0)
	claims := DPoPClaims{Method: "GET", URL: "https://serving.example/api/v1/instance/directory", AccessToken: "test-token", Nonce: "nonce", IssuedAt: now}
	proof, err := GenerateDPoP(private, claims)
	if err != nil {
		t.Fatal(err)
	}
	expected := DPoPExpectation{Method: claims.Method, URL: claims.URL, AccessToken: claims.AccessToken, Nonce: claims.Nonce, Thumbprint: jkt, Now: now}
	verified, err := VerifyDPoP(proof, expected)
	if err != nil || verified.Thumbprint != jkt || verified.JTI == "" {
		t.Fatalf("proof rejected: %v", err)
	}
	cases := map[string]func(*DPoPExpectation){
		"key":      func(e *DPoPExpectation) { e.Thumbprint = strings.Repeat("a", 43) },
		"token":    func(e *DPoPExpectation) { e.AccessToken = "other-token" },
		"method":   func(e *DPoPExpectation) { e.Method = "POST" },
		"url":      func(e *DPoPExpectation) { e.URL += "/different" },
		"origin":   func(e *DPoPExpectation) { e.URL = "https://other.example/api/v1/instance/directory" },
		"nonce":    func(e *DPoPExpectation) { e.Nonce = "other-nonce" },
		"expired":  func(e *DPoPExpectation) { e.Now = now.Add(MaxProofAge + time.Second) },
		"future":   func(e *DPoPExpectation) { e.Now = now.Add(-MaxFutureSkew - time.Second) },
		"no-clock": func(e *DPoPExpectation) { e.Now = time.Time{} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			altered := expected
			change(&altered)
			if _, err := VerifyDPoP(proof, altered); err == nil {
				t.Fatal("proof accepted against wrong expectation")
			}
		})
	}
}

func TestDPoPEnrollmentProofAndQueryRule(t *testing.T) {
	private, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0)
	proof, err := GenerateDPoP(private, DPoPClaims{Method: "POST", URL: "https://serving.example/finish?ignored=yes#fragment", Nonce: "bound-exchange", IssuedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyDPoP(proof, DPoPExpectation{Method: "POST", URL: "https://serving.example/finish?different=yes", Nonce: "bound-exchange", Now: now}); err != nil {
		t.Fatal(err)
	}
}

func TestDPoPRejectsSignedMalformedHeadersAndClaims(t *testing.T) {
	private, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := PublicJWK(private)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0)
	validPayload := `{"jti":"unique","htm":"POST","htu":"https://serving.example/finish","iat":1800000000,"nonce":"bound"}`
	privateJWK := jose.JSONWebKey{Key: private, Algorithm: "EdDSA"}
	malformedKey := map[string]string{"kty": "OKP", "crv": "X25519", "x": base64.RawURLEncoding.EncodeToString(private.Public().(ed25519.PublicKey))}
	cases := []struct {
		name    string
		typ     any
		key     any
		payload string
	}{
		{"numeric-type", 3, public, validPayload},
		{"wrong-type", "JWT", public, validPayload},
		{"private-jwk", "dpop+jwt", privateJWK, validPayload},
		{"wrong-curve", "dpop+jwt", malformedKey, validPayload},
		{"duplicate-jti", "dpop+jwt", public, strings.Replace(validPayload, `"jti":"unique"`, `"jti":"one","jti":"two"`, 1)},
		{"fractional-time", "dpop+jwt", public, strings.Replace(validPayload, "1800000000", "1800000000.5", 1)},
		{"null-time", "dpop+jwt", public, strings.Replace(validPayload, "1800000000", "null", 1)},
		{"missing-time", "dpop+jwt", public, strings.Replace(validPayload, `,"iat":1800000000`, "", 1)},
		{"missing-jti", "dpop+jwt", public, strings.Replace(validPayload, `"jti":"unique",`, "", 1)},
		{"unknown-claim", "dpop+jwt", public, strings.Replace(validPayload, `"jti"`, `"unexpected":true,"jti"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := (&jose.SignerOptions{}).WithHeader("typ", tc.typ).WithHeader("jwk", tc.key)
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: private}, opts)
			if err != nil {
				t.Fatal(err)
			}
			signed, err := signer.Sign([]byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			proof, err := signed.CompactSerialize()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyDPoP(proof, DPoPExpectation{Method: "POST", URL: "https://serving.example/finish", Nonce: "bound", Now: now}); err == nil {
				t.Fatal("malformed signed proof accepted")
			}
		})
	}
}

func TestDPoPRejectsMalformedUntrustedInputWithoutPanic(t *testing.T) {
	expected := DPoPExpectation{Method: "GET", URL: "https://serving.example/", Now: time.Unix(1800000000, 0)}
	for _, proof := range []string{"", ".", "..", "a.b.c", "[]", "null", strings.Repeat("x", MaxProofBytes+1), "e30.e30.AA", "e30.e30.AA\n"} {
		if _, err := VerifyDPoP(proof, expected); err == nil {
			t.Fatal("malformed proof accepted")
		}
	}
	private, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := PublicJWK(private)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePublicJWK(encoded); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"null", "{}", string(encoded[:len(encoded)-1]) + `,"d":"AAAA"}`, string(encoded[:len(encoded)-1]) + `,"crv":"Ed25519"}`} {
		if _, err := ParsePublicJWK([]byte(raw)); err == nil {
			t.Fatal("invalid public key accepted")
		}
	}
}
