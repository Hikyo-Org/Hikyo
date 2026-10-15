package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
)

func TestRunReachesOnlyPinnedLoopbackCredentialEndpoint(t *testing.T) {
	for _, hostname := range []string{"localhost", "127.0.0.1"} {
		for _, matchingPin := range []bool{true, false} {
			t.Run(hostname+"/matching-pin="+strconv.FormatBool(matchingPin), func(t *testing.T) {
				requests := make(chan string, 1)
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == api.PathPrefix+"/meta" {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "fixture", ApiRevision: api.Revision})
						return
					}
					requests <- r.Method + " " + r.URL.Path
					http.Error(w, "stop after transport admission", http.StatusServiceUnavailable)
				}))
				defer server.Close()
				origin := strings.Replace(server.URL, "127.0.0.1", hostname, 1)
				pin := cli.SPKIFingerprint(server.Certificate())
				if !matchingPin {
					pin = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
				}
				directory := t.TempDir()
				if err := os.Chmod(directory, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "authority"), []byte("fixture-authority"), 0o600); err != nil {
					t.Fatal(err)
				}
				err := run(t.Context(), origin, pin, directory, "")
				if err == nil || err.Error() != "chartdoctor credential establishment failed" {
					t.Fatalf("run(%q) = %v, want credential endpoint attempt", origin, err)
				}
				select {
				case request := <-requests:
					if !matchingPin || request != "POST "+api.PathPrefix+"/auth/credential/establish" {
						t.Fatalf("unexpected credential request %q with matching pin=%t", request, matchingPin)
					}
				default:
					if matchingPin {
						t.Fatal("pinned loopback credential endpoint was not reached")
					}
				}
			})
		}
	}
}

func TestRunRefusesUnownedOrUnpinnedOriginsBeforeCustody(t *testing.T) {
	for _, origin := range []string{
		"http://localhost:18080", "https://localhost", "https://LOCALHOST:18080",
		"https://localhost.:18080", "https://localhost.evil:18080", "https://127.0.0.2:18080",
		"https://[::1]:18080", "https://user:password@localhost:18080", "https://localhost:18080/",
		"https://localhost:18080/path", "https://localhost:18080?query=1", "https://localhost:18080#fragment",
	} {
		t.Run(origin, func(t *testing.T) {
			err := run(t.Context(), origin, "pin", "missing-private-directory", "")
			if err == nil || err.Error() != "chartdoctor requires the owned pinned-TLS loopback port-forward origin" {
				t.Fatalf("origin %q was not refused before custody: %v", origin, err)
			}
		})
	}
	if err := run(t.Context(), "https://localhost:18080", "", "missing-private-directory", ""); err == nil || err.Error() != "chartdoctor requires the owned pinned-TLS loopback port-forward origin" {
		t.Fatalf("missing SPKI pin was not refused before custody: %v", err)
	}
}
