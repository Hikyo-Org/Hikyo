package ci_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const dynamicRenewalFixtureRun = `owns_test=$(printf '%s\n' "$ISOLATION_PLAN" | jq -r --argjson shard "$ISOLATION_SHARD" \
  '.[$shard] | if type != "array" then error("invalid isolation shard") else index("TestDynamicRenewalRealPostgresValidity") != null end')
if [ "$owns_test" = true ]; then
  ./scripts/ci/start-dynamic-pg.sh
fi
`

// Preserve the frozen full-suite inventory. Only these two exact additive
// steps are removed after checking their ordering and fail-closed configuration.
func normalizeDynamicRenewalFixture(job map[string]any) error {
	setup := map[string]any{
		"name": "Start TLS PostgreSQL for the dynamic renewal regression shard",
		"env":  map[string]any{"ISOLATION_PLAN": "${{ needs.analysis_shards.outputs.isolation_plan }}", "ISOLATION_SHARD": "${{ matrix.shard }}"},
		"run":  dynamicRenewalFixtureRun,
	}
	cleanup := map[string]any{
		"name": "Remove the dynamic renewal PostgreSQL target",
		"if":   "${{ always() && env.HIKYO_TEST_DYNAMIC_PG_CONTAINER != '' }}",
		"run":  `docker container rm --force "$HIKYO_TEST_DYNAMIC_PG_CONTAINER"`,
	}
	steps, ok := job["steps"].([]any)
	if !ok {
		return fmt.Errorf("isolation shard has no steps")
	}
	setupAt, testAt, cleanupAt := -1, -1, -1
	kept := make([]any, 0, len(steps))
	for i, v := range steps {
		step, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("malformed isolation shard step")
		}
		switch step["name"] {
		case setup["name"]:
			if setupAt >= 0 || !reflect.DeepEqual(step, setup) {
				return fmt.Errorf("dynamic renewal TLS setup changed")
			}
			setupAt = i
		case cleanup["name"]:
			if cleanupAt >= 0 || !reflect.DeepEqual(step, cleanup) {
				return fmt.Errorf("dynamic renewal TLS cleanup changed")
			}
			cleanupAt = i
		default:
			if step["name"] == "Test assigned isolation cases on SQLite and PostgreSQL" {
				testAt = i
			}
			kept = append(kept, step)
		}
	}
	if setupAt < 0 || testAt <= setupAt || cleanupAt <= testAt {
		return fmt.Errorf("dynamic renewal TLS setup/test/cleanup ordering missing")
	}
	job["steps"] = kept
	return nil
}

func TestDynamicRenewalWorkflowFixtureContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	check := func(raw []byte) error {
		var workflow struct {
			Jobs map[string]map[string]any `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			return err
		}
		return normalizeDynamicRenewalFixture(workflow.Jobs["isolation_shard"])
	}
	if err := check(raw); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct{ old, new string }{
		{"TestDynamicRenewalRealPostgresValidity", "TestUnrelated"},
		{"Remove the dynamic renewal PostgreSQL target", "removed cleanup"},
		{"always() && env.HIKYO_TEST_DYNAMIC_PG_CONTAINER != ''", "success() && env.HIKYO_TEST_DYNAMIC_PG_CONTAINER != ''"},
		{"Start TLS PostgreSQL for the dynamic renewal regression shard", "removed setup"},
	} {
		if err := check([]byte(strings.Replace(string(raw), mutation.old, mutation.new, 1))); err == nil {
			t.Errorf("accepted mutation: %s", mutation.old)
		}
	}
}

func TestDynamicRenewalFixtureShardSelection(t *testing.T) {
	work := t.TempDir()
	script := filepath.Join(work, "scripts", "ci", "start-dynamic-pg.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'started' >\"$FIXTURE_MARKER\"\nexit \"$FIXTURE_EXIT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, plan, exit string
		started, success bool
	}{
		{"owner", `[["TestDynamicRenewalRealPostgresValidity"]]`, "0", true, true},
		{"other", `[["TestDynamicRenewalCeiling"]]`, "0", false, true},
		{"startup failure", `[["TestDynamicRenewalRealPostgresValidity"]]`, "17", true, false},
		{"invalid plan", `{}`, "0", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "started")
			cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", dynamicRenewalFixtureRun)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "ISOLATION_PLAN="+tc.plan, "ISOLATION_SHARD=0", "FIXTURE_MARKER="+marker, "FIXTURE_EXIT="+tc.exit)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.success {
				t.Fatalf("success=%v err=%v output=%s", tc.success, err, out)
			}
			_, statErr := os.Stat(marker)
			if (statErr == nil) != tc.started {
				t.Fatalf("fixture started=%v want=%v", statErr == nil, tc.started)
			}
		})
	}
}
