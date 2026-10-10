package ci_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReleaseFloorBenchPRUsesRequiredValidation(t *testing.T) {
	repoRoot := repositoryRoot(t)
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(repoRoot, path))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	var floor struct {
		On map[string]yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(read(".github/workflows/floor-bench.yml"), &floor); err != nil {
		t.Fatal(err)
	}
	if _, ok := floor.On["workflow_call"]; !ok {
		t.Fatal("floor benchmark must remain reusable by required validation")
	}
	if len(floor.On) != 2 {
		t.Fatalf("floor benchmark must have only reusable and tag triggers, got %v", floor.On)
	}
	push, ok := floor.On["push"]
	if !ok {
		t.Fatal("floor benchmark must retain its tag trigger")
	}
	var tagTrigger struct {
		Tags []string `yaml:"tags"`
	}
	if err := push.Decode(&tagTrigger); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tagTrigger.Tags, []string{"**"}) {
		t.Fatalf("floor benchmark tag trigger changed: %v", tagTrigger.Tags)
	}

	var validation mainValidationWorkflow
	if err := yaml.Unmarshal(read(".github/workflows/ci.yml"), &validation); err != nil {
		t.Fatal(err)
	}
	job := validation.Jobs["floor-bench"]
	if job.Uses != "./.github/workflows/floor-bench.yml" || !reflect.DeepEqual(workflowPlanJobs(job.If), []string{"floor-bench"}) {
		t.Fatalf("required validation must call the planned floor benchmark, got %+v", job)
	}
	if !reflect.DeepEqual(job.With, map[string]string{"revision": "${{ inputs.revision }}"}) {
		t.Fatal("required floor measurement must inherit CI's validated immutable revision")
	}
	dependencies, err := workflowNeeds(job.Needs)
	if err != nil || !reflect.DeepEqual(dependencies, []string{"changes"}) {
		t.Fatalf("floor benchmark must depend on the change plan: %v, %v", dependencies, err)
	}
	dependencies, err = workflowNeeds(validation.Jobs["ci-required"].Needs)
	if err != nil || !slices.Contains(dependencies, "floor-bench") {
		t.Fatalf("required aggregate must include the floor benchmark: %v, %v", dependencies, err)
	}

	var jobRegistry registry
	if err := json.Unmarshal(read("scripts/ci/ci-job-registry.json"), &jobRegistry); err != nil {
		t.Fatal(err)
	}
	if rule := jobRegistry.Jobs["floor-bench"]; rule.RequiredGate != "planned" || !reflect.DeepEqual(rule.PlanJobs, []string{"floor-bench"}) {
		t.Fatalf("floor benchmark must remain a planned required gate, got %+v", rule)
	}

	classifier := exec.Command("sh", filepath.Join(repoRoot, "scripts/ci/classify-changed-paths.sh"), "--files")
	classifier.Dir = repoRoot
	classifier.Stdin = strings.NewReader(".github/workflows/floor-bench.yml\n")
	output, err := classifier.Output()
	if err != nil {
		t.Fatalf("classify floor workflow change: %v", err)
	}
	var plan map[string]bool
	if err := json.Unmarshal(output, &plan); err != nil {
		t.Fatalf("parse floor workflow change plan: %v", err)
	}
	if !plan["floor-bench"] {
		t.Fatal("a floor workflow change must select its required benchmark")
	}
	for _, job := range jobRegistry.PathClasses["full"] {
		if !plan[job] {
			t.Errorf("a workflow change must select the full validation plan, missing %s", job)
		}
	}
}

