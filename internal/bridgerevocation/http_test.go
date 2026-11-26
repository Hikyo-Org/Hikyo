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
	// Nothing published yet: the list is absent, which is no answer (the signed not_after still bounds a bridge).
	missing := respond(http.StatusNotFound, "")
	if err := fetchFrom(missing); !errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("a 404 for the list must count as unreachable: %v", err)
	}
	missing.Close()
	// A list that is present without its signature is not an absence.
	unsigned := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sigstore.json") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("{}"))
	}))
	if err := fetchFrom(unsigned); err == nil || errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("a list without its signature fails closed: %v", err)
	}
	unsigned.Close()
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
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
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
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
	if err := hop("https://objects.githubusercontent.com/x", 2); err != nil {
		t.Fatalf("exactly two redirects are followed: %v", err)
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

func TestUnrecordableStateRefuses(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	directory := t.TempDir()
	fetcher := &scripted{}
	fetcher.list, fetcher.signature = signedList(t, f, 1)
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	if _, err := checker.Check(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	// A run that can take the lock but cannot record its outcome refuses: the check record
	// is a non-empty directory, so the atomic rename onto it fails.
	record := filepath.Join(directory, "bridge-revocations-check.json")
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(record, "occupied"), 0o700); err != nil {
		t.Fatal(err)
	}
	off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory, Now: func() time.Time { return testNow }}
	if _, err := off.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "bridge-revocations-check.json") {
		t.Fatalf("a run that cannot record the outcome must refuse with the record error: %v", err)
	}
}

// Once a list is stored, the list later going absent (404) falls back to the stored list
// and its sequence floor: its entries still apply and an older list is still a rollback.
// A list that is present but invalid still refuses.
func TestAbsentListFallsBackToTheStoredListAndItsFloor(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	bridge := releaseidentity.Hash([]byte("bridge"))
	list, signature := signedList(t, f, 5, bridge)
	mode := "list"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case mode == "absent":
			http.NotFound(w, r)
		case mode == "invalid":
			_, _ = w.Write([]byte("{"))
		case strings.HasSuffix(r.URL.Path, ".sigstore.json"):
			_, _ = w.Write(signature)
		default:
			_, _ = w.Write(list)
		}
	}))
	defer server.Close()
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcherFor(t, server.URL+"/r", server), Directory: t.TempDir(), Now: func() time.Time { return testNow }}
	if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Outcome != bridgerevocation.Checked || result.Sequence != 5 {
		t.Fatalf("the list is applied and stored: %+v %v", result, err)
	}
	mode = "absent"
	result, err := checker.Check(t.Context(), snapshot)
	if err != nil || result.Outcome != bridgerevocation.Unreachable || result.Sequence != 5 || !bridgerevocation.Contains(result.Revoked, []releaseidentity.Digest{bridge}) {
		t.Fatalf("an absent list falls back to the stored list and its floor: %+v %v", result, err)
	}
	mode = "invalid"
	if _, err := checker.Check(t.Context(), snapshot); err == nil {
		t.Fatal("a present but invalid list must refuse, not fall back")
	}
}

// A 404 at the end of a redirect is judged like a 404 at the endpoint: the list being
// absent is no answer, its signature being absent is invalid. (The redirect policy is
// opened for the test server; the hosts it allows are pinned by TestOnlyReleaseAssetRedirectsAreFollowed.)
func TestNotFoundAtARedirectTargetIsJudgedLikeNotFoundAtTheEndpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/gone", http.StatusFound)
	}))
	defer server.Close()
	fetcher := fetcherFor(t, server.URL+"/r", server)
	fetcher.Client.CheckRedirect = nil
	if _, _, err := fetcher.Fetch(t.Context()); !errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("a list that redirects to nothing is absent: %v", err)
	}
	signatureGone := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/gone":
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, ".sigstore.json"):
			http.Redirect(w, r, "/gone", http.StatusFound)
		default:
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer signatureGone.Close()
	fetcher = fetcherFor(t, signatureGone.URL+"/r", signatureGone)
	fetcher.Client.CheckRedirect = nil
	if _, _, err := fetcher.Fetch(t.Context()); err == nil || errors.Is(err, bridgerevocation.ErrUnreachable) {
		t.Fatalf("a signature that redirects to nothing fails closed: %v", err)
	}
}

// The fetcher deliberately honors the proxy environment (see NewHTTPFetcher): a
// proxy-only host would otherwise treat the endpoint as unreachable.
func TestFetcherHonorsTheProxyEnvironment(t *testing.T) {
	fetcher := fetcherFor(t, "https://revocations.example/list", nil)
	transport, ok := fetcher.Client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatal("the production fetcher must resolve its proxy from the environment")
	}
}
