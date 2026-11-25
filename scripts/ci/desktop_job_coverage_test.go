package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// The macOS desktop job (#1215) is additive: it builds and drives the nested
// desktop module and no frozen job changed. Validate the entire job against a
// reviewed fixture, then take it and its ci-required dependency out of the
// frozen inventory, like normalizeTestCoreSerial.
func normalizeDesktopJob(workflow map[string]map[string]any) error {
	const jobName = "desktop"
	job, exists := workflow[jobName]
	if !exists {
		return fmt.Errorf("%s job is missing", jobName)
	}
	jobSteps, ok := job["steps"].([]any)
	guardSteps, guardOK := workflow["changes"]["steps"].([]any)
	if !ok || !guardOK || len(jobSteps) == 0 || len(guardSteps) == 0 || !reflect.DeepEqual(jobSteps[0], guardSteps[0]) {
		return fmt.Errorf("%s lost the immutable caller guard", jobName)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "desktop-job.yml"))
	if err != nil {
		return err
	}
	var expected map[string]any
	if err := yaml.Unmarshal(fixture, &expected); err != nil {
		return err
	}
	actual := make(map[string]any, len(job))
	for key, value := range job {
		actual[key] = value
	}
	actual["steps"] = jobSteps[1:]
	if !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("%s lost its complete fail-closed configuration", jobName)
	}
	needs, ok := workflow["ci-required"]["needs"].([]any)
	if !ok {
		return fmt.Errorf("%s lost required aggregate dependencies", jobName)
	}
	kept := make([]any, 0, len(needs))
	found := 0
	for _, need := range needs {
		if need == jobName {
			found++
		} else {
			kept = append(kept, need)
		}
	}
	if found != 1 {
		return fmt.Errorf("%s must gate ci-required exactly once", jobName)
	}
	workflow["ci-required"]["needs"] = kept
	delete(workflow, jobName)
	return nil
}

func TestDesktopJobIsExactAndRequired(t *testing.T) {
	_, raw := readCoverageBaseline(t)
	for _, mutation := range []string{"unmutated", "unpinned runner", "always macOS", "ungated macOS step", "skipped condition", "fail-open step", "lost race tests", "lost driven lane", "not required", "removed"} {
		t.Run(mutation, func(t *testing.T) {
			var workflow struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			job := workflow.Jobs["desktop"]
			steps, _ := job["steps"].([]any)
			switch mutation {
			case "unpinned runner":
				job["runs-on"] = "macos-latest"
			case "always macOS":
				job["runs-on"] = "macos-15"
			case "ungated macOS step":
				for _, step := range steps {
					if step.(map[string]any)["name"] == "Build the release shell" {
						delete(step.(map[string]any), "if")
					}
				}
			case "skipped condition":
				job["if"] = "false"
			case "fail-open step":
				steps[len(steps)-1].(map[string]any)["continue-on-error"] = true
			case "lost race tests":
				for _, step := range steps {
					if step.(map[string]any)["name"] == "Desktop tests with the race detector" {
						step.(map[string]any)["run"] = "go test ./..."
					}
				}
			case "lost driven lane":
				job["steps"] = steps[:len(steps)-1]
			case "not required":
				needs := workflow.Jobs["ci-required"]["needs"].([]any)
				kept := needs[:0]
				for _, need := range needs {
					if need != "desktop" {
						kept = append(kept, need)
					}
				}
				workflow.Jobs["ci-required"]["needs"] = kept
			case "removed":
				delete(workflow.Jobs, "desktop")
			}
			err := normalizeDesktopJob(workflow.Jobs)
			if (mutation == "unmutated") != (err == nil) {
				t.Fatalf("%s: normalization error %v", mutation, err)
			}
		})
	}
}
