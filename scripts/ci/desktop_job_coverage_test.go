package ci_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Owner decision 2026-11-25 (docs/development/ci.md, "Desktop shell (macOS)"): the
// macOS desktop validation (#1215) left the dev and merge-queue critical paths
// because a macOS runner was not acquired for 5 of 21 ci-dev attempts. It runs in
// nightly-desktop.yml against the dev and main heads, and a failure keeps one
// tracking issue open. These tests pin both halves: nothing required can depend on
// desktop again, and the nightly really runs it and can report.

type yamlMap = map[string]any

func readYAMLMap(t *testing.T, name string) yamlMap {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var workflow yamlMap
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func asMap(value any) yamlMap {
	m, _ := value.(yamlMap)
	return m
}

func asList(value any) []any {
	l, _ := value.([]any)
	return l
}

func jobsOf(workflow yamlMap) yamlMap { return asMap(workflow["jobs"]) }

func stepsOf(job yamlMap) []yamlMap {
	var steps []yamlMap
	for _, step := range asList(job["steps"]) {
		steps = append(steps, asMap(step))
	}
	return steps
}

func needsOf(job yamlMap) []string {
	switch needs := job["needs"].(type) {
	case string:
		return []string{needs}
	case []any:
		var names []string
		for _, need := range needs {
			names = append(names, fmt.Sprint(need))
		}
		return names
	}
	return nil
}

// Workflows whose jobs gate dev integration or the merge queue, directly or by
// calling ci.yml.
var requiredPathWorkflows = []string{"ci.yml", "ci-dev.yml", "ci-merge-group.yml", "ci-main.yml", "ci-dev-gate.yml", "ci-queue-gate.yml"}

func checkDesktopOffRequiredPaths(workflows map[string]yamlMap, registryJSON []byte) error {
	for name, workflow := range workflows {
		for jobName, raw := range jobsOf(workflow) {
			if strings.Contains(jobName, "desktop") {
				return fmt.Errorf("%s still has the %s job on a required path", name, jobName)
			}
			for _, need := range needsOf(asMap(raw)) {
				if strings.Contains(need, "desktop") {
					return fmt.Errorf("%s job %s needs %s", name, jobName, need)
				}
			}
		}
	}
	var registry struct {
		Jobs        map[string]any      `json:"jobs"`
		PathClasses map[string][]string `json:"path_classes"`
	}
	if err := json.Unmarshal(registryJSON, &registry); err != nil {
		return err
	}
	if _, ok := registry.Jobs["desktop"]; ok {
		return fmt.Errorf("the job registry still requires desktop")
	}
	for class, domains := range registry.PathClasses {
		if class == "desktop" {
			return fmt.Errorf("the job registry still has a desktop path class")
		}
		for _, domain := range domains {
			if domain == "desktop" {
				return fmt.Errorf("path class %s still selects desktop", class)
			}
		}
	}
	return nil
}

func checkNightlyDesktop(workflow yamlMap) error {
	on := asMap(workflow["on"])
	if len(asList(on["schedule"])) == 0 {
		return fmt.Errorf("nightly-desktop has no schedule")
	}
	if !reflect.DeepEqual(workflow["permissions"], yamlMap{"contents": "read"}) {
		return fmt.Errorf("workflow permissions must stay contents: read")
	}
	jobs := jobsOf(workflow)
	for leg, branch := range map[string]string{"desktop-dev": "dev", "desktop-main": "main"} {
		job := asMap(jobs[leg])
		if job == nil {
			return fmt.Errorf("%s job is missing", leg)
		}
		if job["runs-on"] != "macos-15" || job["continue-on-error"] != nil || job["permissions"] != nil {
			return fmt.Errorf("%s must run on macos-15, fail closed and inherit read-only permissions", leg)
		}
		if !reflect.DeepEqual(needsOf(job), []string{"heads"}) {
			return fmt.Errorf("%s must depend only on the head resolver", leg)
		}
		if asMap(job["env"])["HEAD_SHA"] != "${{ needs.heads.outputs."+branch+" }}" {
			return fmt.Errorf("%s does not test the pinned %s head", leg, branch)
		}
		have := map[string]string{}
		for _, step := range stepsOf(job) {
			if step["continue-on-error"] != nil {
				return fmt.Errorf("%s has a fail-open step", leg)
			}
			if uses, _ := step["uses"].(string); strings.HasPrefix(uses, "actions/checkout@") &&
				asMap(step["with"])["ref"] != "${{ env.HEAD_SHA }}" {
				return fmt.Errorf("%s checks out something other than the pinned head", leg)
			}
			name, _ := step["name"].(string)
			run, _ := step["run"].(string)
			have[name] = run
		}
		for name, command := range map[string]string{
			"Desktop module boundary":                       "check-desktop-deps.sh",
			"Desktop tests with the race detector":          "go test -race",
			"Build the probe shell":                         "-tags probe",
			"Build the release shell":                       "go build",
			"Driven WKWebView lane against a fake instance": "probes/driven",
		} {
			if !strings.Contains(have[name], command) {
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
	if !reflect.DeepEqual(needsOf(report), []string{"heads", "desktop-dev", "desktop-main"}) || report["if"] != "always()" {
		return fmt.Errorf("report must wait for both legs and run after any result")
	}
	for _, step := range stepsOf(report) {
		if step["uses"] != nil {
			return fmt.Errorf("report job must not check out or run any action")
		}
		run, _ := step["run"].(string)
		if strings.Contains(run, "${{") {
			return fmt.Errorf("report job interpolates an expression into its script")
		}
		for _, required := range []string{"gh issue create", "gh issue comment", "gh issue close", `DEV_RESULT" = success`, `MAIN_RESULT" = success`} {
			if !strings.Contains(run, required) {
				return fmt.Errorf("report script lost %q", required)
			}
		}
		if !strings.HasSuffix(strings.TrimSpace(run), "exit 1") {
			return fmt.Errorf("report script must fail the run after reporting")
		}
	}
	return nil
}

func TestDesktopIsOffTheRequiredPaths(t *testing.T) {
	registryJSON, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts/ci/ci-job-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	load := func() map[string]yamlMap {
		workflows := map[string]yamlMap{}
		for _, name := range requiredPathWorkflows {
			workflows[name] = readYAMLMap(t, name)
		}
		return workflows
	}
	if err := checkDesktopOffRequiredPaths(load(), registryJSON); err != nil {
		t.Fatal(err)
	}
	withDesktop := func(old, replacement string) []byte {
		changed := strings.Replace(string(registryJSON), old, replacement, 1)
		if changed == string(registryJSON) {
			t.Fatalf("registry fixture has no %q to mutate", old)
		}
		return []byte(changed)
	}
	for name, mutate := range map[string]func(map[string]yamlMap) []byte{
		"desktop job back in ci.yml": func(w map[string]yamlMap) []byte {
			jobsOf(w["ci.yml"])["desktop"] = yamlMap{"runs-on": "macos-15"}
			return registryJSON
		},
		"ci-required needs desktop": func(w map[string]yamlMap) []byte {
			required := asMap(jobsOf(w["ci.yml"])["ci-required"])
			required["needs"] = append(asList(required["needs"]), "desktop")
			return registryJSON
		},
		"dev workflow needs a desktop job": func(w map[string]yamlMap) []byte {
			asMap(jobsOf(w["ci-dev.yml"])["validation"])["needs"] = "desktop-dev"
			return registryJSON
		},
		"registry requires desktop": func(w map[string]yamlMap) []byte {
			return withDesktop(`"jobs": {`, `"jobs": {"desktop": {"required_gate": "planned", "plan_jobs": ["desktop"]},`)
		},
		"a path class selects desktop": func(w map[string]yamlMap) []byte {
			return withDesktop(`"docs": ["docs"]`, `"docs": ["docs", "desktop"]`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			workflows := load()
			registry := mutate(workflows)
			if err := checkDesktopOffRequiredPaths(workflows, registry); err == nil {
				t.Fatal("mutation was accepted")
			}
		})
	}
}

func TestNightlyDesktopRunsOnBothHeadsAndReports(t *testing.T) {
	if err := checkNightlyDesktop(readYAMLMap(t, "nightly-desktop.yml")); err != nil {
		t.Fatal(err)
	}
	report := func(w yamlMap) yamlMap { return asMap(jobsOf(w)["report"]) }
	leg := func(w yamlMap) yamlMap { return asMap(jobsOf(w)["desktop-main"]) }
	for name, mutate := range map[string]func(yamlMap){
		"no schedule":    func(w yamlMap) { delete(asMap(w["on"]), "schedule") },
		"a leg removed":  func(w yamlMap) { delete(jobsOf(w), "desktop-main") },
		"leg on linux":   func(w yamlMap) { leg(w)["runs-on"] = "ubuntu-latest" },
		"leg fails open": func(w yamlMap) { leg(w)["continue-on-error"] = true },
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
		"report writes contents": func(w yamlMap) {
			report(w)["permissions"] = yamlMap{"issues": "write", "contents": "write"}
		},
		"report has no permissions": func(w yamlMap) { delete(report(w), "permissions") },
		"report checks out code": func(w yamlMap) {
			checkout := yamlMap{"uses": "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"}
			report(w)["steps"] = append([]any{checkout}, asList(report(w)["steps"])...)
		},
		"report interpolates": func(w yamlMap) {
			step := stepsOf(report(w))[0]
			step["run"] = "echo ${{ github.event.head_commit.message }}\n" + step["run"].(string)
		},
		"report skipped on failure": func(w yamlMap) { report(w)["if"] = "success()" },
		"report ignores a leg":      func(w yamlMap) { report(w)["needs"] = []any{"heads", "desktop-dev"} },
		"report leaves the run green": func(w yamlMap) {
			step := stepsOf(report(w))[0]
			step["run"] = strings.TrimSuffix(strings.TrimSpace(step["run"].(string)), "exit 1")
		},
		"report never closes": func(w yamlMap) {
			step := stepsOf(report(w))[0]
			step["run"] = strings.ReplaceAll(step["run"].(string), "gh issue close", "true")
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
