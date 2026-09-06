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
