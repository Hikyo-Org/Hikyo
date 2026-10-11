package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type consolidatedStep struct {
	Name string            `yaml:"name"`
	Run  string            `yaml:"run"`
	Uses string            `yaml:"uses"`
	Env  map[string]string `yaml:"env"`
	With map[string]string `yaml:"with"`
}

type consolidatedJob struct {
	If          string             `yaml:"if"`
	Needs       yaml.Node          `yaml:"needs"`
	RunsOn      string             `yaml:"runs-on"`
	Permissions map[string]string  `yaml:"permissions"`
	Outputs     map[string]string  `yaml:"outputs"`
	Steps       []consolidatedStep `yaml:"steps"`
}

type consolidatedWorkflow struct {
	On          map[string]yaml.Node       `yaml:"on"`
	Permissions map[string]string          `yaml:"permissions"`
	Jobs        map[string]consolidatedJob `yaml:"jobs"`
}

func readConsolidatedWorkflow(t *testing.T, name string) consolidatedWorkflow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name+".yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow consolidatedWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func TestConsolidatedWorkflowRouting(t *testing.T) {
	for _, name := range []string{"reliability", "release-acceptance"} {
		workflow := readConsolidatedWorkflow(t, name)
		var dispatch struct {
			Inputs map[string]struct {
				Type     string
				Required bool
				Default  string
				Options  []string
			}
		}
		dispatchNode := workflow.On["workflow_dispatch"]
		if err := dispatchNode.Decode(&dispatch); err != nil {
			t.Fatal(err)
		}
		input := dispatch.Inputs["suite"]
		suites := []string{"all", "security", "gitlab", "reporting", "race", "channel"}
		families := []string{"security", "gitlab", "reporting", "race", "channel"}
		if name == "release-acceptance" {
			suites = []string{"all", "backup-restore", "operator"}
			families = []string{"backup", "operator"}
		}
		if len(dispatch.Inputs) != 1 || input.Type != "choice" || !input.Required || input.Default != "all" || !reflect.DeepEqual(input.Options, suites) {
			t.Fatalf("%s must expose one closed suite menu with explicit all default", name)
		}
		route := workflow.Jobs["route"]
		if len(route.Steps) != 1 || len(route.Outputs) != len(families) {
			t.Fatalf("%s route must emit every family", name)
		}
		if route.Steps[0].Env["EVENT_NAME"] != "${{ github.event_name }}" || route.Steps[0].Env["REQUESTED_SUITE"] != "${{ inputs.suite }}" || (name == "reliability" && route.Steps[0].Env["EVENT_SCHEDULE"] != "${{ github.event.schedule }}") {
			t.Fatalf("%s routing must bind the authenticated event and selected menu", name)
		}
		for _, family := range families {
			if route.Outputs[family] != "${{ steps.select.outputs."+family+" }}" {
				t.Fatalf("%s has unwired %s output", name, family)
			}
		}
		type scenario struct {
			event, cron, suite string
			selected           []string
			invalid            bool
		}
		var cases []scenario
		for _, suite := range suites {
			selected := []string{suite}
			if suite == "all" {
				selected = families
			}
			if suite == "backup-restore" {
				selected = []string{"backup"}
			}
			cases = append(cases, scenario{event: "workflow_dispatch", suite: suite, selected: selected})
		}
		cases = append(cases, scenario{event: "workflow_dispatch", suite: "", invalid: true}, scenario{event: "workflow_dispatch", suite: "invalid", invalid: true}, scenario{event: "pull_request", suite: "all", invalid: true}, scenario{event: "pull_request_target", suite: "all", invalid: true}, scenario{event: "schedule", cron: "unknown", suite: "all", invalid: true})
		if name == "reliability" {
			crons := []string{"23 7 * * *", "0 4 * * 1", "0 5 * * 1", "17 6 * * 1"}
			var schedule []struct{ Cron string }
			scheduleNode := workflow.On["schedule"]
			if err := scheduleNode.Decode(&schedule); err != nil {
				t.Fatal(err)
			}
			if len(workflow.On) != 2 || len(schedule) != len(crons) {
				t.Fatal("reliability cadence must remain closed")
			}
			for i, cron := range crons {
				if schedule[i].Cron != cron {
					t.Fatal("reliability schedule changed")
				}
			}
			cases = append(cases, scenario{event: "schedule", cron: crons[0], suite: "all", selected: []string{"security"}}, scenario{event: "schedule", cron: crons[1], suite: "all", selected: []string{"race"}}, scenario{event: "schedule", cron: crons[2], suite: "all", selected: []string{"gitlab", "reporting"}}, scenario{event: "schedule", cron: crons[3], suite: "all", selected: []string{"channel"}})
		} else if len(workflow.On) != 1 {
			t.Fatal("custody acceptance must remain manual-only")
		}
		for _, test := range cases {
			t.Run(name+"/"+test.event+"/"+test.cron+"/"+test.suite, func(t *testing.T) {
				outputPath := filepath.Join(t.TempDir(), "output")
				cmd := exec.Command("bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", route.Steps[0].Run)
				cmd.Env = append(os.Environ(), "EVENT_NAME="+test.event, "EVENT_SCHEDULE="+test.cron, "REQUESTED_SUITE="+test.suite, "GITHUB_OUTPUT="+outputPath)
				output, err := cmd.CombinedOutput()
				if (err != nil) != test.invalid {
					t.Fatalf("route error=%v, invalid=%v: %s", err, test.invalid, output)
				}
				data, readErr := os.ReadFile(outputPath)
				if test.invalid {
					if !os.IsNotExist(readErr) {
						t.Fatal("invalid selection emitted outputs")
					}
					return
				}
				if readErr != nil {
					t.Fatal(readErr)
				}
				var expected strings.Builder
				for _, family := range families {
					value := "false"
					for _, selected := range test.selected {
						if selected == family {
							value = "true"
						}
					}
					expected.WriteString(family + "=" + value + "\n")
				}
				if string(data) != expected.String() {
					t.Fatalf("selected suites = %q, want %q", data, expected.String())
				}
			})
		}
	}
}

