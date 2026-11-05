//go:build isorig

package main

import (
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
)

var rigCSP = os.Getenv("HIKYO_SPIKE_CSP") != ""

// logSignals records what the shell can see about a request, before any check, for the isolation rig.
func logSignals(r *http.Request) {
	var keys []string
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var vals []string
	for _, k := range []string{"Origin", "Referer", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "Sec-Fetch-User", "X-Wails-Window-Id", "Ping-From", "Ping-To"} {
		if v := r.Header.Get(k); v != "" {
			vals = append(vals, k+"="+v)
		}
	}
	log.Printf("SIGNAL %s %s%s keys=%s %s", r.Method, r.Host, r.URL.RequestURI(), strings.Join(keys, ","), strings.Join(vals, " "))
}
