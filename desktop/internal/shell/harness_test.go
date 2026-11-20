package shell

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

// fakeNative records what the shell asks of the OS and answers from fields.
type fakeNative struct {
	mu            sync.Mutex
	confirmAnswer bool
	confirms      []string
	alerts        []string
	savePath      string
	saveOK        bool
	saveReplace   bool // the panel reports an approved replacement
	closeErr      error
	onConfirm     func() // runs inside Confirm, as another operation would
	onClose       func() // runs inside CloseWindow, after the marker is stored
	saveNames     []string
	saveMIME      []string
	closed        []string
	wiped         []string
	wipeErr       error
	deviceCodes   []string
	pasteboard    int64
	activation    int64
	deviceURLs    []string
	deviceOpen    func()
	inactive      bool // another app is frontmost
	cleared       int
	opened        map[string]string // host -> window id
}

func newFakeNative() *fakeNative {
	return &fakeNative{confirmAnswer: true, opened: map[string]string{}}
}

func (f *fakeNative) OpenWindow(host, _ string, bind func(string)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "w-" + host
	f.opened[host] = id
	bind(id)
	return nil
}
func (f *fakeNative) CloseWindow(host string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, host)
	if f.onClose != nil {
		f.onClose()
	}
	return f.closeErr
}
func (f *fakeNative) WipeStorage(host string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wiped = append(f.wiped, host)
	return f.wipeErr
}
func (f *fakeNative) Confirm(title, message, _ string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirms = append(f.confirms, title+"\n"+message)
	if f.onConfirm != nil {
		f.onConfirm()
	}
	return f.confirmAnswer
}
func (f *fakeNative) Alert(title, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, title+"\n"+message)
}
func (f *fakeNative) AppActive() bool { f.mu.Lock(); defer f.mu.Unlock(); return !f.inactive }

func (f *fakeNative) SavePanel(name, _, mimeType string) (string, bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveNames = append(f.saveNames, name)
	f.saveMIME = append(f.saveMIME, mimeType)
	return f.savePath, f.saveReplace, f.saveOK
}
func (f *fakeNative) ShowDeviceCode(url, code string, _, open func()) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deviceCodes = append(f.deviceCodes, code)
	f.deviceURLs = append(f.deviceURLs, url)
	f.deviceOpen = open
	return func() {}
}
func (f *fakeNative) PasteboardCount() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pasteboard
}
func (f *fakeNative) PasteboardCountAtActivation() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.activation
}
func (f *fakeNative) ClearPasteboardIf(count int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pasteboard != count {
		return
	}
	f.cleared++
	f.pasteboard++
}

// seenRequest is a request the fake instance received.
type seenRequest struct {
	Method, Path, RawQuery string
	Header                 http.Header
	Body                   string
}

// fakeInstance stands in for a Hikyo server.
type fakeInstance struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	seen     []seenRequest
	handlers map[string]http.HandlerFunc
	caps     []string
	redirect string // loopback redirect_uri from cli-login/start
	loginTok string
	loginSID string
	loginPID string
}

func newFakeInstance(t *testing.T) *fakeInstance {
	f := &fakeInstance{t: t, handlers: map[string]http.HandlerFunc{}, caps: []string{"loopback-handoff", capabilityDesktop},
		loginSID: "ses_0198aaaa-0000-7000-8000-000000000001", loginPID: "usr_0198bbbb-0000-7000-8000-000000000001"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	f.on("GET /api/v1/meta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, apigen.Meta{ServerVersion: "1.1.0-test", ApiRevision: 20, ProtocolCapabilities: f.capabilities()})
	})
	f.on("POST /api/v1/auth/logout", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	f.on("POST /api/v1/auth/cli-login/start", func(w http.ResponseWriter, r *http.Request) {
		var in apigen.CLILoginStartRequest
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &in); err != nil {
			t.Errorf("start body: %v", err)
		}
		f.mu.Lock()
		f.redirect = in.RedirectUri
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, apigen.CLILoginStart{State: in.State, ExpiresAt: time.Now().Add(5 * time.Minute)})
	})
	f.on("POST /api/v1/auth/cli-login/redeem", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		tok, sid, pid := f.loginTok, f.loginSID, f.loginPID
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, loginResult(tok, sid, pid))
	})
	f.loginTok = newToken(t)
	return f
}

