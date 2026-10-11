package ci_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A failed benchmark must fail the already-required core job, while reusing
// its immutable checkout and compiled dependencies instead of another runner.
func TestBenchmarkSmokeUsesRequiredCoreValidation(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var ci mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &ci); err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Jobs map[string]struct {
			ContinueOnError bool `yaml:"continue-on-error"`
			Steps           []struct {
				Name            string `yaml:"name"`
				ContinueOnError bool   `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	core := ci.Jobs["test_core"]
	if !reflect.DeepEqual(workflowPlanJobs(core.If), []string{"test"}) || policy.Jobs["test_core"].ContinueOnError {
		t.Fatal("benchmark smoke must retain the blocking core test plan")
	}
	needs, err := workflowNeeds(ci.Jobs["ci-required"].Needs)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(needs, "test_core") {
		t.Fatal("aggregate must require core validation")
	}
	var smoke mainValidationStep
	compiled := false
	for i, step := range core.Steps {
		if step.Name == "Test all packages except the sharded isolation suite" {
			compiled = true
		}
		if step.Name == "Exercise every benchmark once" {
			if !compiled || step.If != "" || policy.Jobs["test_core"].Steps[i].ContinueOnError {
				t.Fatal("benchmark smoke must run after core tests and propagate failures")
			}
			smoke = step
		}
	}
	if smoke.Run == "" {
		t.Fatal("missing complete benchmark smoke")
	}
	if len(core.Steps) < 2 || core.Steps[0].Name != "Validate the immutable caller revision" || core.Steps[1].With["ref"] != "${{ inputs.revision }}" || core.Steps[1].With["persist-credentials"] != "false" {
		t.Fatal("benchmark smoke must use the guarded immutable read-only checkout")
	}
	if _, err := os.Stat(filepath.Join(root, ".github/workflows/benchmark-smoke.yml")); !os.IsNotExist(err) {
		t.Fatal("standalone benchmark runner must be removed")
	}
	work := t.TempDir()
	capture := filepath.Join(work, "arguments")
	if err := os.WriteFile(filepath.Join(work, "go"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$BENCHMARK_CAPTURE\"\nexit \"$BENCHMARK_EXIT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []string{"test", "-run", "^$", "-bench", ".", "-benchtime=1x", "./internal/crypto/", "./internal/dotenv/", "./internal/scanning/", "./internal/server/", "./internal/service/"}
	for _, status := range []string{"0", "17"} {
		cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", smoke.Run)
		cmd.Env = append(os.Environ(), "PATH="+work+":"+os.Getenv("PATH"), "BENCHMARK_CAPTURE="+capture, "BENCHMARK_EXIT="+status)
		if out, err := cmd.CombinedOutput(); (err == nil) != (status == "0") {
			t.Fatalf("benchmark exit %s was not preserved: %v %s", status, err, out)
		}
		args, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Split(strings.TrimSpace(string(args)), "\n"); !reflect.DeepEqual(got, want) {
			t.Fatalf("benchmark coverage changed: got %v, want %v", got, want)
		}
	}
	for _, source := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40), "main", ""} {
		cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", core.Steps[0].Run)
		cmd.Env = append(os.Environ(), "GITHUB_EVENT_NAME=pull_request", "GITHUB_REPOSITORY=Hikyo-Org/Hikyo", "SOURCE_REVISION="+source,
			"EVENT_REPOSITORY=Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID=10", "PR_NUMBER=882", "PR_HEAD_SHA="+strings.Repeat("a", 40),
			"PR_HEAD_REPOSITORY=fork/Hikyo", "PR_HEAD_REPOSITORY_ID=11", "PR_BASE_REPOSITORY=Hikyo-Org/Hikyo", "PR_BASE_REPOSITORY_ID=10")
		if out, err := cmd.CombinedOutput(); (err == nil) != (source == strings.Repeat("a", 40)) {
			t.Fatalf("benchmark source %q acceptance incorrect: %v %s", source, err, out)
		}
	}
	// Exercise the real aggregate checker with this job's benchmark failure.
	registryData, err := os.ReadFile(filepath.Join(root, "scripts/ci/ci-job-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rules registry
	if err := json.Unmarshal(registryData, &rules); err != nil {
		t.Fatal(err)
	}
	plan := make(map[string]bool)
	for _, key := range rules.PathClasses["full"] {
		plan[key] = true
	}
	results := make(map[string]map[string]string)
	for name, rule := range rules.Jobs {
		if directGate(rule.RequiredGate) {
			results[name] = map[string]string{"result": "success"}
		}
	}
	results["test_core"]["result"] = "failure"
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	resultsJSON, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(root, "scripts/ci/check-required-jobs.sh"), "pull_request", string(resultsJSON), string(planJSON))
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "test_core: expected success, got failure") {
		t.Fatalf("aggregate did not refuse benchmark failure: %v %s", err, out)
	}
}
