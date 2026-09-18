// Package lint holds the three custom static analyzers the tenant-isolation
// ADR fixes (proof signatures, SQL predicate confinement, proof-forgery
// guard). They run against the real repository from this package's tests, so
// a violation is a build failure in CI. Each is a guardrail with its evasion
// limits stated in the ADR — logic bugs inside the trusted set are review's
// job, not lint's.
//
// The ADR sketches these as go/analysis passes; they are implemented
// directly over go/packages type information instead — same inputs, same
// build-failing effect, less framework. The checks return findings as
// strings so the tests (and the invariant suite) can assert emptiness and
// print every violation at once.
package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
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
	// ProductionOnly leaves test packages out of the load. The windows
	// context needs it: internal/upgradecustody's test package only
	// typechecks on unix (local_test.go names a helper declared in a
	// unix-only test file), so windows-only test files are not analysed.
	ProductionOnly bool
}

// Contexts is the supported analysis matrix, shared by the lint and boundary
// loaders. The default context is first, and Load/LoadRepo use it.
var Contexts = []BuildContext{
	{Name: "default"},
	{Name: "ui", BuildFlags: []string{"-tags=ui"}},
	{Name: "windows", GOOS: "windows", ProductionOnly: true},
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

// moduleRoot is the module root, derived from this file's location so the
// loaders work from any package directory.
func moduleRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// Overlay returns the go/packages overlay the context needs: the webui dist
// placeholder for the ui context when no dist file exists, otherwise nil.
func (c BuildContext) Overlay() (map[string][]byte, error) {
	if !c.needsWebuiPlaceholder() {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.Join(moduleRoot(), "internal", "webui", "dist"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(entries) > 0 {
		return nil, nil
	}
	return map[string][]byte{filepath.Join(moduleRoot(), filepath.FromSlash(webuiDistPlaceholder)): nil}, nil
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
	raw, err := json.Marshal(map[string]any{"Replace": replace})
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

var (
	loadMu    sync.Mutex
	loadOnces = map[string]*sync.Once{}
	loaded    = map[string][]*packages.Package{}
	loadErrs  = map[string]error{}
)

// LoadRepo loads every package in the module under the default context, with
// syntax and type information, exactly once per test process.
func LoadRepo() ([]*packages.Package, error) {
	return LoadRepoIn(Contexts[0])
}

// LoadRepoIn is LoadRepo under one supported context, cached per context.
func LoadRepoIn(ctx BuildContext) ([]*packages.Package, error) {
	loadMu.Lock()
	once, ok := loadOnces[ctx.Name]
	if !ok {
		once = &sync.Once{}
		loadOnces[ctx.Name] = once
	}
	loadMu.Unlock()
	once.Do(func() {
		pkgs, err := LoadIn(ctx, Module+"/...")
		loadMu.Lock()
		loaded[ctx.Name], loadErrs[ctx.Name] = pkgs, err
		loadMu.Unlock()
	})
	loadMu.Lock()
	defer loadMu.Unlock()
	return loaded[ctx.Name], loadErrs[ctx.Name]
}

// Load loads arbitrary patterns under the default context (the
// negative-fixture tests use it on testdata packages).
//
// The mode deliberately omits NeedDeps. The type checker still resolves every
// imported symbol — a root package's direct imports get their Types from
// compiler export data as a side effect of type-checking the root — so the
// analyzers, which reason about type identity (authz.Proof, driver handles),
// see everything they need. Adding NeedDeps alongside NeedSyntax|NeedTypesInfo
// would instead mark the entire transitive dependency closure (pgx, x/tools,
// stdlib) as "needs source", parsing every dependency's AST and building a
// full types.Info for each and keeping them all resident. On this package's
// test suite that was the difference between a ~7GB and a ~1.2GB peak RSS, and
// it made each tiny testdata load carry the full closure too; macOS Activity
// Monitor's "Memory" column inflated the former past 20GB once compression and
// -race shadow footprint were counted.
//
// The one seam: flatten() walks into imported module packages that are not
// roots of the load and therefore have Types (from export data) but no
// TypesInfo. Analyzers that reach those either select their target packages by
// path first (never touching a non-root's TypesInfo) or guard on nil type
// information before dereferencing it. In LoadRepo every module package is a
// root, so no module package is skipped there regardless.
func Load(patterns ...string) ([]*packages.Package, error) {
	return LoadIn(Contexts[0], patterns...)
}

// LoadIn loads arbitrary patterns under one supported context.
func LoadIn(ctx BuildContext, patterns ...string) ([]*packages.Package, error) {
	overlay, err := ctx.Overlay()
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		// Test packages are in scope: a forged proof in a test outside authz
		// is the same breach as one in production code.
		Tests:      !ctx.ProductionOnly,
		BuildFlags: ctx.BuildFlags,
		Env:        ctx.Env(),
		Overlay:    overlay,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			return nil, fmt.Errorf("load %s under %s: %s", p.PkgPath, ctx.Name, e)
		}
	}
	return pkgs, nil
}
