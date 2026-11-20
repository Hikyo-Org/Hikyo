package shell

import (
	"context"
	"embed"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

//go:embed ui/index.html ui/app.js ui/app.css
var uiFiles embed.FS

// uiCSP confines the connection list: it loads nothing but its own files.
const uiCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

var uiAssets = map[string]struct{ file, contentType string }{
	"/":           {"ui/index.html", "text/html; charset=utf-8"},
	"/index.html": {"ui/index.html", "text/html; charset=utf-8"},
	"/app.js":     {"ui/app.js", "text/javascript; charset=utf-8"},
	"/app.css":    {"ui/app.css", "text/css; charset=utf-8"},
}

type connectionView struct {
	Host      string `json:"host"`
	Name      string `json:"name"`
	Origin    string `json:"origin"`
	Trust     string `json:"trust"`
	Pin       string `json:"pin,omitzero"`
	Principal string `json:"principal,omitzero"`
	Stopped   string `json:"stopped,omitzero"`
}

type stateView struct {
	Version     string           `json:"version"`
	Connections []connectionView `json:"connections"`
	Busy        bool             `json:"busy"`
}

// State is what the connection list shows.
func (m *Manager) State() stateView {
	out := stateView{Version: m.opts.Version, Connections: []connectionView{}, Busy: m.appSlot.busy.Load()}
	for _, host := range m.Hosts() {
		c := m.conn(host)
		if c == nil {
			continue // removed after the host snapshot
		}
		info := c.Info()
		out.Connections = append(out.Connections, connectionView{
			Host: host, Name: info.Name, Origin: info.Origin, Trust: string(info.Trust.Mode),
			Pin: clienttrust.ShortPin(info.Trust.Pin), Principal: labelOf(info), Stopped: c.Stopped(),
		})
	}
	return out
}

// serveApp answers the connection list: its own files and a small JSON API.
// It's served only to the list's own window (ServeHTTP checks), and none of it
// is reachable from an instance's page.
func (m *Manager) serveApp(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if asset, ok := uiAssets[r.URL.Path]; ok && r.Method == http.MethodGet {
		data, err := uiFiles.ReadFile(asset.file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", asset.contentType)
		w.Header().Set("Content-Security-Policy", uiCSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(data)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/__app/state" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, m.State())
		return
	}
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var err error
	switch r.URL.Path {
	case "/__app/add":
		var in struct {
			Origin    string `json:"origin"`
			Name      string `json:"name,omitzero"`
			TrustMode string `json:"trust_mode"`
			Method    string `json:"method,omitzero"`
		}
		if e := decodeBody(r, 1<<14, &in); e != nil {
			writeShellError(w, e)
			return
		}
		err = m.pending(w, r, func(ctx context.Context) (any, error) {
			host, err := m.Add(ctx, AddRequest{Origin: in.Origin, Name: in.Name, Mode: clienttrust.Mode(in.TrustMode), Method: in.Method})
			return map[string]string{"host": host}, err
		})
	case "/__app/sign-in":
		var in struct {
			Host   string `json:"host"`
			Method string `json:"method,omitzero"`
		}
		if e := decodeBody(r, 1<<12, &in); e != nil {
			writeShellError(w, e)
			return
		}
		err = m.pending(w, r, func(ctx context.Context) (any, error) { return struct{}{}, m.SignIn(ctx, in.Host, in.Method) })
	case "/__app/remove":
		var in struct {
			Host string `json:"host"`
		}
		if e := decodeBody(r, 1<<12, &in); e != nil {
			writeShellError(w, e)
			return
		}
		err = m.pending(w, r, func(ctx context.Context) (any, error) { return m.Remove(ctx, in.Host) })
	case "/__app/open":
		var in struct {
			Host string `json:"host"`
		}
		if e := decodeBody(r, 1<<12, &in); e != nil {
			writeShellError(w, e)
			return
		}
		if err = m.OpenConnection(in.Host); err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	case "/__app/open-browser":
		// The capability refusal offers to open the instance in the browser.
		var in struct {
			Origin string `json:"origin"`
		}
		if e := decodeBody(r, 1<<12, &in); e != nil {
			writeShellError(w, e)
			return
		}
		origin, cerr := canonicalOrigin(in.Origin)
		if u, perr := url.Parse(origin); cerr != nil || perr != nil || u.Scheme != "https" {
			writeShellError(w, &shellErr{codeInvalid, "that is not an https origin"})
			return
		}
		if err = m.opts.OpenURL(origin); err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	case "/__app/cancel":
		m.cancelPending()
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		var se *shellErr
		if errors.As(err, &se) {
			writeShellError(w, se)
			return
		}
		writeShellError(w, &shellErr{codeFailed, err.Error()})
	}
}

// pending runs a long operation from the list, one at a time. The user can
// cancel it with /__app/cancel, and closing the page cancels it too.
func (m *Manager) pending(w http.ResponseWriter, r *http.Request, run func(context.Context) (any, error)) error {
	// The slot is taken and the cancel registered under one lock, so a cancel
	// can't see the slot busy before there is something to cancel.
	m.pendingMu.Lock()
	release, ok := m.appSlot.take()
	if !ok {
		m.pendingMu.Unlock()
		return &shellErr{codeBusy, "another sign-in is already waiting"}
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 11*time.Minute)
	defer cancel()
	m.pendingEnd = cancel
	m.pendingMu.Unlock()
	defer func() {
		m.pendingMu.Lock()
		m.pendingEnd = nil
		m.pendingMu.Unlock()
	}()
	value, err := run(ctx)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.MarshalWrite(w, value)
	return nil
}

func (m *Manager) cancelPending() {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	if m.pendingEnd != nil {
		m.pendingEnd()
	}
}
