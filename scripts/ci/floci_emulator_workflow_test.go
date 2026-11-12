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
	flociRequiredEnv  = "HIKYO_TEST_FLOCI_REQUIRED"
	flociRequiredLine = `          HIKYO_TEST_FLOCI_REQUIRED: "1"
`
	isolationGoTest = `          go test -count=1 -timeout=18m ./internal/isolation/ -run "^(${tests})$"
`
	motoOnlyComment = `          # The shard that owns the AWS Secrets Manager emulator leg starts the
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
// owns its test, and exports HIKYO_TEST_FLOCI_REQUIRED=1 for every shard so a
// shard that runs the test without Floci fails instead of skipping; the frozen
// full-suite inventory is otherwise unchanged. The
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
		goTest := strings.Index(run, "\n"+dedent(isolationGoTest))
		if strings.Count(run, comment) != 1 || strings.Count(run, start) != 1 || strings.Index(run, comment) > strings.Index(run, start) {
			return fmt.Errorf("isolation shard lost its exact Floci emulator addition")
		}
		if goTest < 0 || strings.Index(run, start) > goTest {
			return fmt.Errorf("isolation shard must start Floci before the go test line")
		}
		env, _ := step["env"].(map[string]any)
		if env[flociRequiredEnv] != "1" {
			return fmt.Errorf("isolation shard must export %s=1 unconditionally", flociRequiredEnv)
		}
		delete(env, flociRequiredEnv)
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
		"env missing": func(s string) string { return strings.Replace(s, flociRequiredLine, "", 1) },
		"env not required": func(s string) string {
			return strings.Replace(s, flociRequiredLine, strings.Replace(flociRequiredLine, `"1"`, `"0"`, 1), 1)
		},
		"start after go test": func(s string) string {
			return strings.Replace(strings.Replace(s, flociStart, "", 1), isolationGoTest, isolationGoTest+flociStart, 1)
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

// #158 also shellchecks the owner's sandbox script: lint gains exactly that one
// argument after its last frozen path, and its inventory is otherwise unchanged.
const (
	sandboxShellcheckFrozen = "install/upgrade-nightly*.sh\n"
	sandboxShellcheckAdded  = "install/upgrade-nightly*.sh \\\n  docs/testing/aws-sandbox/*.sh\n"
)

func normalizeAWSSandboxShellcheck(job map[string]any) error {
	steps, _ := job["steps"].([]any)
	for _, value := range steps {
		step, _ := value.(map[string]any)
		if step == nil || step["name"] != "Shellcheck (pinned archive)" {
			continue
		}
		run, _ := step["run"].(string)
		if strings.Count(run, sandboxShellcheckAdded) != 1 || strings.Count(run, "docs/testing/aws-sandbox/") != 1 {
			return fmt.Errorf("lint ShellCheck lost its exact AWS sandbox script argument")
		}
		step["run"] = strings.Replace(run, sandboxShellcheckAdded, sandboxShellcheckFrozen, 1)
		return nil
	}
	return fmt.Errorf("lint has no pinned ShellCheck step")
}

func TestAWSSandboxShellcheckAdditionIsExact(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	const added = " \\\n            docs/testing/aws-sandbox/*.sh\n"
	for name, mutate := range map[string]func(string) string{
		"unchanged": func(s string) string { return s },
		"missing":   func(s string) string { return strings.Replace(s, added, "\n", 1) },
		"duplicated": func(s string) string {
			return strings.Replace(s, added, " \\\n            docs/testing/aws-sandbox/*.sh docs/testing/aws-sandbox/*.sh\n", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if name != "unchanged" && mutate(string(raw)) == string(raw) {
				t.Fatal("mutation did not apply")
			}
			var workflow struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			if err := yaml.Unmarshal([]byte(mutate(string(raw))), &workflow); err != nil {
				t.Fatal(err)
			}
			err := normalizeAWSSandboxShellcheck(workflow.Jobs["lint"])
			if (err == nil) != (name == "unchanged") {
				t.Fatalf("normalize = %v", err)
			}
		})
	}
}
