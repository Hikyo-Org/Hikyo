package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCandidateWorkflowAdmissionAndCoverage(t *testing.T) {
	read := func(name string) mainValidationWorkflow {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		var result mainValidationWorkflow
		if err := yaml.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	candidate := read("ci-candidate.yml")
	if len(candidate.On) != 1 {
		t.Fatal("candidate execution must remain PR-scoped")
	}
	var trigger struct {
		Types []string `yaml:"types"`
	}
	pr := candidate.On["pull_request"]
	if err := pr.Decode(&trigger); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(trigger.Types, []string{"labeled"}) {
		t.Fatal("pushes and unrelated lifecycle events must not restart expensive work")
	}
	validation := candidate.Jobs["validation"]
	if validation.Uses != "./.github/workflows/ci.yml" || !reflect.DeepEqual(validation.With, map[string]string{"revision": "${{ github.sha }}", "candidate": "true"}) {
		t.Fatal("candidate must request complete validation of the authenticated merge result")
	}
	if !strings.Contains(validation.If, "github.event.label.name == 'ci:full'") || !strings.Contains(validation.If, "needs.vouch.result == 'success'") {
		t.Fatal("label requests work, and fork author admission remains mandatory")
	}
	proof := candidate.Jobs["candidate-proof"]
	needs, err := workflowNeeds(proof.Needs)
	if err != nil || !reflect.DeepEqual(needs, []string{"validation"}) {
		t.Fatal("proof must depend on the entire full suite")
	}
	if proof.If != "${{ always() && !cancelled() && github.event.label.name == 'ci:full' }}" || proof.Steps[0].Run != "test \"$VALIDATION_RESULT\" = success" {
		t.Fatal("failed or skipped candidate validation must fail the completion proof")
	}
	ci := read("ci.yml")
	classifier := ci.Jobs["changes"].Steps[2]
	for _, required := range []string{"[ \"$FULL_CANDIDATE\" = true ]", "git rev-parse HEAD^1", "git rev-parse HEAD^2", "CI_JOB_REGISTRY=\"$trusted_registry\" \"$trusted_classifier\" --all"} {
		if !strings.Contains(classifier.Run, required) {
			t.Fatalf("missing candidate planning boundary: %s", required)
		}
	}
	checker := ci.Jobs["ci-required"].Steps[len(ci.Jobs["ci-required"].Steps)-1]
	if !strings.Contains(checker.Run, "[ \"$FULL_CANDIDATE\" = true ]") || !strings.Contains(checker.Run, "checker_event=merge_group") {
		t.Fatal("full candidates must reject selective plans independently of classification")
	}
	guard := ci.Jobs["changes"].Steps[0]
	for _, mutation := range []string{"valid", "head-only", "wrong-merge", "missing-base", "invalid-mode", "wrong-repository"} {
		t.Run(mutation, func(t *testing.T) {
			head, merge, base := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
			env := map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_REF": "refs/pull/1/merge", "GITHUB_SHA": merge, "GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "SOURCE_REVISION": merge, "FULL_CANDIDATE": "true", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10", "PR_NUMBER": "1", "PR_HEAD_SHA": head, "PR_MERGE_SHA": merge, "PR_BASE_SHA": base, "PR_HEAD_REPOSITORY": "fork/Hikyo", "PR_HEAD_REPOSITORY_ID": "11", "PR_BASE_REPOSITORY": "Hikyo-Org/Hikyo", "PR_BASE_REPOSITORY_ID": "10"}
			switch mutation {
			case "head-only":
				env["SOURCE_REVISION"] = head
			case "wrong-merge":
				env["PR_MERGE_SHA"] = base
			case "missing-base":
				env["PR_BASE_SHA"] = ""
			case "invalid-mode":
				env["FULL_CANDIDATE"] = "yes"
			case "wrong-repository":
				env["PR_BASE_REPOSITORY"] = "other/Hikyo"
			}
			command := exec.Command("bash", "-euo", "pipefail", "-c", guard.Run)
			command.Env = os.Environ()
			for name, value := range env {
				command.Env = append(command.Env, name+"="+value)
			}
			output, err := command.CombinedOutput()
			if (err == nil) != (mutation == "valid") {
				t.Fatalf("candidate source allowed=%v: %v %s", err == nil, err, output)
			}
		})
	}
}
