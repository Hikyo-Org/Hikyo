package lint

import (
	"fmt"
	"go/ast"
	"go/types"
	"maps"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// These private implementation holders are compile-time pinned but cannot be
// named by callers outside crypto. swapHandle deliberately remains in BOTH
// sets because the lint guard also follows nested fields of module types.
var internalOnlyRedactionPins = map[string]bool{
	Module + "/internal/crypto.keyHandle": true,
	Module + "/internal/crypto.dekEntry":  true,
}

// redactionPins reads actual typed assertions, including pointer and value
// expressions, rather than introducing a third hand-maintained sensitive set.
func redactionPins(t *testing.T, pkgs []*packages.Package) map[string]bool {
	t.Helper()
	pins := map[string]bool{}
	for _, p := range pkgs {
		if p.PkgPath != sensitiveOwner || p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				v, ok := n.(*ast.ValueSpec)
				if !ok || v.Type == nil {
					return true
				}
				surface, ok := p.TypesInfo.TypeOf(v.Type).(*types.Named)
				if !ok || surface.Obj().Name() != "redactionSurface" || surface.Obj().Pkg().Path() != sensitiveOwner {
					return true
				}
				for _, value := range v.Values {
					typ := types.Unalias(p.TypesInfo.TypeOf(value))
					if pointer, ok := typ.(*types.Pointer); ok {
						typ = types.Unalias(pointer.Elem())
					}
					named, ok := typ.(*types.Named)
					if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != sensitiveOwner {
						t.Fatalf("unexpected redaction pin at %s", p.Fset.Position(value.Pos()))
					}
					pins[namedKey(named)] = true
				}
				return false
			})
		}
	}
	if len(pins) == 0 {
		t.Fatal("no compile-time redactionSurface pins found")
	}
	return pins
}

func compareRedactionPins(pins, sensitive map[string]bool) []string {
	var findings []string
	for name := range internalOnlyRedactionPins {
		if !pins[name] || sensitive[name] {
			findings = append(findings, fmt.Sprintf("internal-only exception %s must be pinned only in crypto", name))
		}
	}
	for name, enabled := range pins {
		if enabled && !internalOnlyRedactionPins[name] && !sensitive[name] {
			findings = append(findings, fmt.Sprintf("compile-time pin %s missing from SensitiveTypes", name))
		}
	}
	for name, enabled := range sensitive {
		if enabled && !pins[name] {
			findings = append(findings, fmt.Sprintf("SensitiveTypes entry %s missing from compile-time pins", name))
		}
	}
	return findings
}

func TestRedactionPinsMatchSensitiveTypes(t *testing.T) {
	eachContext(t, sensitiveOwner, func(t *testing.T, pkgs []*packages.Package) {
		pins := redactionPins(t, pkgs)
		for _, finding := range compareRedactionPins(pins, SensitiveTypes) {
			t.Error(finding)
		}
		local := sensitiveOwner + ".LocalKeys"
		if !pins[local] || !SensitiveTypes[local] {
			t.Fatal("LocalKeys must be in both declared sets")
		}

		// Deliberately mismatched fixtures prove both directions fail, plus
		// stale and incorrectly broadened internal-only exceptions.
		for _, fixture := range []struct {
			name   string
			mutate func(pins, sensitive map[string]bool)
			want   string
		}{
			{"missing lint entry", func(_, sensitive map[string]bool) { delete(sensitive, local) }, "compile-time pin " + local + " missing"},
			{"missing crypto pin", func(pins, _ map[string]bool) { delete(pins, local) }, "SensitiveTypes entry " + local + " missing"},
			{"new exported pin", func(pins, _ map[string]bool) { pins[sensitiveOwner+".FutureKeys"] = true }, "FutureKeys missing from SensitiveTypes"},
			{"new lint entry", func(_, sensitive map[string]bool) { sensitive[sensitiveOwner+".FutureKeys"] = true }, "FutureKeys missing from compile-time pins"},
			{"stale internal exception", func(pins, _ map[string]bool) { delete(pins, sensitiveOwner+".keyHandle") }, "internal-only exception"},
			{"internal exception in lint", func(_, sensitive map[string]bool) { sensitive[sensitiveOwner+".dekEntry"] = true }, "internal-only exception"},
		} {
			t.Run(fixture.name, func(t *testing.T) {
				badPins, badSensitive := maps.Clone(pins), maps.Clone(SensitiveTypes)
				fixture.mutate(badPins, badSensitive)
				findings := compareRedactionPins(badPins, badSensitive)
				if !strings.Contains(strings.Join(findings, "\n"), fixture.want) {
					t.Fatalf("mismatch was not diagnosed as %q: %v", fixture.want, findings)
				}
			})
		}
	})
}
