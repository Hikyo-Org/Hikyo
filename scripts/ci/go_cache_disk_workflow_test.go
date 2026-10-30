package ci_test

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const goReuseDiskStep = "Reserve disk for shared Go cache restore"
const goReuseDiskScript = "./scripts/ci/prepare-go-cache-disk.sh"

// Permit this exact capacity preparation without replacing the frozen hashes
// of any existing validation, cache authority, runner or timeout.
func normalizeGoReuseDiskPreparation(jobs map[string]map[string]any) error {
	readers := map[string]bool{
		"supply-chain-checks": true, "app-build": true, "client": true,
		"compose-demo": true, "web-go": true, "test_core": true,
		"isolation_shard": true, "headline-guarantee": true,
	}
	want := map[string]any{"name": goReuseDiskStep, "run": goReuseDiskScript, "timeout-minutes": 3}
	for name, job := range jobs {
		steps, _ := job["steps"].([]any)
		prepared, restored := -1, -1
		for i, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: malformed step", name)
			}
			run, _ := step["run"].(string)
			if step["name"] == goReuseDiskStep || strings.Contains(run, goReuseDiskScript) {
				if !readers[name] || prepared >= 0 || !reflect.DeepEqual(step, want) {
					return fmt.Errorf("%s: disk preparation must be one exact unconditional step on a shared Go cache reader", name)
				}
				prepared = i
			}
			with, _ := step["with"].(map[string]any)
			key, _ := with["key"].(string)
			uses, _ := step["uses"].(string)
			if strings.HasPrefix(uses, "actions/cache/restore@") && strings.HasPrefix(key, "go-test-v2-") {
				if restored >= 0 {
					return fmt.Errorf("%s: duplicate shared Go cache restore", name)
				}
				restored = i
			}
		}
		if readers[name] {
			if prepared < 0 || restored != prepared+1 {
				return fmt.Errorf("%s: disk preparation must immediately precede shared Go cache restore", name)
			}
			job["steps"] = append(steps[:prepared], steps[prepared+1:]...)
			delete(readers, name)
		}
	}
	if len(readers) != 0 {
		return fmt.Errorf("shared Go cache readers missing: %v", readers)
	}
	return nil
}

func TestGoReuseDiskPreparationWorkflow(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	load := func() map[string]map[string]any {
		t.Helper()
		var workflow struct {
			Jobs map[string]map[string]any `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		return workflow.Jobs
	}
	if err := normalizeGoReuseDiskPreparation(load()); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"missing", "conditional", "command", "deadline", "late", "duplicate", "unrelated-job"} {
		t.Run(mutation, func(t *testing.T) {
			jobs := load()
			steps := jobs["web-go"]["steps"].([]any)
			index := -1
			for i, value := range steps {
				if value.(map[string]any)["name"] == goReuseDiskStep {
					index = i
				}
			}
			step := steps[index].(map[string]any)
			switch mutation {
			case "missing":
				steps = append(steps[:index], steps[index+1:]...)
			case "conditional":
				step["if"] = "false"
			case "command":
				step["run"] = "true"
			case "deadline":
				step["timeout-minutes"] = 4
			case "late":
				steps[index], steps[index+1] = steps[index+1], steps[index]
			case "duplicate":
				steps = append(steps, step)
			case "unrelated-job":
				jobs["preflight"]["steps"] = append(jobs["preflight"]["steps"].([]any), step)
			}
			jobs["web-go"]["steps"] = steps
			if err := normalizeGoReuseDiskPreparation(jobs); err == nil {
				t.Fatal("accepted weakened or misplaced disk preparation")
			}
		})
	}
}

func TestGoReuseDiskPreparationFixtures(t *testing.T) {
	cmd := exec.Command("bash", "prepare-go-cache-disk_test.sh")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Go cache disk fixtures: %v\n%s", err, output)
	}
}
