package shell

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	g.arm("a")
	if fire != nil {
		t.Fatal("armed without a pasteboard change")
	}
	// The page copies a secret, then the shell sees the change.
	n.pasteboard++
	g.observe()
	g.arm("a")
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
	g.arm("a")
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
	code, body, _ := get("/__app/state")
	if code != 200 || strings.Contains(body, secret.Token) || strings.Contains(body, secret.SessionID) {
		t.Fatalf("state %d: %s", code, body)
	}
	var st stateView
	if err := json.Unmarshal([]byte(body), &st); err != nil || len(st.Connections) != 1 || st.Connections[0].Host != c.host {
		t.Fatalf("state %+v err %v", st, err)
	}
	code, body, hdr := get("/index.html")
	if code != 200 || !strings.HasPrefix(http.Header(hdr).Get("Content-Type"), "text/html") || !strings.Contains(body, "<html") {
		t.Fatalf("the embedded index did not load: %d %q", code, http.Header(hdr).Get("Content-Type"))
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
	var releaseOnce sync.Once
	free := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(free) // a failed assertion must not leave the browser callback blocked
	errc := make(chan error, 1)
	go func() { errc <- h.m.SignIn(context.Background(), c.host, "") }()
	select {
	case <-entered:
	case err := <-errc:
		t.Fatalf("the sign-in ended before it opened the browser: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the sign-in never opened the browser")
	}
	if _, err := h.m.Remove(context.Background(), c.host); err != nil {
		t.Fatal(err)
	}
	free()
	select {
	case err := <-errc:
		if err == nil {
			t.Error("the sign-in of a removed connection succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the sign-in did not end after the removal")
	}
	h.m.Wait()
	if _, err := h.secrets.Get(c.host); !errors.Is(err, ErrNoSecret) {
		t.Errorf("a keychain item came back after removal: %v", err)
	}
	if len(h.store.List()) != 0 {
		t.Error("the record came back after removal")
	}
}

// A copy made in another app just before switching back is at or below the
// activation count, whether or not a poll ran in between.
func TestClipboardIgnoresACopyJustBeforeSwitchBack(t *testing.T) {
	n := newFakeNative()
	now := time.Unix(1000, 0)
	g := newClipboardGuard(n, func() time.Time { return now })
	armed := false
	g.after = func(time.Duration, func()) func() { armed = true; return func() {} }
	n.inactive = true
	n.pasteboard++ // copied elsewhere; no poll saw it
	n.inactive = false
	n.activation = n.pasteboard // the app became active
	g.arm("a")
	if armed {
		t.Fatal("armed on a change made before the app came to the front")
	}
	g.observe() // a poll after activation must not attribute it either
	g.arm("a")
	if armed {
		t.Fatal("armed after a late poll attributed the foreign copy to the page")
	}
	n.pasteboard++ // the page copies while active
	g.arm("a")
	if !armed {
		t.Fatal("did not arm for the page's own copy")
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
	g.arm("a")
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
}

func TestOriginSpellingsAgree(t *testing.T) {
	groups := [][]string{
		{"https://bücher.example", "https://xn--bcher-kva.example", "https://BÜCHER.example.:443", "https://xn--bcher-kva.example:0443"},
		{"http://[::1]:8080", "http://[0:0:0:0:0:0:0:1]:8080", "http://[0000::0001]:8080", "http://[::0001]:08080"},
		{"http://[fe80::1%25en0]", "http://[FE80:0:0:0:0:0:0:1%25en0]"},
	}
	for _, g := range groups {
		want, err := canonicalOrigin(g[0])
		if err != nil {
			t.Fatal(g[0], err)
		}
		for _, in := range g {
			if got, err := canonicalOrigin(in); err != nil || got != want {
				t.Errorf("%q = %q, %v; want %q", in, got, err, want)
			}
		}
	}
	distinct := []string{
		"http://[::1]", "http://[::2]", "http://[::ffff:127.0.0.1]", "http://127.0.0.1",
		"http://[fe80::1%25en0]", "http://[fe80::1%25en1]", "http://[fe80::1]",
		"https://bücher.example", "https://bucher.example", "https://xn--bcher-kva.example:8443",
	}
	seen := map[string]string{}
	for _, in := range distinct {
		got, err := canonicalOrigin(in)
		if err != nil {
			t.Fatal(in, err)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%q and %q collide as %q", in, prev, got)
		}
		seen[got] = in
	}
	for _, bad := range []string{"https://[::1", "https://[zz::1]", "https://xn--a.example", "https://exa mple"} {
		if got, err := canonicalOrigin(bad); err == nil {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
}

func TestSameSiteCoversIDNAndIPv6(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	c.inst.Origin = "https://xn--bcher-kva.example"
	for _, in := range []string{"https://xn--bcher-kva.example/x", "https://BÜCHER.example:443/x", "https://xn--bcher-kva.example./x"} {
		u, _ := url.Parse(in)
		if !c.sameSite(u) {
			t.Errorf("%q should be the connection's origin", in)
		}
	}
	for _, in := range []string{"https://bucher.example/x", "https://xn--bcher-kva.example:8443/x", "http://xn--bcher-kva.example/x"} {
		u, _ := url.Parse(in)
		if c.sameSite(u) {
			t.Errorf("%q must not be the connection's origin", in)
		}
	}
	// IPv6: an expanded spelling is the same origin; another address or port is not.
	c.inst.Origin = "https://[2001:db8::1]:8443"
	for _, in := range []string{"https://[2001:db8::1]:8443/x", "https://[2001:0db8:0:0:0:0:0:1]:8443/x", "https://[2001:DB8::1]:8443/x"} {
		u, _ := url.Parse(in)
		if !c.sameSite(u) {
			t.Errorf("%q should be the connection's origin", in)
		}
	}
	for _, in := range []string{"https://[2001:db8::2]:8443/x", "https://[2001:db8::1]:443/x", "https://[2001:db8::1]/x", "http://[2001:db8::1]:8443/x"} {
		u, _ := url.Parse(in)
		if c.sameSite(u) {
			t.Errorf("%q must not be the connection's origin", in)
		}
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

// The device-code dialog's "Open in browser" uses openHandoff: the verification
// page of this origin only, under any spelling of the origin.
func TestOpenHandoffAcceptsOnlyThisOriginsHandoffPages(t *testing.T) {
	h := newHarness(t)
	origin := "https://xn--bcher-kva.example"
	for _, ok := range []string{origin + "/login/cli?device=1", "https://bücher.example:443/login/cli?device=1", origin + "/reauth/cli?x=1"} {
		if err := h.m.openHandoff(origin, ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"https://bucher.example/login/cli", origin + "/orgs", "https://u@xn--bcher-kva.example/login/cli", "http://xn--bcher-kva.example/login/cli", "https://evil.example/login/cli"} {
		if err := h.m.openHandoff(origin, bad); err == nil {
			t.Errorf("%q opened", bad)
		}
	}
	if got := len(h.openedURLs()); got != 3 {
		t.Errorf("opened %d URLs, want 3", got)
	}
}

// One connection arming its clear must not cancel another's.
func TestClipboardArmingIsPerConnection(t *testing.T) {
	n := newFakeNative()
	g := newClipboardGuard(n, time.Now)
	fires := map[string]func(){}
	stopped := map[string]bool{}
	var current string
	g.after = func(_ time.Duration, f func()) func() {
		owner := current
		fires[owner] = f
		return func() { stopped[owner] = true }
	}
	current = "a"
	n.pasteboard++
	g.observe()
	g.arm("a")
	current = "b"
	n.pasteboard++
	g.observe()
	g.arm("b")
	if stopped["a"] {
		t.Fatal("arming b cancelled a's pending clear")
	}
	fires["a"]() // a's secret was replaced by b's: a must not clear it
	if n.cleared != 0 {
		t.Fatal("a cleared b's copy")
	}
	fires["b"]()
	if n.cleared != 1 {
		t.Fatalf("b's copy cleared %d times", n.cleared)
	}
}
