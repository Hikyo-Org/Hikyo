package app

import (
	"context"
	"errors"
	"fmt"
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
	// A probe that did not finish or could not start says nothing about the release, so it
	// must not be reported as a release defect. A candidate that ran and exited nonzero
	// (an unknown flag) or listed other formats is the historical case.
	if ctx.Err() != nil {
		return fmt.Errorf("the staged release did not answer the bundle format probe: %w", ctx.Err())
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return fmt.Errorf("the staged release could not be run to probe its bundle formats: %w", err)
	}
	if err != nil || !slices.Contains(strings.Fields(string(output)), upgradebundle.PlatformIndexFormat) {
		return errors.New("release lacks platform-specific bundle support; select a compatible upgrade route")
	}
	return nil
}
