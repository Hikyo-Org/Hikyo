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

// The planner prepares a release a person tags. Pin that it holds no write path
// except the one issue, no credential beyond the workflow token, and that the
// write permission never shares a job with the code that plans.
func TestSecurityPatchPlanWorkflowBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "security-patch-plan.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Jobs        map[string]struct {
			If          string            `yaml:"if"`
			Needs       any               `yaml:"needs"`
			Environment string            `yaml:"environment"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				If   string `yaml:"if"`
				Uses string `yaml:"uses"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.On) != 3 || len(workflow.Permissions) != 0 {
		t.Fatalf("three triggers and no default token permissions: %v %v", workflow.On, workflow.Permissions)
	}
	var push struct {
		Branches []string `yaml:"branches"`
	}
	node := workflow.On["push"]
	if err := node.Decode(&push); err != nil || !reflect.DeepEqual(push.Branches, []string{"main"}) {
		t.Fatalf("the push trigger is main only: %+v %v", push, err)
	}
	for _, trigger := range []string{"schedule", "workflow_dispatch"} {
		if _, ok := workflow.On[trigger]; !ok {
			t.Fatalf("missing trigger %s", trigger)
		}
	}
	if len(workflow.Jobs) != 2 {
		t.Fatalf("a read-only plan job and one issue job: %d jobs", len(workflow.Jobs))
	}
	plan, issue := workflow.Jobs["plan"], workflow.Jobs["issue"]
	if plan.If != "github.ref == 'refs/heads/main'" || plan.Environment != "" || issue.Environment != "" {
		t.Fatalf("the plan runs on main and neither job takes an environment: %q", plan.If)
	}
	if !reflect.DeepEqual(plan.Permissions, map[string]string{"actions": "read", "checks": "read", "contents": "read", "pull-requests": "read", "vulnerability-alerts": "read"}) {
		t.Fatalf("the plan job is read-only: %v", plan.Permissions)
	}
	for scope, level := range plan.Permissions {
		if level != "read" {
			t.Fatalf("the plan job holds %s: %s", scope, level)
		}
	}
	// A failed, cancelled or timed-out plan must still reach the issue.
	if issue.If != "${{ always() && github.ref == 'refs/heads/main' }}" {
		t.Fatalf("the issue job must run after a failed plan, on main only: %q", issue.If)
	}
	if !reflect.DeepEqual(issue.Permissions, map[string]string{"contents": "read", "issues": "write"}) || issue.Needs != "plan" {
		t.Fatalf("the issue job is the only writer and waits for the plan: %v %v", issue.Permissions, issue.Needs)
	}
	cosign, run := -1, -1
	for i, step := range plan.Steps {
		if strings.Contains(step.Run, "fetch-pinned-tool.sh cosign-linux-amd64") {
			cosign = i
		}
		if strings.Contains(step.Run, "security-patch-plan.sh plan") {
			run = i
		}
	}
	if cosign < 0 || run < 0 || cosign > run {
		t.Fatalf("pinned Cosign must be installed before the plan verifies the previous release: %d %d", cosign, run)
	}
	var reported, published int
	for _, step := range issue.Steps {
		switch {
		case strings.Contains(step.Run, "security-patch-plan.sh issue --failed \"$RUN_URL\""):
			reported++
			if step.If != "${{ needs.plan.result != 'success' }}" {
				t.Fatalf("a failed plan is reported only when the plan did not succeed: %q", step.If)
			}
		case strings.Contains(step.Run, "security-patch-plan.sh issue \"$PLAN_DIR/"):
			published++
			if step.If != "${{ needs.plan.result == 'success' }}" {
				t.Fatalf("the plan is published only when it succeeded: %q", step.If)
			}
		case strings.Contains(step.Uses, "download-artifact"):
			if step.If != "${{ needs.plan.result == 'success' }}" {
				t.Fatalf("the plan artifact is downloaded only when it exists: %q", step.If)
			}
		}
	}
	if reported != 1 || published != 1 {
		t.Fatalf("the issue job publishes a plan and reports a failed one: %d %d", published, reported)
	}
	for _, forbidden := range []string{"secrets.", "create-github-app-token", "git push", "git tag", "gh release", "gh pr ", "gh api -X", "gh api --method", "pull_request"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("the planner must not use %q: it tags and publishes nothing", forbidden)
		}
	}
}

func TestSecurityPatchPlanFixture(t *testing.T) {
	root := repositoryRoot(t)
	command := exec.CommandContext(context.Background(), "sh", filepath.Join(root, "scripts", "release", "security-patch-plan_test.sh"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
}
