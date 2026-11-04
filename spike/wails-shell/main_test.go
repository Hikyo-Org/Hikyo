package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTakeRotatedToken(t *testing.T) {
	cases := []struct{ name, in, token, absent string }{
		{"top level", `{"session_token":"new","session_id":"s"}`, "new", "new"},
		{"under login", `{"recovery_codes":["c"],"login":{"session_token":"new","session":{}}}`, "new", "new"},
		{"no token", `{"session_id":"s"}`, "", ""},
		{"secret named session_token stays", `{"values":{"session_token":"keep"}}`, "", ""},
		{"not an object", `[1,2]`, "", ""},
	}
	for _, c := range cases {
		out, token := takeRotatedToken([]byte(c.in))
		if token != c.token {
			t.Errorf("%s: token %q, want %q", c.name, token, c.token)
		}
		if c.absent != "" && strings.Contains(string(out), c.absent) {
			t.Errorf("%s: token left in body: %s", c.name, out)
		}
		if c.token == "" && string(out) != c.in {
			t.Errorf("%s: body changed: %s", c.name, out)
		}
	}
}

func TestShellRefusals(t *testing.T) {
	var upstream atomic.Int32
	var seen http.Header
	instance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.Add(1)
		seen = r.Header.Clone()
		http.SetCookie(w, &http.Cookie{Name: "__Host-hikyo", Value: "x"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_token":"hik_1_rotated","session_id":"s"}`))
	}))
	defer instance.Close()
	proxies := map[string]*conn{"conn-0": newConn("conn-0", session{Origin: instance.URL, Token: "hik_1_first"})}
	handler := shell(proxies, map[string]string{"1": "conn-0", "2": "conn-1"}, false)(http.NotFoundHandler())

	do := func(method, host, path string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Host = host
		r.Header.Set("x-wails-window-id", "1")
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	refused := []struct {
		name, host, path string
		headers          map[string]string
		code             int
	}{
		{"unknown host", "conn-9", "/", nil, 404},
		{"other connection's origin", "conn-0", "/api", map[string]string{"Origin": "wails://conn-1"}, 403},
		{"external origin", "conn-0", "/api", map[string]string{"Origin": "https://evil.example"}, 403},
		{"other connection's window", "conn-0", "/", map[string]string{"x-wails-window-id": "2"}, 403},
		{"no window", "conn-0", "/", map[string]string{"x-wails-window-id": ""}, 403},
		{"wails runtime", "conn-0", "/wails/runtime", nil, 403},
	}
	for _, c := range refused {
		if w := do("GET", c.host, c.path, c.headers); w.Code != c.code {
			t.Errorf("%s: status %d, want %d", c.name, w.Code, c.code)
		}
	}
	if upstream.Load() != 0 {
		t.Fatalf("%d refused requests reached the instance", upstream.Load())
	}

	w := do("POST", "conn-0", "/api", map[string]string{"Origin": "wails://conn-0", "Cookie": "a=b", "Referer": "wails://conn-0/"})
	if w.Code != 200 || strings.Contains(w.Body.String(), "hik_1_") || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("response leaked: %d %q %q", w.Code, w.Body.String(), w.Header().Get("Set-Cookie"))
	}
	if seen.Get("Authorization") != "Bearer hik_1_first" || seen.Get("Cookie") != "" || seen.Get("Origin") != "" || seen.Get("Referer") != "" {
		t.Fatalf("instance saw %v", seen)
	}
	do("GET", "conn-0", "/api", nil)
	if seen.Get("Authorization") != "Bearer hik_1_rotated" {
		t.Fatalf("rotation not applied: %q", seen.Get("Authorization"))
	}
}

func TestOpenExternalValidation(t *testing.T) {
	t.Setenv("HIKYO_SPIKE_BROWSER", "print")
	ok := []string{"https://example.com/a?b=c", "http://localhost:58090/reauth/cli?transaction=x", "HTTPS://Example.com/"}
	bad := []string{"", "file:///etc/passwd", "javascript:alert(1)", "wails://conn-1/", "custom-app://x", "https://user:pw@example.com/", "https:///path", "//example.com/x", "https:example.com", "blob:wails://conn-0/x", "/relative", "https://exa mple.com/"}
	for _, raw := range ok {
		if err := openExternal(raw); err != nil {
			t.Errorf("%q refused: %v", raw, err)
		}
	}
	for _, raw := range bad {
		if err := openExternal(raw); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
}

func TestDownloadPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIKYO_SPIKE_DOWNLOAD_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "report.csv"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"export.json":        "export.json",
		"../../etc/passwd":   "passwd",
		`..\..\win.ini`:      "win.ini",
		"/abs/path/file.txt": "file.txt",
		".hidden":            "hidden",
		"..":                 "download",
		"":                   "download",
		"report.csv":         "report (1).csv",
	}
	for in, want := range cases {
		if got, err := downloadPath(in); err != nil || got != filepath.Join(dir, want) {
			t.Errorf("%q -> %s, want %s", in, got, want)
		}
	}
}

