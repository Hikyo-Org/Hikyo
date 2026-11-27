package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// laggingListAPI models GitHub's check-run listing trailing a write: after the
// first write, the next `lag` listings return the records as they were before
// it. Group job 43743778985 re-listed under a second after its POST and saw
// nothing, though the record (id 132902584288) existed.
type laggingListAPI struct {
	// mu serializes the wrapper: a pass reads from several goroutines, and the swap below is not atomic.
	mu    sync.Mutex
	inner *fakeAPI
	lag   int
	// stale is what lagging listings show; nil until armed by the first write.
	stale  []check
	armed  bool
	posts  int
	onPost func(*fakeAPI)
	// perWrite, when set, re-arms the lag on every write: write n is followed
	// by perWrite[n] listings that show the records as they were before it.
	perWrite []int
	writes   int
}

func (a *laggingListAPI) read(ctx context.Context, path string, result any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.armed && a.lag > 0 && strings.Contains(path, "/check-runs?") {
		a.lag--
		a.inner.mu.Lock()
		live := a.inner.checks
		a.inner.checks = a.stale
		a.inner.mu.Unlock()
		defer func() {
			a.inner.mu.Lock()
			a.inner.checks = live
			a.inner.mu.Unlock()
		}()
	}
	return a.inner.read(ctx, path, result)
}

func (a *laggingListAPI) write(ctx context.Context, method, path string, payload, result any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.perWrite != nil {
		a.armed, a.stale, a.lag = true, slices.Clone(a.inner.checks), 0
		if a.writes < len(a.perWrite) {
			a.lag = a.perWrite[a.writes]
		}
		a.writes++
	} else if !a.armed {
		a.armed, a.stale = true, slices.Clone(a.inner.checks)
	}
	if method == "POST" {
		a.posts++
	}
	if err := a.inner.write(ctx, method, path, payload, result); err != nil {
		return err
	}
	if method == "POST" && a.onPost != nil {
		a.onPost(a.inner)
	}
	return nil
}

func ensureBlockingOnEmptyHead(t *testing.T, f *queueFixture) error {
	t.Helper()
	session := &queueReconciliation{q: f.q}
	return session.ensureBlocking(context.Background(), errors.New("group proof metadata incomplete"))
}

func TestEnsureBlockingWaitsOutLaggingCheckListing(t *testing.T) {
	f := newQueueFixture(t)
	waits := recordReadWaits(t)
	api := &laggingListAPI{inner: f.api, lag: 2}
	f.c.api = api
	if err := ensureBlockingOnEmptyHead(t, f); err != nil {
		t.Fatalf("barrier written but listed late was not confirmed: %v", err)
	}
	if api.posts != 1 || len(f.api.writes) != 1 {
		t.Fatalf("POST count = %d, writes = %d; the write must happen exactly once", api.posts, len(f.api.writes))
	}
	if len(f.api.checks) != 1 || !blockingQueueCheck(&f.api.checks[0]) {
		t.Fatalf("blocking barrier missing: %+v", f.api.checks)
	}
	if want := writeConfirmBackoff[:2]; !slices.Equal(*waits, want) {
		t.Fatalf("waits = %v, want %v", *waits, want)
	}
}

func TestEnsureBlockingRefusesAfterBoundedLag(t *testing.T) {
	f := newQueueFixture(t)
	waits := recordReadWaits(t)
	api := &laggingListAPI{inner: f.api, lag: 1 << 20}
	f.c.api = api
	err := ensureBlockingOnEmptyHead(t, f)
	if err == nil || !strings.Contains(err.Error(), "could not be confirmed") {
		t.Fatalf("unconfirmed barrier was accepted: %v", err)
	}
	// A recurrence must be diagnosable from the log: the written record and what
	// the listing showed, with IDs, status and conclusion.
	for _, want := range []string{"written id=", `status="queued"`, "listed none"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error does not name %q: %v", want, err)
		}
	}
	if api.posts != 1 || len(f.api.writes) != 1 {
		t.Fatalf("an unconfirmed CREATE was retried: POSTs = %d, writes = %d", api.posts, len(f.api.writes))
	}
	if !slices.Equal(*waits, writeConfirmBackoff) {
		t.Fatalf("re-reads were not bounded: waits = %v, want %v", *waits, writeConfirmBackoff)
	}
	if len(f.api.checks) != 1 || !blockingQueueCheck(&f.api.checks[0]) {
		t.Fatalf("the written barrier must stay blocking: %+v", f.api.checks)
	}
}

func TestEnsureBlockingNeverConfirmsADifferentNewestRecord(t *testing.T) {
	f := newQueueFixture(t)
	waits := recordReadWaits(t)
	api := &laggingListAPI{inner: f.api}
	// A competing writer's newer owned record: its ID is not the written one.
	api.onPost = func(inner *fakeAPI) {
		other := inner.checks[len(inner.checks)-1]
		other.ID++
		inner.checks = append(inner.checks, other)
	}
	f.c.api = api
	err := ensureBlockingOnEmptyHead(t, f)
	if err == nil || !strings.Contains(err.Error(), "could not be confirmed") {
		t.Fatalf("a different newest record confirmed the barrier: %v", err)
	}
	if !strings.Contains(err.Error(), "listed id=") || strings.Contains(err.Error(), "listed none") {
		t.Fatalf("error does not name the different newest record: %v", err)
	}
	if len(*waits) != 0 {
		t.Fatalf("waited on a listing that is not behind: %v", *waits)
	}
}

