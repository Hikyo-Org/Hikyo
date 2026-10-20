package remotefetch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

func TestPairingIdentityProbeVerifiesTLSWithoutHTTPOrAmbientProxy(t *testing.T) {
	certPEM, keyPEM, leaf := tlstest.MintServerCert(t, "localhost", "127.0.0.1")
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	helloNames := make(chan string, 2)
	peer := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	peer.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		helloNames <- hello.ServerName
		return nil, nil
	}}
	peer.Config.ErrorLog = log.New(io.Discard, "", 0)
	peer.StartTLS()
	defer peer.Close()
	origin, err := url.Parse(peer.URL)
	if err != nil {
		t.Fatal(err)
	}
	origin.Host = net.JoinHostPort("localhost", origin.Port())
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("CA fixture invalid")
	}
	t.Setenv("HTTPS_PROXY", "https://must-not-be-contacted.invalid:1")
	t.Setenv("HTTP_PROXY", "https://must-not-be-contacted.invalid:1")
	client := testClient(t)
	before := Dials()
	pin, err := client.pairingSPKIPin(t.Context(), origin.String(), roots)
	if err != nil || pin != SPKIFingerprint(leaf) || Dials() != before+1 {
		t.Fatalf("verified TLS probe: %v", err)
	}
	if name := <-helloNames; name != "localhost" {
		t.Fatal("configured hostname was not used for TLS SNI")
	}
	if _, err := client.PairingSPKIPin(t.Context(), peer.URL, nil); err == nil {
		t.Fatal("untrusted test certificate accepted by production resolver")
	}
	if requests.Load() != 0 {
		t.Fatal("identity probe sent an HTTP request")
	}
}

func TestPairingIdentityProbeReturnsPublicCertificateRatherThanNativeAnchor(t *testing.T) {
	_, _, nativeLeaf := tlstest.MintServerCert(t, "127.0.0.1")
	publicPEM, publicKey, publicLeaf := tlstest.MintServerCert(t, "127.0.0.1")
	pair, err := tls.X509KeyPair(publicPEM, publicKey)
	if err != nil {
		t.Fatal(err)
	}
	peer := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("public TLS discovery sent HTTP") }))
	peer.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	peer.StartTLS()
	defer peer.Close()
	// A test CA models the system trust store for the externally terminated
	// certificate. The independent native leaf is an additional anchor only.
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(publicPEM) {
		t.Fatal("public CA fixture invalid")
	}
	roots.AddCert(nativeLeaf)
	pin, err := testClient(t).pairingSPKIPin(t.Context(), peer.URL, roots)
	if err != nil || pin != SPKIFingerprint(publicLeaf) || pin == SPKIFingerprint(nativeLeaf) {
		t.Fatalf("resolver inferred native key instead of observing public TLS: %v", err)
	}
}

func TestPairingIdentityProbeRejectsHostnameMismatch(t *testing.T) {
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "different.example")
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	peer := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("hostname mismatch reached HTTP") }))
	peer.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	peer.Config.ErrorLog = log.New(io.Discard, "", 0)
	peer.StartTLS()
	defer peer.Close()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	if _, err := testClient(t).pairingSPKIPin(t.Context(), peer.URL, roots); err == nil {
		t.Fatal("hostname mismatch accepted")
	}
}

func TestPairingIdentityProbeDeadlineAndCancellationBoundHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 2 {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, conn)
			_ = conn.Close()
		}
	}()
	cfg := DefaultConfig()
	cfg.Deadline = 100 * time.Millisecond
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	origin := "https://" + listener.Addr().String()
	started := time.Now()
	if _, err := client.PairingSPKIPin(t.Context(), origin, nil); err == nil {
		t.Fatal("stalled TLS handshake accepted")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("configured whole-operation deadline not enforced")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.PairingSPKIPin(ctx, origin, nil); err == nil {
		t.Fatal("cancelled context accepted")
	}
	_ = listener.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("probe retained a connection after deadline")
	}
}

func TestPairingIdentityProbeHonorsConfiguredProxyWithoutDirectFallback(t *testing.T) {
	var requests atomic.Int64
	peer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer peer.Close()
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted proxy reached CONNECT") }))
	defer proxy.Close()
	proxy.Config.ErrorLog = log.New(io.Discard, "", 0)
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Proxy = proxyURL
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(peer.Certificate())
	before := Dials()
	if _, err := client.pairingSPKIPin(t.Context(), peer.URL, roots); err == nil {
		t.Fatal("untrusted explicit proxy accepted")
	}
	if Dials() != before+1 || requests.Load() != 0 {
		t.Fatal("failed configured proxy fell back to direct target")
	}
}
