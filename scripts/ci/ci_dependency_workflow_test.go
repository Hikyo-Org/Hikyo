package ci_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Platform proof gates the final result without serializing Linux validation.
func TestCoreValidationRunsAlongsideCrossPlatformProof(t *testing.T) {
	repoRoot := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var ci workflow
	if err := yaml.Unmarshal(raw, &ci); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"test_core", "scan-xplat"} {
		job, ok := ci.Jobs[name]
		if !ok {
			t.Fatalf("missing %s validation", name)
		}
		needs, err := workflowNeeds(job.Needs)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(needs, []string{"changes"}) {
			t.Fatalf("%s must start after classification independently, got %v", name, needs)
		}
		if !reflect.DeepEqual(workflowPlanJobs(job.If), []string{"test"}) {
			t.Fatalf("%s must retain the complete test plan", name)
		}
	}
	needs, err := workflowNeeds(ci.Jobs["ci-required"].Needs)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"test_core", "scan-xplat"} {
		found := false
		for _, required := range needs {
			found = found || required == name
		}
		if !found {
			t.Fatalf("ci-required must wait for %s independently", name)
		}
	}
	raw, err = os.ReadFile(filepath.Join(repoRoot, "scripts", "ci", "ci-job-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rules registry
	if err := json.Unmarshal(raw, &rules); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"test_core", "scan-xplat"} {
		rule := rules.Jobs[name]
		if rule.RequiredGate != "planned" || !reflect.DeepEqual(rule.PlanJobs, []string{"test"}) {
			t.Fatalf("%s must be directly required by the complete test plan, got %+v", name, rule)
		}
	}
	t.Run("trusted-base-migration", testCoreValidationTrustedBaseMigration)
}

// Execute the real workflow shell against a Git base tree, not a copied model.
func testCoreValidationTrustedBaseMigration(t *testing.T) {
	repoRoot := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var ci struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &ci); err != nil {
		t.Fatal(err)
	}
	var gate string
	for _, step := range ci.Jobs["ci-required"].Steps {
		if step.Name == "Require every validation job" {
			gate = step.Run
		}
	}
	if gate == "" {
		t.Fatal("missing executable required-job step")
	}
	checker, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "ci", "check-required-jobs.sh"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(repoRoot, "scripts", "ci", "ci-job-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var current registry
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	for _, baseKind := range []string{"legacy", "direct", "unknown-scan", "unknown-core"} {
		base := cloneRegistry(t, current)
		if baseKind != "direct" {
			base.Jobs["scan-xplat"] = jobRule{RequiredGate: "indirect", PlanJobs: []string{"test"}}
		}
		switch baseKind {
		case "unknown-scan":
			base.Jobs["scan-xplat"] = jobRule{RequiredGate: "indirect", PlanJobs: []string{"race"}}
		case "unknown-core":
			base.Jobs["test_core"] = jobRule{RequiredGate: "indirect", PlanJobs: []string{"test"}}
		}
		for _, scenario := range []struct {
			name, scan, core string
			docsOnly         bool
			accept           bool
		}{
			{name: "full-success", scan: "success", core: "success", accept: true},
			{name: "scan-failed-core-passed", scan: "failure", core: "success"},
			{name: "scan-cancelled-core-passed", scan: "cancelled", core: "success"},
			{name: "planned-scan-skipped-core-passed", scan: "skipped", core: "success"},
			{name: "scan-missing-core-passed", core: "success"},
			{name: "core-failed-scan-passed", scan: "success", core: "failure"},
			{name: "core-cancelled-scan-passed", scan: "success", core: "cancelled"},
			{name: "core-skipped-scan-passed", scan: "success", core: "skipped"},
			{name: "docs-unplanned-scan-skipped", scan: "skipped", core: "skipped", docsOnly: true, accept: true},
			{name: "docs-unplanned-scan-ran", scan: "success", core: "skipped", docsOnly: true},
			{name: "docs-unplanned-scan-failed", scan: "failure", core: "skipped", docsOnly: true},
			{name: "docs-unplanned-scan-missing", core: "skipped", docsOnly: true},
		} {
			t.Run(baseKind+"/"+scenario.name, func(t *testing.T) {
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
				encode := func(value any) []byte {
					t.Helper()
					data, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					return data
				}
				write("check-required-jobs.sh", checker)
				write("ci-job-registry.json", encode(base))
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
				git("init", "--quiet")
				git("add", "scripts/ci")
				baseTree := git("write-tree")
				git("update-ref", "refs/fixtures/trusted-base", baseTree)
				// A permissive head checker must never replace trusted-base policy.
				write("check-required-jobs.sh", []byte("#!/bin/sh\nexit 0\n"))
				write("ci-job-registry.json", []byte("{}"))
				plan := make(map[string]bool)
				for _, key := range current.PathClasses["full"] {
					plan[key] = !scenario.docsOnly || key == "docs"
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
				results["test_core"]["result"] = scenario.core
				if scenario.scan == "" {
					delete(results, "scan-xplat")
				} else {
					results["scan-xplat"]["result"] = scenario.scan
				}
				cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", gate)
				cmd.Dir = work
				cmd.Env = append(os.Environ(),
					"RUNNER_TEMP="+t.TempDir(), "GITHUB_EVENT_NAME=pull_request",
					"BASE_SHA=refs/fixtures/trusted-base", "NEEDS_JSON="+string(encode(results)),
					"PLAN_JSON="+string(encode(plan)))
				out, err := cmd.CombinedOutput()
				accept := scenario.accept && (baseKind == "legacy" || baseKind == "direct")
				if (err == nil) != accept {
					t.Fatalf("accept = %v, want %v: %s", err == nil, accept, out)
				}
				if accept && !strings.Contains(string(out), "required jobs: planned validation passed") {
					t.Fatalf("trusted checker did not validate the normalized input: %s", out)
				}
			})
		}
	}
}
