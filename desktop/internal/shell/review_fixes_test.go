package shell

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

// breakStore makes the next connection-list write fail.
func (h *harness) breakStore() {
	h.t.Helper()
	dir := filepath.Dir(h.store.path)
	if err := os.Chmod(dir, 0o500); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if f, err := os.CreateTemp(dir, "probe"); err == nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		h.t.Skip("the directory stayed writable (running as root?)")
	}
}

func TestFailedRecordWriteKeepsThePreviousSession(t *testing.T) {
	h := newHarness(t)
	c, old := h.connect()
	h.breakStore()
	tok := newToken(t)
	err := h.m.acceptLogin(context.Background(), c, loginResult(tok, "ses_0198ffff-0000-7000-8000-000000000001", "usr_0198ffff-0000-7000-8000-000000000002"))
	if err == nil {
		t.Fatal("the sign-in succeeded although the record could not be written")
	}
	h.m.Wait()
	if c.bearer() != old.Token || c.Info().PrincipalID != old.PrincipalID {
		t.Errorf("the connection went active with the new session: principal %q", c.Info().PrincipalID)
	}
	if got, gerr := h.secrets.Get(c.host); gerr != nil || got != old {
		t.Errorf("the keychain holds %+v (%v), want the previous item", got, gerr)
	}
	if n := len(h.inst.requests("POST", "/api/v1/auth/logout")); n != 1 {
		t.Errorf("the unused session was revoked %d times, want once", n)
	}
}

func TestFailedRecordWriteLeavesNoKeychainItemForANewConnection(t *testing.T) {
	h := newHarness(t)
	h.playBrowser()
	h.breakStore()
	if _, err := h.m.Add(context.Background(), AddRequest{Origin: h.inst.srv.URL, Mode: clienttrust.ModeSystemCA}); err == nil {
		t.Fatal("Add succeeded without a record")
	}
	h.m.Wait()
	if len(h.secrets.items) != 0 {
		t.Errorf("an orphaned keychain item remains: %d", len(h.secrets.items))
	}
}

func TestKeychainReplacementFailures(t *testing.T) {
	login := func(h *harness, c *Conn) error {
		return h.m.acceptLogin(context.Background(), c, loginResult(newToken(t), h.inst.loginSID, h.inst.loginPID))
	}
	t.Run("before the old item is touched the old session stays", func(t *testing.T) {
		h := newHarness(t)
		c, old := h.connect()
		h.secrets.FailPut = errors.New("denied")
		if login(h, c) == nil {
			t.Fatal("expected a failure")
		}
		if c.Stopped() != "" || c.bearer() != old.Token {
			t.Errorf("the connection lost a session that is still in the keychain: %q", c.Stopped())
		}
	})
	t.Run("after the old item is gone the connection asks for a sign-in", func(t *testing.T) {
		h := newHarness(t)
		c, _ := h.connect()
		h.secrets.FailPutAfterDelete = errors.New("add failed")
		if login(h, c) == nil {
			t.Fatal("expected a failure")
		}
		if c.Stopped() == "" || c.bearer() != "" {
			t.Error("the connection kept using a session that is no longer in the keychain")
		}
	})
}

