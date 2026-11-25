package bridgerevocation_test

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// fetcherFor is the production client (https only, release-host redirects only, its timeouts)
// trusting only the given test server's certificate, if any.
func fetcherFor(t *testing.T, base string, server *httptest.Server) bridgerevocation.HTTPFetcher {
	t.Helper()
	fetcher, err := bridgerevocation.NewHTTPFetcher(base)
	if err != nil {
		t.Fatal(err)
	}
	if server != nil {
		transport, ok := fetcher.Client.Transport.(*http.Transport)
		if !ok {
			t.Fatal("the production client must use an *http.Transport")
		}
		pool := x509.NewCertPool()
		pool.AddCert(server.Certificate())
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return fetcher
}

func TestHTTPFetcherSeparatesUnreachableFromInvalid(t *testing.T) {
	respond := func(status int, body string) *httptest.Server {
		return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}
	fetchFrom := func(server *httptest.Server) error {
		_, _, err := fetcherFor(t, server.URL+"/r", server).Fetch(t.Context())
		return err
	}
	ok := respond(http.StatusOK, "{}")
	defer ok.Close()
	if err := fetchFrom(ok); err != nil {
		t.Fatalf("200 is an answer: %v", err)
	}
	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		server := respond(status, "")
		if err := fetchFrom(server); !errors.Is(err, bridgerevocation.ErrUnreachable) {
			t.Fatalf("status %d must count as unreachable: %v", status, err)
		}
		server.Close()
	}
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusInternalServerError} {
		server := respond(status, "")
		if err := fetchFrom(server); err == nil || errors.Is(err, bridgerevocation.ErrUnreachable) {
			t.Fatalf("status %d is reachable but invalid and must fail closed: %v", status, err)
		}
		server.Close()
	}
	// A redirect to a valid answer must not be followed: it is refused outright.
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			_, _ = w.Write([]byte("{}"))
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer redirect.Close()
	if err := fetchFrom(redirect); err == nil || errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("a redirect is reachable but invalid and must not be followed: %v", err)
	}
	large := respond(http.StatusOK, strings.Repeat("x", 70<<10))
	if err := fetchFrom(large); err == nil || errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("an oversized body is invalid: %v", err)
	}
	large.Close()

	// A server whose certificate the production client does not trust answered, wrongly.
	untrusted := respond(http.StatusOK, "{}")
	if _, _, err := fetcherFor(t, untrusted.URL+"/r", nil).Fetch(t.Context()); err == nil || errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("a certificate failure is not a fallback case: %v", err)
	}
	untrusted.Close()

	// A response that begins and is cut short is an answer that cannot be trusted.
	truncated := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte(`{"schema":`))
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hijacker.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer truncated.Close()
	if err := fetchFrom(truncated); err == nil || errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("a truncated body fails closed: %v", err)
	}
	// A stall after the response began is a timeout: no answer.
	stalledBody := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte(`{"schema":`))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer stalledBody.Close()
	slowBody := fetcherFor(t, stalledBody.URL+"/r", stalledBody)
	slowBody.Client.Timeout = 300 * time.Millisecond
	if _, _, err := slowBody.Fetch(t.Context()); !errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("a timeout while reading the body is unreachable: %v", err)
	}

	// Closed port, reset before any response, immediate close and a stalled
	// server are all "no answer".
	listen := func() net.Listener {
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		return listener
	}
	closedListener := listen()
	closed := "https://" + closedListener.Addr().String() + "/r"
	_ = closedListener.Close()
	if _, _, err := fetcherFor(t, closed, nil).Fetch(t.Context()); !errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("connection refused is unreachable: %v", err)
	}
	for name, linger := range map[string]bool{"reset on accept": true, "close on accept": false} {
		listener := listen()
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				if tcp, ok := conn.(*net.TCPConn); ok && linger {
					_ = tcp.SetLinger(0)
				}
				_ = conn.Close()
			}
		}()
		_, _, err := fetcherFor(t, "https://"+listener.Addr().String()+"/r", nil).Fetch(t.Context())
		_ = listener.Close()
		if !errors.Is(err, bridgerevocation.ErrUnreachable) {
			t.Fatalf("%s before any response is unreachable: %v", name, err)
		}
	}
	stall := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer stall.Close()
	stalled := fetcherFor(t, stall.URL+"/r", stall)
	stalled.Client.Timeout = 200 * time.Millisecond
	if _, _, err := stalled.Fetch(t.Context()); !errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("a timeout is unreachable: %v", err)
	}

	for _, bad := range []string{"http://hikyo.dev/r", "ftp://x/r", "https://user:pw@hikyo.dev/r", "https:///r", "::"} {
		if _, err := bridgerevocation.NewHTTPFetcher(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	if _, err := bridgerevocation.NewHTTPFetcher(bridgerevocation.DefaultURL); err != nil {
		t.Fatalf("the pinned default must be accepted: %v", err)
	}
	if bridgerevocation.DefaultURL != "https://github.com/Hikyo-Org/Hikyo/releases/download/trust/recovery-revocations" {
		t.Fatalf("the default is the fixed trust release asset: %s", bridgerevocation.DefaultURL)
	}
}

// GitHub serves release assets through a redirect to its asset host: that and
// nothing else may be followed.
func TestOnlyReleaseAssetRedirectsAreFollowed(t *testing.T) {
	fetcher, err := bridgerevocation.NewHTTPFetcher(bridgerevocation.DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	hop := func(target string, via int) error {
		request, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		return fetcher.Client.CheckRedirect(request, make([]*http.Request, via))
	}
	for _, allowed := range []string{"https://release-assets.githubusercontent.com/x?sig=1", "https://objects.githubusercontent.com/x", "https://github.com/Hikyo-Org/Hikyo/releases/download/trust/recovery-revocations.json"} {
		if err := hop(allowed, 1); err != nil {
			t.Fatalf("%s must be followed: %v", allowed, err)
		}
	}
	for _, refused := range []string{"http://github.com/x", "https://evil.example/x", "https://github.com.evil.example/x", "https://raw.githubusercontent.com/x"} {
		if err := hop(refused, 1); !errors.Is(err, http.ErrUseLastResponse) {
			t.Fatalf("%s must not be followed: %v", refused, err)
		}
	}
	if err := hop("https://github.com/x", 3); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("a third redirect must not be followed: %v", err)
	}
}

func TestStaleListFallsBackToExpiryAndNeverCountsAsChecked(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	bridge := releaseidentity.Hash([]byte("bridge"))
	directory := t.TempDir()
	fetcher := &scripted{}
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}

	// An endpoint replays an old, validly signed list to an installation that has
	// stored nothing (the v1.0.0 case): the sequence floor cannot tell, its age can.
	fetcher.list, fetcher.signature = signedListAt(t, f, testNow.Add(-bridgerevocation.MaxListAge-time.Second), 1)
	result, err := checker.Check(t.Context(), snapshot)
	if err != nil || result.Outcome != bridgerevocation.Stale {
		t.Fatalf("an old list is stale, never checked: %+v %v", result, err)
	}
	if outcome, _, _, err := bridgerevocation.LastCheck(directory); err != nil || outcome != bridgerevocation.Stale {
		t.Fatalf("the stale outcome is recorded: %v %v", outcome, err)
	}
	// Exactly at the limit is still current.
	fetcher.list, fetcher.signature = signedListAt(t, f, testNow.Add(-bridgerevocation.MaxListAge), 2, bridge)
	result, err = checker.Check(t.Context(), snapshot)
	if err != nil || result.Outcome != bridgerevocation.Checked || !bridgerevocation.Contains(result.Revoked, []releaseidentity.Digest{bridge}) {
		t.Fatalf("a list at the age limit is current: %+v %v", result, err)
	}
	// A stale list's entries still apply (revoking more is always safe).
	later := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: t.TempDir(), Now: func() time.Time { return testNow.Add(bridgerevocation.MaxListAge + time.Hour) }}
	result, err = later.Check(t.Context(), snapshot)
	if err != nil || result.Outcome != bridgerevocation.Stale || !bridgerevocation.Contains(result.Revoked, []releaseidentity.Digest{bridge}) {
		t.Fatalf("a stale list keeps its entries: %+v %v", result, err)
	}
	// The clock-skew allowance is exactly 24 hours: at the limit is accepted,
	// one second beyond is refused.
	skew := 24 * time.Hour
	fetcher.list, fetcher.signature = signedListAt(t, f, testNow.Add(skew), 3)
	if _, err := checker.Check(t.Context(), snapshot); err != nil {
		t.Fatalf("a list issued at the skew limit is accepted: %v", err)
	}
	for sequence, issued := range map[int64]time.Time{4: testNow.Add(skew + time.Second), 5: testNow.Add(48 * time.Hour)} {
		fetcher.list, fetcher.signature = signedListAt(t, f, issued, sequence)
		if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "future") {
			t.Fatalf("a list issued beyond the skew limit is refused: %v", err)
		}
	}
}

func TestStoredStateFailuresRefuse(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	directory := t.TempDir()
	fetcher := &scripted{}
	fetcher.list, fetcher.signature = signedList(t, f, 1)
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	if _, err := checker.Check(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	// A stored list whose signature is missing is not skipped.
	if err := os.Remove(filepath.Join(directory, "bridge-revocations.sigstore.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "lacks its signature") {
		t.Fatalf("a stored list without its signature must refuse: %v", err)
	}
	// A state directory that cannot record the outcome refuses.
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(readOnly, 0o700) }()
	off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: readOnly}
	if os.Geteuid() == 0 {
		t.Skip("root writes through mode 0500")
	}
	if _, err := off.Check(t.Context(), snapshot); err == nil {
		t.Fatal("a state directory that cannot record the outcome must refuse")
	}
}
