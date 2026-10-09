package ci_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type raceIsolationStep struct {
	Name  string `yaml:"name"`
	Shell string `yaml:"shell"`
	Run   string `yaml:"run"`
}

func raceIsolationRunStep(t *testing.T, job, name string) raceIsolationStep {
	t.Helper()
	raw, err := os.ReadFile("../../.github/workflows/race-isolation.yml")
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
	script := filepath.Join(dir, "step.sh")
	if err := os.WriteFile(script, []byte(step.Run), 0600); err != nil {
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
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestRaceIsolationWorkflowPreservesTestFailure(t *testing.T) {
	step := raceIsolationRunStep(t, "race-isolation", "Race detector (probe-based isolation E2E suite)")
	for _, status := range []string{"0", "37"} {
		t.Run(status, func(t *testing.T) {
			dir := t.TempDir()
			stub := `#!/usr/bin/env bash
printf '%s\n' '{"Action":"output","Output":"race fixture output\n"}'
exit "$FIXTURE_GO_STATUS"
`
			if err := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0700); err != nil {
				t.Fatal(err)
			}
			output, err := executeRaceIsolationStep(t, step, dir,
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"RUNNER_TEMP="+dir, "ISOLATION_TARGETS=[\"TestAlpha\"]", "FIXTURE_GO_STATUS="+status)
			if !strings.Contains(output, "race fixture output") {
				t.Fatalf("readable output lost: %s", output)
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
}

func TestRaceIsolationWorkflowPreservesPlannerFailure(t *testing.T) {
	step := raceIsolationRunStep(t, "plan", "Plan every race isolation target exactly once")
	for _, fail := range []string{"false", "true"} {
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
if [ "$shard" = 0 ]; then printf '%s\n' TestAlpha; fi
if [ "$shard" = 7 ] && [ "$FIXTURE_PLANNER_FAIL" = true ]; then exit 29; fi
exit 0
`
			if err := os.WriteFile(filepath.Join(plannerDir, "analysis-shards"), []byte(stub), 0700); err != nil {
				t.Fatal(err)
			}
			outputFile := filepath.Join(dir, "github-output")
			output, err := executeRaceIsolationStep(t, step, dir,
				"GITHUB_OUTPUT="+outputFile, "FIXTURE_PLANNER_FAIL="+fail)
			if fail == "false" {
				if err != nil {
					t.Fatalf("successful planner failed: %v: %s", err, output)
				}
				if _, err := os.Stat(outputFile); err != nil {
					t.Fatal("successful planner did not publish matrix:", err)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 29 {
					t.Fatalf("planner failure was masked: error=%v output=%s", err, output)
				}
				if _, err := os.Stat(outputFile); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed planner published a matrix")
				}
			}
		})
	}
}
