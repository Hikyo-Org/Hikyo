package lint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/format"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func rawSQLDependencyFixture(t *testing.T, extra string) (string, *packages.Package, map[string]RawSQLProtocol) {
	t.Helper()
	root, pkg, _ := rawSQLFixture(t, "", extra)
	decls := rawSQLDeclarations([]*packages.Package{pkg}, root, Module)
	protocols := map[string]RawSQLProtocol{}
	for owner, decl := range decls {
		raw := false
		ast.Inspect(decl.fn.Body, func(n ast.Node) bool { raw = raw || rawSQLNode(pkg.TypesInfo, n); return true })
		if raw {
			hash, err := rawSQLBodyHash(pkg, decl.fn)
			if err != nil {
				t.Fatal(err)
			}
			protocols[owner] = RawSQLProtocol{Hash: hash, Reason: "fixture reviewed protocol"}
		}
	}
	for owner, protocol := range protocols {
		protocol.Dependencies = rawSQLDependencies(owner, decls, protocols, root)
		protocols[owner] = protocol
	}
	if findings := CheckRawSQL([]*packages.Package{pkg}, root, Module, protocols); len(findings) != 0 {
		t.Fatalf("valid fixture: %v", findings)
	}
	return root, pkg, protocols
}

func TestRawSQLProtocolBindsResolvedConstantAliases(t *testing.T) {
	source := `const sqlBase = "SELECT "
const sqlAlias = sqlBase + "1"
func constantProtocol(ctx context.Context,d driver) { d.Exec(ctx,sqlAlias) }
`
	_, _, pins := rawSQLDependencyFixture(t, source)
	root, pkg, _ := rawSQLDependencyFixture(t, strings.Replace(source, `"SELECT "`, `"DELETE "`, 1))
	findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module, pins), "\n")
	if !strings.Contains(findings, "constantProtocol changed its reviewed engine protocol") {
		t.Fatal(findings)
	}
	// Re-spelling the alias with its identical resolved value does not change the
	// executed SQL or the owner body, so it has the same dependency digest.
	root, pkg, _ = rawSQLDependencyFixture(t, strings.Replace(source, `sqlBase + "1"`, `"SELECT 1"`, 1))
	if findings := CheckRawSQL([]*packages.Package{pkg}, root, Module, pins); len(findings) != 0 {
		t.Fatal(findings)
	}
}

func TestRawSQLProtocolRetainsBodyHashWithoutConstants(t *testing.T) {
	_, pkg, file := rawSQLFixture(t, "", "")
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "protocol" {
			continue
		}
		var body bytes.Buffer
		if err := format.Node(&body, pkg.Fset, fn.Body); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(body.Bytes())
		legacy := hex.EncodeToString(digest[:])
		actual, err := rawSQLBodyHash(pkg, fn)
		if err != nil || actual != legacy {
			t.Fatalf("legacy %s actual %s: %v", legacy, actual, err)
		}
	}
}

func TestRawSQLProtocolRejectsSwappedConstantBindings(t *testing.T) {
	source := `const firstSQL = "SELECT 1"
const secondSQL = "SELECT 2"
func constantProtocol(ctx context.Context,d driver) {d.Exec(ctx,firstSQL);d.Exec(ctx,secondSQL)}
`
	_, _, pins := rawSQLDependencyFixture(t, source)
	changed := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(source, "SELECT 1", "SWAP"), "SELECT 2", "SELECT 1"), "SWAP", "SELECT 2")
	root, pkg, _ := rawSQLDependencyFixture(t, changed)
	findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module, pins), "\n")
	if !strings.Contains(findings, "constantProtocol changed its reviewed engine protocol") {
		t.Fatal(findings)
	}
}

