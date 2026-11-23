package lint

import (
	"go/ast"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestRawDriverAuthorityIsExactFileScoped(t *testing.T) {
	files := token.NewFileSet()
	allowed := files.AddFile("/owned/internal/store/admission.go", -1, 100)
	injected := files.AddFile("/owned/internal/store/new_writer.go", -1, 100)
	adjacentTest := files.AddFile("/owned/internal/store/new_writer_test.go", -1, 100)
	if !permittedHandlePosition(files, Module+"/internal/store", allowed.Pos(1)) {
		t.Fatal("reviewed admission boundary refused")
	}
	for _, f := range []*token.File{injected, adjacentTest} {
		if permittedHandlePosition(files, Module+"/internal/store", f.Pos(1)) {
			t.Fatalf("new file received ambient authority: %s", f.Name())
		}
	}
	if permittedRawConstructor(files, Module+"/internal/store", injected.Pos(1)) {
		t.Fatal("new runtime file received private constructor authority")
	}
	if permittedHandlePosition(files, Module+"/internal/service", allowed.Pos(1)) {
		t.Fatal("matching basename granted another package authority")
	}
	// Run the actual typed bypass fixture pretending it resides in store. A
	// package-only allowlist would silently approve the injected writer.
	pkgs, err := loadDefault("./testdata/badhandle")
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		if strings.Contains(pkg.PkgPath, "badhandle") {
			pkg.PkgPath = Module + "/internal/store"
		}
	}
	findings := checkDriverHandles(pkgs)
	assertFindings(t, findings, []string{"calls store.DB.PG", "names driver type database/sql.DB"})
}

func TestArtifactHarnessDriverAuthorityIsExactFileScoped(t *testing.T) {
	const harness = Module + "/scripts/security/artifact-harness"
	const runtimeFile = "/scripts/security/artifact-harness/runtime.go"
	pkgs, err := loadDefault("../../scripts/security/artifact-harness")
	if err != nil {
		t.Fatal(err)
	}
	var actual *packages.Package
	for _, pkg := range pkgs {
		if pkg.PkgPath == harness {
			actual = pkg
			break
		}
	}
	if actual == nil {
		t.Fatal("the real typed artifact harness was not loaded")
	}
	for _, tt := range []struct {
		name, owner, filename string
		generated             bool
		allowed               bool
	}{
		{name: "owned runtime", owner: harness, filename: "/owned" + runtimeFile, allowed: true},
		{name: "neighboring source", owner: harness, filename: "/owned/scripts/security/artifact-harness/another_runtime.go"},
		{name: "neighboring test", owner: harness, filename: "/owned/scripts/security/artifact-harness/runtime_test.go"},
		{name: "similarly named package", owner: harness + "-extra", filename: "/owned/scripts/security/artifact-harness-extra/runtime.go"},
		{name: "subpackage", owner: harness + "/tenant", filename: "/owned/scripts/security/artifact-harness/tenant/runtime.go"},
		{name: "production package", owner: Module + "/internal/service", filename: "/owned" + runtimeFile},
		{name: "external test package", owner: harness + "_test", filename: "/owned" + runtimeFile},
		{name: "matching filename with wrong owner", owner: harness + "-extra", filename: "/owned" + runtimeFile},
		{name: "generated queries remain forbidden", owner: harness, filename: "/owned" + runtimeFile, generated: true, allowed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Keep real parsed/typechecked driver uses and token offsets. Relocate
			// only their source filename/package to exercise the analyzer boundary,
			// rather than testing the inventory lookup in isolation.
			files := token.NewFileSet()
			actual.Fset.Iterate(func(file *token.File) bool {
				name := file.Name()
				if strings.HasSuffix(filepath.ToSlash(name), runtimeFile) {
					name = tt.filename
				}
				files.AddFile(name, file.Base(), file.Size()).SetLines(file.Lines())
				return true
			})
			candidate := *actual
			candidate.PkgPath = tt.owner
			candidate.Fset = files
			if tt.generated {
				addImport(&candidate, Module+"/internal/store/pggen")
			}
			findings := checkDriverHandles([]*packages.Package{&candidate})
			var driverFindings []string
			for _, finding := range findings {
				if strings.Contains(finding, "names driver type github.com/jackc/pgx/v5.Conn") {
					driverFindings = append(driverFindings, finding)
				}
			}
			if tt.allowed && len(driverFindings) != 0 {
				t.Fatalf("owned disposable-database runtime refused: %v", driverFindings)
			}
			if !tt.allowed && len(driverFindings) == 0 {
				t.Fatal("an adjacent file/package received raw driver authority")
			}
			if tt.generated {
				assertFindings(t, findings, []string{"imports " + Module + "/internal/store/pggen"})
			} else if tt.allowed && len(findings) != 0 {
				t.Fatalf("unexpected allowed-harness findings: %v", findings)
			}
		})
	}
}

