package config

import (
	"strings"
	"testing"
)

const haTestDSN = "postgres://u:p@localhost/hikyo"

func TestHARequiresPostgres(t *testing.T) {
	_, _, err := Load("server", nil, env(
		"HIKYO_HA", "true", "HIKYO_NODE_ID", "node-a", "HIKYO_ROOT_KEY", "x",
		"HIKYO_DB", "sqlite:/data/hikyo.db"), nil)
	if err == nil || !strings.Contains(err.Error(), "PostgreSQL") {
		t.Fatalf("HA on sqlite must be refused naming PostgreSQL, got: %v", err)
	}
}

func TestHARequiresNodeID(t *testing.T) {
	_, _, err := Load("server", nil, env(
		"HIKYO_HA", "true", "HIKYO_ROOT_KEY", "x", "HIKYO_DB", haTestDSN), nil)
	if err == nil || !strings.Contains(err.Error(), "HIKYO_NODE_ID") {
		t.Fatalf("HA without a node id must be refused, got: %v", err)
	}
}

func TestHARequiresExplicitRootKey(t *testing.T) {
	_, _, err := Load("server", nil, env(
		"HIKYO_HA", "true", "HIKYO_NODE_ID", "node-a", "HIKYO_DB", haTestDSN), nil)
	if err == nil || !strings.Contains(err.Error(), "root-key") {
		t.Fatalf("HA without an explicit root-key source must be refused, got: %v", err)
	}
}

func TestHAEnabledWithCompleteConfig(t *testing.T) {
	cfg, _, err := Load("server", nil, env(
		"HIKYO_HA", "true", "HIKYO_NODE_ID", "node-a", "HIKYO_ROOT_KEY", "x",
		"HIKYO_DB", haTestDSN), nil)
	if err != nil {
		t.Fatalf("complete HA config: %v", err)
	}
	if !cfg.HA || cfg.NodeID != "node-a" {
		t.Fatalf("HA=%v NodeID=%q, want true/node-a", cfg.HA, cfg.NodeID)
	}
}

func TestSingletonNodeIDSurvivesConfigurationLoad(t *testing.T) {
	for _, ha := range []string{"", "false"} {
		t.Run("ha="+ha, func(t *testing.T) {
			cfg, _, err := Load("server", nil, env(
				"HIKYO_HA", ha, "HIKYO_NODE_ID", "hikyo-server", "HIKYO_DB", haTestDSN), nil)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HA || cfg.NodeID != "hikyo-server" {
				t.Fatalf("singleton identity changed: HA=%v NodeID=%q", cfg.HA, cfg.NodeID)
			}
		})
	}
}

func TestHADisabledLeavesConfigOff(t *testing.T) {
	cfg, _, err := Load("server", nil, env(
		"HIKYO_HA", "false", "HIKYO_DB", haTestDSN), nil)
	if err != nil {
		t.Fatalf("HA=false: %v", err)
	}
	if cfg.HA {
		t.Fatal("HIKYO_HA=false must not enable HA")
	}
}
