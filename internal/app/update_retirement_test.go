package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/updater"
)

func TestBootRefusesConfiguredUpdaterBeforeOpeningResources(t *testing.T) {
	// Bypass config.Load deliberately. Direct callers cannot re-enable the
	// retired helper, even with an otherwise unusable datastore configuration.
	server, err := Boot(t.Context(), &config.Config{UpdaterSocket: "/run/legacy-updater.sock"}, nil)
	if server != nil || !errors.Is(err, updater.ErrRemoteApplyDisabled) || !strings.Contains(err.Error(), "HIKYO_UPDATER_SOCKET") {
		t.Fatalf("server=%v error=%v, want named updater refusal before startup", server, err)
	}
}
