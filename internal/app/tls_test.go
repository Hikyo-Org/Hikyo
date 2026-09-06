package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

func TestManagedCertificateValidatesPairAndRetainsImmutableCertificate(t *testing.T) {
	firstCert, firstKey, firstLeaf := tlstest.MintServerCert(t, "127.0.0.1")
	first, err := newManagedCertificate(string(firstCert), string(firstKey))
	if err != nil {
		t.Fatal(err)
	}
	secondCert, secondKey, secondLeaf := tlstest.MintServerCert(t, "127.0.0.1")
	second, err := newManagedCertificate(string(secondCert), string(secondKey))
	if err != nil {
		t.Fatal(err)
	}
	if first.pair.Leaf.SerialNumber.Cmp(firstLeaf.SerialNumber) != 0 || second.pair.Leaf.SerialNumber.Cmp(secondLeaf.SerialNumber) != 0 {
		t.Fatal("candidate construction mutated another certificate")
	}
	if _, err := newManagedCertificate(string(secondCert), string(firstKey)); err == nil {
		t.Fatal("mismatched pair accepted")
	}
	if _, err := newManagedCertificate("truncated", string(secondKey)); err == nil {
		t.Fatal("malformed certificate accepted")
	}
	if first.pair.Leaf.SerialNumber.Cmp(firstLeaf.SerialNumber) != 0 {
		t.Fatal("failed candidate replaced certificate")
	}
}

func TestManagedCertificateExpiryMetricsAndExpiredPairRefusal(t *testing.T) {
	now := time.Now()
	cert, key, leaf := tlstest.MintServerCertWithValidity(t, now.Add(-time.Hour), now.Add(7*24*time.Hour), "localhost")
	certificate, err := newManagedCertificate(string(cert), string(key))
	if err != nil {
		t.Fatal(err)
	}
	expires, failures := certificate.TLSMetrics()
	if expires != leaf.NotAfter.Unix() || failures != 0 {
		t.Fatal("applied certificate metrics differ")
	}
	expired, expiredKey, _ := tlstest.MintServerCertWithValidity(t, now.Add(-48*time.Hour), now.Add(-time.Hour), "localhost")
	if _, err := newManagedCertificate(string(expired), string(expiredKey)); err == nil {
		t.Fatal("expired certificate accepted")
	}
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
	go func() { done <- srv.ServeWithReady(serveCtx, nil) }()
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
	go func() { done <- srv.ServeWithReady(ctx, nil) }()
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
