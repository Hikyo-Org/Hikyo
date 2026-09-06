package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
)

func upgradeEnrollmentFixture(t *testing.T) (*config.Config, string) {
	t.Helper()
	initial := configrollout.UpgradeCustodySource{BundleDirectory: "/run/hikyo-upgrade/bundle", StateDirectory: "/var/lib/hikyo-upgrade/operator-custody", OperatorPublicKeyFile: "/run/hikyo-upgrade/operator.pub"}
	next := initial
	next.BundleDirectory = "/run/hikyo-upgrade/next/bundle"
	enrollment := configrollout.Enrollment{ID: "enrollment", OwnerInstanceID: "owner", Incarnation: "incarnation", Target: configrollout.Target{Namespace: "hikyo", Deployment: "hikyo", DeploymentUID: "deployment", Container: "server", StableNodeID: "local", ConfigSecret: "config", RollbackSecret: "rollback", RequestSecret: "request", ReceiptSecret: "receipt", InitialUpgradeSource: "initial", UpgradeSources: map[string]configrollout.UpgradeCustodySource{"initial": initial, "next": next}}, CommandSecret: "command", CommandSecretUID: "command", ResponseSecret: "response", ResponseSecretUID: "response", JournalSecret: "journal", JournalSecretUID: "journal", LeaseName: "lease", LeaseUID: "lease", ExecutorPod: "executor-0"}
	raw, err := json.Marshal(enrollment)
	if err != nil {
		t.Fatal(err)
	}
	cfg := selectedUpgradeConfiguration(&config.Config{}, initial)
	cfg.ConfigRolloutEnrollment = filepath.Join(t.TempDir(), "enrollment.json")
	writeDeploymentFixture(t, cfg.ConfigRolloutEnrollment, string(raw), 0444)
	selection := t.TempDir()
	writeDeploymentFixture(t, filepath.Join(selection, "upgrade-alias"), "initial", 0444)
	writeDeploymentFixture(t, filepath.Join(selection, "upgrade-proof"), "", 0444)
	return cfg, selection
}

func TestUpgradeSelectionResolvesOnlyExactEnrolledStartupTuple(t *testing.T) {
	cfg, selection := upgradeEnrollmentFixture(t)
	next, err := resolveSelectedUpgrade(t.Context(), cfg, selection)
	if err != nil || next.UpgradeSource != "initial" || next.Upgrade != cfg.Upgrade || cfg.UpgradeSource != "" {
		t.Fatalf("initial import: %+v %v", next, err)
	}
	changed := *cfg
	changed.Upgrade.CiphertextPath = "/operator-value-must-not-disappear"
	if _, err := resolveSelectedUpgrade(t.Context(), &changed, selection); err == nil || !strings.Contains(err.Error(), "matching upgradeSources profile") {
		t.Fatalf("silent startup value drop: %v", err)
	}
	writeDeploymentFixture(t, filepath.Join(selection, "upgrade-alias"), "next", 0444)
	changed = *cfg
	changed.Upgrade.BundleDirectory = "/run/hikyo-upgrade/next/bundle"
	if _, err := resolveSelectedUpgrade(t.Context(), &changed, selection); err == nil || !strings.Contains(err.Error(), "requires its authorized material proof") {
		t.Fatalf("applied alias lost proof: %v", err)
	}
	writeDeploymentFixture(t, filepath.Join(selection, "upgrade-alias"), "unknown", 0444)
	if _, err := resolveSelectedUpgrade(t.Context(), cfg, selection); err == nil {
		t.Fatal("unknown alias accepted")
	}
	writeDeploymentFixture(t, filepath.Join(selection, "upgrade-alias"), "initial", 0444)
	writeDeploymentFixture(t, filepath.Join(selection, "upgrade-proof"), "invalid", 0444)
	if _, err := resolveSelectedUpgrade(t.Context(), cfg, selection); err == nil {
		t.Fatal("invalid material proof accepted")
	}
	changed = *cfg
	changed.Dev = true
	if _, err := resolveSelectedUpgrade(t.Context(), &changed, selection); err == nil {
		t.Fatal("production upgrade source adopted development material")
	}
	unconfigured := &config.Config{Upgrade: config.UpgradeConfiguration{BundleDirectory: "/operator/bootstrap", StateDirectory: "/operator/custody"}}
	initial, err := resolveSelectedUpgrade(t.Context(), unconfigured, "/must-not-read")
	if err != nil || initial != unconfigured {
		t.Fatal("non-enrolled bootstrap now requires managed setup")
	}
}
