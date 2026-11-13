package ci_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Preserve the historical job digest. Admit only the exact additional owner
// suite, after its Helm/chart prerequisites, without weakening any older check.
func normalizeGitOpsFixtureAddition(job map[string]any) error {
	expected := map[string]any{
		"name": "GitOps chart trust, singleton values and controller policy fixtures",
		"run":  "python3 -B -m unittest discover -s scripts/deploy -p 'test_*.py'",
	}
	steps, ok := job["steps"].([]any)
	if !ok {
		return fmt.Errorf("GitOps fixtures lost their owner job steps")
	}
	kept := make([]any, 0, len(steps))
	found := false
	previous := ""
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("GitOps fixture owner has malformed steps")
		}
		if step["name"] == expected["name"] {
			if found || !reflect.DeepEqual(step, expected) || previous != "Chart renders across modes with hardening and refusal assertions" {
				return fmt.Errorf("GitOps fixtures require one exact fail-closed suite after the Helm/chart checks")
			}
			found = true
		} else {
			kept = append(kept, step)
		}
		previous, _ = step["name"].(string)
	}
	if !found {
		return fmt.Errorf("missing mandatory GitOps fixture owner suite")
	}
	job["steps"] = kept
	return nil
}

func TestGitOpsFixtureCoverageAdditionIsExact(t *testing.T) {
	baseline, original := readCoverageBaseline(t)
	for _, mutation := range []string{
		"unchanged", "removed", "duplicated", "conditional", "fail open", "wrong suite",
		"narrowed tests", "bytecode writes", "environment", "working directory",
		"before prerequisites", "skipped job", "fail-open job", "removed baseline check",
	} {
		t.Run(mutation, func(t *testing.T) {
			var workflow struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(original, &workflow); err != nil {
				t.Fatal(err)
			}
			job := workflow.Jobs["supply-chain-checks"]
			steps := job["steps"].([]any)
			index := -1
			for i, value := range steps {
				if value.(map[string]any)["name"] == "GitOps chart trust, singleton values and controller policy fixtures" {
					index = i
					break
				}
			}
			if index < 1 {
				t.Fatal("missing GitOps owner suite or its prerequisite")
			}
			step := steps[index].(map[string]any)
			run := step["run"].(string)
			switch mutation {
			case "unchanged":
			case "removed":
				job["steps"] = append(steps[:index], steps[index+1:]...)
			case "duplicated":
				job["steps"] = append(steps, step)
			case "conditional":
				step["if"] = "false"
			case "fail open":
				step["continue-on-error"] = true
			case "wrong suite":
				step["run"] = strings.Replace(run, "scripts/deploy", "scripts/wrong", 1)
			case "narrowed tests":
				step["run"] = strings.Replace(run, "test_*.py", "test_subset.py", 1)
			case "bytecode writes":
				step["run"] = strings.Replace(run, " -B", "", 1)
			case "environment":
				step["env"] = map[string]any{"PYTHONPATH": "different"}
			case "working directory":
				step["working-directory"] = "scripts/deploy"
			case "before prerequisites":
				steps[index-1], steps[index] = steps[index], steps[index-1]
			case "skipped job":
				job["if"] = "false"
			case "fail-open job":
				job["continue-on-error"] = true
			case "removed baseline check":
				job["steps"] = append(steps[:index-1], steps[index:]...)
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
					t.Fatalf("exact GitOps addition must conserve baseline: %v", err)
				}
			} else if err == nil {
				t.Fatal("GitOps fixture mutation escaped the frozen coverage inventory")
			}
		})
	}
}
