package ci_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// nightly-desktop.yml carries the desktop validation that left ci.yml (owner decision
// 2026-11-25). These tests make sure it cannot be silently emptied: every real step
// must stay gated on the module and run inside it, both heads must resolve, the
// schedule must fire daily, and the reporter must really open, update and close.

const (
	desktopModuleGuard = "${{ hashFiles('desktop/go.mod') != '' }}"
	reportCondition    = "always() && github.ref == 'refs/heads/main'"
)

var (
	dailyCron      = regexp.MustCompile(`^([0-9]|[1-5][0-9]) ([0-9]|1[0-9]|2[0-3]) \* \* \*$`)
	narrowedTestRE = regexp.MustCompile(`(^|\s)-(run|skip)([=\s]|$)`)
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
	for _, step := range stepsOf(heads) {
		run, _ := step["run"].(string)
		for _, required := range []string{"for branch in dev main", "echo macos-15", "echo ubuntu-latest", "${branch}_runner"} {
			if !strings.Contains(run, required) {
				return fmt.Errorf("heads resolver lost %q", required)
			}
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
			"Desktop module boundary":                       "check-desktop-deps.sh",
			"Desktop tests with the race detector":          "go test -race",
			"Build the probe shell":                         "-tags probe",
			"Build the release shell":                       "go build",
			"Driven WKWebView lane against a fake instance": "probes/driven",
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
			if !strings.Contains(run, command) || narrowedTestRE.MatchString(run) {
				return fmt.Errorf("%s step %q lost %q or narrowed its tests", leg, name, command)
			}
			seen[name] = true
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
	if !reflect.DeepEqual(needsOf(report), []string{"heads", "desktop-dev", "desktop-main"}) || report["if"] != reportCondition {
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
		"heads resolves only main": func(w yamlMap) {
			s := stepsOf(heads(w))[0]
			s["run"] = strings.Replace(s["run"].(string), "for branch in dev main", "for branch in main", 1)
		},
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

const stubGH = `#!/bin/bash
echo "$*" >>"$GH_LOG"
if [ "$1 $2" = "issue list" ]; then
  while [ $# -gt 0 ]; do
    if [ "$1" = --jq ]; then jq -r "$2" <"$GH_ISSUES"; exit 0; fi
    shift
  done
fi
`

// Run the report script against a stub gh; return its exit status and the gh calls.
func runReport(t *testing.T, script, issues, devResult, mainResult string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stubGH), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "issues.json"), []byte(issues), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GH_LOG="+filepath.Join(dir, "log"), "GH_ISSUES="+filepath.Join(dir, "issues.json"),
		"GH_TOKEN=x", "GH_REPO=o/r", "RUN_URL=https://example/run/1", "DEV_SHA=aaaa", "MAIN_SHA=bbbb",
		"DEV_RESULT="+devResult, "MAIN_RESULT="+mainResult)
	status := 0
	if out, err := cmd.CombinedOutput(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("%v: %s", err, out)
		}
		status = exitErr.ExitCode()
	}
	log, _ := os.ReadFile(filepath.Join(dir, "log"))
	return status, string(log)
}

func TestNightlyDesktopReportLifecycle(t *testing.T) {
	steps := stepsOf(asMap(jobsOf(readYAMLMap(t, "nightly-desktop.yml"))["report"]))
	if len(steps) != 1 {
		t.Fatal("report job must have exactly one step")
	}
	script, _ := steps[0]["run"].(string)
	const title = "ci: nightly desktop validation failing"
	const marker = "<!-- hikyo-nightly-desktop -->"
	// A same-titled issue by someone else sorts first and must never be touched.
	existing := `[{"number":8,"title":"` + title + `","author":{"login":"someone"},"body":"` + marker + `"},` +
		`{"number":7,"title":"` + title + `","author":{"login":"app/github-actions"},"body":"` + marker + `\n\nold"},` +
		`{"number":9,"title":"unrelated","author":{"login":"app/github-actions"},"body":"` + marker + `"}]`
	for _, tc := range []struct {
		name       string
		issues     string
		dev, main  string
		wantStatus int
		want       []string
		absent     []string
	}{
		{"open a new issue", "[]", "failure", "success", 1,
			[]string{"issue list --state open --label ci-nightly-desktop", "label create ci-nightly-desktop", "issue create --title " + title + " --label ci-nightly-desktop"},
			[]string{"issue close", "issue comment"}},
		{"comment on the existing issue", existing, "success", "failure", 1,
			[]string{"issue comment 7 "}, []string{"issue create", "issue close", "issue comment 8", "issue comment 9"}},
		{"a cancelled leg counts as failing", existing, "cancelled", "success", 1,
			[]string{"issue comment 7 "}, []string{"issue close", "issue create"}},
		{"skipped legs count as failing", existing, "skipped", "skipped", 1,
			[]string{"issue comment 7 "}, []string{"issue close", "issue create"}},
		{"close on a green night", existing, "success", "success", 0,
			[]string{"issue close 7 "}, []string{"issue create", "issue comment", "issue close 8", "issue close 9"}},
		{"nothing to do on a green night", "[]", "success", "success", 0,
			[]string{"issue list"}, []string{"issue create", "issue comment", "issue close", "label create"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, calls := runReport(t, script, tc.issues, tc.dev, tc.main)
			if status != tc.wantStatus {
				t.Fatalf("exit %d, want %d; gh calls:\n%s", status, tc.wantStatus, calls)
			}
			for _, want := range tc.want {
				if !strings.Contains(calls, want) {
					t.Fatalf("missing gh call %q in:\n%s", want, calls)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(calls, absent) {
					t.Fatalf("unexpected gh call %q in:\n%s", absent, calls)
				}
			}
		})
	}
}
