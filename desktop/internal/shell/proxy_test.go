package shell

import (
	"net/http"
	"strings"
	"testing"
)

func TestProxyBindsRequestsToTheConnection(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	h.inst.on("GET /api/v1/orgs", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "leak", Value: "x"})
		writeJSON(w, 200, map[string]string{"ok": "yes"})
	})
	rec := h.page(c, "GET", "/api/v1/orgs", "", "Cookie", "a=b", "Origin", PageScheme+"://"+c.host, "Referer", "wails://"+c.host+"/x",
		"Authorization", "Bearer attacker", "X-Http-Method-Override", "DELETE", "X-Wails-Body-Encoding", "base64")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("Set-Cookie reached the page")
	}
	got := h.inst.requests("GET", "/api/v1/orgs")
	if len(got) != 1 {
		t.Fatalf("instance saw %d requests", len(got))
	}
	hd := got[0].Header
	if hd.Get("Authorization") != "Bearer "+secret.Token {
		t.Errorf("authorization = %q", hd.Get("Authorization"))
	}
	for _, name := range []string{"Cookie", "Origin", "Referer", "X-Http-Method-Override", "X-Wails-Window-Id", "X-Wails-Body-Encoding"} {
		if hd.Get(name) != "" {
			t.Errorf("%s was forwarded: %q", name, hd.Get(name))
		}
	}
	if hd.Get("User-Agent") != "Hikyo-Desktop/0.0.0-test" {
		t.Errorf("user agent = %q", hd.Get("User-Agent"))
	}
	if hd.Get("Accept-Encoding") != "identity" {
		t.Errorf("accept-encoding = %q", hd.Get("Accept-Encoding"))
	}
}

func TestProxyNeverFollowsRedirects(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	other := newFakeInstance(t)
	h.inst.on("GET /api/v1/jump", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.srv.URL+"/steal", http.StatusFound)
	})
	h.inst.on("GET /api/v1/same", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, h.inst.srv.URL+"/next?x=1", http.StatusFound)
	})
	rec := h.page(c, "GET", "/api/v1/jump", "")
	if rec.Code != http.StatusFound || other.total() != 0 {
		t.Fatalf("status %d, other instance saw %d requests", rec.Code, other.total())
	}
	if loc := h.page(c, "GET", "/api/v1/same", "").Header().Get("Location"); loc != "/next?x=1" {
		t.Errorf("same-origin Location not made relative: %q", loc)
	}
}

func TestRequestsAreBoundToTheOwningWindow(t *testing.T) {
	h := newHarness(t)
	a, _ := h.connect()
	b, _ := h.connect()
	h.inst.on("GET /api/v1/orgs", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, struct{}{}) })
	cases := []struct {
		name    string
		host    string
		window  string
		headers []string
		want    int
	}{
		{"own window", a.host, "win-" + a.host, nil, 200},
		{"other connection's window", a.host, "win-" + b.host, nil, 403},
		{"no window id", a.host, "", nil, 403},
		{"unknown window", a.host, "999", nil, 403},
		{"unknown host", "c-00000000000000000000000000000000", "win-" + a.host, nil, 404},
		{"foreign origin", a.host, "win-" + a.host, []string{"Origin", "wails://" + b.host}, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := h.inst.total()
			rec := h.pageFrom(tc.host, tc.window, "GET", "/api/v1/orgs", "", tc.headers...)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d", rec.Code, tc.want)
			}
			if tc.want != 200 && h.inst.total() != before {
				t.Fatalf("a refused request reached the instance")
			}
		})
	}
	if rec := h.page(a, "GET", "/wails/runtime.js", ""); rec.Code != 403 {
		t.Errorf("/wails/ answered %d", rec.Code)
	}
	if rec := h.pageFrom(AppHost, "win-"+a.host, "GET", "/__app/state", ""); rec.Code != 403 {
		t.Errorf("a connection window reached the connection list: %d", rec.Code)
	}
}

func TestStoppedConnectionAnswers401WithoutCallingTheInstance(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	c.stop("test")
	before := h.inst.total()
	if rec := h.page(c, "GET", "/api/v1/orgs", ""); rec.Code != 401 || h.inst.total() != before {
		t.Fatalf("status %d, instance saw %d new requests", rec.Code, h.inst.total()-before)
	}
	if len(h.native.alerts) != 1 {
		t.Errorf("a stop must be visible: %v", h.native.alerts)
	}
}

func TestIdempotentRequestRetriesOnceAfterRotation(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	next := newToken(t)
	h.inst.on("GET /api/v1/slow", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer "+secret.Token {
			// The rotation lands while this request is in flight.
			c.mu.Lock()
			c.secret = secret
			c.secret.Token = next
			c.mu.Unlock()
			w.WriteHeader(401)
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "retried"})
	})
	if rec := h.page(c, "GET", "/api/v1/slow", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "retried") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if n := len(h.inst.requests("GET", "/api/v1/slow")); n != 2 {
		t.Errorf("instance saw %d attempts, want 2", n)
	}
	// A POST is not idempotent and is not retried.
	h.inst.on("POST /api/v1/once", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.secret = secret
		c.secret.Token = newToken(t)
		c.mu.Unlock()
		w.WriteHeader(401)
	})
	if rec := h.page(c, "POST", "/api/v1/once", `{}`); rec.Code != 401 || len(h.inst.requests("POST", "/api/v1/once")) != 1 {
		t.Fatalf("status %d, attempts %d", rec.Code, len(h.inst.requests("POST", "/api/v1/once")))
	}
}
