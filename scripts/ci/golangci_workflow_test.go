package ci_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	golangciScript   = "scripts/ci/golangci-lint-new.sh"
	golangciWorkflow = ".github/workflows/ci-golangci.yml"
	golangciPin      = "scripts/ci/golangci-lint-version"
)

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), path))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// quotedList returns the words of the single-quoted shell assignment name='...'.
func quotedList(t *testing.T, source, name string) []string {
	t.Helper()
	match := regexp.MustCompile(`(?m)^\s*` + name + `='([^']+)'$`).FindStringSubmatch(source)
	if match == nil {
		t.Fatalf("no %s list", name)
	}
	return strings.Fields(match[1])
}

func TestGolangciAdvisoryWorkflow(t *testing.T) {
	raw := []byte(readRepoFile(t, golangciWorkflow))

	// Unknown keys are rejected: no extra trigger option, permission override,
	// service or continue-on-error goes unnoticed.
	var workflow struct {
		Name string `yaml:"name"`
		On   map[string]struct {
			Types []string `yaml:"types"`
			Paths []string `yaml:"paths"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			Name    string            `yaml:"name"`
			Runner  string            `yaml:"runs-on"`
			Timeout int               `yaml:"timeout-minutes"`
			Env     map[string]string `yaml:"env"`
			Steps   []storybookStep   `yaml:"steps"`
		} `yaml:"jobs"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&workflow); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}
	if workflow.Name != "ci-advisory-golangci" {
		t.Errorf("workflow name %q", workflow.Name)
	}
	trigger, ok := workflow.On["pull_request"]
	if len(workflow.On) != 1 || !ok {
		t.Fatal("the only trigger must be pull_request, never a privileged or chained event")
	}
	if !reflect.DeepEqual(trigger.Types, []string{"opened", "synchronize", "reopened"}) {
		t.Errorf("pull_request types %v", trigger.Types)
	}
	if !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) {
		t.Errorf("permissions %v, want contents: read only", workflow.Permissions)
	}
	if workflow.Concurrency.Group != "advisory-golangci-${{ github.event.pull_request.number }}" || !workflow.Concurrency.Cancel {
		t.Error("concurrency must be one cancelling group per PR number")
	}
	job, ok := workflow.Jobs["golangci"]
	if len(workflow.Jobs) != 1 || !ok {
		t.Fatal("the workflow must declare exactly one job, golangci")
	}
	if job.Runner != "ubuntu-latest" || job.Timeout <= 0 || job.Timeout > 20 {
		t.Errorf("job runs on %q for %d minutes", job.Runner, job.Timeout)
	}
	wantEnv := map[string]string{
		"EXPECTED_HEAD": "${{ github.event.pull_request.head.sha }}",
		"BASE_REF":      "${{ github.event.pull_request.base.ref }}",
	}
	if !reflect.DeepEqual(job.Env, wantEnv) {
		t.Errorf("job env %v, want %v", job.Env, wantEnv)
	}

	// The last step runs the script against the PR base; the PR head, not a
	// merge ref, is what gets checked out and verified.
	steps := job.Steps
	if len(steps) == 0 || steps[len(steps)-1].Run != `./scripts/ci/golangci-lint-new.sh "origin/${BASE_REF}"` {
		t.Error("the final step must run golangci-lint-new.sh against origin/${BASE_REF}")
	}
	if steps[0].With["ref"] != wantEnv["EXPECTED_HEAD"] || steps[0].With["persist-credentials"] != "false" || steps[0].With["fetch-depth"] != "0" {
		t.Errorf("checkout step %+v must take the PR head with full history and no credentials", steps[0])
	}
	restores := 0
	for _, step := range steps {
		switch {
		case strings.HasPrefix(step.Uses, "actions/cache/restore@"):
			restores++
		case strings.HasPrefix(step.Uses, "actions/cache"):
			t.Errorf("step %q uses %s; only actions/cache/restore may touch caches", step.Name, step.Uses)
		case strings.HasPrefix(step.Uses, "actions/setup-go@"):
			// setup-go saves a cache at the end of the job unless told not to.
			if step.With["cache"] != "false" {
				t.Errorf("setup-go must set cache: false, got %q", step.With["cache"])
			}
		}
	}
	if restores != 2 {
		t.Errorf("want exactly the module and build cache restores, got %d restore steps", restores)
	}

	for _, forbidden := range []string{
		"secrets.", "GH_TOKEN", "GITHUB_TOKEN", "github.token",
		"actions/cache/save", "actions/cache@", "upload-artifact", "download-artifact",
		"pull_request_target", "workflow_run", "workflow_dispatch", "workflow_call",
		"continue-on-error", "self-hosted", "cache: true",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("advisory golangci must not gain authority or save any cache: %s", forbidden)
		}
	}
}