func TestConsolidatedWorkflowTrustAndCoverage(t *testing.T) {
	for _, name := range []string{"reliability", "release-acceptance"} {
		workflow := readConsolidatedWorkflow(t, name)
		if !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) {
			t.Fatalf("%s default must be read-only", name)
		}
		families := map[string]string{"go-patch": "security", "binary-security": "security", "gitlab-e2e": "gitlab", "reporting-e2e": "reporting", "plan": "race", "fallback-channel": "channel"}
		if name == "release-acceptance" {
			families = map[string]string{"floor-backup-restore": "backup", "operator-floor": "operator"}
		}
		expectedJobs := []string{"route"}
		for jobName, family := range families {
			expectedJobs = append(expectedJobs, jobName)
			job, ok := workflow.Jobs[jobName]
			needs, err := workflowNeeds(job.Needs)
			if !ok || err != nil || !reflect.DeepEqual(needs, []string{"route"}) || job.If != "needs.route.outputs."+family+" == 'true'" {
				t.Fatalf("%s/%s bypasses its suite selection", name, jobName)
			}
		}
		if name == "reliability" {
			expectedJobs = append(expectedJobs, "race-isolation")
			needs, err := workflowNeeds(workflow.Jobs["race-isolation"].Needs)
			if err != nil || !reflect.DeepEqual(needs, []string{"plan"}) || workflow.Jobs["race-isolation"].If != "" {
				t.Fatal("race shards must depend on successful selected planner")
			}
		}
		var actualJobs []string
		for jobName, job := range workflow.Jobs {
			actualJobs = append(actualJobs, jobName)
			if jobName == "fallback-channel" {
				if !reflect.DeepEqual(job.Permissions, map[string]string{"contents": "read", "issues": "write"}) {
					t.Fatal("channel reporter requires narrowly scoped issue permission")
				}
			} else if len(job.Permissions) != 0 {
				t.Fatalf("%s/%s must inherit read-only authority", name, jobName)
			}
			wantRunner := "ubuntu-latest"
			if name == "release-acceptance" && jobName != "route" {
				wantRunner = "ubuntu-24.04-arm"
			}
			if job.RunsOn != wantRunner {
				t.Fatalf("%s/%s runner changed", name, jobName)
			}
			for _, step := range job.Steps {
				if strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["persist-credentials"] != "false" {
					t.Fatal("checkout retains credentials")
				}
				for key, value := range step.Env {
					if strings.Contains(value, "secrets.") && (name != "release-acceptance" || jobName != "floor-backup-restore" || (key != "FLOOR_BACKUP_CUSTODY" && key != "FLOOR_ROOT_CUSTODY")) {
						t.Fatalf("custody leaked to %s/%s", name, jobName)
					}
				}
			}
		}
		sort.Strings(actualJobs)
		sort.Strings(expectedJobs)
		if !reflect.DeepEqual(actualJobs, expectedJobs) {
			t.Fatalf("%s suites changed: %v", name, actualJobs)
		}
	}
	for _, old := range []string{"go-security", "gitlab-e2e", "reporting-e2e", "race-isolation", "security-channel", "floor-acceptance", "operator-floor"} {
		if _, err := os.Stat(filepath.Join(repositoryRoot(t), ".github", "workflows", old+".yml")); !os.IsNotExist(err) {
			t.Fatalf("duplicate entrypoint %s remains", old)
		}
	}
}
