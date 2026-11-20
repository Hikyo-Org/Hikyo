package lint

import (
	"strings"
	"testing"
)

func TestDeferredSite(t *testing.T) {
	const inPackage = Module + "/internal/store [" + Module + "/internal/store.test]"
	const external = Module + "/internal/store_test [" + Module + "/internal/store.test]"
	tests := []struct {
		name, pkgID, analyzer, posn string
		want                        bool
	}{
		{"handles in an in-package test file", inPackage, "handles", "/r/internal/store/a_test.go:3:4", true},
		{"redactfmt in an in-package test file", inPackage, "redactfmt", "/r/internal/store/a_test.go:3:4", true},
		{"production file of the test variant stays strict", inPackage, "handles", "/r/internal/store/a.go:3:4", false},
		{"external test package stays strict", external, "handles", "/r/internal/store/a_test.go:3:4", false},
		{"other analyzers stay strict", inPackage, "forgeguard", "/r/internal/store/a_test.go:3:4", false},
		{"malformed position stays strict", inPackage, "handles", "a_test.go", false},
		{"position without a line stays strict", inPackage, "handles", "/r/a_test.go:x:4", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := deferredSite(tc.pkgID, tc.analyzer, tc.posn); got != tc.want {
				t.Fatalf("deferredSite = %v, want %v", got, tc.want)
			}
		})
	}
}

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
			name: "findings after a header line", raw: "# " + pkg + "\n" + tree("handles", diag("")),
			check: func(t *testing.T, out *vetOutcome) {
				want := "/r/a_test.go:1:2: handles: m"
				if len(out.Findings) != 1 || out.Findings[0] != want || !out.Deferred[want] {
					t.Fatalf("findings = %v, deferred = %v", out.Findings, out.Deferred)
				}
			},
		},
		{
			name: "related and suggested fixes are accepted", raw: tree("forgeguard", diag(`,"related":[{"posn":"x","message":"y"}],"suggested_fixes":[{"message":"z"}]`)),
			check: func(t *testing.T, out *vetOutcome) {
				if len(out.Findings) != 1 || out.Deferred[out.Findings[0]] {
					t.Fatalf("findings = %v, deferred = %v", out.Findings, out.Deferred)
				}
			},
		},
		{name: "analyzer error object", raw: tree("handles", `{"error":"boom"}`), wantErr: "analyzer handles failed"},
		{name: "empty error object", raw: tree("handles", `{}`), wantErr: "neither a diagnostic list nor an error"},
		{name: "unknown diagnostic field", raw: tree("handles", diag(`,"extra":1`)), wantErr: "unknown field"},
		{name: "unknown error object field", raw: tree("handles", `{"error":"x","extra":1}`), wantErr: "unknown field"},
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

func TestCheckDeferredCeiling(t *testing.T) {
	pinned := map[string]map[string]int{"handles": {"a_test.go": 3, "b_test.go": 2}}
	tests := []struct {
		name     string
		observed map[string]map[string]int
		want     []string
	}{
		{"at the ceiling", map[string]map[string]int{"handles": {"a_test.go": 3, "b_test.go": 2}}, nil},
		{"above the ceiling", map[string]map[string]int{"handles": {"a_test.go": 4, "b_test.go": 2}}, []string{"above its pinned ceiling 3"}},
		{"new file", map[string]map[string]int{"handles": {"a_test.go": 3, "b_test.go": 2, "c_test.go": 1}}, []string{"c_test.go gained 1"}},
		{"new analyzer", map[string]map[string]int{"handles": {"a_test.go": 3, "b_test.go": 2}, "redactfmt": {"a_test.go": 1}}, []string{"redactfmt: a_test.go gained 1"}},
		{"dropped count", map[string]map[string]int{"handles": {"a_test.go": 2, "b_test.go": 2}}, []string{"lower the pin"}},
		{"cleared file", map[string]map[string]int{"handles": {"a_test.go": 3}}, []string{"b_test.go has 0", "lower the pin"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(checkDeferredCeiling("default", pinned, tc.observed), "\n")
			if len(tc.want) == 0 && got != "" {
				t.Fatalf("unexpected findings:\n%s", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("findings do not contain %q:\n%s", want, got)
				}
			}
		})
	}
}
