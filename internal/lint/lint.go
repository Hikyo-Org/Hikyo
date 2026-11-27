// Package lint holds the repository's custom static analyzers. They are
// go/analysis passes built into one vet tool (cmd/lintvet); this package's
// tests build it and run it with go vet once per analysis context, so a
// violation is a build failure in CI (docs/adr/lint-analysis-driver.md). Each
// is a guardrail with its evasion limits stated in the ADR; logic bugs inside
// the trusted set are review's job, not lint's.
package lint

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/modroot"
)

// Module is the module path the analyzers reason about.
const Module = "github.com/Hikyo-Org/hikyo"

// BuildContext is one supported analysis context: a source set a release build
// selects. The analyzers and the boundary tests run under every context, so a
// file selected only by a build tag or by GOOS is inspected rather than left
// to whatever the host's ambient build context happens to pick.
type BuildContext struct {
	Name string
	// BuildFlags are passed to the build system (for example -tags=ui).
	BuildFlags []string
	// GOOS overrides the target operating system; empty keeps the host's.
	GOOS string
}

// Contexts is the supported analysis matrix, shared by the lint and boundary
// loaders. The default context is first.
var Contexts = []BuildContext{
	{Name: "default"},
	{Name: "ui", BuildFlags: []string{"-tags=ui"}},
	{Name: "darwin", GOOS: "darwin"},
	{Name: "windows", GOOS: "windows"},
}

// Env is the explicit environment for a build-system query under the context.
// GOFLAGS is cleared so the ambient shell cannot change file selection, and
// GOOS is pinned when the context names one.
func (c BuildContext) Env() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOFLAGS=") || strings.HasPrefix(kv, "GOOS=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "GOFLAGS=")
	if c.GOOS != "" {
		env = append(env, "GOOS="+c.GOOS)
	}
	return env
}

// webuiDistPlaceholder is the file the ui context overlays when the Vite build
// output is absent: internal/webui/embedded.go embeds all:dist, so without at
// least one file the package cannot be listed under -tags=ui. An overlay makes
// the analysis independent of whether the web build ran, without writing into
// the tree; embedded bytes never affect imports or types.
const webuiDistPlaceholder = "internal/webui/dist/index.html"

// Overlay returns the build overlay the context needs: the webui dist
// placeholder for the ui context when no dist file exists, otherwise nil.
// It locates the module from the working directory, which must be inside the
// module, as it already must be for the go command to build it.
func (c BuildContext) Overlay() (map[string][]byte, error) {
	if !c.needsWebuiPlaceholder() {
		return nil, nil
	}
	root, err := modroot.Find()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, "internal", "webui", "dist"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(entries) > 0 {
		return nil, nil
	}
	return map[string][]byte{filepath.Join(root, filepath.FromSlash(webuiDistPlaceholder)): nil}, nil
}

// WriteOverlay materializes Overlay as a `go list -overlay` file inside dir and
// returns its path, or "" when the context needs no overlay.
func (c BuildContext) WriteOverlay(dir string) (string, error) {
	overlay, err := c.Overlay()
	if err != nil || len(overlay) == 0 {
		return "", err
	}
	replace := map[string]string{}
	for path, contents := range overlay {
		placeholder := filepath.Join(dir, filepath.Base(path))
		if err := os.WriteFile(placeholder, contents, 0o600); err != nil {
			return "", err
		}
		replace[path] = placeholder
	}
	raw, err := json.Marshal(map[string]any{"Replace": replace}, jsonwire.Stable)
	if err != nil {
		return "", err
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, raw, 0o600); err != nil {
		return "", err
	}
	return overlayPath, nil
}

func (c BuildContext) needsWebuiPlaceholder() bool {
	for _, flag := range c.BuildFlags {
		if flag == "-tags=ui" || strings.Contains(flag, "=ui,") || strings.HasSuffix(flag, ",ui") {
			return true
		}
	}
	return false
}
