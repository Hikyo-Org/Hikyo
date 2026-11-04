package main

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"sync/atomic"
	"testing"
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
	proxies := map[string]*httputil.ReverseProxy{"conn-0": newProxy("conn-0", session{Origin: instance.URL, Token: "hik_1_first"})}
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