// The script lints every package when one of these changes; the workflow
// triggers on them plus every Go file and itself; the hook gates on them.
func TestGolangciInputListsAgree(t *testing.T) {
	script := quotedList(t, readRepoFile(t, golangciScript), "everything")
	hook := quotedList(t, readRepoFile(t, ".githooks/pre-push"), "golangci_paths")
	var workflow struct {
		On struct {
			PullRequest struct {
				Paths []string `yaml:"paths"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal([]byte(readRepoFile(t, golangciWorkflow)), &workflow); err != nil {
		t.Fatal(err)
	}
	want := append([]string{"**/*.go", golangciWorkflow}, script...)
	got := slices.Clone(workflow.On.PullRequest.Paths)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		t.Errorf("workflow triggers on %v, want the script's lint-everything list plus **/*.go and itself: %v", got, want)
	}
	if !slices.Equal(script, hook) {
		t.Errorf("hook gates on %v but the script lints everything for %v", hook, script)
	}
	for _, path := range script {
		if _, err := os.Stat(filepath.Join(repositoryRoot(t), path)); err != nil {
			t.Errorf("lint-everything input %s: %v", path, err)
		}
	}
}

// The ratchet stays advisory: no required registry, gate, job or fast lane
// mentions it, and its pin stays out of the file that keys every Go cache.
func TestGolangciIsNotRequired(t *testing.T) {
	for _, path := range []string{
		"release/repository/main-ci-gate.json",
		"scripts/ci/ci-job-registry.json",
		".github/workflows/ci.yml",
		".github/workflows/ci-feedback.yml",
		".github/workflows/ci-merge-group.yml",
		"scripts/ci/fast-feedback.sh",
		"scripts/ci/go-tool-modules.txt",
	} {
		text := readRepoFile(t, path)
		for _, banned := range []string{"golangci", "advisory-golangci"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s mentions %q; the ratchet must stay advisory and out of cache keys", path, banned)
			}
		}
	}
}

func TestGolangciPinIsOneSemver(t *testing.T) {
	pin := readRepoFile(t, golangciPin)
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+\n$`).MatchString(pin) {
		t.Errorf("%s must hold exactly one vMAJOR.MINOR.PATCH line, got %q", golangciPin, pin)
	}
	runner := readRepoFile(t, "scripts/ci/run-go-tool.sh")
	if !strings.Contains(runner, "module=github.com/golangci/golangci-lint/v2") {
		t.Error("run-go-tool.sh does not know the golangci-lint package")
	}
	if !regexp.MustCompile(`(?m)^version: "2"$`).MatchString(readRepoFile(t, ".golangci.yml")) {
		t.Error(".golangci.yml must use the v2 schema")
	}
}

func TestGolangciPackageSelectionFixture(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "sh", filepath.Join(repositoryRoot(t), "scripts/ci/golangci-lint-new_test.sh")).CombinedOutput()
	if err != nil {
		t.Fatalf("golangci-lint-new fixture failed: %v\n%s", err, output)
	}
}