func TestNewestAfterWriteFollowsTheWrittenStatus(t *testing.T) {
	record := func(f *queueFixture, status, conclusion string) check {
		v := check{ID: 7, Name: queueCheckName, HeadSHA: f.pr.Head.SHA, ExternalID: newBinding(f.pr.Base.Repo.ID, f.pr, nil).externalID(), Status: status, Conclusion: conclusion}
		v.App.ID, v.App.Slug = actionsAppID, "github-actions"
		return v
	}
	for _, tc := range []struct {
		name       string
		stale      [2]string
		lag, waits int
		done       bool
	}{
		{"visible at once", [2]string{"queued", ""}, 0, 0, true},
		{"completed status trails", [2]string{"queued", ""}, 2, 2, true},
		{"completed status never arrives", [2]string{"queued", ""}, 1 << 20, len(writeConfirmBackoff), false},
		// failOwnedBarrier PATCHes a record from completed/success to failure;
		// a view still showing the old verdict is behind, not settled.
		{"conclusion trails", [2]string{"completed", "success"}, 2, 2, true},
		{"conclusion never arrives", [2]string{"completed", "success"}, 1 << 20, len(writeConfirmBackoff), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQueueFixture(t)
			waits := recordReadWaits(t)
			written := record(f, "completed", "failure")
			f.api.checks = []check{written}
			f.c.api = &laggingListAPI{inner: f.api, lag: tc.lag, armed: true, stale: []check{record(f, tc.stale[0], tc.stale[1])}}
			newest, err := f.c.newestAfterWrite(context.Background(), &written, new(time.Duration))
			if err != nil || newest == nil || newest.ID != written.ID {
				t.Fatalf("newest = %+v, err = %v", newest, err)
			}
			if got := newest.Status == written.Status && newest.Conclusion == written.Conclusion; got != tc.done {
				t.Fatalf("settled = %t, want %t", got, tc.done)
			}
			if len(*waits) != tc.waits {
				t.Fatalf("waits = %v, want %d", *waits, tc.waits)
			}
		})
	}
}

func TestNewestAfterWriteStopsWhenTheWaitIsCancelled(t *testing.T) {
	f := newQueueFixture(t)
	old := readRetryWait
	readRetryWait = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	t.Cleanup(func() { readRetryWait = old })
	written := check{ID: 7, Status: "queued"}
	if _, err := f.c.newestAfterWrite(context.Background(), &written, new(time.Duration)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a spent deadline did not end the re-read: %v", err)
	}
}

// The route group job 43743778985 took: no barrier yet, proof metadata still
// lagging, so reconcile creates the barrier and must confirm it from a listing
// that trails the POST, inside the re-reading loop's own reserve.
func TestGroupColdStartBarrierSurvivesLaggingCheckListing(t *testing.T) {
	f := newQueueFixture(t)
	f.staleGroup()
	clock := newGroupClock(t)
	clock.before = func(int) { f.groupRefresh() }
	waits := recordReadWaits(t)
	api := &laggingListAPI{inner: f.api, lag: 2}
	f.c.api = api
	if err := f.group(); err != nil {
		t.Fatalf("cold-start barrier behind a lagging listing failed the job: %v", err)
	}
	if api.posts != 1 {
		// The verified terminal record updates the barrier; it is never a second CREATE.
		t.Fatalf("POSTs = %d, want only the barrier", api.posts)
	}
	if !slices.Equal(*waits, writeConfirmBackoff[:2]) {
		t.Fatalf("listing waits = %v, want %v", *waits, writeConfirmBackoff[:2])
	}
	f.assertNewest(t, "success", "Merge group validated", "")
}

// ensureBlocking creates the barrier and finishGroupFailure then fails it, each
// confirmed against a listing that trails the write. Both share one pass's
// confirmation budget, so together they may wait no longer than one full backoff.
func TestBarrierAndFailureConfirmationsShareOneWaitBudget(t *testing.T) {
	f := newQueueFixture(t)
	f.q.dco = func(context.Context, pull) (int, error) { return 0, errors.New("DCO check: empty commit range") }
	waits := recordReadWaits(t)
	f.c.api = &laggingListAPI{inner: f.api, perWrite: []int{2, 1 << 20}}
	err := f.group()
	if err == nil || !strings.Contains(err.Error(), "empty commit range") {
		t.Fatalf("group failure not reported: %v", err)
	}
	// The failure PATCH never settles, so the refusal must name both records.
	if !strings.Contains(err.Error(), "failure fallback is not the effective blocking record") || !strings.Contains(err.Error(), "written id=") || !strings.Contains(err.Error(), "listed id=") {
		t.Fatalf("failure-fallback refusal does not describe the records: %v", err)
	}
	var total time.Duration
	for _, d := range *waits {
		total += d
	}
	if total > writeConfirmTotal() || len(*waits) == 0 {
		t.Fatalf("confirmations waited %v (%v) under one reserve, budget %v", total, *waits, writeConfirmTotal())
	}
	if want := writeConfirmTotal(); want != 3*time.Second {
		t.Fatalf("budget %v no longer fits the 5 s reserve with the rest of the pass", want)
	}
}
