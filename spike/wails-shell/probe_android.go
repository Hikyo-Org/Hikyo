//go:build android && probe

package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func secureSet(k, v string) error              { return application.Android.SecureSet(k, v) }
func secureGet(k string) (string, bool, error) { return application.Android.SecureGet(k) }
func secureDelete(k string) error              { return application.Android.SecureDelete(k) }

// Safari has no counterpart: the only hop is the browser the Intent picks. Mode "fgs" first starts the shortService
// foreground service (see android-host/java/WailsForegroundService.java), while the app is still visible, and the callback stops it.
func hopOpen(mode, u string) error {
	if mode == "fgs" {
		startHopService()
	}
	return openURL(u)
}

func startHopService() {
	application.Android.StartForegroundService(`{"title":"Signing in","text":"Waiting for the browser"}`)
}

func hopDone(mode string) {
	if mode == "fgs" {
		application.Android.StopForegroundService()
	}
}

func probeRoutes(handle func(string, bool, http.HandlerFunc)) {
	// Sets the padding added to event streams, in bytes, to measure how much the WebView needs; 0 turns it off.
	handle("/sse-pad", true, func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		ssePad.Store(int64(n))
		fmt.Fprintln(w, "pad", n)
	})
	// Starts or stops the service on its own, for a measurement that does not need the browser first.
	handle("/fgs", true, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stop") == "1" {
			hopDone("fgs")
		} else {
			startHopService()
		}
		fmt.Fprintln(w, "ok")
	})
}
