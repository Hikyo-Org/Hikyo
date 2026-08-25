package app

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Hikyo-Org/hikyo/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func devConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, _, err := config.Load("server", []string{"--dev", "--listen", "127.0.0.1:0"},
		func(k string) string {
			switch k {
			case "HIKYO_DB":
				return "sqlite:" + filepath.Join(t.TempDir(), "hikyo.db")
			case "HIKYO_OPERATIONAL_LISTEN":
				return "localhost:0"
			}
			return ""
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDevBootSeparatesPublicAndOperationalRoutes(t *testing.T) {
	cfg := devConfig(t)
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	for path, want := range map[string]int{"/healthz": 200, "/readyz": 200, "/metrics": 200, "/api/v1/meta": 404} {
		resp, err := http.Get("http://" + srv.OperationalAddr + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("GET %s = %d, want %d", path, resp.StatusCode, want)
		}
	}
	resp, err := http.Get("http://" + srv.Addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("public /healthz = %d, want 404", resp.StatusCode)
	}
}

func TestServeCancellationStopsBothListeners(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	for _, address := range []string{srv.Addr, srv.OperationalAddr} {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatalf("listener %s did not start: %v", address, err)
		}
		conn.Close()
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not stop both listeners within 2 seconds")
	}
	for _, address := range []string{srv.Addr, srv.OperationalAddr} {
		if conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond); err == nil {
			conn.Close()
			t.Errorf("listener %s still accepts after shutdown", address)
		}
	}
}

func TestServeWithReadySignalsOnlyAfterHTTPServingStarts(t *testing.T) {
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	srv, err := Boot(t.Context(), devConfig(t), log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logged.String(), "server ready") {
		srv.Close()
		t.Fatal("boot logged readiness before serving started")
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	ready := make(chan error, 1)
	go func() {
		done <- srv.ServeWithReady(ctx, func() {
			response, err := http.Get("http://" + srv.Addr + "/api/v1/meta")
			if err == nil {
				response.Body.Close()
			}
			ready <- err
		})
	}()

	select {
	case err := <-ready:
		if err != nil {
			cancel()
			<-done
			t.Fatalf("ready callback ran before HTTP serving: %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("ready callback was not called")
	}
	if !strings.Contains(logged.String(), "server ready") {
		cancel()
		<-done
		t.Fatal("serving did not emit the structured ready log")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("ServeWithReady cancellation: %v", err)
	}
}

// The slow-client limits are stdlib machinery; what can regress silently is
// them being unset, so assert the configuration itself.
func TestHTTPServerSlowClientLimitsConfigured(t *testing.T) {
	srv := newHTTPServer(nil)
	if srv.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout must be bounded")
	}
	if srv.ReadTimeout <= 0 {
		t.Error("ReadTimeout must be bounded")
	}
	if srv.IdleTimeout <= 0 {
		t.Error("IdleTimeout must be bounded")
	}
	if srv.MaxHeaderBytes <= 0 {
		t.Error("MaxHeaderBytes must be bounded")
	}
	if srv.WriteTimeout != 0 {
		t.Error("WriteTimeout must stay unset until SSE decides it")
	}
	if srv.ReadHeaderTimeout != 10*time.Second || srv.MaxHeaderBytes != 64<<10 {
		t.Fatalf("HTTP limits = header timeout %s, max headers %d", srv.ReadHeaderTimeout, srv.MaxHeaderBytes)
	}
}

func TestServiceBudgetCanOnlyBeDisabledByExplicitDevConfig(t *testing.T) {
	cfg := &config.Config{}
	if serviceBudget(cfg) == nil {
		t.Fatal("service budget disabled by default")
	}
	cfg.DevServiceBudgetsDisabled = true
	if serviceBudget(cfg) == nil {
		t.Fatal("production service budget disabled by a development-only setting")
	}
	cfg.Dev = true
	if serviceBudget(cfg) != nil {
		t.Fatal("explicit development service-budget override was ignored")
	}
}

func TestPendingMigrationsWithAutoMigrateOffRefusesToServe(t *testing.T) {
	cfg := devConfig(t)
	cfg.AutoMigrate = false
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err == nil {
		srv.Close()
		t.Fatal("boot with pending migrations and auto-migrate disabled must refuse to serve")
	}
}

func TestSchemaAheadOfBinaryRefusesToServe(t *testing.T) {
	cfg := devConfig(t)
	if err := RunMigrate(t.Context(), cfg, testLogger()); err != nil {
		t.Fatal(err)
	}
	// Simulate a newer binary having applied an unknown migration.
	db, err := sql.Open("sqlite", "file:"+cfg.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (99999, 1, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	srv, err := Boot(t.Context(), cfg, testLogger())
	if err == nil {
		srv.Close()
		t.Fatal("a database migrated by a newer binary must refuse to serve")
	}
	if !strings.Contains(err.Error(), "unknown to this binary") {
		t.Fatalf("refusal must name the unknown-schema cause, got: %v", err)
	}
}

func TestExplicitMigrateThenBootWithoutAutoMigrate(t *testing.T) {
	cfg := devConfig(t)
	if err := RunMigrate(t.Context(), cfg, testLogger()); err != nil {
		t.Fatal(err)
	}
	cfg.AutoMigrate = false
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()
}
