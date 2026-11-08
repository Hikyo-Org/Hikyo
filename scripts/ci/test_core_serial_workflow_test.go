package ci_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// test_core_serial runs internal/app and internal/lint on a runner of its own
// so they stop extending test_core's serial tail. Validate the entire job
// against a reviewed fixture before excluding this additive execution from the
// frozen inventory: test_core itself is unchanged, because the pool is the
// script's default mode. Call after the shared Go cache disk step is normalized.
func normalizeTestCoreSerial(workflow map[string]map[string]any) error {
	const jobName = "test_core_serial"
	job, exists := workflow[jobName]
	if !exists {
		return fmt.Errorf("%s job is missing", jobName)
	}
	jobSteps, ok := job["steps"].([]any)
	guardSteps, guardOK := workflow["changes"]["steps"].([]any)
	if !ok || !guardOK || len(jobSteps) == 0 || len(guardSteps) == 0 || !reflect.DeepEqual(jobSteps[0], guardSteps[0]) {
		return fmt.Errorf("%s lost the immutable caller guard", jobName)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "test-core-serial.yml"))
	if err != nil {
		return err
	}
	var expected map[string]any
	if err := yaml.Unmarshal(fixture, &expected); err != nil {
		return err
	}
	actual := make(map[string]any, len(job))
	for key, value := range job {
		actual[key] = value
	}
	actual["steps"] = jobSteps[1:]
	if !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("%s lost its complete fail-closed configuration", jobName)
	}
	needs, ok := workflow["ci-required"]["needs"].([]any)
	if !ok {
		return fmt.Errorf("%s lost required aggregate dependencies", jobName)
	}
	kept := make([]any, 0, len(needs))
	found := 0
	for _, need := range needs {
		if need == jobName {
			found++
		} else {
			kept = append(kept, need)
		}
	}
	if found != 1 {
		return fmt.Errorf("%s must gate ci-required exactly once", jobName)
	}
	workflow["ci-required"]["needs"] = kept
	delete(workflow, jobName)
	return nil
}

// Merge groups run the protected base's checker and registry against the
// candidate's ci.yml. A base that predates test_core_serial must still accept a
// candidate whose serial job really ran, and must never accept one whose job
// failed, was cancelled, was skipped while planned, or is missing.
func TestTestCoreSerialTrustedBaseMigration(t *testing.T) {
	root := repositoryRoot(t)
	var ci mainValidationWorkflow
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &ci); err != nil {
		t.Fatal(err)
	}
	gate := ""
	for _, step := range ci.Jobs["ci-required"].Steps {
		if step.Name == "Require every validation job" {
			gate = step.Run
		}
	}
	if gate == "" {
		t.Fatal("missing executable required-job gate")
	}
	checker, err := os.ReadFile(filepath.Join(root, "scripts/ci/check-required-jobs.sh"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(root, "scripts/ci/ci-job-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var current registry
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	if rule := current.Jobs["test_core_serial"]; rule.RequiredGate != "planned" || !reflect.DeepEqual(rule.PlanJobs, []string{"test"}) {
		t.Fatalf("test_core_serial must be directly required by the test plan, got %+v", rule)
	}
	for _, baseKind := range []string{"current", "predates-serial"} {
		for _, scenario := range []string{"full-success", "failure", "cancelled", "skipped", "missing"} {
			t.Run(baseKind+"/"+scenario, func(t *testing.T) {
				base := cloneRegistry(t, current)
				if baseKind == "predates-serial" {
					delete(base.Jobs, "test_core_serial")
				}
				plan := make(map[string]bool)
				for _, key := range current.PathClasses["full"] {
					plan[key] = true
				}
				results := make(map[string]map[string]string)
				for name, rule := range current.Jobs {
					if !directGate(rule.RequiredGate) {
						continue
					}
					result := "success"
					if rule.RequiredGate == "planned" {
						result = "skipped"
						for _, key := range rule.PlanJobs {
							if plan[key] {
								result = "success"
							}
						}
					}
					results[name] = map[string]string{"result": result}
				}
				switch scenario {
				case "failure", "cancelled", "skipped":
					results["test_core_serial"]["result"] = scenario
				case "missing":
					delete(results, "test_core_serial")
				}
				encode := func(value any) []byte {
					t.Helper()
					data, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					return data
				}
				work := t.TempDir()
				scriptDir := filepath.Join(work, "scripts", "ci")
				if err := os.MkdirAll(scriptDir, 0o755); err != nil {
					t.Fatal(err)
				}
				write := func(name string, data []byte) {
					t.Helper()
					if err := os.WriteFile(filepath.Join(scriptDir, name), data, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				git := func(args ...string) string {
					t.Helper()
					cmd := exec.Command("git", args...)
					cmd.Dir = work
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("git %v: %v: %s", args, err, out)
					}
					return strings.TrimSpace(string(out))
				}
				write("check-required-jobs.sh", checker)
				write("ci-job-registry.json", encode(base))
				git("init", "--quiet")
				git("add", "scripts/ci")
				baseTree := git("write-tree")
				// A permissive candidate checker must never replace base policy.
				write("check-required-jobs.sh", []byte("#!/bin/sh\nexit 0\n"))
				write("ci-job-registry.json", []byte("{}"))
				cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", gate)
				cmd.Dir = work
				cmd.Env = append(os.Environ(), "RUNNER_TEMP="+t.TempDir(), "GITHUB_EVENT_NAME=merge_group",
					"BASE_SHA="+baseTree, "NEEDS_JSON="+string(encode(results)), "PLAN_JSON="+string(encode(plan)))
				out, err := cmd.CombinedOutput()
				accept := scenario == "full-success"
				if (err == nil) != accept {
					t.Fatalf("accepted %v want %v: %s", err == nil, accept, out)
				}
			})
		}
	}
}
