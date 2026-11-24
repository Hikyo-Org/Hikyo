package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The PostgreSQL 17 refusal fixture (#1552) is one exact additive step, directly
// after the checked datastore start, in each job that runs the packages whose
// tests read HIKYO_TEST_POSTGRES_OLD_DSN and fail under CI without it.
var oldPostgresFixtureJobs = []string{"test_core", "race_shard"}

func normalizeOldPostgresFixture(jobs map[string]map[string]any) error {
	want := map[string]any{
		"name": "Start PostgreSQL 17 to prove the PostgreSQL 18 minimum is enforced",
		"run":  "./scripts/ci/start-old-postgres.sh",
	}
	for _, name := range oldPostgresFixtureJobs {
		steps, ok := jobs[name]["steps"].([]any)
		if !ok {
			return fmt.Errorf("%s has no steps", name)
		}
		kept := make([]any, 0, len(steps))
		found, afterDatastore := 0, false
		for i, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("%s has a malformed step", name)
			}
			if step["name"] != want["name"] {
				kept = append(kept, step)
				continue
			}
			if !reflect.DeepEqual(step, want) {
				return fmt.Errorf("%s PostgreSQL 17 refusal fixture changed", name)
			}
			if i > 0 {
				previous, _ := steps[i-1].(map[string]any)
				afterDatastore = previous["id"] == "ci-postgres"
			}
			found++
		}
		if found != 1 || !afterDatastore {
			return fmt.Errorf("%s must start the PostgreSQL 17 refusal fixture exactly once, right after the datastore", name)
		}
		jobs[name]["steps"] = kept
	}
	return nil
}

func TestOldPostgresFixtureContract(t *testing.T) {
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
		return normalizeOldPostgresFixture(workflow.Jobs)
	}
	if err := check(raw); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct{ old, new string }{
		{"Start PostgreSQL 17 to prove the PostgreSQL 18 minimum is enforced", "removed fixture"},
		{"run: ./scripts/ci/start-old-postgres.sh", "run: ./scripts/ci/start-old-postgres.sh || true"},
	} {
		if err := check([]byte(strings.Replace(string(raw), mutation.old, mutation.new, 1))); err == nil {
			t.Errorf("accepted mutation: %s", mutation.old)
		}
	}
}
