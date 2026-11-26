package ci_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type revokeWorkflowJob struct {
	If              string            `yaml:"if"`
	Needs           yaml.Node         `yaml:"needs"`
	Strategy        map[string]any    `yaml:"strategy"`
	Timeout         any               `yaml:"timeout-minutes"`
	ContinueOnError bool              `yaml:"continue-on-error"`
	Perms           map[string]string `yaml:"permissions"`
	Steps           []struct {
		Name string            `yaml:"name"`
		Env  map[string]string `yaml:"env"`
		Run  string            `yaml:"run"`
	} `yaml:"steps"`
}

func readRevokeWorkflow(t *testing.T, file string) map[string]revokeWorkflowJob {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", file))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]revokeWorkflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow.Jobs
}

func needsOf(t *testing.T, j revokeWorkflowJob) []string {
	t.Helper()
	var list []string
	switch j.Needs.Kind {
	case 0:
	case yaml.ScalarNode:
		list = []string{j.Needs.Value}
	default:
		if err := j.Needs.Decode(&list); err != nil {
			t.Fatal(err)
		}
	}
	return list
}

// rerunClosure models GitHub's rerun of one job: the job and every job that needs it, transitively.
func rerunClosure(t *testing.T, jobs map[string]revokeWorkflowJob, id string) []string {
	t.Helper()
	closure := []string{id}
	for grew := true; grew; {
		grew = false
		for other, j := range jobs {
			if slices.Contains(closure, other) {
				continue
			}
			for _, n := range needsOf(t, j) {
				if slices.Contains(closure, n) {
					closure = append(closure, other)
					grew = true
					break
				}
			}
		}
	}
	slices.Sort(closure)
	return closure
}

// A push to main or dev, or a dispatch for every PR, makes every earlier
// ci-dev-required success potentially stale. The publish matrix reconciles heads
// at most max-parallel at a time, so a head near the end of the matrix would keep
// its stale success for minutes. The dev-revoke job runs first, outside the
// matrix, and blocks every routed head before any throttled work starts.
func TestDevGateRevokesEveryRoutedHeadBeforeThrottledWork(t *testing.T) {
	jobs := readRevokeWorkflow(t, "ci-dev-gate.yml")
	revoke, publish := jobs["revoke"], jobs["publish"]
	if revoke.Strategy != nil {
		t.Fatal("the revocation first pass must not sit behind a throttle: no matrix, no max-parallel")
	}
	if !reflect.DeepEqual(needsOf(t, revoke), []string{"discover"}) {
		t.Fatalf("revoke must need only discovery, got %v", needsOf(t, revoke))
	}
	if !reflect.DeepEqual(needsOf(t, publish), []string{"discover", "revoke"}) {
		t.Fatalf("no throttled reconcile may start before the first pass finishes: needs %v", needsOf(t, publish))
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

// GitHub reruns a job together with every job that needs it. Every dev publisher needs
// dev-revoke, so a rerun of dev-revoke would cost one reconcile for each head. The structure
// keeps that from happening: the job cannot fail, the retry workflow never selects it, and
// the jobs the retry does select have no dependents.
func TestDevRevokeRerunCannotCascadeIntoThePublishers(t *testing.T) {
	dev := readRevokeWorkflow(t, "ci-dev-gate.yml")
	if got := rerunClosure(t, dev, "revoke"); !reflect.DeepEqual(got, []string{"publish", "revoke"}) {
		t.Fatalf("model check: a revoke rerun must reach its dependents, got %v", got)
	}
	if !dev["revoke"].ContinueOnError {
		t.Fatal("a failed first pass must not fail its job: a failed job is a rerun candidate, and its rerun starts every publisher")
	}
	retryRaw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-queue-retry.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(retryRaw), `and .name != "dev-revoke")`); n != 2 {
		t.Fatalf("both failed-job filters of the retry workflow must leave dev-revoke alone, found %d", n)
	}
	// The jobs the retry reruns by id are the publishers and group jobs; none may have a dependent.
	for file, ids := range map[string][]string{"ci-dev-gate.yml": {"publish"}, "ci-queue-worker.yml": {"publish"}, "ci-queue-gate.yml": {"publish", "group"}} {
		jobs := readRevokeWorkflow(t, file)
		for _, id := range ids {
			if got := rerunClosure(t, jobs, id); !reflect.DeepEqual(got, []string{id}) {
				t.Fatalf("%s: rerunning %s would also rerun %v, which the retry budget does not count", file, id, got)
			}
		}
	}
}

// Recorded in docs/development/ci.md.
func TestDevRevokeDesignIsDocumented(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs", "development", "ci.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"##### Revocation is not queued behind the throttle", "dev-revoke", "ci-queue-worker.yml", "ci-queue-retry.yml", "PR validation revoked", "25%", "3 + N"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("docs/development/ci.md must record the revocation design and the sibling matrices: missing %q", want)
		}
	}
}
