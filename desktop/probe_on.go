//go:build darwin && probe

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/Hikyo-Org/hikyo/desktop/internal/shell"
	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

// Probe builds (`-tags probe`) exist for driven checks of the renderer policy
// (ADR D10). They are never part of a release.

// devDataDir and devAllowLoopbackHTTP read the development variables, in dev
// and probe builds only (see probe_off.go).
func devDataDir() string { return os.Getenv("HIKYO_DESKTOP_HOME") }
func devAllowLoopbackHTTP() bool {
	return os.Getenv("HIKYO_DESKTOP_ALLOW_LOOPBACK_HTTP") == "1"
}

// installProbe runs the script in HIKYO_DESKTOP_PROBE_JS after every
// navigation of a window. Scripts dropped into HIKYO_DESKTOP_PROBE_CMD_DIR as
// <host>.js run once in that host's window (the connection list is "app").
func installProbe(w *application.WebviewWindow, host string) {
	path := os.Getenv("HIKYO_DESKTOP_PROBE_JS")
	var ready atomic.Bool
	w.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
		// ExecJS queues until the Wails runtime reports ready, which this shell never loads.
		if !ready.Load() {
			w.HandleMessage("wails:runtime:ready")
			ready.Store(true)
		}
		if path == "" {
			return
		}
		script, err := os.ReadFile(path)
		if err != nil {
			log.Printf("probe: %v", err)
			return
		}
		w.ExecJS(string(script))
	})
	dir := os.Getenv("HIKYO_DESKTOP_PROBE_CMD_DIR")
	if dir == "" {
		return
	}
	name := host
	if host == shell.AppHost {
		name = "app"
	}
	// The poller ends with its window, so a reopened host's command file is
	// never taken by the old one.
	closed := make(chan struct{})
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { close(closed) })
	go func() {
		file := filepath.Join(dir, name+".js")
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-closed:
				return
			case <-tick.C:
			}
			script, err := os.ReadFile(file)
			if err != nil || !ready.Load() {
				continue
			}
			_ = os.Remove(file)
			log.Printf("probe: running %s (%d bytes)", file, len(script))
			w.ExecJS(string(script))
		}
	}()
}

// probeSeed adds one connection to HIKYO_DESKTOP_PROBE_SEED (an http loopback
// origin) with a fake token, before the manager loads the store.
func probeSeed(s *shell.Store) string {
	origin := os.Getenv("HIKYO_DESKTOP_PROBE_SEED")
	if origin == "" {
		return ""
	}
	host, err := s.NewHost()
	if err != nil {
		log.Fatal(err)
	}
	info := shell.Connection{Host: host, Origin: origin, Name: "probe", Trust: shell.Trust{Mode: clienttrust.ModeSystemCA},
		PrincipalID: "usr_probe", PrincipalName: "Probe", Created: time.Now()}
	if err := s.Put(info); err != nil {
		log.Fatal(err)
	}
	// Recorded before the keychain write, outside the renderer log, so a run
	// that dies early still has its item cleaned up.
	if dir := devDataDir(); dir != "" {
		if f, err := os.OpenFile(filepath.Join(dir, "seeded"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_, _ = f.WriteString(host + "\n")
			_ = f.Close()
		}
	}
	if err := (keychain{}).Put(host, shell.Secret{Token: "probe-token", SessionID: "ses_probe", Origin: origin, TrustMode: clienttrust.ModeSystemCA, PrincipalID: "usr_probe"}); err != nil {
		log.Fatal(err)
	}
	return host
}

// probeOpen opens the seeded connection's window.
func probeOpen(m *shell.Manager, host string) {
	if host == "" {
		return
	}
	if err := m.OpenConnection(host); err != nil {
		log.Printf("probe: %v", err)
	}
}

// probeNative answers the native dialogs without a click so a driven run can
// get past them. HIKYO_DESKTOP_PROBE_CONFIRM=1 accepts every confirmation (its
// text is logged); HIKYO_DESKTOP_PROBE_SAVE_DIR makes the save panel return
// that directory plus the suggested name.
func probeNative(n shell.Native) shell.Native { return probeDialogs{n} }

type probeDialogs struct{ shell.Native }

func (p probeDialogs) Confirm(title, message, label string) bool {
	if os.Getenv("HIKYO_DESKTOP_PROBE_CONFIRM") != "1" {
		return p.Native.Confirm(title, message, label)
	}
	log.Printf("probe: confirm %q accepted: %s", title, message)
	return true
}

func (p probeDialogs) SavePanel(name, source, mimeType string) (string, bool, bool) {
	dir := os.Getenv("HIKYO_DESKTOP_PROBE_SAVE_DIR")
	if dir == "" {
		return p.Native.SavePanel(name, source, mimeType)
	}
	log.Printf("probe: save panel for %q from %q", name, source)
	dest := filepath.Join(dir, name)
	_, err := os.Lstat(dest)
	return dest, err == nil, true
}

// probeOpener appends the URL the shell wants opened to
// HIKYO_DESKTOP_PROBE_OPEN_LOG instead of starting a browser; a driver reads
// the file and plays the browser's part.
func probeOpener(open func(string) error) func(string) error {
	path := os.Getenv("HIKYO_DESKTOP_PROBE_OPEN_LOG")
	if path == "" {
		return open
	}
	return func(u string) error {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		_, err = f.WriteString(u + "\n")
		return err
	}
}

// probeMiddleware gives a probe script a way to report: a POST to
// /__probe/log on its own origin is written to the log.
func probeMiddleware(next func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		inner := next(h)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/__probe/log" && r.Method == http.MethodPost {
				body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
				log.Printf("probe-js [%s]: %s", r.Host, body)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if r.URL.Path == "/__probe/secret" && r.Method == http.MethodGet {
				// Fingerprints only, so a run can see a rotation without a token leaving the keychain.
				secret, err := keychain{}.Get(r.Host)
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				sum := sha256.Sum256([]byte(secret.Token))
				_ = json.MarshalWrite(w, map[string]string{"token_fp": hex.EncodeToString(sum[:4]), "session_id": secret.SessionID})
				return
			}
			inner.ServeHTTP(w, r)
		})
	}
}
