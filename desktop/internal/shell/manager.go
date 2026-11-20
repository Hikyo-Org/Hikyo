package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// AppHost is the origin host of the connection list, served by the shell.
	AppHost = "app"
	// PageScheme is the scheme every window loads.
	PageScheme = "wails"

	capabilityDesktop = "desktop-app/1"
)

// RequiredServerVersion is the first server release that advertises
// desktop-app/1. shortcut: set when plan PR 1 ships a tagged release; the
// refusal text names it.
var RequiredServerVersion = "1.1.0"

// Native is everything the shell asks of the operating system. The macOS
// implementation lives in the darwin files; tests supply a fake.
type Native interface {
	// OpenWindow creates the window for host, or focuses it. bind is called
	// with the window's id before the window loads anything, so the shell
	// can tell which window a request came from.
	OpenWindow(host, title string, bind func(windowID string)) error
	// CloseWindow closes host's window and returns only once its web view is
	// torn down, so the storage can be wiped. An error means it is not known to
	// be gone.
	CloseWindow(host string) error
	// WipeStorage deletes the website data of the connection's origin.
	WipeStorage(host string) error
	// Confirm shows a blocking native confirmation.
	Confirm(title, message, confirmLabel string) bool
	// Alert shows a non-blocking native message.
	Alert(title, message string)
	// SavePanel shows a blocking save panel with a suggested name. source
	// names the connection asking, so the user can tell which one it is.
	// mimeType, when set, is the page's type hint for the file. replace is true
	// only when the person was asked about, and accepted, replacing a file that
	// was at path when the panel answered; nothing else may be replaced.
	SavePanel(suggestedName, source, mimeType string) (path string, replace, ok bool)
	// AppActive reports whether the app is the frontmost application.
	AppActive() bool
	// ShowDeviceCode shows a device-flow code until dismiss is called. cancel
	// ends the sign-in when the user closes it; open opens the verification
	// URL in the browser and leaves the code on screen.
	ShowDeviceCode(verificationURL, code string, cancel, open func()) (dismiss func())
	PasteboardCount() int64
	// PasteboardCountAtActivation is the change count the instant the app last
	// became frontmost. Changes up to it were made elsewhere (ADR D12).
	PasteboardCountAtActivation() int64
	// ClearPasteboardIf clears the pasteboard only if its change count is still
	// count, checked and cleared as one step.
	ClearPasteboardIf(count int64)
}

// Options configure a Manager.
type Options struct {
	// Version is the app version, sent as User-Agent Hikyo-Desktop/<Version>.
	Version string
	// AllowLoopbackHTTP lets a connection use plain http to a loopback
	// origin. It is for local development against a hikyo server only.
	AllowLoopbackHTTP bool
	// OpenURL opens an http or https URL in the system browser.
	OpenURL func(string) error
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// Manager owns the connections and answers every request the windows make.
type Manager struct {
	store   *Store
	secrets SecretStore
	native  Native
	policy  *Policy
	opts    Options
	clip    *clipboardGuard

	mu      sync.RWMutex
	conns   map[string]*Conn
	windows map[string]string // window id -> host

	wg sync.WaitGroup

	appSlot    slot
	pendingMu  sync.Mutex
	pendingEnd context.CancelFunc
}

// NewManager loads the stored connections. A connection whose session can't be
// loaded is kept, stopped, so the user can sign in again.
func NewManager(store *Store, secrets SecretStore, native Native, opts Options) (*Manager, error) {
	policy, err := DefaultPolicy()
	if err != nil {
		return nil, err
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.OpenURL == nil {
		return nil, errors.New("Options.OpenURL is required")
	}
	m := &Manager{store: store, secrets: secrets, native: native, policy: policy, opts: opts,
		conns: map[string]*Conn{}, windows: map[string]string{}}
	m.clip = newClipboardGuard(native, m.now)
	for _, info := range store.List() {
		c, err := m.newConn(info)
		if err != nil { // an unusable record doesn't get here; see newConn
			return nil, fmt.Errorf("connection %s: %w", info.Name, err)
		}
		m.conns[info.Host] = c
	}
	return m, nil
}

func (m *Manager) now() time.Time { return m.opts.Now() }

func (m *Manager) userAgent() string { return "Hikyo-Desktop/" + m.opts.Version }

// background runs fn on a goroutine that Wait can join.
func (m *Manager) background(fn func()) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		fn()
	}()
}

