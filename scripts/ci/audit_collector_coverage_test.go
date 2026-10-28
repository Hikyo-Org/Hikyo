package ci_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAuditCollectorFixtureCoverageAdditionIsExact(t *testing.T) {
	baseline, original := readCoverageBaseline(t)
	for _, mutation := range []string{
		"unmutated", "removed", "duplicated", "renamed", "skipped", "conditional",
		"fail-open step", "fail-open command", "narrowed suite", "wrong suite",
		"ambient imports", "bytecode writes", "duplicated command", "environment",
		"working directory", "skipped job", "fail-open job", "removed baseline check",
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
				if value.(map[string]any)["name"] == "Independent audit receiver rehearsal" {
					if index != -1 {
						t.Fatal("fixture owner is duplicated")
					}
					index = i
				}
			}
			if index == -1 {
				t.Fatal("fixture owner is missing")
			}
			step := steps[index].(map[string]any)
			run := step["run"].(string)
			switch mutation {
			case "unmutated":
			case "removed":
				job["steps"] = append(steps[:index], steps[index+1:]...)
			case "duplicated":
				job["steps"] = append(steps, step)
			case "renamed":
				step["name"] = "Renamed receiver rehearsal"
			case "skipped":
				step["if"] = "false"
			case "conditional":
				step["if"] = "github.event_name == 'pull_request'"
			case "fail-open step":
				step["continue-on-error"] = true
			case "fail-open command":
				step["run"] = run + " || true"
			case "narrowed suite":
				step["run"] = strings.Replace(run, "test_*.py", "test_subset.py", 1)
			case "wrong suite":
				step["run"] = strings.Replace(run, "scripts/ops/audit-collector", "scripts/wrong", 1)
			case "ambient imports":
				step["run"] = strings.Replace(run, " -I", "", 1)
			case "bytecode writes":
				step["run"] = strings.Replace(run, " -B", "", 1)
			case "duplicated command":
				step["run"] = run + "\n" + run
			case "environment":
				step["env"] = map[string]any{"PYTHONPATH": "untrusted"}
			case "working directory":
				step["working-directory"] = "scripts/ops/audit-collector"
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
			if mutation == "unmutated" {
				if err != nil {
					t.Fatalf("exact mandatory addition must conserve baseline: %v", err)
				}
			} else if err == nil {
				t.Fatal("collector fixture mutation escaped the frozen execution inventory")
			}
		})
	}
}
