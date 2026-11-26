package ci_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// nightly-desktop.yml carries the desktop validation that left ci.yml (owner decision
// 2026-11-25). These tests make sure it cannot be silently emptied: every real step
// must stay gated on the module and run inside it, both heads must resolve, the
// schedule must fire daily, and the reporter must really open, update and close.

const (
	desktopModuleGuard  = "${{ hashFiles('desktop/go.mod') != '' }}"
	macOSGuardCondition = "${{ hashFiles('desktop/go.mod') != '' && runner.os != 'macOS' }}"
	macOSGuardRun       = "echo \"desktop/go.mod exists at this commit but the runner is $RUNNER_OS, not macOS.\" >&2\nexit 1"
	reportCondition     = "always() && github.ref == 'refs/heads/main'"
)

var (
	dailyCron = regexp.MustCompile(`^([0-9]|[1-5][0-9]) ([0-9]|1[0-9]|2[0-3]) \* \* \*$`)
)

func onlyKeys(m yamlMap, allowed ...string) error {
	for key := range m {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("unexpected key %q", key)
		}
	}
	return nil
}

// Actions the legs may use, by their exact pinned reference and exact inputs.
var pinnedActions = map[string]yamlMap{
	"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1": {"ref": "${{ env.HEAD_SHA }}", "persist-credentials": false},
	"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e": {"go-version-file": "go.mod", "cache": false},
}

// Keys outside these lists (shell, env, defaults, services and the like) can change
// what a step runs without touching its run text, so none are accepted.
func checkPlainSteps(name string, job yamlMap, allowed ...string) error {
	for _, step := range stepsOf(job) {
		if err := onlyKeys(step, allowed...); err != nil {
			return fmt.Errorf("%s step %q: %w", name, step["name"], err)
		}
		if run, _ := step["run"].(string); strings.Contains(run, "${{") {
			return fmt.Errorf("%s interpolates an expression into its script", name)
		}
	}
	return nil
}

