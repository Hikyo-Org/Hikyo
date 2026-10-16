package ci_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type coverageBaseline struct {
	Version int               `json:"inventory_version"`
	Source  string            `json:"source_commit"`
	Domains []string          `json:"full_domains"`
	Jobs    map[string]string `json:"jobs"`
}

// Fingerprint complete execution configuration, including job conditions,
// dependencies, matrices, fail-open settings, and every step input/environment.
// Only the changed source guard/aggregate are excluded. Exact candidate base
// plumbing is normalized below; its runtime trust is covered independently.
func coverageDigests(raw []byte, candidate bool) (map[string]string, error) {
	var workflow struct {
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return nil, err
	}
	digests := make(map[string]string)
	for name, job := range workflow.Jobs {
		if name == "changes" { // classifier/full-plan policy has separate mutation tests
			continue
		}
		if steps, exists := job["steps"].([]any); exists {
			kept := make([]any, 0, len(steps))
			for _, value := range steps {
				step, ok := value.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%s has malformed step configuration", name)
				}
				if step["name"] == "Validate the immutable caller revision" || step["name"] == "Require every validation job" {
					continue
				}
				if candidate {
					// Preserve the original tool inputs while allowing only the
					// reviewed immutable live-base selector introduced by this split.
					allowed := map[string]string{
						"preflight":       "Require DCO sign-off on every PR commit",
						"analysis_shards": "Plan complete test, race, and fuzz shards",
						"race_shard":      "Race detector (all packages except the isolation suite)",
					}
					if step["name"] == allowed[name] {
						if env, ok := step["env"].(map[string]any); ok {
							old := "${{ github.event.pull_request.base.sha || github.event.merge_group.base_sha }}"
							current := "${{ inputs.candidate && needs.changes.outputs.base_sha || github.event.pull_request.base.sha || github.event.merge_group.base_sha }}"
							if name == "preflight" {
								old = "${{ github.event.pull_request.base.sha }}"
								current = "${{ inputs.candidate && needs.changes.outputs.base_sha || github.event.pull_request.base.sha }}"
							}
							if env["BASE_SHA"] == current {
								env["BASE_SHA"] = old
							}
						}
					}
				}
				kept = append(kept, step)
			}
			job["steps"] = kept
		}
		if candidate && name == "floor-bench" {
			with, ok := job["with"].(map[string]any)
			if !ok || with["candidate"] != "${{ inputs.candidate }}" {
				return nil, fmt.Errorf("floor benchmark lost exact candidate propagation")
			}
			delete(with, "candidate")
		}
		encoded, err := json.Marshal(job)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(encoded)
		digests[name] = hex.EncodeToString(digest[:])
	}
	return digests, nil
}

