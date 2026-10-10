package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeGroupWorkflowRetainsRequiredValidation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-merge-group.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Name        string               `yaml:"name"`
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			Uses        string               `yaml:"uses"`
			With        map[string]string    `yaml:"with"`
			If          string               `yaml:"if"`
			Needs       string               `yaml:"needs"`
			Permissions map[string]string    `yaml:"permissions"`
			Steps       []mainValidationStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	_, isMergeGroup := workflow.On["merge_group"]
	readOnly := map[string]string{"actions": "read", "contents": "read"}
	if workflow.Name != "trusted-merge-ci" || !isMergeGroup || len(workflow.On) != 1 || !reflect.DeepEqual(workflow.Permissions, readOnly) || len(workflow.Jobs) != 2 {
		t.Fatal("merge-queue validation must have one isolated read-only event and its required gate")
	}
	if workflow.Concurrency.Group != "trusted-merge-ci-${{ github.run_id }}" || !workflow.Concurrency.Cancel {
		t.Fatal("independent merge groups must not cancel each other's validation")
	}
	validation := workflow.Jobs["validation"]
	if validation.Uses != "./.github/workflows/ci.yml" || validation.If != "" || validation.Needs != "" || !reflect.DeepEqual(validation.With, map[string]string{"revision": "${{ github.sha }}"}) || !reflect.DeepEqual(validation.Permissions, readOnly) {
		t.Fatal("merge-queue validation must unconditionally bind the full CI graph to its event SHA")
	}
	gate, exists := workflow.Jobs["ci-required"]
	if !exists || gate.If != "always() && !cancelled()" || gate.Needs != "validation" || !reflect.DeepEqual(gate.Permissions, readOnly) || len(gate.Steps) != 1 {
		t.Fatal("the required merge-queue context must independently reject failed or skipped validation")
	}
	proof := gate.Steps[0]
	if proof.If != "" || proof.Run != `test "$VALIDATION_RESULT" = success` || !reflect.DeepEqual(proof.Env, map[string]string{"VALIDATION_RESULT": "${{ needs.validation.result }}"}) {
		t.Fatal("merge-queue proof must execute against the actual reusable validation result")
	}
	for _, result := range []string{"success", "failure", "cancelled", "skipped", "pending", "", "success failure"} {
		cmd := exec.Command("bash", "-e", "-c", proof.Run)
		cmd.Env = append(os.Environ(), "VALIDATION_RESULT="+result)
		if (cmd.Run() == nil) != (result == "success") {
			t.Fatalf("merge-queue proof accepted invalid validation %q", result)
		}
	}
}

func TestPullRequestTargetCannotReachValidationGraph(t *testing.T) {
	type workflow struct {
		On   map[string]yaml.Node `yaml:"on"`
		Jobs map[string]struct {
			Uses string `yaml:"uses"`
		} `yaml:"jobs"`
	}
	paths, err := filepath.Glob(filepath.Join(repositoryRoot(t), ".github/workflows/*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflows := map[string]workflow{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var decoded workflow
		if err := yaml.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		workflows[filepath.Base(path)] = decoded
	}
	var visit func(string, map[string]bool)
	visit = func(name string, visited map[string]bool) {
		if name == "ci.yml" || name == "floor-bench.yml" {
			t.Fatalf("pull_request_target reaches executable validation in %s", name)
		}
		if visited[name] {
			return
		}
		visited[name] = true
		current, exists := workflows[name]
		if !exists {
			t.Fatalf("cannot verify local reusable workflow %s", name)
		}
		for _, job := range current.Jobs {
			if strings.HasPrefix(job.Uses, "./.github/workflows/") {
				visit(strings.TrimPrefix(job.Uses, "./.github/workflows/"), visited)
			}
		}
	}
	for name, current := range workflows {
		if _, exists := current.On["pull_request_target"]; exists {
			visit(name, map[string]bool{})
		}
	}
}
