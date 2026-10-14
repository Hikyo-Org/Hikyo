package samlsp

import (
	"bytes"
	"crypto/x509"
	"testing"
	"time"
)

// FuzzValidateResponseBinding reaches the actual signature and policy owner.
// Parsing XML alone never establishes a valid SAML authentication result.
func FuzzValidateResponseBinding(f *testing.F) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	raw, certificate := signedResponseFixture(f, now)
	_, untrusted := signedResponseFixture(f, now)
	pins := []*x509.Certificate{certificate}
	expected := ValidationExpectations{
		ProviderEntityID: "https://idp.example/metadata",
		SPEntityID:       "https://hikyo.example/saml/metadata",
		ACSURL:           "https://hikyo.example/api/v1/auth/saml/provider/acs",
		RequestID:        "_request", Now: now,
	}
	claims, err := ValidateResponse(raw, pins, expected)
	if err != nil || string(claims.NameID.Value) != "Alice" {
		f.Fatalf("genuine signed response failed: %v", err)
	}
	for selector := byte(0); selector < 8; selector++ {
		f.Add("other", selector, byte(1))
	}
	f.Add("\x00https://idp.example/metadata", byte(4), byte(255))
	f.Fuzz(func(t *testing.T, suffix string, selector, mask byte) {
		if len(suffix) > 4096 {
			return
		}
		wrong := expected
		input, trusted := raw, pins
		// Always unequal, including empty, malformed and unicode fuzz inputs.
		switch selector % 8 {
		case 0:
			wrong.ProviderEntityID += "#wrong-" + suffix
		case 1:
			wrong.SPEntityID += "#wrong-" + suffix
		case 2:
			wrong.ACSURL += "#wrong-" + suffix
		case 3:
			wrong.RequestID += "-wrong-" + suffix
		case 4:
			wrong.Now = now.Add(time.Hour)
		case 5:
			wrong.Now = now.Add(-time.Hour)
		case 6:
			trusted = []*x509.Certificate{untrusted}
		case 7:
			input = bytes.Clone(raw)
			at := bytes.Index(input, []byte(">Alice<"))
			if at < 0 {
				t.Fatal("fixture lost its signed NameID")
			}
			if mask == 0 {
				mask = 1
			}
			input[at+1+int(selector)%len("Alice")] ^= mask
		}
		if _, err := ValidateResponse(input, trusted, wrong); err == nil {
			t.Fatal("signed response accepted outside its request/provider/audience/time binding")
		}
	})
}
