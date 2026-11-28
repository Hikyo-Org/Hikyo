package shell

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func rotateBody(tok, sid, pid string) map[string]any {
	return map[string]any{"session_token": tok, "session": map[string]any{"id": sid}, "principal": map[string]any{"id": pid}, "ok": true}
}

func TestRotateStoresTheNewTokenAndStripsItFromThePage(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	next := newToken(t)
	h.inst.on("POST /api/v1/auth/totp/step-up", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, rotateBody(next, secret.SessionID, h.inst.loginPID))
	})
	rec := h.page(c, "POST", "/api/v1/auth/totp/step-up", `{"code":"123456"}`)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), next) || strings.Contains(rec.Body.String(), "session_token") {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if got, _ := h.secrets.Get(c.host); got.Token != next || got.SessionID != secret.SessionID {
		t.Errorf("keychain holds %+v", got)
	}
	if c.bearer() != next {
		t.Errorf("memory still holds the old token")
	}
}

func TestReissueChecksPrincipalAndStoresTheNewSessionID(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	next := newToken(t)
	newSID := "ses_0198cccc-0000-7000-8000-000000000009"
	h.inst.on("PUT /api/v1/auth/password", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, rotateBody(next, newSID, h.inst.loginPID))
	})
	if rec := h.page(c, "PUT", "/api/v1/auth/password", `{}`); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got, _ := h.secrets.Get(c.host); got.Token != next || got.SessionID != newSID {
		t.Errorf("keychain holds %+v", got)
	}
}

func TestRotationFailuresStopTheConnectionAndKeepTheTokenFromThePage(t *testing.T) {
	cases := map[string]struct {
		path, method string
		body         func(h *harness, secret Secret, next string) any
		failPut      error
	}{
		"rotate with another session id": {"/api/v1/auth/totp/step-up", "POST", func(h *harness, s Secret, n string) any {
			return rotateBody(n, "ses_0198dddd-0000-7000-8000-000000000001", h.inst.loginPID)
		}, nil},
		"reissue for another principal": {"/api/v1/auth/password", "PUT", func(h *harness, s Secret, n string) any {
			return rotateBody(n, "ses_0198dddd-0000-7000-8000-000000000002", "usr_0198eeee-0000-7000-8000-000000000001")
		}, nil},
		"token is not a CLI token": {"/api/v1/auth/totp/step-up", "POST", func(h *harness, s Secret, n string) any {
			return rotateBody("hik_not_a_token", s.SessionID, h.inst.loginPID)
		}, nil},
		"keychain write fails": {"/api/v1/auth/totp/step-up", "POST", func(h *harness, s Secret, n string) any {
			return rotateBody(n, s.SessionID, h.inst.loginPID)
		}, errors.New("keychain locked")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			c, secret := h.connect()
			next := newToken(t)
			h.inst.on(tc.method+" "+tc.path, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, tc.body(h, secret, next)) })
			h.secrets.FailPut = tc.failPut
			rec := h.page(c, tc.method, tc.path, `{}`)
			h.m.Wait()
			if strings.Contains(rec.Body.String(), next) || strings.Contains(rec.Body.String(), "hik_") {
				t.Errorf("token reached the page: %s", rec.Body)
			}
			if c.Stopped() == "" {
				t.Error("the connection must stop visibly")
			}
			if len(h.native.alerts) == 0 {
				t.Error("no alert shown")
			}
			if got, _ := h.secrets.Get(c.host); got.Token != secret.Token {
				t.Errorf("keychain changed on a failed rotation: %+v", got)
			}
			// A token that is not even a CLI token cannot be revoked.
			if name != "token is not a CLI token" && len(h.inst.requests("POST", "/api/v1/auth/logout")) == 0 {
				t.Error("the new session was not revoked")
			}
		})
	}
}

func TestTokenOnUnlistedRouteIsAnErrorAndRevoked(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	leaked := newToken(t)
	h.inst.on("GET /api/v1/surprise", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"session_token": leaked})
	})
	rec := h.page(c, "GET", "/api/v1/surprise", "")
	h.m.Wait()
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), leaked) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	revoked := h.inst.requests("POST", "/api/v1/auth/logout")
	if len(revoked) != 1 || revoked[0].Header.Get("Authorization") != "Bearer "+leaked {
		t.Errorf("the unexpected token was not revoked: %+v", revoked)
	}
}

func TestSplitTokenIsStrict(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate top-level key": `{"session_token":"a","session_token":"b"}`,
		"duplicate login key":     `{"login":{"session_token":"a","session_token":"b"}}`,
		"token not a string":      `{"session_token":5}`,
		"empty token":             `{"session_token":""}`,
		"repeated in login":       `{"session_token":"a","login":{"session_token":"b"}}`,
		"invalid json":            `{"session_token":`,
	} {
		if _, err := splitToken([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	view, err := splitToken([]byte(`{"login":{"session_token":"tok","session":{"id":"s1"},"principal":{"id":"p1"}},"n":1}`))
	if err != nil || view.Token != "tok" || view.SessionID != "s1" || view.PrincipalID != "p1" || strings.Contains(string(view.Body), "tok") {
		t.Fatalf("view %+v err %v", view, err)
	}
	if _, err := splitToken([]byte(`[1,2]`)); !errors.Is(err, errNotObject) {
		t.Errorf("array: %v", err)
	}
}
