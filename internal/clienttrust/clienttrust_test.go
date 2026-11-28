package clienttrust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json/v2"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTLSConfigPinAcceptsOnlyTheExactLeaf(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	leaf := server.Certificate()
	pin := SPKIFingerprint(leaf)
	config := TLSConfig(server.URL, ModeSPKI, pin, "")
	if err := config.VerifyConnection(peers(leaf)); err != nil {
		t.Fatalf("matching pin refused: %v", err)
	}
	// The refusal also names the remedy for proxies that re-key on renewal.
	if err := TLSConfig(server.URL, ModeSPKI, "AAAA", "").VerifyConnection(peers(leaf)); err == nil ||
		!strings.Contains(err.Error(), "does not match the pin") || !strings.Contains(err.Error(), "system CA (system-ca) trust") {
		t.Fatalf("wrong pin = %v", err)
	}
	if err := config.VerifyConnection(peers()); err == nil {
		t.Fatal("empty chain accepted")
	}
	// The pin is checked against the leaf only: the pinned certificate as a
	// later chain element must not satisfy it.
	if err := config.VerifyConnection(peers(parseCert(t, selfSigned(t)), leaf)); err == nil {
		t.Fatal("pin satisfied by a non-leaf certificate")
	}
}

func TestTLSConfigSystemCAKeepsChainVerification(t *testing.T) {
	config := TLSConfig("https://hikyo.example", ModeSystemCA, "", "")
	if config.InsecureSkipVerify || config.VerifyConnection != nil || config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("system-ca config %+v", config)
	}
}

func TestInspectIdentityReturnsPinAndRefusesUnearnedPolicy(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	pin, err := InspectIdentity(t.Context(), server.URL, 5*time.Second, ModeSPKI)
	if err != nil || pin != SPKIFingerprint(server.Certificate()) {
		t.Fatalf("pin %q err %v", pin, err)
	}
	// A system-CA probe verifies normally, and the test server's CA is not a root.
	if _, err := InspectIdentity(t.Context(), server.URL, 5*time.Second, ModeSystemCA); err == nil {
		t.Fatal("system-ca probe trusted a private CA")
	}
	if _, err := InspectIdentity(t.Context(), server.URL, time.Second, ""); err == nil {
		t.Fatal("probe ran without an explicit policy")
	}
	if _, err := InspectIdentity(t.Context(), "http://127.0.0.1:1", time.Second, ModeSPKI); err == nil {
		t.Fatal("probe accepted plaintext http")
	}
}

