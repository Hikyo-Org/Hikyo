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
