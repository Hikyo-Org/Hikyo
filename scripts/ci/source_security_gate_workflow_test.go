package ci_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The relative ci-candidate/ci-merge-group calls select the candidate's ci.yml,
// but its aggregate must execute protected-base policy. These legacy fixtures
// are exact git-show bytes from 4f1d1ec63, not a modeled permissive checker.
func TestSourceSecurityGateTrustedBaseMigration(t *testing.T) {
	root := repositoryRoot(t)
	read := func(path string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var ci mainValidationWorkflow
	if err := yaml.Unmarshal(read(".github/workflows/ci.yml"), &ci); err != nil {
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
	var current registry
	if err := json.Unmarshal(read("scripts/ci/ci-job-registry.json"), &current); err != nil {
		t.Fatal(err)
	}
	encode := func(value any) []byte {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	for _, baseKind := range []string{"4f1d1ec63", "current", "unknown-web-go"} {
		checker := read("scripts/ci/check-required-jobs.sh")
		baseRegistry := read("scripts/ci/ci-job-registry.json")
		if baseKind != "current" {
			checker = read("scripts/ci/testdata/source-security-trusted-base/check-required-jobs.sh")
			baseRegistry = read("scripts/ci/testdata/source-security-trusted-base/ci-job-registry.json")
		}
		if baseKind == "unknown-web-go" {
			var base registry
			if err := json.Unmarshal(baseRegistry, &base); err != nil {
				t.Fatal(err)
			}
			base.Jobs["web-go"] = jobRule{RequiredGate: "planned", PlanJobs: []string{"web"}}
			baseRegistry = encode(base)
		}
		for _, event := range []string{"pull_request", "merge_group"} {
			for _, scenario := range []string{"full-success", "failure", "cancelled", "skipped", "missing", "unknown-extra", "legacy-job-failed", "malformed-plan", "missing-plan", "docs-success", "docs-unplanned-ran", "docs-missing"} {
				t.Run(baseKind+"/"+event+"/"+scenario, func(t *testing.T) {
					docs := strings.HasPrefix(scenario, "docs-")
					plan := make(map[string]any)
					for _, key := range current.PathClasses["full"] {
						plan[key] = !docs || key == "docs"
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
								if plan[key] == true {
									result = "success"
								}
							}
						}
						results[name] = map[string]string{"result": result}
					}
					switch scenario {
					case "failure", "cancelled", "skipped":
						results["source-artifact-security"]["result"] = scenario
					case "missing", "docs-missing":
						delete(results, "source-artifact-security")
					case "unknown-extra":
						results["future-required-job"] = map[string]string{"result": "success"}
					case "legacy-job-failed":
						results["web-go"]["result"] = "failure"
					case "malformed-plan":
						plan["web-go"] = "true"
					case "missing-plan":
						delete(plan, "web-go")
					case "docs-unplanned-ran":
						results["source-artifact-security"]["result"] = "success"
					}
					work := t.TempDir()
					scriptDir := filepath.Join(work, "scripts", "ci")
					if err := os.MkdirAll(scriptDir, 0700); err != nil {
						t.Fatal(err)
					}
					write := func(name string, raw []byte) {
						t.Helper()
						if err := os.WriteFile(filepath.Join(scriptDir, name), raw, 0700); err != nil {
							t.Fatal(err)
						}
					}
					write("check-required-jobs.sh", checker)
					write("ci-job-registry.json", baseRegistry)
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
					// Prove the PR checkout cannot replace base policy.
					write("check-required-jobs.sh", []byte("#!/bin/sh\nexit 0\n"))
					write("ci-job-registry.json", []byte("{}"))
					runnerTemp := t.TempDir()
					candidate := "true"
					if docs {
						candidate = "false"
					}
					cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", gate)
					cmd.Dir = work
					cmd.Env = append(os.Environ(), "RUNNER_TEMP="+runnerTemp,
						"GITHUB_EVENT_NAME="+event, "FULL_CANDIDATE="+candidate,
						"BASE_SHA="+baseTree, "NEEDS_JSON="+string(encode(results)),
						"PLAN_JSON="+string(encode(plan)))
					out, err := cmd.CombinedOutput()
					accept := baseKind != "unknown-web-go" && (scenario == "full-success" || (scenario == "docs-success" && event == "pull_request"))
					if (err == nil) != accept {
						t.Fatalf("accepted %v want %v: %s", err == nil, accept, out)
					}
					for name, expected := range map[string][]byte{"check-required-jobs.sh": checker, "ci-job-registry.json": baseRegistry} {
						actual, err := os.ReadFile(filepath.Join(runnerTemp, name))
						if err != nil || string(actual) != string(expected) {
							t.Fatalf("trusted base %s was replaced or changed: %v", name, err)
						}
					}
				})
			}
		}
	}
}
