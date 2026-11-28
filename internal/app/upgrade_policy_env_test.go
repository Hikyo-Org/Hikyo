package app

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// A launcher that filters HIKYO_UPGRADE_* must still hand the revocation policy to its child.
func TestUnattendedChildInheritsTheUpgradePolicy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Upgrade = config.UpgradeConfiguration{RevocationCheck: "off", RevocationURL: "https://mirror.invalid/list"}
	cfg.Store.Path = t.TempDir() + "/hikyo.db"
	t.Setenv("HIKYO_UPGRADE_REVOCATION_CHECK", "stale-from-the-parent")
	root := bytes.Repeat([]byte{1}, 32)
	cfg.RootKeyFile = filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(cfg.RootKeyFile, []byte(crypto.EncodeRootKey(root)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := &unattendedProcessHost{cfg: cfg, root: root, directory: t.TempDir()}
	command, closeCredential, err := host.child(t.Context(), "/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	defer closeCredential()
	got := map[string]string{}
	for _, entry := range command.Env {
		key, value, _ := strings.Cut(entry, "=")
		got[key] = value
	}
	policy := cfg.Upgrade.PolicyEnvironment()
	for _, key := range config.UpgradePolicyVariables {
		if want, ok := policy[key]; !ok || want == "" || got[key] != want {
			t.Fatalf("child %s = %q, want %q", key, got[key], want)
		}
	}
}

func TestRevocationCheckSkippedByTheOperatorWarnsAtTheDefaultLevel(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	cfg := &config.Config{}
	cfg.Upgrade.RevocationCheck = "off"
	cfg.Upgrade.StateDirectory = t.TempDir()
	if _, err := bridgeRevocation(cfg)(t.Context(), releasetrust.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logged.String(), "level=WARN") || !strings.Contains(logged.String(), string(bridgerevocation.Disabled)) {
		t.Fatalf("a disabled check must warn: %q", logged.String())
	}
}
