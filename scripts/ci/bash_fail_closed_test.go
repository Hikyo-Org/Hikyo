package ci_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// bash before 4.1 (macOS /bin/bash 3.2) ignores errexit for a failed bare [[ ]] or
// (( )) statement, so a guard written as `[[ $x =~ re ]]` under `set -e` lets the
// script carry on: it fails open. Every such statement therefore carries an explicit
// failure branch (`|| exit 1`, `|| return 1`), which every bash version honours.

// minBashMajor and minBashMinor name the oldest bash the Go harness runs the guards with.
const (
	minBashMajor = 4
	minBashMinor = 1
)

// checkBash reports why the bash at path cannot run the workflow-shell guards
// faithfully. No CI job runs this package on macOS (ci.yml's scripts/ci steps run on
// ubuntu-latest), so the prerequisite only affects local contributors.
func checkBash(ctx context.Context, path string) error {
	out, err := exec.CommandContext(ctx, path, "-c", `printf '%s.%s' "${BASH_VERSINFO[0]}" "${BASH_VERSINFO[1]}"`).Output()
	if err != nil {
		return fmt.Errorf("cannot run %s: %w", path, err)
	}
	if !bashAtLeast(string(out), minBashMajor, minBashMinor) {
		return fmt.Errorf("%s is bash %s: the scripts/ci tests need bash >= %d.%d (older bash ignores errexit for a failed [[ ]] or (( )), so the guards under test fail open there); put a newer bash first on PATH, for example Homebrew bash on macOS",
			path, out, minBashMajor, minBashMinor)
	}
	return nil
}

func bashAtLeast(version string, major, minor int) bool {
	majorText, minorText, ok := strings.Cut(version, ".")
	if !ok {
		return false
	}
	gotMajor, err := strconv.Atoi(majorText)
	if err != nil {
		return false
	}
	gotMinor, err := strconv.Atoi(minorText)
	if err != nil {
		return false
	}
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}

