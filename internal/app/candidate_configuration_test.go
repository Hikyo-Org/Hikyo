package app

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
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

func TestCandidateHealthValidatesSavedNodeBeforeRetiredBootstrap(t *testing.T) {
	cfg := devConfig(t)
	values, err := cfg.ManagedSeed()
	if err != nil {
		t.Fatal(err)
	}
	nodeValues, err := cfg.SeedNodeValues()
	if err != nil {
		t.Fatal(err)
	}
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = "local"
	}
	values[config.ManagedNodeOverridesKey], err = runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{nodeID: nodeValues})
	if err != nil {
		t.Fatal(err)
	}
	projection := &upgrade.CandidateConfiguration{SchemaVersion: runtimeconfig.SchemaVersion}
	for _, key := range runtimeconfig.Catalogue() {
		compiled, err := schema.CompileClassified(key.Classification, key.Declaration)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := compiled.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		projection.Catalogue = append(projection.Catalogue, upgrade.CandidateConfigurationKey{Name: key.Name, Classification: string(key.Classification), Declaration: string(canonical), RequiredMode: string(schema.PresenceNone), ForbiddenMode: string(schema.PresenceNone)})
	}
	// The persisted complete node projection owns these settings now.
	cfg.TLSCertFile = filepath.Join(t.TempDir(), "retired.crt")
	cfg.TLSKeyFile = filepath.Join(t.TempDir(), "retired.key")
	cfg.Argon2MemoryKiB = 1
	cfg.TrustedProxyCIDRs = []string{"retired invalid CIDR"}
	if err := checkCandidateConfiguration(t.Context(), cfg, projection, values); err != nil {
		t.Fatalf("retired bootstrap overrode saved configuration: %v", err)
	}
	var nodes struct {
		Version int                          `json:"version"`
		Nodes   map[string]map[string]string `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(values[config.ManagedNodeOverridesKey]), &nodes); err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes.Nodes {
		node["HIKYO_TLS_CERT_PEM"] = "invalid managed certificate"
		node["HIKYO_TLS_KEY_PEM"] = "invalid managed key"
	}
	raw, err := json.Marshal(nodes)
	if err != nil {
		t.Fatal(err)
	}
	values[config.ManagedNodeOverridesKey] = string(raw)
	if err := checkCandidateConfiguration(t.Context(), cfg, projection, values); err == nil {
		t.Fatal("invalid saved TLS marked candidate healthy")
	}
}
