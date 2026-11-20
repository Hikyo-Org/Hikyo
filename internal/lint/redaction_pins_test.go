package lint

import (
	"fmt"
	"maps"
	"strings"
	"testing"
)

// These private implementation holders are compile-time pinned but cannot be
// named by callers outside crypto. swapHandle deliberately remains in BOTH
// sets because the lint guard also follows nested fields of module types.
var internalOnlyRedactionPins = map[string]bool{
	Module + "/internal/crypto.keyHandle": true,
	Module + "/internal/crypto.dekEntry":  true,
}

func pinSet(t *testing.T, summaries []Summary) map[string]bool {
	t.Helper()
	pins := map[string]bool{}
	for _, s := range summaries {
		if s.Kind == summaryPin {
			pins[s.Subject] = true
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
	// The real pins are compared with SensitiveTypes by TestVetDriverRepo; this
	// proves the comparison fails in every direction from a consistent baseline.
	{
		pins := maps.Clone(SensitiveTypes)
		for name := range internalOnlyRedactionPins {
			pins[name] = true
		}
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
	}
}
