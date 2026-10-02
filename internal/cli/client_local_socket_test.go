//go:build linux || darwin

package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/localsocket"
)

func TestClientCarriesBearerOnlyOverSameUserSocket(t *testing.T) {
	directory, err := os.MkdirTemp("", "hks-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "cli.sock")
	listener, err := localsocket.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	seenBearer := make(chan string, 1)
	server := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == api.PathPrefix+"/meta" {
				_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "socket-fixture", ApiRevision: api.Revision})
				return
			}
			seenBearer <- r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		<-done
	})

	entry := TrustEntry{Name: "local", Origin: "http://127.0.0.1:8080", CLISocket: path}
	client, err := NewClient(entry, "fixture-bearer")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Do(t.Context(), http.MethodGet, api.PathPrefix+"/orgs", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := <-seenBearer; got != "Bearer fixture-bearer" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestClientRefusesIdentityFreeLoopbackTCP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("identity-free TCP server received a request")
	}))
	defer server.Close()
	_, err := NewClient(TrustEntry{Name: "local", Origin: server.URL}, "fixture-bearer")
	if err == nil || !strings.Contains(err.Error(), "identity-free loopback http") {
		t.Fatalf("NewClient() error = %v", err)
	}
}
