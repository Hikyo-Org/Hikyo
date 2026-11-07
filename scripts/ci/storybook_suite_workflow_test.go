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
	storybookSuiteScript    = "scripts/ci/storybook-suite.sh"
	storybookAdvisoryPath   = ".github/workflows/ci-storybook.yml"
	storybookInstallClient  = "Install client package (generated sources' own deps)"
	storybookDesignImages   = "Every linked design image ships in the static build"
	storybookPlaywrightPins = "mcr.microsoft.com/playwright:"
)

type storybookStep struct {
	ID               string            `yaml:"id,omitempty"`
	Name             string            `yaml:"name,omitempty"`
	Uses             string            `yaml:"uses,omitempty"`
	With             map[string]string `yaml:"with,omitempty"`
	Run              string            `yaml:"run,omitempty"`
	WorkingDirectory string            `yaml:"working-directory,omitempty"`
}

type storybookJob struct {
	Name      string `yaml:"name"`
	Runner    string `yaml:"runs-on"`
	Container struct {
		Image string `yaml:"image"`
	} `yaml:"container"`
	Timeout int               `yaml:"timeout-minutes"`
	Env     map[string]string `yaml:"env"`
	Steps   []storybookStep   `yaml:"steps"`
}

// storybookCISteps returns the ci.yml storybook job's steps and its container image.
func storybookCISteps(t *testing.T, ci []byte) storybookJob {
	t.Helper()
	var document struct {
		Jobs map[string]storybookJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(ci, &document); err != nil {
		t.Fatal(err)
	}
	job, ok := document.Jobs["storybook"]
	if !ok {
		t.Fatal("ci.yml has no storybook job")
	}
	return job
}

// storybookCICommands flattens the suite portion of the job (from the client
// install through the design image loop) into "DIR: COMMAND" lines, with the
// final loop returned separately as its trimmed lines.
func storybookCICommands(t *testing.T, job storybookJob) (commands, loop []string) {
	t.Helper()
	started := false
	for _, step := range job.Steps {
		if step.Name == storybookInstallClient {
			started = true
		}
		if !started || step.Run == "" {
			continue
		}
		lines := nonBlankTrimmed(step.Run)
		if step.Name == storybookDesignImages {
			return commands, lines
		}
		for _, line := range lines {
			commands = append(commands, step.WorkingDirectory+": "+line)
		}
	}
	t.Fatalf("ci.yml storybook job has no %q step after %q", storybookDesignImages, storybookInstallClient)
	return nil, nil
}

func nonBlankTrimmed(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// storybookStepFunction is the pinned body of the script's step() helper.
var storybookStepFunction = []string{
	"step() {",
	"dir=$1",
	"shift",
	`printf '==> %s: %s\n' "$dir" "$*" >&2`,
	`(cd "$dir" && "$@")`,
	"}",
}

const storybookLoopBanner = `printf '==> web: every linked design image ships in the static build\n' >&2`

func scriptCodeLines(script string) []string {
	var lines []string
	for _, line := range nonBlankTrimmed(script) {
		if !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

// storybookScriptProblems requires everything from the step() helper on to be
// exactly: the pinned helper, one `step DIR COMMAND...` line per ci.yml command
// and the closing design image loop. Only the prelude (toolchain and
// environment checks) before the helper is free, and it may not run suite
// commands.
func storybookScriptProblems(script string, wantCommands, wantLoop []string) []string {
	var problems []string
	lines := scriptCodeLines(script)
	start := slices.Index(lines, storybookStepFunction[0])
	if start < 0 {
		return []string{"script has no step() helper"}
	}
	if lines[0] != "set -eu" {
		problems = append(problems, "the first code line must be `set -eu`, got: "+lines[0])
	}
	// The prelude may define only fail(), the one place that exits.
	functionDefinition := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*\(\)`)
	exits := regexp.MustCompile(`\bexit\b`)
	inFail := false
	for index, line := range lines[:start] {
		switch {
		case line == "fail() {":
			inFail = true
			continue
		case inFail && line == "}":
			inFail = false
			continue
		}
		switch {
		case strings.HasPrefix(line, "step ") || strings.HasPrefix(line, "pnpm ") || strings.HasPrefix(line, "(cd "):
			problems = append(problems, "the prelude runs a suite command: "+line)
		case functionDefinition.MatchString(line):
			problems = append(problems, "the prelude defines a function other than fail(): "+line)
		case !inFail && exits.MatchString(line):
			problems = append(problems, "the prelude exits outside fail(): "+line)
		case strings.HasPrefix(line, "alias ") || (index > 0 && strings.HasPrefix(line, "set ")):
			problems = append(problems, "the prelude changes shell behaviour: "+line)
		}
	}
	rest := lines[start:]
	if len(rest) < len(storybookStepFunction) || !slices.Equal(rest[:len(storybookStepFunction)], storybookStepFunction) {
		problems = append(problems, "the step() helper differs from the pinned body")
		return problems
	}
	rest = rest[len(storybookStepFunction):]
	stepLine := regexp.MustCompile(`^step (\S+) (.+)$`)
	var gotCommands []string
	for len(rest) > 0 {
		match := stepLine.FindStringSubmatch(rest[0])
		if match == nil {
			break
		}
		gotCommands = append(gotCommands, match[1]+": "+match[2])
		rest = rest[1:]
	}
	if !slices.Equal(gotCommands, wantCommands) {
		problems = append(problems, fmt.Sprintf("script steps\n  %v\ndiffer from the ci.yml storybook job\n  %v", gotCommands, wantCommands))
	}
	wantTail := append([]string{storybookLoopBanner, "cd web"}, wantLoop...)
	if len(wantLoop) == 0 || !slices.Equal(rest, wantTail) {
		problems = append(problems, fmt.Sprintf("script ends with\n  %v\nbut ci.yml's design image check needs\n  %v", rest, wantTail))
	}
	return problems
}

// storybookCIStrict rejects anything on the mirrored suite steps that the
// script cannot reproduce: env, if, shell, continue-on-error, timeouts, a
// custom shell, or any other key but name, run and working-directory.
func storybookCIStrict(ci []byte) error {
	var document struct {
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(ci, &document); err != nil {
		return err
	}
	node, ok := document.Jobs["storybook"]
	if !ok {
		return fmt.Errorf("ci.yml has no storybook job")
	}
	var job struct {
		Steps []yaml.Node `yaml:"steps"`
	}
	if err := node.Decode(&job); err != nil {
		return err
	}
	// Job-level env and defaults would change every step without the script knowing.
	var settings map[string]yaml.Node
	if err := node.Decode(&settings); err != nil {
		return err
	}
	for _, key := range []string{"env", "defaults"} {
		if _, ok := settings[key]; ok {
			return fmt.Errorf("the ci.yml storybook job sets %s, which the script cannot mirror", key)
		}
	}
	type suiteStep struct {
		Name             string `yaml:"name"`
		Run              string `yaml:"run"`
		WorkingDirectory string `yaml:"working-directory"`
	}
	inSuite := false
	for _, stepNode := range job.Steps {
		var probe struct {
			Name string `yaml:"name"`
		}
		if err := stepNode.Decode(&probe); err != nil {
			return err
		}
		if probe.Name == storybookInstallClient {
			inSuite = true
		}
		if !inSuite {
			continue
		}
		raw, err := yaml.Marshal(&stepNode)
		if err != nil {
			return err
		}
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		decoder.KnownFields(true)
		var step suiteStep
		if err := decoder.Decode(&step); err != nil {
			return fmt.Errorf("suite step %q has settings the script cannot mirror: %w", probe.Name, err)
		}
		if probe.Name == storybookDesignImages {
			return nil
		}
	}
	return fmt.Errorf("ci.yml storybook job has no %q step", storybookDesignImages)
}

func readStorybookInputs(t *testing.T) (ci, script []byte) {
	t.Helper()
	root := repositoryRoot(t)
	ci, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	script, err = os.ReadFile(filepath.Join(root, storybookSuiteScript))
	if err != nil {
		t.Fatal(err)
	}
	return ci, script
}

func TestStorybookSuiteScriptMatchesCIJob(t *testing.T) {
	ci, script := readStorybookInputs(t)
	if err := storybookCIStrict(ci); err != nil {
		t.Fatal(err)
	}
	wantCommands, wantLoop := storybookCICommands(t, storybookCISteps(t, ci))
	for _, problem := range storybookScriptProblems(string(script), wantCommands, wantLoop) {
		t.Error(problem)
	}
}

func TestStorybookSuiteScriptDriftIsDetected(t *testing.T) {
	ci, script := readStorybookInputs(t)
	wantCommands, wantLoop := storybookCICommands(t, storybookCISteps(t, ci))
	for name, mutate := range map[string]func(string) string{
		"dropped light run": func(s string) string { return strings.Replace(s, "step web pnpm run test-storybook:light\n", "", 1) },
		"reordered builds": func(s string) string {
			return strings.Replace(s, "step web pnpm exec storybook build\n", "", 1) + "step web pnpm exec storybook build\n"
		},
		"wrong directory": func(s string) string {
			return strings.Replace(s, "step web pnpm run test:demo", "step . pnpm run test:demo", 1)
		},
		"extra command between steps": func(s string) string {
			return strings.Replace(s, "step web pnpm run design:export\n", "step web pnpm run design:export\nrm -rf web/node_modules\n", 1)
		},
		"extra command after the loop": func(s string) string { return s + "true\n" },
		"suite command in the prelude": func(s string) string {
			return strings.Replace(s, "step() {", "pnpm run lint\nstep() {", 1)
		},
		"errexit dropped": func(s string) string { return strings.Replace(s, "set -eu\n", "set -u\n", 1) },
		"early exit":      func(s string) string { return strings.Replace(s, "cd \"$(dirname", "exit 0\ncd \"$(dirname", 1) },
		"shadowed pnpm":   func(s string) string { return strings.Replace(s, "step() {", "pnpm() { :; }\nstep() {", 1) },
		"errexit off":     func(s string) string { return strings.Replace(s, "step() {", "set +e\nstep() {", 1) },
		"altered helper": func(s string) string {
			return strings.Replace(s, `(cd "$dir" && "$@")`, `(cd "$dir" && "$@" || true)`, 1)
		},
		"altered loop": func(s string) string { return strings.Replace(s, "exit 1; }", "true; }", 1) },
		"dropped loop": func(s string) string { return strings.Replace(s, "cd web\n", "", 1) },
		"inline suppress": func(s string) string {
			return strings.Replace(s, "step web pnpm run test:demo", "step web pnpm run test:demo || true", 1)
		},
	} {
		mutated := mutate(string(script))
		if mutated == string(script) {
			t.Fatalf("%s: mutation did not change the script", name)
		}
		if len(storybookScriptProblems(mutated, wantCommands, wantLoop)) == 0 {
			t.Errorf("%s: drift went undetected", name)
		}
	}

	// Job-level settings on the reference job are refused too.
	for name, extra := range map[string]string{
		"job env":      "    env:\n      CI: ''\n",
		"job defaults": "    defaults:\n      run:\n        shell: sh\n",
	} {
		anchor := "  storybook:\n    needs: [changes]\n"
		if !strings.Contains(string(ci), anchor) {
			t.Fatalf("anchor missing from ci.yml")
		}
		if storybookCIStrict([]byte(strings.Replace(string(ci), anchor, anchor+extra, 1))) == nil {
			t.Errorf("%s on the storybook job was accepted", name)
		}
	}

	// The reference side: settings the script cannot reproduce are refused.
	for name, extra := range map[string]string{
		"env":               "        env:\n          CI: ''\n",
		"if":                "        if: always()\n",
		"shell":             "        shell: sh\n",
		"continue-on-error": "        continue-on-error: true\n",
	} {
		anchor := "      - name: Storybook story tests (light)\n        working-directory: web\n"
		if !strings.Contains(string(ci), anchor) {
			t.Fatalf("anchor missing from ci.yml")
		}
		mutated := strings.Replace(string(ci), anchor, anchor+extra, 1)
		if storybookCIStrict([]byte(mutated)) == nil {
			t.Errorf("%s on a mirrored step was accepted", name)
		}
	}
}

func TestStorybookAdvisoryWorkflow(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, storybookAdvisoryPath))
	if err != nil {
		t.Fatal(err)
	}
	ci, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	reference := storybookCISteps(t, ci)

	// The decoder rejects unknown keys: no extra trigger option, job condition,
	// permission override, service or continue-on-error goes unnoticed.
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
		Jobs map[string]storybookJob `yaml:"jobs"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&workflow); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}

	if workflow.Name != "ci-advisory-storybook" {
		t.Errorf("workflow name %q", workflow.Name)
	}
	trigger, ok := workflow.On["pull_request"]
	if len(workflow.On) != 1 || !ok {
		t.Fatal("the only trigger must be pull_request, never a privileged or chained event")
	}
	if !reflect.DeepEqual(trigger.Types, []string{"opened", "synchronize", "reopened"}) {
		t.Errorf("pull_request types %v", trigger.Types)
	}
	wantPaths := []string{"web/**", "clients/ts/**", ".nvmrc", "package.json", "docs/site/scripts/test-csp.mjs", storybookSuiteScript, storybookAdvisoryPath}
	if !reflect.DeepEqual(trigger.Paths, wantPaths) {
		t.Errorf("pull_request paths %v, want %v", trigger.Paths, wantPaths)
	}
	if !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) {
		t.Errorf("permissions %v, want contents: read only", workflow.Permissions)
	}
	if workflow.Concurrency.Group != "advisory-storybook-${{ github.event.pull_request.number }}" || !workflow.Concurrency.Cancel {
		t.Error("concurrency must be one cancelling group per PR number")
	}

	job, ok := workflow.Jobs["storybook"]
	if len(workflow.Jobs) != 1 || !ok {
		t.Fatal("the workflow must declare exactly one job, storybook")
	}
	if job.Runner != "ubuntu-latest" || job.Timeout != reference.Timeout {
		t.Errorf("job runs on %q for %d minutes, want ubuntu-latest for %d", job.Runner, job.Timeout, reference.Timeout)
	}
	if !strings.HasPrefix(job.Container.Image, storybookPlaywrightPins) || job.Container.Image != reference.Container.Image {
		t.Errorf("container %q must be ci.yml's pinned %q", job.Container.Image, reference.Container.Image)
	}
	head := "${{ github.event.pull_request.head.sha }}"
	if !reflect.DeepEqual(job.Env, map[string]string{"EXPECTED_HEAD": head}) {
		t.Errorf("job env %v, want only EXPECTED_HEAD", job.Env)
	}

	// The toolchain and cache steps mirror ci.yml's storybook job exactly.
	mirror := map[string]storybookStep{}
	for _, step := range reference.Steps {
		switch {
		case strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["persist-credentials"] == "false":
			mirror["checkout"] = step
		case strings.HasPrefix(step.Uses, "actions/setup-node@"):
			mirror["setup-node"] = step
		case step.ID == "pnpm-cache" || step.ID == "pnpm-store":
			mirror[step.ID] = step
		case strings.HasPrefix(step.Run, "./scripts/ci/install-corepack.sh"):
			mirror["corepack"] = step
		}
	}
	for _, key := range []string{"checkout", "setup-node", "corepack", "pnpm-cache", "pnpm-store"} {
		if _, ok := mirror[key]; !ok {
			t.Fatalf("ci.yml storybook job has no %s step to mirror", key)
		}
	}
	want := []storybookStep{
		{
			Name: "Check out the exact PR head without credentials",
			Uses: mirror["checkout"].Uses,
			With: map[string]string{"ref": head, "persist-credentials": "false"},
		},
		{Name: "Verify the checkout is the PR head", Run: `test "$(git rev-parse HEAD)" = "$EXPECTED_HEAD"`},
		mirror["setup-node"],
		mirror["corepack"],
		mirror["pnpm-cache"],
		mirror["pnpm-store"],
		{Name: "Storybook, theme and static demo suite", Run: "./" + storybookSuiteScript},
	}
	if len(job.Steps) != len(want) {
		t.Fatalf("job has %d steps, want %d", len(job.Steps), len(want))
	}
	for index := range want {
		if !reflect.DeepEqual(job.Steps[index], want[index]) {
			t.Errorf("step %d is %+v, want %+v", index+1, job.Steps[index], want[index])
		}
	}
	if !strings.HasPrefix(job.Steps[5].Uses, "actions/cache/restore@") {
		t.Error("the pnpm store must be restored, never saved")
	}

	for _, forbidden := range []string{
		"secrets.", "GH_TOKEN", "GITHUB_TOKEN", "github.token",
		"actions/cache/save", "upload-artifact", "download-artifact",
		"pull_request_target", "workflow_run", "workflow_dispatch", "workflow_call",
		"continue-on-error", "self-hosted",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("advisory storybook must not gain authority or publication: %s", forbidden)
		}
	}
}