func checkNightlyDesktop(workflow yamlMap) error {
	if err := onlyKeys(workflow, "name", "on", "permissions", "concurrency", "jobs"); err != nil {
		return fmt.Errorf("workflow: %w", err)
	}
	for name, raw := range jobsOf(workflow) {
		if asMap(raw)["defaults"] != nil {
			return fmt.Errorf("job %s sets defaults, which can replace the shell of every step", name)
		}
	}
	schedules := asList(asMap(workflow["on"])["schedule"])
	if len(schedules) == 0 {
		return fmt.Errorf("nightly-desktop has no schedule")
	}
	for _, entry := range schedules {
		if cron, _ := asMap(entry)["cron"].(string); !dailyCron.MatchString(cron) {
			return fmt.Errorf("cron %q does not fire every day", cron)
		}
	}
	if !reflect.DeepEqual(workflow["permissions"], yamlMap{"contents": "read"}) {
		return fmt.Errorf("workflow permissions must stay contents: read")
	}
	jobs := jobsOf(workflow)
	heads := asMap(jobs["heads"])
	if heads == nil || heads["if"] != "github.ref == 'refs/heads/main'" || heads["continue-on-error"] != nil {
		return fmt.Errorf("heads must run, and only on main")
	}
	if err := onlyKeys(heads, "if", "runs-on", "timeout-minutes", "outputs", "steps"); err != nil {
		return fmt.Errorf("heads: %w", err)
	}
	if err := checkPlainSteps("heads", heads, "name", "id", "env", "run"); err != nil {
		return err
	}
	outputs := asMap(heads["outputs"])
	for _, name := range []string{"dev", "main", "dev_runner", "main_runner"} {
		if outputs[name] != "${{ steps.resolve."+"outputs."+name+" }}" {
			return fmt.Errorf("heads lost its %s output", name)
		}
	}
	shim := asMap(jobs["shim"])
	if shim == nil || shim["if"] != "github.ref == 'refs/heads/main'" || shim["continue-on-error"] != nil || shim["permissions"] != nil ||
		!reflect.DeepEqual(yamlNeedsOf(shim), []string{"heads"}) || asMap(shim["outputs"])["state"] != "${{ steps.check.outputs.state }}" {
		return fmt.Errorf("shim must run only on main after heads, fail closed, stay read-only and expose its state")
	}
	if err := onlyKeys(shim, "needs", "if", "runs-on", "timeout-minutes", "outputs", "steps"); err != nil {
		return fmt.Errorf("shim: %w", err)
	}
	if err := checkPlainSteps("shim", shim, "name", "id", "env", "run"); err != nil {
		return err
	}
	for leg, branch := range map[string]string{"desktop-dev": "dev", "desktop-main": "main"} {
		job := asMap(jobs[leg])
		if job == nil {
			return fmt.Errorf("%s job is missing", leg)
		}
		if job["runs-on"] != "${{ needs.heads.outputs."+branch+"_runner }}" || job["continue-on-error"] != nil || job["permissions"] != nil || job["if"] != nil {
			return fmt.Errorf("%s must take the resolved runner, fail closed, always run and inherit read-only permissions", leg)
		}
		if !reflect.DeepEqual(yamlNeedsOf(job), []string{"heads"}) {
			return fmt.Errorf("%s must depend only on the head resolver", leg)
		}
		if err := onlyKeys(job, "needs", "runs-on", "timeout-minutes", "env", "steps"); err != nil {
			return fmt.Errorf("%s: %w", leg, err)
		}
		wantEnv := yamlMap{
			"HEAD_SHA":    "${{ needs.heads.outputs." + branch + " }}",
			"CGO_ENABLED": "1",
			"CGO_CFLAGS":  "-mmacosx-version-min=13.0",
			"CGO_LDFLAGS": "-mmacosx-version-min=13.0",
		}
		if !reflect.DeepEqual(job["env"], wantEnv) {
			return fmt.Errorf("%s env must be exactly %v, got %v", leg, wantEnv, job["env"])
		}
		want := map[string]string{
			"Require macOS where the shell exists":          macOSGuardRun,
			"Desktop module boundary":                       "./scripts/ci/check-desktop-deps_test.sh\n./scripts/ci/check-desktop-deps.sh",
			"Desktop tests with the race detector":          "go test -race -count=1 -p 2 ./...",
			"Build the probe shell":                         `go build -p 2 -tags probe -o "$RUNNER_TEMP/hikyo-desktop-probe" .`,
			"Build the release shell":                       `go build -p 2 -o "$RUNNER_TEMP/hikyo-desktop" .`,
			"Driven WKWebView lane against a fake instance": `go run ./probes/driven -app "$RUNNER_TEMP/hikyo-desktop-probe" -timeout 120s`,
		}
		seen := map[string]bool{}
		for _, step := range stepsOf(job) {
			if step["continue-on-error"] != nil {
				return fmt.Errorf("%s has a fail-open step", leg)
			}
			if uses, _ := step["uses"].(string); uses != "" {
				inputs, pinned := pinnedActions[uses]
				if !pinned {
					return fmt.Errorf("%s uses %q, which is not a pinned setup action", leg, uses)
				}
				if err := onlyKeys(step, "name", "uses", "with"); err != nil {
					return fmt.Errorf("%s setup step %q: %w", leg, uses, err)
				}
				if !reflect.DeepEqual(step["with"], inputs) {
					return fmt.Errorf("%s step %q must take exactly the inputs %v", leg, uses, inputs)
				}
				continue
			}
			if err := onlyKeys(step, "name", "if", "run", "working-directory"); err != nil {
				return fmt.Errorf("%s step %q: %w", leg, step["name"], err)
			}
			name, _ := step["name"].(string)
			run, _ := step["run"].(string)
			command, known := want[name]
			if !known {
				return fmt.Errorf("%s has an unexpected step %q", leg, name)
			}
			// Exact match: a suffix such as "|| true" or a narrowed target would fail open.
			if strings.TrimSpace(run) != command {
				return fmt.Errorf("%s step %q must run exactly %q", leg, name, command)
			}
			seen[name] = true
			if name == "Require macOS where the shell exists" {
				if step["if"] != macOSGuardCondition || step["working-directory"] != nil {
					return fmt.Errorf("%s macOS guard must fire when the module exists on a non-macOS runner", leg)
				}
				continue
			}
			if name == "Desktop module boundary" {
				if step["if"] != nil || step["working-directory"] != nil {
					return fmt.Errorf("%s boundary check must always run at the repository root", leg)
				}
				continue
			}
			if step["if"] != desktopModuleGuard || step["working-directory"] != "desktop" {
				return fmt.Errorf("%s step %q must be gated on desktop/go.mod and run in desktop", leg, name)
			}
		}
		for name := range want {
			if !seen[name] {
				return fmt.Errorf("%s lost the %q step", leg, name)
			}
		}
	}
	report := asMap(jobs["report"])
	if report == nil {
		return fmt.Errorf("report job is missing")
	}
	if !reflect.DeepEqual(report["permissions"], yamlMap{"issues": "write"}) {
		return fmt.Errorf("report job permissions must be exactly issues: write")
	}
	if !reflect.DeepEqual(yamlNeedsOf(report), []string{"heads", "desktop-dev", "desktop-main", "shim"}) || report["if"] != reportCondition {
		return fmt.Errorf("report must wait for both legs, run after any result, and only on main")
	}
	if err := onlyKeys(report, "needs", "if", "runs-on", "timeout-minutes", "permissions", "steps"); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if err := checkPlainSteps("report", report, "name", "env", "run"); err != nil {
		return err
	}
	for _, step := range stepsOf(report) {
		if step["uses"] != nil {
			return fmt.Errorf("report job must not check out or run any action")
		}
		run, _ := step["run"].(string)
		if strings.Contains(run, "${{") {
			return fmt.Errorf("report job interpolates an expression into its script")
		}
		if !strings.HasSuffix(strings.TrimSpace(run), "exit 1") {
			return fmt.Errorf("report script must fail the run after reporting")
		}
	}
	return nil
}