func requireBaselineCoverage(raw []byte, baseline coverageBaseline) error {
	// Main 48087bde06fd28546dfbca9bfb5b7c16f5b13002 (#884) added this
	// mandatory demo gate. Require its complete configuration first, then omit
	// only that exact additive step when comparing the unchanged e4 inventory.
	// Replacing the old hashes would conceal an unrelated execution reduction.
	var workflow struct {
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return err
	}
	if err := normalizeCompleteShardPlanning(workflow.Jobs["analysis_shards"]); err != nil {
		return err
	}
	if err := normalizeSecurityAdditions(workflow.Jobs); err != nil {
		return err
	}
	if err := normalizeImpactAdditions(workflow.Jobs); err != nil {
		return err
	}
	if err := normalizeMailFixtureAddition(workflow.Jobs); err != nil {
		return err
	}
	if err := normalizeDynamicRenewalFixture(workflow.Jobs["isolation_shard"]); err != nil {
		return err
	}
	// The feedback fixtures execute the real documentation checker. Pin its
	// owner runtime before the fixtures, and normalize only this exact addition.
	expectedNode := map[string]any{
		"name": "Select pinned Node for feedback fixtures",
		"uses": "actions/setup-node@2028fbc5c25fe9cf00d9f06a71cc4710d4507903",
		"with": map[string]any{"node-version-file": ".nvmrc"},
	}
	lintSteps, ok := workflow.Jobs["lint"]["steps"].([]any)
	if !ok {
		return fmt.Errorf("lint lost feedback fixture tooling")
	}
	keptLint := make([]any, 0, len(lintSteps))
	pinnedNode := false
	for _, value := range lintSteps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("lint has malformed step configuration")
		}
		if step["name"] == expectedNode["name"] {
			if pinnedNode || !reflect.DeepEqual(step, expectedNode) {
				return fmt.Errorf("feedback fixtures require exactly one unconditional pinned Node setup")
			}
			pinnedNode = true
		} else {
			if step["name"] == "Fast feedback refusal and deadline fixtures" && !pinnedNode {
				return fmt.Errorf("pinned Node must precede feedback fixture execution")
			}
			keptLint = append(keptLint, step)
		}
	}
	if !pinnedNode {
		return fmt.Errorf("feedback fixtures lost pinned Node setup")
	}
	workflow.Jobs["lint"]["steps"] = keptLint
	// PR886 moves the same five-package benchmark smoke into required core
	// validation, reusing its compiled packages. It is additive to this frozen
	// inventory, and must not become skipped, fail-open, narrowed, or duplicated.
	additions := map[string]map[string]any{
		// PR883 transfers all feedback supervisor/refusal fixtures from fast
		// feedback into full validation. Require the exact unconditional step.
		"lint": {
			"name": "Fast feedback refusal and deadline fixtures",
			"run":  "bash scripts/ci/fast-feedback_test.sh",
		},
		"storybook": {
			"name": "Static demo accounts and interactions", "working-directory": "web",
			"run": "pnpm run build:demo\npnpm run test:demo\n",
		},
		"test_core": {
			"name": "Exercise every benchmark once",
			"run":  "go test -run '^$' -bench . -benchtime=1x ./internal/crypto/ ./internal/dotenv/ ./internal/scanning/ ./internal/server/ ./internal/service/",
		},
	}
	for jobName, expected := range additions {
		steps, ok := workflow.Jobs[jobName]["steps"].([]any)
		if !ok {
			return fmt.Errorf("%s lost mandatory additive validation", jobName)
		}
		kept := make([]any, 0, len(steps))
		found := 0
		for _, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("%s has malformed step configuration", jobName)
			}
			if step["name"] == expected["name"] {
				if !reflect.DeepEqual(step, expected) {
					return fmt.Errorf("%s additive validation lost its exact fail-closed configuration", jobName)
				}
				found++
			} else {
				kept = append(kept, step)
			}
		}
		if found != 1 {
			return fmt.Errorf("%s requires exactly one mandatory additive validation step, got %d", jobName, found)
		}
		workflow.Jobs[jobName]["steps"] = kept
	}
	normalized, err := yaml.Marshal(workflow)
	if err != nil {
		return err
	}
	actual, err := coverageDigests(normalized, true)
	if err != nil {
		return err
	}
	for name, expected := range baseline.Jobs {
		if actual[name] != expected {
			return fmt.Errorf("%s no longer conserves baseline execution configuration and checks", name)
		}
	}
	return nil
}

func readCoverageBaseline(t *testing.T) (coverageBaseline, []byte) {
	t.Helper()
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts/ci/testdata/full-suite-baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	var baseline coverageBaseline
	if err = json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}
	if baseline.Version != 2 || baseline.Source != "e4bf44e5ce66237f58f81bdb965c2e548a6285a1" {
		t.Fatal("coverage inventory lost its measured pre-split source or execution schema")
	}
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return baseline, raw
}

