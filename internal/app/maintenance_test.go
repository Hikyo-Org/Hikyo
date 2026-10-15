package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

func TestMaintenanceBootHonorsBootstrapOperationalBinding(t *testing.T) {
	for _, refusal := range []error{upgradegate.ErrNextBinary, upgradegate.ErrRestoreRequired} {
		t.Run(refusal.Error(), func(t *testing.T) {
			for _, tc := range []struct {
				name, environment, fallback, want string
				flags                             []string
				invalid                           bool
			}{
				{name: "environment", environment: "127.0.0.1:0", want: "127.0.0.1:0"},
				{name: "flag", flags: []string{"--operational-listen=127.0.0.1:0"}, want: "127.0.0.1:0"},
				{name: "flag overrides environment", environment: "invalid", flags: []string{"--operational-listen=127.0.0.1:0"}, want: "127.0.0.1:0"},
				{name: "resolved fallback", fallback: "localhost:0", want: "localhost:0"},
				{name: "invalid environment", environment: "invalid", invalid: true},
				{name: "invalid flag", flags: []string{"--operational-listen=127.0.0.1:65536"}, invalid: true},
				{name: "invalid fallback", fallback: "127.0.0.1:http", invalid: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					directory := t.TempDir()
					values := map[string]string{"HIKYO_DB": "sqlite:" + filepath.Join(directory, "unused.db"), "HIKYO_OPERATIONAL_LISTEN": tc.environment, "HIKYO_TLS_CERT_FILE": filepath.Join(directory, "absent.crt"), "HIKYO_TLS_KEY_FILE": filepath.Join(directory, "absent.key")}
					cfg, _, err := config.LoadBootstrap("server", append([]string{"--dev"}, tc.flags...), func(key string) string { return values[key] }, nil)
					if err != nil {
						t.Fatal(err)
					}
					if tc.fallback != "" {
						cfg.OperationalListen = tc.fallback
					}
					resources := defaultBootResources()
					resources.databaseGate = func(context.Context, *config.Config, []byte, upgradegate.Mode) (upgradegate.Result, error) {
						return upgradegate.Result{}, refusal
					}
					listened := false
					resources.listen = func(network, address string) (net.Listener, error) {
						listened = true
						if tc.invalid || address != tc.want {
							t.Fatalf("maintenance attempted unexpected bind %q, want %q", address, tc.want)
						}
						return net.Listen(network, address)
					}
					srv, err := boot(t.Context(), cfg, testLogger(), resources)
					if tc.invalid {
						if err == nil || listened {
							t.Fatal("invalid maintenance binding did not refuse before listen")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = srv.Close() })
					if !listened || !srv.Maintenance || srv.Addr != "" || srv.publicLn != nil || srv.db != nil || srv.keyring != nil || srv.owner != nil || srv.selfConfig != nil {
						t.Fatal("maintenance boot acquired a tenant-serving resource")
					}
				})
			}
		})
	}
}

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
