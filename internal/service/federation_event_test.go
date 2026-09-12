package service

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestIssuerEventCABundleDigest(t *testing.T) {
	for _, bundle := range []string{"", "public CA bundle fixture"} {
		event, err := issuerEvent(t.Context(), "usr_test", "issuer_test", "https://issuer.example", "updated", IssuerRequest{CABundlePEM: bundle})
		if err != nil {
			t.Fatal(err)
		}
		digest, present := event.Payload["ca_bundle_sha256"]
		if present != (bundle != "") {
			t.Fatalf("digest present=%v for empty bundle=%v", present, bundle == "")
		}
		if present && digest != fmt.Sprintf("%x", sha256.Sum256([]byte(bundle))) {
			t.Fatal("incorrect CA digest")
		}
	}
}
