package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

const listenerCanary = "Bearer canary-credential-1127-listener"

// peerClass is a client stack a real caller might use against Hikyo.
type peerClass struct {
	name string
	cfg  func(roots *x509.CertPool) *tls.Config
	// strictOK is whether a strict listener must serve it.
	strictOK bool
}

func peerClasses() []peerClass {
	return []peerClass{
		{"compatible hybrid peer", func(r *x509.CertPool) *tls.Config { return &tls.Config{RootCAs: r, MinVersion: tls.VersionTLS12} }, true},
		{"TLS 1.3 only classical peer", func(r *x509.CertPool) *tls.Config {
			return &tls.Config{RootCAs: r, MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256}}
		}, false},
		{"TLS 1.2 only peer", func(r *x509.CertPool) *tls.Config {
			return &tls.Config{RootCAs: r, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}
		}, false},
	}
}

type listenerRecord struct {
	mu          sync.Mutex
	hits        int
	credentials []string
	seen        []tlspolicy.Negotiation
}

// serveCertificate serves the managed certificate's real tlsConfig over HTTPS and
// records what the handler and the observation hook saw.
func serveCertificate(t *testing.T, certificate *managedCertificate) (string, *listenerRecord) {
	t.Helper()
	record := &listenerRecord{}
	certificate.observe = func(n tlspolicy.Negotiation) {
		record.mu.Lock()
		record.seen = append(record.seen, n)
		record.mu.Unlock()
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record.mu.Lock()
		record.hits++
		record.credentials = append(record.credentials, r.Header.Get("Authorization"))
		record.mu.Unlock()
	}), ErrorLog: log.New(io.Discard, "", 0)}
	go func() { _ = server.Serve(tls.NewListener(raw, certificate.tlsConfig())) }()
	t.Cleanup(func() { _ = server.Close() })
	return raw.Addr().String(), record
}

func TestNativeListenerProfilesAgainstRealPeers(t *testing.T) {
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "127.0.0.1")
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	for _, profile := range []tlspolicy.Profile{tlspolicy.ProfileCompat, tlspolicy.ProfileStrict} {
		certificate, err := newManagedCertificate(string(certPEM), string(keyPEM), profile)
		if err != nil {
			t.Fatal(err)
		}
		addr, record := serveCertificate(t, certificate)
		for _, peer := range peerClasses() {
			t.Run(string(profile)+"/"+peer.name, func(t *testing.T) {
				before := record.hits
				transport := &http.Transport{TLSClientConfig: peer.cfg(roots), DisableKeepAlives: true}
				defer transport.CloseIdleConnections()
				req, _ := http.NewRequest(http.MethodGet, "https://"+addr+"/", nil)
				req.Header.Set("Authorization", listenerCanary)
				resp, err := transport.RoundTrip(req)
				wantOK := profile == tlspolicy.ProfileCompat || peer.strictOK
				if (err == nil) != wantOK {
					t.Fatalf("profile %s peer %q: served=%v, want %v (err=%v)", profile, peer.name, err == nil, wantOK, err)
				}
				record.mu.Lock()
				defer record.mu.Unlock()
				if err != nil {
					// The refusal came from the server's handshake: its handler
					// never ran and never saw the credential.
					if record.hits != before {
						t.Fatal("a refused peer's request reached the handler")
					}
					return
				}
				defer resp.Body.Close()
				last := record.seen[len(record.seen)-1]
				if last.Hop != tlspolicy.HopNativeListener || last.Profile != profile || last.Version == "" || last.Group == "" {
					t.Fatalf("observation %+v", last)
				}
				if profile == tlspolicy.ProfileStrict && (last.Version != "tls1.3" || last.Group != "x25519mlkem768") {
					t.Fatalf("strict served %+v", last)
				}
			})
		}
		record.mu.Lock()
		for _, c := range record.credentials {
			if profile == tlspolicy.ProfileStrict && c != listenerCanary {
				t.Fatalf("unexpected credential %q", c)
			}
		}
		if profile == tlspolicy.ProfileStrict && record.hits != 1 {
			t.Fatalf("strict listener handled %d requests, want exactly the hybrid peer's", record.hits)
		}
		record.mu.Unlock()
	}
}

