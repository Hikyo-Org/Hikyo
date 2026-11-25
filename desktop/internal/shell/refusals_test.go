package shell

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

// expectedRefused is every session_token route in the contract that is neither
// on the rotation list nor handled by the shell itself. A new login route
// shows up in the derived list and fails this test until someone classifies it.
var expectedRefused = []string{
	"GET /api/v1/auth/oauth2/{provider}/callback",
	"GET /api/v1/auth/oidc/{provider}/callback",
	"POST /api/v1/auth/cli-login/redeem",
	"POST /api/v1/auth/cli-reauth/redeem",
	"POST /api/v1/auth/local/login",
	"POST /api/v1/auth/login/challenge/{challenge}/totp",
	"POST /api/v1/auth/login/challenge/{challenge}/webauthn/finish",
	"POST /api/v1/auth/saml/{provider}/acs",
	"POST /api/v1/auth/webauthn/enrol/finish",
	"POST /api/v1/auth/webauthn/login/finish",
	"POST /api/v1/auth/webauthn/step-up/finish",
	"POST /api/v1/auth/webauthn/reauth/finish",
}

func TestEverySessionTokenRouteIsRefusedOrRotatesOrIsKnown(t *testing.T) {
	p, err := DefaultPolicy()
	if err != nil {
		t.Fatal(err)
	}
	rotation := map[string]bool{}
	for _, r := range RotationRoutes {
		rotation[strings.ToUpper(r.Method)+" "+r.Path] = true
	}
	known := map[string]bool{}
	for _, r := range expectedRefused {
		known[r] = true
	}
	derived := p.SessionRoutes()
	sort.Strings(derived)
	seen := map[string]bool{}
	for _, route := range derived {
		seen[route] = true
		if !rotation[route] && !known[route] {
			t.Errorf("%s returns a session_token and is neither on the rotation list nor in expectedRefused: classify it", route)
		}
	}
	for route := range rotation {
		if !seen[route] {
			t.Errorf("rotation route %s no longer returns a session_token in the contract", route)
		}
	}
	for route := range known {
		if !seen[route] {
			t.Errorf("expectedRefused route %s is not in the contract's session-token routes", route)
		}
	}
}

func TestClassifyRefusesWhatTheADRNames(t *testing.T) {
	p, err := DefaultPolicy()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, path string
		want         Verdict
	}{
		{"GET", "/api/v1/orgs", Allowed},
		{"GET", "/assets/app.js", Allowed},
		{"GET", "/search/100%", Allowed},
		{"GET", "/search/100%zz", Allowed},
		{"POST", "/%61pi/v1/auth/local/login%", Malformed},
		{"POST", "/api/v1/auth/local/login", Refused},
		{"POST", "/api/v1/auth/LOCAL/login", Refused},
		{"POST", "/api/V1/auth/local/login", Refused},
		{"POST", "/api/v1/auth/login/challenge/abc/totp", Refused},
		{"POST", "/api/v1/auth/cli-login/start", Refused},
		{"GET", "/api/v1/auth/cli-reauth/anything", Refused},
		{"DELETE", "/api/v1/auth/workspace/x", Refused},
		{"GET", "/api/v1/auth/oidc/google/callback", Refused},
		{"POST", "/api/v1/orgs/o/projects/p/environments/e/developer-credentials", Refused},
		{"POST", "/api/v1/auth/totp/step-up", Allowed},
		{"POST", "/api/v1/auth/reauth/totp", Allowed},
		{"PUT", "/api/v1/auth/password", Allowed},
		{"DELETE", "/api/v1/auth/identities/abc", Allowed},
		{"GET", "/api/v1/auth/../auth/login", Malformed},
		{"GET", "/api/v1/./orgs", Malformed},
		{"GET", "/api/v1//orgs", Malformed},
		{"GET", "/api/v1/auth%2flogin", Malformed},
		{"GET", "/api/v1/auth%5clogin", Malformed},
		{"GET", "/api/v1/auth\\login", Malformed},
		{"GET", "/api/v1/au\x01th", Malformed},
		{"GET", "/api/v1/%zz", Malformed},
		{"POST", "/%61pi/v1/auth/local/login", Malformed},
		{"POST", "/%2561pi/v1/orgs", Allowed},
		{"POST", "/api%2Fv1/auth/local/login", Malformed},
		{"POST", "/./api/v1/auth/local/login", Malformed},
		{"POST", "//api/v1/auth/local/login", Malformed},
		{"POST", "/x/../api/v1/auth/local/login", Malformed},
		{"POST", "/x/%2e%2e/api/v1/auth/local/login", Malformed},
		{"POST", "/\\api/v1/auth/local/login", Malformed},
		{"GET", "/assets/app.js", Allowed},
		{"GET", "/search/100%", Allowed},
		{"GET", "/search/100%zz", Allowed},
		{"POST", "/%61pi/v1/auth/local/login%", Malformed},
		{"GET", "/orgs/a/", Allowed},
	}
	for _, tc := range cases {
		if got := p.Classify(tc.method, tc.path); got != tc.want {
			t.Errorf("%s %s = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestRotationFor(t *testing.T) {
	p, _ := DefaultPolicy()
	if p.RotationFor("POST", "/api/v1/auth/totp/step-up") != Rotate {
		t.Error("step-up must rotate")
	}
	if p.RotationFor("PUT", "/api/v1/auth/password") != Reissue {
		t.Error("password must reissue")
	}
	if p.RotationFor("POST", "/api/v1/auth/local/login") != NotRotating {
		t.Error("login must not rotate")
	}
}

func TestProxyRefusesWithoutCallingTheInstance(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"POST", "/api/v1/auth/local/login", 403},
		{"POST", "/api/v1/auth/cli-login/start", 403},
		{"POST", "/api/v1/auth/workspace/approve", 403},
		{"GET", "/api/v1/auth/%2e%2e/login", 400},
	} {
		before := h.inst.total()
		rec := h.page(c, tc.method, tc.path, `{}`)
		if rec.Code != tc.want || h.inst.total() != before {
			t.Errorf("%s %s: status %d (want %d), instance saw %d requests", tc.method, tc.path, rec.Code, tc.want, h.inst.total()-before)
		}
	}
}

