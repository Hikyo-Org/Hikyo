package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

// reload builds a second Manager over the same store and keychain, as a
// restart does.
func (h *harness) reload() *Manager {
	h.t.Helper()
	m, err := NewManager(h.store, h.secrets, h.native, h.m.opts)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { m.Wait(); m.Close() })
	return m
}

// A same-user process can rewrite connections.json. Any edit that disagrees
// with the keychain item must stop the connection before a bearer is sent.
func TestRecordEditedOutsideTheAppStopsTheConnection(t *testing.T) {
	edits := map[string]func(*Connection){
		"origin":          func(c *Connection) { c.Origin = "https://attacker.example" },
		"trust mode":      func(c *Connection) { c.Trust.Mode = clienttrust.ModeSPKI },
		"certificate pin": func(c *Connection) { c.Trust.Pin = "AAAA" },
		"account":         func(c *Connection) { c.PrincipalID = "usr_0198bbbb-0000-7000-8000-0000000000ff" },
	}
	for field, edit := range edits {
		t.Run(field, func(t *testing.T) {
			h := newHarness(t)
			c, secret := h.connect()
			info := c.Info()
			edit(&info)
			if err := h.store.Put(info); err != nil {
				t.Fatal(err)
			}
			m2 := h.reload()
			conn := m2.conn(info.Host)
			if reason := conn.Stopped(); !strings.Contains(reason, field) || !strings.Contains(reason, "Remove the connection") {
				t.Fatalf("stopped = %q", reason)
			}
			if conn.bearer() != "" {
				t.Fatal("a bearer is available on a tampered connection")
			}
			before := h.inst.total()
			if e := conn.runSignIn(context.Background()); e == nil || e.code != codeRefused {
				t.Errorf("sign-in on a tampered connection = %v", e)
			}
			if err := m2.SignIn(context.Background(), info.Host, ""); err == nil {
				t.Error("SignIn on a tampered connection succeeded")
			}
			if _, err := m2.Remove(context.Background(), info.Host); err != nil {
				t.Fatal(err)
			}
			for _, req := range h.inst.requests("POST", "/api/v1/auth/logout") {
				if strings.Contains(req.Header.Get("Authorization"), secret.Token) {
					t.Error("the bearer was sent to the edited origin on removal")
				}
			}
			if h.inst.total() != before {
				t.Errorf("a tampered connection reached the instance: %d new requests", h.inst.total()-before)
			}
		})
	}
}

func TestSessionStoredWithItsBinding(t *testing.T) {
	h := newHarness(t)
	h.playBrowser()
	host, err := h.m.Add(context.Background(), AddRequest{Origin: h.inst.srv.URL, Mode: clienttrust.ModeSystemCA})
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := h.secrets.Get(host)
	info := h.store.List()[0]
	if secret.Origin != info.Origin || secret.TrustMode != clienttrust.ModeSystemCA || secret.PrincipalID != h.inst.loginPID {
		t.Fatalf("keychain item %+v does not carry the binding", secret)
	}
	if conn := h.reload().conn(host); conn.Stopped() != "" {
		t.Fatalf("an untouched connection is stopped: %s", conn.Stopped())
	}
}

func TestRotationKeepsTheBinding(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	next := newToken(t)
	if err := c.applyRotation(Rotate, tokenView{Token: next, SessionID: secret.SessionID}); err != nil {
		t.Fatal(err)
	}
	got, err := h.secrets.Get(c.host)
	if err != nil || got.Token != next || got.Origin != secret.Origin || got.PrincipalID != secret.PrincipalID {
		t.Fatalf("rotated item %+v err %v", got, err)
	}
}