func (f *fakeInstance) capabilities() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.caps
}

func (f *fakeInstance) on(route string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[route] = h
}

func (f *fakeInstance) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.seen = append(f.seen, seenRequest{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), string(body)})
	h := f.handlers[r.Method+" "+r.URL.Path]
	f.mu.Unlock()
	if h == nil {
		http.NotFound(w, r)
		return
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	h(w, r)
}

func (f *fakeInstance) requests(method, path string) []seenRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []seenRequest
	for _, s := range f.seen {
		if s.Method == method && s.Path == path {
			out = append(out, s)
		}
	}
	return out
}

func (f *fakeInstance) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.MarshalWrite(w, v)
}

func newToken(t *testing.T) string {
	t.Helper()
	tok, _, err := crypto.NewArtifact(crypto.ArtifactCLISession)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func loginResult(token, sessionID, principalID string) apigen.LoginResult {
	now := time.Now().UTC()
	name := "Test Person"
	return apigen.LoginResult{
		SessionToken: &token,
		Principal:    apigen.Principal{Id: principalID, Kind: "human", DisplayName: &name},
		Session: apigen.Session{
			Id: sessionID, Artifact: "cli", CreatedAt: now,
			AbsoluteExpiresAt: now.Add(90 * 24 * time.Hour), IdleExpiresAt: now.Add(30 * 24 * time.Hour),
		},
	}
}

// harness wires a Manager to a fake instance and a fake OS.
type harness struct {
	t       *testing.T
	m       *Manager
	native  *fakeNative
	secrets *MemorySecrets
	store   *Store
	inst    *fakeInstance
	opened  []string
	mu      sync.Mutex
	// browser lets a test play the user's browser when the shell opens a URL.
	browser func(target string)
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, native: newFakeNative(), secrets: &MemorySecrets{}, inst: newFakeInstance(t)}
	var err error
	if h.store, err = OpenStore(t.TempDir() + "/connections.json"); err != nil {
		t.Fatal(err)
	}
	h.m, err = NewManager(h.store, h.secrets, h.native, Options{
		Version: "0.0.0-test", AllowLoopbackHTTP: true,
		OpenURL: func(u string) error {
			h.mu.Lock()
			h.opened = append(h.opened, u)
			browser := h.browser
			h.mu.Unlock()
			if browser != nil {
				go browser(u)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.m.Wait(); h.m.Close() })
	return h
}

// openedURLs returns the URLs the shell opened in the browser.
func (h *harness) openedURLs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.opened...)
}

// connect stores a signed-in connection to the fake instance and binds its window.
func (h *harness) connect() (*Conn, Secret) {
	h.t.Helper()
	host, err := h.store.NewHost()
	if err != nil {
		h.t.Fatal(err)
	}
	info := Connection{Host: host, Origin: h.inst.srv.URL, Name: "Test", Trust: Trust{Mode: clienttrust.ModeSystemCA},
		PrincipalID: h.inst.loginPID, PrincipalName: "Test Person", Created: time.Now()}
	secret := Secret{Token: newToken(h.t), SessionID: h.inst.loginSID, Origin: info.Origin,
		TrustMode: info.Trust.Mode, Pin: info.Trust.Pin, PrincipalID: info.PrincipalID}
	if err := h.secrets.Put(host, secret); err != nil {
		h.t.Fatal(err)
	}
	if err := h.store.Put(info); err != nil {
		h.t.Fatal(err)
	}
	c, err := h.m.newConn(info)
	if err != nil {
		h.t.Fatal(err)
	}
	h.m.mu.Lock()
	h.m.conns[host] = c
	h.m.mu.Unlock()
	h.m.BindWindow("win-"+host, host)
	return c, secret
}

// page sends a request as the connection's own window.
func (h *harness) page(c *Conn, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	return h.pageFrom(c.host, "win-"+c.host, method, path, body, headers...)
}

func (h *harness) pageFrom(host, windowID, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, "http://"+host+path, strings.NewReader(body))
	req.Host = host
	req.Header.Set("X-Wails-Window-Id", windowID)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.m.ServeHTTP(rec, req.WithContext(context.Background()))
	return rec
}
