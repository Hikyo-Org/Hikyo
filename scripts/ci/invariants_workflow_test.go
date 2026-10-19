package ci_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	invariantsWorkflowPath = ".github/workflows/ci-invariants.yml"
	invariantsJobName      = "repository invariants (boundary, lint, CI policy)"
	invariantsHeadRef      = "${{ github.event.pull_request.head.sha }}"
	invariantsPackages     = "./internal/boundary ./scripts/ci"
	invariantsLintPackage  = "./internal/lint"
)

type invariantsStep struct {
	ID   string            `yaml:"id,omitempty"`
	Name string            `yaml:"name,omitempty"`
	Uses string            `yaml:"uses,omitempty"`
	With map[string]string `yaml:"with,omitempty"`
	Run  string            `yaml:"run,omitempty"`
}

// The decoder rejects unknown keys, so any trigger option, job condition,
// permission override, service, container, strategy or continue-on-error that
// the workflow does not already declare is a refusal, not silently ignored.
type invariantsWorkflow struct {
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
		Steps   []invariantsStep  `yaml:"steps"`
	} `yaml:"jobs"`
}

// invariantsReferenceSteps returns the toolchain and cache steps of ci.yml's
// test_core job, which the advisory job must mirror exactly.
func invariantsReferenceSteps(ci []byte) (map[string]invariantsStep, error) {
	var document struct {
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(ci, &document); err != nil {
		return nil, err
	}
	node, ok := document.Jobs["test_core"]
	if !ok {
		return nil, fmt.Errorf("ci.yml has no test_core job to mirror")
	}
	var core struct {
		Steps []invariantsStep `yaml:"steps"`
	}
	if err := node.Decode(&core); err != nil {
		return nil, err
	}
	reference := map[string]invariantsStep{}
	for _, step := range core.Steps {
		switch {
		case step.ID == "go-module-cache" || step.ID == "runner-cache-abi" || step.ID == "test-go-cache":
			reference[step.ID] = step
		case strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["persist-credentials"] == "false":
			reference["checkout"] = step
		case strings.HasPrefix(step.Uses, "actions/setup-go@"):
			reference["setup-go"] = step
		}
	}
	for _, key := range []string{"checkout", "setup-go", "go-module-cache", "runner-cache-abi", "test-go-cache"} {
		if _, ok := reference[key]; !ok {
			return nil, fmt.Errorf("test_core has no %s step to mirror", key)
		}
	}
	return reference, nil
}

func invariantsWorkflowErrors(raw, ci []byte) []error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }

	var workflow invariantsWorkflow
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&workflow); err != nil {
		return []error{fmt.Errorf("decode workflow: %w", err)}
	}
	reference, err := invariantsReferenceSteps(ci)
	if err != nil {
		return []error{err}
	}

	if workflow.Name != "repository-invariants" {
		fail("workflow name %q", workflow.Name)
	}
	trigger, ok := workflow.On["pull_request"]
	if len(workflow.On) != 1 || !ok {
		fail("the only trigger must be pull_request, never a privileged or chained event")
	}
	if !reflect.DeepEqual(trigger.Types, []string{"opened", "synchronize", "reopened"}) {
		fail("pull_request types %v: base edits must not start head-only tests", trigger.Types)
	}
	wantPaths := []string{
		"**.go", "go.mod", "go.sum", ".github/**", "scripts/**", "release/**",
		"internal/store/**", "internal/lint/testdata/**", ".goreleaser.yaml", "web/e2e/**", "web/playwright.config.ts",
	}
	if !reflect.DeepEqual(trigger.Paths, wantPaths) {
		fail("pull_request paths %v, want %v", trigger.Paths, wantPaths)
	}
	if !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) {
		fail("permissions %v, want contents: read only", workflow.Permissions)
	}
	if workflow.Concurrency.Group != "repository-invariants-${{ github.event.pull_request.number }}" || !workflow.Concurrency.Cancel {
		fail("concurrency must be one cancelling group per PR number")
	}

	job, ok := workflow.Jobs["invariants"]
	if len(workflow.Jobs) != 1 || !ok {
		fail("the workflow must declare exactly one job, invariants")
		return problems
	}
	if job.Name != invariantsJobName || strings.Contains(job.Name, "${{") {
		fail("job name %q must be the static %q", job.Name, invariantsJobName)
	}
	if job.Runner != "ubuntu-latest" || job.Timeout != 20 {
		fail("job must run on ubuntu-latest with a 20 minute budget, got %q and %d", job.Runner, job.Timeout)
	}
	if !reflect.DeepEqual(job.Env, map[string]string{"EXPECTED_HEAD": invariantsHeadRef}) {
		fail("job env %v, want only EXPECTED_HEAD", job.Env)
	}

	want := []invariantsStep{
		{
			Name: "Check out the exact PR head without credentials",
			Uses: reference["checkout"].Uses,
			With: map[string]string{"ref": invariantsHeadRef, "persist-credentials": "false"},
		},
		{
			Name: "Verify the checkout is the PR head",
			Run:  `test "$(git rev-parse HEAD)" = "$EXPECTED_HEAD"`,
		},
		{
			Uses: reference["setup-go"].Uses,
			With: reference["setup-go"].With,
		},
		reference["go-module-cache"],
		reference["runner-cache-abi"],
		reference["test-go-cache"],
		{
			Name: "Prepare lint compiler cache",
			Run:  "go run ./scripts/ci/prepare-lint-cache",
		},
		{
			Name: "Repository invariant tests",
			Run:  "go test -count=1 " + invariantsPackages,
		},
		{
			Name: "Lint driver tests",
			Run:  "go test -count=1 " + invariantsLintPackage,
		},
	}
	if len(job.Steps) != len(want) {
		fail("job has %d steps, want %d", len(job.Steps), len(want))
		return problems
	}
	for index := range want {
		got, expected := job.Steps[index], want[index]
		if index >= 3 && index <= 5 {
			// Display names of the mirrored steps belong to ci.yml; the job has
			// to carry the same ids, actions and inputs.
			got.Name, expected.Name = "", ""
		}
		if !reflect.DeepEqual(got, expected) {
			fail("step %d is %+v, want %+v", index+1, got, expected)
		}
	}
	if job.Steps[2].With["cache"] != "false" {
		fail("setup-go must not publish its own cache")
	}
	for _, index := range []int{3, 5} {
		if !strings.HasPrefix(job.Steps[index].Uses, "actions/cache/restore@") {
			fail("step %d must restore caches only", index+1)
		}
	}

	for _, forbidden := range []string{
		"secrets.", "GH_TOKEN", "GITHUB_TOKEN", "github.token",
		"actions/cache/save", "upload-artifact", "download-artifact",
		"pull_request_target", "workflow_run", "workflow_dispatch", "workflow_call",
		"continue-on-error", "self-hosted",
	} {
		if strings.Contains(string(raw), forbidden) {
			fail("advisory invariants must not gain authority or publication: %s", forbidden)
		}
	}
	return problems
}