func TestNightlyDesktopRunsOnBothHeadsAndReports(t *testing.T) {
	if err := checkNightlyDesktop(readYAMLMap(t, "nightly-desktop.yml")); err != nil {
		t.Fatal(err)
	}
	report := func(w yamlMap) yamlMap { return asMap(jobsOf(w)["report"]) }
	heads := func(w yamlMap) yamlMap { return asMap(jobsOf(w)["heads"]) }
	leg := func(w yamlMap) yamlMap { return asMap(jobsOf(w)["desktop-main"]) }
	step := func(job yamlMap, name string) yamlMap {
		for _, s := range stepsOf(job) {
			if s["name"] == name {
				return s
			}
		}
		t.Fatalf("no step %q", name)
		return nil
	}
	appendRun := func(s yamlMap, suffix string) { s["run"] = strings.TrimSpace(s["run"].(string)) + suffix }
	replaceRun := func(s yamlMap, old, replacement string) {
		changed := strings.Replace(s["run"].(string), old, replacement, 1)
		if changed == s["run"].(string) {
			t.Fatalf("no %q to mutate in %q", old, s["name"])
		}
		s["run"] = changed
	}
	removeStep := func(job yamlMap, name string) {
		var kept []any
		for _, s := range asList(job["steps"]) {
			if asMap(s)["name"] != name {
				kept = append(kept, s)
			}
		}
		job["steps"] = kept
	}
	for name, mutate := range map[string]func(yamlMap){
		"no schedule":                func(w yamlMap) { delete(asMap(w["on"]), "schedule") },
		"a cron that never fires":    func(w yamlMap) { asMap(asList(asMap(w["on"])["schedule"])[0])["cron"] = "41 3 31 2 *" },
		"a weekly cron":              func(w yamlMap) { asMap(asList(asMap(w["on"])["schedule"])[0])["cron"] = "41 3 * * 1" },
		"a leg removed":              func(w yamlMap) { delete(jobsOf(w), "desktop-main") },
		"leg pinned to one runner":   func(w yamlMap) { leg(w)["runs-on"] = "macos-15" },
		"leg takes the dev runner":   func(w yamlMap) { leg(w)["runs-on"] = "${{ needs.heads.outputs.dev_runner }}" },
		"leg fails open":             func(w yamlMap) { leg(w)["continue-on-error"] = true },
		"leg skipped":                func(w yamlMap) { leg(w)["if"] = "false" },
		"heads skipped":              func(w yamlMap) { heads(w)["if"] = "false" },
		"heads off main":             func(w yamlMap) { heads(w)["if"] = "always()" },
		"heads lost a runner output": func(w yamlMap) { delete(asMap(heads(w)["outputs"]), "main_runner") },
		"race tests fail open":       func(w yamlMap) { appendRun(step(leg(w), "Desktop tests with the race detector"), " || true") },
		"race tests lose ./...":      func(w yamlMap) { replaceRun(step(leg(w), "Desktop tests with the race detector"), "./...", ".") },
		"probe build fails open":     func(w yamlMap) { appendRun(step(leg(w), "Build the probe shell"), " || true") },
		"release build fails open":   func(w yamlMap) { appendRun(step(leg(w), "Build the release shell"), " || true") },
		"driven lane fails open":     func(w yamlMap) { appendRun(step(leg(w), "Driven WKWebView lane against a fake instance"), " || true") },
		"boundary is only a help": func(w yamlMap) {
			replaceRun(step(leg(w), "Desktop module boundary"), "check-desktop-deps.sh", "check-desktop-deps.sh --help || true")
		},
		"macOS guard removed":       func(w yamlMap) { removeStep(leg(w), "Require macOS where the shell exists") },
		"macOS guard never fires":   func(w yamlMap) { step(leg(w), "Require macOS where the shell exists")["if"] = "false" },
		"macOS guard passes":        func(w yamlMap) { replaceRun(step(leg(w), "Require macOS where the shell exists"), "exit 1", "exit 0") },
		"shim job removed":          func(w yamlMap) { delete(jobsOf(w), "shim") },
		"shim job fails open":       func(w yamlMap) { asMap(jobsOf(w)["shim"])["continue-on-error"] = true },
		"shim job off main":         func(w yamlMap) { asMap(jobsOf(w)["shim"])["if"] = "always()" },
		"shim state output dropped": func(w yamlMap) { delete(asMap(asMap(jobsOf(w)["shim"])["outputs"]), "state") },
		"report ignores the shim":   func(w yamlMap) { report(w)["needs"] = []any{"heads", "desktop-dev", "desktop-main"} },
		"leg tests wrong head": func(w yamlMap) {
			asMap(leg(w)["env"])["HEAD_SHA"] = "${{ needs.heads.outputs.dev }}"
		},
		"leg unpinned checkout": func(w yamlMap) {
			asMap(stepsOf(leg(w))[0]["with"])["ref"] = "main"
		},
		"leg lost the driven lane": func(w yamlMap) {
			steps := asList(leg(w)["steps"])
			leg(w)["steps"] = steps[:len(steps)-1]
		},
		"every real step is if: false": func(w yamlMap) {
			for _, s := range stepsOf(leg(w)) {
				if s["run"] != nil {
					s["if"] = "false"
				}
			}
		},
		"race tests ungated": func(w yamlMap) {
			delete(step(leg(w), "Desktop tests with the race detector"), "if")
		},
		"race tests run in the root": func(w yamlMap) {
			step(leg(w), "Desktop tests with the race detector")["working-directory"] = "."
		},
		"race tests narrowed": func(w yamlMap) {
			s := step(leg(w), "Desktop tests with the race detector")
			s["run"] = s["run"].(string) + " -run XXX"
		},
		"race tests skipped": func(w yamlMap) {
			s := step(leg(w), "Desktop tests with the race detector")
			s["run"] = s["run"].(string) + " -skip Everything"
		},
		"boundary gated on the module": func(w yamlMap) {
			step(leg(w), "Desktop module boundary")["if"] = desktopModuleGuard
		},
		"boundary lost its script": func(w yamlMap) {
			step(leg(w), "Desktop module boundary")["run"] = "true"
		},
		"report writes contents": func(w yamlMap) {
			report(w)["permissions"] = yamlMap{"issues": "write", "contents": "write"}
		},
		"report has no permissions": func(w yamlMap) { delete(report(w), "permissions") },
		"report checks out code": func(w yamlMap) {
			checkout := yamlMap{"uses": "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"}
			report(w)["steps"] = append([]any{checkout}, asList(report(w)["steps"])...)
		},
		"report interpolates": func(w yamlMap) {
			s := stepsOf(report(w))[0]
			s["run"] = "echo ${{ github.event.head_commit.message }}\n" + s["run"].(string)
		},
		"report skipped on failure": func(w yamlMap) { report(w)["if"] = "success()" },
		"report runs off main":      func(w yamlMap) { report(w)["if"] = "always()" },
		"report ignores a leg":      func(w yamlMap) { report(w)["needs"] = []any{"heads", "desktop-dev"} },
		"report leaves the run green": func(w yamlMap) {
			s := stepsOf(report(w))[0]
			s["run"] = strings.TrimSuffix(strings.TrimSpace(s["run"].(string)), "exit 1")
		},
		"workflow token widened": func(w yamlMap) { w["permissions"] = yamlMap{"contents": "write"} },
		"step shell replaced": func(w yamlMap) {
			step(leg(w), "Desktop tests with the race detector")["shell"] = "true {0}"
		},
		"step env narrows the tests": func(w yamlMap) {
			step(leg(w), "Desktop tests with the race detector")["env"] = yamlMap{"GOFLAGS": "-run=XXX"}
		},
		"job default shell replaced": func(w yamlMap) {
			leg(w)["defaults"] = yamlMap{"run": yamlMap{"shell": "true {0}"}}
		},
		"workflow default shell replaced": func(w yamlMap) {
			w["defaults"] = yamlMap{"run": yamlMap{"shell": "true {0}"}}
		},
		"job env GOFLAGS":         func(w yamlMap) { asMap(leg(w)["env"])["GOFLAGS"] = "-run=XXX" },
		"job env lost a cgo flag": func(w yamlMap) { delete(asMap(leg(w)["env"]), "CGO_LDFLAGS") },
		"extra unpinned action": func(w yamlMap) {
			evil := yamlMap{"name": "x", "uses": "evil/action@v1"}
			leg(w)["steps"] = append(asList(leg(w)["steps"]), evil)
		},
		"setup-go cache enabled": func(w yamlMap) { asMap(stepsOf(leg(w))[2]["with"])["cache"] = true },
		"shim step shell replaced": func(w yamlMap) {
			stepsOf(asMap(jobsOf(w)["shim"]))[0]["shell"] = "true {0}"
		},
		"shim script interpolates": func(w yamlMap) {
			s := stepsOf(asMap(jobsOf(w)["shim"]))[0]
			s["run"] = "echo ${{ github.event.head_commit.message }}\n" + s["run"].(string)
		},
		"heads script interpolates": func(w yamlMap) {
			s := stepsOf(heads(w))[0]
			s["run"] = "echo ${{ github.event.head_commit.message }}\n" + s["run"].(string)
		},
		"report step shell replaced": func(w yamlMap) { stepsOf(report(w))[0]["shell"] = "true {0}" },
	} {
		t.Run(name, func(t *testing.T) {
			workflow := readYAMLMap(t, "nightly-desktop.yml")
			mutate(workflow)
			if err := checkNightlyDesktop(workflow); err == nil {
				t.Fatal("mutation was accepted")
			}
		})
	}
}

