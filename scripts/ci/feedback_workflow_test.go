package ci_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFeedbackWorkflowPreservesEveryCommitAndImmutableBootstrap(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-feedback.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Timeout int               `yaml:"timeout-minutes"`
			Env     map[string]string `yaml:"env"`
			Steps   []struct {
				Name string            `yaml:"name"`
				Run  string            `yaml:"run"`
				Uses string            `yaml:"uses"`
				If   string            `yaml:"if"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Concurrency.Group != "ci-feedback-${{ github.event.pull_request.number }}-${{ github.event.pull_request.head.sha }}-${{ github.event.pull_request.base.sha }}" || !got.Concurrency.Cancel {
		t.Fatal("a later pushed commit must not cancel an earlier commit's feedback; only duplicate exact inputs may supersede")
	}
	if !reflect.DeepEqual(got.Permissions, map[string]string{"contents": "read"}) {
		t.Fatal("feedback execution must remain read-only")
	}
	job := got.Jobs["feedback"]
	if len(got.Jobs) != 1 || job.Timeout != 4 {
		t.Fatal("one bounded feedback runner includes setup in its four-minute budget")
	}
	if job.Env["FAST_FEEDBACK_TOOLS_DIR"] != "${{ github.workspace }}/feedback-tools/scripts/ci" || job.Env["FAST_FEEDBACK_HEAD"] != "${{ github.event.pull_request.head.sha }}" {
		t.Fatal("immutable informational helper and exact head must be separate")
	}
	if job.Steps[0].With["ref"] != "${{ github.sha }}" || job.Steps[0].With["path"] != "feedback-tools" || job.Steps[1].With["ref"] != "${{ github.event.pull_request.head.sha }}" || job.Steps[1].With["path"] != "source" {
		t.Fatal("old PR heads must bootstrap feedback from the immutable merge revision without changing tested source")
	}
	for _, step := range job.Steps {
		if strings.Contains(step.Uses, "setup-node@") && !strings.Contains(step.If, "fromJSON(steps.plan.outputs.plan).docs") {
			t.Fatal("docs-only feedback must select the pinned Node runtime")
		}
		if (strings.Contains(step.Run, "pnpm install") || strings.Contains(step.Run, "install-corepack.sh")) && strings.Contains(step.If, ".docs") {
			t.Fatal("read-only docs preparation must not require Node dependency installation")
		}
		if strings.Contains(step.Uses, "cache/save") || strings.Contains(step.Uses, "/.github/workflows/ci.yml") || strings.Contains(step.Run, "bash scripts/ci/fast-feedback.sh") {
			t.Fatal("feedback cannot write shared caches, run the full graph, or depend on a helper in an old head")
		}
		if strings.Contains(step.Uses, "checkout@") && step.With["persist-credentials"] != "false" {
			t.Fatal("source and tooling must not persist credentials")
		}
	}
}