func TestSignInCancelledBeforeItCompletesStoresNothing(t *testing.T) {
	h := newHarness(t)
	c, old := h.connect()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.m.acceptLogin(ctx, c, loginResult(newToken(t), h.inst.loginSID, h.inst.loginPID)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	h.m.Wait()
	if got, _ := h.secrets.Get(c.host); got != old {
		t.Error("a cancelled sign-in replaced the session")
	}
}

func TestRotationForAReplacedSessionIsDiscardedWithoutStopping(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	stale := newToken(t) // the bearer a request carried before a sign-in replaced it
	err := c.applyRotation(Rotate, tokenView{Token: newToken(t), SessionID: "ses_other"}, stale)
	if !errors.Is(err, errSuperseded) {
		t.Fatalf("err = %v", err)
	}
	h.m.Wait()
	if c.Stopped() != "" || c.bearer() != secret.Token {
		t.Error("an obsolete response stopped or replaced the current session")
	}
	if n := len(h.inst.requests("POST", "/api/v1/auth/logout")); n != 1 {
		t.Errorf("the returned token was revoked %d times, want once", n)
	}
}

func TestSessionChangesAreSerialized(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	var mu sync.Mutex
	inFlight, peak, calls := 0, 0, 0
	release := make(chan struct{})
	var releaseOnce sync.Once
	free := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(free) // a failed assertion must not leave the handler blocked
	entered := make(chan struct{}, 2)
	h.inst.on("POST /api/v1/auth/totp/step-up", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		inFlight++
		calls++
		peak = max(peak, inFlight)
		mu.Unlock()
		entered <- struct{}{}
		<-release
		mu.Lock()
		inFlight--
		mu.Unlock()
		writeJSON(w, 200, rotateBody(newToken(t), secret.SessionID, h.inst.loginPID))
	})
	h.inst.on("GET /api/v1/orgs", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]any{"ok": true}) })
	codes := make(chan int, 2)
	for range 2 {
		go func() { codes <- h.page(c, "POST", "/api/v1/auth/totp/step-up", `{"code":"123456"}`).Code }()
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("no step-up reached the instance")
	}
	// Ordinary reads are not held up by the gate.
	read := make(chan int, 1)
	go func() { read <- h.page(c, "GET", "/api/v1/orgs", "").Code }()
	select {
	case code := <-read:
		if code != 200 {
			t.Errorf("a read failed: %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Error("a read waited behind a session change")
	}
	select {
	case <-entered:
		t.Error("a second session change reached the instance while the first was running")
	case <-time.After(300 * time.Millisecond):
	}
	free()
	for range 2 {
		select {
		case code := <-codes:
			if code != 200 {
				t.Errorf("a queued session change was rejected: %d", code)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("a session change never finished")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if peak != 1 || calls != 2 {
		t.Errorf("peak %d, calls %d: want 1 at a time and both delivered", peak, calls)
	}
}

func TestAStreamWhoseBearerChangedBeforeItsHeadersIsCancelled(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	free := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(free) // a failed rotation must not leave the handler blocked
	h.inst.on("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	done := make(chan struct{})
	go func() { defer close(done); h.page(c, "GET", "/api/v1/events", "") }()
	<-entered
	if err := c.applyRotation(Rotate, tokenView{Token: newToken(t), SessionID: secret.SessionID}, secret.Token); err != nil {
		t.Fatal(err)
	}
	free()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a stream sent with the old bearer stayed open")
	}
}

func TestErrorResponsesAreScannedForSessionTokens(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	leak := newToken(t)
	h.inst.on("GET /api/v1/orgs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 400, map[string]string{"error": "bad", "session_token": leak})
	})
	h.inst.on("GET /api/v1/teams", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 403, map[string]string{"error": "forbidden"})
	})
	rec := h.page(c, "GET", "/api/v1/orgs", "")
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), leak) {
		t.Errorf("a token in an error body reached the page: %d %s", rec.Code, rec.Body)
	}
	h.m.Wait()
	if got := h.inst.requests("POST", "/api/v1/auth/logout"); len(got) != 1 || got[0].Header.Get("Authorization") != "Bearer "+leak {
		t.Errorf("the unexpected session was not revoked: %+v", got)
	}
	if c.bearer() == leak {
		t.Error("a token from an error response was installed")
	}
	if rec := h.page(c, "GET", "/api/v1/teams", ""); rec.Code != 403 || !strings.Contains(rec.Body.String(), "forbidden") {
		t.Errorf("a token-free error was not preserved: %d %s", rec.Code, rec.Body)
	}
}

func TestAnUnreadableSessionChangeStopsTheConnection(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.inst.on("POST /api/v1/auth/totp/step-up", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write([]byte("not gzip"))
	})
	if rec := h.page(c, "POST", "/api/v1/auth/totp/step-up", `{"code":"123456"}`); rec.Code != http.StatusBadGateway {
		t.Errorf("status %d", rec.Code)
	}
	if c.Stopped() == "" {
		t.Error("a session change the app could not read left the connection active")
	}
	// A failed one is not a session change: the connection stays.
	c2, _ := h.connect()
	h.inst.on("POST /api/v1/auth/totp/step-up", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "br")
		w.WriteHeader(400)
	})
	h.page(c2, "POST", "/api/v1/auth/totp/step-up", `{"code":"123456"}`)
	if c2.Stopped() != "" {
		t.Error("an unreadable error response stopped the connection")
	}
}

