package config

import (
	"slices"
	"strings"
	"testing"
)

const strictTLSNoCertMessage = "HIKYO_TLS_PROFILE=strict requires a TLS certificate pair"

func tlsServerEnv(pairs ...string) func(string) string {
	return env(append([]string{"HIKYO_TLS_CERT_FILE", "/tls.crt", "HIKYO_TLS_KEY_FILE", "/tls.key", "HIKYO_EXTERNAL_ORIGIN", "https://hikyo.example.test", "HIKYO_LISTEN", "0.0.0.0:8443", "HIKYO_DB", "sqlite:hikyo.db"}, pairs...)...)
}

func TestTLSProfileDefaultsToCompatAndRoundTrips(t *testing.T) {
	cfg, _, err := Load("server", []string{"--dev"}, env(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLSProfile != TLSProfileCompat || cfg.ManagedOwnerValues()["HIKYO_TLS_PROFILE"] != TLSProfileCompat {
		t.Fatalf("the default must be compat, got %q", cfg.TLSProfile)
	}
	if !slices.Contains(ManagedOwnerKeys(), "HIKYO_TLS_PROFILE") {
		t.Fatal("the profile must be an owner-managed key")
	}
	for _, d := range VariableInventory() {
		if d.Key == "HIKYO_TLS_PROFILE" && (d.Secret || d.Scope != VariableOwner || d.Activation != VariableAppReload || d.Import != VariableValue || d.Audience != VariableServer) {
			t.Fatalf("inventory ownership is wrong: %+v", d)
		}
	}
	strict, _, err := Load("server", nil, tlsServerEnv("HIKYO_TLS_PROFILE", "strict"), nil)
	if err != nil || strict.TLSProfile != TLSProfileStrict {
		t.Fatalf("strict with a certificate pair: %v", err)
	}
	updated, err := ApplyManagedOwnerValues(strict, strict.ManagedOwnerValues())
	if err != nil || updated.TLSProfile != TLSProfileStrict {
		t.Fatalf("managed apply dropped strict: %v %q", err, updated.TLSProfile)
	}
}

func TestTLSProfileRejectsUnknownValuesWithoutEchoingThemAsAccepted(t *testing.T) {
	for _, bad := range []string{"Strict", " strict", "strict\n", "pq", "hybrid", "off"} {
		if _, _, err := Load("server", []string{"--dev"}, env("HIKYO_TLS_PROFILE", bad), nil); err == nil || !strings.HasPrefix(err.Error(), "HIKYO_TLS_PROFILE") {
			t.Fatalf("%q accepted by Load: %v", bad, err)
		}
		if err := ValidateManagedOwnerValues(map[string]string{"HIKYO_TLS_PROFILE": bad}); err == nil {
			t.Fatalf("%q accepted by managed validation", bad)
		}
	}
}

func TestStrictWithoutCertificateFailsClosedAtStartup(t *testing.T) {
	// Trusted-proxy mode: a plaintext non-loopback listener that is otherwise allowed.
	_, _, err := Load("server", nil, env("HIKYO_DB", "sqlite:hikyo.db", "HIKYO_LISTEN", "0.0.0.0:8080", "HIKYO_TRUSTED_PROXY_CIDRS", "10.0.0.0/8", "HIKYO_EXTERNAL_ORIGIN", "https://hikyo.example.test"), nil)
	if err != nil {
		t.Fatalf("control: trusted-proxy plaintext must stay valid under compat: %v", err)
	}
	_, _, err = Load("server", nil, env("HIKYO_DB", "sqlite:hikyo.db", "HIKYO_LISTEN", "0.0.0.0:8080", "HIKYO_TRUSTED_PROXY_CIDRS", "10.0.0.0/8", "HIKYO_EXTERNAL_ORIGIN", "https://hikyo.example.test", "HIKYO_TLS_PROFILE", "strict"), nil)
	if err == nil || !strings.Contains(err.Error(), strictTLSNoCertMessage) {
		t.Fatalf("strict on a plaintext listener must refuse at startup: %v", err)
	}
	// Loopback development plaintext has no TLS to make strict either.
	if _, _, err := Load("server", []string{"--dev"}, env("HIKYO_TLS_PROFILE", "strict"), nil); err == nil || !strings.Contains(err.Error(), strictTLSNoCertMessage) {
		t.Fatalf("strict on loopback plaintext must refuse: %v", err)
	}
}

func TestStrictWithoutCertificateIsRefusedAtManagedApplyButNotParserValidation(t *testing.T) {
	base, _, err := Load("server", nil, env("HIKYO_DB", "sqlite:hikyo.db", "HIKYO_LISTEN", "0.0.0.0:8080", "HIKYO_TRUSTED_PROXY_CIDRS", "10.0.0.0/8", "HIKYO_EXTERNAL_ORIGIN", "https://hikyo.example.test"), nil)
	if err != nil {
		t.Fatal(err)
	}
	values := base.ManagedOwnerValues()
	values["HIKYO_TLS_PROFILE"] = "strict"
	// Parser-only validation has no listener context: it must accept the value.
	if err := ValidateManagedOwnerValues(values); err != nil {
		t.Fatalf("parser-only validation must not know the node has no certificate: %v", err)
	}
	if _, err := ApplyManagedOwnerValues(base, values); err == nil || !strings.Contains(err.Error(), strictTLSNoCertMessage) {
		t.Fatalf("apply on a plaintext node must be refused: %v", err)
	}
	values["HIKYO_TLS_PROFILE"] = "compat"
	if _, err := ApplyManagedOwnerValues(base, values); err != nil {
		t.Fatalf("compat on the same node must apply: %v", err)
	}
}

func TestBootstrapEnvironmentIsSeedOnlyAndManagedValueWins(t *testing.T) {
	boot, _, err := LoadBootstrap("server", []string{"--dev"}, env("HIKYO_TLS_PROFILE", "strict"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if boot.ManagedInputs["HIKYO_TLS_PROFILE"] != "strict" {
		t.Fatalf("bootstrap must seed the managed owner value, got %v", boot.ManagedInputs)
	}
	// After adoption the managed snapshot is complete and decides: a missing key
	// means the default, never a stale process environment value.
	adopted, err := ApplyManagedOwnerValues(boot, map[string]string{"HIKYO_TLS_PROFILE": "compat"})
	if err != nil || adopted.TLSProfile != TLSProfileCompat {
		t.Fatalf("managed compat must win over bootstrap strict: %v %q", err, adopted.TLSProfile)
	}
}

// The owner value being applied decides, not the stale bootstrap profile in
// base, so one Apply can downgrade strict to compat and drop the certificate.
func TestNodeDocumentWithoutCertificateFollowsTheAppliedOwnerProfile(t *testing.T) {
	base, _, err := LoadBootstrap("server", []string{"--dev"}, env("HIKYO_TLS_PROFILE", "strict"), nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := base.ManagedOwnerValues()
	owner["HIKYO_TLS_PROFILE"] = "compat"
	if _, err := ApplyManagedOwnerAndNodeValues(base, owner, managedNodeTestValues()); err != nil {
		t.Fatalf("a downgrade to compat in one Apply must not be refused by a stale bootstrap profile: %v", err)
	}
	owner["HIKYO_TLS_PROFILE"] = "strict"
	if _, err := ApplyManagedOwnerAndNodeValues(base, owner, managedNodeTestValues()); err == nil || !strings.Contains(err.Error(), strictTLSNoCertMessage) {
		t.Fatalf("strict with a node document that has no certificate must be refused: %v", err)
	}
}