// A stub gh serves everything the workflow scripts ask of it from files in GH_DIR:
// issues.<label>.json for `issue list --label <label> --jq` ([] when absent; `issue create`
// appends to it as number 100+), sha.<branch> for a branch head, module.<sha>
// holding an HTTP status for desktop/go.mod, and content.<ref>.<path, / as _> for raw
// file contents (404 when absent).
const stubGH = `#!/bin/bash
echo "$*" >>"$GH_LOG"
if [ "$1 $2" = "issue list" ]; then
  label=""
  filter=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --label) label=$2 ;;
      --jq) filter=$2 ;;
    esac
    shift
  done
  file="$GH_DIR/issues.$label.json"
  [ -f "$file" ] || { echo "[]" >"$file"; }
  jq -r "$filter" <"$file"
  exit 0
fi
if [ "$1 $2" = "issue create" ]; then
  shift 2
  title=""
  label=""
  body=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --title) title=$2 ;;
      --label) label=$2 ;;
      --body) body=$2 ;;
    esac
    shift
  done
  file="$GH_DIR/issues.$label.json"
  [ -f "$file" ] || { echo "[]" >"$file"; }
  jq --arg t "$title" --arg b "$body" '. + [{number: (length + 100), title: $t, author: {login: "app/github-actions"}, body: $b}]' "$file" >"$file.new"
  mv "$file.new" "$file"
  exit 0
fi
if [ "$1" = api ]; then
  path=""
  for arg in "$@"; do
    case "$arg" in repos/*) path=$arg ;; esac
  done
  case "$path" in
    */git/ref/heads/*) cat "$GH_DIR/sha.${path##*/}"; exit 0 ;;
    */contents/desktop/go.mod\?ref=*)
      case "$(cat "$GH_DIR/module.${path##*=}")" in
        200) exit 0 ;;
        404) echo "gh: Not Found (HTTP 404)" >&2; exit 1 ;;
        *) echo "gh: Internal Server Error (HTTP 500)" >&2; exit 1 ;;
      esac ;;
    */contents/*\?ref=*)
      file=${path#*/contents/}
      ref=${file##*ref=}
      file=${file%%\?*}
      if ! cat "$GH_DIR/content.$ref.${file//\//_}" 2>/dev/null; then
        echo "gh: Not Found (HTTP 404)" >&2
        exit 1
      fi
      exit 0 ;;
  esac
fi
`

