package ci_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The desktop job left ci.yml on 2026-11-25. Merge groups run the protected base's
// checker, registry and planner against the candidate's ci.yml, so a base that
// still registers desktop must accept a candidate that really lacks the job, and
// still refuse everything else, including a candidate that has the job and
// failed it. A base that already dropped desktop is judged directly.
func TestDesktopRemovalTrustedBaseMigration(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var ci mainValidationWorkflow
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
	if _, ok := current.Jobs["desktop"]; ok || slices.Contains(current.PathClasses["full"], "desktop") {
		t.Fatal("the candidate registry must not require desktop")
	}
	encode := func(value any) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, baseKind := range []string{"base-has-desktop", "base-lacks-desktop"} {
		for _, scenario := range []string{"full-success", "docs-only", "test_core failed", "test_core cancelled", "test_core skipped", "test_core missing", "desktop ran and passed", "desktop ran and failed", "docs-only desktop ran"} {
			t.Run(baseKind+"/"+scenario, func(t *testing.T) {
				base := cloneRegistry(t, current)
				plan := make(map[string]bool)
				docsOnly := strings.HasPrefix(scenario, "docs-only")
				for _, key := range current.PathClasses["full"] {
					plan[key] = !docsOnly || key == "docs"
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
				if baseKind == "base-has-desktop" {
					base.Jobs["desktop"] = jobRule{RequiredGate: "planned", PlanJobs: []string{"desktop"}}
					base.PathClasses["full"] = append(base.PathClasses["full"], "desktop")
					base.PathClasses["desktop"] = []string{"desktop"}
					// The base planner still emits the key the candidate no longer has.
					plan["desktop"] = !docsOnly
				}
				switch scenario {
				case "test_core failed", "test_core cancelled", "test_core skipped":
					results["test_core"]["result"] = strings.TrimPrefix(scenario, "test_core ")
				case "test_core missing":
					delete(results, "test_core")
				case "desktop ran and passed":
					results["desktop"] = map[string]string{"result": "success"}
				case "desktop ran and failed":
					results["desktop"] = map[string]string{"result": "failure"}
				case "docs-only desktop ran":
					results["desktop"] = map[string]string{"result": "success"}
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
				if docsOnly {
					decision := encode(map[string]any{"mode": "docs", "plan": plan})
					write("plan-merge-group.sh", []byte("#!/bin/sh\nprintf '%s\\n' '"+string(decision)+"'\n"))
					write("classify-changed-paths.sh", []byte("#!/bin/sh\nexit 0\n"))
				}
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
				accept := scenario == "full-success" || scenario == "docs-only"
				if baseKind == "base-has-desktop" && scenario == "desktop ran and passed" {
					// A candidate that still has the job is judged by the unmodified base.
					accept = true
				}
				if (err == nil) != accept {
					t.Fatalf("accepted %v want %v: %s", err == nil, accept, out)
				}
			})
		}
	}
}