// Wait blocks until background work (ceremonies, revocations) has finished.
func (m *Manager) Wait() { m.wg.Wait() }

// WatchClipboard polls the pasteboard until ctx ends.
func (m *Manager) WatchClipboard(ctx context.Context) {
	t := time.NewTicker(clipboardPoll)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			m.clip.observe()
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) conn(host string) *Conn {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conns[host]
}

// Conn returns the live connection for a host, or nil.
func (m *Manager) Conn(host string) *Conn { return m.conn(host) }

// Hosts lists the connection hosts in storage order.
func (m *Manager) Hosts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []string
	for _, info := range m.store.List() {
		if _, ok := m.conns[info.Host]; ok {
			out = append(out, info.Host)
		}
	}
	return out
}

// BindWindow records that windowID shows host. Requests from that window for
// any other host are refused.
func (m *Manager) BindWindow(windowID, host string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.windows[windowID] = host
}

// UnbindWindow forgets a closed window.
func (m *Manager) UnbindWindow(windowID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.windows, windowID)
}

func (m *Manager) windowHost(windowID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.windows[windowID]
}

// OpenConnection opens or focuses the connection's window.
func (m *Manager) OpenConnection(host string) error {
	c := m.conn(host)
	if c == nil {
		return errors.New("unknown connection")
	}
	if c.isTampered() {
		return &shellErr{codeRefused, c.Stopped()}
	}
	// The instance may have been downgraded since the connection was added
	// (ADR D8): check on every open and refuse by name. An instance that can't
	// be reached is not refused here; its window shows that itself.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var missing capabilityError
	if err := checkCapability(ctx, c.inst); errors.As(err, &missing) {
		return &shellErr{codeCapabilityMissing, c.Info().Name + ": " + missing.Error()}
	}
	// Held across the open so a removal either finishes first (refused here) or
	// starts after the window exists (and closes it).
	c.lifeMu.Lock()
	defer c.lifeMu.Unlock()
	if c.state != lifeActive || c.isTampered() {
		return &shellErr{codeRefused, c.Stopped()}
	}
	return m.native.OpenWindow(host, "Hikyo: "+c.Info().Name, func(id string) { m.BindWindow(id, host) })
}

// OpenApp opens or focuses the connection list.
func (m *Manager) OpenApp() error {
	return m.native.OpenWindow(AppHost, "Hikyo", func(id string) { m.BindWindow(id, AppHost) })
}

// Middleware returns the outermost handler of the shell's asset server: the
// only way a window's request reaches anything.
func (m *Manager) Middleware() func(http.Handler) http.Handler {
	return func(http.Handler) http.Handler { return m }
}

// ServeHTTP routes a request from a window. A request is served only for a
// known host, from the window bound to that host, with no foreign Origin.
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(r.Host)
	var c *Conn
	if host != AppHost {
		if c = m.conn(host); c == nil || c.inst == nil { // no instance: an unusable record
			http.NotFound(w, r)
			return
		}
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != PageScheme+"://"+host {
		// Without this, connection A could spend connection B's session on a
		// blind request.
		log.Printf("refused cross-connection request %s -> %s %s", origin, host, logSafe(r.URL.Path))
		jsonError(w, http.StatusForbidden, codeRefused, "cross-connection request refused")
		return
	}
	if m.windowHost(r.Header.Get("X-Wails-Window-Id")) != host {
		log.Printf("refused request from another window to %s %s", host, logSafe(r.URL.Path))
		jsonError(w, http.StatusForbidden, codeRefused, "this window belongs to another connection")
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/wails/"):
		jsonError(w, http.StatusForbidden, codeRefused, "the runtime is not exposed to pages")
	case c == nil:
		m.serveApp(w, r)
	case strings.HasPrefix(r.URL.Path, "/__shell/") || r.URL.Path == "/__shell":
		c.serveShell(w, r)
	default:
		c.serveProxy(w, r)
	}
}

// Close releases idle connections.
func (m *Manager) Close() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.conns {
		c.closeInstance()
	}
}
