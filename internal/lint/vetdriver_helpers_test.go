package lint

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestDecodeVetOutput(t *testing.T) {
	const pkg = Module + "/internal/store [" + Module + "/internal/store.test]"
	tree := func(analyzer, result string) string {
		return `{"` + pkg + `":{"` + analyzer + `":` + result + `}}`
	}
	diag := func(extra string) string {
		return `[{"category":"","posn":"/r/a_test.go:1:2","end":"/r/a_test.go:1:3","message":"m"` + extra + `}]`
	}
	summary := func(message string) string {
		return `[{"category":"summary","posn":"/r/a.go:1:1","end":"/r/a.go:1:1","message":"` + message + `"}]`
	}
	tests := []struct {
		name, raw, wantErr string
		check              func(t *testing.T, out *vetOutcome)
	}{
		{name: "empty output", raw: ""},
		{
			name: "a finding after a header line", raw: "# " + pkg + "\n" + tree("handles", diag("")),
			check: func(t *testing.T, out *vetOutcome) {
				if want := "/r/a_test.go:1:2: handles: m"; len(out.Findings) != 1 || out.Findings[0] != want {
					t.Fatalf("findings = %v", out.Findings)
				}
			},
		},
		{
			name: "a skipped-symlink warning is not output", raw: "warning: ignoring symlink /r/docs/site/node_modules/x\n" + tree("handles", diag("")),
			check: func(t *testing.T, out *vetOutcome) {
				if len(out.Findings) != 1 {
					t.Fatalf("findings = %v", out.Findings)
				}
			},
		},
		{
			name: "related and suggested fixes are accepted", raw: tree("forgeguard", diag(`,"related":[{"posn":"x","end":"y","message":"y"}],"suggested_fixes":[{"message":"z","edits":[{"filename":"f","start":1,"end":2,"new":"n"}]}]`)),
			check: func(t *testing.T, out *vetOutcome) {
				if len(out.Findings) != 1 {
					t.Fatalf("findings = %v", out.Findings)
				}
			},
		},
		{name: "unknown related field", raw: tree("forgeguard", diag(`,"related":[{"posn":"x","extra":1}]`)), wantErr: "unknown object member name"},
		{name: "unknown edit field", raw: tree("forgeguard", diag(`,"suggested_fixes":[{"message":"z","edits":[{"filename":"f","extra":1}]}]`)), wantErr: "unknown object member name"},
		{name: "mistyped edit offset", raw: tree("forgeguard", diag(`,"suggested_fixes":[{"message":"z","edits":[{"start":"1"}]}]`)), wantErr: "decode vet output"},
		{name: "analyzer error object", raw: tree("handles", `{"error":"boom"}`), wantErr: "analyzer handles failed"},
		{name: "empty error object", raw: tree("handles", `{}`), wantErr: "neither a diagnostic list nor an error"},
		{name: "unknown diagnostic field", raw: tree("handles", diag(`,"extra":1`)), wantErr: "unknown object member name"},
		{name: "unknown error object field", raw: tree("handles", `{"error":"x","extra":1}`), wantErr: "unknown object member name"},
		{name: "stray non-JSON line", raw: "go: downloading something\n" + tree("handles", diag("")), wantErr: "decode vet output"},
		{
			name: "target summary", raw: tree("fence", summary("target "+Module+"/internal/service")),
			check: func(t *testing.T, out *vetOutcome) {
				if !out.Targets["fence"][Module+"/internal/service"] || len(out.Findings) != 0 {
					t.Fatalf("targets = %v, findings = %v", out.Targets, out.Findings)
				}
			},
		},
		{
			name: "pin summary from redactpins", raw: tree("redactpins", summary("pin "+Module+"/internal/crypto.Key")),
			check: func(t *testing.T, out *vetOutcome) {
				if len(out.Summaries["redactpins"]) != 1 {
					t.Fatalf("summaries = %v", out.Summaries)
				}
			},
		},
		{name: "pin summary from another analyzer", raw: tree("fence", summary("pin x")), wantErr: "only redactpins may"},
		{name: "gofile summary from another analyzer", raw: tree("fence", summary("gofile /r/a.go")), wantErr: "only jsonv1 may"},
		{name: "exception summary from jsonv1", raw: tree("jsonv1", summary("exception "+Module+"/api/apigen"))},
		{
			name: "rawsql summaries", raw: tree("rawsql", summary("rawowner a.go:F")),
			check: func(t *testing.T, out *vetOutcome) {
				if len(out.Summaries["rawsql"]) != 1 || len(out.Findings) != 0 {
					t.Fatalf("summaries = %v, findings = %v", out.Summaries, out.Findings)
				}
			},
		},
		{name: "rawsql owner summary from another analyzer", raw: tree("fence", summary("rawowner a.go:F")), wantErr: "only rawsql may"},
		{name: "rawsql caller summary from another analyzer", raw: tree("fence", summary("rawcaller a.go:F b.go:G")), wantErr: "only rawsql may"},
		{name: "rawsql helper summary from another analyzer", raw: tree("fence", summary("rawhelper a.go:F")), wantErr: "only rawsql may"},
		{name: "rawsql unreached summary from another analyzer", raw: tree("fence", summary("rawunreached a.go:F b.go:G")), wantErr: "only rawsql may"},
		{name: "unknown summary kind", raw: tree("fence", summary("surprise x")), wantErr: "unknown summary kind"},
		{name: "malformed summary", raw: tree("fence", summary("target")), wantErr: "malformed summary"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := decodeVetOutput([]byte(tc.raw))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.check != nil {
				tc.check(t, out)
			}
		})
	}
}