// addImport adds an import spec to the first file of a copy of the package; the
// analyzer judges imports from the variant's files, not from Imports.
func addImport(p *packages.Package, path string) {
	file := *p.Syntax[0]
	file.Imports = append(slices.Clone(file.Imports), &ast.ImportSpec{
		Path: &ast.BasicLit{ValuePos: file.Package, Kind: token.STRING, Value: strconv.Quote(path)},
	})
	p.Syntax = slices.Clone(p.Syntax)
	p.Syntax[0] = &file
}

// Every inventoried file exists: a renamed or deleted harness file must leave
// the inventory instead of silently keeping its authority for a same-named
// successor.
func TestDriverFilesExist(t *testing.T) {
	for file := range driverFiles {
		if _, err := os.Stat(filepath.Join(moduleRoot(), filepath.FromSlash(file))); err != nil {
			t.Errorf("driverFiles entry %s: %v", file, err)
		}
	}
}

// A //line directive cannot lend a file the authority of an inventoried path:
// the decision reads the file on disk.
func TestInventoriedPathClaimedByLineDirectiveIsRefused(t *testing.T) {
	const claimed = "/owned/internal/isolation/oauth2_e2e_test.go"
	if !driverFiles["internal/isolation/oauth2_e2e_test.go"] {
		t.Fatal("the claimed path is no longer inventoried; pick another")
	}
	files := token.NewFileSet()
	f := files.AddFile("/owned/internal/isolation/new_writer_test.go", -1, 100)
	f.SetLines([]int{0})
	f.AddLineInfo(0, claimed, 1)
	if got := files.Position(f.Pos(1)).Filename; got != claimed {
		t.Fatalf("directive not applied: %s", got)
	}
	if permittedHandlePosition(files, Module+"/internal/isolation", f.Pos(1)) {
		t.Fatal("a //line claim received an inventoried file's authority")
	}
}

// Every inventory entry still earns its place: with the inventory emptied,
// each entry's file produces at least one handles diagnostic in some analysis
// context. A file that no longer needs a raw handle must leave the inventory,
// so the inventory only pays down.
func TestDriverFilesAreAllUsed(t *testing.T) {
	saved := driverFiles
	driverFiles = map[string]bool{}
	defer func() { driverFiles = saved }()
	dirs := map[string]bool{}
	for file := range saved {
		dirs[Module+"/"+filepath.ToSlash(filepath.Dir(file))] = true
	}
	patterns := slices.Sorted(maps.Keys(dirs))
	used := map[string]bool{}
	root := filepath.ToSlash(moduleRoot()) + "/"
	for _, ctx := range Contexts {
		var pkgs []*packages.Package
		for _, pattern := range patterns {
			loaded, err := loadFixture(ctx, true, pattern)
			if err != nil && strings.Contains(err.Error(), "build constraints exclude all Go files") {
				continue // a package that does not build under this context
			}
			if err != nil {
				t.Fatal(err)
			}
			pkgs = append(pkgs, loaded...)
		}
		for _, finding := range checkDriverHandles(pkgs) {
			posn, _, _ := strings.Cut(finding, ": handles: ")
			if i := strings.LastIndex(posn, ":"); i >= 0 {
				posn = posn[:i]
			}
			if i := strings.LastIndex(posn, ":"); i >= 0 {
				posn = posn[:i]
			}
			used[strings.TrimPrefix(filepath.ToSlash(posn), root)] = true
		}
	}
	for file := range saved {
		if !used[file] {
			t.Errorf("driverFiles entry %s produces no handles diagnostic without it; remove it from the inventory", file)
		}
	}
}
