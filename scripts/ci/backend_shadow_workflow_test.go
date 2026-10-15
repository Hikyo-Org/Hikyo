package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBackendImpactShadowCannotSelectRequiredJobs(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-impact-shadow.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]string    `yaml:"permissions"`
			Steps       []mainValidationStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if _, ok := workflow.On["merge_group"]; !ok || len(workflow.On) != 1 || len(workflow.Jobs) != 1 {
		t.Fatal("shadow work must run independently on the immutable merge candidate")
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" || len(workflow.Jobs["observe"].Permissions) != 0 {
		t.Fatal("candidate inspection must not have write permissions")
	}
	if strings.Contains(string(raw), "secrets.") || strings.Contains(string(raw), "actions/cache/") {
		t.Fatal("shadow inspection must not receive secrets or publish caches")
	}
	var prepare, analyze string
	for _, step := range workflow.Jobs["observe"].Steps {
		switch step.Name {
		case "Prepare immutable base tooling":
			prepare = step.Run
		case "Observe backend dependency impact without selecting tests":
			analyze = step.Run
		}
	}
	if !strings.Contains(prepare, `git show "$BASE_SHA:scripts/ci/impact-shadow/main.go"`) || !strings.Contains(analyze, "GO111MODULE=off go build") || !strings.Contains(analyze, "--timeout 3m") {
		t.Fatal("shadow analysis must use bounded trusted standalone tooling")
	}
	ci, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var required mainValidationWorkflow
	if err := yaml.Unmarshal(ci, &required); err != nil {
		t.Fatal(err)
	}
	for name, job := range required.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Run, "report.json") || strings.Contains(step.Run, "candidate_packages") {
				t.Fatalf("required job %s consumes advisory shadow selection", name)
			}
		}
	}
	// The rollout's base has no shadow tool yet. Candidate source must not be
	// substituted even when it offers an executable with the expected filename.
	f := newDocsQueueFixture(t, []string{"README.md"})
	f.write(t, "scripts/ci/impact-shadow/main.go", "candidate code must not execute")
	f.write(t, "go.mod", "module fixture\n")
	// Add the base Go version as an immutable tree object for this script fixture.
	f.git(t, "add", "go.mod")
	tree := f.git(t, "write-tree")
	f.base = f.git(t, "commit-tree", tree, "-p", f.candidate, "-m", "base with toolchain")
	f.candidate = f.base
	f.git(t, "checkout", "-q", "--detach", f.base)
	cmd := exec.Command("bash", "-euo", "pipefail", "-c", prepare)
	cmd.Dir = f.dir
	cmd.Env = append(f.env(t), "HEAD_SHA="+f.candidate, "GITHUB_SHA="+f.candidate)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shadow bootstrap: %v %s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(f.dir, "runner/impact-shadow/report.json"))
	if err != nil || !strings.Contains(string(data), `"authoritative":false`) || !strings.Contains(string(data), `"recommendation":"full"`) {
		t.Fatal("missing base shadow tool must retain an explicit advisory full fallback")
	}
}
