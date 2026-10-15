package main

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	vw "github.com/descope/virtualwebauthn"
)

func TestTargetPublicHostnameMatchesTrustedTLSCertificate(t *testing.T) {
	target, cleanup, err := newTarget(t.Context(), options{}, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	})
	origin, err := url.Parse(target.base)
	if err != nil || origin.Scheme != "https" || origin.Hostname() != "localhost" || origin.Port() == "" {
		t.Fatalf("public WebAuthn origin = %q, want an HTTPS localhost origin with an explicit port", target.base)
	}
	var address, configuredOrigin string
	for _, value := range target.env {
		if strings.HasPrefix(value, "HIKYO_LISTEN=") {
			address = strings.TrimPrefix(value, "HIKYO_LISTEN=")
		}
		if strings.HasPrefix(value, "HIKYO_EXTERNAL_ORIGIN=") {
			configuredOrigin = strings.TrimPrefix(value, "HIKYO_EXTERNAL_ORIGIN=")
		}
	}
	if configuredOrigin != target.base || address != net.JoinHostPort("127.0.0.1", origin.Port()) {
		t.Fatalf("public origin %q and private listener %q do not match target %q", configuredOrigin, address, target.base)
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(target.dir, "cert.pem"), filepath.Join(target.dir, "key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	_ = server.Listener.Close()
	server.Listener = listener
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	rsp, err := target.client.Get(target.base)
	if err != nil {
		t.Fatalf("fixture HTTPS client must verify the public hostname: %v", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusNoContent {
		t.Fatalf("fixture HTTPS status = %d", rsp.StatusCode)
	}
}

func TestAssertionDoesNotReplayRegistrationOnlyExtensionOutputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/webauthn/step-up/start":
			_, _ = w.Write([]byte(`{"publicKey":{"challenge":"Y2FtcGFpZ24tYXNzZXJ0aW9u","rpId":"localhost"}}`))
		case "/api/v1/auth/webauthn/step-up/finish":
			var assertion struct {
				ClientExtensionResults map[string]any `json:"clientExtensionResults"`
			}
			if err := json.NewDecoder(r.Body).Decode(&assertion); err != nil || len(assertion.ClientExtensionResults) != 0 {
				http.Error(w, "unrequested assertion extension output", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fixture := fixture{device: &passkey{
		rp:         vw.RelyingParty{Name: "hikyo", ID: "localhost", Origin: "https://localhost:8080"},
		opts:       vw.AuthenticatorOptions{ClientExtensionResults: map[string]any{"credProps": map[string]any{"rk": true}}},
		credential: vw.NewCredential(vw.KeyTypeEC2),
	}}
	target := &target{ctx: t.Context(), base: server.URL, client: server.Client()}
	if err := fixture.assert(target, "/api/v1/auth/webauthn/step-up", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
}
