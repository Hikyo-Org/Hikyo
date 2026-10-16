package ci_test

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type mailCoverageAddition struct {
	job, step, section, field string
	original, approved        string
}

// Require the exact producer/consumer additions before comparing the remaining
// execution configuration against the unchanged historical inventory. Whole
// field equality also refuses duplicated helper lines or arguments; step lookup
// requires exactly one correctly named owner. Other step and job fields remain
// in the frozen digest, including conditions, fail-open settings and checks.
var mailCoverageAdditions = []mailCoverageAddition{
	{
		job: "app-build", step: "Build the release-shaped app", field: "run",
		original: "mkdir -p ci-artifacts\ngo build -tags ui -o ci-artifacts/hikyo-ui ./cmd/hikyo\ngo build -o ci-artifacts/oidctest-idp ./internal/oidctest/cmd\n",
		approved: "mkdir -p ci-artifacts\ngo build -tags ui -o ci-artifacts/hikyo-ui ./cmd/hikyo\ngo build -o ci-artifacts/oidctest-idp ./internal/oidctest/cmd\ngo build -o ci-artifacts/mail-sink ./internal/mailtest/cmd\n",
	},
	{
		job: "app-build", step: "Upload app build for downstream validation", section: "with", field: "path",
		original: "internal/webui/dist/\nci-artifacts/hikyo-ui\nci-artifacts/oidctest-idp\n",
		approved: "internal/webui/dist/\nci-artifacts/hikyo-ui\nci-artifacts/oidctest-idp\nci-artifacts/mail-sink\n",
	},
	{
		job: "web", step: "Prepare prebuilt browser binaries", field: "run",
		original: "chmod +x ci-artifacts/hikyo-ui ci-artifacts/oidctest-idp",
		approved: "chmod +x ci-artifacts/hikyo-ui ci-artifacts/oidctest-idp ci-artifacts/mail-sink",
	},
	{
		job: "web", step: "Flow suite (${{ matrix.project }} group ${{ matrix.group }})", section: "env", field: "HIKYO_E2E_MAIL_BINARY",
		approved: "${{ github.workspace }}/ci-artifacts/mail-sink",
	},
}

func mailCoverageStep(jobs map[string]map[string]any, addition mailCoverageAddition) (map[string]any, error) {
	steps, ok := jobs[addition.job]["steps"].([]any)
	if !ok {
		return nil, fmt.Errorf("%s lost browser mail fixture steps", addition.job)
	}
	var found map[string]any
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s has malformed step configuration", addition.job)
		}
		if step["name"] == addition.step {
			if found != nil {
				return nil, fmt.Errorf("%s duplicates browser mail fixture step %q", addition.job, addition.step)
			}
			found = step
		}
	}
	if found == nil {
		return nil, fmt.Errorf("%s lost browser mail fixture step %q", addition.job, addition.step)
	}
	return found, nil
}

func normalizeMailFixtureAddition(jobs map[string]map[string]any) error {
	for _, addition := range mailCoverageAdditions {
		step, err := mailCoverageStep(jobs, addition)
		if err != nil {
			return err
		}
		fields := step
		if addition.section != "" {
			var ok bool
			fields, ok = step[addition.section].(map[string]any)
			if !ok {
				return fmt.Errorf("%s browser mail fixture step lost %s", addition.job, addition.section)
			}
		}
		if fields[addition.field] != addition.approved {
			return fmt.Errorf("%s browser mail fixture step %q lost its exact %s addition", addition.job, addition.step, addition.field)
		}
		if addition.original == "" {
			delete(fields, addition.field)
		} else {
			fields[addition.field] = addition.original
		}
	}
	return nil
}

func TestMailFixtureCoverageAdditionsAreExact(t *testing.T) {
	baseline, original := readCoverageBaseline(t)
	for _, addition := range mailCoverageAdditions {
		for _, mutation := range []string{"missing addition", "malformed addition", "duplicate addition", "missing owner", "duplicate owner", "skipped owner", "fail-open owner", "changed original check"} {
			t.Run(addition.step+"/"+mutation, func(t *testing.T) {
				var workflow struct {
					Jobs map[string]map[string]any `yaml:"jobs"`
				}
				if err := yaml.Unmarshal(original, &workflow); err != nil {
					t.Fatal(err)
				}
				step, err := mailCoverageStep(workflow.Jobs, addition)
				if err != nil {
					t.Fatal(err)
				}
				fields := step
				if addition.section != "" {
					var ok bool
					fields, ok = step[addition.section].(map[string]any)
					if !ok {
						t.Fatal("fixture lost its expected field owner")
					}
				}
				switch mutation {
				case "missing addition":
					if addition.original == "" {
						delete(fields, addition.field)
					} else {
						fields[addition.field] = addition.original
					}
				case "malformed addition":
					fields[addition.field] = strings.ReplaceAll(addition.approved, "ci-artifacts/mail-sink", "ci-artifacts/wrong-mail-sink")
				case "duplicate addition":
					fields[addition.field] = addition.approved + addition.approved
				case "missing owner":
					step["name"] = "Renamed browser mail fixture owner"
				case "duplicate owner":
					steps := workflow.Jobs[addition.job]["steps"].([]any)
					workflow.Jobs[addition.job]["steps"] = append(steps, step)
				case "skipped owner":
					step["if"] = "false"
				case "fail-open owner":
					step["continue-on-error"] = true
				case "changed original check":
					if addition.section == "env" {
						fields["HIKYO_E2E_BINARY"] = "ci-artifacts/wrong-app"
					} else {
						fields[addition.field] = strings.ReplaceAll(addition.approved, "ci-artifacts/hikyo-ui", "ci-artifacts/wrong-app")
					}
				}
				raw, err := yaml.Marshal(workflow)
				if err != nil {
					t.Fatal(err)
				}
				if err := requireBaselineCoverage(raw, baseline); err == nil {
					t.Fatal("mail fixture change escaped the complete execution inventory")
				}
			})
		}
	}
}