func readInvariantsInputs(t *testing.T) (raw, ci []byte) {
	t.Helper()
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, invariantsWorkflowPath))
	if err != nil {
		t.Fatal(err)
	}
	ci, err = os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return raw, ci
}

func TestInvariantsWorkflowScopeAndAuthority(t *testing.T) {
	raw, ci := readInvariantsInputs(t)
	for _, problem := range invariantsWorkflowErrors(raw, ci) {
		t.Error(problem)
	}
}

func TestInvariantsWorkflowRejectsMutations(t *testing.T) {
	raw, ci := readInvariantsInputs(t)
	text := string(raw)
	// Anchors follow the workflow's own pins, so a routine pin bump does not
	// turn into a missing-anchor failure.
	pin := func(action string) string {
		match := regexp.MustCompile(`uses: (` + regexp.QuoteMeta(action) + `@[0-9a-f]{40} # \S+)`).FindStringSubmatch(text)
		if match == nil {
			t.Fatalf("workflow has no pinned %s step", action)
		}
		return match[1]
	}
	checkoutPin, restorePin := pin("actions/checkout"), pin("actions/cache/restore")
	appendStep := func(step string) func(string) string {
		return func(s string) string { return s + step }
	}
	replace := func(old, replacement string) func(string) string {
		return func(s string) string {
			if !strings.Contains(s, old) {
				t.Fatalf("mutation anchor missing from workflow: %q", old)
			}
			return strings.Replace(s, old, replacement, 1)
		}
	}

	mutations := map[string]func(string) string{
		"privileged trigger":       replace("  pull_request:\n", "  pull_request_target:\n    types: [opened]\n  pull_request:\n"),
		"chained trigger":          replace("  pull_request:\n", "  workflow_run:\n    types: [completed]\n  pull_request:\n"),
		"manual trigger":           replace("  pull_request:\n", "  workflow_dispatch:\n  pull_request:\n"),
		"edited type":              replace("types: [opened, synchronize, reopened]", "types: [opened, synchronize, reopened, edited]"),
		"missing synchronize":      replace("types: [opened, synchronize, reopened]", "types: [opened, reopened]"),
		"branch filter":            replace("    types: [opened, synchronize, reopened]\n", "    types: [opened, synchronize, reopened]\n    branches: [main]\n"),
		"dropped path":             replace("      - 'release/**'\n", ""),
		"dropped store path":       replace("      - 'internal/store/**'\n", ""),
		"dropped e2e path":         replace("      - 'web/e2e/**'\n", ""),
		"extra path":               replace("      - 'release/**'\n", "      - 'release/**'\n      - 'docs/**'\n"),
		"write permission":         replace("  contents: read\n", "  contents: write\n"),
		"extra permission":         replace("  contents: read\n", "  contents: read\n  pull-requests: write\n"),
		"job permission override":  replace("    timeout-minutes: 20\n", "    timeout-minutes: 20\n    permissions:\n      contents: write\n"),
		"secret in environment":    replace("    env:\n", "    env:\n      TOKEN: ${{ secrets.CI_TOKEN }}\n"),
		"token in environment":     replace("    env:\n", "    env:\n      GH_TOKEN: ${{ github.token }}\n"),
		"other runner":             replace("runs-on: ubuntu-latest", "runs-on: ubuntu-24.04"),
		"self-hosted runner":       replace("runs-on: ubuntu-latest", "runs-on: self-hosted"),
		"dynamic job name":         replace("name: "+invariantsJobName, "name: "+invariantsJobName+" ${{ github.event.action }}"),
		"renamed job":              replace("name: "+invariantsJobName, "name: invariants"),
		"conditional job":          replace("    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-latest\n    if: github.event.action != 'edited'\n"),
		"continue on error":        replace("    timeout-minutes: 20\n", "    timeout-minutes: 20\n    continue-on-error: true\n"),
		"longer budget":            replace("timeout-minutes: 20", "timeout-minutes: 60"),
		"no cancellation":          replace("cancel-in-progress: true", "cancel-in-progress: false"),
		"shared concurrency group": replace("repository-invariants-${{ github.event.pull_request.number }}", "repository-invariants"),
		"extra job":                appendStep("  other:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n"),
		"unpinned checkout":        replace(checkoutPin, "actions/checkout@v7"),
		"drifted checkout pin":     replace(checkoutPin, "actions/checkout@0000000000000000000000000000000000000000 # v7.0.1"),
		"merge ref checkout":       replace("ref: "+invariantsHeadRef, "ref: ${{ github.sha }}"),
		"persisted credentials":    replace("persist-credentials: false", "persist-credentials: true"),
		"deep checkout":            replace("persist-credentials: false", "persist-credentials: false\n          fetch-depth: 0"),
		"unverified head":          replace(`run: test "$(git rev-parse HEAD)" = "$EXPECTED_HEAD"`, "run: 'true'"),
		"setup-go caches":          replace("cache: false", "cache: true"),
		"module cache writer":      replace(restorePin, strings.Replace(restorePin, "cache/restore", "cache", 1)),
		"drifted restore pin":      replace(restorePin, "actions/cache/restore@0000000000000000000000000000000000000000 # v6.1.0"),
		"module key version":       replace("key: go-mod-v2-", "key: go-mod-v3-"),
		"module restore prefix":    replace("restore-keys: go-mod-v2-${{ runner.os }}-\n", "restore-keys: go-mod-v2-\n"),
		"module key inputs":        replace("hashFiles('go.mod', 'go.sum', 'scripts/ci/go-tool-modules.txt')", "hashFiles('go.mod')"),
		"test key version":         replace("key: go-test-v2-", "key: go-test-v1-"),
		"test key without ABI":     replace("-${{ steps.runner-cache-abi.outputs.value }}-${{ hashFiles(", "-${{ hashFiles("),
		"test restore prefix":      replace("            go-test-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-\n", "            go-test-v2-${{ runner.os }}-\n"),
		"run id in cache key":      replace("key: go-test-v2-${{ runner.os }}", "key: go-test-v2-${{ github.run_id }}-${{ runner.os }}"),
		"cache save step":          appendStep("      - uses: " + cacheSaveAction + " # v6.1.0\n        with:\n          path: ~/.cache/go-build\n          key: go-test-v2\n"),
		"artifact upload":          appendStep("      - uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02 # v4.6.2\n        with:\n          path: .\n"),
		"skipped lint cache":       replace("      - name: Prepare lint compiler cache\n        run: go run ./scripts/ci/prepare-lint-cache\n", ""),
		"dropped boundary package": replace(" ./internal/boundary", ""),
		"dropped lint package":     replace(" ./internal/lint", ""),
		"lint shares the pool":     replace(" ./internal/boundary ./scripts/ci", " ./internal/boundary ./internal/lint ./scripts/ci"),
		"dropped CI policy":        replace(" ./scripts/ci\n", "\n"),
		"extra package":            replace(" ./scripts/ci\n", " ./scripts/ci ./internal/isolation\n"),
		"cached test results":      replace("go test -count=1 ", "go test "),
		"narrowed tests":           replace("go test -count=1 ", "go test -count=1 -run TestForbiddenEdges "),
		"PostgreSQL service":       replace("    env:\n", "    services:\n      postgres:\n        image: postgres\n    env:\n"),
		"workflow level secrets":   replace("permissions:\n  contents: read\n", "env:\n  TOKEN: ${{ secrets.CI_TOKEN }}\npermissions:\n  contents: read\n"),
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			mutated := mutate(text)
			if mutated == text {
				t.Fatal("mutation did not change the workflow")
			}
			problems := invariantsWorkflowErrors([]byte(mutated), ci)
			if len(problems) == 0 {
				t.Fatalf("mutated workflow was accepted:\n%s", mutated)
			}
			t.Logf("refused: %v", problems[0])
		})
	}

	// The cache readers are compared with ci.yml's test_core job, not with a
	// fixed copy, so drift in either file is a refusal.
	for name, drift := range map[string]string{
		"test_core module key":    "go-mod-v2-${{ runner.os }}-${{ hashFiles(",
		"test_core test key":      "go-test-v2-${{ runner.os }}-${{ runner.arch }}",
		"test_core setup-go form": "go-version-file: go.mod # exact toolchain",
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(string(ci), drift) {
				t.Fatalf("drift anchor missing from ci.yml: %q", drift)
			}
			driftedCI := strings.ReplaceAll(string(ci), drift, strings.Replace(drift, "v2", "v9", 1))
			if name == "test_core setup-go form" {
				driftedCI = strings.ReplaceAll(string(ci), drift, "go-version-file: go.work # exact toolchain")
			}
			if len(invariantsWorkflowErrors(raw, []byte(driftedCI))) == 0 {
				t.Fatal("workflow was accepted although test_core's reader or toolchain changed")
			}
		})
	}
	t.Run("test_core missing", func(t *testing.T) {
		if len(invariantsWorkflowErrors(raw, []byte("jobs:\n  other:\n    runs-on: ubuntu-latest\n"))) == 0 {
			t.Fatal("workflow was accepted without a test_core job to mirror")
		}
	})
}

func TestInvariantsWorkflowIsNotARequiredContext(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"main-ci-gate.json", "main-ci-gate-legacy.json"} {
		raw, err := os.ReadFile(filepath.Join(root, "release", "repository", name))
		if err != nil {
			t.Fatal(err)
		}
		var ruleset struct {
			Rules []struct {
				Parameters struct {
					Checks []struct {
						Context string `json:"context"`
					} `json:"required_status_checks"`
				} `json:"parameters"`
			} `json:"rules"`
		}
		if err := json.Unmarshal(raw, &ruleset); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, rule := range ruleset.Rules {
			for _, check := range rule.Parameters.Checks {
				if strings.Contains(check.Context, "invariants") {
					t.Fatalf("%s requires %q: the early invariant signal is advisory and the merge queue owns the complete suite", name, check.Context)
				}
			}
		}
	}
}
