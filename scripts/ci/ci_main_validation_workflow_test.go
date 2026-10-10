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

type mainValidationStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	If   string            `yaml:"if"`
	Env  map[string]string `yaml:"env"`
	With map[string]string `yaml:"with"`
}

type mainValidationWorkflow struct {
	On   map[string]yaml.Node `yaml:"on"`
	Jobs map[string]struct {
		Uses        string               `yaml:"uses"`
		If          string               `yaml:"if"`
		Needs       yaml.Node            `yaml:"needs"`
		Permissions map[string]string    `yaml:"permissions"`
		Steps       []mainValidationStep `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestMainValidationWorkflowPolicy(t *testing.T) {
	read := func(name string) mainValidationWorkflow {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		var got mainValidationWorkflow
		if err := yaml.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	ci, nightly, floor := read("ci.yml"), read("nightly.yml"), read("floor-bench.yml")
	if len(ci.On) != 2 {
		t.Fatal("CI may only be called for validation or manually dispatched")
	}
	for _, event := range []string{"workflow_call", "workflow_dispatch"} {
		if _, ok := ci.On[event]; !ok {
			t.Fatalf("missing %s entrypoint", event)
		}
	}
	validation := nightly.Jobs["validation"]
	if validation.Uses != "./.github/workflows/ci.yml" || validation.If != "github.ref == 'refs/heads/main'" ||
		!reflect.DeepEqual(validation.Permissions, map[string]string{"actions": "read", "contents": "read"}) {
		t.Fatal("nightly validation must use the unchanged read-only CI graph on main")
	}
	needs, err := workflowNeeds(nightly.Jobs["publish"].Needs)
	if err != nil || !reflect.DeepEqual(needs, []string{"validation"}) {
		t.Fatalf("nightly publication must depend on full validation: %v %v", needs, err)
	}
	var proof, classify, gate mainValidationStep
	for _, step := range nightly.Jobs["publish"].Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["ref"] != "${{ github.sha }}" {
			t.Fatal("nightly must publish its validated exact source")
		}
		if step.Name == "Require exact-head green main CI" {
			proof = step
		}
	}
	if proof.Run != `test "$VALIDATION_RESULT" = success` || !reflect.DeepEqual(proof.Env, map[string]string{"VALIDATION_RESULT": "${{ needs.validation.result }}"}) {
		t.Fatal("nightly proof must bind the current reusable validation result")
	}
	for _, result := range []string{"success", "failure", "cancelled", "skipped", ""} {
		cmd := exec.Command("bash", "-e", "-c", proof.Run)
		cmd.Env = append(os.Environ(), "VALIDATION_RESULT="+result)
		if (cmd.Run() == nil) != (result == "success") {
			t.Fatalf("nightly publication accepted invalid validation %q", result)
		}
	}
	for _, step := range ci.Jobs["changes"].Steps {
		if step.Name == "Classify changed paths" {
			classify = step
		}
	}
	for _, step := range ci.Jobs["ci-required"].Steps {
		if step.Name == "Require every validation job" {
			gate = step
		}
	}
	if classify.Run == "" || gate.Run == "" {
		t.Fatal("missing executable classification or required-job proof")
	}
	const mainEvents = "(github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'"
	floorAllows, floorCheckout := false, false
	for _, step := range floor.Jobs["measure"].Steps {
		condition := strings.Join(strings.Fields(step.If), " ")
		if strings.Contains(condition, mainEvents) {
			floorAllows = floorAllows || step.Name == "Refuse unsupported source trust contexts"
			floorCheckout = floorCheckout || (strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["ref"] == "${{ github.sha }}")
		}
	}
	if !floorAllows || !floorCheckout {
		t.Fatal("cache-free floor proof must allow main validation and check out its exact SHA")
	}
	var rules registry
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "ci", "ci-job-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &rules); err != nil {
		t.Fatal(err)
	}
	checker, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "ci", "check-required-jobs.sh"))
	if err != nil {
		t.Fatal(err)
	}
	encode := func(value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	for _, event := range []string{"workflow_dispatch", "schedule", "repository_dispatch"} {
		for _, ref := range []string{"refs/heads/main", "refs/heads/feature"} {
			t.Run(event+"/"+ref, func(t *testing.T) {
				work := t.TempDir()
				scriptDir := filepath.Join(work, "scripts", "ci")
				if err := os.MkdirAll(scriptDir, 0o755); err != nil {
					t.Fatal(err)
				}
				write := func(name, data string) {
					t.Helper()
					if err := os.WriteFile(filepath.Join(scriptDir, name), []byte(data), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				plan := make(map[string]bool)
				for _, key := range rules.PathClasses["full"] {
					plan[key] = true
				}
				write("classify-changed-paths.sh", "#!/bin/sh\nset -eu\n[ \"$1\" = --all ]\nprintf '%s\\n' \"$FIXTURE_PLAN\"\n")
				write("check-required-jobs.sh", string(checker))
				write("ci-job-registry.json", encode(rules))
				run := func(source string, env ...string) ([]byte, error) {
					t.Helper()
					cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", source)
					cmd.Dir = work
					cmd.Env = append(os.Environ(), append([]string{"GITHUB_REF=" + ref, "GITHUB_EVENT_NAME=" + event,
						"EVENT_NAME=" + event, "RUNNER_TEMP=" + t.TempDir(), "GITHUB_OUTPUT=" + filepath.Join(work, "output"),
						"FIXTURE_PLAN=" + encode(plan), "BASE_SHA=", "HEAD_SHA="}, env...)...)
					return cmd.CombinedOutput()
				}
				out, err := run(classify.Run)
				if (err == nil) != (ref == "refs/heads/main") {
					t.Fatalf("classification trusted-ref refusal: %v %s", err, out)
				}
				if ref == "refs/heads/main" {
					output, err := os.ReadFile(filepath.Join(work, "output"))
					if err != nil || string(output) != "plan="+encode(plan)+"\n" {
						t.Fatalf("main validation did not select the complete plan: %v %s", err, output)
					}
				}
				results := make(map[string]map[string]string)
				for name, rule := range rules.Jobs {
					if directGate(rule.RequiredGate) {
						results[name] = map[string]string{"result": "success"}
					}
				}
				out, err = run(gate.Run, "NEEDS_JSON="+encode(results), "PLAN_JSON="+encode(plan))
				if (err == nil) != (ref == "refs/heads/main") {
					t.Fatalf("gate trusted-ref refusal: %v %s", err, out)
				}
				if ref == "refs/heads/main" {
					results["test_core"]["result"] = "failure"
					if out, err := run(gate.Run, "NEEDS_JSON="+encode(results), "PLAN_JSON="+encode(plan)); err == nil {
						t.Fatalf("main gate accepted failed core validation: %s", out)
					}
					for key := range plan {
						plan[key] = key == "docs"
					}
					for name, rule := range rules.Jobs {
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
						results[name]["result"] = result
					}
					if out, err := run(gate.Run, "NEEDS_JSON="+encode(results), "PLAN_JSON="+encode(plan)); err == nil {
						t.Fatalf("main gate accepted a selective plan: %s", out)
					}
				}
			})
		}
	}
}
