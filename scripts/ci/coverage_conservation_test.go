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
	if err := normalizeSharedRaceReuse(workflow.Jobs["race_shard"]); err != nil {
		return err
	}
	if err := normalizeGoReuseDiskPreparation(workflow.Jobs); err != nil {
		return err
	}
	// The feedback fixtures execute the real documentation checker. Pin its
	// owner runtime before the fixtures, and normalize only this exact addition.
	expectedNode := map[string]any{
		"name": "Select pinned Node for feedback fixtures",
		"uses": "actions/setup-node@2028fbc5c25fe9cf00d9f06a71cc4710d4507903",
		"with": map[string]any{"node-version-file": ".nvmrc"},
	}
	// #1046 adds mandatory trusted-release fixtures. Require their complete
	// unconditional, fail-closed setup and execution before omitting this exact
	// addition from the unchanged historical execution inventory.
	expectedRelease := map[string]any{
		"name": "Trusted release admission contract fixtures",
		"run": "python3 -m venv \"$RUNNER_TEMP/release-admission-checks\"\n" +
			"\"$RUNNER_TEMP/release-admission-checks/bin/pip\" install -r scripts/ci/release-admission/requirements.txt\n" +
			"\"$RUNNER_TEMP/release-admission-checks/bin/python\" -B scripts/ci/release-admission/run_checks.py --unit\n",
	}
	lintSteps, ok := workflow.Jobs["lint"]["steps"].([]any)
	if !ok {
		return fmt.Errorf("lint lost feedback fixture tooling")
	}
	keptLint := make([]any, 0, len(lintSteps))
	pinnedNode := false
	trustedRelease := false
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
		} else if step["name"] == expectedRelease["name"] {
			if trustedRelease || !reflect.DeepEqual(step, expectedRelease) {
				return fmt.Errorf("trusted release fixtures require exactly one unconditional fail-closed setup and execution step")
			}
			trustedRelease = true
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
	if !trustedRelease {
		return fmt.Errorf("lint lost mandatory trusted release admission contract fixtures")
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
		// #1037 adds the external receiver rehearsal. Require the exact
		// unconditional command without replacing the historical job digest.
		"supply-chain-checks": {
			"name": "Independent audit receiver rehearsal",
			"run":  "python3 -I -B -m unittest discover -s scripts/ops/audit-collector -p 'test_*.py'",
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
	// Accept only the reviewed artifact runtime upgrades, then restore their
	// frozen pins for hashing. Every other step setting remains fingerprinted.
	artifactPins := map[string][2]string{
		"actions/upload-artifact@":   {"cf430e030ddbb5b0abf93d22962f4752f3646cd9", "ea165f8d65b6e75b540449e92b4886f43607fa02"},
		"actions/download-artifact@": {"9000827ccba6bdab643e8b6fd33ac0654aef8333", "3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c"},
	}
	for name, job := range workflow.Jobs {
		steps, _ := job["steps"].([]any)
		for _, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("%s has malformed step configuration", name)
			}
			uses, _ := step["uses"].(string)
			for prefix, pins := range artifactPins {
				if strings.HasPrefix(uses, prefix) {
					if uses != prefix+pins[0] {
						return fmt.Errorf("%s requires the reviewed artifact action pin: %s", name, uses)
					}
					step["uses"] = prefix + pins[1]
				}
			}
		}
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

// The race build cache moved from one restore/save pair per shard to a single
// shared cache that only ci-race-cache.yml writes. Require exactly the shared
// restore step and no cache writer in race_shard, then rewrite the job to the
// frozen per-shard shape so any other race_shard change still moves its digest.
func normalizeSharedRaceReuse(job map[string]any) error {
	steps, ok := job["steps"].([]any)
	if !ok {
		return fmt.Errorf("race_shard lost its steps")
	}
	sharedRestore := map[string]any{
		"name": "Restore race Go cache",
		"uses": reuseRestoreAction,
		"with": map[string]any{
			"path":         "~/.cache/go-build",
			"key":          sharedRaceReuseKey,
			"restore-keys": sharedRaceReuseRestoreKeys,
		},
	}
	frozenRestore := map[string]any{
		"name": "Restore race Go cache",
		"id":   "race-go-cache",
		"uses": reuseRestoreAction,
		"with": map[string]any{
			"path":         "~/.cache/go-build",
			"key":          "go-race-v2-shard-${{ matrix.shard }}-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-${{ hashFiles('go.mod', 'go.sum') }}-${{ hashFiles('scripts/ci/analysis-shards-go/main.go') }}",
			"restore-keys": "go-race-v2-shard-${{ matrix.shard }}-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-",
		},
	}
	frozenSave := map[string]any{
		"name": "Save race Go cache (trusted main validation only)",
		"if":   "success() && steps.race-go-cache.outputs.cache-hit != 'true' && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'",
		"uses": reuseSaveAction,
		"with": map[string]any{
			"path": "~/.cache/go-build",
			"key":  "${{ steps.race-go-cache.outputs.cache-primary-key }}",
		},
	}
	restored, anchored := 0, 0
	normalized := make([]any, 0, len(steps)+1)
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("race_shard has malformed step configuration")
		}
		if uses, _ := step["uses"].(string); strings.HasPrefix(uses, "actions/cache/save@") || strings.HasPrefix(uses, "actions/cache@") {
			return fmt.Errorf("race_shard must not write the race cache; ci-race-cache.yml is its only writer")
		}
		if step["name"] == "Restore race Go cache" {
			if !reflect.DeepEqual(step, sharedRestore) {
				return fmt.Errorf("race_shard must restore exactly the shared race cache")
			}
			restored++
			normalized = append(normalized, frozenRestore)
			continue
		}
		normalized = append(normalized, step)
		if step["name"] == "Upload race target timings" {
			anchored++
			normalized = append(normalized, frozenSave)
		}
	}
	if restored != 1 || anchored != 1 {
		return fmt.Errorf("race_shard requires exactly one shared cache restore after one timings upload, got %d and %d", restored, anchored)
	}
	job["steps"] = normalized
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

func TestTrustedReleaseFixtureCoverageAdditionIsExact(t *testing.T) {
	baseline, original := readCoverageBaseline(t)
	for _, mutation := range []string{
		"unmutated", "removed", "duplicated", "renamed", "skipped", "conditional",
		"fail-open step", "fail-open command", "missing setup", "missing pinned install",
		"unpinned install", "runner Python", "missing unit mode", "narrowed checks",
		"duplicated command", "setup after install", "changed environment", "changed directory",
		"skipped job", "fail-open job", "removed baseline check",
	} {
		t.Run(mutation, func(t *testing.T) {
			var workflow struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(original, &workflow); err != nil {
				t.Fatal(err)
			}
			job := workflow.Jobs["lint"]
			steps := job["steps"].([]any)
			index := -1
			for i, value := range steps {
				if value.(map[string]any)["name"] == "Trusted release admission contract fixtures" {
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
			lines := strings.Split(strings.TrimSuffix(run, "\n"), "\n")
			if len(lines) != 3 {
				t.Fatal("expected setup, pinned install, and unit execution")
			}
			switch mutation {
			case "unmutated":
			case "removed":
				job["steps"] = append(steps[:index], steps[index+1:]...)
			case "duplicated":
				job["steps"] = append(steps, step)
			case "renamed":
				step["name"] = "Renamed release fixtures"
			case "skipped":
				step["if"] = "false"
			case "conditional":
				step["if"] = "github.event_name == 'pull_request'"
			case "fail-open step":
				step["continue-on-error"] = true
			case "fail-open command":
				step["run"] = strings.TrimSuffix(run, "\n") + " || true\n"
			case "missing setup":
				step["run"] = strings.Join(lines[1:], "\n") + "\n"
			case "missing pinned install":
				step["run"] = lines[0] + "\n" + lines[2] + "\n"
			case "unpinned install":
				step["run"] = strings.Replace(run, "-r scripts/ci/release-admission/requirements.txt", "PyYAML jsonschema cryptography", 1)
			case "runner Python":
				step["run"] = strings.Replace(run, `"$RUNNER_TEMP/release-admission-checks/bin/python"`, "python3", 1)
			case "missing unit mode":
				step["run"] = strings.Replace(run, " --unit", "", 1)
			case "narrowed checks":
				step["run"] = strings.Replace(run, "run_checks.py --unit", "test_trusted_source.py", 1)
			case "duplicated command":
				step["run"] = run + lines[2] + "\n"
			case "setup after install":
				step["run"] = lines[1] + "\n" + lines[0] + "\n" + lines[2] + "\n"
			case "changed environment":
				step["env"] = map[string]any{"PYTHONPATH": "untrusted"}
			case "changed directory":
				step["working-directory"] = "scripts/ci/release-admission"
			case "skipped job":
				job["if"] = "false"
			case "fail-open job":
				job["continue-on-error"] = true
			case "removed baseline check":
				for i, value := range steps {
					if value.(map[string]any)["name"] == "Actionlint" {
						job["steps"] = append(steps[:i], steps[i+1:]...)
						break
					}
				}
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
				t.Fatal("release fixture mutation escaped the frozen execution inventory")
			}
		})
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
		"race cache restore reads a per-shard key": func(j map[string]map[string]any) {
			mutateRaceReuseRestore(j, "key", "go-race-v2-shard-${{ matrix.shard }}-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-${{ hashFiles('go.mod', 'go.sum') }}")
		},
		"race cache restore ignores go.sum": func(j map[string]map[string]any) {
			mutateRaceReuseRestore(j, "key", strings.Replace(sharedRaceReuseKey, "'go.mod', 'go.sum'", "'go.mod'", 1))
		},
		"race cache restore prefix widens": func(j map[string]map[string]any) {
			mutateRaceReuseRestore(j, "restore-keys", "go-race-v2-")
		},
		"race cache restore reads another path": func(j map[string]map[string]any) {
			mutateRaceReuseRestore(j, "path", "~/go/pkg/mod")
		},
		"race cache restore is removed": func(j map[string]map[string]any) {
			steps := j["race_shard"]["steps"].([]any)
			for i, value := range steps {
				if value.(map[string]any)["name"] == "Restore race Go cache" {
					j["race_shard"]["steps"] = append(steps[:i], steps[i+1:]...)
					return
				}
			}
		},
		"race cache restore is skipped": func(j map[string]map[string]any) {
			for _, value := range j["race_shard"]["steps"].([]any) {
				if step := value.(map[string]any); step["name"] == "Restore race Go cache" {
					step["if"] = "false"
				}
			}
		},
		"race shard re-adds a cache save": func(j map[string]map[string]any) {
			steps := j["race_shard"]["steps"].([]any)
			j["race_shard"]["steps"] = append(steps, map[string]any{
				"name": "Save race Go cache (trusted main validation only)",
				"uses": reuseSaveAction,
				"with": map[string]any{"path": "~/.cache/go-build", "key": sharedRaceReuseKey},
			})
		},
		"race shard writes the cache with the combined action": func(j map[string]map[string]any) {
			steps := j["race_shard"]["steps"].([]any)
			j["race_shard"]["steps"] = append(steps, map[string]any{
				"uses": "actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9",
				"with": map[string]any{"path": "~/.cache/go-build", "key": sharedRaceReuseKey},
			})
		},
		"remove race detector": func(j map[string]map[string]any) {
			steps := j["race_shard"]["steps"].([]any)
			for i, value := range steps {
				if value.(map[string]any)["name"] == "Race detector (all packages except the isolation suite)" {
					j["race_shard"]["steps"] = append(steps[:i], steps[i+1:]...)
					return
				}
			}
		},
		"skip race detector": func(j map[string]map[string]any) {
			for _, value := range j["race_shard"]["steps"].([]any) {
				if step := value.(map[string]any); step["name"] == "Race detector (all packages except the isolation suite)" {
					step["if"] = "false"
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

func TestCoverageInventoryRejectsArtifactTransferChanges(t *testing.T) {
	baseline, original := readCoverageBaseline(t)
	upload := "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9"
	download := "actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333"
	for _, fixture := range []struct {
		name, before, after string
	}{
		{"mutable upload", upload, "actions/upload-artifact@v7"},
		{"mutable download", download, "actions/download-artifact@v8"},
		{"old upload runtime", upload, "actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02"},
		{"old download runtime", download, "actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c"},
		{"fail-open upload", upload, upload + "\n        continue-on-error: true"},
		{"missing files ignored", "if-no-files-found: error", "if-no-files-found: ignore"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if !strings.Contains(string(original), fixture.before) {
				t.Fatal("artifact transfer mutation target missing")
			}
			raw := []byte(strings.Replace(string(original), fixture.before, fixture.after, 1))
			if err := requireBaselineCoverage(raw, baseline); err == nil {
				t.Fatal("artifact transfer change escaped the frozen execution inventory")
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

func mutateRaceReuseRestore(jobs map[string]map[string]any, key string, value any) {
	for _, step := range jobs["race_shard"]["steps"].([]any) {
		if s := step.(map[string]any); s["name"] == "Restore race Go cache" {
			s["with"].(map[string]any)[key] = value
			return
		}
	}
	panic("race cache restore step not found")
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