func TestInstallersFlagSet(t *testing.T) {
	tests := []struct {
		name, value, wantErr, wantString string
	}{
		{name: "one owner", value: "a=X", wantString: "a=X"},
		{name: "names and owners", value: "b=Y,Z;a=X", wantString: "a=X;b=Y,Z"},
		{name: "empty value", value: "", wantErr: "is not owner=Name,Name"},
		{name: "no equals", value: "a", wantErr: "is not owner=Name,Name"},
		{name: "empty owner", value: "=X", wantErr: "is not owner=Name,Name"},
		{name: "empty names", value: "a=", wantErr: "is not owner=Name,Name"},
		{name: "trailing separator", value: "a=X;", wantErr: "is not owner=Name,Name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f installersFlag
			err := f.Set(tc.value)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := f.String(); got != tc.wantString {
				t.Fatalf("String = %q, want %q", got, tc.wantString)
			}
		})
	}
	var f installersFlag
	if err := f.Set("a=X"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("b=Y"); err != nil || f.String() != "b=Y" {
		t.Fatalf("second Set replaces the first: %v %q", err, f.String())
	}
}

func TestConfinementFlagRoundTrip(t *testing.T) {
	rules := append(slices.Clone(badRecoveryRules), badSetterRules...)
	var got confinementFlag
	if err := got.Set(rulesFlag(rules)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]RecoveryRule(got), normalizeRules(rules)) {
		t.Fatalf("round trip = %v, want %v", got, rules)
	}
	for _, bad := range []string{"NoEquals", "=pkg:Fn", "Name=pkgWithoutColon", "Name=pkg:", "Name=pkg:Fn,"} {
		if err := new(confinementFlag).Set(bad); err == nil {
			t.Errorf("Set(%q) accepted a malformed rule", bad)
		}
	}
}

// normalizeRules gives a rule with no entries the empty map Set produces.
func normalizeRules(rules []RecoveryRule) []RecoveryRule {
	out := slices.Clone(rules)
	for i := range out {
		if out[i].Allowed == nil {
			out[i].Allowed = map[string][]string{}
		}
	}
	return out
}
