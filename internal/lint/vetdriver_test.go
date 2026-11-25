package lint

// The vet driver: the analyzers built into one go vet tool and run once per
// analysis context (lint-analysis driver ADR, decision 1).

import (
	"bytes"
	_ "embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

// strictMembers makes an unknown member a decode error, as v1's
// DisallowUnknownFields did.
var strictMembers = json.RejectUnknownMembers(true)

// vetResult is one analyzer's output for one package: diagnostics, or the
// analyzer's own error.
type vetResult struct {
	Diagnostics []vetDiagnostic
	Err         string
}

func (r *vetResult) UnmarshalJSON(raw []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		return json.Unmarshal(raw, &r.Diagnostics, strictMembers)
	}
	var failure struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &failure, strictMembers); err != nil {
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
	// Targets is analyzer -> package paths its passes reported as analyzed.
	Targets   map[string]map[string]bool
	Summaries map[string][]Summary
	// Context is the analysis context the run used and Flags the analyzer flags
	// the run passed, so an aggregate judges what the passes saw.
	Context string
	Flags   []string
}

// decodeVetOutput parses vet's concatenated JSON trees. go prefixes a package's
// output with a "# pkg" header line, and warns "ignoring symlink" for each
// symlinked directory under the pattern (such as pnpm's node_modules); anything
// else that is not JSON is a tool failure and fails the decode.
func decodeVetOutput(raw []byte) (*vetOutcome, error) {
	var kept []byte
	for line := range bytes.Lines(raw) {
		if bytes.HasPrefix(line, []byte("# ")) || bytes.HasPrefix(line, []byte("warning: ignoring symlink ")) {
			continue
		}
		kept = append(kept, line...)
	}
	out := &vetOutcome{Targets: map[string]map[string]bool{}, Summaries: map[string][]Summary{}}
	dec := jsontext.NewDecoder(bytes.NewReader(kept))
	for {
		var tree vetTree
		if err := json.UnmarshalDecode(dec, &tree, strictMembers); errors.Is(err, io.EOF) {
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
						finding := formatFinding(d.Posn, analyzer, d.Message)
						out.Findings = append(out.Findings, finding)
						continue
					}
					summary, err := parseSummary(d.Message)
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
	return runVetArgs(t, bin, ctx, nil, flags, patterns)
}

// runRepoVet vets with the digests of the declared inputs as they are now.
// go vet caches a -json run's output even when it carries a refusal, so an
// input edited while a run read it would replay that digest-mismatch refusal
// after the edit is undone. If the inputs still hash to the digests passed,
// such a refusal is stale: one rerun with -a re-vets and replaces the cached
// entry. Inputs that changed during the run fail instead of retrying.
func runRepoVet(t *testing.T, bin string, ctx BuildContext, flags func() []string, patterns []string) *vetOutcome {
	t.Helper()
	passed := flags()
	out := runVet(t, bin, ctx, passed, patterns)
	if !hasDigestRefusal(out.Findings) {
		return out
	}
	if !slices.Equal(flags(), passed) {
		t.Fatalf("declared inputs changed during the vet run under %s; rerun the test", ctx.Name)
	}
	return runVetArgs(t, bin, ctx, []string{"-a"}, passed, patterns)
}

func hasDigestRefusal(findings []string) bool {
	return slices.ContainsFunc(findings, func(f string) bool { return strings.Contains(f, "inputs refused: "+ErrInputDigest.Error()) })
}

func runVetArgs(t *testing.T, bin string, ctx BuildContext, goFlags, flags, patterns []string) *vetOutcome {
	t.Helper()
	args := append([]string{"vet", "-vettool=" + bin, "-json", "-p", "2"}, goFlags...)
	args = append(args, ctx.BuildFlags...)
	overlay, err := ctx.WriteOverlay(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if overlay != "" {
		args = append(args, "-overlay="+overlay)
	}
	flags = append(slices.Clone(flags), "-rawsql.context="+ctx.Name)
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
	outcome.Context, outcome.Flags = ctx.Name, flags
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
		missing = append(missing, targetFindings(c.Analyzer.Name, c.Targets, out.Targets[c.Analyzer.Name])...)
	}
	return missing
}

// The ported analyzers run over the whole module under every context. Test
// files are analyzed in every context, so the windows and darwin source sets
// are no longer production-only here.
func TestVetDriverRepo(t *testing.T) {
	ran := 0
	analyzed := map[string]bool{}
	exceptionSeen := false
	bin := buildVetTool(t)
	for _, ctx := range Contexts {
		t.Run(ctx.Name, func(t *testing.T) {
			out := runRepoVet(t, bin, ctx, func() []string { return repoInputFlags(t) }, []string{Module + "/..."})
			for _, finding := range out.Findings {
				t.Error(finding)
			}
			ran++
			for _, finding := range missingTargets(out, Checks()) {
				t.Error(finding)
			}
			aggregated, err := rawSQLAggregate(out, moduleRoot())
			if err != nil {
				t.Fatal(err)
			}
			for _, finding := range aggregated {
				t.Error(finding)
			}
			files, exception := analyzedGoFiles(out)
			maps.Copy(analyzed, files)
			exceptionSeen = exceptionSeen || exception
			pins := pinSet(t, out.Summaries[RedactPins.Name])
			for _, finding := range compareRedactionPins(pins, SensitiveTypes) {
				t.Error(finding)
			}
		})
	}
	// Go files no context's packages contain are judged here, in the test process.
	if ran == len(Contexts) {
		for _, finding := range checkUnanalyzedGoFiles(moduleRoot(), analyzed, exceptionSeen) {
			t.Error(finding)
		}
	} else {
		t.Logf("ran %d of %d contexts: the unanalyzed Go file check needs every context", ran, len(Contexts))
	}
}

// TestVetDriverChangedPackages is the fast-feedback form of TestVetDriverRepo: the
// analyzers' per-package findings in the default context over only the packages named in
// HIKYO_VET_PACKAGES (space separated ./paths). The whole-module parts (target presence, the
// raw SQL aggregate, unanalyzed files, redaction pins) stay in TestVetDriverRepo.
func TestVetDriverChangedPackages(t *testing.T) {
	patterns := strings.Fields(os.Getenv("HIKYO_VET_PACKAGES"))
	if len(patterns) == 0 {
		t.Skip("HIKYO_VET_PACKAGES names no package")
	}
	bin := buildVetTool(t)
	out := runRepoVet(t, bin, Contexts[0], func() []string { return repoInputFlags(t) }, patterns)
	for _, finding := range out.Findings {
		t.Error(finding)
	}
}

// vetFixture is one negative fixture: the analyzer it exercises and its flags.
type vetFixture struct {
	name     string
	analyzer string
	patterns []string
	flags    []string
	targets  []string
	// inputsRoot is the fixture's repository root, relative to the module root,
	// for an analyzer that reads declared inputs; the driver adds its -root and
	// -inputs flags.
	inputsRoot string
	// aggregate returns the findings the test process adds to the vet run's
	// (see checkUnanalyzedGoFiles).
	aggregate func(out *vetOutcome) []string
	// wantAggregate, when set, is the exact aggregate the fixture must produce:
	// findings with no source line to carry a want comment.
	wantAggregate func(context string) []string
}