func TestStorybookAdvisoryWorkflowIsNotARequiredContext(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "release", "repository", "main-ci-gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "advisory-storybook") || strings.Contains(string(raw), "storybook, theme") {
		t.Fatal("the advisory Storybook signal must not be a required context")
	}
}

// The pre-push hook fixture runs with the ./scripts/ci tests (test_core and the
// advisory invariants workflow), not as a ci.yml step: coverage-conservation
// tests freeze the steps of the supply-chain-checks job.
func TestPrePushHookFixture(t *testing.T) {
	output, err := exec.Command("sh", filepath.Join(repositoryRoot(t), "scripts/git/pre-push_test.sh")).CombinedOutput()
	if err != nil {
		t.Fatalf("pre-push hook fixture failed: %v\n%s", err, output)
	}
}

// The hook keys on, and the advisory workflow triggers on, the same inputs.
func TestPrePushKeyPathsMatchAdvisoryTriggerPaths(t *testing.T) {
	root := repositoryRoot(t)
	hook, err := os.ReadFile(filepath.Join(root, ".githooks/pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^storybook_paths='([^']+)'$`).FindSubmatch(hook)
	if match == nil {
		t.Fatal("pre-push hook has no storybook_paths list")
	}
	raw, err := os.ReadFile(filepath.Join(root, storybookAdvisoryPath))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On struct {
			PullRequest struct {
				Paths []string `yaml:"paths"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for _, path := range workflow.On.PullRequest.Paths {
		if path != storybookAdvisoryPath {
			triggers = append(triggers, strings.TrimSuffix(path, "/**"))
		}
	}
	keyed := strings.Fields(string(match[1]))
	slices.Sort(triggers)
	slices.Sort(keyed)
	if !slices.Equal(keyed, triggers) {
		t.Errorf("hook keys on %v but %s triggers on %v", keyed, storybookAdvisoryPath, triggers)
	}
}
