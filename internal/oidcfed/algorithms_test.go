package oidcfed

import (
	"crypto/mldsa"
	"errors"
	"slices"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
)

// go-jose v4.1.8 added ML-DSA (#1191, #1130, DEC-13). The accepted algorithms are
// an explicit list here, not whatever the library supports, so a library bump
// must not widen them. Changing this list is a profile decision, not a
// dependency update.
func TestSignatureAllowlistIsExplicit(t *testing.T) {
	want := []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "PS256", "PS384", "PS512"}
	if !slices.Equal(allowedAlgs, want) {
		t.Fatalf("allowedAlgs = %v, want %v", allowedAlgs, want)
	}
	got := make([]string, len(joseAlgs))
	for i, alg := range joseAlgs {
		got[i] = string(alg)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("joseAlgs = %v, want %v", got, want)
	}
}

// A token signed with an algorithm go-jose now supports but Hikyo does not
// allow is not a token at all.
func TestPeekRefusesMLDSAToken(t *testing.T) {
	priv, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.MLDSA65, Key: priv}, nil)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign([]byte(`{"iss":"https://issuer.example"}`))
	if err != nil {
		t.Fatal(err)
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Peek(compact); !errors.Is(err, ErrNotAToken) {
		t.Fatalf("Peek(ML-DSA token) error = %v, want ErrNotAToken", err)
	}
}
