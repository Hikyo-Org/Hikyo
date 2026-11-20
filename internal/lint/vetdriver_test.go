package lint

// The vet driver: the analyzers built into one go vet tool and run once per
// analysis context (lint-analysis driver ADR, decision 1). The old go/packages
// harness keeps running for the same analyzers until TestVetDriverParity shows
// every old finding is also reported here.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// vetDiagnostic is one entry of `go vet -json`; unknown fields fail the decode.
type vetDiagnostic struct {
	Category string `json:"category"`
	Posn     string `json:"posn"`
	End      string `json:"end"`
	Message  string `json:"message"`
}

// vetResult is one analyzer's output for one package: diagnostics, or the
// analyzer's own error.
type vetResult struct {
	Diagnostics []vetDiagnostic
	Err         string
}

func (r *vetResult) UnmarshalJSON(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		return dec.Decode(&r.Diagnostics)
	}
	var failure struct {
		Error string `json:"error"`
	}
	if err := dec.Decode(&failure); err != nil {
		return err
	}
	if failure.Error == "" {
		return errors.New("analyzer result is neither a diagnostic list nor an error")
	}
	r.Err = failure.Error
	return nil
}

// vetTree is package ID -> analyzer name -> result.
type vetTree map[string]map[string]vetResult

// vetOutcome is a decoded vet run: findings separated from summaries.
type vetOutcome struct {
	Findings []string
	// Deferred are the findings of strictDeferred analyzers in in-package
	// _test.go files; they stay in Findings for parity.
	Deferred map[string]bool
	// Targets is analyzer -> package paths its passes reported as analyzed.
	Targets   map[string]map[string]bool
	Summaries map[string][]Summary
}

// decodeVetOutput parses vet's concatenated JSON trees. go prefixes a package's
// output with a "# pkg" header line; anything else that is not JSON is a tool
// failure and fails the decode.
func decodeVetOutput(raw []byte) (*vetOutcome, error) {
	var kept []byte
	for line := range bytes.Lines(raw) {
		if bytes.HasPrefix(line, []byte("# ")) {
			continue
		}
		kept = append(kept, line...)
	}
	out := &vetOutcome{Deferred: map[string]bool{}, Targets: map[string]map[string]bool{}, Summaries: map[string][]Summary{}}
	dec := json.NewDecoder(bytes.NewReader(kept))
	for {
		var tree vetTree
		if err := dec.Decode(&tree); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode vet output: %w", err)
		}
		for pkg, analyzers := range tree {
			for analyzer, result := range analyzers {
				if result.Err != "" {
					return nil, fmt.Errorf("analyzer %s failed on %s: %s", analyzer, pkg, result.Err)
				}
				for _, d := range result.Diagnostics {
					if d.Category != SummaryCategory {
						finding := FormatFinding(d.Posn, analyzer, d.Message)
						out.Findings = append(out.Findings, finding)
						if deferredSite(pkg, analyzer, d.Posn) {
							out.Deferred[finding] = true
						}
						continue
					}
					summary, err := ParseSummary(d.Message)
					if err != nil {
						return nil, err
					}
					out.Summaries[analyzer] = append(out.Summaries[analyzer], summary)
					if summary.Kind == summaryTarget {
						if out.Targets[analyzer] == nil {
							out.Targets[analyzer] = map[string]bool{}
						}
						out.Targets[analyzer][summary.Subject] = true
					}
				}
			}
		}
	}
	slices.Sort(out.Findings)
	return out, nil
}

// strictDeferred are the analyzers whose in-package test sites the vet driver
// reports but does not yet gate: the old harness never saw those files, and
// the sites await a reviewed follow-up (docs/handoff/lint-vet-driver-test-sites.md).
// Production files and external test packages stay strict.
var strictDeferred = map[string]bool{"handles": true, "redactfmt": true}

