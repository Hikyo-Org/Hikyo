package shell

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
			// The item itself is trustworthy, so removal revokes through the
			// item's own binding: one logout, to the instance the item names.
			res, err := m2.Remove(context.Background(), info.Host)
			if err != nil || !res.Revoked {
				t.Fatalf("remove: %+v %v", res, err)
			}
			logout := h.inst.requests("POST", "/api/v1/auth/logout")
			if len(logout) != 1 || logout[0].Header.Get("Authorization") != "Bearer "+secret.Token {
				t.Errorf("revocation: %+v", logout)
			}
			if h.inst.total() != before+1 {
				t.Errorf("a tampered connection made %d requests, want only the logout", h.inst.total()-before)
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
	if err := c.applyRotation(Rotate, tokenView{Token: next, SessionID: secret.SessionID}, secret.Token); err != nil {
		t.Fatal(err)
	}
	got, err := h.secrets.Get(c.host)
	if err != nil || got.Token != next || got.Origin != secret.Origin || got.PrincipalID != secret.PrincipalID {
		t.Fatalf("rotated item %+v err %v", got, err)
	}
}

// failFirstGet fails the first read, as a locked keychain whose prompt the user
// dismissed does at launch.
type failFirstGet struct {
	SecretStore
	failed bool
}

func (f *failFirstGet) Get(host string) (Secret, error) {
	if !f.failed {
		f.failed = true
		return Secret{}, errors.New("user canceled the operation")
	}
	return f.SecretStore.Get(host)
}

// untrustedItem reports every item as open to other applications.
type untrustedItem struct{ SecretStore }

func (untrustedItem) Get(string) (Secret, error) { return Secret{}, ErrItemUntrusted }

func (h *harness) attacker() (*httptest.Server, *atomic.Int32) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	h.t.Cleanup(srv.Close)
	return srv, &hits
}

// The launch-time keychain read failed, so the record was never compared to the
// item. Removal must still not send the bearer to the origin the file names.
func TestRemoveAfterAFailedLaunchReadRevokesOnlyAtTheItemsOrigin(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	atk, hits := h.attacker()
	info := c.Info()
	info.Origin = atk.URL
	if err := h.store.Put(info); err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager(h.store, &failFirstGet{SecretStore: h.secrets}, h.native, h.m.opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m2.Wait(); m2.Close() })
	if m2.conn(info.Host).Stopped() == "" {
		t.Fatal("the unreadable keychain did not stop the connection")
	}
	res, err := m2.Remove(context.Background(), info.Host)
	if err != nil || !res.Revoked {
		t.Fatalf("remove: %+v %v", res, err)
	}
	if hits.Load() != 0 {
		t.Error("the bearer reached the origin from the edited record")
	}
	logout := h.inst.requests("POST", "/api/v1/auth/logout")
	if len(logout) != 1 || logout[0].Header.Get("Authorization") != "Bearer "+secret.Token {
		t.Errorf("revocation: %+v", logout)
	}
}

// An item this build may not use (another build created it, or
// other applications can read it) is never used or revoked through, and
// removal says the session was not revoked.
func TestUntrustedKeychainItemIsNeverUsedOrRevokedThrough(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	info := c.Info()
	m2, err := NewManager(h.store, untrustedItem{h.secrets}, h.native, h.m.opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m2.Wait(); m2.Close() })
	conn := m2.conn(info.Host)
	if conn.Stopped() == "" || conn.bearer() != "" || !strings.Contains(conn.Stopped(), "Sign in again") {
		t.Fatalf("untrusted item accepted or unexplained: stopped %q", conn.Stopped())
	}
	before := h.inst.total()
	res, err := m2.Remove(context.Background(), info.Host)
	if err != nil || res.Revoked || res.Message == "" {
		t.Fatalf("remove: %+v %v", res, err)
	}
	if h.inst.total() != before {
		t.Errorf("an untrusted item reached the instance: %d requests", h.inst.total()-before)
	}
	// untrustedItem.Get always refuses, so look at the real store.
	if _, err := h.secrets.Get(info.Host); !errors.Is(err, ErrNoSecret) {
		t.Errorf("removal left the refused keychain item: %v", err)
	}
}

// Signing in again replaces a refused item. The old token is never sent, and
// the person confirms the origin because no item vouches for the record.
func TestSignInAgainReplacesAnUntrustedItem(t *testing.T) {
	h := newHarness(t)
	c, old := h.connect()
	info := c.Info()
	h.playBrowser()
	h.native.confirmAnswer = true
	m2, err := NewManager(h.store, &untrustedOnce{SecretStore: h.secrets}, h.native, h.m.opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m2.Wait(); m2.Close() })
	if m2.conn(info.Host).Stopped() == "" {
		t.Fatal("the untrusted item did not stop the connection")
	}
	if err := m2.SignIn(context.Background(), info.Host, ""); err != nil {
		t.Fatal(err)
	}
	if len(h.native.confirms) == 0 || !strings.Contains(h.native.confirms[len(h.native.confirms)-1], info.Origin) {
		t.Errorf("sign-in did not name the origin: %q", h.native.confirms)
	}
	got, err := h.secrets.Get(info.Host)
	if err != nil || got.Token == old.Token || got.Origin != info.Origin {
		t.Fatalf("item after sign-in: %+v %v", got, err)
	}
	for _, r := range h.inst.requests("POST", "/api/v1/auth/logout") {
		if r.Header.Get("Authorization") == "Bearer "+old.Token {
			t.Error("the refused item's token was sent")
		}
	}
}

// untrustedOnce refuses the first read only: the launch-time read of an item
// another build created.
type untrustedOnce struct {
	SecretStore
	done bool
}

func (u *untrustedOnce) Get(host string) (Secret, error) {
	if !u.done {
		u.done = true
		return Secret{}, ErrItemUntrusted
	}
	return u.SecretStore.Get(host)
}
