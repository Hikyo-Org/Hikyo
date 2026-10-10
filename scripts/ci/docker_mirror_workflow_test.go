package ci_test

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Service containers start before steps, so every PostgreSQL fixture must start
// after checkout to allow daemon mirror configuration with canonical fallback.
func TestPostgresFixturesStartAfterCheckout(t *testing.T) {
	for workflow, jobs := range map[string][]string{
		"ci":             {"client", "test_core", "isolation_shard", "race_shard", "headline-guarantee"},
		"race-isolation": {"race-isolation"},
		"gitlab-e2e":     {"gitlab-e2e"},
		"release":        {"build-signed-draft"},
		"nightly":        {"publish"},
	} {
		raw, err := os.ReadFile("../../.github/workflows/" + workflow + ".yml")
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Jobs map[string]struct {
				Services map[string]yaml.Node `yaml:"services"`
				Steps    []struct {
					ID   string            `yaml:"id"`
					Uses string            `yaml:"uses"`
					Run  string            `yaml:"run"`
					If   string            `yaml:"if"`
					Env  map[string]string `yaml:"env"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		for _, name := range jobs {
			job, ok := document.Jobs[name]
			if !ok || len(job.Services) != 0 {
				t.Fatalf("%s/%s: missing job or fixture starts before daemon setup", workflow, name)
			}
			checkout, start, cleanup := -1, -1, -1
			for i, step := range job.Steps {
				if strings.HasPrefix(step.Uses, "actions/checkout@") {
					checkout = i
				}
				if step.ID == "ci-postgres" {
					start = i
					if step.Run != "./scripts/ci/start-ci-postgres.sh" {
						t.Fatalf("%s/%s: fixture bypasses checked startup", workflow, name)
					}
					if workflow == "release" || workflow == "nightly" {
						if step.Env["POSTGRES_DB"] != "hikyo_release_schema" || step.Env["POSTGRES_PASSWORD"] != "release-schema-fixture" || step.Env["POSTGRES_HEALTH_RETRIES"] != "12" {
							t.Fatalf("%s/%s: release fixture settings changed", workflow, name)
						}
					}
				}
				if step.Run == "docker container rm --force hikyo-ci-postgres" {
					cleanup = i
					if !strings.Contains(step.If, "always() && steps.ci-postgres.outcome == 'success'") {
						t.Fatalf("%s/%s: cleanup does not require fixture ownership", workflow, name)
					}
				}
			}
			if checkout < 0 || start != checkout+1 || cleanup != len(job.Steps)-1 {
				t.Fatalf("%s/%s: checkout/start/cleanup order is %d/%d/%d", workflow, name, checkout, start, cleanup)
			}
		}
	}
}
