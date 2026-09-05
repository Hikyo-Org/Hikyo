package app

import (
	"context"
	"net/http"
	"testing"
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
	if !srv.Maintenance || srv.Addr != "" || srv.publicLn != nil || srv.db != nil || srv.keyring != nil || srv.scheduler != nil || srv.adapterWorker != nil || srv.dynamicWorker != nil || srv.updateReconciler != nil {
		srv.Close()
		t.Fatal("maintenance boot acquired a tenant-serving resource")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.ServeWithReady(ctx, nil) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	for path, want := range map[string]int{"/healthz": 200, "/readyz": 503, "/metrics": 503, "/api/v1/meta": 404, "/mcp": 404, "/": 404} {
		resp, err := http.Get("http://" + srv.OperationalAddr + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: got %d, want %d", path, resp.StatusCode, want)
		}
	}
}
