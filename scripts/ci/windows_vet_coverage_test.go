package ci_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const windowsVetStepName = "Windows vet"

// #1393 adds a native Windows go vet to scan-xplat. It is additive to the
// frozen inventory: require it exactly once, conditional only on the Windows
// leg and fail-closed, then omit it for the digest comparison.
func normalizeWindowsVetAddition(job map[string]any) error {
	expected := map[string]any{
		"name": windowsVetStepName,
		"if":   "${{ runner.os == 'Windows' }}",
		"run":  "go vet ./...",
	}
	steps, ok := job["steps"].([]any)
	if !ok {
		return fmt.Errorf("scan-xplat lost its steps")
	}
	kept := make([]any, 0, len(steps))
	found := false
	previous := ""
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("scan-xplat has malformed step configuration")
		}
		name, _ := step["name"].(string)
		if name != expected["name"] {
			kept = append(kept, step)
			previous = name
			continue
		}
		// After checkout and Go setup, so the step compiles the validated revision.
		if found || !reflect.DeepEqual(step, expected) || previous != "Windows credential custody tests" {
			return fmt.Errorf("scan-xplat requires exactly one fail-closed Windows vet step after the Windows custody tests")
		}
		found = true
	}
	if !found {
		return fmt.Errorf("scan-xplat lost its mandatory Windows vet step")
	}
	job["steps"] = kept
	return nil
}

func TestWindowsVetAdditionIsExact(t *testing.T) {
	baseline, original := readCoverageBaseline(t)
	for _, mutation := range []string{
		"unchanged", "removed", "duplicated", "conditional", "fail open", "narrowed",
		"wrong shell", "environment", "working directory", "before checkout", "skipped job", "fail-open job",
	} {
		t.Run(mutation, func(t *testing.T) {
			var workflow struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(original, &workflow); err != nil {
				t.Fatal(err)
			}
			job := workflow.Jobs["scan-xplat"]
			steps := job["steps"].([]any)
			index := -1
			for i, value := range steps {
				if value.(map[string]any)["name"] == windowsVetStepName {
					index = i
					break
				}
			}
			if index < 0 {
				t.Fatal("missing Windows vet step")
			}
			step := steps[index].(map[string]any)
			switch mutation {
			case "unchanged":
			case "removed":
				job["steps"] = append(append([]any{}, steps[:index]...), steps[index+1:]...)
			case "duplicated":
				job["steps"] = append(steps, step)
			case "conditional":
				step["if"] = "false"
			case "fail open":
				step["continue-on-error"] = true
			case "narrowed":
				step["run"] = strings.Replace(step["run"].(string), "./...", "./internal/app", 1)
			case "wrong shell":
				step["shell"] = "pwsh"
			case "environment":
				step["env"] = map[string]any{"GOFLAGS": "-run=none"}
			case "working directory":
				step["working-directory"] = "internal"
			case "before checkout":
				steps[0], steps[index] = steps[index], steps[0]
			case "skipped job":
				job["if"] = "false"
			case "fail-open job":
				job["continue-on-error"] = true
			default:
				t.Fatal("unknown mutation")
			}
			raw, err := yaml.Marshal(workflow)
			if err != nil {
				t.Fatal(err)
			}
			err = requireBaselineCoverage(raw, baseline)
			if mutation == "unchanged" {
				if err != nil {
					t.Fatalf("exact Windows vet addition must conserve baseline: %v", err)
				}
			} else if err == nil {
				t.Fatal("Windows vet mutation escaped the frozen coverage inventory")
			}
		})
	}
}
