package ci_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDevGateExecutesOnlyTrustedMainCodeUnderDistinctHeadLease(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-dev-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Name        string               `yaml:"name"`
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Jobs        map[string]struct {
			Timeout     int               `yaml:"timeout-minutes"`
			Permissions map[string]string `yaml:"permissions"`
			Concurrency struct {
				Group  string `yaml:"group"`
				Cancel bool   `yaml:"cancel-in-progress"`
				Queue  string `yaml:"queue"`
			} `yaml:"concurrency"`
			Steps []struct {
				Name string            `yaml:"name"`
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				If   string            `yaml:"if"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.Name != "gate-dev" || !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) || len(workflow.Jobs) != 2 {
		t.Fatal("dev publisher lost its read-only discovery/write-worker separation")
	}
	var target struct {
		Branches []string `yaml:"branches"`
	}
	pr := workflow.On["pull_request_target"]
	if err := pr.Decode(&target); err != nil || !reflect.DeepEqual(target.Branches, []string{"dev"}) {
		t.Fatal("dev target publisher must listen only to dev PRs")
	}
	discover, publish := workflow.Jobs["discover"], workflow.Jobs["publish"]
	if !reflect.DeepEqual(discover.Permissions, map[string]string{"contents": "read", "pull-requests": "read"}) {
		t.Fatal("routing discovery must not write checks")
	}
	if !reflect.DeepEqual(publish.Permissions, map[string]string{"actions": "read", "contents": "read", "pull-requests": "read", "checks": "write"}) || publish.Timeout != 1 {
		t.Fatal("dev admission must retain narrow permissions and setup-inclusive budget")
	}
	if publish.Concurrency.Group != "dev-check-${{ github.repository_id }}-${{ matrix.work.head }}" || publish.Concurrency.Cancel || publish.Concurrency.Queue != "max" {
		t.Fatal("dev writers must serialize under their distinct exact-head lease")
	}
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/checkout@") && (step.With["ref"] != "${{ github.event.repository.default_branch }}" || step.With["persist-credentials"] != "false") {
				t.Fatal("privileged dev publisher checked out candidate-controlled code")
			}
			if strings.HasPrefix(step.Uses, "actions/cache") {
				t.Fatal("fast dev admission added persistent cache storage")
			}
		}
	}
	if len(publish.Steps) != 4 || !strings.Contains(publish.Steps[0].Run, "PR_GATE_DEADLINE=$((started + 60))") ||
		!strings.Contains(publish.Steps[2].Run, "PR_GATE_DEADLINE - 15") || publish.Steps[3].If != "always()" ||
		!strings.Contains(publish.Steps[3].Run, "--dev --dev-pr") || !strings.Contains(publish.Steps[3].Run, `--expected-head "$HEAD"`) {
		t.Fatal("dev worker lost authenticated setup deadline, reserved settlement or typed producer mode")
	}
	if strings.Contains(string(raw), "uses: ./.github/workflows/ci.yml") || strings.Contains(string(raw), "secrets:") {
		t.Fatal("dev admission must not run the full graph or receive publication custody")
	}
}
