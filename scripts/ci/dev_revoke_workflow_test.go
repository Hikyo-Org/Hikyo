package ci_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A push to main or dev, or a dispatch for every PR, makes every earlier
// ci-dev-required success potentially stale. The publish matrix reconciles heads
// at most max-parallel at a time, so a head near the end of the matrix would keep
// its stale success for minutes. The dev-revoke job runs first, outside the
// matrix, and blocks every routed head before any throttled work starts.
func TestDevGateRevokesEveryRoutedHeadBeforeThrottledWork(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-dev-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			If       string            `yaml:"if"`
			Needs    yaml.Node         `yaml:"needs"`
			Strategy map[string]any    `yaml:"strategy"`
			Timeout  int               `yaml:"timeout-minutes"`
			Perms    map[string]string `yaml:"permissions"`
			Steps    []struct {
				Uses string            `yaml:"uses"`
				Env  map[string]string `yaml:"env"`
				Run  string            `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	revoke, publish := workflow.Jobs["revoke"], workflow.Jobs["publish"]
	if revoke.Strategy != nil {
		t.Fatal("the revocation first pass must not sit behind a throttle: no matrix, no max-parallel")
	}
	var revokeNeeds, publishNeeds any
	if err := revoke.Needs.Decode(&revokeNeeds); err != nil || revokeNeeds != "discover" {
		t.Fatalf("revoke must need only discovery, got %v", revokeNeeds)
	}
	if err := publish.Needs.Decode(&publishNeeds); err != nil || !reflect.DeepEqual(publishNeeds, []any{"discover", "revoke"}) {
		t.Fatalf("no throttled reconcile may start before the first pass finishes: needs %v", publishNeeds)
	}
	// The first pass is an addition, not a gate: its failure or skip must not stop the reconcile.
	if !strings.Contains(publish.If, "!cancelled()") || strings.Contains(publish.If, "needs.revoke") || !strings.Contains(publish.If, "needs.discover.result == 'success'") {
		t.Fatalf("publish must run after a failed or skipped first pass, and still not after failed discovery: %q", publish.If)
	}
	for _, event := range []string{"github.event_name == 'push'", "github.event_name == 'workflow_dispatch'", "github.event.inputs.pr_number == ''", "needs.discover.outputs.work != '[]'"} {
		if !strings.Contains(revoke.If, event) {
			t.Fatalf("revoke must run on every fan-out event and nothing else: missing %q in %q", event, revoke.If)
		}
	}
	if strings.Contains(revoke.If, "pull_request_target") || strings.Contains(revoke.If, "schedule") || strings.Contains(revoke.If, "workflow_run") {
		t.Fatal("single-head events and the sweep are not throttled behind a fan-out and need no first pass")
	}
	if !reflect.DeepEqual(revoke.Perms, map[string]string{"actions": "read", "contents": "read", "pull-requests": "read", "checks": "write"}) || revoke.Timeout != 4 {
		t.Fatal("the first pass keeps the publisher's narrow permissions and a bounded timeout")
	}
	var run, work string
	for _, step := range revoke.Steps {
		run += step.Run + "\n"
		if v, ok := step.Env["DEV_REVOKE_WORK"]; ok {
			work = v
		}
	}
	if !strings.Contains(run, `"$RUNNER_TEMP/queue-publisher" --dev --dev-revoke`) || work != "${{ needs.discover.outputs.work }}" {
		t.Fatal("the first pass must run the trusted publisher in revoke mode on the discovered work")
	}
	if strings.Contains(run, "enforce") || strings.Contains(run, "rate_limit") {
		t.Fatal("revocation is never deferred by the reserve")
	}
}

// The sibling throttled matrix, the queue head workers, is safe from the same window
// for a different reason, recorded in docs/development/ci.md: a stale PR-head admission
// only lets a head enter the queue, and the merge group is judged by its own proof.
func TestQueueWorkerThrottleIsDocumentedAsNotRevocationCritical(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs", "development", "ci.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"##### Revocation is not queued behind the throttle", "dev-revoke", "ci-queue-worker.yml", "ci-queue-retry.yml"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("docs/development/ci.md must record the revocation design and the sibling matrices: missing %q", want)
		}
	}
}
