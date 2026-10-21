package awssm

import (
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The existing error-only API must not acquire a response-shape requirement.
// Only the explicit metadata identity path requires ARN and Name.
func TestCreateSecretIdentityPreservesLegacyContract(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Amz-Target") != "secretsmanager.CreateSecret" {
			t.Error("unexpected operation")
		}
		var request map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid request")
		}
		if _, exists := request["SecretString"]; exists {
			t.Error("metadata create added a value")
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, err := NewClient(ClientConfig{Origin: server.URL, Credential: staticDescriptor(testRegion), Deadline: time.Second, RootCAs: roots, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Forget()
	input := CreateSecretInput{Name: "offline/metadata", ClientRequestToken: strings.Repeat("a", 64)}
	if err := client.CreateSecret(t.Context(), input); err != nil {
		t.Fatal("legacy create began requiring response identity")
	}
	if _, err := client.CreateSecretIdentity(t.Context(), input, testAccount); err == nil {
		t.Fatal("strict creation identity accepted an absent identity")
	}
	if calls.Load() != 2 {
		t.Fatal("create path repeated a write")
	}
	if _, err := client.CreateSecretIdentity(t.Context(), input, "invalid"); err == nil || calls.Load() != 2 {
		t.Fatal("invalid expected account reached provider")
	}
}
