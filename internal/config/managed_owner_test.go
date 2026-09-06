package config

import (
	"maps"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestManagedOwnerRoundTripPreservesDefaultsAndNodeConfiguration(t *testing.T) {
	cfg, _, err := Load("server", []string{"--dev"}, func(string) string { return "" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RootKeyFile = "/unreadable/root"
	cfg.TLSCertFile, cfg.TLSKeyFile = "/unreadable/cert", "/unreadable/key"
	cfg.NodeID, cfg.HA = "node-a", true
	cfg.Store = Datastore{Engine: EnginePostgres, DSN: "do-not-open", PostgresPoolMax: 7}
	cfg.Upgrade.StateDirectory = "/unreadable/state"
	cfg.AdapterEgressPolicy = map[string][]netip.Prefix{"https://private.example": {netip.MustParsePrefix("10.0.0.0/8")}}
	cfg.ManagedInputs = map[string]string{"HIKYO_ROOT_KEY": "do-not-read"}
	values := cfg.ManagedOwnerValues()
	result, err := ApplyManagedOwnerValues(cfg, values)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, cfg) {
		t.Fatal("round trip changed effective configuration")
	}
	result.AdapterEgressPolicy["https://private.example"][0] = netip.MustParsePrefix("192.168.0.0/16")
	result.ManagedInputs["HIKYO_ROOT_KEY"] = "changed"
	if cfg.AdapterEgressPolicy["https://private.example"][0].String() != "10.0.0.0/8" || cfg.ManagedInputs["HIKYO_ROOT_KEY"] != "do-not-read" {
		t.Fatal("result aliases base mutable values")
	}
}

func TestManagedOwnerValuesOverrideAndRemoveStaleExternalSettings(t *testing.T) {
	base := &Config{Listen: "127.0.0.1:8080", DirectoryProxy: "https://stale.invalid", ExternalOrigin: "https://stale.invalid", MCPEnabled: true, MCPAllowedOrigins: []string{"https://stale.invalid"}, AuditAccessRetainDays: 200, ReauthWindow: time.Hour}
	values := map[string]string{"HIKYO_EXTERNAL_ORIGIN": "https://managed.example", "HIKYO_ARGON2_TIME": "4", "HIKYO_AUDIT_ACCESS_RETAIN_DAYS": "100", "HIKYO_AUDIT_SECURITY_RETAIN_DAYS": "200", "HIKYO_REAUTH_WINDOW_SECONDS": "12"}
	result, err := ApplyManagedOwnerValues(base, values)
	if err != nil {
		t.Fatal(err)
	}
	if result.DirectoryProxy != "" || result.MCPEnabled || len(result.MCPAllowedOrigins) != 0 || result.ExternalOrigin != "https://managed.example" || result.Argon2Time != 4 || result.AuditAccessRetainDays != 100 || result.ReauthWindow != 12*time.Second {
		t.Fatal("owner settings did not replace stale external values")
	}
	if base.DirectoryProxy != "https://stale.invalid" || values["HIKYO_ARGON2_TIME"] != "4" {
		t.Fatal("validation mutated its inputs")
	}
}

func TestManagedOwnerValidationSeparatesNodeConstraints(t *testing.T) {
	recipient := "public-recipient-format-owned-by-runtime-validation"
	tests := []struct {
		name              string
		values            map[string]string
		refused, accepted Config
	}{
		{"backup destination", map[string]string{"HIKYO_BACKUP_RECIPIENTS": recipient}, Config{}, Config{BackupDir: "/must-not-be-opened"}},
		{"development origin", map[string]string{"HIKYO_MCP_ENABLED": "true", "HIKYO_EXTERNAL_ORIGIN": "http://localhost:8080"}, Config{}, Config{Dev: true}},
		{"listener proxy trust", nil, Config{Listen: "0.0.0.0:8080"}, Config{Listen: "127.0.0.1:8080"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateManagedOwnerValues(tt.values); err != nil {
				t.Fatalf("generic validation used an assumed node: %v", err)
			}
			if _, err := ApplyManagedOwnerValues(&tt.refused, tt.values); err == nil {
				t.Fatal("actual node constraint not enforced")
			}
			if _, err := ApplyManagedOwnerValues(&tt.accepted, tt.values); err != nil {
				t.Fatalf("suitable node refused: %v", err)
			}
		})
	}
}

func TestManagedOwnerValidationRejectsInvalidValuesWithoutDisclosure(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"unknown":                  {"HIKYO_ROOT_KEY": "do-not-disclose"},
		"blank":                    {"HIKYO_DIRECTORY_PROXY": ""},
		"credentials":              {"HIKYO_DIRECTORY_PROXY": "http://user:do-not-disclose@proxy.invalid"},
		"KDF parallelism overflow": {"HIKYO_ARGON2_PARALLELISM": "256"},
		"reauth ceiling":           {"HIKYO_REAUTH_WINDOW_SECONDS": "86401"},
		"audit relationship":       {"HIKYO_AUDIT_ACCESS_RETAIN_DAYS": "300", "HIKYO_AUDIT_SECURITY_RETAIN_DAYS": "299"},
		"schedule without policy":  {"HIKYO_BACKUP_INTERVAL": "1h"},
		"MCP dependency":           {"HIKYO_MCP_ALLOWED_ORIGINS": "https://example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateManagedOwnerValues(values)
			if err == nil || strings.Contains(err.Error(), "do-not-disclose") {
				t.Fatalf("invalid/redaction result: %v", err)
			}
		})
	}
}

func TestBootstrapOwnerInputsAreDeferredAndSeedImportsOriginalValues(t *testing.T) {
	input := map[string]string{"HIKYO_EXTERNAL_ORIGIN": "https://seed.example", "HIKYO_ARGON2_TIME": "4", "HIKYO_TRUSTED_PROXY_CIDRS": "192.168.0.0/16", "HIKYO_LISTEN": "0.0.0.0:8080"}
	cfg, _, err := LoadBootstrap("server", []string{"--dev"}, func(key string) string { return input[key] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Argon2Time != 3 || cfg.ManagedInputs["HIKYO_ARGON2_TIME"] != "4" {
		t.Fatal("bootstrap did not defer original owner inputs")
	}
	seed, err := cfg.ManagedSeed()
	if err != nil {
		t.Fatal(err)
	}
	if seed["HIKYO_ARGON2_TIME"] != "4" || seed["HIKYO_EXTERNAL_ORIGIN"] != "https://seed.example" {
		t.Fatal("seed replaced original owner settings with bootstrap defaults")
	}
	node, err := cfg.SeedNodeValues()
	if err != nil || node["HIKYO_TRUSTED_PROXY_CIDRS"] != "192.168.0.0/16" {
		t.Fatalf("node seed lost imported ingress policy: %v", err)
	}
	if _, present := seed["HIKYO_TRUSTED_PROXY_CIDRS"]; present {
		t.Fatal("per-node ingress policy contaminated the shared owner seed")
	}
	stale := maps.Clone(input)
	stale["HIKYO_DIRECTORY_PROXY"] = "http://user:do-not-disclose@proxy.invalid"
	cfg, _, err = LoadBootstrap("server", []string{"--dev"}, func(key string) string { return stale[key] }, nil)
	if err != nil {
		t.Fatal("stale managed environment blocked bootstrap")
	}
	if _, err := cfg.ManagedSeed(); err == nil || strings.Contains(err.Error(), "do-not-disclose") {
		t.Fatalf("new adoption did not validate original inputs safely: %v", err)
	}
}
