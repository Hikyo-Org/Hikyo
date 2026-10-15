package ci_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDependencyCompatibilityWorkflowScopeAndAuthority(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-dependency-compatibility.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On map[string]struct {
			Types []string `yaml:"types"`
			Paths []string `yaml:"paths"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			If       string            `yaml:"if"`
			Timeout  int               `yaml:"timeout-minutes"`
			Continue bool              `yaml:"continue-on-error"`
			Env      map[string]string `yaml:"env"`
			Steps    []struct {
				Uses      string            `yaml:"uses"`
				Run       string            `yaml:"run"`
				With      map[string]string `yaml:"with"`
				Directory string            `yaml:"working-directory"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	trigger := workflow.On["pull_request"]
	if len(workflow.On) != 1 || !reflect.DeepEqual(trigger.Types, []string{"opened", "synchronize", "reopened", "edited"}) || !reflect.DeepEqual(trigger.Paths, []string{"go.mod", "go.sum"}) {
		t.Fatal("compatibility must automatically run for PR dependency selection changes with no privileged event")
	}
	if !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) {
		t.Fatal("dependency consumers must execute with read-only permission")
	}
	job := workflow.Jobs["compatibility"]
	if len(workflow.Jobs) != 1 || job.Timeout != 20 || job.Continue || job.If != "github.event.action != 'edited' || github.event.changes.base.ref.from != ''" {
		t.Fatal("compatibility needs its explicit full-build budget and must propagate real failures")
	}
	if job.Env["GOCACHE"] != "${{ github.workspace }}/.dependency-compatibility" || job.Env["GOFLAGS"] != "-mod=readonly" || job.Env["FAST_FEEDBACK_DEADLINE"] != "" {
		t.Fatal("compatibility must have separate temporary compiler state and timing")
	}
	if len(job.Steps) != 5 {
		t.Fatal("unexpected dependency compatibility steps")
	}
	for index, ref := range []string{"${{ github.sha }}", "${{ github.event.pull_request.head.sha }}"} {
		step := job.Steps[index]
		if !strings.HasPrefix(step.Uses, "actions/checkout@") || step.With["ref"] != ref || step.With["persist-credentials"] != "false" {
			t.Fatal("tooling and source must be immutable and have no checkout credentials")
		}
	}
	if !strings.Contains(job.Steps[2].Run, `test "$(git -C source rev-parse HEAD)" = "$EXPECTED_HEAD"`) || !strings.Contains(job.Steps[2].Run, `test "$(git -C compatibility-tools rev-parse HEAD^2)" = "$EXPECTED_HEAD"`) {
		t.Fatal("the inspected source and immutable integration tooling must identify the exact PR head")
	}
	if job.Steps[3].With["cache"] != "false" || job.Steps[3].With["go-version-file"] != "source/go.mod" || job.Steps[4].Directory != "source" || job.Steps[4].Run != "bash ../compatibility-tools/scripts/ci/dependency-compatibility.sh" {
		t.Fatal("all dependency consumers need the selected Go toolchain with no implicit cache publisher")
	}
	for _, forbidden := range []string{"secrets.", "GH_TOKEN", "actions/cache", "upload-artifact", "download-artifact", "pull_request_target", "continue-on-error"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("compatibility must not gain publication authority: %s", forbidden)
		}
	}
}
