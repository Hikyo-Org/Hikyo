package ci_test

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/analysis-shards-step-legacy.sh
var legacyShardPlanningFixture string

//go:embed testdata/analysis-shards-step-all.sh
var completeShardPlanningFixture string

//go:embed testdata/analysis-shards-legacy.go.txt
var legacyShardPlannerSource string

var legacyShardPlanningRun = shardPlanningFixtureBody(legacyShardPlanningFixture)
var completeShardPlanningRun = shardPlanningFixtureBody(completeShardPlanningFixture)

// Standalone fixtures declare Bash for linting. Remove only that exact header
// so every byte of the embedded workflow body remains covered by comparisons.
func shardPlanningFixtureBody(fixture string) string {
	body, ok := strings.CutPrefix(fixture, "#!/usr/bin/env bash\n")
	if !ok {
		panic("shard planning fixture must declare Bash")
	}
	return body
}

// Scheduling changes may conserve the old inventory only through this exact,
// separately executed transformation. Other conditions, inputs and outputs
// remain covered by the original complete-job fingerprint.
func normalizeCompleteShardPlanning(job map[string]any) error {
	steps, ok := job["steps"].([]any)
	if !ok {
		return fmt.Errorf("analysis planner has no steps")
	}
	found := false
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("analysis planner has malformed steps")
		}
		if step["name"] != "Plan complete test, race, and fuzz shards" {
			continue
		}
		expected := map[string]any{
			"name": "Plan complete test, race, and fuzz shards", "id": "plan", "run": completeShardPlanningRun,
			"env": map[string]any{
				"BASE_SHA":   "${{ inputs.candidate && needs.changes.outputs.base_sha || github.event.pull_request.base.sha || github.event.merge_group.base_sha }}",
				"EVENT_NAME": "${{ github.event_name }}",
			},
		}
		if found || !reflect.DeepEqual(step, expected) {
			return fmt.Errorf("analysis planner changed its approved complete-plan execution contract")
		}
		found = true
		step["run"] = legacyShardPlanningRun
	}
	if !found {
		return fmt.Errorf("complete shard planning step is missing")
	}
	return nil
}

