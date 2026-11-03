package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// barrierReadAPI holds each listed read (at its given occurrence) until every
// listed read has started. A phase that issues them one after another can never
// satisfy the barrier, so its first read fails with errReadsNotOverlapped.
type barrierReadAPI struct {
	api
	mu      sync.Mutex
	want    map[string]int // endpoint -> occurrence that must overlap
	seen    map[string]int
	arrived map[string]bool
	gate    chan struct{}
}

var errReadsNotOverlapped = errors.New("independent reads of one phase did not overlap")

func newBarrierReadAPI(inner api, want map[string]int) *barrierReadAPI {
	return &barrierReadAPI{api: inner, want: want, seen: map[string]int{}, arrived: map[string]bool{}, gate: make(chan struct{})}
}

func (a *barrierReadAPI) read(ctx context.Context, path string, out any) error {
	a.mu.Lock()
	a.seen[path]++
	wait := a.want[path] == a.seen[path]
	if wait {
		a.arrived[path] = true
		if len(a.arrived) == len(a.want) {
			close(a.gate)
		}
	}
	a.mu.Unlock()
	if wait {
		select {
		case <-a.gate:
		case <-time.After(2 * time.Second):
			return errReadsNotOverlapped
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return a.api.read(ctx, path, out)
}

func (a *barrierReadAPI) overlapped() bool {
	select {
	case <-a.gate:
		return true
	default:
		return false
	}
}

func (f *queueFixture) barrier(t *testing.T, want map[string]int) *barrierReadAPI {
	t.Helper()
	endpoints := map[string]int{}
	for path, n := range want {
		endpoints[f.c.endpoint(path)] = n
	}
	b := newBarrierReadAPI(f.c.api, endpoints)
	f.c.api = b
	return b
}

func TestDevPolicyReadsMainAuthorityBesideRulesets(t *testing.T) {
	f := newDevFixture(t)
	b := f.barrier(t, map[string]int{"rulesets?per_page=100&page=1": 1, "git/ref/heads/main": 1})
	if _, err := f.q.policy(context.Background()); err != nil || !b.overlapped() {
		t.Fatalf("main authority waited for ruleset discovery: %v", err)
	}
}

func TestDevPublisherContractReadsAllThreeCopiesTogether(t *testing.T) {
	f := newDevFixture(t)
	main := strings.Repeat("e", 40)
	b := f.barrier(t, map[string]int{
		"contents/" + devGateWorkflowPath + "?ref=" + main:          1,
		"contents/" + devGateWorkflowPath + "?ref=" + f.pr.Base.SHA: 1,
		"contents/" + devGateWorkflowPath + "?ref=" + f.pr.Head.SHA: 1,
	})
	if err := f.q.devPublisherContract(context.Background(), f.pr); err != nil || !b.overlapped() {
		t.Fatalf("publisher copies were read serially: %v", err)
	}
}

func TestDevInitialAuthorityReadsOverlap(t *testing.T) {
	f := newDevFixture(t)
	f.q.targeted = true // the dev publisher always runs targeted, which reads feedback up front
	b := f.barrier(t, map[string]int{
		"actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=" + f.pr.Head.SHA + "&per_page=100&page=1": 1,
		"pulls?state=open&per_page=100&page=1":                      1,
		"contents/" + devGateWorkflowPath + "?ref=" + f.pr.Base.SHA: 1,
	})
	if err := f.q.admit(context.Background(), f.pr.Number); err != nil || !b.overlapped() {
		t.Fatalf("feedback, role inventory and publisher contract did not overlap: %v", err)
	}
	requireDevSuccess(t, f)
}

func TestDevFinalPRAndPolicyReadTogether(t *testing.T) {
	f := newDevFixture(t)
	b := f.barrier(t, map[string]int{"pulls/7": 2, "rulesets?per_page=100&page=1": 2})
	if err := f.q.admit(context.Background(), f.pr.Number); err != nil || !b.overlapped() {
		t.Fatalf("final PR and policy reads were serial: %v", err)
	}
	requireDevSuccess(t, f)
}

func TestDevCIDefinitionCheckRunsBesideDCO(t *testing.T) {
	f := newDevFixture(t)
	files := f.c.endpoint(devFilesPage(f, 1))
	dcoStarted, filesStarted := make(chan struct{}), make(chan struct{})
	var dcoOnce, filesOnce sync.Once
	f.q.dco = func(ctx context.Context, _ pull) (int, error) {
		dcoOnce.Do(func() { close(dcoStarted) })
		select {
		case <-filesStarted:
		case <-time.After(2 * time.Second):
			return 0, errReadsNotOverlapped
		}
		return 0, nil
	}
	f.c.api = &startedReadAPI{api: f.c.api, path: files, started: func() {
		filesOnce.Do(func() { close(filesStarted) })
		select {
		case <-dcoStarted:
		case <-time.After(2 * time.Second):
			t.Error(errReadsNotOverlapped)
		}
	}}
	if err := f.q.admit(context.Background(), f.pr.Number); err != nil {
		t.Fatalf("DCO and the CI definition check were serial: %v", err)
	}
	requireDevSuccess(t, f)
}

type startedReadAPI struct {
	api
	path    string
	started func()
}

func (a *startedReadAPI) read(ctx context.Context, path string, out any) error {
	if path == a.path {
		a.started()
	}
	return a.api.read(ctx, path, out)
}

// A dev admission starts from the PR its caller already read, so only the final
// comparison reads it again. The final read still happens.
func TestQueueRouteCheckRunsBesideFeedback(t *testing.T) {
	f := newQueueFixture(t)
	f.q.targeted = true
	f.barrier(t, map[string]int{"git/ref/heads/dev": 1, "actions/workflows/ci-feedback.yml": 1})
	// A serial read gives up on the barrier, even though it closes when the other arrives.
	if err := f.q.admit(context.Background(), f.pr.Number); errors.Is(err, errReadsNotOverlapped) {
		t.Fatal(err)
	}
}

// Route and contract reads belong to the gated branch, and need a live policy.
func TestFirstPassSkipsRouteReadsThatCannotApply(t *testing.T) {
	devRef := func(f *queueFixture) int { return f.api.reads[f.c.endpoint("git/ref/heads/dev")] }
	t.Run("another base", func(t *testing.T) {
		f := newQueueFixture(t)
		f.pr.Base.Ref = "dev"
		f.refresh()
		_ = f.q.admit(context.Background(), f.pr.Number)
		if got := devRef(f); got != 0 {
			t.Fatalf("route check read dev %d times for a PR it does not gate", got)
		}
	})
	t.Run("policy off", func(t *testing.T) {
		f := newQueueFixture(t)
		f.rules = f.rules[:len(f.rules)-1]
		f.policyRefresh()
		_ = f.q.admit(context.Background(), f.pr.Number)
		if got := devRef(f); got != 0 {
			t.Fatalf("route check read dev %d times with the queue policy off", got)
		}
	})
}

func TestDevAdmissionReadsThePullRequestTwice(t *testing.T) {
	f := newDevFixture(t)
	t.Setenv("GITHUB_REF", "refs/heads/main")
	f.c.repo = f.pr.Base.Repo
	f.c.repo.DefaultBranch = "main"
	e := event{Repository: f.c.repo}
	if err := f.q.executeDev(context.Background(), "workflow_dispatch", e, f.pr.Number); err != nil {
		t.Fatal(err)
	}
	if got := f.api.reads[f.c.endpoint("pulls/7")]; got != 2 {
		t.Fatalf("PR read %d times, want the routing read and the final comparison only", got)
	}
	total := 0
	for _, n := range f.api.reads {
		total += n
	}
	if total != 32 {
		t.Fatalf("dev admission spent %d reads, want 32", total)
	}
	requireDevSuccess(t, f)
}

func requireDevSuccess(t *testing.T, f *queueFixture) {
	t.Helper()
	if latest := newestQueueCheck(f.api.checks); latest.Name != devCheckName || latest.Conclusion != "success" {
		t.Fatalf("dev admission did not publish success: %+v", latest)
	}
}
