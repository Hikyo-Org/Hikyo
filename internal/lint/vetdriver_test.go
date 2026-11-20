package lint

// The vet driver: the analyzers built into one go vet tool and run once per
// analysis context (lint-analysis driver ADR, decision 1). The old go/packages
// harness keeps running for the same analyzers until TestVetDriverParity shows
// every old finding is also reported here.

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// vetDiagnostic is one entry of `go vet -json`, mirroring the JSON shapes of
// golang.org/x/tools/internal/analysis/driverutil; unknown fields fail the
// decode.
type vetDiagnostic struct {
	Category       string            `json:"category"`
	Posn           string            `json:"posn"`
	End            string            `json:"end"`
	Message        string            `json:"message"`
	Related        []vetRelated      `json:"related"`
	SuggestedFixes []vetSuggestedFix `json:"suggested_fixes"`
}

type vetRelated struct {
	Posn    string `json:"posn"`
	End     string `json:"end"`
	Message string `json:"message"`
}

type vetSuggestedFix struct {
	Message string        `json:"message"`
	Edits   []vetTextEdit `json:"edits"`
}

type vetTextEdit struct {
	Filename string `json:"filename"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	New      string `json:"new"`
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
	// Deferred maps the findings the analyzer marked deferredCategory (see
	// strictDeferred) to their analyzer; they stay in Findings for parity.
	Deferred map[string]string
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
	out := &vetOutcome{Deferred: map[string]string{}, Targets: map[string]map[string]bool{}, Summaries: map[string][]Summary{}}
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
						if d.Category == deferredCategory {
							out.Deferred[finding] = analyzer
						}
						continue
					}
					summary, err := ParseSummary(d.Message)
					if err == nil {
						err = summary.validate(analyzer)
					}
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

// buildVetTool builds the vet tool into the test's temp dir.
func buildVetTool(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lintvet")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", bin, Module+"/internal/lint/cmd/lintvet")
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
	cmd := exec.CommandContext(t.Context(), "go", args...)
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

// deferredSites is the checked-in ceiling of deferred in-package test-site
// diagnostics (see strictDeferred), so the sites can only be paid down.
//
//go:embed testdata/deferred_sites.json
var deferredSitesJSON []byte

var updateDeferredSites = flag.Bool("update-deferred-sites", false, "rewrite testdata/deferred_sites.json from the observed deferred counts")

const deferredSitesFile = "internal/lint/testdata/deferred_sites.json"

// deferredCounts is context name -> analyzer -> repo-relative file -> count.
type deferredCounts struct {
	Contexts map[string]map[string]map[string]int `json:"contexts"`
}

func decodeDeferredCounts(raw []byte) (deferredCounts, error) {
	var counts deferredCounts
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&counts); err != nil {
		return deferredCounts{}, fmt.Errorf("decode %s: %w", deferredSitesFile, err)
	}
	if dec.More() {
		return deferredCounts{}, fmt.Errorf("decode %s: trailing data", deferredSitesFile)
	}
	return counts, nil
}

// observedDeferred counts a run's deferred findings per analyzer and file.
func observedDeferred(out *vetOutcome) (map[string]map[string]int, error) {
	counts := map[string]map[string]int{}
	for _, finding := range out.Findings {
		analyzer, ok := out.Deferred[finding]
		if !ok {
			continue
		}
		cut := strings.Index(finding, ": "+analyzer+": ")
		if cut < 0 {
			return nil, fmt.Errorf("deferred finding %q has no analyzer", finding)
		}
		file, _, err := splitPosn(finding[:cut])
		if err != nil {
			return nil, err
		}
		if rel, err := filepath.Rel(moduleRoot(), file); err == nil {
			file = rel
		}
		if counts[analyzer] == nil {
			counts[analyzer] = map[string]int{}
		}
		counts[analyzer][filepath.ToSlash(file)]++
	}
	return counts, nil
}

// checkDeferredCeiling compares observed counts with the pinned ceiling: a file
// absent from the pin or above its ceiling fails, and a count below its ceiling
// fails with an instruction to lower the pin, so the pin only ratchets down.
func checkDeferredCeiling(context string, pinned, observed map[string]map[string]int) []string {
	var findings []string
	for _, analyzer := range slices.Sorted(maps.Keys(observed)) {
		for _, file := range slices.Sorted(maps.Keys(observed[analyzer])) {
			got := observed[analyzer][file]
			ceiling, ok := pinned[analyzer][file]
			switch {
			case !ok:
				findings = append(findings, fmt.Sprintf("%s: %s: %s gained %d deferred diagnostics and is not in the pin; fix them", context, analyzer, file, got))
			case got > ceiling:
				findings = append(findings, fmt.Sprintf("%s: %s: %s has %d deferred diagnostics, above its pinned ceiling %d; fix them", context, analyzer, file, got, ceiling))
			}
		}
	}
	for _, analyzer := range slices.Sorted(maps.Keys(pinned)) {
		for _, file := range slices.Sorted(maps.Keys(pinned[analyzer])) {
			ceiling := pinned[analyzer][file]
			if got := observed[analyzer][file]; got < ceiling {
				findings = append(findings, fmt.Sprintf("%s: %s: %s has %d deferred diagnostics, below its pinned ceiling %d; lower the pin in %s (go test ./internal/lint -run TestVetDriverRepo -update-deferred-sites)", context, analyzer, file, got, ceiling, deferredSitesFile))
			}
		}
	}
	return findings
}

// raisedDeferred reports what an update would add or raise. The update only
// lowers or removes pins; adding a file or raising a ceiling is a hand edit
// that review sees.
func raisedDeferred(context string, pinned, observed map[string]map[string]int) []string {
	var findings []string
	for _, analyzer := range slices.Sorted(maps.Keys(observed)) {
		for _, file := range slices.Sorted(maps.Keys(observed[analyzer])) {
			if ceiling, ok := pinned[analyzer][file]; !ok || observed[analyzer][file] > ceiling {
				findings = append(findings, fmt.Sprintf("%s: %s: %s has %d deferred diagnostics, above its pin (%d, 0 when absent); -update-deferred-sites only lowers pins, so edit %s by hand or fix the sites", context, analyzer, file, observed[analyzer][file], ceiling, deferredSitesFile))
			}
		}
	}
	return findings
}

// The ported analyzers run over the whole module under every context. Test
// files are analyzed in every context, so the windows and darwin source sets
// are no longer production-only here.
func TestVetDriverRepo(t *testing.T) {
	pinned, err := decodeDeferredCounts(deferredSitesJSON)
	if err != nil {
		t.Fatal(err)
	}
	observed := deferredCounts{Contexts: map[string]map[string]map[string]int{}}
	ran := 0
	bin := buildVetTool(t)
	for _, ctx := range Contexts {
		t.Run(ctx.Name, func(t *testing.T) {
			out := runVet(t, bin, ctx, nil, []string{Module + "/..."})
			for _, finding := range out.Findings {
				if _, deferred := out.Deferred[finding]; !deferred {
					t.Error(finding)
				}
			}
			counts, err := observedDeferred(out)
			if err != nil {
				t.Fatal(err)
			}
			observed.Contexts[ctx.Name] = counts
			ran++
			findings := checkDeferredCeiling(ctx.Name, pinned.Contexts[ctx.Name], counts)
			if *updateDeferredSites {
				findings = raisedDeferred(ctx.Name, pinned.Contexts[ctx.Name], counts)
			}
			for _, finding := range findings {
				t.Error(finding)
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
	if *updateDeferredSites && ran != len(Contexts) {
		t.Fatalf("-update-deferred-sites ran %d of %d contexts; run it without -run filters so no context's pin is dropped", ran, len(Contexts))
	}
	if *updateDeferredSites && !t.Failed() {
		raw, err := json.MarshalIndent(observed, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(moduleRoot(), filepath.FromSlash(deferredSitesFile)), append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
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
