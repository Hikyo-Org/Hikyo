package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedSeedImportsPasswordFileWithoutTrimming(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("  password\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{ManagedInputs: map[string]string{"HIKYO_MAIL_PASSWORD_FILE": path}}
	values, err := cfg.ManagedSeed()
	if err != nil {
		t.Fatal(err)
	}
	if values["HIKYO_MAIL_PASSWORD"] != "  password\r\n" {
		t.Fatal("password bytes changed")
	}
	if _, exists := values["HIKYO_MAIL_PASSWORD_FILE"]; exists {
		t.Fatal("file reference became managed configuration")
	}
}

func TestManagedSeedRejectsConflictingPasswordSourcesWithoutLeakingThem(t *testing.T) {
	cfg := &Config{ManagedInputs: map[string]string{"HIKYO_MAIL_PASSWORD": "private-value", "HIKYO_MAIL_PASSWORD_FILE": "/private/path"}}
	_, err := cfg.ManagedSeed()
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe conflict error: %v", err)
	}
}

func TestBootstrapLoadingDefersManagedInputsUntilAuthorityIsKnown(t *testing.T) {
	inputs := map[string]string{
		"HIKYO_UPDATE_CHANNEL":     "stale-invalid-channel",
		"HIKYO_MAIL_PASSWORD_FILE": "/missing/old-secret",
	}
	cfg, _, err := LoadBootstrap("server", []string{"--dev"}, func(key string) string { return inputs[key] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ManagedInputs["HIKYO_UPDATE_CHANNEL"] != "stale-invalid-channel" {
		t.Fatal("bootstrap loading discarded the seed before adoption could inspect it")
	}
	if cfg.ManagedInputs["HIKYO_MAIL_PASSWORD_FILE"] != "/missing/old-secret" {
		t.Fatal("bootstrap loading did not retain the deferred file reference")
	}
}

func TestBootstrapLoadingStillRejectsInvalidExternalConfiguration(t *testing.T) {
	_, _, err := LoadBootstrap("server", []string{"--dev"}, func(key string) string {
		if key == "HIKYO_UPDATER_SOCKET" {
			return "/legacy/socket"
		}
		return ""
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "HIKYO_UPDATER_SOCKET") {
		t.Fatalf("external bootstrap error = %v", err)
	}
}

func TestFreshBootstrapOriginRetainsItsExplicitAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, origin string
		refused      bool
		tls          bool
	}{
		{"explicit IP", "http://127.0.0.1:8080", true, false},
		{"explicit TLS IP", "https://127.0.0.1:8443", true, true},
		{"explicit local hostname", "http://localhost:8080", false, false},
		{"explicit TLS hostname", "https://localhost:8443", false, true},
		{"explicit public hostname", "https://hikyo.example.com", false, false},
		{"derived", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pairs := []string{"HIKYO_EXTERNAL_ORIGIN", tc.origin}
			if tc.tls {
				cert, key := managedNodeTestTLS(t)
				dir := t.TempDir()
				certPath, keyPath := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
				if err := os.WriteFile(certPath, []byte(cert), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
					t.Fatal(err)
				}
				pairs = append(pairs, "HIKYO_LISTEN", "127.0.0.1:8443", "HIKYO_TLS_CERT_FILE", certPath, "HIKYO_TLS_KEY_FILE", keyPath)
			}
			cfg, _, err := LoadBootstrap("server", []string{"--dev"}, env(pairs...), nil)
			if err != nil {
				t.Fatalf("bootstrap must defer managed authority: %v", err)
			}
			node, err := cfg.SeedNodeValues()
			if err != nil {
				t.Fatal(err)
			}
			seed, err := cfg.ManagedSeedForNode(node)
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "hostname") {
					t.Fatalf("fresh explicit IP must refuse rather than migrate, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if seed["HIKYO_EXTERNAL_ORIGIN"] != tc.origin {
				t.Fatalf("seed origin=%q, want operator statement %q", seed["HIKYO_EXTERNAL_ORIGIN"], tc.origin)
			}
			owner := make(map[string]string)
			for _, key := range ManagedOwnerKeys() {
				if value, present := seed[key]; present {
					owner[key] = value
				}
			}
			effective, err := ApplyManagedOwnerAndNodeValues(cfg, owner, node)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.origin
			if want == "" {
				want = "http://localhost:8080"
			}
			if effective.ExternalOrigin != want || effective.ExternalOriginExplicit != (tc.origin != "") {
				t.Fatalf("origin=%q explicit=%t, want %q explicit=%t", effective.ExternalOrigin, effective.ExternalOriginExplicit, want, tc.origin != "")
			}
			if cfg.ExternalOriginExplicit || cfg.ManagedInputs["HIKYO_EXTERNAL_ORIGIN"] != tc.origin {
				t.Fatal("seed evaluation mutated deferred bootstrap authority")
			}
		})
	}
}

func TestAdoptedLegacyIPOriginIgnoresStaleBootstrapSeedAuthority(t *testing.T) {
	for _, staleOrigin := range []string{"http://127.0.0.1:8080", "http://localhost:8080", "https://stale.example.com", "invalid-stale-origin"} {
		t.Run(staleOrigin, func(t *testing.T) {
			cfg, _, err := LoadBootstrap("server", []string{"--dev"}, env("HIKYO_EXTERNAL_ORIGIN", staleOrigin), nil)
			if err != nil {
				t.Fatal(err)
			}
			effective, err := ApplyManagedOwnerValues(cfg, map[string]string{"HIKYO_EXTERNAL_ORIGIN": "http://127.0.0.1:8080"})
			if err != nil {
				t.Fatal(err)
			}
			if effective.ExternalOrigin != "http://localhost:8080" || effective.ExternalOriginExplicit {
				t.Fatalf("stale seed changed adopted legacy origin: %q explicit=%t", effective.ExternalOrigin, effective.ExternalOriginExplicit)
			}
		})
	}
}

func TestManagedSnapshotMatchingCurrentHostnameProcessStatementRemainsExplicit(t *testing.T) {
	const origin = "http://localhost:8080"
	cfg, _, err := LoadBootstrap("server", []string{"--dev"}, env("HIKYO_EXTERNAL_ORIGIN", origin), nil)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := ApplyManagedOwnerValues(cfg, map[string]string{"HIKYO_EXTERNAL_ORIGIN": origin})
	if err != nil {
		t.Fatal(err)
	}
	// The matching current hostname is an operator statement on the process,
	// including when applying a snapshot, and satisfies no-public-origin.
	if effective.ExternalOrigin != origin || !effective.ExternalOriginExplicit || effective.ManagedOwnerValues()["HIKYO_EXTERNAL_ORIGIN"] != origin {
		t.Fatalf("matching hostname statement lost explicit authority: origin=%q explicit=%t", effective.ExternalOrigin, effective.ExternalOriginExplicit)
	}
}
