//go:build darwin && probe

package main

import (
	"log"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/Hikyo-Org/hikyo/desktop/internal/shell"
	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

// Probe builds (`-tags probe`) exist for driven checks of the renderer policy
// (ADR D10). They are never part of a release.

// installProbe runs the script in HIKYO_DESKTOP_PROBE_JS after every
// navigation of a window.
func installProbe(w *application.WebviewWindow) {
	path := os.Getenv("HIKYO_DESKTOP_PROBE_JS")
	if path == "" {
		return
	}
	w.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
		script, err := os.ReadFile(path)
		if err != nil {
			log.Printf("probe: %v", err)
			return
		}
		// ExecJS queues until the Wails runtime reports ready, which this shell never loads.
		w.HandleMessage("wails:runtime:ready")
		w.ExecJS(string(script))
	})
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
	if err := (keychain{}).Put(host, shell.Secret{Token: "probe-token", SessionID: "ses_probe"}); err != nil {
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
