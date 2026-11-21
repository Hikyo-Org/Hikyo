package shell

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

// playBrowser answers the shell's loopback handoff like a signed-in browser:
// it follows the redirect URI the instance was given, with the state it saw.
func (h *harness) playBrowser() {
	h.browser = func(target string) {
		u, err := url.Parse(target)
		if err != nil {
			return
		}
		h.inst.mu.Lock()
		redirect := h.inst.redirect
		h.inst.mu.Unlock()
		state := u.Query().Get("state")
		if redirect == "" || state == "" {
			h.t.Errorf("handoff %q lacks state or redirect", target)
			return
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, redirect+"?"+url.Values{"code": {"code-1"}, "state": {state}}.Encode(), nil)
		if err != nil {
			return
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}
}

func TestAddSignsInAndStoresOneItemPerConnection(t *testing.T) {
	h := newHarness(t)
	h.playBrowser()
	host, err := h.m.Add(context.Background(), AddRequest{Origin: h.inst.srv.URL, Mode: clienttrust.ModeSystemCA, Name: "Dev"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := h.secrets.Get(host)
	if err != nil || secret.Token != h.inst.loginTok || secret.SessionID != h.inst.loginSID {
		t.Fatalf("secret %+v err %v", secret, err)
	}
	c := h.m.conn(host)
	if c == nil || c.Info().PrincipalID != h.inst.loginPID {
		t.Fatalf("connection %+v", c)
	}
	if got := h.inst.requests("GET", "/api/v1/meta"); len(got) != 1 || got[0].Header.Get("User-Agent") != "Hikyo-Desktop/0.0.0-test" {
		t.Errorf("meta requests: %+v", got)
	}
}

func TestAddRefusesAnInstanceWithoutTheCapabilityBeforeSignIn(t *testing.T) {
	h := newHarness(t)
	h.inst.caps = []string{"loopback-handoff"}
	h.playBrowser()
	_, err := h.m.Add(context.Background(), AddRequest{Origin: h.inst.srv.URL, Mode: clienttrust.ModeSystemCA})
	var e *shellErr
	ok := errors.As(err, &e)
	if !ok || e.code != codeCapabilityMissing || !strings.Contains(e.message, RequiredServerVersion) {
		t.Fatalf("error %v", err)
	}
	if len(h.inst.requests("POST", "/api/v1/auth/cli-login/start")) != 0 || len(h.openedURLs()) != 0 {
		t.Error("sign-in started on an instance that lacks the capability")
	}
	if len(h.store.List()) != 0 {
		t.Error("a record was stored")
	}
}

func TestAddDeclinedTrustSendsNothing(t *testing.T) {
	h := newHarness(t)
	h.native.confirmAnswer = false
	_, err := h.m.Add(context.Background(), AddRequest{Origin: h.inst.srv.URL, Mode: clienttrust.ModeSystemCA})
	var declined *shellErr
	if !errors.As(err, &declined) || declined.code != codeTrustDeclined {
		t.Fatalf("error %v", err)
	}
	if h.inst.total() != 0 {
		t.Errorf("the instance saw %d requests before trust", h.inst.total())
	}
}

func TestAddValidatesBeforeAnyDialogOrRequest(t *testing.T) {
	h := newHarness(t)
	for name, req := range map[string]AddRequest{
		"no mode":       {Origin: h.inst.srv.URL},
		"bad origin":    {Origin: "not a url", Mode: clienttrust.ModeSystemCA},
		"plain http":    {Origin: "http://example.com", Mode: clienttrust.ModeSystemCA},
		"insecure mode": {Origin: h.inst.srv.URL, Mode: clienttrust.Mode("insecure")},
	} {
		if _, err := h.m.Add(context.Background(), req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(h.native.confirms) != 0 || h.inst.total() != 0 {
		t.Errorf("dialogs %d, requests %d", len(h.native.confirms), h.inst.total())
	}
}

func TestAddRejectsADuplicateInstance(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	if _, err := h.m.Add(context.Background(), AddRequest{Origin: c.info.Origin, Mode: clienttrust.ModeSystemCA}); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestRemoveRevokesFirstThenDeletesEverything(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	res, err := h.m.Remove(context.Background(), c.host)
	if err != nil || !res.Revoked {
		t.Fatalf("result %+v err %v", res, err)
	}
	logout := h.inst.requests("POST", "/api/v1/auth/logout")
	if len(logout) != 1 || logout[0].Header.Get("Authorization") != "Bearer "+secret.Token {
		t.Errorf("revocation: %+v", logout)
	}
	if _, err := h.secrets.Get(c.host); !errors.Is(err, ErrNoSecret) {
		t.Errorf("keychain item survived: %v", err)
	}
	if len(h.store.List()) != 0 || h.m.conn(c.host) != nil {
		t.Error("record survived")
	}
	if len(h.native.closed) != 1 || len(h.native.wiped) != 1 {
		t.Errorf("window closed %v, storage wiped %v", h.native.closed, h.native.wiped)
	}
	if rec := h.page(c, "GET", "/api/v1/orgs", ""); rec.Code != 404 {
		t.Errorf("a removed host still answers: %d", rec.Code)
	}
}

func TestRemoveStillDeletesWhenRevocationFails(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.inst.on("POST /api/v1/auth/logout", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	res, err := h.m.Remove(context.Background(), c.host)
	if err != nil || res.Revoked || res.Message == "" {
		t.Fatalf("result %+v err %v", res, err)
	}
	if len(h.store.List()) != 0 {
		t.Error("data kept")
	}
}

func TestRemoveKeepsTheRecordWhenTheKeychainDeleteFails(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.m.secrets = failingDelete{h.secrets}
	if _, err := h.m.Remove(context.Background(), c.host); err == nil {
		t.Fatal("expected an error")
	}
	if len(h.store.List()) != 1 {
		t.Error("record removed although its keychain item remains")
	}
}

type failingDelete struct{ SecretStore }

func (failingDelete) Delete(string) error { return http.ErrAbortHandler }

func TestHostsAreRandomAndNeverReused(t *testing.T) {
	h := newHarness(t)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		host, err := h.store.NewHost()
		if err != nil || !validHost(host) || seen[host] {
			t.Fatalf("host %q err %v", host, err)
		}
		seen[host] = true
	}
}

func TestOpenRechecksTheCapabilityEveryTime(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	if err := h.m.OpenConnection(c.host); err != nil {
		t.Fatal(err)
	}
	delete(h.native.opened, c.host)
	h.inst.mu.Lock()
	h.inst.caps = []string{"loopback-handoff"} // the instance was downgraded
	h.inst.mu.Unlock()
	err := h.m.OpenConnection(c.host)
	var e *shellErr
	if !errors.As(err, &e) || e.code != codeCapabilityMissing || !strings.Contains(e.message, c.Info().Name) || !strings.Contains(e.message, capabilityDesktop) {
		t.Fatalf("error %v", err)
	}
	if _, opened := h.native.opened[c.host]; opened {
		t.Error("a window opened for an instance without the capability")
	}
}

func TestLogSafeBoundsInstanceText(t *testing.T) {
	got := logSafe("bad\nINFO forged\x1b[31m" + strings.Repeat("é", 500))
	if strings.ContainsAny(got, "\n\x1b") || len(got) > 210 {
		t.Fatalf("%q", got)
	}
}