type stubRun struct {
	status int
	log    string
	output []string
}

// Run a workflow script against the stub gh with the given GH_DIR files and extra env.
func runStubbed(t *testing.T, script string, files map[string]string, env ...string) stubRun {
	t.Helper()
	return runStubbedIn(t, t.TempDir(), script, files, env...)
}

// Like runStubbed, but in a caller-owned directory so a second run sees the first
// run's created issues. Each run starts with a fresh gh log.
func runStubbedIn(t *testing.T, dir, script string, files map[string]string, env ...string) stubRun {
	t.Helper()
	_ = os.Remove(filepath.Join(dir, "log"))
	_ = os.Remove(filepath.Join(dir, "out"))
	write := func(name, content string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("gh", stubGH, 0o755)
	for name, content := range files {
		write(name, content, 0o644)
	}
	cmd := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail", "-c", script)
	cmd.Env = append(append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GH_DIR="+dir, "GH_LOG="+filepath.Join(dir, "log"), "GH_TOKEN=x", "GH_REPO=o/r",
		"RUNNER_TEMP="+dir, "GITHUB_OUTPUT="+filepath.Join(dir, "out")), env...)
	run := stubRun{}
	if out, err := cmd.CombinedOutput(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("%v: %s", err, out)
		}
		run.status = exitErr.ExitCode()
	}
	log, _ := os.ReadFile(filepath.Join(dir, "log"))
	run.log = string(log)
	outputs, _ := os.ReadFile(filepath.Join(dir, "out"))
	run.output = strings.Fields(string(outputs))
	return run
}

func stepRun(t *testing.T, job string, name string) string {
	t.Helper()
	for _, s := range stepsOf(asMap(jobsOf(readYAMLMap(t, "nightly-desktop.yml"))[job])) {
		if s["name"] == name {
			run, _ := s["run"].(string)
			return run
		}
	}
	t.Fatalf("no step %q in %s", name, job)
	return ""
}

func mutateScript(t *testing.T, script, old, replacement string) string {
	t.Helper()
	changed := strings.Replace(script, old, replacement, 1)
	if changed == script {
		t.Fatalf("script has no %q to mutate", old)
	}
	return changed
}

// The resolver decides each leg's runner from the HTTP answer for desktop/go.mod:
// 200 is macOS, only a 404 is Linux, and anything else must fail the night.
func checkResolver(t *testing.T, script string) error {
	t.Helper()
	dev, main := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, tc := range []struct {
		name, devSHA, devModule, mainModule string
		fails                               bool
		want                                []string
	}{
		{"both heads have the module", dev, "200", "200", false,
			[]string{"dev=" + dev, "dev_runner=macos-15", "main=" + main, "main_runner=macos-15"}},
		{"main has no module yet", dev, "200", "404", false,
			[]string{"dev=" + dev, "dev_runner=macos-15", "main=" + main, "main_runner=ubuntu-latest"}},
		{"dev has no module", dev, "404", "200", false,
			[]string{"dev_runner=ubuntu-latest", "main_runner=macos-15"}},
		{"the API answers 500", dev, "200", "500", true, nil},
		{"a head is not a commit SHA", "not-a-sha", "200", "200", true, nil},
	} {
		run := runStubbed(t, script, map[string]string{
			"sha.dev": tc.devSHA, "sha.main": main, "module." + dev: tc.devModule, "module." + main: tc.mainModule, "module." + tc.devSHA: tc.devModule,
		})
		if tc.fails {
			if run.status == 0 {
				return fmt.Errorf("%s: the resolver passed", tc.name)
			}
			continue
		}
		if run.status != 0 {
			return fmt.Errorf("%s: the resolver failed", tc.name)
		}
		for _, want := range tc.want {
			if !slices.Contains(run.output, want) {
				return fmt.Errorf("%s: output %v lacks %q", tc.name, run.output, want)
			}
		}
	}
	return nil
}

