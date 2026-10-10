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

// Each job independently binds its source before checkout, including the
// always-running aggregate when planning or another dependency has failed.
func TestValidationCheckoutWorkflowPolicy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var ci mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &ci); err != nil {
		t.Fatal(err)
	}
	guard := ci.Jobs["changes"].Steps[0]
	expectedEnv := map[string]string{
		"SOURCE_REVISION":       "${{ inputs.revision }}",
		"EVENT_REPOSITORY":      "${{ github.event.repository.full_name }}",
		"EVENT_REPOSITORY_ID":   "${{ github.event.repository.id }}",
		"PR_NUMBER":             "${{ github.event.pull_request.number }}",
		"PR_HEAD_SHA":           "${{ github.event.pull_request.head.sha }}",
		"PR_HEAD_REPOSITORY":    "${{ github.event.pull_request.head.repo.full_name }}",
		"PR_HEAD_REPOSITORY_ID": "${{ github.event.pull_request.head.repo.id }}",
		"PR_BASE_REPOSITORY":    "${{ github.event.pull_request.base.repo.full_name }}",
		"PR_BASE_REPOSITORY_ID": "${{ github.event.pull_request.base.repo.id }}",
		"MERGE_GROUP_SHA":       "${{ github.event.merge_group.head_sha }}",
	}
	if guard.Name != "Validate the immutable caller revision" || guard.Run == "" || guard.If != "" || !reflect.DeepEqual(guard.Env, expectedEnv) {
		t.Fatal("pre-checkout authority must come only from the required input and authenticated GitHub metadata")
	}
	fullHistory := map[string]bool{"changes": true, "preflight": true, "client": true, "analysis_shards": true, "race_shard": true, "ci-required": true}
	for name, job := range ci.Jobs {
		if job.Uses != "" {
			continue
		}
		if len(job.Steps) < 2 || !reflect.DeepEqual(job.Steps[0], guard) {
			t.Fatalf("%s must independently validate source before all other steps", name)
		}
		checkout := job.Steps[1]
		if checkout.Name != "Check out the validated immutable revision" || checkout.Uses != "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1" || checkout.If != "" || checkout.With["ref"] != "${{ inputs.revision }}" || checkout.With["persist-credentials"] != "false" {
			t.Fatalf("%s must check out only its validated immutable input without credentials", name)
		}
		if fullHistory[name] && checkout.With["fetch-depth"] != "0" {
			t.Fatalf("%s must preserve full history", name)
		}
		for _, later := range job.Steps[2:] {
			if strings.HasPrefix(later.Uses, "actions/checkout@") {
				t.Fatalf("%s changes source after its validated checkout", name)
			}
		}
	}
	for _, event := range []string{"pull_request", "merge_group", "workflow_dispatch", "schedule", "repository_dispatch", "pull_request_target", "push", "workflow_run"} {
		for _, ref := range []string{"refs/heads/main", "refs/heads/feature"} {
			for _, mutation := range []string{"valid", "wrong-revision", "missing-revision", "mutable-ref", "uppercase-sha", "wrong-repository", "missing-repository-id", "wrong-base-repository", "wrong-base-id", "missing-head-repository", "missing-head-id", "missing-pr", "wrong-merge-sha"} {
				t.Run(event+"/"+ref+"/"+mutation, func(t *testing.T) {
					head, merge := strings.Repeat("a", 40), strings.Repeat("b", 40)
					source := merge
					if event == "pull_request" {
						source = head
					}
					env := map[string]string{"GITHUB_EVENT_NAME": event, "GITHUB_REF": ref, "GITHUB_SHA": merge, "GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "SOURCE_REVISION": source, "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10", "PR_NUMBER": "882", "PR_HEAD_SHA": head, "PR_HEAD_REPOSITORY": "fork/Hikyo", "PR_HEAD_REPOSITORY_ID": "11", "PR_BASE_REPOSITORY": "Hikyo-Org/Hikyo", "PR_BASE_REPOSITORY_ID": "10", "MERGE_GROUP_SHA": merge}
					want := event == "pull_request" || event == "merge_group" || (ref == "refs/heads/main" && (event == "workflow_dispatch" || event == "schedule" || event == "repository_dispatch"))
					switch mutation {
					case "wrong-revision":
						env["SOURCE_REVISION"] = strings.Repeat("c", 40)
						want = false
					case "missing-revision":
						env["SOURCE_REVISION"] = ""
						want = false
					case "mutable-ref":
						env["SOURCE_REVISION"] = "main"
						want = false
					case "uppercase-sha":
						env["SOURCE_REVISION"] = strings.Repeat("A", 40)
						want = false
					case "wrong-repository":
						env["EVENT_REPOSITORY"] = "other/Hikyo"
						want = false
					case "missing-repository-id":
						env["EVENT_REPOSITORY_ID"] = ""
						want = false
					case "wrong-base-repository":
						env["PR_BASE_REPOSITORY"] = "other/Hikyo"
						want = want && event != "pull_request"
					case "wrong-base-id":
						env["PR_BASE_REPOSITORY_ID"] = "99"
						want = want && event != "pull_request"
					case "missing-head-repository":
						env["PR_HEAD_REPOSITORY"] = ""
						want = want && event != "pull_request"
					case "missing-head-id":
						env["PR_HEAD_REPOSITORY_ID"] = ""
						want = want && event != "pull_request"
					case "missing-pr":
						env["PR_NUMBER"] = ""
						want = want && event != "pull_request"
					case "wrong-merge-sha":
						env["MERGE_GROUP_SHA"] = head
						want = want && event != "merge_group"
					}
					cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", guard.Run)
					cmd.Env = os.Environ()
					for key, value := range env {
						cmd.Env = append(cmd.Env, key+"="+value)
					}
					output, err := cmd.CombinedOutput()
					if (err == nil) != want {
						t.Fatalf("source guard allowed=%v, want %v: %v %s", err == nil, want, err, output)
					}
				})
			}
		}
	}
}
