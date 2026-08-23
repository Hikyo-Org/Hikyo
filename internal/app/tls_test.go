package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

func TestCertReloaderSwapsAtomicallyAndRetainsLastKnownGood(t *testing.T) {
	dir := t.TempDir()
	firstCert, firstKey, firstLeaf := tlstest.MintServerCert(t, "127.0.0.1")
	certPath, keyPath := tlstest.WritePair(t, dir, firstCert, firstKey)
	reloader, err := newCertReloader(certPath, keyPath, testLogger(), 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	secondCert, secondKey, secondLeaf := tlstest.MintServerCert(t, "127.0.0.1")
	if err := os.WriteFile(certPath, secondCert, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, secondKey, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reloader.reload(); err != nil {
		t.Fatal(err)
	}
	served, err := reloader.getCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if served.Leaf.SerialNumber.Cmp(secondLeaf.SerialNumber) != 0 || served.Leaf.SerialNumber.Cmp(firstLeaf.SerialNumber) == 0 {
		t.Fatalf("served serial = %s, want second serial %s", served.Leaf.SerialNumber, secondLeaf.SerialNumber)
	}
	if err := os.WriteFile(certPath, []byte("truncated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reloader.reload(); err == nil {
		t.Fatal("malformed replacement must fail")
	}
	served, err = reloader.getCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if served.Leaf.SerialNumber.Cmp(secondLeaf.SerialNumber) != 0 {
		t.Fatalf("failed reload replaced live certificate with serial %s", served.Leaf.SerialNumber)
	}
	_, failures := reloader.TLSMetrics()
	if failures != 1 {
		t.Fatalf("reload failures = %d, want 1", failures)
	}
}

func TestCertReloaderPollsForReplacementFiles(t *testing.T) {
	dir := t.TempDir()
	firstCert, firstKey, _ := tlstest.MintServerCert(t, "127.0.0.1")
	certPath, keyPath := tlstest.WritePair(t, dir, firstCert, firstKey)
	reloader, err := newCertReloader(certPath, keyPath, testLogger(), 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	secondCert, secondKey, secondLeaf := tlstest.MintServerCert(t, "127.0.0.1")
	if err := os.WriteFile(certPath, secondCert, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, secondKey, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(certPath, future, future); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go reloader.run(ctx)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		served, err := reloader.getCertificate(nil)
		if err != nil {
			t.Fatal(err)
		}
		if served.Leaf.SerialNumber.Cmp(secondLeaf.SerialNumber) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("poller did not load replacement certificate")
}

func TestNativeTLSBootServesHTTPSAndKeepsOpsPlaintext(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "127.0.0.1")
	certPath, keyPath := tlstest.WritePair(t, dir, certPEM, keyPEM)
	environment := map[string]string{
		"HIKYO_DB":                 "sqlite:" + filepath.Join(dir, "hikyo.db"),
		"HIKYO_OPERATIONAL_LISTEN": "localhost:0",
		"HIKYO_TLS_CERT_FILE":      certPath,
		"HIKYO_TLS_KEY_FILE":       keyPath,
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
	go func() { done <- srv.Serve(serveCtx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve shutdown: %v", err)
		}
	}()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("test CA was not parsed")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	resp, err := client.Get("https://" + srv.Addr + "/api/v1/meta")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTPS metadata status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("loopback HSTS = %q, want absent", got)
	}
	operationalResp, err := http.Get("http://" + srv.OperationalAddr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	operationalResp.Body.Close()
	if operationalResp.StatusCode != http.StatusOK {
		t.Fatalf("operational health status = %d", operationalResp.StatusCode)
	}
}

func TestOperationalListenerFailureStopsWholeLifecycle(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	if err := srv.operationalLn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("unexpected operational listener failure returned nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not surface operational listener failure")
	}
}
