package ci

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Embed the same manifest run-go-tool.sh uses so compiled evidence validators
// retain their reviewed tool pins without depending on the working directory.
//
//go:embed go-tool-modules.txt
var goToolModules string

// GoToolVersion returns the exact, unique version pinned for a Go CI tool module.
func GoToolVersion(module string) (string, error) {
	return goToolVersion(goToolModules, module)
}

func goToolVersion(manifest, module string) (string, error) {
	var version string
	matches := 0
	for line := range strings.SplitSeq(manifest, "\n") {
		path, pin, found := strings.Cut(line, "@")
		if path != module {
			continue
		}
		matches++
		if !found || !strings.HasPrefix(pin, "v") {
			return "", fmt.Errorf("malformed Go CI tool pin for %s", module)
		}
		if _, err := semver.StrictNewVersion(strings.TrimPrefix(pin, "v")); err != nil {
			return "", fmt.Errorf("malformed Go CI tool pin for %s: %w", module, err)
		}
		version = pin
	}
	if matches != 1 {
		return "", fmt.Errorf("expected exactly one Go CI tool pin for %s, got %d", module, matches)
	}
	return version, nil
}