func TestNightlyDesktopRunnerResolver(t *testing.T) {
	script := stepRun(t, "heads", "Resolve the dev and main heads")
	if err := checkResolver(t, script); err != nil {
		t.Fatal(err)
	}
	for name, mutation := range map[string][2]string{
		"always macOS":        {"echo ubuntu-latest", "echo macos-15"},
		"always Linux":        {"echo macos-15", "echo ubuntu-latest"},
		"runners swapped":     {"echo macos-15", "echo QQ"},
		"any error is Linux":  {`elif grep -F 'HTTP 404' "$RUNNER_TEMP/module.err" >/dev/null; then`, "elif true; then"},
		"no SHA check":        {`[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || { echo "Invalid $branch head." >&2; exit 1; }`, "true"},
		"only the main head":  {"for branch in dev main", "for branch in main"},
		"runner not recorded": {`echo "${branch}_runner=$runner" >>"$GITHUB_OUTPUT"`, "true"},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := mutateScript(t, script, mutation[0], mutation[1])
			if name == "runners swapped" {
				mutated = strings.Replace(strings.Replace(mutated, "echo ubuntu-latest", "echo macos-15", 1), "echo QQ", "echo ubuntu-latest", 1)
			}
			if err := checkResolver(t, mutated); err == nil {
				t.Fatal("mutation was accepted")
			}
		})
	}
}

// The shim in ci.yml is dead once no long-lived branch registers desktop. The night
// must say so, and must not guess when a file cannot be read.
func checkShim(t *testing.T, script string) error {
	t.Helper()
	const withShim = "run: jq ... ci-job-registry.without-desktop.json"
	listed, unlisted := `{"jobs":{"desktop":{}}}`, `{"jobs":{"docs":{}}}`
	for _, tc := range []struct {
		name, ci, mainRegistry, devRegistry, state string
		fails                                      bool
	}{
		{"the shim is not on main yet", "name: ci", unlisted, unlisted, "gone", false},
		{"main still registers desktop", withShim, listed, unlisted, "needed", false},
		{"only dev registers desktop", withShim, unlisted, listed, "needed", false},
		{"neither head registers desktop", withShim, unlisted, unlisted, "dead", false},
		{"the dev registry cannot be read", withShim, unlisted, "", "", true},
		{"the main registry cannot be read", withShim, "", unlisted, "", true},
		{"ci.yml cannot be read", "", unlisted, unlisted, "", true},
		{"the main registry has no jobs", withShim, "{}", unlisted, "", true},
		{"the dev registry has no jobs", withShim, unlisted, "{}", "", true},
	} {
		files := map[string]string{}
		put := func(ref, path, body string) {
			if body == "" {
				return
			}
			files["content."+ref+"."+strings.ReplaceAll(path, "/", "_")] = body
		}
		put("mainsha", ".github/workflows/ci.yml", tc.ci)
		put("mainsha", "scripts/ci/ci-job-registry.json", tc.mainRegistry)
		put("devsha", "scripts/ci/ci-job-registry.json", tc.devRegistry)
		run := runStubbed(t, script, files, "MAIN_SHA=mainsha", "DEV_SHA=devsha")
		if tc.fails {
			if run.status == 0 {
				return fmt.Errorf("%s: the shim check passed", tc.name)
			}
			continue
		}
		if run.status != 0 || !slices.Contains(run.output, "state="+tc.state) {
			return fmt.Errorf("%s: status %d, output %v, want state=%s", tc.name, run.status, run.output, tc.state)
		}
	}
	return nil
}