// deferredSite reports whether a diagnostic is in a _test.go file of an
// in-package test variant: vet keys that variant by the plain package path,
// while an external test package's key ends in "_test".
func deferredSite(pkgID, analyzer, posn string) bool {
	path, _, _ := strings.Cut(pkgID, " [")
	if !strictDeferred[analyzer] || strings.HasSuffix(path, "_test") {
		return false
	}
	file, _, err := splitPosn(posn)
	return err == nil && strings.HasSuffix(file, "_test.go")
}

// buildVetTool builds the vet tool into the test's temp dir.
func buildVetTool(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lintvet")
	cmd := exec.Command("go", "build", "-o", bin, Module+"/internal/lint/cmd/lintvet")
	cmd.Dir = moduleRoot()
	cmd.Env = Contexts[0].Env()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build vet tool: %v\n%s", err, out)
	}
	return bin
}

// runVet runs `go vet -vettool` under one context. With -json vet exits zero
// when it reports diagnostics, so a non-zero exit is a tool or type-check
// failure, never findings.
func runVet(t *testing.T, bin string, ctx BuildContext, flags, patterns []string) *vetOutcome {
	t.Helper()
	args := []string{"vet", "-vettool=" + bin, "-json", "-p", "2"}
	args = append(args, ctx.BuildFlags...)
	overlay, err := ctx.WriteOverlay(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if overlay != "" {
		args = append(args, "-overlay="+overlay)
	}
	args = append(append(args, flags...), patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = moduleRoot()
	cmd.Env = ctx.Env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go %s under %s: %v\n%s", strings.Join(args, " "), ctx.Name, err, excerpt(stdout.String()+stderr.String()))
	}
	outcome, err := decodeVetOutput(append(stdout.Bytes(), stderr.Bytes()...))
	if err != nil {
		t.Fatalf("go %s under %s: %v\n%s", strings.Join(args, " "), ctx.Name, err, excerpt(stdout.String()+stderr.String()))
	}
	return outcome
}

func excerpt(s string) string {
	const limit = 4000
	if len(s) > limit {
		return s[:limit] + "\n... truncated"
	}
	return s
}

// missingTargets renders the target-presence findings for the checks.
func missingTargets(out *vetOutcome, checks []Check) []string {
	var missing []string
	for _, c := range checks {
		missing = append(missing, MissingTargets(c.Analyzer.Name, c.Targets, out.Targets[c.Analyzer.Name])...)
	}
	return missing
}

// The ported analyzers run over the whole module under every context. Test
// files are analyzed in every context, so the windows and darwin source sets
// are no longer production-only here.
func TestVetDriverRepo(t *testing.T) {
	bin := buildVetTool(t)
	for _, ctx := range Contexts {
		t.Run(ctx.Name, func(t *testing.T) {
			out := runVet(t, bin, ctx, nil, []string{Module + "/..."})
			deferred := map[string]int{}
			for _, finding := range out.Findings {
				if out.Deferred[finding] {
					deferred[deferredAnalyzer(finding)]++
					continue
				}
				t.Error(finding)
			}
			for analyzer, n := range deferred {
				t.Logf("deferred in-package test sites, %s: %d", analyzer, n)
			}
			for _, finding := range missingTargets(out, Checks()) {
				t.Error(finding)
			}
			pins := pinSet(t, out.Summaries[RedactPins.Name])
			for _, finding := range compareRedactionPins(pins, SensitiveTypes) {
				t.Error(finding)
			}
		})
	}
}

func deferredAnalyzer(finding string) string {
	for name := range strictDeferred {
		if strings.Contains(finding, ": "+name+": ") {
			return name
		}
	}
	return "?"
}

// vetFixture is one negative fixture: the analyzer it exercises, its flags, and
// the old harness entry point it must stay in parity with.
type vetFixture struct {
	name     string
	analyzer string
	patterns []string
	flags    []string
	targets  []string
	old      func(pkgs []*packages.Package) []string
}