func TestARedirectPathCannotNameAnotherHost(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.inst.on("GET /api/v1/jump", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, h.inst.srv.URL+"//c-0123456789abcdef0123456789abcdef/x", http.StatusFound)
	})
	loc := h.page(c, "GET", "/api/v1/jump", "").Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil || u.Scheme != PageScheme || u.Host != c.host {
		t.Errorf("Location %q leaves the connection (%v)", loc, err)
	}
}

func TestSchemaVisitedUnderLoginIsStillSearchedAtTheRoot(t *testing.T) {
	token := openapi3.NewStringSchema()
	inner := &openapi3.Schema{Properties: openapi3.Schemas{"login": &openapi3.SchemaRef{Value: &openapi3.Schema{
		Properties: openapi3.Schemas{"session_token": &openapi3.SchemaRef{Value: token}}}}}}
	shared := &openapi3.SchemaRef{Value: inner}
	viaLogin := &openapi3.SchemaRef{Value: &openapi3.Schema{Properties: openapi3.Schemas{"login": shared}}}
	root := &openapi3.SchemaRef{Value: &openapi3.Schema{AllOf: openapi3.SchemaRefs{viaLogin, shared}}}
	if !schemaHasToken(root, map[visit]bool{}, true) {
		t.Error("a login.session_token reachable at the root was missed after a nested visit")
	}
}

func TestSplitTokenTellsTruncatedJSONFromAValidArray(t *testing.T) {
	if _, err := splitToken([]byte(`[1,2`)); err == nil || errors.Is(err, errNotObject) {
		t.Errorf("truncated array: %v", err)
	}
	if _, err := splitToken([]byte(` [1,2]`)); !errors.Is(err, errNotObject) {
		t.Errorf("valid array: %v", err)
	}
}

func TestStoreRefusesWhatItCannotReload(t *testing.T) {
	if validHost("c-0123456789ABCDEF0123456789abcdef") {
		t.Error("an uppercase host suffix was accepted")
	}
	h := newHarness(t)
	c, _ := h.connect()
	info := c.Info()
	info.Name = strings.Repeat("x", maxStoreSize)
	before, _ := os.ReadFile(h.store.path)
	if err := h.store.Put(info); err == nil {
		t.Fatal("an oversized list was written")
	}
	if after, _ := os.ReadFile(h.store.path); string(after) != string(before) {
		t.Error("the previous file was replaced")
	}
	if _, err := OpenStore(h.store.path); err != nil {
		t.Errorf("the file no longer loads: %v", err)
	}
}

