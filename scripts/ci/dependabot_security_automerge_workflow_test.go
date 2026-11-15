package ci_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The auto-merge sweep holds the repository's only unattended write path to
// main's queue, so pin who may start it, what its token can do and that no
// credential beyond the workflow token exists.
func TestDependabotSecurityAutomergeWorkflowBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "dependabot-security-automerge.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Name        string               `yaml:"name"`
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Jobs        map[string]struct {
			If          string            `yaml:"if"`
			Environment string            `yaml:"environment"`
			Permissions map[string]string `yaml:"permissions"`
			Concurrency map[string]any    `yaml:"concurrency"`
			Steps       []struct {
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				Env  map[string]string `yaml:"env"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.Name != "dependabot-security-automerge" || len(workflow.Permissions) != 0 || len(workflow.On) != 3 {
		t.Fatal("the sweep needs its name, no default token permissions and exactly three triggers")
	}
	var trigger struct {
		Branches []string `yaml:"branches"`
		Types    []string `yaml:"types"`
	}
	node := workflow.On["pull_request_target"]
	if err := node.Decode(&trigger); err != nil || !reflect.DeepEqual(trigger.Branches, []string{"main"}) ||
		!reflect.DeepEqual(trigger.Types, []string{"opened", "reopened", "synchronize", "ready_for_review"}) {
		t.Fatalf("the pull request trigger must wake the sweep for main only: %+v %v", trigger, err)
	}
	if _, ok := workflow.On["schedule"]; !ok {
		t.Fatal("a schedule must catch an alert that is linked after its pull request opened")
	}
	if _, ok := workflow.On["workflow_dispatch"]; !ok {
		t.Fatal("the sweep must be runnable by hand")
	}
	if len(workflow.Jobs) != 1 {
		t.Fatal("the sweep is one job")
	}
	job := workflow.Jobs["sweep"]
	condition := strings.Join(strings.Fields(job.If), " ")
	for _, required := range []string{
		"github.ref == 'refs/heads/main'",
		"github.event.pull_request.user.id == 49699333",
		"github.event.pull_request.head.repo.full_name == github.repository",
	} {
		if !strings.Contains(condition, required) {
			t.Fatalf("the sweep condition lost %q: %s", required, condition)
		}
	}
	if job.Environment != "" || job.Concurrency["group"] != "dependabot-security-automerge" || job.Concurrency["cancel-in-progress"] != false {
		t.Fatal("the sweep takes no environment and runs one at a time without cancelling")
	}
	if !reflect.DeepEqual(job.Permissions, map[string]string{
		"contents": "write", "pull-requests": "write", "checks": "read", "vulnerability-alerts": "read",
	}) {
		t.Fatalf("the sweep token permissions drifted: %v", job.Permissions)
	}
	if len(job.Steps) != 2 {
		t.Fatalf("the sweep is a checkout and the reviewed script: %d steps", len(job.Steps))
	}
	checkout := job.Steps[0]
	if !strings.HasPrefix(checkout.Uses, "actions/checkout@") || checkout.With["ref"] != "${{ github.sha }}" || checkout.With["persist-credentials"] != "false" || len(checkout.With) != 2 {
		t.Fatalf("the sweep may only check out main's own commit without credentials: %v", checkout.With)
	}
	run := job.Steps[1]
	if run.Run != "./scripts/ci/dependabot-security-automerge.sh" || !reflect.DeepEqual(run.Env, map[string]string{
		"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}",
	}) {
		t.Fatalf("the sweep must run only the reviewed script with the workflow token: %+v", run)
	}
	for _, forbidden := range []string{"secrets.", "create-github-app-token", "pull_request.head.sha", "pull_request.head.ref", "github.head_ref"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("the sweep must not use %q: no credential beyond the workflow token and no pull request code", forbidden)
		}
	}
}

func TestDependabotSecurityAutomergeFixture(t *testing.T) {
	root := repositoryRoot(t)
	command := exec.CommandContext(context.Background(), "sh", filepath.Join(root, "scripts", "ci", "dependabot-security-automerge_test.sh"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
}