func TestCompleteShardWorkflowProvenanceAndMigration(t *testing.T) {
	root := repositoryRoot(t)
	workflow := readConsolidatedWorkflow(t, "ci")
	var script string
	for _, step := range workflow.Jobs["analysis_shards"].Steps {
		if step.Name == "Plan complete test, race, and fuzz shards" {
			script = step.Run
		}
	}
	if script != completeShardPlanningRun {
		t.Fatal("workflow must use the separately verified complete-plan protocol")
	}
	inventory := exec.Command("go", "list", "-json", "./...")
	inventory.Dir = root
	metadata, err := inventory.Output()
	if err != nil {
		t.Fatal(err)
	}
	newSource, err := os.ReadFile(filepath.Join(root, "scripts/ci/analysis-shards-go/main.go"))
	if err != nil {
		t.Fatal(err)
	}
	oldSource := []byte(legacyShardPlannerSource)
	if got := fmt.Sprintf("%x", sha256.Sum256(oldSource)); got != "1349c193de1ca689aa322bb71affdea4e287a972af38d0d5530a6978e22b2d3c" {
		t.Fatal("embedded trusted legacy source differs from the migration allowlist")
	}
	const badBulk = `package main
import ("fmt"; "os")
func main() { if len(os.Args)==2 && os.Args[1]=="--supports-all-v1" { fmt.Println("analysis-shards-all-v1"); return }; os.Exit(2) }
`
	const malformedBulk = `package main
import ("fmt"; "os")
func main() { if len(os.Args)==2 && os.Args[1]=="--supports-all-v1" { fmt.Println("analysis-shards-all-v1"); return }; fmt.Println("{}") }
`
	var expected map[string]json.RawMessage
	for _, scenario := range []struct {
		name, event, ref string
		source           []byte
		wantSuccess      bool
		wantCalls        int
	}{
		{"known legacy base", "merge_group", "", oldSource, true, 18},
		{"new trusted base", "merge_group", "", newSource, true, 1},
		{"trusted main", "workflow_dispatch", "refs/heads/main", newSource, true, 1},
		{"authenticated release tag", "push", "refs/tags/v1.2.3", newSource, true, 1},
		{"nonmain manual", "workflow_dispatch", "refs/heads/topic", newSource, false, 0},
		{"unknown legacy source", "merge_group", "", append(append([]byte(nil), oldSource...), '\n'), false, 0},
		{"bulk error has no fallback", "merge_group", "", []byte(badBulk), false, 0},
		{"malformed bulk plan", "merge_group", "", []byte(malformedBulk), false, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			work := t.TempDir()
			trusted := filepath.Join(work, "trusted.go")
			output := filepath.Join(work, "outputs")
			marker := filepath.Join(work, "candidate-executed")
			calls := filepath.Join(work, "calls")
			inventoryPath := filepath.Join(work, "inventory.json")
			goStub := filepath.Join(work, "go-list")
			candidatePath := filepath.Join(work, "scripts/ci/analysis-shards-go/main.go")
			if err := os.MkdirAll(filepath.Dir(candidatePath), 0700); err != nil {
				t.Fatal(err)
			}
			candidate := []byte("package main\nimport \"os\"\nfunc main(){ os.WriteFile(os.Getenv(\"CANDIDATE_MARKER\"), []byte(\"executed\"), 0600); os.Exit(1) }\n")
			if scenario.event == "workflow_dispatch" || scenario.event == "push" {
				// The caller guard authenticates main or the canonical release tag,
				// and the release classifier verifies its peeled main ancestry.
				candidate = scenario.source
			}
			for path, contents := range map[string][]byte{
				trusted: scenario.source, inventoryPath: metadata, candidatePath: candidate,
				goStub: []byte("#!/bin/sh\nset -eu\ntest \"$*\" = 'list -json ./...'\nprintf 'list\\n' >>\"$PLANNER_CALLS\"\ncat \"$PLANNER_INVENTORY\"\n"),
			} {
				if err := os.WriteFile(path, contents, 0700); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command("bash", "-c", "git() { test \"$1\" = show && test \"$2\" = \"$BASE_SHA:scripts/ci/analysis-shards-go/main.go\" && cat \"$TRUSTED_SOURCE\"; }\n"+script)
			command.Dir = work
			command.Env = append(os.Environ(), "EVENT_NAME="+scenario.event, "GITHUB_REF="+scenario.ref,
				"BASE_SHA="+strings.Repeat("a", 40), "RUNNER_TEMP="+work, "GITHUB_OUTPUT="+output,
				"TRUSTED_SOURCE="+trusted, "GO_BIN="+goStub, "PLANNER_CALLS="+calls,
				"PLANNER_INVENTORY="+inventoryPath, "CANDIDATE_MARKER="+marker)
			logs, runErr := command.CombinedOutput()
			if (runErr == nil) != scenario.wantSuccess {
				t.Fatalf("success=%t, error=%v\n%s", scenario.wantSuccess, runErr, logs)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("candidate planner code executed")
			}
			callBytes, err := os.ReadFile(calls)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if got := bytes.Count(callBytes, []byte("list\n")); got != scenario.wantCalls {
				t.Fatalf("package inventories=%d, want %d", got, scenario.wantCalls)
			}
			data, err := os.ReadFile(output)
			if !scenario.wantSuccess {
				if err == nil && len(data) != 0 {
					t.Fatal("refused planning wrote partial downstream outputs")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			actual := map[string]json.RawMessage{}
			for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
				key, value, ok := strings.Cut(line, "=")
				if !ok || actual[key] != nil || !json.Valid([]byte(value)) {
					t.Fatal("invalid or duplicate workflow output")
				}
				actual[key] = json.RawMessage(value)
			}
			if len(actual) != 5 {
				t.Fatal("workflow lost complete matrix outputs")
			}
			if expected == nil {
				expected = actual
			} else if !reflect.DeepEqual(actual, expected) {
				t.Fatal("legacy and bulk outputs differ on the same candidate inventory")
			}
		})
	}
}
