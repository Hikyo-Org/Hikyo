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

func TestAlertCollectorDriverAuthorityIsExactFileScoped(t *testing.T) {
	const owner = Module + "/scripts/security/alert-route"
	const sourceFile = "/" + alertCollectorSnapshotFile
	pkgs, err := Load("../../scripts/security/alert-route")
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
		t.Fatal("real alert collector package not loaded")
	}
	for _, tc := range []struct {
		name, pkg, file    string
		allowed, generated bool
	}{
		{name: "owned read-only snapshot", pkg: owner, file: "/owned" + sourceFile, allowed: true},
		{name: "neighboring source", pkg: owner, file: "/owned/scripts/security/alert-route/collector.go"},
		{name: "neighboring test", pkg: owner, file: "/owned/scripts/security/alert-route/collector_snapshot_test.go"},
		{name: "similar package", pkg: owner + "-extra", file: "/owned" + sourceFile},
		{name: "subpackage", pkg: owner + "/other", file: "/owned" + sourceFile},
		{name: "application package", pkg: Module + "/internal/service", file: "/owned" + sourceFile},
		{name: "external test package", pkg: owner + "_test", file: "/owned" + sourceFile},
		{name: "generated store import refused", pkg: owner, file: "/owned" + sourceFile, allowed: true, generated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := token.NewFileSet()
			actual.Fset.Iterate(func(file *token.File) bool {
				name := file.Name()
				if strings.HasSuffix(filepath.ToSlash(name), sourceFile) {
					name = tc.file
				}
				files.AddFile(name, file.Base(), file.Size()).SetLines(file.Lines())
				return true
			})
			candidate := *actual
			candidate.PkgPath = tc.pkg
			candidate.Fset = files
			if tc.generated {
				candidate.Imports = maps.Clone(actual.Imports)
				candidate.Imports[Module+"/internal/store/sqlitegen"] = &packages.Package{PkgPath: Module + "/internal/store/sqlitegen"}
			}
			findings := CheckDriverHandles([]*packages.Package{&candidate})
			driverFound := false
			for _, finding := range findings {
				driverFound = driverFound || strings.Contains(finding, "names driver type database/sql.")
			}
			if tc.allowed && driverFound {
				t.Fatalf("owned external snapshot refused: %v", findings)
			}
			if !tc.allowed && !driverFound {
				t.Fatal("neighboring source/package received raw-driver authority")
			}
			if tc.generated {
				assertFindings(t, findings, []string{"imports " + Module + "/internal/store/sqlitegen"})
			} else if tc.allowed && len(findings) != 0 {
				t.Fatal(findings)
			}
		})
	}
}

func TestAlertCollectorSnapshotProtocolRejectsDrift(t *testing.T) {
	root := repoRoot(t)
	filename := filepath.Join(root, alertCollectorSnapshotFile)
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	protocols, err := reviewedPins.rawProtocols()
	if err != nil {
		t.Fatal(err)
	}
	owner := alertCollectorSnapshotFile + ":readCollectorSnapshot"
	pin, ok := protocols[owner]
	if !ok {
		t.Fatal("missing exact external receiver protocol")
	}
	pins := map[string]RawSQLProtocol{owner: pin}
	change := func(old, new string) string {
		t.Helper()
		if !strings.Contains(source, old) {
			t.Fatal("missing mutation target", old)
		}
		return strings.Replace(source, old, new, 1)
	}
	for _, tc := range []struct{ name, source, alternate, refusal string }{
		{name: "reviewed snapshot", source: source},
		{name: "write-capable open", source: change(`RawQuery: "mode=ro"`, `RawQuery: "mode=rw"`), refusal: "readCollectorSnapshot changed its reviewed engine protocol"},
		{name: "application query", source: change(`"SELECT binding FROM stream"`, `"SELECT id FROM principals"`), refusal: "readCollectorSnapshot changed its reviewed engine protocol"},
		{name: "lost stream binding", source: change(`mustJSON(actual) != mustJSON(binding(s))`, `mustJSON(actual) == "impossible"`), refusal: "readCollectorSnapshot changed its reviewed engine protocol"},
		{name: "lost refusal guard", source: change(`failed.Int64 >= received`, `failed.Int64 > now.UnixNano()`), refusal: "readCollectorSnapshot changed its reviewed engine protocol"},
		{name: "scope helper drift", source: change(`b.Scope["project_id"] = s.Project`, `b.Scope["project_id"] = s.Org`), refusal: "changed reviewed helper dependency " + alertCollectorSnapshotFile + ":binding"},
		{name: "unreviewed caller", source: source + "\nfunc anotherReader(s source, now time.Time) ([]byte,int,error) { return readCollectorSnapshot(s,now) }\n", refusal: "anotherReader references protocol"},
		{name: "unreviewed SQL", source: source + "\nfunc otherSQL(db *sql.DB) { _, _ = db.Exec(\"DELETE FROM batches\") }\n", refusal: "otherSQL executes handwritten SQL outside a named engine protocol"},
		{name: "neighboring file", source: source, alternate: filepath.Join(filepath.Dir(filename), "unreviewed_snapshot.go"), refusal: "unreviewed_snapshot.go:readCollectorSnapshot executes handwritten SQL outside a named engine protocol"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overlay := map[string][]byte{filename: []byte(tc.source)}
			if tc.alternate != "" {
				overlay[filename] = []byte("package main\n")
				overlay[tc.alternate] = []byte(tc.source)
			}
			pkgs, err := packages.Load(&packages.Config{Dir: root, Overlay: overlay, Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports}, Module+"/scripts/security/alert-route")
			if err != nil {
				t.Fatal(err)
			}
			if len(pkgs) == 0 {
				t.Fatal("no typed alert collector loaded")
			}
			for _, p := range pkgs {
				for _, err := range p.Errors {
					t.Fatal(err)
				}
			}
			findings := CheckRawSQL(pkgs, root, Module, pins)
			if tc.refusal == "" {
				if len(findings) != 0 {
					t.Fatalf("reviewed external snapshot refused: %v", findings)
				}
			} else {
				assertFindings(t, findings, []string{tc.refusal})
			}
		})
	}
}