func TestRawSQLProtocolPinsTransitiveAliasedHelpers(t *testing.T) {
	source := `const queryPrefix = "SELECT "
func escape(v string) string {return "["+v+"]"}
func build(v string) string { alias:=escape;return queryPrefix+alias(v) }
func helperProtocol(ctx context.Context,d driver) {d.Exec(ctx,build("1"))}
func retired() string {return "unused"}
`
	_, _, pins := rawSQLDependencyFixture(t, source)
	owner := "protocol.go:helperProtocol"
	for _, helper := range []string{"protocol.go:escape", "protocol.go:build"} {
		if pins[owner].Dependencies[helper] == "" {
			t.Fatalf("missing transitive helper %s", helper)
		}
	}
	for _, change := range []struct{ name, old, new string }{
		{"helper body", `"["+v+"]"`, `"("+v+")"`},
		{"helper constant", `"SELECT "`, `"DELETE "`},
	} {
		t.Run(change.name, func(t *testing.T) {
			root, pkg, _ := rawSQLDependencyFixture(t, strings.Replace(source, change.old, change.new, 1))
			findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module, pins), "\n")
			if !strings.Contains(findings, "helperProtocol changed reviewed helper dependency") {
				t.Fatal(findings)
			}
		})
	}
	for _, test := range []struct{ name, key, want string }{
		{"missing", "protocol.go:escape", "is missing reviewed helper dependency"},
		{"absent", "protocol.go:absent", "is absent from analyzed production source"},
		{"retired", "protocol.go:retired", "retire its dependency pin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, pkg, changed := rawSQLDependencyFixture(t, source)
			protocol := changed[owner]
			if test.name == "missing" {
				delete(protocol.Dependencies, test.key)
			} else {
				protocol.Dependencies[test.key] = "pinned"
			}
			changed[owner] = protocol
			findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module, changed), "\n")
			if !strings.Contains(findings, test.want) {
				t.Fatal(findings)
			}
		})
	}
}

func TestRawSQLProtocolPinsGenericReceiverHelpers(t *testing.T) {
	for _, parameters := range []string{"K any", "K, V any"} {
		t.Run(parameters, func(t *testing.T) {
			arguments, instantiated := "K", "int"
			if parameters == "K, V any" {
				arguments, instantiated = "K,V", "int,string"
			}
			source := "type builder[" + parameters + "] struct{}\nfunc (b *builder[" + arguments + "]) query() string {return \"SELECT 1\"}\nfunc genericProtocol(ctx context.Context,d driver) {b:=builder[" + instantiated + "]{}; alias:=b.query;d.Exec(ctx,alias())}\n"
			_, _, pins := rawSQLDependencyFixture(t, source)
			owner := "protocol.go:genericProtocol"
			if pins[owner].Dependencies["protocol.go:builder.query"] == "" {
				t.Fatal("generic method missing from dependencies")
			}
			root, pkg, _ := rawSQLDependencyFixture(t, strings.Replace(source, "SELECT 1", "SELECT 2", 1))
			findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module, pins), "\n")
			if !strings.Contains(findings, "genericProtocol changed reviewed helper dependency protocol.go:builder.query") {
				t.Fatal(findings)
			}
		})
	}
}

func TestRawSQLProtocolRejectsInvalidBuildDependencyPins(t *testing.T) {
	source := `func build() string {return "SELECT 1"}
func helperProtocol(ctx context.Context,d driver) {d.Exec(ctx,build())}
`
	for _, test := range []struct{ name, context, want string }{
		{"unknown context", "unknown", "unknown dependency build context unknown"},
		{"duplicate common", "default", "duplicates common helper dependency protocol.go:build"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, pkg, pins := rawSQLDependencyFixture(t, source)
			owner := "protocol.go:helperProtocol"
			pin := pins[owner]
			pin.BuildDependencies = map[string]map[string]string{test.context: {"protocol.go:build": pin.Dependencies["protocol.go:build"]}}
			pins[owner] = pin
			findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module, pins), "\n")
			if !strings.Contains(findings, test.want) {
				t.Fatal(findings)
			}
		})
	}
}