func TestMain(m *testing.M) {
	if err := checkBash(context.Background(), "bash"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func TestCheckBashRefusesOldBash(t *testing.T) {
	fake := func(version string) string {
		path := filepath.Join(t.TempDir(), "bash")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf "+version+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, old := range []string{"3.2", "2.5", "4.0"} {
		if err := checkBash(t.Context(), fake(old)); err == nil || !strings.Contains(err.Error(), "bash >= 4.1") {
			t.Fatalf("bash %s: got %v, want refusal naming bash >= 4.1", old, err)
		}
	}
	for _, ok := range []string{"4.1", "4.4", "5.0", "5.3"} {
		if err := checkBash(t.Context(), fake(ok)); err != nil {
			t.Fatalf("bash %s: %v", ok, err)
		}
	}
	if err := checkBash(t.Context(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing bash must be refused")
	}
}

type failClosedStep struct {
	Name  string `yaml:"name"`
	Shell string `yaml:"shell"`
	Run   string `yaml:"run"`
}

type failClosedDefaults struct {
	Run struct {
		Shell string `yaml:"shell"`
	} `yaml:"run"`
}

type failClosedWorkflow struct {
	Defaults failClosedDefaults `yaml:"defaults"`
	Jobs     map[string]struct {
		RunsOn   string             `yaml:"runs-on"`
		Defaults failClosedDefaults `yaml:"defaults"`
		Steps    []failClosedStep   `yaml:"steps"`
	} `yaml:"jobs"`
}

// isBashShell reports whether a step runs under bash: an unset shell is bash on the
// Linux and macOS runners.
func isBashShell(shell, runsOn string) bool {
	if shell == "" {
		// GitHub's default on a Windows runner is pwsh. A runs-on expression or matrix
		// value we cannot resolve is treated as bash, which fails safe; a list or
		// mapping runs-on fails the YAML decode loudly instead.
		return !strings.HasPrefix(strings.ToLower(runsOn), "windows")
	}
	return shell == "bash" || strings.HasPrefix(shell, "bash ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// oldBash returns /bin/bash when it is older than 4.1 (macOS ships 3.2), else "".
func oldBash(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "/bin/bash", "-c", `printf '%s.%s' "${BASH_VERSINFO[0]}" "${BASH_VERSINFO[1]}"`).Output()
	if err != nil || bashAtLeast(string(out), minBashMajor, minBashMinor) {
		return ""
	}
	return "/bin/bash"
}

// stepScript returns the run script of a workflow job's step.
func stepScript(t *testing.T, workflowFile, job string, step int) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", workflowFile))
	if err != nil {
		t.Fatal(err)
	}
	var workflow failClosedWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow.Jobs[job].Steps[step].Run
}

// through keeps the script up to and including the first line that contains marker.
func through(t *testing.T, script, marker string) string {
	t.Helper()
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		if strings.Contains(line, marker) {
			return strings.Join(lines[:i+1], "\n")
		}
	}
	t.Fatalf("marker %q not found in script", marker)
	return ""
}

// The guard statements that failed open on bash 3.2 must refuse there. Each case runs
// the real step or script, cut right after the guard under test, with a good
// environment (must exit 0, so the guard is the only thing that can stop it) and with
// a failing one (must exit non-zero). Skips only where no bash older than 4.1 exists;
// TestNoBareBashTestRelyingOnErrexit holds the line everywhere.
func TestGuardsFailClosedOnOldBash(t *testing.T) {
	bash := oldBash(t)
	if bash == "" {
		t.Skip("/bin/bash is not older than 4.1 (macOS ships 3.2): no old bash to run the guards under")
	}
	root := repositoryRoot(t)
	sha := strings.Repeat("a", 40)
	publisher, err := os.ReadFile(filepath.Join(root, "scripts/ci/build-queue-publisher.sh"))
	if err != nil {
		t.Fatal(err)
	}
	validate := stepScript(t, "ci.yml", "changes", 0)
	validateEnv := map[string]string{
		"GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10",
		"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/tags/v1.2.3", "GITHUB_SHA": sha, "SOURCE_REVISION": sha,
		"DEVELOPMENT": "false", "DEFAULT_BRANCH": "main", "TAG_DELETED": "false", "MERGE_GROUP_SHA": "",
	}
	floor := stepScript(t, "floor-bench.yml", "measure", 0)
	floorEnv := map[string]string{
		"GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10",
		"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/tags/v1.2.3", "GITHUB_SHA": sha, "SOURCE_REVISION": "",
		"DEVELOPMENT": "false", "DEFAULT_BRANCH": "main", "TAG_DELETED": "false", "MERGE_GROUP_SHA": "",
	}
	release := stepScript(t, "security-release.yml", "resolve", 0)
	releaseEnv := map[string]string{
		"GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "DEFAULT_BRANCH": "main",
		"GITHUB_EVENT_NAME": "workflow_run", "NIGHTLY_PATH": ".github/workflows/nightly.yml", "NIGHTLY_BRANCH": "main",
		"NIGHTLY_REPOSITORY": "Hikyo-Org/Hikyo", "NIGHTLY_EVENT": "schedule", "NIGHTLY_RUN_NUMBER": "42", "NIGHTLY_SHA": sha,
	}
	worker := stepScript(t, "ci-queue-worker.yml", "publish", 0)
	workerEnv := map[string]string{"HEAD": sha, "GITHUB_SHA": sha}
	cases := []struct {
		name, script, marker, suffix string
		args                         []string
		goMod                        string
		env                          map[string]string
		bad                          map[string]map[string]string
		badArgs                      []string
		badGoMod                     string
	}{
		{name: "validate the immutable caller revision", script: validate, marker: `!= *-nightly.*`, suffix: "\nfi\n;;\n*) ;;\nesac", env: validateEnv, bad: map[string]map[string]string{
			"non-numeric repository id": {"EVENT_REPOSITORY_ID": "abc"},
			"dev ref is not a tag":      {"GITHUB_REF": "refs/heads/dev"},
			"nightly tag":               {"GITHUB_REF": "refs/tags/v1.2.3-nightly.20261014.1.gaaaaaaaa"},
		}},
		{name: "floor-bench source guard", script: floor, marker: `"$EVENT_REPOSITORY_ID" =~`, env: floorEnv, bad: map[string]map[string]string{
			"push without a tag":        {"GITHUB_REF": "refs/tags/"},
			"push at a non-hex sha":     {"GITHUB_SHA": "zzz"},
			"non-numeric repository id": {"EVENT_REPOSITORY_ID": "0"},
		}},
		{name: "queue worker head binding", script: worker, marker: `[[ "$JOB_HEAD"`, env: workerEnv, bad: map[string]map[string]string{
			"head is not a sha":     {"HEAD": "main"},
			"job head is not a sha": {"GITHUB_SHA": "123"},
		}},
		{name: "security release nightly resolver", script: release, marker: `[[ "$NIGHTLY_SHA"`, suffix: "\n;;\nesac", env: releaseEnv, bad: map[string]map[string]string{
			"unexpected nightly event": {"NIGHTLY_EVENT": "push"},
			"zero run number":          {"NIGHTLY_RUN_NUMBER": "0"},
			"short nightly sha":        {"NIGHTLY_SHA": "abc"},
		}},
		{name: "publisher builder arguments", script: string(publisher), marker: `[[ $# == 0 ]]`, env: map[string]string{}, badArgs: []string{"unexpected"}},
		{name: "publisher builder relative tool cache", script: string(publisher), marker: `[[ "$RUNNER_TOOL_CACHE" == /* ]]`,
			env: map[string]string{"RUNNER_TEMP": "/tmp", "RUNNER_TOOL_CACHE": "/opt/cache"}, bad: map[string]map[string]string{"relative tool cache": {"RUNNER_TOOL_CACHE": "cache"}}},
		{name: "publisher builder go pin", script: string(publisher), marker: `(( 10#${BASH_REMATCH[1]} >= 21 ))`,
			env: map[string]string{"RUNNER_TEMP": "/tmp", "RUNNER_TOOL_CACHE": "/opt/cache"}, goMod: "go 1.27.2\n", badGoMod: "go 1.20.0\n"},
	}
	run := func(t *testing.T, script string, args []string, goMod string, env map[string]string) error {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), bash, append([]string{"-e", "-o", "pipefail", "-c", script, "guard"}, args...)...)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		for key, value := range env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			err = fmt.Errorf("%w: %s", err, out)
		}
		return err
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The trailing `:` keeps a bare guard from setting the exit status as the last command.
			script := through(t, tc.script, tc.marker) + tc.suffix + "\n:"
			goMod := tc.goMod
			if goMod == "" {
				goMod = "go 1.27.2\n"
			}
			if err := run(t, script, nil, goMod, tc.env); err != nil {
				t.Fatalf("control run with a good environment must pass: %v", err)
			}
			assertRefused := func(err error, what string) {
				t.Helper()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() == 0 {
					t.Errorf("%s was accepted under %s (err=%v): the guard fails open", what, bash, err)
				}
			}
			for what, change := range tc.bad {
				env := map[string]string{}
				for key, value := range tc.env {
					env[key] = value
				}
				for key, value := range change {
					env[key] = value
				}
				assertRefused(run(t, script, nil, goMod, env), what)
			}
			if tc.badArgs != nil {
				assertRefused(run(t, script, tc.badArgs, goMod, tc.env), "an unexpected argument")
			}
			if tc.badGoMod != "" {
				assertRefused(run(t, script, tc.args, tc.badGoMod, tc.env), "a go.mod below go 1.21")
			}
		})
	}
}

func TestIsBashShellTreatsWindowsDefaultAsPowerShell(t *testing.T) {
	for _, tc := range []struct {
		shell, runsOn string
		want          bool
	}{
		{"", "ubuntu-latest", true},
		{"", "macos-15", true},
		{"", "windows-latest", false},
		{"", "${{ matrix.os }}", true}, // unresolvable: scanned as bash, which fails safe
		{"bash", "windows-latest", true},
		{"bash -e {0}", "windows-latest", true},
		{"pwsh", "ubuntu-latest", false},
		{"sh", "", false},
	} {
		if got := isBashShell(tc.shell, tc.runsOn); got != tc.want {
			t.Errorf("isBashShell(%q, %q) = %v, want %v", tc.shell, tc.runsOn, got, tc.want)
		}
	}
}
