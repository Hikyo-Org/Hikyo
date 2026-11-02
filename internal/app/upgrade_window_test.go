package app

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

func TestUpgradeWindowTLSStatusReadinessAndSocketHandoff(t *testing.T) {
	cfg := devConfig(t)
	cert, key, leaf := tlstest.MintServerCert(t, "127.0.0.1")
	cfg.TLSCertPEM, cfg.TLSKeyPEM = string(cert), string(key)
	cfg.Listen, cfg.OperationalListen = "127.0.0.1:0", "127.0.0.1:0"
	window := NewUpgradeWindow()
	if err := window.Start(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = window.Close() })
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	get := func(url string, code int, contains string) {
		t.Helper()
		response, err := client.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != code || !strings.Contains(string(body), contains) {
			t.Fatalf("GET %s: status=%d body=%s err=%v", url, response.StatusCode, body, err)
		}
	}
	get("https://"+window.publicAddr+"/api/v1/runtime/status", 200, `"phase":"preparing"`)
	window.Progress("restore-check")
	get("https://"+window.publicAddr+"/api/v1/runtime/status", 200, `"phase":"restore-check"`)
	get("https://"+window.publicAddr+"/api/v1/me/profile", 503, "service_unavailable")
	get("https://"+window.publicAddr+"/mcp", 503, "service_unavailable")
	get("http://"+window.operationalAddr+"/healthz", 200, "")
	get("http://"+window.operationalAddr+"/readyz", 503, "")
	window.Progress("recovery-required")
	get("https://"+window.publicAddr+"/api/v1/runtime/status", 200, `"phase":null,"state":"recovery-required"`)
	transport.CloseIdleConnections()
	if err := window.Close(); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{window.publicAddr, window.operationalAddr} {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("socket not handed back: %v", err)
		}
		_ = listener.Close()
	}
}

func TestUpgradeWindowRejectsInvalidTLSBeforeBinding(t *testing.T) {
	cfg := devConfig(t)
	cfg.TLSCertPEM = "invalid"
	window := NewUpgradeWindow()
	if err := window.Start(cfg); err == nil || window.Started() {
		t.Fatal("invalid transport started maintenance listener")
	}
}

func TestUpgradeWindowHonorsTheStrictProfile(t *testing.T) {
	cfg := devConfig(t)
	cert, key, leaf := tlstest.MintServerCert(t, "127.0.0.1")
	cfg.TLSCertPEM, cfg.TLSKeyPEM, cfg.TLSProfile = string(cert), string(key), config.TLSProfileStrict
	cfg.Listen, cfg.OperationalListen = "127.0.0.1:0", "127.0.0.1:0"
	window := NewUpgradeWindow()
	if err := window.Start(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = window.Close() })
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	url := "https://" + window.publicAddr + "/api/v1/runtime/status"
	for name, tlsCfg := range map[string]*tls.Config{
		"classical TLS 1.3": {RootCAs: roots, MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}},
		"TLS 1.2":           {RootCAs: roots, MaxVersion: tls.VersionTLS12},
	} {
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg, DisableKeepAlives: true}, Timeout: 3 * time.Second}
		if resp, err := client.Get(url); err == nil {
			resp.Body.Close()
			t.Fatalf("%s peer was served by the strict maintenance listener", name)
		}
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DisableKeepAlives: true}, Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("hybrid peer refused: %v", err)
	}
	resp.Body.Close()
	if resp.TLS.Version != tls.VersionTLS13 || resp.TLS.CurveID != tls.X25519MLKEM768 {
		t.Fatalf("maintenance listener negotiated %x %v", resp.TLS.Version, resp.TLS.CurveID)
	}

	plain := devConfig(t)
	plain.TLSProfile = config.TLSProfileStrict
	plain.Listen, plain.OperationalListen = "127.0.0.1:0", "127.0.0.1:0"
	refused := NewUpgradeWindow()
	if err := refused.Start(plain); err == nil || refused.Started() {
		t.Fatal("strict without a certificate must not start a plaintext maintenance listener")
	}
}