func TestMetaClientErrorIsARefusalAndServerErrorIsNot(t *testing.T) {
	for status, refused := range map[int]bool{404: true, 403: true, 500: false, 503: false, 429: false} {
		h := newHarness(t)
		c, _ := h.connect()
		h.inst.on("GET /api/v1/meta", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		err := h.m.OpenConnection(c.host)
		var se *shellErr
		got := errors.As(err, &se) && se.code == codeCapabilityMissing
		if got != refused {
			t.Errorf("meta %d: refused=%v, want %v (%v)", status, got, refused, err)
		}
		if !refused && (err != nil || h.native.opened[c.host] == "") {
			t.Errorf("meta %d: the window did not open (%v)", status, err)
		}
	}
}

func TestRemoveWithAFailedWipeKeepsAnUnusableRecordUntilItSucceeds(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	h.native.wipeErr = errors.New("web view silent")
	res, err := h.m.Remove(context.Background(), c.host)
	if err != nil || !res.Revoked || !res.StorageKept || res.Message == "" {
		t.Fatalf("remove: %+v %v", res, err)
	}
	if _, gerr := h.secrets.Get(c.host); !errors.Is(gerr, ErrNoSecret) {
		t.Error("the keychain item was kept")
	}
	list := h.store.List()
	if len(list) != 1 || !list[0].WipePending {
		t.Fatalf("the cleanup record is missing: %+v", list)
	}
	// It survives a restart and can neither open nor sign in.
	m2 := h.reload()
	if err := m2.OpenConnection(c.host); err == nil {
		t.Error("a connection with owed cleanup opened")
	}
	if err := m2.SignIn(context.Background(), c.host, ""); err == nil {
		t.Error("a connection with owed cleanup signed in")
	}
	// The retry revokes nothing again and deletes the record once the wipe works.
	h.native.wipeErr = nil
	logouts := len(h.inst.requests("POST", "/api/v1/auth/logout"))
	if res, err := m2.Remove(context.Background(), c.host); err != nil || res.StorageKept {
		t.Fatalf("retry: %+v %v", res, err)
	}
	if len(h.store.List()) != 0 || len(h.inst.requests("POST", "/api/v1/auth/logout")) != logouts {
		t.Error("the retry kept the record or revoked again")
	}
	_ = secret
}

func TestAnUnusableRecordDoesNotBlockStartup(t *testing.T) {
	for name, edit := range map[string]func(*Connection){
		"https spki without a pin": func(c *Connection) {
			c.Origin, c.Trust = "https://instance.example", Trust{Mode: clienttrust.ModeSPKI}
		},
		"loopback http without the switch": func(*Connection) {},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			c, _ := h.connect()
			info := c.Info()
			edit(&info)
			if err := h.store.Put(info); err != nil {
				t.Fatal(err)
			}
			opts := h.m.opts
			opts.AllowLoopbackHTTP = false
			m2, err := NewManager(h.store, h.secrets, h.native, opts)
			if err != nil {
				t.Fatalf("startup failed: %v", err)
			}
			t.Cleanup(func() { m2.Wait(); m2.Close() })
			c2 := m2.conn(info.Host)
			if c2 == nil || c2.Stopped() == "" {
				t.Fatal("the unusable connection is missing or active")
			}
			if len(m2.State().Connections) != 1 {
				t.Error("the list does not show it")
			}
			if err := m2.OpenConnection(info.Host); err == nil {
				t.Error("an unusable connection opened")
			}
			if rec := httptest.NewRecorder(); true {
				req := httptest.NewRequestWithContext(context.Background(), "GET", "http://"+info.Host+"/", nil)
				req.Host = info.Host
				m2.BindWindow("w", info.Host)
				req.Header.Set("X-Wails-Window-Id", "w")
				m2.ServeHTTP(rec, req)
				if rec.Code != http.StatusNotFound {
					t.Errorf("a window reached an unusable connection: %d", rec.Code)
				}
			}
			if _, err := m2.Remove(context.Background(), info.Host); err != nil || len(h.store.List()) != 0 {
				t.Errorf("remove: %v, %d records left", err, len(h.store.List()))
			}
		})
	}
}

func TestDownloadRetriesAStaleBearerOnce(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	h.savingTo("out")
	next := newToken(t)
	var mu sync.Mutex
	calls := 0
	h.inst.on("GET /api/v1/export", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		if r.Header.Get("Authorization") == "Bearer "+secret.Token {
			// Another operation rotated the session while this request was out.
			if err := c.applyRotation(Rotate, tokenView{Token: next, SessionID: secret.SessionID}, secret.Token); err != nil {
				t.Error(err)
			}
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		_, _ = io.WriteString(w, "a,b\n")
	})
	if rec := h.page(c, "POST", "/__shell/download", `{"path":"/api/v1/export","filename":"e.csv"}`); rec.Code >= 400 {
		t.Fatalf("download: %d %s", rec.Code, rec.Body)
	}
	if calls != 2 {
		t.Errorf("the instance saw %d requests, want 2", calls)
	}
	// An unchanged bearer that is rejected stays a failure.
	h.inst.on("GET /api/v1/export", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) })
	if rec := h.page(c, "POST", "/__shell/download", `{"path":"/api/v1/export","filename":"e.csv"}`); rec.Code < 400 {
		t.Errorf("a 401 with an unchanged bearer succeeded: %d", rec.Code)
	}
}

