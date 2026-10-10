package ci_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The publisher holds check-write credentials. Pin its executable boundary,
// including absence of a skipped job that could satisfy the required check.
func TestPRGateWorkflowTrust(t *testing.T) {
	var workflow struct {
		On map[string]struct {
			Types     []string `yaml:"types"`
			Workflows []string `yaml:"workflows"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
			Queue  string `yaml:"queue"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			Timeout     int               `yaml:"timeout-minutes"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Name string            `yaml:"name"`
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-pr-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// schedule is a sequence; parse event keys separately from typed triggers.
	var document map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	var events map[string]yaml.Node
	on := document["on"]
	if err := on.Decode(&events); err != nil {
		t.Fatal(err)
	}
	delete(document, "on")
	withoutOn, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(withoutOn, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatal("publisher must handle PR lifecycle, validation callbacks, manual repair and scheduled repair")
	}
	for _, name := range []string{"pull_request_target", "workflow_run", "workflow_dispatch", "schedule"} {
		if _, ok := events[name]; !ok {
			t.Fatalf("missing trusted event %s", name)
		}
	}
	var pr struct {
		Types []string `yaml:"types"`
	}
	prEvent := events["pull_request_target"]
	if err := prEvent.Decode(&pr); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pr.Types, []string{"opened", "synchronize", "reopened", "edited", "closed"}) {
		t.Fatal("head changes, retargets and same-head PR membership changes must reconcile the gate")
	}
	var callback struct {
		Types     []string `yaml:"types"`
		Workflows []string `yaml:"workflows"`
	}
	callbackEvent := events["workflow_run"]
	if err := callbackEvent.Decode(&callback); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(callback.Types, []string{"requested", "in_progress", "completed"}) || !slices.Equal(callback.Workflows, []string{"fork-ci", "pr-validation"}) {
		t.Fatal("callback must cover new runs, reruns and completion under both workflow names")
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		t.Fatal("publisher defaults must be contents-read only")
	}
	if workflow.Concurrency.Group != "pr-gate-publisher" || workflow.Concurrency.Cancel || workflow.Concurrency.Queue != "max" {
		t.Fatal("commit-scoped check mutations require a single non-cancelling writer with pending-event retention")
	}
	job, ok := workflow.Jobs["publish"]
	if !ok || len(workflow.Jobs) != 1 || job.Timeout <= 0 || job.Timeout > 10 {
		t.Fatal("publisher must be one bounded job, never a ci-required job")
	}
	if len(job.Permissions) != 4 || job.Permissions["checks"] != "write" || job.Permissions["actions"] != "read" || job.Permissions["contents"] != "read" || job.Permissions["pull-requests"] != "read" {
		t.Fatal("only check publication may write; source, actions and PR metadata remain read-only")
	}
	if len(job.Steps) != 3 {
		t.Fatal("publisher may only check out trusted code, prepare Go, and run the controller")
	}
	checkout, setup, publish := job.Steps[0], job.Steps[1], job.Steps[2]
	if !strings.HasPrefix(checkout.Uses, "actions/checkout@") || checkout.With["ref"] != "${{ github.event.repository.default_branch }}" || checkout.With["persist-credentials"] != "false" {
		t.Fatal("publisher must execute default-branch tooling without persisted credentials")
	}
	if !strings.HasPrefix(setup.Uses, "actions/setup-go@") || setup.With["cache"] != "false" || setup.With["go-version-file"] != "go.mod" {
		t.Fatal("trusted publisher must use repository-pinned Go without consuming execution caches")
	}
	if publish.Name != "Reconcile observed PR check" || publish.Run != "go run ./scripts/ci/pr-gate" || len(publish.Env) != 3 || publish.Env["GH_TOKEN"] != "${{ github.token }}" || publish.Env["GH_REPO"] != "${{ github.repository }}" || publish.Env["PR_GATE_CHECK_NAME"] != "ci-required-next" {
		t.Fatal("observer must execute only the trusted controller under an isolated non-required check name")
	}
	for _, forbidden := range []string{"secrets.", "download-artifact", "continue-on-error", "pull_request.head.sha"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("publisher crosses its trusted boundary: %s", forbidden)
		}
	}
}

func TestRequiredJobCannotSkipOnPullRequests(t *testing.T) {
	var workflow struct {
		On   map[string]yaml.Node `yaml:"on"`
		Jobs map[string]struct {
			If    string `yaml:"if"`
			Needs string `yaml:"needs"`
			Steps []struct {
				Name string            `yaml:"name"`
				If   string            `yaml:"if"`
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-control.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.On) != 1 {
		t.Fatal("the base-controlled PR gate must have no event that calls the validation graph")
	}
	if _, ok := workflow.On["pull_request_target"]; !ok {
		t.Fatal("the existing base-controlled PR gate must remain enabled during observation")
	}
	gate, ok := workflow.Jobs["ci-required"]
	if !ok || len(workflow.Jobs) != 1 || gate.If != "always() && !cancelled()" || gate.Needs != "" {
		t.Fatal("the legacy required PR gate must run independently of untrusted validation")
	}
	checkedBase, verifiedPR := false, false
	for _, step := range gate.Steps {
		if step.Name == "Check out the base branch" {
			checkedBase = step.If == "github.event_name == 'pull_request_target'" && step.With["ref"] == "${{ github.event.pull_request.base.sha }}" && step.With["persist-credentials"] == "false"
		}
		if step.Run == "./scripts/ci/check-fork-validation.sh" {
			verifiedPR = step.If == "github.event_name == 'pull_request_target'" && step.Env["PR_NUMBER"] == "${{ github.event.pull_request.number }}" && step.Env["HEAD_SHA"] == "${{ github.event.pull_request.head.sha }}" && step.Env["PR_GATE_ONCE"] == ""
		}
	}
	if !checkedBase || !verifiedPR {
		t.Fatal("the required PR gate must execute its real base-controlled verifier rather than skip or accept pending")
	}
	var observer struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string            `yaml:"run"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	raw, err = os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-pr-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &observer); err != nil {
		t.Fatal(err)
	}
	isolated := false
	for _, step := range observer.Jobs["publish"].Steps {
		if step.Run == "go run ./scripts/ci/pr-gate" {
			isolated = step.Env["PR_GATE_CHECK_NAME"] == "ci-required-next"
		}
	}
	if !isolated {
		t.Fatal("observation must not emit the existing required check name")
	}
}
