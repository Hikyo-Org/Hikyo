package app

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestRestoreRequiredRestartExposesOnlyOperationalHealth(t *testing.T) {
	cfg := devConfig(t)
	if err := RunMigrate(t.Context(), cfg, testLogger()); err != nil {
		t.Fatal(err)
	}
	validMemory := cfg.Argon2MemoryKiB
	cfg.Argon2MemoryKiB = 1
	if srv, err := Boot(t.Context(), cfg, testLogger()); err == nil {
		srv.Close()
		t.Fatal("invalid candidate boot unexpectedly succeeded")
	}
	cfg.Argon2MemoryKiB = validMemory
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if !srv.Maintenance || srv.Addr != "" || srv.publicLn != nil || srv.db != nil || srv.keyring != nil || srv.owner != nil || srv.selfConfig != nil {
		srv.Close()
		t.Fatal("maintenance boot acquired a tenant-serving resource")
	}
	ctx, cancel := context.WithCancel(t.Context())
	transport := &http.Transport{}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.ServeWithReady(ctx, nil) }()
	t.Cleanup(func() {
		// An unused transport dial is StateNew to net/http and can consume
		// the entire server drain window. Release this fixture's connections
		// before testing server shutdown, including any spare keep-alive dial.
		transport.CloseIdleConnections()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("maintenance server did not finish bounded shutdown")
		}
	})
	for path, want := range map[string]int{"/healthz": 200, "/readyz": 503, "/metrics": 503, "/api/v1/meta": 404, "/mcp": 404, "/": 404} {
		resp, err := client.Get("http://" + srv.OperationalAddr + path)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if resp.StatusCode != want {
			t.Errorf("%s: got %d, want %d", path, resp.StatusCode, want)
		}
	}
}
