package config

import (
	"errors"
	"maps"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestManagedOwnerRoundTripPreservesDefaultsAndNodeConfiguration(t *testing.T) {
	cfg, _, err := Load("server", []string{"--dev"}, env("HIKYO_TLS_CERT_FILE", "/unreadable/cert", "HIKYO_TLS_KEY_FILE", "/unreadable/key"), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RootKeyFile = "/unreadable/root"
	cfg.NodeID, cfg.HA = "node-a", true
	cfg.Store = Datastore{Engine: EnginePostgres, DSN: "do-not-open", PostgresPoolMax: 7, PostgresStorage: PostgresStorage{KubeletURL: "https://tokyo:10250", Namespace: "database", PVC: "data", Node: "tokyo"}}
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

func TestManagedOwnerRefusalNamesKeyAndHidesSecretValues(t *testing.T) {
	for _, tc := range []struct {
		name, err, want string
	}{
		{"non-secret key echoes the parser message", `HIKYO_MCP_ALLOWED_ORIGINS: "192.168.0.0/24" is not an exact HTTP(S) origin`, `HIKYO_MCP_ALLOWED_ORIGINS: "192.168.0.0/24" is not an exact HTTP(S) origin`},
		{"secret key names itself only", `HIKYO_DIRECTORY_PROXY: "http://u:hunter2@proxy" must be https`, "HIKYO_DIRECTORY_PROXY is invalid for this node"},
		{"parser-only input keeps the fixed wording", "HIKYO_DB: invalid postgres DSN", "managed owner configuration is invalid for this node"},
		{"cross-variable rule keeps the fixed wording", "MCP requires an https HIKYO_EXTERNAL_ORIGIN", "managed owner configuration is invalid for this node"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := managedOwnerRefusal(errors.New(tc.err), ManagedOwnerKeys()).Error()
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// A snapshot saved before #606 holds the listen-derived origin as a value.
// Re-applying it must not turn the derivation into an explicit statement;
// a genuinely different managed origin still is one.
func TestManagedLegacyDerivedOriginIsNotExplicit(t *testing.T) {
	base, _, err := Load("server", []string{"--dev"}, func(string) string { return "" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if base.ExternalOriginExplicit {
		t.Fatal("a listen-derived origin loaded as explicit")
	}
	legacy := base.ManagedOwnerValues()
	legacy["HIKYO_EXTERNAL_ORIGIN"] = "http://127.0.0.1:8080"
	result, err := ApplyManagedOwnerValues(base, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExternalOriginExplicit || result.ExternalOrigin != base.ExternalOrigin {
		t.Fatalf("legacy derived origin = %q explicit=%t, want the derivation, not explicit", result.ExternalOrigin, result.ExternalOriginExplicit)
	}
	stated := base.ManagedOwnerValues()
	stated["HIKYO_EXTERNAL_ORIGIN"] = "https://hikyo.example.com"
	result, err = ApplyManagedOwnerValues(base, stated)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ExternalOriginExplicit || result.ExternalOrigin != "https://hikyo.example.com" {
		t.Fatalf("stated managed origin = %q explicit=%t, want explicit", result.ExternalOrigin, result.ExternalOriginExplicit)
	}
}

func TestManagedLegacyOriginMatchesOriginalNodeDerivation(t *testing.T) {
	for _, tc := range []struct {
		name, listen, legacy, want string
		tls                        bool
	}{
		{"loopback", "127.0.0.1:8080", "http://127.0.0.1:8080", "http://localhost:8080", false},
		{"wildcard", "0.0.0.0:9443", "http://0.0.0.0:9443", "http://localhost:9443", false},
		{"IPv6", "[::1]:8080", "http://[::1]:8080", "http://localhost:8080", false},
		{"native TLS", "127.0.0.1:9443", "https://127.0.0.1:9443", "https://localhost:9443", true},
		{"hostname", "hikyo.localhost:8080", "http://hikyo.localhost:8080", "http://hikyo.localhost:8080", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pairs := []string{"HIKYO_TRUSTED_PROXY_CIDRS", "192.0.2.0/24"}
			if tc.tls {
				pairs = append(pairs, "HIKYO_TLS_CERT_FILE", "unreadable.crt", "HIKYO_TLS_KEY_FILE", "unreadable.key")
			}
			base, _, err := Load("server", []string{"--dev", "--listen", tc.listen}, env(pairs...), nil)
			if err != nil {
				t.Fatal(err)
			}
			// Managed nodes retain certificate content instead of startup paths.
			if tc.tls {
				base.TLSCertFile, base.TLSKeyFile = "", ""
				delete(base.ManagedNodeInputs, "HIKYO_TLS_CERT_FILE")
				delete(base.ManagedNodeInputs, "HIKYO_TLS_KEY_FILE")
				base.TLSCertPEM, base.TLSKeyPEM = managedNodeTestTLS(t)
			}
			values := base.ManagedOwnerValues()
			values["HIKYO_EXTERNAL_ORIGIN"] = tc.legacy
			before := maps.Clone(values)
			result, err := ApplyManagedOwnerValues(base, values)
			if err != nil {
				t.Fatal(err)
			}
			if result.ExternalOrigin != tc.want || result.ExternalOriginExplicit {
				t.Fatalf("origin=%q explicit=%t, want derived %q", result.ExternalOrigin, result.ExternalOriginExplicit, tc.want)
			}
			if _, present := result.ManagedOwnerValues()["HIKYO_EXTERNAL_ORIGIN"]; present {
				t.Fatal("legacy derivation was exported as an operator statement")
			}
			if !maps.Equal(values, before) {
				t.Fatal("legacy normalization mutated the stored snapshot")
			}
			// Once imported, an origin remains a derivation when a subsequent
			// managed node activation changes both the listener and TLS scheme.
			node, err := result.SeedNodeValues()
			if err != nil {
				t.Fatal(err)
			}
			node["HIKYO_LISTEN"] = "[::]:9444"
			delete(node, "HIKYO_TLS_CERT_PEM")
			delete(node, "HIKYO_TLS_KEY_PEM")
			result, err = ApplyManagedNodeValues(result, node)
			if err != nil || result.ExternalOrigin != "http://localhost:9444" || result.ExternalOriginExplicit {
				t.Fatalf("updated node result=%+v error=%v", result, err)
			}
		})
	}
}

func TestManagedLegacyMigrationPreservesExplicitOriginBoundary(t *testing.T) {
	for _, origin := range []string{"http://127.0.0.1:8081", "https://127.0.0.1:8080", "http://192.0.2.1:8080"} {
		t.Run(origin, func(t *testing.T) {
			base, _, err := Load("server", []string{"--dev"}, env(), nil)
			if err != nil {
				t.Fatal(err)
			}
			values := map[string]string{"HIKYO_EXTERNAL_ORIGIN": origin}
			if err := ValidateManagedOwnerValues(values); err != nil {
				t.Fatalf("node-independent validation must defer legacy attribution: %v", err)
			}
			if _, err := ApplyManagedOwnerValues(base, values); err == nil || !strings.Contains(err.Error(), "hostname") {
				t.Fatalf("a nonmatching IP origin must remain explicit and refuse, got %v", err)
			}
		})
	}
	for _, origin := range []string{"http://localhost:8080", "https://public.example.com:9443"} {
		t.Run(origin, func(t *testing.T) {
			base, _, err := Load("server", []string{"--dev"}, env("HIKYO_EXTERNAL_ORIGIN", origin), nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := ApplyManagedOwnerValues(base, base.ManagedOwnerValues())
			if err != nil || result.ExternalOrigin != origin || !result.ExternalOriginExplicit {
				t.Fatalf("explicit origin changed: result=%+v error=%v", result, err)
			}
			if _, err := ApplyManagedOwnerValues(base, map[string]string{"HIKYO_EXTERNAL_ORIGIN": "http://127.0.0.1:8080"}); err == nil {
				t.Fatal("explicit bootstrap origin must not classify a managed IP origin as derived")
			}
		})
	}
	for _, origin := range []string{"http://127.0.0.1:8080/", "http://127.0.0.1:8080?", "http://user:pass@127.0.0.1:8080"} {
		if err := ValidateManagedOwnerValues(map[string]string{"HIKYO_EXTERNAL_ORIGIN": origin}); err == nil {
			t.Fatalf("node-independent validation accepted malformed exact origin %q", origin)
		}
	}
}
