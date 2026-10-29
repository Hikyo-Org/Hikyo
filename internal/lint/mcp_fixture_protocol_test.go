package lint

import (
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestMCPFixtureDriverAuthorityIsExactFileScoped(t *testing.T) {
	const owner = Module + "/scripts/acceptance/mcp-fixture"
	pkgs, err := Load("../../scripts/acceptance/mcp-fixture")
	if err != nil {
		t.Fatal(err)
	}
	var actual *packages.Package
	for _, p := range pkgs {
		if p.PkgPath == owner {
			actual = p
			break
		}
	}
	if actual == nil {
		t.Fatal("real typed MCP fixture package not loaded")
	}
	for _, source := range []string{"seed.go", "local.go", "lifecycle.go", "public.go"} {
		for _, tc := range []struct {
			name, pkg, file    string
			allowed, generated bool
		}{
			{name: "owned source", pkg: owner, file: "/owned/" + mcpFixtureDirectory + source, allowed: true},
			{name: "neighboring source", pkg: owner, file: "/owned/" + mcpFixtureDirectory + "new_writer.go"},
			{name: "neighboring test", pkg: owner, file: "/owned/" + mcpFixtureDirectory + "new_writer_test.go"},
			{name: "similar package", pkg: owner + "-extra", file: "/owned/" + mcpFixtureDirectory + source},
			{name: "subpackage", pkg: owner + "/other", file: "/owned/" + mcpFixtureDirectory + source},
			{name: "application package", pkg: Module + "/internal/service", file: "/owned/" + mcpFixtureDirectory + source},
			{name: "external tests", pkg: owner + "_test", file: "/owned/" + mcpFixtureDirectory + source},
			{name: "generated imports remain refused", pkg: owner, file: "/owned/" + mcpFixtureDirectory + source, allowed: true, generated: true},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				files := token.NewFileSet()
				actual.Fset.Iterate(func(file *token.File) bool {
					name := file.Name()
					if strings.HasSuffix(filepath.ToSlash(name), "/"+mcpFixtureDirectory+source) {
						name = tc.file
					}
					files.AddFile(name, file.Base(), file.Size()).SetLines(file.Lines())
					return true
				})
				candidate := *actual
				candidate.PkgPath, candidate.Fset = tc.pkg, files
				if tc.generated {
					candidate.Imports = maps.Clone(actual.Imports)
					candidate.Imports[Module+"/internal/store/pggen"] = &packages.Package{PkgPath: Module + "/internal/store/pggen"}
				}
				findings := CheckDriverHandles([]*packages.Package{&candidate})
				driverFound := false
				for _, finding := range findings {
					driverFound = driverFound || strings.Contains(finding, tc.file+":") && (strings.Contains(finding, "names driver type") || strings.Contains(finding, "calls store.DB.PG"))
				}
				if tc.allowed && driverFound {
					t.Fatalf("exact fixture file refused: %v", findings)
				}
				if !tc.allowed && !driverFound {
					t.Fatal("neighboring file/package received raw-driver authority")
				}
				if tc.generated {
					assertFindings(t, findings, []string{"imports " + Module + "/internal/store/pggen"})
				} else if tc.allowed && len(findings) != 0 {
					t.Fatal(findings)
				}
			})
		}
	}
}

