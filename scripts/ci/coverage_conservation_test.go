package ci_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	actual, err := coverageDigests(raw, true)
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
	for name, mutation := range map[string]func(map[string]map[string]any){
		"skip job":      func(j map[string]map[string]any) { j["test_core"]["if"] = "false" },
		"job fail open": func(j map[string]map[string]any) { j["test_core"]["continue-on-error"] = true },
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
