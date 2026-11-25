package app

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

func TestBridgeRevocationRefusesMisconfiguration(t *testing.T) {
	for name, tc := range map[string]struct {
		check, state, want string
	}{
		"no state directory": {check: "off", state: "", want: "HIKYO_UPGRADE_STATE_DIR"},
		"unknown mode":       {check: "sometimes", state: t.TempDir(), want: "online or off"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Upgrade.RevocationCheck, cfg.Upgrade.StateDirectory = tc.check, tc.state
			_, err := bridgeRevocation(cfg)(t.Context(), releasetrust.Snapshot{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}
