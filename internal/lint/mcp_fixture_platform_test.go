package lint

import (
	"go/ast"
	"go/types"
	"maps"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestMCPFixtureHelperPinsMatchTargetPlatform(t *testing.T) {
	const helper = mcpFixtureDirectory + "listeners.go:listenerAccepting"
	const lifecycle = mcpFixtureDirectory + "lifecycle.go:probe.lifecycle"
	linuxHash := "c1a9de843f6d422726c459d63e5b000144a62a4a7f13b61ebec73327c85a2f9a"
	darwinHash := "c338e1dabd997e89c20b02319506c81824f51d5870c975bd57c5ee696bce3e8b"
	root := repoRoot(t)
	loaded := map[string][]*packages.Package{}
	for _, tc := range []struct{ native, context, target, errno, hash, wrong string }{
		{"linux", "default", "linux", "111", linuxHash, darwinHash},
		{"linux", "ui", "linux", "111", linuxHash, darwinHash},
		{"linux", "darwin", "darwin", "61", darwinHash, linuxHash},
		{"darwin", "default", "darwin", "61", darwinHash, linuxHash},
		{"darwin", "ui", "darwin", "61", darwinHash, linuxHash},
	} {
		t.Run(tc.native+"/"+tc.context, func(t *testing.T) {
			context := BuildContext{Name: tc.context, GOOS: tc.target, ProductionOnly: true}
			if tc.context == "ui" {
				context.BuildFlags = []string{"-tags=ui"}
			}
			key := tc.target + "/" + tc.context
			pkgs := loaded[key]
			if pkgs == nil {
				var err error
				pkgs, err = LoadIn(context, Module+"/scripts/acceptance/mcp-fixture")
				if err != nil {
					t.Fatal(err)
				}
				loaded[key] = pkgs
			}
			declaration, exists := rawSQLDeclarations(pkgs, root, Module)[helper]
			if !exists {
				t.Fatal("real target helper missing")
			}
			hash, err := rawSQLBodyHash(declaration.pkg, declaration.fn)
			if err != nil || hash != tc.hash {
				t.Fatalf("actual %s helper hash %s: %v", tc.target, hash, err)
			}
			errno := ""
			ast.Inspect(declaration.fn.Body, func(node ast.Node) bool {
				if id, ok := node.(*ast.Ident); ok {
					if c, ok := declaration.pkg.TypesInfo.Uses[id].(*types.Const); ok && c.Pkg() != nil && c.Pkg().Path() == "syscall" && c.Name() == "ECONNREFUSED" {
						errno = c.Val().ExactString()
					}
				}
				return true
			})
			if errno != tc.errno {
				t.Fatalf("target constant value %s, want %s", errno, tc.errno)
			}
			protocols, err := reviewedPins.rawProtocolsForGOOS(tc.native)
			if err != nil {
				t.Fatal(err)
			}
			pins := map[string]RawSQLProtocol{}
			for owner, pin := range protocols {
				if strings.HasPrefix(owner, mcpFixtureDirectory) {
					pins[owner] = pin
				}
			}
			if actual := rawSQLProtocolDependencies(pins[lifecycle], tc.context)[helper]; actual != hash {
				t.Fatalf("selected pin %s, actual target %s", actual, hash)
			}
			if findings := CheckRawSQL(pkgs, root, Module, pins, tc.context); len(findings) != 0 {
				t.Fatal(findings)
			}
			// The other OS's exact hash cannot stand in for this target's pin.
			bad := pins[lifecycle]
			bad.BuildDependencies = maps.Clone(bad.BuildDependencies)
			bad.BuildDependencies[tc.context] = maps.Clone(bad.BuildDependencies[tc.context])
			bad.BuildDependencies[tc.context][helper] = tc.wrong
			pins[lifecycle] = bad
			assertFindings(t, CheckRawSQL(pkgs, root, Module, pins, tc.context), []string{"changed reviewed helper dependency " + helper})
		})
	}
}
