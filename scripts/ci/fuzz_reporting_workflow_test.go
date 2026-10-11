package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMainFuzzReportingWorkflowPolicy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "fuzz-report.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var reporter mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &reporter); err != nil {
		t.Fatal(err)
	}
	trigger, ok := reporter.On["workflow_run"]
	if !ok || len(reporter.On) != 1 {
		t.Fatal("fuzz reporting must remain a trusted completion callback")
	}
	var completed struct {
		Workflows []string `yaml:"workflows"`
		Types     []string `yaml:"types"`
	}
	if err := trigger.Decode(&completed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(completed.Workflows, []string{"trusted-ci", "ci", "ci-main", "nightly-release"}) || !reflect.DeepEqual(completed.Types, []string{"completed"}) {
		t.Fatal("reporter must receive completed manual, nightly, and existing PR validation")
	}
	job := reporter.Jobs["report-main"]
	if !reflect.DeepEqual(job.Permissions, map[string]string{"actions": "read", "contents": "read", "issues": "write"}) || len(job.Steps) != 2 {
		t.Fatal("main reporting must remain one trusted checkout and issue-only reporter")
	}
	checkout, report := job.Steps[0], job.Steps[1]
	if !strings.HasPrefix(checkout.Uses, "actions/checkout@") || checkout.With["ref"] != "${{ github.event.repository.default_branch }}" || checkout.With["persist-credentials"] != "false" {
		t.Fatal("fuzz reporter must execute tooling only from the trusted default branch")
	}
	if strings.Join(strings.Fields(report.Run), " ") != `./scripts/ci/report-fuzz-finding.sh main "$HEAD_SHA" "$RUN_URL" "$RUN_ID" "$RUN_ATTEMPT"` {
		t.Fatal("fuzz artifacts must remain data passed to the existing trusted reporter")
	}
	for key, expected := range map[string]string{
		"HEAD_SHA":    "${{ github.event.workflow_run.head_sha }}",
		"RUN_ID":      "${{ github.event.workflow_run.id }}",
		"RUN_ATTEMPT": "${{ github.event.workflow_run.run_attempt }}",
	} {
		if report.Env[key] != expected {
			t.Fatalf("fuzz report %s must bind GitHub-owned run metadata", key)
		}
	}
	// This condition uses only string equality and boolean operators. Evaluate
	// its actual expression in bash with metadata passed through environment
	// variables, preserving parentheses and avoiding executable interpolation.
	condition := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(job.If), "${{"), "}}"))
	fields := regexp.MustCompile(`github\.event\.workflow_run\.([a-z_]+)`)
	condition = fields.ReplaceAllString(condition, `"${FIXTURE_$1}"`)
	for _, source := range []struct {
		name, path, event string
	}{
		{"ci-main", ".github/workflows/ci-main.yml", "workflow_dispatch"},
		{"ci", ".github/workflows/ci.yml", "workflow_dispatch"},
		{"ci", ".github/workflows/ci.yml", "push"},
		{"nightly-release", ".github/workflows/nightly.yml", "schedule"},
		{"nightly-release", ".github/workflows/nightly.yml", "repository_dispatch"},
	} {
		for _, mutation := range []string{"valid", "wrong-branch", "success", "cancelled", "wrong-name", "wrong-path", "pull-request", "wrong-event"} {
			t.Run(source.name+"/"+source.event+"/"+mutation, func(t *testing.T) {
				metadata := map[string]string{"name": source.name, "path": source.path, "event": source.event, "head_branch": "main", "conclusion": "failure"}
				switch mutation {
				case "wrong-branch":
					metadata["head_branch"] = "feature"
				case "success", "cancelled":
					metadata["conclusion"] = mutation
				case "wrong-name":
					metadata["name"] = "untrusted-validation"
				case "wrong-path":
					metadata["path"] = ".github/workflows/untrusted.yml"
				case "pull-request":
					metadata["event"] = "pull_request"
				case "wrong-event":
					if source.name == "ci" || source.name == "ci-main" {
						metadata["event"] = "schedule"
					} else {
						metadata["event"] = "workflow_dispatch"
					}
				}
				cmd := exec.Command("bash", "-e", "-c", "if [[ "+condition+" ]]; then exit 0; else exit 1; fi")
				cmd.Env = os.Environ()
				for key, value := range metadata {
					cmd.Env = append(cmd.Env, "FIXTURE_"+key+"="+value)
				}
				out, err := cmd.CombinedOutput()
				if (err == nil) != (mutation == "valid") {
					t.Fatalf("main reporting source refusal: %v %s", err, out)
				}
			})
		}
	}
}