// fakeInstance implements the two cli-reauth endpoints the way the CLI's server does:
// PKCE-checked, single-use code, rotated bearer.
type fakeInstance struct {
	*httptest.Server
	redirect, state, challenge string
	redeems                    atomic.Int32
	bearers                    chan string
}

func newFakeInstance(t *testing.T) *fakeInstance {
	f := &fakeInstance{bearers: make(chan string, 8)}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case f.bearers <- r.Header.Get("Authorization"):
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/api/v1/auth/cli-reauth/start":
			f.redirect, _ = body["redirect_uri"].(string)
			f.challenge, _ = body["pkce_challenge"].(string)
			f.state = "state-1"
			_, _ = w.Write([]byte(`{"state":"state-1","expires_at":"2099-01-01T00:00:00Z"}`))
		case "/api/v1/auth/cli-reauth/redeem":
			digest := sha256.Sum256([]byte(body["pkce_verifier"].(string)))
			if body["code"] != "code-1" || base64.RawURLEncoding.EncodeToString(digest[:]) != f.challenge || f.redeems.Add(1) > 1 {
				http.Error(w, `{"error":"no"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"session_id":"s2","session_token":"hik_2_rotated","windows":[{"environment_id":"e1","session_token":"hik_2_rotated"}]}`))
		default:
			t.Errorf("unexpected instance path %s", r.URL.Path)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func get(target string) int {
	resp, err := http.Get(target)
	if err != nil {
		return 0 // the listener is gone, which also means it cannot be reused
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestReauthHandoff(t *testing.T) {
	inst := newFakeInstance(t)
	c := newConn("conn-0", session{Origin: inst.URL, Token: "hik_1_first"})
	handler := shell(map[string]*conn{"conn-0": c}, map[string]string{"1": "conn-0"}, false)(http.NotFoundHandler())

	var opened string
	callbackStatuses := map[string]int{}
	probed := make(chan struct{})
	openBrowser = func(target string) error {
		opened = target
		cb := inst.redirect
		go func() {
			good := cb + "?code=code-1&state=state-1"
			callbackStatuses["wrong state"] = get(cb + "?code=code-1&state=other")
			callbackStatuses["extra param"] = get(good + "&x=1")
			callbackStatuses["no code"] = get(cb + "?state=state-1")
			callbackStatuses["duplicate code"] = get(cb + "?code=a&code=b&state=state-1")
			callbackStatuses["good"] = get(good)
			callbackStatuses["reused"] = get(good)
			close(probed)
		}()
		return nil
	}
	t.Cleanup(func() { openBrowser = openExternal })

	body := `{"purpose":"reveal","operation":"value.reveal","environment_ids":["e1"],"key_ids":["k1"]}`
	r := httptest.NewRequest("POST", "/__shell/reauth", strings.NewReader(body))
	r.Host = "conn-0"
	r.Header.Set("Origin", "wails://conn-0")
	r.Header.Set("x-wails-window-id", "1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "hik_") || !strings.Contains(w.Body.String(), `"environment_id":"e1"`) {
		t.Fatalf("page got %s", w.Body)
	}
	u, _ := url.Parse(opened)
	if u.Path != "/reauth/cli" || u.Query().Get("transaction") != "state-1" || u.Host != strings.TrimPrefix(inst.URL, "http://") {
		t.Fatalf("opened %s", opened)
	}
	select {
	case <-probed:
	case <-time.After(10 * time.Second):
		t.Fatal("callback probes did not finish")
	}
	for name, want := range map[string]int{"wrong state": 400, "extra param": 400, "no code": 400, "duplicate code": 400, "good": 200} {
		if callbackStatuses[name] != want {
			t.Errorf("callback %s: %d, want %d", name, callbackStatuses[name], want)
		}
	}
	if s := callbackStatuses["reused"]; s == 200 {
		t.Errorf("a used callback answered 200")
	}
	if inst.redeems.Load() != 1 || c.bearer() != "hik_2_rotated" {
		t.Fatalf("redeems %d bearer %q", inst.redeems.Load(), c.bearer())
	}
	if first := <-inst.bearers; first != "Bearer hik_1_first" {
		t.Fatalf("start used %q", first)
	}
}

func TestReauthOneAtATime(t *testing.T) {
	inst := newFakeInstance(t)
	c := newConn("conn-0", session{Origin: inst.URL, Token: "hik_1_first"})
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	openBrowser = func(string) error { once.Do(func() { close(started) }); <-release; return io.ErrClosedPipe }
	t.Cleanup(func() { openBrowser = openExternal })
	do := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/__shell/reauth", strings.NewReader(`{"purpose":"reveal","operation":"value.reveal","environment_ids":["e1"],"key_ids":["k1"]}`))
		w := httptest.NewRecorder()
		c.reauth(w, r)
		return w
	}
	done := make(chan int)
	go func() { done <- do().Code }()
	<-started
	if code := do().Code; code != http.StatusConflict {
		t.Errorf("second ceremony: %d, want 409", code)
	}
	close(release)
	if code := <-done; code != http.StatusBadGateway {
		t.Errorf("failed open: %d, want 502", code)
	}
	if c.bearer() != "hik_1_first" {
		t.Errorf("bearer changed on failure")
	}
	if do().Code == http.StatusConflict {
		t.Errorf("busy flag not released")
	}
}

func TestSaveThroughProxy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIKYO_SPIKE_DOWNLOAD_DIR", dir)
	var seen string
	instance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/export":
			w.Header().Set("Content-Disposition", `attachment; filename="../../bundle.json"`)
			_, _ = w.Write([]byte(`{"ok":` + r.URL.Query().Get("n") + `}`))
		default:
			http.Error(w, "no", http.StatusForbidden)
		}
	}))
	defer instance.Close()
	connections = map[string]*conn{"conn-0": newConn("conn-0", session{Origin: instance.URL, Token: "hik_1_first"})}
	t.Cleanup(func() { connections = nil })

	p, n, err := saveThroughProxy("wails://conn-0/api/export?n=7")
	if err != nil || p != filepath.Join(dir, "bundle.json") || n != 8 || seen != "Bearer hik_1_first" {
		t.Fatalf("saved %q %d %v with %q", p, n, err, seen)
	}
	if got, _ := os.ReadFile(p); string(got) != `{"ok":7}` {
		t.Fatalf("file %q", got)
	}
	if p2, _, err := saveThroughProxy("wails://conn-0/api/export?n=7"); err != nil || p2 != filepath.Join(dir, "bundle (1).json") {
		t.Fatalf("second save %q %v", p2, err)
	}
	for _, bad := range []string{"wails://conn-9/api/export", "https://conn-0/api/export", "wails://conn-0/api/denied", "%zz"} {
		if _, _, err := saveThroughProxy(bad); err == nil {
			t.Errorf("%q saved", bad)
		}
	}
}