func TestStrictProfileRefusesAListenerWithoutCertificate(t *testing.T) {
	if _, err := newManagedCertificate("", "", tlspolicy.ProfileStrict); err == nil {
		t.Fatal("strict with no certificate must fail closed")
	}
	if c, err := newManagedCertificate("", "", tlspolicy.ProfileCompat); err != nil || c != nil {
		t.Fatalf("compat plaintext must stay valid: %v", err)
	}
}

func TestProfileChangeReplacesTheListenerTransport(t *testing.T) {
	a := &config.Config{TLSCertPEM: "c", TLSKeyPEM: "k", TLSProfile: config.TLSProfileCompat}
	b := &config.Config{TLSCertPEM: "c", TLSKeyPEM: "k", TLSProfile: config.TLSProfileStrict}
	if !listenerTransportChanged(a, b) || !listenerTransportChanged(b, a) {
		t.Fatal("a profile-only change must replace the transport and close existing connections")
	}
	if listenerTransportChanged(a, &config.Config{TLSCertPEM: "c", TLSKeyPEM: "k", TLSProfile: config.TLSProfileCompat}) {
		t.Fatal("an unchanged transport must not churn connections")
	}
}

func TestConfigAndPolicyVocabulariesAgree(t *testing.T) {
	if config.TLSProfileCompat != string(tlspolicy.ProfileCompat) || config.TLSProfileStrict != string(tlspolicy.ProfileStrict) {
		t.Fatal("config and tlspolicy disagree on profile names")
	}
}

func TestBootWithStrictProfileServesOnlyHybridPeers(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "127.0.0.1")
	certPath, keyPath := tlstest.WritePair(t, dir, certPEM, keyPEM)
	environment := map[string]string{
		"HIKYO_DB":                 "sqlite:" + filepath.Join(dir, "hikyo.db"),
		"HIKYO_OPERATIONAL_LISTEN": "localhost:0",
		"HIKYO_TLS_CERT_FILE":      certPath,
		"HIKYO_TLS_KEY_FILE":       keyPath,
		"HIKYO_TLS_PROFILE":        "strict",
	}
	cfg, _, err := config.Load("server", []string{"--dev", "--listen", "127.0.0.1:0"}, func(key string) string { return environment[key] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.ServeWithReady(serveCtx, nil) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve shutdown: %v", err)
		}
	}()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	for _, peer := range peerClasses() {
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: peer.cfg(roots), DisableKeepAlives: true}}
		req, _ := http.NewRequest(http.MethodGet, "https://"+srv.Addr+"/api/v1/meta", nil)
		req.Header.Set("Authorization", listenerCanary)
		resp, err := client.Do(req)
		if (err == nil) != peer.strictOK {
			t.Fatalf("%s: served=%v, want %v (%v)", peer.name, err == nil, peer.strictOK, err)
		}
		if err == nil {
			if resp.TLS.Version != tls.VersionTLS13 || resp.TLS.CurveID != tls.X25519MLKEM768 {
				t.Fatalf("strict Boot negotiated %x %v", resp.TLS.Version, resp.TLS.CurveID)
			}
			resp.Body.Close()
		}
	}
}

func TestListenerReportsConfiguredAndObservedEvidenceSeparately(t *testing.T) {
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "127.0.0.1")
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	certificate, err := newManagedCertificate(string(certPEM), string(keyPEM), tlspolicy.ProfileStrict)
	if err != nil {
		t.Fatal(err)
	}
	var out syncBuffer
	certificate.reportTo(slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.NotFoundHandler(), ErrorLog: log.New(io.Discard, "", 0)}
	go func() { _ = server.Serve(tls.NewListener(raw, certificate.tlsConfig())) }()
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DisableKeepAlives: true}
	resp, err := (&http.Client{Transport: transport}).Get("https://" + raw.Addr().String() + "/?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	logged := out.String()
	for _, want := range []string{
		"evidence=configured", "profile=strict", "classical certificate chain; reported separately",
		"evidence=observed", "version=tls1.3", "group=x25519mlkem768", "hop=native-listener",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log lacks %q:\n%s", want, logged)
		}
	}
	if strings.Contains(logged, "secret") || strings.Contains(logged, "127.0.0.1") {
		t.Fatalf("log leaked a request or peer detail:\n%s", logged)
	}
}