func TestReleaseFloorBenchImmutableSourceContract(t *testing.T) {
	read := func(name string) mainValidationWorkflow {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		var got mainValidationWorkflow
		if err := yaml.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	floor := read("floor-bench.yml")
	var contract struct {
		Inputs map[string]struct {
			Required bool   `yaml:"required"`
			Type     string `yaml:"type"`
		} `yaml:"inputs"`
	}
	call := floor.On["workflow_call"]
	if err := call.Decode(&contract); err != nil {
		t.Fatal(err)
	}
	if len(contract.Inputs) != 1 || !contract.Inputs["revision"].Required || contract.Inputs["revision"].Type != "string" {
		t.Fatal("reusable floor proof requires one immutable revision input")
	}
	if !reflect.DeepEqual(read("release.yml").Jobs["floor-bench"].With, map[string]string{"revision": "${{ github.sha }}"}) {
		t.Fatal("stable release must measure its exact tag event SHA")
	}
	steps := floor.Jobs["measure"].Steps
	if len(steps) < 3 {
		t.Fatal("floor source guard must precede both checkout lanes")
	}
	guard := steps[0]
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
	if guard.Name != "Validate the immutable caller or tag revision" || guard.Run == "" || guard.If != "" || !reflect.DeepEqual(guard.Env, expectedEnv) {
		t.Fatal("floor guard must independently bind input to authenticated event metadata")
	}
	for index, expected := range []struct{ condition, revision string }{
		{"github.event_name != 'push'", "${{ inputs.revision }}"},
		{"github.event_name == 'push'", "${{ github.sha }}"},
	} {
		step := steps[index+1]
		if step.Uses != "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1" || step.If != expected.condition || step.With["ref"] != expected.revision || step.With["persist-credentials"] != "false" {
			t.Fatalf("floor checkout lane %d lost its immutable source contract", index)
		}
	}
	measured, uploaded := false, false
	for _, step := range steps[3:] {
		if strings.HasPrefix(step.Uses, "actions/checkout@") || strings.HasPrefix(step.Uses, "actions/cache@") || strings.HasPrefix(step.Uses, "actions/cache/save@") {
			t.Fatal("floor measurement must not change source or write a shared cache")
		}
		if strings.HasPrefix(step.Uses, "actions/setup-go@") && step.With["cache"] != "false" {
			t.Fatal("floor Go setup must remain cache free")
		}
		measured = measured || step.Run == `./scripts/bench/floor.sh "$RUNNER_TEMP/floor-bench"`
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
			uploaded = step.If == "always()" && step.With["name"] == "floor-bench-${{ inputs.revision || github.sha }}" && step.With["retention-days"] == "90" && step.With["if-no-files-found"] == "error"
		}
	}
	if !measured || !uploaded {
		t.Fatal("full floor measurement and exact-source evidence must remain intact")
	}
	for _, event := range []string{"pull_request", "merge_group", "workflow_dispatch", "schedule", "repository_dispatch", "push", "pull_request_target", "workflow_run"} {
		for _, ref := range []string{"refs/heads/main", "refs/heads/feature", "refs/tags/v1.2.3", "refs/tags/"} {
			for _, mutation := range []string{"valid", "wrong-revision", "missing-revision", "mutable-ref", "uppercase-sha", "invalid-tag-sha", "wrong-repository", "missing-repository-id", "wrong-base-repository", "wrong-base-id", "missing-head-repository", "missing-head-id", "missing-pr", "wrong-merge-sha", "same-repository-head"} {
				t.Run(event+"/"+ref+"/"+mutation, func(t *testing.T) {
					head, merge := strings.Repeat("a", 40), strings.Repeat("b", 40)
					source := merge
					if event == "pull_request" {
						source = head
					}
					env := map[string]string{"GITHUB_EVENT_NAME": event, "GITHUB_REF": ref, "GITHUB_SHA": merge, "GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "SOURCE_REVISION": source, "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10", "PR_NUMBER": "882", "PR_HEAD_SHA": head, "PR_HEAD_REPOSITORY": "fork/Hikyo", "PR_HEAD_REPOSITORY_ID": "11", "PR_BASE_REPOSITORY": "Hikyo-Org/Hikyo", "PR_BASE_REPOSITORY_ID": "10", "MERGE_GROUP_SHA": merge}
					want := event == "pull_request" || event == "merge_group" ||
						(ref == "refs/heads/main" && (event == "workflow_dispatch" || event == "schedule" || event == "repository_dispatch")) ||
						(event == "push" && ref == "refs/tags/v1.2.3")
					switch mutation {
					case "wrong-revision":
						env["SOURCE_REVISION"] = strings.Repeat("c", 40)
						want = false
					case "missing-revision":
						env["SOURCE_REVISION"] = ""
						want = event == "push" && ref == "refs/tags/v1.2.3"
					case "mutable-ref", "uppercase-sha":
						env["SOURCE_REVISION"] = "main"
						if mutation == "uppercase-sha" {
							env["SOURCE_REVISION"] = strings.Repeat("A", 40)
						}
						want = false
					case "invalid-tag-sha":
						env["GITHUB_SHA"] = strings.Repeat("B", 40)
						if event == "push" {
							env["SOURCE_REVISION"] = ""
						}
						want = want && event == "pull_request"
					case "wrong-repository", "missing-repository-id":
						env["EVENT_REPOSITORY"] = "other/Hikyo"
						if mutation == "missing-repository-id" {
							env["EVENT_REPOSITORY"] = "Hikyo-Org/Hikyo"
							env["EVENT_REPOSITORY_ID"] = ""
						}
						want = false
					case "wrong-base-repository", "wrong-base-id", "missing-head-repository", "missing-head-id", "missing-pr":
						key, value := "PR_BASE_REPOSITORY", "other/Hikyo"
						switch mutation {
						case "wrong-base-id":
							key, value = "PR_BASE_REPOSITORY_ID", "99"
						case "missing-head-repository":
							key, value = "PR_HEAD_REPOSITORY", ""
						case "missing-head-id":
							key, value = "PR_HEAD_REPOSITORY_ID", ""
						case "missing-pr":
							key, value = "PR_NUMBER", ""
						}
						env[key] = value
						want = want && event != "pull_request"
					case "wrong-merge-sha":
						env["MERGE_GROUP_SHA"] = head
						want = want && event != "merge_group"
					case "same-repository-head":
						env["PR_HEAD_REPOSITORY"], env["PR_HEAD_REPOSITORY_ID"] = "Hikyo-Org/Hikyo", "10"
					}
					cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", guard.Run)
					cmd.Env = os.Environ()
					for key, value := range env {
						cmd.Env = append(cmd.Env, key+"="+value)
					}
					if output, err := cmd.CombinedOutput(); (err == nil) != want {
						t.Fatalf("floor source allowed=%v, want %v: %v %s", err == nil, want, err, output)
					}
				})
			}
		}
	}
}