func TestDownloadPathHostileNames(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIKYO_SPIKE_DOWNLOAD_DIR", dir)
	got, err := downloadPath(strings.Repeat("a", 5000) + ".txt")
	if err != nil || len(filepath.Base(got)) > 204 || !strings.HasSuffix(got, ".txt") {
		t.Fatalf("long name: %q %v", got, err)
	}
	got, err = downloadPath("re\u202Eadme\x00\n.txt")
	if err != nil || filepath.Base(got) != "readme.txt" {
		t.Fatalf("control characters: %q %v", got, err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HIKYO_SPIKE_DOWNLOAD_DIR", file) // Lstat gives ENOTDIR, which the old loop retried forever
	if got, err := downloadPath("a.txt"); err == nil {
		t.Fatalf("a bad directory must fail, not spin: %q", got)
	}
}

func TestSaveThroughProxyKeepsTheBearerOnItsOrigin(t *testing.T) {
	t.Setenv("HIKYO_SPIKE_DOWNLOAD_DIR", t.TempDir())
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Store(true) }))
	defer other.Close()
	instance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer instance.Close()
	connections = map[string]*conn{"conn-0": newConn("conn-0", session{Origin: instance.URL, Token: "hik_1_x"})}
	if _, _, err := saveThroughProxy("wails://conn-0/export"); err == nil {
		t.Fatal("a redirect must fail the download")
	}
	if leaked.Load() {
		t.Fatal("the request followed the redirect off the instance")
	}
}
