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
	if len(workflow.Jobs) != 1 || job.If != "" || job.Environment != "" || len(job.Permissions) != 0 {
		t.Fatalf("the pre-tag check is one job with no condition, environment or extra permission: %+v", job)
	}
	// A dispatch from another branch must fail with exit 2, not be skipped: the first step guards the ref.
	if len(job.Steps) == 0 || !strings.Contains(job.Steps[0].Run, "refs/heads/main") || !strings.Contains(job.Steps[0].Run, "exit 2") {
		t.Fatalf("the first step must fail a non-main dispatch with exit 2: %+v", job.Steps)
	}
	for _, forbidden := range []string{"secrets.", "create-github-app-token", "git push", "git tag", "gh release", "gh api -X", "gh api --method"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("the pre-tag check must not use %q: it only reads", forbidden)
		}
	}
	// The verdict tells a difference (1) from a baseline that cannot be verified (5) and from a failed run.
	for _, verdict := range []string{"0) verdict=", "1) verdict=", "5) verdict=", "*) verdict="} {
		if !strings.Contains(string(raw), verdict) {
			t.Fatalf("the verdict must classify every exit status of the comparison: %s missing", verdict)
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
	// Trust must be authenticated and synchronized before a baseline is chosen from it, so an
	// older release is never compared when a newer one is published but not yet recorded.
	run := job.Steps[compare].Run
	preflight, synced, candidate := strings.Index(run, "stable preflight"), strings.Index(run, "require-synced-trust.sh"), strings.Index(run, "stable candidate")
	if preflight < 0 || synced < preflight || candidate < synced {
		t.Fatalf("preflight, then the synchronized-trust check, then the candidate: %d %d %d", preflight, synced, candidate)
	}
	// A refused shape also exits 1 and prints no diff, so the verdict must not promise one.
	if strings.Contains(string(raw), "the log shows the diff") {
		t.Fatal("the exit 1 verdict must say \"see the log\", not promise a diff")
	}
}

func TestCompatibilityUnchangedFixture(t *testing.T) {
	root := repositoryRoot(t)
	command := exec.CommandContext(context.Background(), "sh", filepath.Join(root, "scripts", "release", "check-compatibility-unchanged_test.sh"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
}