func TestNightlyDesktopShimCheck(t *testing.T) {
	script := stepRun(t, "shim", "Check whether the desktop-removal shim is still needed")
	if err := checkShim(t, script); err != nil {
		t.Fatal(err)
	}
	for name, mutation := range map[string][2]string{
		"always dead":                                 {"state=needed", "state=dead"},
		"always needed":                               {"state=dead", "state=needed"},
		"ignores dev":                                 {` || [ "$dev_has" = true ]`, ""},
		"ignores main":                                {`[ "$main_has" = true ] || `, ""},
		"ignores the marker":                          {`if grep -qF ci-job-registry.without-desktop.json <<<"$ci"; then`, "if true; then"},
		"swallows a dev registry error":               {`dev_registry=$(fetch scripts/ci/ci-job-registry.json "$DEV_SHA")`, `dev_registry=$(fetch scripts/ci/ci-job-registry.json "$DEV_SHA" || true)`},
		"swallows a main registry error":              {`main_registry=$(fetch scripts/ci/ci-job-registry.json "$MAIN_SHA")`, `main_registry=$(fetch scripts/ci/ci-job-registry.json "$MAIN_SHA" || true)`},
		"swallows a ci.yml error":                     {`ci=$(fetch .github/workflows/ci.yml "$MAIN_SHA")`, `ci=$(fetch .github/workflows/ci.yml "$MAIN_SHA" || true)`},
		"a registry without jobs reads as no desktop": {`else error("the registry has no jobs") end`, `else false end`},
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkShim(t, mutateScript(t, script, mutation[0], mutation[1])); err == nil {
				t.Fatal("mutation was accepted")
			}
		})
	}
}

