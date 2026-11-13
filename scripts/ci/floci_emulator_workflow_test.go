package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	isolationTestStep = "Test assigned isolation cases on SQLite and PostgreSQL"
	motoOnlyComment   = `          # The shard that owns the AWS Secrets Manager emulator leg starts the
          # pinned moto harness; the test then refuses to skip (#158).
`
	flociComment = `          # The shard that owns the AWS Secrets Manager lifecycle leg starts the
          # pinned moto harness; the shard that owns the acceptance leg starts
          # the pinned Floci harness. Each test refuses to skip (#158).
`
	flociStart = `          case "|$tests|" in
            *'|TestAWSSecretsManagerEmulatorAcceptance|'*)
              floci_env=$(./scripts/ci/start-floci-emulator.sh "$RUNNER_TEMP/floci-emulator")
              for assignment in $floci_env; do
                export "${assignment?}"
              done
              ;;
          esac
`
)

// The Floci acceptance leg (#158) only adds an emulator start to the shard that
// owns its test; the frozen full-suite inventory is otherwise unchanged. The
// workflow embeds the run script indented, so compare on the dedented text.
func normalizeFlociEmulatorAddition(job map[string]any) error {
	steps, ok := job["steps"].([]any)
	if !ok {
		return fmt.Errorf("isolation shard has no steps")
	}
	for _, v := range steps {
		step, ok := v.(map[string]any)
		if !ok || step["name"] != isolationTestStep {
			continue
		}
		run, _ := step["run"].(string)
		comment, start := dedent(flociComment), dedent(flociStart)
		if strings.Count(run, comment) != 1 || strings.Count(run, start) != 1 || strings.Index(run, comment) > strings.Index(run, start) {
			return fmt.Errorf("isolation shard lost its exact Floci emulator addition")
		}
		run = strings.Replace(run, start, "", 1)
		step["run"] = strings.Replace(run, comment, dedent(motoOnlyComment), 1)
		return nil
	}
	return fmt.Errorf("isolation shard has no test step")
}

func dedent(block string) string {
	lines := strings.SplitAfter(block, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "          ")
	}
	return strings.Join(lines, "")
}

func TestFlociEmulatorAdditionIsExact(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"unchanged":     func(s string) string { return s },
		"missing start": func(s string) string { return strings.Replace(s, flociStart, "", 1) },
		"fail-open start": func(s string) string {
			return strings.Replace(s, `start-floci-emulator.sh "$RUNNER_TEMP`, `start-floci-emulator.sh "$RUNNER_TEMP" || true #`, 1)
		},
		"duplicated start": func(s string) string { return strings.Replace(s, flociStart, flociStart+flociStart, 1) },
		"start before note": func(s string) string {
			return strings.Replace(strings.Replace(s, flociStart, "", 1), flociComment, flociStart+flociComment, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var workflow struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			if err := yaml.Unmarshal([]byte(mutate(string(raw))), &workflow); err != nil {
				t.Fatal(err)
			}
			err := normalizeFlociEmulatorAddition(workflow.Jobs["isolation_shard"])
			if (err == nil) != (name == "unchanged") {
				t.Fatalf("normalize = %v", err)
			}
		})
	}
}
