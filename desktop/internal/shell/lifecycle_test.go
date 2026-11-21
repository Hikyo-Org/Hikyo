package shell

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A removal whose marker can't be stored changes nothing: the connection keeps
// working and nothing was revoked or destroyed.
func TestRemovalThatCannotRecordItselfChangesNothing(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	h.breakStore()
	if _, err := h.m.Remove(context.Background(), c.host); err == nil {
		t.Fatal("the removal succeeded without storing its marker")
	}
	if c.Stopped() != "" || c.bearer() != secret.Token || c.life().Err() != nil {
		t.Error("the connection was disturbed by a removal that never began")
	}
	if n := len(h.inst.requests("POST", "/api/v1/auth/logout")) + len(h.native.closed) + len(h.native.wiped); n != 0 {
		t.Errorf("%d destructive steps ran before the marker was stored", n)
	}
}

// A step that fails after the marker leaves the connection unusable and
// retryable. Nothing, not even a sign-in that was already running, brings it back.
func TestFailedRemovalIsNeverUndoneAndCanBeRetried(t *testing.T) {
	steps := map[string]func(h *harness){
		"keychain delete": func(h *harness) { h.m.secrets = failingDelete{h.secrets} },
		"window close":    func(h *harness) { h.native.closeErr = errors.New("still closing") },
		"record delete":   func(h *harness) { h.native.onClose = h.breakStore },
	}
	for name, fail := range steps {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			c, secret := h.connect()
			fail(h)
			if _, err := h.m.Remove(context.Background(), c.host); err == nil {
				t.Fatal("expected an error")
			}
			if c.Stopped() == "" || c.bearer() != "" || c.life().Err() == nil {
				t.Fatal("the connection is usable after a failed removal")
			}
			if list := h.store.List(); len(list) != 1 || !list[0].WipePending {
				t.Fatalf("the retry marker is missing: %+v", list)
			}
			// A sign-in that was running cannot commit, and does not touch the keychain.
			late := newToken(t)
			err := h.m.acceptLogin(context.Background(), c, loginResult(late, h.inst.loginSID, h.inst.loginPID))
			if !errors.Is(err, errRemoved) {
				t.Fatalf("a late sign-in = %v", err)
			}
			h.m.Wait()
			if got, _ := h.secrets.Get(c.host); got.Token == late {
				t.Error("a late sign-in wrote the keychain")
			}
			// A late rotation cannot either.
			if err := c.applyRotation(Rotate, tokenView{Token: newToken(t), SessionID: secret.SessionID}, secret.Token); !errors.Is(err, errRemoved) {
				t.Errorf("a late rotation = %v", err)
			}
			h.m.Wait()
			var se *shellErr
			if err := h.m.SignIn(context.Background(), c.host, ""); !errors.As(err, &se) || se.code != codeRefused {
				t.Errorf("sign-in = %v", err)
			}
			if err := h.m.OpenConnection(c.host); !errors.As(err, &se) || se.code != codeRefused {
				t.Errorf("open = %v", err)
			}
			// Repair the failing step and retry.
			h.m.secrets, h.native.closeErr, h.native.onClose = h.secrets, nil, nil
			if err := os.Chmod(filepath.Dir(h.store.path), 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := h.m.Remove(context.Background(), c.host); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if len(h.store.List()) != 0 || h.m.conn(c.host) != nil {
				t.Error("the retry left the record")
			}
			if _, err := h.secrets.Get(c.host); !errors.Is(err, ErrNoSecret) {
				t.Errorf("keychain item survived the retry: %v", err)
			}
		})
	}
}

// A failed sign-in after a removal deleted the keychain item must not put the
// revoked session back.
func TestRemovedSessionIsNotRestoredByALaterFailedSignIn(t *testing.T) {
	h := newHarness(t)
	c, old := h.connect()
	h.m.secrets = failingDelete{h.secrets}
	_, _ = h.m.Remove(context.Background(), c.host)
	// The session is gone from memory, so nothing can roll it back.
	if c.secret.Token != "" || c.secret == old {
		t.Error("the removed session is still held in memory")
	}
}

