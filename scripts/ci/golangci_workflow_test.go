package ci_test

import (
	"bytes"
	"fmt"
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

// golangciConfig is the whole of .golangci.yml; unknown keys are rejected so
// no setting can be added without this test noticing.
type golangciConfig struct {
	Version string `yaml:"version"`
	Linters struct {
		Default  string   `yaml:"default"`
		Enable   []string `yaml:"enable"`
		Settings struct {
			Staticcheck struct {
				Checks []string `yaml:"checks"`
			} `yaml:"staticcheck"`
			Gosec struct {
				Includes []string `yaml:"includes"`
			} `yaml:"gosec"`
		} `yaml:"settings"`
		Exclusions struct {
			Rules []struct {
				Path    string   `yaml:"path"`
				Linters []string `yaml:"linters"`
			} `yaml:"rules"`
		} `yaml:"exclusions"`
	} `yaml:"linters"`
	Issues struct {
		// Pointers: a missing key falls back to golangci-lint's caps (50 and 3),
		// which must not read as the pinned 0.
		MaxPerLinter *int `yaml:"max-issues-per-linter"`
		MaxSame      *int `yaml:"max-same-issues"`
	} `yaml:"issues"`
}

func golangciConfigProblems(raw string) []string {
	var config golangciConfig
	decoder := yaml.NewDecoder(strings.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return []string{"decode: " + err.Error()}
	}
	var problems []string
	check := func(name string, got, want any) {
		if !reflect.DeepEqual(got, want) {
			problems = append(problems, fmt.Sprintf("%s is %v, want %v", name, got, want))
		}
	}
	enabled := slices.Clone(config.Linters.Enable)
	slices.Sort(enabled)
	check("version", config.Version, "2")
	check("linters.default", config.Linters.Default, "none")
	check("linters.enable", enabled, []string{"errcheck", "errchkjson", "errorlint", "gosec", "noctx", "staticcheck", "unused", "wastedassign"})
	// A configured list replaces staticcheck's default of SA, S, ST and QF.
	check("staticcheck.checks", config.Linters.Settings.Staticcheck.Checks, []string{"SA*", "QF1001", "QF1003"})
	check("gosec.includes", config.Linters.Settings.Gosec.Includes, []string{"G115", "G204", "G306"})
	check("exclusion rules", len(config.Linters.Exclusions.Rules), 1)
	if len(config.Linters.Exclusions.Rules) == 1 {
		check("exclusion path", config.Linters.Exclusions.Rules[0].Path, `_test\.go`)
		check("exclusion linters", config.Linters.Exclusions.Rules[0].Linters, []string{"gosec"})
	}
	uncapped := func(name string, got *int) {
		if got == nil || *got != 0 {
			problems = append(problems, name+" must be set to 0 (no cap)")
		}
	}
	uncapped("max-issues-per-linter", config.Issues.MaxPerLinter)
	uncapped("max-same-issues", config.Issues.MaxSame)
	return problems
}

func TestGolangciConfigIsPinned(t *testing.T) {
	raw := readRepoFile(t, ".golangci.yml")
	for _, problem := range golangciConfigProblems(raw) {
		t.Error(problem)
	}
	for name, mutate := range map[string]func(string) string{
		"extra linter":   func(s string) string { return strings.Replace(s, "    - gosec\n", "    - gosec\n    - revive\n", 1) },
		"dropped linter": func(s string) string { return strings.Replace(s, "    - noctx\n", "", 1) },
		"default staticcheck": func(s string) string {
			return strings.Replace(s, `      checks: ["SA*", "QF1001", "QF1003"]`+"\n", "", 1)
		},
		"widened staticcheck": func(s string) string { return strings.Replace(s, `"QF1003"`, `"QF*"`, 1) },
		"widened gosec":       func(s string) string { return strings.Replace(s, "[G115, G204, G306]", "[G115, G204, G306, G104]", 1) },
		"gosec on tests":      func(s string) string { return strings.Replace(s, "linters: [gosec]", "linters: [unused]", 1) },
		"default linters on":  func(s string) string { return strings.Replace(s, "default: none", "default: standard", 1) },
		"issue cap": func(s string) string {
			return strings.Replace(s, "max-issues-per-linter: 0", "max-issues-per-linter: 50", 1)
		},
		"per-linter cap dropped": func(s string) string { return strings.Replace(s, "  max-issues-per-linter: 0\n", "", 1) },
		"same-issue cap dropped": func(s string) string { return strings.Replace(s, "  max-same-issues: 0\n", "", 1) },
		"formatters section":     func(s string) string { return s + "formatters:\n  enable: [gofmt]\n" },
	} {
		mutated := mutate(raw)
		if mutated == raw {
			t.Fatalf("%s: mutation did not change the config", name)
		}
		if len(golangciConfigProblems(mutated)) == 0 {
			t.Errorf("%s: config drift went undetected", name)
		}
	}
}

// golangciStrictConfig is the whole of .golangci-strict.yml, the zero-baseline
// rules the script runs on whole changed packages; unknown keys are rejected.
type golangciStrictConfig struct {
	Version string `yaml:"version"`
	Linters struct {
		Default  string   `yaml:"default"`
		Enable   []string `yaml:"enable"`
		Settings struct {
			Staticcheck struct {
				Checks []string `yaml:"checks"`
			} `yaml:"staticcheck"`
		} `yaml:"settings"`
	} `yaml:"linters"`
	Issues struct {
		MaxPerLinter *int `yaml:"max-issues-per-linter"`
		MaxSame      *int `yaml:"max-same-issues"`
	} `yaml:"issues"`
}

func golangciStrictConfigProblems(raw string) []string {
	var config golangciStrictConfig
	decoder := yaml.NewDecoder(strings.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return []string{"decode: " + err.Error()}
	}
	var problems []string
	check := func(name string, got, want any) {
		if !reflect.DeepEqual(got, want) {
			problems = append(problems, fmt.Sprintf("%s is %v, want %v", name, got, want))
		}
	}
	enabled := slices.Clone(config.Linters.Enable)
	slices.Sort(enabled)
	check("version", config.Version, "2")
	check("linters.default", config.Linters.Default, "none")
	check("linters.enable", enabled, []string{"staticcheck", "unused", "wastedassign"})
	check("staticcheck.checks", config.Linters.Settings.Staticcheck.Checks, []string{"SA4006", "SA4009"})
	uncapped := func(name string, got *int) {
		if got == nil || *got != 0 {
			problems = append(problems, name+" must be set to 0 (no cap)")
		}
	}
	uncapped("max-issues-per-linter", config.Issues.MaxPerLinter)
	uncapped("max-same-issues", config.Issues.MaxSame)
	return problems
}

func TestGolangciStrictConfigIsPinned(t *testing.T) {
	raw := readRepoFile(t, ".golangci-strict.yml")
	for _, problem := range golangciStrictConfigProblems(raw) {
		t.Error(problem)
	}
	for name, mutate := range map[string]func(string) string{
		"extra linter": func(s string) string {
			return strings.Replace(s, "    - unused\n", "    - unused\n    - errcheck\n", 1)
		},
		"dropped linter":     func(s string) string { return strings.Replace(s, "    - wastedassign\n", "", 1) },
		"default checks":     func(s string) string { return strings.Replace(s, `      checks: ["SA4006", "SA4009"]`+"\n", "", 1) },
		"added check":        func(s string) string { return strings.Replace(s, `"SA4009"`, `"SA4009", "SA1019"`, 1) },
		"dropped check":      func(s string) string { return strings.Replace(s, `"SA4006", `, "", 1) },
		"default linters on": func(s string) string { return strings.Replace(s, "default: none", "default: standard", 1) },
		"issue cap": func(s string) string {
			return strings.Replace(s, "max-issues-per-linter: 0", "max-issues-per-linter: 50", 1)
		},
		"same-issue cap changed": func(s string) string { return strings.Replace(s, "max-same-issues: 0", "max-same-issues: 3", 1) },
		"per-linter cap dropped": func(s string) string { return strings.Replace(s, "  max-issues-per-linter: 0\n", "", 1) },
		"same-issue cap dropped": func(s string) string { return strings.Replace(s, "  max-same-issues: 0\n", "", 1) },
		"unknown key":            func(s string) string { return s + "formatters:\n  enable: [gofmt]\n" },
	} {
		mutated := mutate(raw)
		if mutated == raw {
			t.Fatalf("%s: mutation did not change the config", name)
		}
		if len(golangciStrictConfigProblems(mutated)) == 0 {
			t.Errorf("%s: config drift went undetected", name)
		}
	}
}
