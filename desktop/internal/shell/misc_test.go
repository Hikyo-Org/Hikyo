package shell

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

func TestNavigatePolicy(t *testing.T) {
	h := newHarness(t)
	a, _ := h.connect()
	b, _ := h.connect()
	own := PageScheme + "://" + a.host
	for _, tc := range []struct {
		name string
		req  NavRequest
		want NavAction
	}{
		{"first load of a known host", NavRequest{URL: own + "/", MainFrame: true}, NavAllow},
		{"first load of an unknown host", NavRequest{URL: PageScheme + "://c-00000000000000000000000000000000/", MainFrame: true}, NavCancel},
		{"same host", NavRequest{PageHost: a.host, URL: own + "/orgs", MainFrame: true}, NavAllow},
		{"other connection", NavRequest{PageHost: a.host, URL: PageScheme + "://" + b.host + "/", MainFrame: true}, NavCancel},
		{"about:blank", NavRequest{PageHost: a.host, URL: "about:blank"}, NavAllow},
		{"file", NavRequest{PageHost: a.host, URL: "file:///etc/passwd", MainFrame: true, UserGesture: true}, NavCancel},
		{"javascript", NavRequest{PageHost: a.host, URL: "javascript:alert(1)", MainFrame: true, UserGesture: true}, NavCancel},
		{"http link", NavRequest{PageHost: a.host, URL: "http://example.com/", MainFrame: true, UserGesture: true}, NavCancel},
		{"https without gesture", NavRequest{PageHost: a.host, URL: "https://example.com/", MainFrame: true}, NavCancel},
		{"https from subframe", NavRequest{PageHost: a.host, URL: "https://example.com/", UserGesture: true}, NavCancel},
		{"https from the connection list", NavRequest{PageHost: AppHost, URL: "https://example.com/", MainFrame: true, UserGesture: true}, NavCancel},
		{"download of own blob", NavRequest{PageHost: a.host, URL: "blob:" + own + "/uuid", Download: true}, NavDownload},
		{"download of another blob", NavRequest{PageHost: a.host, URL: "blob:" + PageScheme + "://" + b.host + "/uuid", Download: true}, NavCancel},
		{"download from an unknown page", NavRequest{URL: "blob:" + own + "/uuid", Download: true}, NavCancel},
	} {
		if got := h.m.Navigate(tc.req); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := h.openedURLs(); len(got) != 0 {
		t.Fatalf("a refused navigation opened %v", got)
	}
	// An https link with a gesture leaves for the system browser, then is cancelled.
	if got := h.m.Navigate(NavRequest{PageHost: a.host, URL: "https://example.com/p#x", MainFrame: true, UserGesture: true}); got != NavCancel {
		t.Errorf("external link not cancelled: %v", got)
	}
	if got := h.openedURLs(); len(got) != 1 || got[0] != "https://example.com/p" {
		t.Errorf("opened %v", got)
	}
	// ...and is rate limited.
	for i := 0; i < 20; i++ {
		h.m.Navigate(NavRequest{PageHost: a.host, URL: "https://example.com/p", MainFrame: true, UserGesture: true})
	}
	if got := len(h.openedURLs()); got != 10 {
		t.Errorf("opened %d links, want the limit of 10", got)
	}
}

func TestClipboardClearsOnlyAnUnchangedPasteboard(t *testing.T) {
	n := newFakeNative()
	now := time.Unix(1000, 0)
	g := newClipboardGuard(n, func() time.Time { return now })
	var fire func()
	g.after = func(d time.Duration, f func()) func() {
		if d != clipboardClearAfter {
			t.Errorf("delay %v", d)
		}
		fire = f
		return func() {}
	}
	// Nothing copied: arm does nothing.
	g.arm()
	if fire != nil {
		t.Fatal("armed without a pasteboard change")
	}
	// The page copies a secret, then the shell sees the change.
	n.pasteboard++
	g.observe()
	g.arm()
	if fire == nil {
		t.Fatal("not armed after a change")
	}
	// The user copies something else before the timer: leave it alone.
	n.pasteboard++
	fire()
	if n.cleared != 0 {
		t.Error("cleared the user's newer clipboard")
	}
	// Unchanged: clear.
	n.pasteboard++
	g.observe()
	fire = nil
	g.arm()
	fire()
	if n.cleared != 1 {
		t.Errorf("cleared %d times", n.cleared)
	}
}

func TestStoreIsPrivateAndStrict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "connections.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	host, _ := s.NewHost()
	if err := s.Put(Connection{Host: host, Origin: "https://x.example", Name: "x", Trust: Trust{Mode: clienttrust.ModeSystemCA}}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v err %v", fi.Mode(), err)
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", di.Mode())
	}
	if raw, _ := os.ReadFile(path); strings.Contains(string(raw), "hik_") {
		t.Error("the store holds a token")
	}
	if again, err := OpenStore(path); err != nil || len(again.List()) != 1 {
		t.Fatalf("reload: %v", err)
	}
	for name, content := range map[string]string{
		"duplicate key": `{"version":1,"version":1,"connections":[]}`,
		"bad host":      `{"version":1,"connections":[{"host":"evil.example","origin":"https://x.example"}]}`,
		"unknown field": `{"version":1,"connections":[],"extra":1}`,
		"wrong version": `{"version":9,"connections":[]}`,
	} {
		bad := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".json")
		_ = os.WriteFile(bad, []byte(content), 0o600)
		if _, err := OpenStore(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestConnectionListUI(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	h.m.BindWindow("app-win", AppHost)
	get := func(path string) (int, string, map[string][]string) {
		rec := h.pageFrom(AppHost, "app-win", "GET", path, "")
		return rec.Code, rec.Body.String(), rec.Header()
	}
	code, body, hdr := get("/__app/state")
	if code != 200 || strings.Contains(body, secret.Token) || strings.Contains(body, secret.SessionID) {
		t.Fatalf("state %d: %s", code, body)
	}
	var st stateView
	if err := json.Unmarshal([]byte(body), &st); err != nil || len(st.Connections) != 1 || st.Connections[0].Host != c.host {
		t.Fatalf("state %+v err %v", st, err)
	}
	_ = hdr
	code, _, hdr = get("/index.html")
	if code != 200 && code != 404 {
		t.Fatalf("index %d", code)
	}
	// The list's API is not reachable from a connection's window.
	if rec := h.page(c, "POST", "/__app/remove", `{"host":"`+c.host+`"}`); rec.Code == 200 || len(h.store.List()) != 1 {
		t.Errorf("a connection window reached /__app: %d", rec.Code)
	}
	// Removal through the list.
	rec := h.pageFrom(AppHost, "app-win", "POST", "/__app/remove", `{"host":"`+c.host+`"}`)
	if rec.Code != 200 || len(h.store.List()) != 0 {
		t.Errorf("remove: %d %s", rec.Code, rec.Body)
	}
	// open-browser is https only.
	if rec := h.pageFrom(AppHost, "app-win", "POST", "/__app/open-browser", `{"url":"http://example.com"}`); rec.Code < 400 {
		t.Errorf("http accepted: %d", rec.Code)
	}
	if rec := h.pageFrom(AppHost, "app-win", "POST", "/__app/open-browser", `{"url":"javascript:alert(1)"}`); rec.Code < 400 {
		t.Errorf("javascript accepted: %d", rec.Code)
	}
}

func TestRemovalDuringSignInStoresNothing(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	entered := make(chan struct{})
	release := make(chan struct{})
	h.browser = func(string) { close(entered); <-release }
	errc := make(chan error, 1)
	go func() { errc <- h.m.SignIn(context.Background(), c.host, "") }()
	<-entered
	if _, err := h.m.Remove(context.Background(), c.host); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-errc; err == nil {
		t.Error("the sign-in of a removed connection succeeded")
	}
	h.m.Wait()
	if _, err := h.secrets.Get(c.host); !errors.Is(err, ErrNoSecret) {
		t.Errorf("a keychain item came back after removal: %v", err)
	}
	if len(h.store.List()) != 0 {
		t.Error("the record came back after removal")
	}
}

func TestClipboardIgnoresCopiesMadeInOtherApps(t *testing.T) {
	n := newFakeNative()
	g := newClipboardGuard(n, time.Now)
	armed := false
	g.after = func(time.Duration, func()) func() { armed = true; return func() {} }
	n.inactive = true
	n.pasteboard++
	g.observe()
	g.arm()
	if armed {
		t.Fatal("armed for a copy made while another app was frontmost")
	}
}

func TestCanonicalOriginHasOneSpelling(t *testing.T) {
	for in, want := range map[string]string{
		"https://Hikyo.example":      "https://hikyo.example",
		"https://hikyo.example:443":  "https://hikyo.example",
		"https://hikyo.example.":     "https://hikyo.example",
		"https://hikyo.example.:443": "https://hikyo.example",
		"https://hikyo.example:8443": "https://hikyo.example:8443",
		"http://127.0.0.1:80":        "http://127.0.0.1",
		"http://[::1]:8080":          "http://[::1]:8080",
		"https://hikyo.example:0443": "https://hikyo.example",
	} {
		if got, err := canonicalOrigin(in); err != nil || got != want {
			t.Errorf("%q = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestCanonicalOriginKeepsIPv6ZoneAndNormalizesPortSpellings(t *testing.T) {
	got, err := canonicalOrigin("http://[fe80::1%25en0]:8080")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := canonicalOrigin(got); err != nil || again != got {
		t.Errorf("not idempotent: %q then %q, %v", got, again, err)
	}
	u, _ := url.Parse("https://x:0443/")
	if sitePort(u) != 443 {
		t.Errorf("0443 = %d", sitePort(u))
	}
}

func TestAddSeesLegacyOriginSpellingsAsDuplicates(t *testing.T) {
	h := newHarness(t)
	host, _ := h.store.NewHost()
	// A spelling an older build could have stored.
	if err := h.store.Put(Connection{Host: host, Origin: "https://x.example:443", Name: "x", Trust: Trust{Mode: clienttrust.ModeSystemCA}}); err != nil {
		t.Fatal(err)
	}
	_, err := h.m.Add(context.Background(), AddRequest{Origin: "https://x.example", Mode: clienttrust.ModeSystemCA})
	var e *shellErr
	if !errors.As(err, &e) || e.code != codeInvalid || !strings.Contains(e.message, "already") {
		t.Fatalf("a second connection to the same instance was not refused as a duplicate: %v", err)
	}
}

func TestFailedRemovalLeavesAWorkingConnection(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.m.secrets = failingDelete{h.secrets}
	if _, err := h.m.Remove(context.Background(), c.host); err == nil {
		t.Fatal("expected an error")
	}
	// The connection must work again, including new ceremonies.
	if c.life().Err() != nil {
		t.Error("the connection's context stayed cancelled")
	}
	c.rotMu.Lock()
	removed := c.removed
	c.rotMu.Unlock()
	if removed {
		t.Error("the connection still refuses sessions")
	}
}
