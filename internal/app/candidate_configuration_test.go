package app

import (
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

func TestCandidateConfigurationFailureKeepsRestoreRequired(t *testing.T) {
	for _, failure := range []string{"authentication", "tls", "proxy"} {
		t.Run(failure, func(t *testing.T) {
			cfg := devConfig(t)
			if err := RunMigrate(t.Context(), cfg, testLogger()); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "authentication":
				cfg.Argon2MemoryKiB = 1
			case "tls":
				cfg.TLSCertFile = filepath.Join(t.TempDir(), "missing.crt")
				cfg.TLSKeyFile = filepath.Join(t.TempDir(), "missing.key")
			case "proxy":
				cfg.TrustedProxyCIDRs = []string{"invalid CIDR"}
			}
			record := &bootResourceRecord{}
			if server, err := boot(t.Context(), cfg, testLogger(), recordingBootResources(record)); err == nil {
				server.Close()
				t.Fatal("invalid candidate became ready")
			}
			if record.database != nil || len(record.listeners) != 0 {
				t.Fatal("invalid candidate acquired runtime database or listener")
			}
			state, err := upgrade.InspectControl(t.Context(), upgrade.Config{Engine: "sqlite", Path: cfg.Store.Path})
			if err != nil {
				t.Fatal(err)
			}
			if !state.Maintenance || state.Pending.Phase != upgrade.RestoreRequired {
				t.Fatalf("invalid post-migration candidate state: phase=%s maintenance=%v", state.Pending.Phase, state.Maintenance)
			}
		})
	}
}