func TestPolicyValidate(t *testing.T) {
	for name, tc := range map[string]struct {
		policy Policy
		ok     bool
	}{
		"legacy https":           {Policy{Origin: "https://a.example"}, true},
		"system-ca":              {Policy{Origin: "https://a.example", Mode: ModeSystemCA}, true},
		"system-ca with pin":     {Policy{Origin: "https://a.example", Mode: ModeSystemCA, SPKIPin: "x"}, false},
		"https with socket":      {Policy{Origin: "https://a.example", CLISocket: "/tmp/s"}, false},
		"http with mode":         {Policy{Origin: "http://127.0.0.1:1", Mode: ModeSPKI}, false},
		"strict over http":       {Policy{Origin: "http://127.0.0.1:1", Profile: "strict"}, false},
		"unknown mode":           {Policy{Origin: "https://a.example", Mode: "any"}, false},
		"strict https spki":      {Policy{Origin: "https://a.example", Mode: ModeSPKI, Profile: "strict"}, true},
		"plain loopback no mode": {Policy{Origin: "http://127.0.0.1:1"}, true},
	} {
		if err := tc.policy.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestModeAndProfileJSONRefuseUnknownValues(t *testing.T) {
	var entry struct {
		Mode    Mode    `json:"mode"`
		Profile Profile `json:"profile"`
	}
	if err := json.Unmarshal([]byte(`{"mode":"system-ca","profile":"strict"}`), &entry); err != nil || entry.Mode != ModeSystemCA || entry.Profile != "strict" {
		t.Fatalf("entry %+v err %v", entry, err)
	}
	for _, raw := range []string{`{"mode":""}`, `{"mode":null}`, `{"profile":"weak"}`, `{"profile":""}`} {
		err := json.Unmarshal([]byte(raw), &entry)
		var field FieldError
		if err == nil || !errors.As(err, &field) {
			t.Errorf("%s accepted: %v", raw, err)
		}
	}
}

func TestCanonicalOriginAndLoopback(t *testing.T) {
	if got, err := CanonicalOrigin("HTTPS://Hikyo.Example:8443/"); err != nil || got != "https://hikyo.example:8443" {
		t.Fatalf("got %q err %v", got, err)
	}
	for _, raw := range []string{"ftp://a", "https://", "https://u:p@a", "https://a/path", "https://a?q=1", "https://a#f"} {
		if _, err := CanonicalOrigin(raw); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
	for origin, want := range map[string]bool{"http://localhost:1": true, "http://[::1]:8080": true, "http://127.0.0.2": true, "http://example.com": false} {
		if IsLoopbackOrigin(origin) != want {
			t.Errorf("IsLoopbackOrigin(%q) != %t", origin, want)
		}
	}
}

func TestFirstContactPromptNamesOriginModeAndIdentity(t *testing.T) {
	pin := strings.Repeat("A", 44)
	spki := FirstContactPrompt("https://a.example", ModeSPKI, pin, "")
	for _, want := range []string{"https://a.example", "spki", ShortPin(pin), "no CA chain"} {
		if !strings.Contains(spki, want) {
			t.Errorf("spki prompt lacks %q: %s", want, spki)
		}
	}
	if ca := FirstContactPrompt("https://a.example", ModeSystemCA, "", ""); !strings.Contains(ca, "system trusted CA chain") {
		t.Errorf("system-ca prompt: %s", ca)
	}
	if local := FirstContactPrompt("http://127.0.0.1:1", "", "", "/run/hikyo.sock"); !strings.Contains(local, "same-user local socket") || !strings.Contains(local, "/run/hikyo.sock") {
		t.Errorf("socket prompt: %s", local)
	}
}

// selfSigned returns a certificate with a key unrelated to the test server's.
func selfSigned(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "other"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func peers(certs ...*x509.Certificate) tls.ConnectionState {
	return tls.ConnectionState{PeerCertificates: certs}
}

func parseCert(t *testing.T, der []byte) *x509.Certificate {
	t.Helper()
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// A resumed session skips VerifyPeerCertificate; the pin must still hold.
func TestTLSConfigPinIsEnforcedOnResumedSessions(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.StartTLS()
	defer server.Close()
	addr := server.Listener.Addr().String()
	cache := tls.NewLRUClientSessionCache(4)
	dial := func(pin string) (*tls.Conn, error) {
		config := TLSConfig(server.URL, ModeSPKI, pin, "")
		config.ClientSessionCache = cache
		raw, err := (&tls.Dialer{Config: config}).DialContext(t.Context(), "tcp", addr)
		if err != nil {
			return nil, err
		}
		conn := raw.(*tls.Conn)
		// TLS 1.3 tickets arrive after the handshake; one read drains them.
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, _ = conn.Read(make([]byte, 1))
		return conn, nil
	}
	good := SPKIFingerprint(server.Certificate())
	first, err := dial(good)
	if err != nil {
		t.Fatalf("first handshake: %v", err)
	}
	_ = first.Close()
	second, err := dial(good)
	if err != nil {
		t.Fatalf("second handshake: %v", err)
	}
	if !second.ConnectionState().DidResume {
		t.Fatal("test premise broken: session was not resumed")
	}
	_ = second.Close()
	if conn, err := dial("AAAA"); err == nil {
		_ = conn.Close()
		t.Fatal("wrong pin accepted on a resumed session")
	}
}
