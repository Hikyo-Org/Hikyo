package app

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

// Probe only a staged, authenticated executable, before service fencing or
// migration. Historical runtimes require all platforms' payloads and cannot
// consume v2. Never silently redownload foreign payloads to accommodate them.
func checkAutomaticBundleFormat(ctx context.Context, executable string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--upgrade-bundle-formats")
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	output, err := command.Output()
	if err != nil || !slices.Contains(strings.Fields(string(output)), upgradebundle.PlatformIndexFormat) {
		return errors.New("release lacks platform-specific bundle support; select a compatible upgrade route")
	}
	return nil
}
