package cli

import (
	"strings"
	"testing"
)

// A trust field's refusal reads as it did under encoding/json (v1): the
// field's own message, not v2's wrapper naming the Go type.
func TestTrustFieldRefusalKeepsItsOwnText(t *testing.T) {
	var entry struct {
		Mode    TLSMode    `json:"tls_mode"`
		Profile TLSProfile `json:"tls_profile"`
	}
	err := decodeTrustJSON([]byte(`{"tls_mode":"bogus"}`), &entry)
	if err == nil || err.Error() != `unknown TLS verification mode "bogus"` {
		t.Fatalf("tls_mode refusal = %v", err)
	}
	err = decodeTrustJSON([]byte(`{"tls_profile":"weird"}`), &entry)
	if err == nil || strings.Contains(err.Error(), "cli.TLSProfile") || strings.Contains(err.Error(), "/tls_profile") {
		t.Fatalf("tls_profile refusal = %v", err)
	}
	err = decodeTrustJSON([]byte(`{"tls_mod":"spki"}`), &entry)
	if err == nil || !strings.Contains(err.Error(), "tls_mod") {
		t.Fatalf("unknown member refusal = %v", err)
	}
}
