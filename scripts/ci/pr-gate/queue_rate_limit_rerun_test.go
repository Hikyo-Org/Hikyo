package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// spentWindow installs a stub gh that answers like GitHub does while the shared token's
// window is spent: a 403 whose body carries the status on stdout and whose stderr names
// the rate limit, and a rate_limit read whose reset is far beyond the job's budget.
// The tests below send their reads through the production reader (retryingAPI over
// ghAPI), so they fail when rate-limit detection is disabled.
type spentWindow struct {
	t    *testing.T
	flag string
}

func newSpentWindow(t *testing.T) *spentWindow {
	t.Helper()
	dir := t.TempDir()
	flag := filepath.Join(dir, "spent")
	if err := os.WriteFile(flag, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	reset := time.Now().Add(40 * time.Minute).Unix()
	fakeGH(t, fmt.Sprintf(`if [ "$2" = rate_limit ]; then echo '{"resources":{"core":{"remaining":0,"reset":%d},"graphql":{"remaining":900,"reset":%d}}}'; exit 0; fi
echo '{"message":"API rate limit exceeded for installation ID 1.","status":"403"}'
echo 'gh: API rate limit exceeded for installation ID 1. (HTTP 403)' >&2
exit 1`, reset, reset))
	t.Setenv("PR_GATE_DEADLINE", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))
	return &spentWindow{t: t, flag: flag}
}

func (w *spentWindow) spent() bool {
	_, err := os.Stat(w.flag)
	return err == nil
}

func (w *spentWindow) lift() {
	w.t.Helper()
	if err := os.Remove(w.flag); err != nil {
		w.t.Fatal(err)
	}
}

// read is a production read against the spent window: the error carries whatever
// classification the reader gave it.
func (w *spentWindow) read(ctx context.Context) error {
	var discard map[string]any
	return retryingGH.read(ctx, "repos/Hikyo-Org/Hikyo", &discard)
}

// limitedReads makes every read fail the way a spent window does, through the real
// reader, until lifted.
func (f *queueFixture) limitedReads(w *spentWindow) {
	f.probe().fault = func(ctx context.Context, _ string, _ int) error {
		if w.spent() {
			return w.read(ctx)
		}
		return nil
	}
}

// A rate-limited job whose reset is beyond its own budget fails and leaves the
// blocking check pending (#1553). The retry workflow reruns exactly that job after
// the reset; this pins that the rerun, with the same inputs and a fresh budget,
// completes the check, so the merge group never waits for its queue timeout.
func TestRateLimitedGroupJobCompletesItsCheckWhenRerunAfterTheReset(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	w := newSpentWindow(t)
	f.limitedReads(w)
	err := f.group()
	if err == nil {
		t.Fatal("a job that cannot read inside its budget must fail so the retry can find it")
	}
	if !rateLimitedFailure(err) {
		t.Fatalf("the failure must be recognized as the rate limit: %v", err)
	}
	f.assertNoSuccess(t)
	f.assertNoTerminalWrites(t)
	w.lift()
	if err := f.group(); err != nil {
		t.Fatalf("the rerun after the reset failed: %v", err)
	}
	f.assertNewest(t, "success", "Merge group validated", "")
}

// The #1514 path: a merge-group callback (workflow_run of trusted-merge-ci) whose group
// job is rate-limited in initRepository, the very first read. Nothing is written
// (the repository id is unknown, so no check can be owned), no later event exists, and
// the rerun of that same job after the reset must complete the check.
func TestGroupJobRateLimitedInInitRepositoryCompletesOnRerun(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	w := newSpentWindow(t)
	writes := len(f.api.writes)
	f.probe().fault = func(ctx context.Context, path string, _ int) error {
		if w.spent() && path == "repos/"+f.c.repoName {
			return w.read(ctx)
		}
		return nil
	}
	err := f.group()
	if err == nil {
		t.Fatal("a group job that cannot read the repository must fail so the retry can find it")
	}
	if !rateLimitedFailure(err) {
		t.Fatalf("the failure must be recognized as the rate limit: %v", err)
	}
	if len(f.api.writes) != writes {
		t.Fatalf("a job limited in initRepository must write nothing: %+v", f.api.writes[writes:])
	}
	if latest := newestQueueCheck(f.api.checks); latest == nil || latest.Status == "completed" {
		t.Fatalf("the check must stay pending for the rerun: %+v", latest)
	}
	w.lift()
	if err := f.group(); err != nil {
		t.Fatalf("the rerun after the reset failed: %v", err)
	}
	f.assertNewest(t, "success", "Merge group validated", "")
}

// A publisher on its last attempt that hits the spent window through the real reader must
// leave a blocking, pending check, never the terminal refusal; the later rerun admits.
func TestRateLimitedPublishJobCompletesItsCheckWhenRerunAfterTheReset(t *testing.T) {
	t.Setenv("GITHUB_RUN_ATTEMPT", strconv.Itoa(admissionMaxAttempts))
	f := newQueueFixture(t)
	w := newSpentWindow(t)
	verify := f.c.verify
	f.c.verify = func(ctx context.Context, number int, sha string) (policyDecision, error) {
		if w.spent() {
			return policyDecision{}, fmt.Errorf("verify: %w", w.read(ctx))
		}
		return verify(ctx, number, sha)
	}
	b := admissionBudget{Started: time.Now(), Deadline: time.Now().Add(admissionReserve + 2*time.Second)}
	if err := f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), b); err == nil {
		t.Fatal("a publisher that cannot read inside its budget must fail so the retry can find it")
	}
	f.assertNoSuccess(t)
	for _, write := range f.api.writes {
		if write["status"] == "completed" {
			t.Fatalf("the final attempt published a terminal verdict for a rate limit: %+v", write)
		}
	}
	w.lift()
	if err := f.admit(); err != nil {
		t.Fatalf("the rerun after the reset failed: %v", err)
	}
	if got := f.newestOnHead(); got.Conclusion != "success" {
		t.Fatalf("the rerun must publish the head's admitted check, got %+v", got)
	}
}
