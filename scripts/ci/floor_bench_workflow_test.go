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

func TestReleaseFloorBenchPRUsesRequiredValidation(t *testing.T) {
	repoRoot := repositoryRoot(t)
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(repoRoot, path))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	var floor struct {
		On map[string]yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(read(".github/workflows/floor-bench.yml"), &floor); err != nil {
		t.Fatal(err)
	}
	if _, ok := floor.On["workflow_call"]; !ok {
		t.Fatal("floor benchmark must remain reusable by required validation")
	}
	if len(floor.On) != 2 {
		t.Fatalf("floor benchmark must have only reusable and tag triggers, got %v", floor.On)
	}
	push, ok := floor.On["push"]
	if !ok {
		t.Fatal("floor benchmark must retain its tag trigger")
	}
	var tagTrigger struct {
		Tags []string `yaml:"tags"`
	}
	if err := push.Decode(&tagTrigger); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tagTrigger.Tags, []string{"**"}) {
		t.Fatalf("floor benchmark tag trigger changed: %v", tagTrigger.Tags)
	}

	var validation workflow
	if err := yaml.Unmarshal(read(".github/workflows/ci.yml"), &validation); err != nil {
		t.Fatal(err)
	}
	job := validation.Jobs["floor-bench"]
	if job.Uses != "./.github/workflows/floor-bench.yml" || !reflect.DeepEqual(workflowPlanJobs(job.If), []string{"floor-bench"}) {
		t.Fatalf("required validation must call the planned floor benchmark, got %+v", job)
	}
	dependencies, err := workflowNeeds(job.Needs)
	if err != nil || !reflect.DeepEqual(dependencies, []string{"changes"}) {
		t.Fatalf("floor benchmark must depend on the change plan: %v, %v", dependencies, err)
	}
	dependencies, err = workflowNeeds(validation.Jobs["ci-required"].Needs)
	if err != nil || !slices.Contains(dependencies, "floor-bench") {
		t.Fatalf("required aggregate must include the floor benchmark: %v, %v", dependencies, err)
	}

	var jobRegistry registry
	if err := json.Unmarshal(read("scripts/ci/ci-job-registry.json"), &jobRegistry); err != nil {
		t.Fatal(err)
	}
	if rule := jobRegistry.Jobs["floor-bench"]; rule.RequiredGate != "planned" || !reflect.DeepEqual(rule.PlanJobs, []string{"floor-bench"}) {
		t.Fatalf("floor benchmark must remain a planned required gate, got %+v", rule)
	}

	classifier := exec.Command("sh", filepath.Join(repoRoot, "scripts/ci/classify-changed-paths.sh"), "--files")
	classifier.Dir = repoRoot
	classifier.Stdin = strings.NewReader(".github/workflows/floor-bench.yml\n")
	output, err := classifier.Output()
	if err != nil {
		t.Fatalf("classify floor workflow change: %v", err)
	}
	var plan map[string]bool
	if err := json.Unmarshal(output, &plan); err != nil {
		t.Fatalf("parse floor workflow change plan: %v", err)
	}
	if !plan["floor-bench"] {
		t.Fatal("a floor workflow change must select its required benchmark")
	}
	for _, job := range jobRegistry.PathClasses["full"] {
		if !plan[job] {
			t.Errorf("a workflow change must select the full validation plan, missing %s", job)
		}
	}
}
