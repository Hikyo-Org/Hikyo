//go:build ios && probe

package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func secureSet(k, v string) error              { return application.IOS.SecureSet(k, v) }
func secureGet(k string) (string, bool, error) { return application.IOS.SecureGet(k) }
func secureDelete(k string) error              { return application.IOS.SecureDelete(k) }

// hopOpen sends the user to the browser: mode "ases" uses ASWebAuthenticationSession, anything else Safari.
func hopOpen(mode, u string) error {
	if mode == "ases" {
		asesStart(u)
		return nil
	}
	return openURL(u)
}

// hopDone closes the session: nothing tells it the page it shows is done, so native code does.
func hopDone(mode string) {
	if mode == "ases" {
		go func() { time.Sleep(500 * time.Millisecond); asesCancel() }()
	}
}

func probeRoutes(handle func(string, bool, http.HandlerFunc)) {
	handle("/ases", true, func(w http.ResponseWriter, r *http.Request) {
		log.Printf("OPEN-ASES %s %s", stamp(), r.URL.Query().Get("url"))
		asesStart(r.URL.Query().Get("url"))
		fmt.Fprintln(w, "started")
	})
	handle("/ases/cancel", true, func(w http.ResponseWriter, r *http.Request) { asesCancel(); fmt.Fprintln(w, "cancelled") })
	handle("/policy", true, func(w http.ResponseWriter, r *http.Request) { replaceNavigationPolicy(); fmt.Fprintln(w, "replaced") })
}