func TestMCPFixtureProtocolsRejectDrift(t *testing.T) {
	root := repoRoot(t)
	protocols, err := reviewedPins.rawProtocols()
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]RawSQLProtocol{}
	for _, owner := range []string{"seed.go:requireEmptyPostgres", "public.go:observeReadOnly", "public.go:holdFixtureCatalogue", "lifecycle.go:probe.lifecycle"} {
		pin, exists := protocols[mcpFixtureDirectory+owner]
		if !exists {
			t.Fatal("missing named fixture protocol", owner)
		}
		pins[mcpFixtureDirectory+owner] = pin
	}
	for _, tc := range []struct{ name, file, function, old, replacement, appendSource, alternate, refusal string }{
		{name: "reviewed fixture"},
		{name: "fresh tenant query", file: "seed.go", old: "SELECT COUNT(*) FROM public.orgs", replacement: "SELECT COUNT(*) FROM principals", refusal: "requireEmptyPostgres changed its reviewed engine protocol"},
		{name: "write-capable preflight", file: "seed.go", old: "AccessMode: pgx.ReadOnly", replacement: "AccessMode: pgx.ReadWrite", refusal: "requireEmptyPostgres changed its reviewed engine protocol"},
		{name: "MCP database guard", file: "seed.go", old: `u.Path != "/hikyo_mcp_651"`, replacement: `u.Path != "/unowned"`, refusal: "changed reviewed helper dependency " + mcpFixtureDirectory + "seed.go:validateDSN"},
		{name: "audit database guard", file: "audit_seed.go", old: `u.Path != "/hikyo_audit_1037"`, replacement: `u.Path != "/hikyo_mcp_651"`, refusal: "changed reviewed helper dependency " + mcpFixtureDirectory + "audit_seed.go:auditStoreConfig"},
		{name: "claim observation", file: "public.go", old: "SELECT COUNT(*) FROM mcp_inflight", replacement: "SELECT COUNT(*) FROM principals", refusal: "observeReadOnly changed its reviewed engine protocol"},
		{name: "catalogue lock", file: "public.go", old: "LOCK TABLE keys IN ACCESS EXCLUSIVE MODE", replacement: "LOCK TABLE principals IN ACCESS EXCLUSIVE MODE", refusal: "holdFixtureCatalogue changed its reviewed engine protocol"},
		{name: "lifecycle mutation", file: "lifecycle.go", old: "LOCK TABLE keys IN ACCESS EXCLUSIVE MODE", replacement: "DELETE FROM keys", refusal: "probe.lifecycle changed its reviewed engine protocol"},
		{name: "admitted caller drift", file: "main.go", old: "opener = openFreshFixture", replacement: "opener = openFixture", refusal: "run references protocol"},
		{name: "pre-admission check removed", file: "seed.go", function: "openFreshFixture", old: "if err := requireEmptyPostgres(ctx, cfg, false); err != nil {\n\t\treturn nil, nil, err\n\t}", replacement: "if err := error(nil); err != nil {\n\t\treturn nil, nil, err\n\t}", refusal: "openFreshFixture references protocol"},
		{name: "post-admission check removed", file: "seed.go", function: "openFreshFixture", old: "if err := requireEmptyPostgres(ctx, cfg, false); err != nil {\n\t\tdb.Close()", replacement: "if err := error(nil); err != nil {\n\t\tdb.Close()", refusal: "openFreshFixture references protocol"},
		{name: "admission precedes first check", file: "seed.go", function: "openFreshFixture", old: "if err := requireEmptyPostgres(ctx, cfg, false); err != nil {\n\t\treturn nil, nil, err\n\t}\n\tdb, kr, err := openFixture(ctx, cfg)", replacement: "db, kr, err := openFixture(ctx, cfg)\n\tif err := requireEmptyPostgres(ctx, cfg, false); err != nil {\n\t\treturn nil, nil, err\n\t}", refusal: "openFreshFixture references protocol"},
		{name: "hold guard removed", file: "public.go", function: "holdFixtureCatalogue", old: "validateDSN(cfg.DSN, cfg.Development)", replacement: "error(nil)", refusal: "holdFixtureCatalogue changed its reviewed engine protocol"},
		{name: "hold connection override", file: "public.go", function: "holdFixtureCatalogue", old: "pgx.Connect(ctx, cfg.DSN)", replacement: "pgx.Connect(ctx, cfg.DSN+\"&host=unowned.invalid\")", refusal: "holdFixtureCatalogue changed its reviewed engine protocol"},
		{name: "new caller", file: "seed.go", appendSource: "\nfunc anotherPreflight(ctx context.Context, cfg settings) error { return requireEmptyPostgres(ctx, cfg, false) }\n", refusal: "anotherPreflight references protocol"},
		{name: "new alias", file: "seed.go", appendSource: "\nfunc aliasPreflight(ctx context.Context, cfg settings) error { preflight := requireEmptyPostgres; return preflight(ctx, cfg, false) }\n", refusal: "aliasPreflight references protocol"},
		{name: "new SQL owner", file: "seed.go", appendSource: "\nfunc anotherSQL(ctx context.Context, conn *pgx.Conn) { _, _ = conn.Exec(ctx, \"DELETE FROM orgs\") }\n", refusal: "anotherSQL executes handwritten SQL outside a named engine protocol"},
		{name: "neighboring file", file: "public.go", alternate: "unreviewed_public.go", refusal: "unreviewed_public.go:observeReadOnly executes handwritten SQL outside a named engine protocol"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overlay := map[string][]byte{}
			if tc.file != "" {
				path := filepath.Join(root, mcpFixtureDirectory, tc.file)
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				source := string(raw)
				if tc.old != "" {
					start := 0
					if tc.function != "" {
						start = strings.Index(source, "func "+tc.function+"(")
						if start < 0 {
							t.Fatal("missing mutation function", tc.function)
						}
					}
					if !strings.Contains(source[start:], tc.old) {
						t.Fatal("missing mutation target", tc.old)
					}
					source = source[:start] + strings.Replace(source[start:], tc.old, tc.replacement, 1)
				}
				overlay[path] = []byte(source + tc.appendSource)
				if tc.alternate != "" {
					overlay[path] = []byte("package main\n")
					overlay[filepath.Join(filepath.Dir(path), tc.alternate)] = []byte(source)
				}
			}
			// Typecheck the actual fixture and its guard/probe helpers.
			// Whole-module ownership is checked separately in every build context.
			pkgs, err := packages.Load(&packages.Config{
				Dir: root, Overlay: overlay,
				Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
					packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
			}, Module+"/scripts/acceptance/mcp-fixture")
			if err != nil || len(pkgs) == 0 {
				t.Fatalf("typed fixture load: %v", err)
			}
			for _, pkg := range pkgs {
				for _, err := range pkg.Errors {
					t.Fatal(err)
				}
			}
			findings := CheckRawSQL(pkgs, root, Module, pins)
			if tc.refusal == "" {
				if len(findings) != 0 {
					t.Fatalf("reviewed fixture refused: %v", findings)
				}
			} else {
				assertFindings(t, findings, []string{tc.refusal})
			}
		})
	}
}