func TestNightlyDesktopReportLifecycle(t *testing.T) {
	steps := stepsOf(asMap(jobsOf(readYAMLMap(t, "nightly-desktop.yml"))["report"]))
	if len(steps) != 1 {
		t.Fatal("report job must have exactly one step")
	}
	script, _ := steps[0]["run"].(string)
	const (
		desktopTitle  = "ci: nightly desktop validation failing"
		desktopMarker = "<!-- hikyo-nightly-desktop -->"
		desktopLabel  = "ci-nightly-desktop"
		shimTitle     = "ci: desktop-removal shim check (#1595)"
		shimMarker    = "<!-- hikyo-nightly-desktop-shim -->"
		shimLabel     = "ci-desktop-removal-shim"
	)
	issue := func(number int, title, author, body string) string {
		return fmt.Sprintf(`{"number":%d,"title":%q,"author":{"login":%q},"body":%q}`, number, title, author, body)
	}
	list := func(issues ...string) string { return "[" + strings.Join(issues, ",") + "]" }
	const bot = "app/github-actions"
	// Everything sorts ahead of the one issue per label the report may touch: a wrong
	// title, a missing marker, a human author and the other issue's title and marker
	// must each be rejected on their own.
	desktopIssues := list(
		issue(5, "unrelated", bot, desktopMarker),
		issue(6, desktopTitle, bot, "no marker here"),
		issue(8, desktopTitle, "someone", desktopMarker),
		issue(9, shimTitle, bot, shimMarker),
		issue(7, desktopTitle, bot, desktopMarker+"\n\nold"),
	)
	shimIssues := list(
		issue(15, "unrelated", bot, shimMarker),
		issue(16, shimTitle, bot, "no marker here"),
		issue(18, shimTitle, "someone", shimMarker),
		issue(19, desktopTitle, bot, desktopMarker),
		issue(17, shimTitle, bot, shimMarker+"\n\nold"),
	)
	const runURL = "https://example/run/1"
	tick := "`"
	line := func(branch, sha, result string) string {
		return "branch " + branch + ", head " + tick + sha + tick + ": " + result
	}
	desktopFailed := "Nightly desktop validation did not pass: " + runURL
	desktopHealed := "Nightly desktop validation passed on dev (" + tick + "aaaa" + tick + ") and main (" + tick + "bbbb" + tick + "): " + runURL
	shimDead := "The desktop-removal shim in ci.yml is dead: neither head registers desktop any more. Remove the shim and its migration tests (#1595). Seen: " + runURL
	shimHealed := "The desktop-removal shim check is healthy again: " + runURL
	var untouched []string
	for _, n := range []int{5, 6, 8, 9, 15, 16, 18, 19} {
		untouched = append(untouched, fmt.Sprintf("issue comment %d ", n), fmt.Sprintf("issue close %d ", n))
	}
	for _, tc := range []struct {
		name                   string
		desktopList, shimList  string
		dev, main, shim, state string
		wantStatus             int
		want, absent           []string
	}{
		{"open a new desktop issue", "[]", "[]", "failure", "success", "success", "needed", 1,
			[]string{"issue list --state open --label " + desktopLabel, "label create " + desktopLabel + " --force --color B60205 --description Automation-owned nightly desktop validation failure",
				"issue create --title " + desktopTitle + " --label " + desktopLabel, desktopMarker, desktopFailed, line("dev", "aaaa", "failure"), line("main", "bbbb", "success")},
			[]string{"issue close", "issue comment", "issue create --title " + shimTitle, "label create " + shimLabel}},
		{"comment on the desktop issue and leave the shim issue closed", desktopIssues, shimIssues, "success", "failure", "success", "needed", 1,
			[]string{"issue comment 7 ", "issue close 17 ", shimHealed, desktopFailed, line("dev", "aaaa", "success"), line("main", "bbbb", "failure")}, append([]string{"issue create", "issue comment 17 ", "issue close 7 "}, untouched...)},
		{"a cancelled leg counts as failing", desktopIssues, "[]", "cancelled", "success", "success", "needed", 1,
			[]string{"issue comment 7 ", line("dev", "aaaa", "cancelled"), line("main", "bbbb", "success")}, []string{"issue close", "issue create"}},
		{"skipped legs count as failing", desktopIssues, "[]", "skipped", "skipped", "success", "needed", 1,
			[]string{"issue comment 7 ", line("dev", "aaaa", "skipped"), line("main", "bbbb", "skipped")}, []string{"issue close", "issue create"}},
		{"a dead shim opens its own issue and closes a healed desktop one", desktopIssues, "[]", "success", "success", "success", "dead", 1,
			[]string{"issue close 7 ", desktopHealed, "label create " + shimLabel + " --force --color B60205 --description Automation-owned desktop-removal shim check",
				"issue create --title " + shimTitle + " --label " + shimLabel, shimMarker, shimDead},
			[]string{"issue comment", "issue create --title " + desktopTitle}},
		{"a dead shim comments on its own issue", desktopIssues, shimIssues, "success", "success", "success", "dead", 1,
			[]string{"issue close 7 ", "issue comment 17 ", shimDead}, append([]string{"issue create", "issue comment 7 ", "issue close 17 "}, untouched...)},
		{"a shim fetch failure goes to the shim issue", "[]", shimIssues, "success", "success", "failure", "", 1,
			[]string{"issue comment 17 ", "did not complete (failure)", runURL}, []string{"issue create", "issue close", "issue comment 7 "}},
		{"a failing leg and a dead shim are both reported", desktopIssues, shimIssues, "failure", "success", "success", "dead", 1,
			[]string{"issue comment 7 ", "issue comment 17 "}, append([]string{"issue create", "issue close"}, untouched...)},
		{"close both on a green night", desktopIssues, shimIssues, "success", "success", "success", "needed", 0,
			[]string{"issue close 7 ", "issue close 17 ", desktopHealed, shimHealed}, append([]string{"issue create", "issue comment"}, untouched...)},
		{"close once the shim is gone", desktopIssues, shimIssues, "success", "success", "success", "gone", 0,
			[]string{"issue close 7 ", "issue close 17 ", desktopHealed, shimHealed}, []string{"issue create", "issue comment"}},
		{"nothing to do on a green night", "[]", "[]", "success", "success", "success", "gone", 0,
			[]string{"issue list"}, []string{"issue create", "issue comment", "issue close", "label create"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := runStubbed(t, script, map[string]string{
				"issues." + desktopLabel + ".json": tc.desktopList, "issues." + shimLabel + ".json": tc.shimList,
			}, "RUN_URL="+runURL, "DEV_SHA=aaaa", "MAIN_SHA=bbbb",
				"DEV_RESULT="+tc.dev, "MAIN_RESULT="+tc.main, "SHIM_RESULT="+tc.shim, "SHIM_STATE="+tc.state)
			if run.status != tc.wantStatus {
				t.Fatalf("exit %d, want %d; gh calls:\n%s", run.status, tc.wantStatus, run.log)
			}
			for _, want := range tc.want {
				if !strings.Contains(run.log, want) {
					t.Fatalf("missing gh call %q in:\n%s", want, run.log)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(run.log, absent) {
					t.Fatalf("unexpected gh call %q in:\n%s", absent, run.log)
				}
			}
		})
	}
}

// A created issue must be found again the next night: the marker in its body, its
// title, author and label are what the lookup matches. If any were lost a duplicate
// would open every red night. Each run here reads what the previous run created.
func TestNightlyDesktopReportFindsItsOwnIssues(t *testing.T) {
	steps := stepsOf(asMap(jobsOf(readYAMLMap(t, "nightly-desktop.yml"))["report"]))
	script, _ := steps[0]["run"].(string)
	for _, tc := range []struct {
		name, title, label, dev, main, shim, state string
	}{
		{"desktop issue", "ci: nightly desktop validation failing", "ci-nightly-desktop", "failure", "success", "success", "needed"},
		{"shim issue", "ci: desktop-removal shim check (#1595)", "ci-desktop-removal-shim", "success", "success", "success", "dead"},
		{"shim check failure", "ci: desktop-removal shim check (#1595)", "ci-desktop-removal-shim", "success", "success", "failure", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			env := []string{"RUN_URL=https://example/run/1", "DEV_SHA=aaaa", "MAIN_SHA=bbbb",
				"DEV_RESULT=" + tc.dev, "MAIN_RESULT=" + tc.main, "SHIM_RESULT=" + tc.shim, "SHIM_STATE=" + tc.state}
			first := runStubbedIn(t, dir, script, nil, env...)
			if first.status != 1 || strings.Count(first.log, "issue create") != 1 || !strings.Contains(first.log, "issue create --title "+tc.title+" --label "+tc.label) {
				t.Fatalf("first night, exit %d:\n%s", first.status, first.log)
			}
			second := runStubbedIn(t, dir, script, nil, env...)
			if second.status != 1 || !strings.Contains(second.log, "issue comment 100 ") || strings.Contains(second.log, "issue create") {
				t.Fatalf("second night did not comment on the issue the first one created, exit %d:\n%s", second.status, second.log)
			}
		})
	}
}
