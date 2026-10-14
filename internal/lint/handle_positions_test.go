package lint

import (
	"go/token"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestRawDriverAuthorityIsExactFileScoped(t *testing.T) {
	files := token.NewFileSet()
	allowed := files.AddFile("/owned/internal/store/admission.go", -1, 100)
	injected := files.AddFile("/owned/internal/store/new_writer.go", -1, 100)
	adjacentTest := files.AddFile("/owned/internal/store/new_writer_test.go", -1, 100)
	p := &packages.Package{Fset: files}
	if !permittedHandlePosition(p, Module+"/internal/store", allowed.Pos(1)) {
		t.Fatal("reviewed admission boundary refused")
	}
	for _, f := range []*token.File{injected, adjacentTest} {
		if permittedHandlePosition(p, Module+"/internal/store", f.Pos(1)) {
			t.Fatalf("new file received ambient authority: %s", f.Name())
		}
	}
	if permittedRawConstructor(p, Module+"/internal/store", injected.Pos(1)) {
		t.Fatal("new runtime file received private constructor authority")
	}
	if permittedHandlePosition(p, Module+"/internal/service", allowed.Pos(1)) {
		t.Fatal("matching basename granted another package authority")
	}
	// Run the actual typed bypass fixture pretending it resides in store. A
	// package-only allowlist would silently approve the injected writer.
	pkgs, err := Load("./testdata/badhandle")
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		if strings.Contains(pkg.PkgPath, "badhandle") {
			pkg.PkgPath = Module + "/internal/store"
		}
	}
	findings := CheckDriverHandles(pkgs)
	assertFindings(t, findings, []string{"calls store.DB.PG", "names driver type database/sql.DB"})
}

func TestArtifactHarnessDriverAuthorityIsExactFileScoped(t *testing.T) {
	const harness = Module + "/scripts/security/artifact-harness"
	const runtimeFile = "/scripts/security/artifact-harness/runtime.go"
	pkgs, err := Load("../../scripts/security/artifact-harness")
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
				candidate.Imports = maps.Clone(actual.Imports)
				candidate.Imports[Module+"/internal/store/pggen"] = &packages.Package{PkgPath: Module + "/internal/store/pggen"}
			}
			findings := CheckDriverHandles([]*packages.Package{&candidate})
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
