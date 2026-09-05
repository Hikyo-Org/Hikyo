package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Hikyo-Org/hikyo/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// syncBuffer guards a bytes.Buffer so a background server goroutine can log
// through it while the test goroutine inspects what has been logged. A plain
// bytes.Buffer is not safe for concurrent Write and String.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
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
	go func() { done <- srv.ServeWithReady(ctx, nil) }()
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

func TestProxyHTTPSOriginEmitsHSTSOnLoopbackBackend(t *testing.T) {
	cfg := devConfig(t)
	cfg.ExternalOrigin = "https://hikyo.example.com"
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.ServeWithReady(ctx, nil) }()
	t.Cleanup(func() { cancel(); <-done })

	resp, err := http.Get("http://" + srv.Addr + "/api/v1/meta")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("proxy HTTPS origin HSTS = %q, want max-age=31536000", got)
	}
}

func TestServeCancellationStopsBothListeners(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.ServeWithReady(ctx, nil) }()
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

func startDrainTestServer(t *testing.T, handler http.Handler) (*managedHTTPServer, string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := newHTTPServer(handler)
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	return httpServer, "http://" + listener.Addr().String(), done
}

func TestMCPGracefulShutdownLetsActiveCallComplete(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	httpServer, baseURL, serveDone := startDrainTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	responseDone := make(chan error, 1)
	go func() {
		response, err := http.Post(baseURL+"/mcp", "application/json", strings.NewReader(`{}`))
		if err == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				err = errors.New("active MCP call returned an unexpected status")
			}
		}
		responseDone <- err
	}()
	<-started
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- shutdownHTTPServers(time.Second, httpServer) }()
	close(release)
	if err := <-responseDone; err != nil {
		t.Fatal(err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatalf("graceful MCP drain: %v", err)
	}
	if err := <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve() = %v, want http.ErrServerClosed", err)
	}
}

func TestMCPGracefulShutdownCancelsCallWhenDrainExpires(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	finishCleanup := make(chan struct{})
	cleaned := make(chan struct{})
	httpServer, baseURL, serveDone := startDrainTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
		<-finishCleanup
		close(cleaned)
	}))

	responseDone := make(chan error, 1)
	go func() {
		response, err := http.Post(baseURL+"/mcp", "application/json", strings.NewReader(`{}`))
		if err == nil {
			response.Body.Close()
		}
		responseDone <- err
	}()
	<-started
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- shutdownHTTPServers(25*time.Millisecond, httpServer) }()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("active MCP request context was not cancelled after drain expiry")
	}
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before active request cleanup: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(finishCleanup)
	<-cleaned
	err := <-shutdownDone
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain expiry = %v, want context deadline exceeded", err)
	}
	if err := <-responseDone; err == nil {
		t.Fatal("client unexpectedly received a successful response after forced MCP cancellation")
	}
	if err := <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve() = %v, want http.ErrServerClosed", err)
	}
}

func TestServeWithReadySignalsOnlyAfterHTTPServingStarts(t *testing.T) {
	var logged syncBuffer
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
	ready := make(chan struct{}, 1)
	go func() {
		done <- srv.ServeWithReady(ctx, func() {
			// Keep the callback as the readiness event. Probing an API route
			// here couples callback delivery to unrelated handler latency.
			ready <- struct{}{}
		})
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("ready callback was not called")
	}
	response, err := http.Get("http://" + srv.Addr + "/api/v1/meta")
	if err != nil {
		cancel()
		<-done
		t.Fatalf("HTTP server was unavailable after ready callback: %v", err)
	}
	response.Body.Close()
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
	if srv.WriteTimeout != 60*time.Second {
		t.Errorf("WriteTimeout = %s, want 60s", srv.WriteTimeout)
	}
	if srv.ReadHeaderTimeout != 10*time.Second || srv.ReadTimeout != 30*time.Second || srv.IdleTimeout != 120*time.Second || srv.MaxHeaderBytes != 64<<10 {
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

func TestDevAdmissionOverrideCoversDiscoveryTraffic(t *testing.T) {
	const override = 500
	cfg := devConfig(t)
	cfg.DevAdmissionPerIPPerMinute = override
	_, limiter, err := AuthComponents(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for request := range override {
		if !limiter.AllowDiscovery("127.0.0.1") {
			t.Fatalf("discovery request %d was refused despite the development override", request+1)
		}
	}
	if limiter.AllowDiscovery("127.0.0.1") {
		t.Fatal("discovery request beyond the development override was admitted")
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
	if !strings.Contains(err.Error(), "schema differs from signed target") {
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
