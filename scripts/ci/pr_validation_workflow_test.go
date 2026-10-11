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
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-candidate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.Name != "pr-candidate" {
		t.Fatal("explicit full admission must retain the candidate workflow identity")
	}
	if len(workflow.On) != 1 || !slices.Equal(workflow.On["pull_request"].Types, []string{"labeled"}) {
		t.Fatal("full validation must only start on explicit admission; head/base changes invalidate the proof")
	}
	if workflow.Concurrency.Group != "${{ github.event.label.name == 'ci:full' && 'candidate-ci' || 'pr-metadata' }}-${{ github.event.pull_request.number }}" {
		t.Fatal("unrelated labels must never cancel an admitted full candidate")
	}
	// The trusted default-branch controller binds full runs to this identity.
	if workflow.RunName != "${{ github.event.label.name == 'ci:full' && 'candidate-ci' || 'pr-metadata' }} #${{ github.event.pull_request.number }}" {
		t.Fatal("retain the exact run identity used by trusted candidate verification")
	}
	if len(workflow.Permissions) != 2 || workflow.Permissions["actions"] != "read" || workflow.Permissions["contents"] != "read" {
		t.Fatal("PR execution must remain read-only")
	}
	if workflow.Jobs["vouch"].If != "github.event.label.name == 'ci:full' && github.event.pull_request.base.ref == github.event.repository.default_branch && github.event.pull_request.head.repo.full_name != github.repository" {
		t.Fatal("only forks should allocate an author-vouch runner")
	}
	validation := workflow.Jobs["validation"]
	if validation.Needs != "vouch" {
		t.Fatal("fork validation must still depend on author admission")
	}
	if validation.If != "${{ !cancelled() && github.event.label.name == 'ci:full' && github.event.pull_request.base.ref == github.event.repository.default_branch && (needs.vouch.result == 'success' || (needs.vouch.result == 'skipped' && github.event.pull_request.head.repo.full_name == github.repository)) }}" {
		t.Fatal("only explicit default-branch admission with successful fork vouch or same-repo identity may validate")
	}
}