func TestFullCandidateConservesBaselineCoverage(t *testing.T) {
	baseline, raw := readCoverageBaseline(t)
	var registry struct {
		Classes map[string][]string `json:"path_classes"`
	}
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts/ci/ci-job-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(registry.Classes["full"], baseline.Domains) {
		t.Fatal("full candidate must preserve all baseline validation domains")
	}
	if err = requireBaselineCoverage(raw, baseline); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageInventoryRejectsExecutionReductions(t *testing.T) {
	baseline, original := readCoverageBaseline(t)
	if err := requireBaselineCoverage(original, baseline); err != nil {
		t.Fatalf("unmutated workflow must conserve baseline coverage: %v", err)
	}
	for name, mutation := range map[string]func(map[string]map[string]any){
		"narrow complete shard counts": func(j map[string]map[string]any) {
			mutateShardPlanningStep(j, "run", replaceShardPlanningRunText("race_count=12", "race_count=6"))
		},
		"use candidate planner": func(j map[string]map[string]any) {
			mutateShardPlanningStep(j, "run", replaceShardPlanningRunText("git show \"$BASE_SHA:scripts/ci/analysis-shards-go/main.go\"", "git show \"HEAD:scripts/ci/analysis-shards-go/main.go\""))
		},
		"fail open complete planning": func(j map[string]map[string]any) { mutateShardPlanningStep(j, "continue-on-error", true) },
		"skip complete planning":      func(j map[string]map[string]any) { mutateShardPlanningStep(j, "if", "false") },
		"queue gate loses PR metadata permission": func(j map[string]map[string]any) {
			delete(j["ci-required"]["permissions"].(map[string]any), "pull-requests")
		},
		"queue gate gains PR writes": func(j map[string]map[string]any) {
			j["ci-required"]["permissions"].(map[string]any)["pull-requests"] = "write"
		},
		"skip impact fixtures": func(j map[string]map[string]any) {
			for _, step := range j["supply-chain-checks"]["steps"].([]any) {
				s := step.(map[string]any)
				if s["name"] == "Staged impact CI fixtures" {
					s["if"] = "false"
				}
			}
		},
		"narrow impact fixtures": func(j map[string]map[string]any) {
			for _, step := range j["supply-chain-checks"]["steps"].([]any) {
				s := step.(map[string]any)
				if s["name"] == "Staged impact CI fixtures" {
					s["run"] = "true"
				}
			}
		},
		"remove source artifact job": func(j map[string]map[string]any) {
			delete(j, "source-artifact-security")
		},
		"fail open source artifact job": func(j map[string]map[string]any) {
			j["source-artifact-security"]["continue-on-error"] = true
		},
		"source artifact job skips app build": func(j map[string]map[string]any) {
			j["source-artifact-security"]["needs"] = []any{"changes"}
		},
		"source artifact job changes plan": func(j map[string]map[string]any) {
			j["source-artifact-security"]["if"] = "${{ fromJSON(needs.changes.outputs.plan).web }}"
		},
		"source artifact job loses caller guard": func(j map[string]map[string]any) {
			j["source-artifact-security"]["steps"] = j["source-artifact-security"]["steps"].([]any)[1:]
		},
		"source artifact job is not required": func(j map[string]map[string]any) {
			needs := j["ci-required"]["needs"].([]any)
			for i, need := range needs {
				if need == "source-artifact-security" {
					j["ci-required"]["needs"] = append(needs[:i], needs[i+1:]...)
					return
				}
			}
		},
		"source artifact evidence uploads on failure": func(j map[string]map[string]any) {
			steps := j["source-artifact-security"]["steps"].([]any)
			steps[len(steps)-1].(map[string]any)["if"] = "always()"
		},
		"source artifact cleanup skips failure": func(j map[string]map[string]any) {
			steps := j["source-artifact-security"]["steps"].([]any)
			steps[len(steps)-2].(map[string]any)["if"] = "success()"
		},
		"increase original web Go timeout": func(j map[string]map[string]any) {
			j["web-go"]["timeout-minutes"] = 30
		},
		"remove fixture Node pin": func(j map[string]map[string]any) {
			steps := j["lint"]["steps"].([]any)
			for i, value := range steps {
				if value.(map[string]any)["name"] == "Select pinned Node for feedback fixtures" {
					j["lint"]["steps"] = append(steps[:i], steps[i+1:]...)
					return
				}
			}
		},
		"skip fixture Node pin":           func(j map[string]map[string]any) { mutateFeedbackNodeStep(j, "if", "false") },
		"fail open fixture Node pin":      func(j map[string]map[string]any) { mutateFeedbackNodeStep(j, "continue-on-error", true) },
		"unversioned fixture Node action": func(j map[string]map[string]any) { mutateFeedbackNodeStep(j, "uses", "actions/setup-node@v6") },
		"runner-default fixture Node": func(j map[string]map[string]any) {
			mutateFeedbackNodeStep(j, "with", map[string]any{"node-version": "latest"})
		},
		"fixture Node pin too late": func(j map[string]map[string]any) {
			steps := j["lint"]["steps"].([]any)
			for i, value := range steps {
				if value.(map[string]any)["name"] == "Select pinned Node for feedback fixtures" {
					steps[i], steps[i+1] = steps[i+1], steps[i]
					return
				}
			}
		},
		"remove feedback fixtures": func(j map[string]map[string]any) {
			steps := j["lint"]["steps"].([]any)
			for i, value := range steps {
				if value.(map[string]any)["name"] == "Fast feedback refusal and deadline fixtures" {
					j["lint"]["steps"] = append(steps[:i], steps[i+1:]...)
					return
				}
			}
		},
		"skip feedback fixtures":      func(j map[string]map[string]any) { mutateFeedbackFixtureStep(j, "if", "false") },
		"fail open feedback fixtures": func(j map[string]map[string]any) { mutateFeedbackFixtureStep(j, "continue-on-error", true) },
		"drop supervisor fixtures":    func(j map[string]map[string]any) { mutateFeedbackFixtureStep(j, "run", "true") },
		"skip job":                    func(j map[string]map[string]any) { j["test_core"]["if"] = "false" },
		"job fail open":               func(j map[string]map[string]any) { j["test_core"]["continue-on-error"] = true },
		"narrow platform matrix": func(j map[string]map[string]any) {
			j["scan-xplat"]["strategy"] = map[string]any{"matrix": map[string]any{"os": []any{"ubuntu-latest"}}}
		},
		"remove dependency":     func(j map[string]map[string]any) { j["test_core"]["needs"] = []any{} },
		"change reusable input": func(j map[string]map[string]any) { j["floor-bench"]["with"].(map[string]any)["revision"] = "HEAD" },
		"narrow step environment": func(j map[string]map[string]any) {
			for _, value := range j["race_shard"]["steps"].([]any) {
				step := value.(map[string]any)
				if step["name"] == "Race detector (all packages except the isolation suite)" {
					step["env"].(map[string]any)["RACE_SHARD"] = "0"
				}
			}
		},
		"step fail open": func(j map[string]map[string]any) {
			j["test_core"]["steps"].([]any)[1].(map[string]any)["continue-on-error"] = true
		},
		"disable step": func(j map[string]map[string]any) {
			j["test_core"]["steps"].([]any)[1].(map[string]any)["if"] = "false"
		},
		"change action input": func(j map[string]map[string]any) {
			j["test_core"]["steps"].([]any)[1].(map[string]any)["with"].(map[string]any)["ref"] = "HEAD"
		},
		"remove upstream demo gate": func(j map[string]map[string]any) {
			steps := j["storybook"]["steps"].([]any)
			for i, value := range steps {
				if value.(map[string]any)["name"] == "Static demo accounts and interactions" {
					j["storybook"]["steps"] = append(steps[:i], steps[i+1:]...)
					return
				}
			}
		},
		"remove benchmark smoke": func(j map[string]map[string]any) {
			steps := j["test_core"]["steps"].([]any)
			for i, value := range steps {
				if value.(map[string]any)["name"] == "Exercise every benchmark once" {
					j["test_core"]["steps"] = append(steps[:i], steps[i+1:]...)
					return
				}
			}
		},
		"skip benchmark smoke":      func(j map[string]map[string]any) { mutateBenchmarkStep(j, "if", "false") },
		"fail open benchmark smoke": func(j map[string]map[string]any) { mutateBenchmarkStep(j, "continue-on-error", true) },
		"narrow benchmark smoke": func(j map[string]map[string]any) {
			mutateBenchmarkStep(j, "run", "go test -run '^$' -bench . -benchtime=1x ./internal/crypto/")
		},
		"skip upstream demo gate":      func(j map[string]map[string]any) { mutateDemoStep(j, "if", "false") },
		"fail open upstream demo gate": func(j map[string]map[string]any) { mutateDemoStep(j, "continue-on-error", true) },
		"remove demo browser tests":    func(j map[string]map[string]any) { mutateDemoStep(j, "run", "pnpm run build:demo\n") },
	} {
		t.Run(name, func(t *testing.T) {
			var workflow struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(original, &workflow); err != nil {
				t.Fatal(err)
			}
			mutation(workflow.Jobs)
			raw, err := yaml.Marshal(workflow)
			if err != nil {
				t.Fatal(err)
			}
			if err := requireBaselineCoverage(raw, baseline); err == nil {
				t.Fatal("coverage reduction escaped the frozen execution inventory")
			}
		})
	}
}

func replaceShardPlanningRunText(old, replacement string) string {
	if old == replacement || strings.Count(completeShardPlanningRun, old) != 1 {
		panic("planner mutation must replace exactly one matching target")
	}
	return strings.Replace(completeShardPlanningRun, old, replacement, 1)
}

func mutateShardPlanningStep(jobs map[string]map[string]any, key string, value any) {
	var target map[string]any
	for _, item := range jobs["analysis_shards"]["steps"].([]any) {
		step := item.(map[string]any)
		if step["name"] == "Plan complete test, race, and fuzz shards" {
			if target != nil {
				panic("complete shard planning step is duplicated")
			}
			target = step
		}
	}
	if target == nil {
		panic("complete shard planning step not found")
	}
	if reflect.DeepEqual(target[key], value) {
		panic("planner mutation must change its target")
	}
	target[key] = value
}

func mutateDemoStep(jobs map[string]map[string]any, key string, value any) {
	for _, step := range jobs["storybook"]["steps"].([]any) {
		if step.(map[string]any)["name"] == "Static demo accounts and interactions" {
			step.(map[string]any)[key] = value
			return
		}
	}
}

func mutateBenchmarkStep(jobs map[string]map[string]any, key string, value any) {
	for _, step := range jobs["test_core"]["steps"].([]any) {
		if step.(map[string]any)["name"] == "Exercise every benchmark once" {
			step.(map[string]any)[key] = value
			return
		}
	}
}

func mutateFeedbackFixtureStep(jobs map[string]map[string]any, key string, value any) {
	for _, step := range jobs["lint"]["steps"].([]any) {
		if step.(map[string]any)["name"] == "Fast feedback refusal and deadline fixtures" {
			step.(map[string]any)[key] = value
			return
		}
	}
}

func mutateFeedbackNodeStep(jobs map[string]map[string]any, key string, value any) {
	for _, step := range jobs["lint"]["steps"].([]any) {
		if step.(map[string]any)["name"] == "Select pinned Node for feedback fixtures" {
			step.(map[string]any)[key] = value
			return
		}
	}
}

func TestShardPlanningMutationGuards(t *testing.T) {
	step := func() map[string]any {
		return map[string]any{"name": "Plan complete test, race, and fuzz shards", "run": completeShardPlanningRun}
	}
	jobs := func(steps ...any) map[string]map[string]any {
		return map[string]map[string]any{"analysis_shards": {"steps": steps}}
	}
	for name, mutation := range map[string]func(){
		"missing step":    func() { mutateShardPlanningStep(jobs(), "if", "false") },
		"duplicated step": func() { mutateShardPlanningStep(jobs(step(), step()), "if", "false") },
		"unchanged value": func() { mutateShardPlanningStep(jobs(step()), "run", completeShardPlanningRun) },
		"missing text":    func() { replaceShardPlanningRunText("missing planner mutation target", "replacement") },
		"unchanged text":  func() { replaceShardPlanningRunText("race_count=12", "race_count=12") },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("ineffective or ambiguous planner mutation must fail")
				}
			}()
			mutation()
		})
	}
}