func TestTOTPReauthForDeveloperCredentialIsRefusedAndFailsClosed(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.inst.on("POST /api/v1/auth/reauth/totp", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, struct{}{}) })
	for name, body := range map[string]string{
		"developer-credential": `{"purpose":"developer-credential","code":"123456"}`,
		"duplicate key":        `{"purpose":"self-config","purpose":"developer-credential","code":"1"}`,
		"upper case name":      `{"PURPOSE":"developer-credential","code":"1"}`,
		"mixed case name":      `{"Purpose":"developer-credential","code":"1"}`,
		"long s name":          `{"purpoſe":"developer-credential","code":"1"}`,
		"case variant dup":     `{"purpose":"self-config","Purpose":"developer-credential","code":"1"}`,
		"case variant dup ok":  `{"purpose":"self-config","PURPOSE":"self-config","code":"1"}`,
		"non-string variant":   `{"Purpose":5,"code":"1"}`,
		"not json":             `purpose=developer-credential`,
		"truncated":            `{"purpose":"developer-cred`,
		"too large":            `{"code":"` + strings.Repeat("a", 70<<10) + `"}`,
	} {
		before := h.inst.total()
		rec := h.page(c, "POST", "/api/v1/auth/reauth/totp", body)
		if rec.Code < 400 || h.inst.total() != before {
			t.Errorf("%s: status %d, instance saw %d requests", name, rec.Code, h.inst.total()-before)
		}
	}
	if rec := h.page(c, "POST", "/api/v1/auth/reauth/totp", `{"purpose":"self-config","code":"123456"}`); rec.Code != 200 {
		t.Errorf("a legitimate totp reauth was refused: %d %s", rec.Code, rec.Body)
	}
}

func TestAStreamCannotAnswerASessionChange(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.inst.on("POST /api/v1/auth/totp/step-up", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"session_token\":\"x\"}\n\n"))
	})
	rec := h.page(c, "POST", "/api/v1/auth/totp/step-up", `{}`)
	if rec.Code != 502 || strings.Contains(rec.Body.String(), "session_token") {
		t.Fatalf("stream on a rotation route: %d %s", rec.Code, rec.Body)
	}
	if c.Stopped() == "" {
		t.Error("the connection was not stopped")
	}
}
