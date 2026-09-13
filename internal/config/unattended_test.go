package config

import (
	"strings"
	"testing"
)

func TestUnattendedUpgradeRequiresExplicitOptInAndPersistentCustody(t *testing.T) {
	base := []string{"HIKYO_DB", "sqlite:/data/hikyo.db", "HIKYO_ROOT_KEY_FILE", "/keys/root", "HIKYO_UPGRADE_STATE_DIR", "/data/upgrade"}
	cfg, _, err := Load("server", nil, env(base...), nil)
	if err != nil || cfg.Upgrade.Unattended {
		t.Fatalf("manual default changed: %v", err)
	}
	opted := append(append([]string{}, base...), "HIKYO_UPGRADE_UNATTENDED", "true")
	cfg, warnings, err := Load("server", nil, env(opted...), environFrom(opted...))
	if err != nil || !cfg.Upgrade.Unattended || len(warnings) != 0 {
		t.Fatalf("explicit opt-in failed: %v %v", err, warnings)
	}
	cfg, _, err = Load("server", []string{"--upgrade-unattended=false"}, env(opted...), nil)
	if err != nil || cfg.Upgrade.Unattended {
		t.Fatalf("flag did not override env: %v", err)
	}
	for _, tc := range []struct {
		name  string
		args  []string
		extra []string
	}{
		{"development", []string{"--dev"}, nil},
		{"no migrations", []string{"--auto-migrate=false"}, nil},
		{"relative state", nil, []string{"HIKYO_UPGRADE_STATE_DIR", "relative"}},
		{"missing root", nil, []string{"HIKYO_ROOT_KEY_FILE", ""}},
		{"wrong scratch engine", nil, []string{"HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN", "postgres://localhost/scratch"}},
		{"invalid boolean", nil, []string{"HIKYO_UPGRADE_UNATTENDED", "perhaps"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pairs := append(append([]string{}, opted...), tc.extra...)
			if _, _, err := Load("server", tc.args, env(pairs...), nil); err == nil {
				t.Fatal("invalid opt-in accepted")
			}
		})
	}
}

func TestUnattendedPostgresRequiresSeparateScratchAndRejectsHA(t *testing.T) {
	base := []string{"HIKYO_DB", "postgres://user:live-secret@localhost/live", "HIKYO_ROOT_KEY_FILE", "/keys/root", "HIKYO_UPGRADE_STATE_DIR", "/data/upgrade", "HIKYO_UPGRADE_UNATTENDED", "true"}
	for _, scratch := range []string{"", "sqlite:/tmp/scratch", "postgres://user:live-secret@localhost/live"} {
		pairs := append(append([]string{}, base...), "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN", scratch)
		_, _, err := Load("server", nil, env(pairs...), nil)
		if err == nil {
			t.Fatal("missing or unsafe scratch accepted")
		}
		if strings.Contains(err.Error(), "live-secret") {
			t.Fatal("configuration error disclosed secret")
		}
	}
	base = append(base, "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN", "postgres://user:scratch-secret@localhost/scratch")
	cfg, warnings, err := Load("server", nil, env(base...), environFrom(base...))
	if err != nil || !cfg.Upgrade.Unattended || cfg.Upgrade.ScratchPostgresDSN == "" || len(warnings) != 0 {
		t.Fatalf("separate scratch refused: %v %v", err, warnings)
	}
	base = append(base, "HIKYO_HA", "true", "HIKYO_NODE_ID", "one")
	if _, _, err := Load("server", nil, env(base...), nil); err == nil || !strings.Contains(err.Error(), "one replica") {
		t.Fatalf("HA not rejected: %v", err)
	}
}
