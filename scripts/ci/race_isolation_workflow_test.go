package ci_test

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type raceIsolationStep struct {
	Name  string            `yaml:"name"`
	Shell string            `yaml:"shell"`
	Run   string            `yaml:"run"`
	Env   map[string]string `yaml:"env"`
}

func raceIsolationRunStep(t *testing.T, job, name string) raceIsolationStep {
	t.Helper()
	raw, err := os.ReadFile("../../.github/workflows/reliability.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []raceIsolationStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs[job].Steps {
		if step.Name == name {
			return step
		}
	}
	t.Fatalf("missing %s step %q", job, name)
	return raceIsolationStep{}
}

// Execute the checked-in run body with GitHub's shell behavior. An unspecified
// Linux shell uses bash -e; explicit bash additionally enables pipefail.
func executeRaceIsolationStep(t *testing.T, step raceIsolationStep, dir string, env ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatal("race workflow fixture requires jq:", err)
	}
	run := step.Run
	if step.Name != "Plan every race isolation target exactly once" {
		if strings.TrimSpace(run) != "./scripts/ci/test-isolation-race.sh" {
			t.Fatal("both race jobs must use the shared test runner")
		}
		raw, err := os.ReadFile("test-isolation-race.sh")
		if err != nil {
			t.Fatal(err)
		}
		run = string(raw)
	}
	script := filepath.Join(dir, "step.sh")
	if err := os.WriteFile(script, []byte(run), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--noprofile", "--norc", "-e"}
	if step.Shell == "bash" {
		args = append(args, "-o", "pipefail")
	} else if step.Shell != "" {
		t.Fatalf("unsupported fixture shell %q", step.Shell)
	}
	args = append(args, script)
	cmd := exec.Command("bash", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if value, ok := step.Env["GOMAXPROCS"]; ok {
		cmd.Env = append(cmd.Env, "GOMAXPROCS="+value)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestRaceIsolationWorkflowPreservesTestFailure(t *testing.T) {
	for _, job := range []struct{ id, step string }{
		{"race-isolation", "Race detector (probe-based isolation E2E suite)"},
		{"race-analyzers", "Race detector (static analyzer invariants)"},
	} {
		t.Run(job.id, func(t *testing.T) {
			step := raceIsolationRunStep(t, job.id, job.step)
			wantProcs := "2"
			if job.id == "race-analyzers" {
				wantProcs = "1"
				if step.Env["GOMAXPROCS"] != "1" {
					t.Fatal("analyzer job must set GOMAXPROCS=1 before Go starts")
				}
			} else if _, exists := step.Env["GOMAXPROCS"]; exists {
				t.Fatal("runtime shards must preserve their existing concurrency")
			}
			for _, status := range []string{"0", "37"} {
				t.Run(status, func(t *testing.T) {
					dir := t.TempDir()
					stub := `#!/usr/bin/env bash
printf '%s\n' "$GOMAXPROCS" >"$RUNNER_TEMP/maxprocs"
printf '%s\n' "$*" >"$RUNNER_TEMP/go-args"
if [ "$FIXTURE_GO_STATUS" = 0 ]; then
  printf '%s\n' '{"Action":"output","Output":"race fixture output\n"}'
else
  printf '%s\n' '{"Action":"build-output","ImportPath":"example/isolation","Output":"race fixture compiler error\n"}'
  printf '%s\n' '{"Action":"build-fail","ImportPath":"example/isolation"}'
fi
exit "$FIXTURE_GO_STATUS"
`
					if err := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0700); err != nil {
						t.Fatal(err)
					}
					output, err := executeRaceIsolationStep(t, step, dir,
						"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
						"GOMAXPROCS=2", "RUNNER_TEMP="+dir, "ISOLATION_TARGETS=[\"TestAlpha\"]", "FIXTURE_GO_STATUS="+status)
					procs, readErr := os.ReadFile(filepath.Join(dir, "maxprocs"))
					if readErr != nil || strings.TrimSpace(string(procs)) != wantProcs {
						t.Fatalf("Go received wrong concurrency: %q, %v", procs, readErr)
					}
					args, readErr := os.ReadFile(filepath.Join(dir, "go-args"))
					if readErr != nil || !strings.Contains(string(args), " -race -count=1 ") || !strings.Contains(string(args), "-run ^(TestAlpha)$") {
						t.Fatalf("race coverage or exact target filter lost: %q, %v", args, readErr)
					}
					message := "race fixture output"
					if status != "0" {
						message = "race fixture compiler error"
					}
					if !strings.Contains(output, message) {
						t.Fatalf("readable %s lost: %s", message, output)
					}
					if status == "0" && err != nil {
						t.Fatalf("successful Go execution failed: %v: %s", err, output)
					}
					if status != "0" {
						var exit *exec.ExitError
						if !errors.As(err, &exit) || exit.ExitCode() != 37 {
							t.Fatalf("Go failure was masked: error=%v output=%s", err, output)
						}
					}
					data, err := os.ReadFile(filepath.Join(dir, "isolation-race.wall.json"))
					if err != nil {
						t.Fatal(err)
					}
					var timing struct{ ExitCode int }
					if err := json.Unmarshal(data, &timing); err != nil {
						t.Fatal(err)
					}
					if status == "37" && timing.ExitCode != 37 {
						t.Fatalf("failure timing reports %d, want 37", timing.ExitCode)
					}
				})
			}
		})
	}
}

func TestRaceIsolationWorkflowPreservesPlannerFailure(t *testing.T) {
	step := raceIsolationRunStep(t, "plan", "Plan every race isolation target exactly once")
	analyzers := raceIsolationAnalyzerTests(t)
	for _, fail := range []string{"false", "true", "missing", "duplicate"} {
		t.Run(fail, func(t *testing.T) {
			dir := t.TempDir()
			plannerDir := filepath.Join(dir, "scripts", "ci")
			if err := os.MkdirAll(plannerDir, 0700); err != nil {
				t.Fatal(err)
			}
			// A failed empty shard would otherwise pass the coverage comparison,
			// because shard zero and the reference inventory both contain Alpha.
			stub := `#!/usr/bin/env bash
shard=
while [ "$#" -gt 0 ]; do
  if [ "$1" = --shard ]; then shard=$2; fi
  shift
done
if [ "$shard" = 0 ]; then
  printf '%s\n' TestAlpha FuzzSeeds Example_output
  if [ "$FIXTURE_PLANNER_FAIL" != missing ]; then printf '%s\n' "$FIXTURE_ANALYZERS"; fi
  if [ "$FIXTURE_PLANNER_FAIL" = duplicate ]; then printf '%s\n' TestAlpha; fi
fi
if [ "$shard" = 7 ] && [ "$FIXTURE_PLANNER_FAIL" = true ]; then exit 29; fi
exit 0
`
			if err := os.WriteFile(filepath.Join(plannerDir, "analysis-shards"), []byte(stub), 0700); err != nil {
				t.Fatal(err)
			}
			outputFile := filepath.Join(dir, "github-output")
			output, err := executeRaceIsolationStep(t, step, dir,
				"GITHUB_OUTPUT="+outputFile, "FIXTURE_PLANNER_FAIL="+fail, "FIXTURE_ANALYZERS="+strings.Join(analyzers, "\n"))
			if fail == "false" {
				if err != nil {
					t.Fatalf("successful planner failed: %v: %s", err, output)
				}
				published, err := os.ReadFile(outputFile)
				if err != nil {
					t.Fatal(err)
				}
				outputs := map[string]string{}
				for _, line := range strings.Split(strings.TrimSpace(string(published)), "\n") {
					key, value, ok := strings.Cut(line, "=")
					if !ok {
						t.Fatalf("malformed output %q", line)
					}
					outputs[key] = value
				}
				var selected []string
				if err := json.Unmarshal([]byte(outputs["analyzers"]), &selected); err != nil {
					t.Fatal("analyzer plan missing:", err)
				}
				sort.Strings(selected)
				if strings.Join(selected, "\n") != strings.Join(analyzers, "\n") {
					t.Fatalf("serial group must match every LoadRepo caller: got %v, want %v", selected, analyzers)
				}
				var matrix struct {
					Include []struct {
						Targets []string `json:"targets"`
					} `json:"include"`
				}
				if err := json.Unmarshal([]byte(outputs["matrix"]), &matrix); err != nil {
					t.Fatal(err)
				}
				var runtime []string
				for _, shard := range matrix.Include {
					runtime = append(runtime, shard.Targets...)
				}
				sort.Strings(runtime)
				if strings.Join(runtime, "\n") != "Example_output\nFuzzSeeds\nTestAlpha" {
					t.Fatalf("runtime targets lost, duplicated, or contain analyzers: %v", runtime)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || (fail == "true" && exit.ExitCode() != 29) {
					t.Fatalf("planner failure was masked: error=%v output=%s", err, output)
				}
				if _, err := os.Stat(outputFile); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed planner published a matrix")
				}
			}
		})
	}
}

// Keep the serial group tied to source: a new repository-loading invariant must
// be assigned to the analyzer job rather than quietly reintroducing this race.
func raceIsolationAnalyzerTests(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../internal/isolation/*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		lintName := ""
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == "github.com/Hikyo-Org/hikyo/internal/lint" {
				lintName = "lint"
				if spec.Name != nil {
					lintName = spec.Name.Name
				}
				if lintName == "." {
					t.Fatal("lint imports must be qualified for analyzer discovery")
				}
			}
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			loadsRepo := false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := selector.X.(*ast.Ident)
				if ok && pkg.Name == lintName && (selector.Sel.Name == "LoadRepo" || selector.Sel.Name == "LoadRepoIn") {
					loadsRepo = true
				}
				return true
			})
			if loadsRepo {
				names = append(names, fn.Name.Name)
			}
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no repository-loading invariants found")
	}
	return names
}

func TestRaceIsolationWorkflowRejectsInvalidTargets(t *testing.T) {
	step := raceIsolationRunStep(t, "race-isolation", "Race detector (probe-based isolation E2E suite)")
	for _, targets := range []string{"[]", "null", "{}", `["TestAlpha|TestBeta"]`, `["TestAlpha",2]`} {
		t.Run(targets, func(t *testing.T) {
			dir := t.TempDir()
			stub := "#!/bin/sh\ntouch \"$RUNNER_TEMP/go-started\"\n"
			if err := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0700); err != nil {
				t.Fatal(err)
			}
			output, err := executeRaceIsolationStep(t, step, dir,
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"RUNNER_TEMP="+dir, "ISOLATION_TARGETS="+targets)
			if err == nil {
				t.Fatalf("invalid target list accepted: %s", output)
			}
			if _, err := os.Stat(filepath.Join(dir, "go-started")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid inventory reached Go")
			}
		})
	}
}

func TestRaceIsolationWorkflowAnalyzerJobWiring(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/reliability.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Needs   string              `yaml:"needs"`
			Outputs map[string]string   `yaml:"outputs"`
			Steps   []raceIsolationStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.Jobs["plan"].Outputs["analyzers"] != "${{ steps.plan.outputs.analyzers }}" {
		t.Fatal("planner must expose the analyzer group")
	}
	if workflow.Jobs["race-analyzers"].Needs != "plan" {
		t.Fatal("analyzers must run alongside runtime shards after planning")
	}
	step := raceIsolationRunStep(t, "race-analyzers", "Race detector (static analyzer invariants)")
	if step.Env["ISOLATION_TARGETS"] != "${{ needs.plan.outputs.analyzers }}" {
		t.Fatal("analyzers must execute the exact validated group")
	}
	for _, step := range workflow.Jobs["race-analyzers"].Steps {
		if strings.Contains(step.Run, "postgres") || strings.Contains(step.Name, "PostgreSQL") {
			t.Fatal("static analyzers do not need the runtime database fixture")
		}
	}
}
