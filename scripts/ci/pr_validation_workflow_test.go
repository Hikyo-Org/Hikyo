package ci_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPRValidationWorkflowAdmission(t *testing.T) {
	var workflow struct {
		Name    string `yaml:"name"`
		RunName string `yaml:"run-name"`
		On      map[string]struct {
			Types []string `yaml:"types"`
		} `yaml:"on"`
		Concurrency struct {
			Group string `yaml:"group"`
		} `yaml:"concurrency"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			If    string `yaml:"if"`
			Needs string `yaml:"needs"`
		} `yaml:"jobs"`
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-fork.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.Name != "pr-validation" {
		t.Fatal("all PRs must use the neutral pr-validation workflow name")
	}
	if len(workflow.On) != 1 || !slices.Equal(workflow.On["pull_request"].Types, []string{"opened", "synchronize", "reopened", "edited"}) {
		t.Fatal("PR validation must run on base retargets as well as head changes")
	}
	if workflow.Concurrency.Group != "${{ github.event.action == 'edited' && github.event.changes.base.ref.from == null && 'pr-metadata' || 'fork-ci' }}-${{ github.event.pull_request.number }}" {
		t.Fatal("title/body edits must never cancel an active validation run")
	}
	// Existing trusted bases bind validation to this exact title. Changing it
	// in the same rollout would strand gates that still execute the old base.
	if workflow.RunName != "${{ github.event.action == 'edited' && github.event.changes.base.ref.from == null && 'pr-metadata' || 'fork-ci' }} #${{ github.event.pull_request.number }}" {
		t.Fatal("retain the legacy run identity for existing trusted gates")
	}
	if len(workflow.Permissions) != 2 || workflow.Permissions["actions"] != "read" || workflow.Permissions["contents"] != "read" {
		t.Fatal("PR execution must remain read-only")
	}
	if workflow.Jobs["vouch"].If != "github.event.pull_request.head.repo.full_name != github.repository && (github.event.action != 'edited' || github.event.changes.base.ref.from != null)" {
		t.Fatal("only forks should allocate an author-vouch runner")
	}
	validation := workflow.Jobs["validation"]
	if validation.Needs != "vouch" {
		t.Fatal("fork validation must still depend on author admission")
	}
	if validation.If != "${{ !cancelled() && (github.event.action != 'edited' || github.event.changes.base.ref.from != null) && (needs.vouch.result == 'success' || (needs.vouch.result == 'skipped' && github.event.pull_request.head.repo.full_name == github.repository)) }}" {
		t.Fatal("only successful fork admission or a same-repo skipped vouch may run validation")
	}
}
