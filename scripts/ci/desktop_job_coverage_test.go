package ci_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
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

func yamlNeedsOf(job yamlMap) []string {
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
			for _, need := range yamlNeedsOf(asMap(raw)) {
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
