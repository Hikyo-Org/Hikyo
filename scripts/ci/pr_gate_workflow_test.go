package ci_test

import (
	"encoding/json"
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
			If          string            `yaml:"if"`
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
	if len(events) != 5 {
		t.Fatal("publisher must handle PR lifecycle, validation callbacks, manual repair and scheduled repair")
	}
	for _, name := range []string{"pull_request_target", "workflow_run", "workflow_dispatch", "schedule", "push"} {
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
	if !slices.Equal(callback.Types, []string{"requested", "in_progress", "completed"}) || !slices.Equal(callback.Workflows, []string{"pr-candidate"}) {
		t.Fatal("callback must cover new runs, reruns and completion under both workflow names")
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		t.Fatal("publisher defaults must be contents-read only")
	}
	if workflow.Concurrency.Group != "pr-gate-publisher" || workflow.Concurrency.Cancel || workflow.Concurrency.Queue != "max" {
		t.Fatal("commit-scoped check mutations require a single non-cancelling writer with pending-event retention")
	}
	job, ok := workflow.Jobs["publish"]
	if job.If != "github.event_name != 'workflow_run' || !startsWith(github.event.workflow_run.display_title, 'pr-metadata #')" {
		t.Fatal("only metadata-only callback runs may skip the publisher runner; lifecycle and validation events must reconcile")
	}
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
		t.Fatal("publisher must execute only the trusted controller under a required complete-candidate check name")
	}
	for _, forbidden := range []string{"secrets.", "download-artifact", "continue-on-error", "pull_request.head.sha"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("publisher crosses its trusted boundary: %s", forbidden)
		}
	}
}

// The required API context must be produced by the independent trusted
// controller, never by an admission-skipped native PR job.
func TestRequiredJobCannotSkipOnPullRequests(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"ci-control.yml", "ci-fork.yml", "ci-gate-reconcile.yml"} {
		if _, err := os.Stat(filepath.Join(root, ".github/workflows", name)); !os.IsNotExist(err) {
			t.Fatalf("obsolete automatic producer %s must be retired", name)
		}
	}
	var policy struct {
		Rules []struct {
			Type       string `json:"type"`
			Parameters struct {
				Checks []struct {
					Context string `json:"context"`
				} `json:"required_status_checks"`
			} `json:"parameters"`
		} `json:"rules"`
	}
	raw, err := os.ReadFile(filepath.Join(root, "release/repository/main-ci-gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	var contexts []string
	for _, rule := range policy.Rules {
		if rule.Type == "required_status_checks" {
			for _, c := range rule.Parameters.Checks {
				contexts = append(contexts, c.Context)
			}
		}
	}
	if !slices.Equal(contexts, []string{"ci-queue-required"}) {
		t.Fatal("canonical policy must require the trusted queue producer")
	}
	var observer struct {
		Jobs map[string]struct {
			If    string    `yaml:"if"`
			Needs yaml.Node `yaml:"needs"`
			Steps []struct {
				Run string            `yaml:"run"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	raw, err = os.ReadFile(filepath.Join(root, ".github/workflows/ci-pr-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &observer); err != nil {
		t.Fatal(err)
	}
	job := observer.Jobs["publish"]
	if job.If != "github.event_name != 'workflow_run' || !startsWith(github.event.workflow_run.display_title, 'pr-metadata #')" || job.Needs.Kind != 0 {
		t.Fatal("head/base lifecycle reconciliation must run independently of label/vouch/validation admission")
	}
	found := false
	for _, step := range job.Steps {
		if step.Run == "go run ./scripts/ci/pr-gate" {
			found = step.Env["PR_GATE_CHECK_NAME"] == "ci-required-next"
		}
	}
	if !found {
		t.Fatal("legacy rollback context must retain its full-only trusted API publisher")
	}
	if strings.Contains(string(raw), "uses: ./.github/workflows/ci.yml") {
		t.Fatal("credentialed publisher must never execute the untrusted validation graph")
	}
	queueRaw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-queue-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// The canonical context is now published by direct expected-head jobs and
	// dispatched head workers. Reuse their actual-source trust contracts instead
	// of tying merge eligibility to the former unbounded go-run command.
	if err := queuePublisherContract(queueRaw); err != nil {
		t.Fatalf("canonical queue context must match trusted direct writers and read-only discovery: %v", err)
	}
	workerRaw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-queue-worker.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := queueWorkerContract(workerRaw); err != nil {
		t.Fatalf("canonical queue context must match trusted expected-head workers: %v", err)
	}

	// GitHub required contexts have a name/app identity, not workflow identity.
	// This prevents accidental native collisions; actual hostile collisions are
	// refused with owned pending barriers by the controller's runtime tests.
	entries, err := os.ReadDir(filepath.Join(root, ".github/workflows"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, ".github/workflows", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var w struct {
			Jobs map[string]struct {
				Name string `yaml:"name"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(content, &w); err != nil {
			t.Fatal(err)
		}
		for id, j := range w.Jobs {
			if id == contexts[0] || j.Name == contexts[0] || id == "ci-required-next" || j.Name == "ci-required-next" {
				t.Fatalf("native workflow %s must not publish the required PR context", entry.Name())
			}
		}
	}
}