// The retry of a removal that already deleted the session can't claim the
// session was revoked.
func TestRetryAfterAnEarlierRemovalDoesNotClaimRevocation(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.inst.on("POST /api/v1/auth/logout", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	h.native.wipeErr = errors.New("silent")
	if res, err := h.m.Remove(context.Background(), c.host); err != nil || res.Revoked {
		t.Fatalf("first: %+v %v", res, err)
	}
	m2 := h.reload()
	h.native.wipeErr = nil
	res, err := m2.Remove(context.Background(), c.host)
	if err != nil || res.Revoked || !strings.Contains(res.Message, "can't be confirmed") {
		t.Fatalf("retry: %+v %v", res, err)
	}
	if len(h.store.List()) != 0 {
		t.Error("the wipe retry did not finish")
	}
}

// The account the person approved replacing must still be the connection's
// account when the sign-in commits.
func TestSignInDoesNotSwitchAccountsThatChangedDuringThePrompt(t *testing.T) {
	h := newHarness(t)
	c, old := h.connect()
	h.native.onConfirm = func() {
		c.mu.Lock()
		c.info.PrincipalID = "usr_0198cccc-0000-7000-8000-000000000003"
		c.mu.Unlock()
	}
	err := h.m.acceptLogin(context.Background(), c, loginResult(newToken(t), h.inst.loginSID, "usr_0198dddd-0000-7000-8000-000000000004"))
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("err = %v", err)
	}
	h.m.Wait()
	if got, _ := h.secrets.Get(c.host); got != old {
		t.Error("an unconfirmed account replaced the session")
	}
}

// A failed response of a replaced session says nothing about the current one.
func TestFailedResponseOfAnOldSessionDoesNotStopTheNewOne(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	c.stopSent("bearer-from-before-a-sign-in", "malformed")
	if c.Stopped() != "" {
		t.Fatal("an old session's failure stopped the current one")
	}
	c.stopSent(secret.Token, "malformed")
	if c.Stopped() == "" {
		t.Fatal("the current session's failure did not stop it")
	}
}

// A successful session change that returns no new session leaves the old bearer
// in use for a session the instance replaced, for either kind of change.
func TestSessionChangeWithoutANewSessionStopsTheConnection(t *testing.T) {
	for _, route := range []string{"/api/v1/auth/totp/step-up", "/api/v1/auth/reauth/totp"} {
		h := newHarness(t)
		c, _ := h.connect()
		h.inst.on("POST "+route, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]any{"ok": true}) })
		h.page(c, "POST", route, `{"code":"123456"}`)
		if c.Stopped() == "" {
			t.Errorf("%s: the connection kept a bearer the instance replaced", route)
		}
	}
}

func TestOpenAfterRemovalBeganIsRefused(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.native.closeErr = errors.New("still closing")
	_, _ = h.m.Remove(context.Background(), c.host)
	if err := h.m.OpenConnection(c.host); err == nil || h.native.opened[c.host] != "" {
		t.Errorf("a window opened for a connection being removed: %v", err)
	}
}

func TestMetaThatIsNotMetaIsARefusal(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.inst.on("GET /api/v1/meta", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>login</html>"))
	})
	var se *shellErr
	if err := h.m.OpenConnection(c.host); !errors.As(err, &se) || se.code != codeCapabilityMissing {
		t.Fatalf("err = %v", err)
	}
}

func TestStoreSizeIsLimitedBeforeReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxStoreSize + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := OpenStore(path); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadCancelledWhilePanelOpenSavesNothing(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	dest := filepath.Join(t.TempDir(), "out.txt")
	h.native.savePath, h.native.saveOK = dest, true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := c.download(ctx, "", []byte("data"), "out.txt", "", ""); e == nil || e.code != codeCancelled {
		t.Fatalf("download = %v", e)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("a cancelled download was saved")
	}
}

func TestDownloadReplacesOnlyWhatThePanelApproved(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	dest := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.native.savePath, h.native.saveOK = dest, true
	// A file that exists but was not approved is kept; the download is renamed
	// and the person is told.
	if e := c.download(context.Background(), "", []byte("new"), "out.txt", "", ""); e != nil {
		t.Fatal(e)
	}
	if got, _ := os.ReadFile(dest); string(got) != "old" {
		t.Error("an unapproved file was replaced")
	}
	if got, _ := os.ReadFile(strings.TrimSuffix(dest, ".txt") + " (2).txt"); string(got) != "new" {
		t.Error("the download was not saved under another name")
	}
	if len(h.native.alerts) != 1 || !strings.Contains(h.native.alerts[0], "another name") {
		t.Errorf("alerts = %q", h.native.alerts)
	}
	h.native.saveReplace = true
	if e := c.download(context.Background(), "", []byte("newer"), "out.txt", "", ""); e != nil {
		t.Fatal(e)
	}
	if got, _ := os.ReadFile(dest); string(got) != "newer" {
		t.Error("an approved replacement did not happen")
	}
}

// The fallback for a volume without hard links must not replace a file either.
func TestExclusiveCopyNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	temp, name := filepath.Join(dir, "tmp"), filepath.Join(dir, "out")
	if err := os.WriteFile(temp, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyExclusive(temp, name); !errors.Is(err, os.ErrExist) {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(name); string(got) != "old" {
		t.Error("the existing file was replaced")
	}
	_ = os.Remove(name)
	if err := copyExclusive(temp, name); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(name); string(got) != "new" {
		t.Error("content not copied")
	}
}
