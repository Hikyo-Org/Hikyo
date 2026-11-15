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

// The pre-tag check must stay read-only and manual: no write permission, no
// secret, no trigger other than a hand dispatch from main.
func TestReleaseCompatibilityCheckWorkflowBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release-compatibility-check.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Jobs        map[string]struct {
			If          string            `yaml:"if"`
			Environment string            `yaml:"environment"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Uses string `yaml:"uses"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if _, ok := workflow.On["workflow_dispatch"]; !ok || len(workflow.On) != 1 {
		t.Fatalf("the pre-tag check is started by hand only: %v", workflow.On)
	}
	if !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) {
		t.Fatalf("the pre-tag check token is read-only: %v", workflow.Permissions)
	}
	job := workflow.Jobs["check"]
	if len(workflow.Jobs) != 1 || job.If != "github.ref == 'refs/heads/main'" || job.Environment != "" || len(job.Permissions) != 0 {
		t.Fatalf("the pre-tag check is one main-only job with no environment or extra permission: %+v", job)
	}
	for _, forbidden := range []string{"secrets.", "create-github-app-token", "git push", "git tag", "gh release", "gh api -X", "gh api --method"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("the pre-tag check must not use %q: it only reads", forbidden)
		}
	}
	// The verifier of the previous release needs the pinned Cosign before the comparison runs.
	cosign, compare := -1, -1
	for i, step := range job.Steps {
		if strings.Contains(step.Run, "fetch-pinned-tool.sh cosign-linux-amd64") {
			cosign = i
		}
		if strings.Contains(step.Run, "./scripts/release/check-compatibility-unchanged.sh") {
			compare = i
		}
	}
	if cosign < 0 || compare < 0 || cosign > compare {
		t.Fatalf("pinned Cosign must be installed before the comparison: cosign=%d compare=%d", cosign, compare)
	}
}

func TestCompatibilityUnchangedFixture(t *testing.T) {
	root := repositoryRoot(t)
	command := exec.CommandContext(context.Background(), "sh", filepath.Join(root, "scripts", "release", "check-compatibility-unchanged_test.sh"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
}
