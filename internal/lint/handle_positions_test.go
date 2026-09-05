package lint

import (
	"go/token"
	"golang.org/x/tools/go/packages"
	"strings"
	"testing"
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
