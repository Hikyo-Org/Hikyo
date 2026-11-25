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

func checkNightlyDesktop(workflow yamlMap) error {
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
	outputs := asMap(heads["outputs"])
	for _, name := range []string{"dev", "main", "dev_runner", "main_runner"} {
		if outputs[name] != "${{ steps.resolve."+"outputs."+name+" }}" {
			return fmt.Errorf("heads lost its %s output", name)
		}
	}
	shim := asMap(jobs["shim"])
	if shim == nil || shim["if"] != "github.ref == 'refs/heads/main'" || shim["continue-on-error"] != nil || shim["permissions"] != nil ||
		!reflect.DeepEqual(needsOf(shim), []string{"heads"}) || asMap(shim["outputs"])["state"] != "${{ steps.check.outputs.state }}" {
		return fmt.Errorf("shim must run only on main after heads, fail closed, stay read-only and expose its state")
	}
	for _, step := range stepsOf(shim) {
		if step["uses"] != nil {
			return fmt.Errorf("shim job must not check out or run any action")
		}
	}
	for leg, branch := range map[string]string{"desktop-dev": "dev", "desktop-main": "main"} {
		job := asMap(jobs[leg])
		if job == nil {
			return fmt.Errorf("%s job is missing", leg)
		}
		if job["runs-on"] != "${{ needs.heads.outputs."+branch+"_runner }}" || job["continue-on-error"] != nil || job["permissions"] != nil || job["if"] != nil {
			return fmt.Errorf("%s must take the resolved runner, fail closed, always run and inherit read-only permissions", leg)
		}
		if !reflect.DeepEqual(needsOf(job), []string{"heads"}) {
			return fmt.Errorf("%s must depend only on the head resolver", leg)
		}
		if asMap(job["env"])["HEAD_SHA"] != "${{ needs.heads.outputs."+branch+" }}" {
			return fmt.Errorf("%s does not test the pinned %s head", leg, branch)
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
				if step["if"] != nil {
					return fmt.Errorf("%s has a conditional setup step", leg)
				}
				if strings.HasPrefix(uses, "actions/checkout@") && asMap(step["with"])["ref"] != "${{ env.HEAD_SHA }}" {
					return fmt.Errorf("%s checks out something other than the pinned head", leg)
				}
				continue
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
	if !reflect.DeepEqual(needsOf(report), []string{"heads", "desktop-dev", "desktop-main", "shim"}) || report["if"] != reportCondition {
		return fmt.Errorf("report must wait for both legs, run after any result, and only on main")
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
// issues.json for `issue list --jq`, sha.<branch> for a branch head, module.<sha>
// holding an HTTP status for desktop/go.mod, and content.<ref>.<path, / as _> for raw
// file contents (404 when absent).
const stubGH = `#!/bin/bash
echo "$*" >>"$GH_LOG"
if [ "$1 $2" = "issue list" ]; then
  while [ $# -gt 0 ]; do
    if [ "$1" = --jq ]; then jq -r "$2" <"$GH_DIR/issues.json"; exit 0; fi
    shift
  done
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
	dir := t.TempDir()
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
		{"a registry cannot be read", withShim, unlisted, "", "", true},
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
		"always dead":        {"state=needed", "state=dead"},
		"always needed":      {"state=dead", "state=needed"},
		"ignores dev":        {` || [ "$dev_has" = true ]`, ""},
		"ignores main":       {`[ "$main_has" = true ] || `, ""},
		"ignores the marker": {`if grep -qF ci-job-registry.without-desktop.json <<<"$ci"; then`, "if true; then"},
		"swallows an error":  {`dev_registry=$(fetch scripts/ci/ci-job-registry.json "$DEV_SHA")`, `dev_registry=$(fetch scripts/ci/ci-job-registry.json "$DEV_SHA" || true)`},
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
	const title = "ci: nightly desktop validation failing"
	const marker = "<!-- hikyo-nightly-desktop -->"
	issue := func(number int, issueTitle, author, body string) string {
		return fmt.Sprintf(`{"number":%d,"title":%q,"author":{"login":%q},"body":%q}`, number, issueTitle, author, body)
	}
	// Everything sorts ahead of #7, the only issue the report may touch: a wrong title,
	// a missing marker and a human author must each be rejected on their own.
	existing := "[" + strings.Join([]string{
		issue(5, "unrelated", "app/github-actions", marker),
		issue(6, title, "app/github-actions", "no marker here"),
		issue(8, title, "someone", marker),
		issue(7, title, "app/github-actions", marker+"\n\nold"),
	}, ",") + "]"
	untouched := []string{"issue comment 5", "issue comment 6", "issue comment 8", "issue close 5", "issue close 6", "issue close 8"}
	for _, tc := range []struct {
		name         string
		issues       string
		dev, main    string
		shim, state  string
		wantStatus   int
		want, absent []string
	}{
		{"open a new issue", "[]", "failure", "success", "success", "needed", 1,
			[]string{"issue list --state open --label ci-nightly-desktop", "label create ci-nightly-desktop", "issue create --title " + title + " --label ci-nightly-desktop"},
			[]string{"issue close", "issue comment"}},
		{"comment on the existing issue", existing, "success", "failure", "success", "needed", 1,
			[]string{"issue comment 7 "}, append([]string{"issue create", "issue close"}, untouched...)},
		{"a cancelled leg counts as failing", existing, "cancelled", "success", "success", "needed", 1,
			[]string{"issue comment 7 "}, []string{"issue close", "issue create"}},
		{"skipped legs count as failing", existing, "skipped", "skipped", "success", "needed", 1,
			[]string{"issue comment 7 "}, []string{"issue close", "issue create"}},
		{"a dead shim fails the night", existing, "success", "success", "success", "dead", 1,
			[]string{"issue comment 7 ", "desktop-removal shim in ci.yml is dead", "#1595"}, []string{"issue close", "issue create"}},
		{"an unreadable shim check fails the night", existing, "success", "success", "failure", "", 1,
			[]string{"issue comment 7 ", "desktop-removal shim check: failure"}, []string{"issue close", "issue create"}},
		{"close on a green night", existing, "success", "success", "success", "needed", 0,
			[]string{"issue close 7 "}, append([]string{"issue create", "issue comment"}, untouched...)},
		{"close once the shim is gone", existing, "success", "success", "success", "gone", 0,
			[]string{"issue close 7 "}, []string{"issue create", "issue comment"}},
		{"nothing to do on a green night", "[]", "success", "success", "success", "gone", 0,
			[]string{"issue list"}, []string{"issue create", "issue comment", "issue close", "label create"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := runStubbed(t, script, map[string]string{"issues.json": tc.issues},
				"RUN_URL=https://example/run/1", "DEV_SHA=aaaa", "MAIN_SHA=bbbb",
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