func TestMCPFixtureProtocolBuildContextsFailClosed(t *testing.T) {
	root := repoRoot(t)
	pkgs, err := Load("../../scripts/acceptance/mcp-fixture")
	if err != nil {
		t.Fatal(err)
	}
	protocols, err := reviewedPins.rawProtocols()
	if err != nil {
		t.Fatal(err)
	}
	owner := mcpFixtureDirectory + "seed.go:requireEmptyPostgres"
	pin := protocols[owner]
	for _, context := range []string{"default", "ui", "darwin", "windows"} {
		selected, err := rawSQLProtocolsInContext(map[string]RawSQLProtocol{owner: pin}, context)
		if err != nil {
			t.Fatal(err)
		}
		_, exists := selected[owner]
		if exists != (context != "windows") {
			t.Fatalf("unexpected source authority in %s: %v", context, selected)
		}
	}
	// Exclusion removes SQL authority, rather than silencing findings if a
	// Unix-only source unexpectedly enters the Windows build later.
	findings := CheckRawSQL(pkgs, root, Module, map[string]RawSQLProtocol{owner: pin}, "windows")
	assertFindings(t, findings, []string{"requireEmptyPostgres executes handwritten SQL outside a named engine protocol"})
	// A selected source set still requires the exact owner to be present.
	assertFindings(t, CheckRawSQL(nil, root, Module, map[string]RawSQLProtocol{owner: pin}), []string{"protocol owner " + owner + " is absent"})
	if got := CheckRawSQL(nil, root, Module, map[string]RawSQLProtocol{owner: pin}, "windows"); len(got) != 0 {
		t.Fatalf("excluded owner incorrectly required: %v", got)
	}
	for _, contexts := range [][]string{{"unknown"}, {"default", "default"}} {
		bad := pin
		bad.BuildContexts = contexts
		assertFindings(t, CheckRawSQL(nil, root, Module, map[string]RawSQLProtocol{owner: bad}), []string{"unknown or duplicate owner build context"})
	}
	// Absence of a build restriction retains the existing all-context rule.
	pin.BuildContexts = nil
	assertFindings(t, CheckRawSQL(nil, root, Module, map[string]RawSQLProtocol{owner: pin}, "windows"), []string{"protocol owner " + owner + " is absent"})
}
