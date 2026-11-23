package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Keep the frozen coverage hashes. Normalize only the exact resource bounds;
// all other build inputs, step fields and downstream checks remain fingerprinted.
func normalizeSnapshotConcurrency(job map[string]any) error {
	expected := map[string]any{
		"name": "GoReleaser configuration and snapshot",
		"uses": "goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94",
		"env":  map[string]any{"GOMAXPROCS": "2"},
		"with": map[string]any{
			"distribution": "goreleaser", "version": "v2.17.1",
			"args": "release --snapshot --clean --skip=publish --parallelism=2",
		},
	}
	steps, ok := job["steps"].([]any)
	if !ok {
		return fmt.Errorf("snapshot lost its build steps")
	}
	found := false
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("snapshot has malformed steps")
		}
		if step["name"] != expected["name"] {
			continue
		}
		if found || !reflect.DeepEqual(step, expected) {
			return fmt.Errorf("snapshot requires exactly one unchanged build with bounded concurrency")
		}
		found = true
		delete(step, "env")
		step["with"].(map[string]any)["args"] = "release --snapshot --clean --skip=publish"
	}
	if !found {
		return fmt.Errorf("snapshot lost its bounded build")
	}
	return nil
}

// A cold six-target snapshot must bound both simultaneous build graphs and
// compiler workers. Otherwise their temporary archives can fill the runner.
func TestReleaseSnapshotBoundsColdCrossBuilds(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var ci struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				Env  map[string]string `yaml:"env"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &ci); err != nil {
		t.Fatal(err)
	}
	for _, step := range ci.Jobs["release-snapshot"].Steps {
		if !strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
			continue
		}
		args := strings.Fields(step.With["args"])
		for _, required := range []string{"release", "--snapshot", "--clean", "--skip=publish", "--parallelism=2"} {
			if !slices.Contains(args, required) {
				t.Errorf("snapshot is missing %s", required)
			}
		}
		if step.Env["GOMAXPROCS"] != "2" {
			t.Error("snapshot compiler workers must also be bounded independently")
		}
		return
	}
	t.Fatal("missing release snapshot build")
}

func TestSnapshotResourceBoundsPreserveCoverage(t *testing.T) {
	baseline, original := readCoverageBaseline(t)
	for _, mutation := range []string{
		"unchanged", "unbounded targets", "unbounded compiler", "skip builds",
		"extra environment", "conditional", "fail open", "duplicate", "removed",
	} {
		t.Run(mutation, func(t *testing.T) {
			var ci struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(original, &ci); err != nil {
				t.Fatal(err)
			}
			job := ci.Jobs["release-snapshot"]
			steps := job["steps"].([]any)
			for index, value := range steps {
				step := value.(map[string]any)
				if step["name"] != "GoReleaser configuration and snapshot" {
					continue
				}
				switch mutation {
				case "unchanged":
				case "unbounded targets":
					step["with"].(map[string]any)["args"] = "release --snapshot --clean --skip=publish"
				case "unbounded compiler":
					delete(step, "env")
				case "skip builds":
					step["with"].(map[string]any)["args"] = "release --snapshot --clean --skip=publish,build --parallelism=2"
				case "extra environment":
					step["env"].(map[string]any)["GOFLAGS"] = "-tags=narrow"
				case "conditional":
					step["if"] = "false"
				case "fail open":
					step["continue-on-error"] = true
				case "duplicate":
					job["steps"] = append(steps, step)
				case "removed":
					job["steps"] = append(steps[:index], steps[index+1:]...)
				}
				break
			}
			raw, err := yaml.Marshal(ci)
			if err != nil {
				t.Fatal(err)
			}
			err = requireBaselineCoverage(raw, baseline)
			if mutation == "unchanged" {
				if err != nil {
					t.Fatalf("resource bounds changed baseline coverage: %v", err)
				}
			} else if err == nil {
				t.Fatal("snapshot mutation escaped the frozen execution inventory")
			}
		})
	}
}

const (
	snapshotGoCacheRestore = "Restore release-snapshot Go cache"
	snapshotGoCacheSave    = "Save release-snapshot Go cache"
)

// The release snapshot's 2.71 GB Go build cache moved to the lint driver seed.
// Require that the job neither restores, prepares disk for, nor writes any Go
// build cache, then reinsert the frozen restore and save so any other
// release-snapshot change still moves its digest. Run after the shared disk
// normalizer, which already removed the frozen disk step from the digest shape.
func normalizeReleaseSnapshotGoCacheRemoval(job map[string]any) error {
	steps, ok := job["steps"].([]any)
	if !ok {
		return fmt.Errorf("snapshot lost its build steps")
	}
	frozenRestore := map[string]any{
		"name": snapshotGoCacheRestore,
		"id":   "release-snapshot-go-cache",
		"uses": reuseRestoreAction,
		"with": map[string]any{
			"path":         "~/.cache/go-build",
			"key":          "go-release-snapshot-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-${{ hashFiles('go.mod', 'go.sum', '.goreleaser.yaml') }}",
			"restore-keys": "go-release-snapshot-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-\n",
		},
	}
	frozenSave := map[string]any{
		"name": snapshotGoCacheSave,
		"if":   "success() && steps.release-snapshot-go-cache.outputs.cache-hit != 'true' && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'",
		"uses": reuseSaveAction,
		"with": map[string]any{
			"path": "~/.cache/go-build",
			"key":  "${{ steps.release-snapshot-go-cache.outputs.cache-primary-key }}",
		},
	}
	abi, upload := 0, 0
	normalized := make([]any, 0, len(steps)+2)
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("snapshot has malformed steps")
		}
		uses, _ := step["uses"].(string)
		run, _ := step["run"].(string)
		with, _ := step["with"].(map[string]any)
		if step["name"] == snapshotGoCacheRestore || step["name"] == snapshotGoCacheSave || step["name"] == goReuseDiskStep ||
			strings.Contains(run, goReuseDiskScript) || with["path"] == "~/.cache/go-build" ||
			strings.HasPrefix(uses, "actions/cache/save@") || strings.HasPrefix(uses, "actions/cache@") {
			return fmt.Errorf("release-snapshot must not prepare, restore or write a Go build cache")
		}
		normalized = append(normalized, step)
		if step["id"] == "runner-cache-abi" {
			abi++
			normalized = append(normalized, frozenRestore)
		}
		if step["name"] == "Upload unsigned development snapshot" {
			upload++
		}
	}
	if abi != 1 || upload != 1 {
		return fmt.Errorf("release-snapshot lost its runner ABI or snapshot upload anchor")
	}
	if last, _ := steps[len(steps)-1].(map[string]any); last["name"] != "Upload unsigned development snapshot" {
		return fmt.Errorf("release-snapshot must end with its snapshot upload")
	}
	job["steps"] = append(normalized, frozenSave)
	return nil
}

func TestReleaseSnapshotGoCacheRemovalPreservesCoverage(t *testing.T) {
	baseline, original := readCoverageBaseline(t)
	for _, mutation := range []string{"unchanged", "restore", "save", "disk", "combined", "removed abi", "extra step"} {
		t.Run(mutation, func(t *testing.T) {
			var ci struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(original, &ci); err != nil {
				t.Fatal(err)
			}
			job := ci.Jobs["release-snapshot"]
			steps := job["steps"].([]any)
			cache := map[string]any{"path": "~/.cache/go-build", "key": "go-test-v2-x"}
			switch mutation {
			case "unchanged":
			case "restore":
				steps = append(steps, map[string]any{"name": "Restore Go cache", "uses": reuseRestoreAction, "with": cache})
			case "save":
				steps = append(steps, map[string]any{"name": "Save Go cache", "uses": reuseSaveAction, "with": cache})
			case "disk":
				steps = append([]any{map[string]any{"name": goReuseDiskStep, "run": goReuseDiskScript, "timeout-minutes": 3}}, steps...)
			case "combined":
				steps = append(steps, map[string]any{"uses": "actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9", "with": cache})
			case "removed abi":
				for i, value := range steps {
					if value.(map[string]any)["id"] == "runner-cache-abi" {
						steps = append(steps[:i], steps[i+1:]...)
						break
					}
				}
			case "extra step":
				steps = append(steps[:1], append([]any{map[string]any{"run": "true"}}, steps[1:]...)...)
			}
			job["steps"] = steps
			raw, err := yaml.Marshal(ci)
			if err != nil {
				t.Fatal(err)
			}
			err = requireBaselineCoverage(raw, baseline)
			if mutation == "unchanged" {
				if err != nil {
					t.Fatalf("Go cache removal changed baseline coverage: %v", err)
				}
			} else if err == nil {
				t.Fatal("release-snapshot cache mutation escaped the frozen execution inventory")
			}
		})
	}
}