func TestDownloadRevokesAnUnexpectedSessionToken(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.savingTo("out")
	leak := newToken(t)
	h.inst.on("GET /api/v1/export", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"session_token": leak})
	})
	if rec := h.page(c, "POST", "/__shell/download", `{"path":"/api/v1/export","filename":"e.json"}`); rec.Code < 400 {
		t.Fatalf("download of a token body: %d", rec.Code)
	}
	h.m.Wait()
	if got := h.inst.requests("POST", "/api/v1/auth/logout"); len(got) != 1 || got[0].Header.Get("Authorization") != "Bearer "+leak {
		t.Errorf("the unexpected session was not revoked: %+v", got)
	}
}

func TestDownloadEndsWhenTheServerStopsSending(t *testing.T) {
	old := downloadIdle
	downloadIdle = 200 * time.Millisecond
	t.Cleanup(func() { downloadIdle = old })
	h := newHarness(t)
	c, _ := h.connect()
	h.savingTo("out")
	stall := make(chan struct{})
	t.Cleanup(func() { close(stall) })
	h.inst.on("GET /api/v1/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "partial")
		w.(http.Flusher).Flush()
		select {
		case <-stall:
		case <-r.Context().Done():
		}
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- h.page(c, "POST", "/__shell/download", `{"path":"/api/v1/export","filename":"e.csv"}`) }()
	select {
	case rec := <-done:
		if rec.Code < 400 {
			t.Errorf("a stalled download reported success: %d", rec.Code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a stalled download never ended")
	}
	if _, busy := c.downloadSlot.take(); !busy {
		t.Error("the download slot stayed taken")
	}
}

func TestPublishDownloadNeverReplacesAFileThatAppearedLater(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "report.csv")
	stage := func(content string) string {
		f, err := os.CreateTemp(dir, ".stage-*")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString(content)
		_ = f.Close()
		return f.Name()
	}
	if err := os.WriteFile(final, []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved, err := PublishDownload(stage("mine"), final, false)
	if err != nil || saved != filepath.Join(dir, "report (2).csv") {
		t.Fatalf("saved %q: %v", saved, err)
	}
	if got, _ := os.ReadFile(final); string(got) != "theirs" {
		t.Errorf("the existing file was replaced: %q", got)
	}
	// A name the panel saw and the person approved is replaced.
	if saved, err := PublishDownload(stage("mine"), final, true); err != nil || saved != final {
		t.Fatalf("approved replacement: %q %v", saved, err)
	}
	if got, _ := os.ReadFile(final); string(got) != "mine" {
		t.Errorf("the approved replacement did not happen: %q", got)
	}
}

func TestGzipHelperIsStillAccepted(t *testing.T) {
	// A response that really is gzip is still read (the scheme handler's bytes
	// are not decoded by WebKit).
	h := newHarness(t)
	c, _ := h.connect()
	h.inst.on("GET /api/v1/orgs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		_, _ = zw.Write([]byte(`{"ok":true}`))
		_ = zw.Close()
	})
	if rec := h.page(c, "GET", "/api/v1/orgs", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

func TestFinishDownloadsWaitsForAPublicationInProgress(t *testing.T) {
	publishing.RLock() // a publication in progress
	var endOnce sync.Once
	endPublication := func() { endOnce.Do(publishing.RUnlock) }
	t.Cleanup(endPublication) // a failed assertion must not keep the lock for later tests
	done := make(chan struct{})
	go func() {
		FinishDownloads()
		publishing.Unlock() // later tests publish again, whatever the test saw
		close(done)
	}()
	// A pending writer makes TryRLock fail, so FinishDownloads is now waiting.
	deadline := time.Now().Add(5 * time.Second)
	for publishing.TryRLock() {
		publishing.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("FinishDownloads never started waiting")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("FinishDownloads returned while a publication was in progress")
	default:
	}
	endPublication()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("FinishDownloads did not return after the publication ended")
	}
}
