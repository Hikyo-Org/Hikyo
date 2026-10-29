package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// groupClock replaces the re-read clock and timer: each wait advances time by
// exactly the requested interval and runs before, so tests can change the fake
// API between iterations without sleeping.
type groupClock struct {
	now       time.Time
	waits     []time.Duration
	deadlines []time.Time // deadline of each wait's context (zero when none)
	before    func(wait int)
}

func (c *groupClock) install() func() {
	oldNow, oldWait := groupRereadNow, groupRereadWait
	groupRereadNow = func() time.Time { return c.now }
	groupRereadWait = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.waits = append(c.waits, d)
		deadline, _ := ctx.Deadline()
		c.deadlines = append(c.deadlines, deadline)
		c.now = c.now.Add(d)
		if c.before != nil {
			c.before(len(c.waits))
		}
		return nil
	}
	return func() { groupRereadNow, groupRereadWait = oldNow, oldWait }
}

func newGroupClock(t *testing.T) *groupClock {
	t.Helper()
	c := &groupClock{now: time.Unix(1_800_000_000, 0)}
	t.Cleanup(c.install())
	return c
}

func (f *queueFixture) proofJob(id int64, name, status, conclusion string) job {
	r := f.groupRun
	return job{ID: id, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: name, Status: status, Conclusion: conclusion}
}

func (f *queueFixture) groupJobs(jobs ...job) {
	r := f.groupRun
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", r.ID, r.Attempt))] = map[string]any{"total_count": len(jobs), "jobs": jobs}
}

func (f *queueFixture) groupJobsPath() string {
	return f.c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", f.groupRun.ID, f.groupRun.Attempt))
}

// staleGroup reproduces the incident: the run is completed and successful while
// the jobs API still lists merge-group-proof as running.
func (f *queueFixture) staleGroup() {
	f.groupJobs(f.proofJob(203, "validation / ci-required", "completed", "success"), f.proofJob(204, "merge-group-proof", "in_progress", ""))
}

// pendingGroup establishes the pending barrier a running group would hold.
func (f *queueFixture) pendingGroup(t *testing.T) {
	t.Helper()
	f.groupRun.Status, f.groupRun.Conclusion = "in_progress", ""
	f.groupRefresh()
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	if latest := newestQueueCheck(f.api.checks); latest == nil || latest.Status != "queued" {
		t.Fatal("running group did not establish its pending barrier")
	}
	f.groupRun.Status, f.groupRun.Conclusion = "completed", "success"
	f.groupRefresh()
}

func (f *queueFixture) assertNoTerminalWrites(t *testing.T) {
	t.Helper()
	for _, w := range f.api.writes {
		if w["status"] == "completed" {
			t.Fatalf("terminal verdict was written: %+v", w)
		}
	}
	if latest := newestQueueCheck(f.api.checks); latest == nil || !blockingQueueCheck(latest) || latest.Status == "completed" {
		t.Fatalf("record is not left pending: %+v", latest)
	}
}

func (f *queueFixture) assertNewest(t *testing.T, conclusion, title, summary string) {
	t.Helper()
	latest := newestQueueCheck(f.api.checks)
	if latest == nil || latest.Status != "completed" || latest.Conclusion != conclusion || latest.Output.Title != title || !strings.Contains(latest.Output.Summary, summary) {
		t.Fatalf("newest record = %+v, want %s %q containing %q", latest, conclusion, title, summary)
	}
}

const (
	metadataExpiredTitle   = "Merge group proof metadata unavailable"
	metadataExpiredSummary = "GitHub job metadata stayed incomplete or unreadable after a successful merge-group run, through bounded re-reads or a rate limit; re-queue the PR."
	unsafeProofSummary     = "Current proof could not be safely established"
	failedProofSummary     = "Required merge-group validation failed"
)

// expectedWaits is the number of waits before the pass that finds no room for
// another interval, which is the last one: the window's final decision.
const expectedWaits = int(groupRereadWindow/groupRereadInterval) - 1

// persistentFailureWaits is the schedule for a transient read failure that never
// clears: full passes start at 0, 20, 60, 140 and 260 s. The pass at 260 s finds
// no room for the next 120 s wait (260+120 >= 360), so it is the final decision.
var persistentFailureWaits = []time.Duration{20 * time.Second, 40 * time.Second, 80 * time.Second, 120 * time.Second}

func assertPersistentFailureWaits(t *testing.T, waits []time.Duration) {
	t.Helper()
	if !slices.Equal(waits, persistentFailureWaits) {
		t.Fatalf("waits = %v, want %v", waits, persistentFailureWaits)
	}
}

// groupProbe wraps the fake API to inject read faults and to observe the
// contexts handed to reads and writes. Reads may run in parallel.
type groupProbe struct {
	api
	mu      sync.Mutex
	counts  map[string]int
	fault   func(ctx context.Context, path string, n int) error
	onRead  func(ctx context.Context, path string)
	onWrite func(ctx context.Context, method, path string)
}

func (p *groupProbe) read(ctx context.Context, path string, result any) error {
	p.mu.Lock()
	p.counts[path]++
	n := p.counts[path]
	p.mu.Unlock()
	if p.onRead != nil {
		p.onRead(ctx, path)
	}
	if p.fault != nil {
		if err := p.fault(ctx, path, n); err != nil {
			return err
		}
	}
	return p.api.read(ctx, path, result)
}

func (p *groupProbe) write(ctx context.Context, method, path string, payload, result any) error {
	if p.onWrite != nil {
		p.onWrite(ctx, method, path)
	}
	return p.api.write(ctx, method, path, payload, result)
}

func (f *queueFixture) probe() *groupProbe {
	p := &groupProbe{api: f.api, counts: make(map[string]int)}
	f.c.api = p
	return p
}

func (f *queueFixture) workflowPath() string {
	return f.c.endpoint("actions/workflows/ci-merge-group.yml")
}

func (f *queueFixture) checksPath() string {
	return f.c.endpoint(fmt.Sprintf("commits/%s/check-runs?check_name=%s&filter=all&per_page=100&page=1", f.groupRun.HeadSHA, queueCheckName))
}

func (f *queueFixture) totalReads() int {
	total := 0
	for _, n := range f.api.reads {
		total += n
	}
	return total
}

func (f *queueFixture) healthyJobs() []job {
	return []job{f.proofJob(203, "validation / ci-required", "completed", "success"), f.proofJob(204, "merge-group-proof", "completed", "success")}
}

func TestGroupRereadWaitsForStaleProofJobThenValidates(t *testing.T) {
	for _, seeded := range []bool{true, false} {
		t.Run(fmt.Sprintf("existing pending barrier=%t", seeded), func(t *testing.T) {
			f := newQueueFixture(t)
			if seeded {
				f.pendingGroup(t)
			}
			f.staleGroup()
			clock := newGroupClock(t)
			written := -1
			clock.before = func(wait int) {
				for _, w := range f.api.writes {
					if w["status"] == "completed" {
						t.Errorf("intermediate terminal write at wait %d: %+v", wait, w)
					}
				}
				if latest := newestQueueCheck(f.api.checks); latest == nil || latest.Status != "queued" {
					t.Errorf("no pending barrier while waiting: %+v", latest)
				}
				// The first pass may refresh a changed source binding; later passes
				// must leave the record alone.
				if wait > 2 && len(f.api.writes) != written {
					t.Errorf("wait %d wrote an intermediate update", wait)
				}
				written = len(f.api.writes)
				if wait == 3 {
					f.groupRefresh()
				}
			}
			if err := f.group(); err != nil {
				t.Fatalf("lagging metadata was not waited out: %v", err)
			}
			if len(clock.waits) != 3 || clock.waits[0] != 20*time.Second {
				t.Fatalf("waits = %v, want three 20s waits", clock.waits)
			}
			f.assertNewest(t, "success", "Merge group validated", "authenticated queue membership")
		})
	}
}

func TestGroupRereadWaitsForMissingProofJob(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.groupJobs(f.proofJob(203, "validation / ci-required", "completed", "success"))
	clock := newGroupClock(t)
	clock.before = func(int) { f.groupRefresh() }
	if err := f.group(); err != nil {
		t.Fatalf("not-yet-listed proof job was not waited out: %v", err)
	}
	if len(clock.waits) != 1 {
		t.Fatalf("waits = %v", clock.waits)
	}
	f.assertNewest(t, "success", "Merge group validated", "")
}

func TestGroupRereadWorkerLoopsOnlyForGroupKind(t *testing.T) {
	t.Run("group", func(t *testing.T) {
		f := newQueueFixture(t)
		f.pendingGroup(t)
		f.staleGroup()
		clock := newGroupClock(t)
		clock.before = func(int) { f.groupRefresh() }
		e := f.event
		e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"group"}]`, f.groupRun.HeadSHA)
		if err := f.q.runHead(context.Background(), "workflow_dispatch", e, "group"); err != nil {
			t.Fatalf("group head worker did not wait out lagging metadata: %v", err)
		}
		if len(clock.waits) != 1 {
			t.Fatalf("waits = %v", clock.waits)
		}
		f.assertNewest(t, "success", "Merge group validated", "")
	})
	// The same curable failure, on workers whose proof nothing could cure by
	// waiting, must end after one pass.
	for _, tc := range []struct {
		kind   string
		breaks func(*queueFixture)
	}{
		{"admission", func(f *queueFixture) {
			path := f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1")
			f.api.responses[path] = &githubReadError{path: path, status: 503}
		}},
		{"ambiguous", func(f *queueFixture) {
			path := f.c.endpoint("pulls?state=open&per_page=100&page=1")
			f.api.responses[path] = &githubReadError{path: path, status: 503}
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			f := newQueueFixture(t)
			tc.breaks(f)
			clock := newGroupClock(t)
			e := f.event
			e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":%q}]`, f.pr.Head.SHA, tc.kind)
			err := f.q.runHead(context.Background(), "workflow_dispatch", e, tc.kind)
			if err == nil || !deferrableGroupRead(err) {
				t.Fatalf("fixture must fail with a curable read (else the test proves nothing): %v", err)
			}
			if len(clock.waits) != 0 {
				t.Fatalf("%s worker waited: %v", tc.kind, clock.waits)
			}
			f.assertNoTerminalWrites(t)
		})
	}
}

func TestGroupRereadStaleProofPastWindowPublishesDistinctTerminalRefusal(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	clock := newGroupClock(t)
	workflowReads := f.api.reads[f.workflowPath()]
	clock.before = func(wait int) {
		for _, w := range f.api.writes {
			if w["status"] == "completed" {
				t.Errorf("terminal write before the window closed (wait %d)", wait)
			}
		}
	}
	err := f.group()
	var incomplete *groupProofIncomplete
	if !errors.As(err, &incomplete) {
		t.Fatalf("expired window must still report the cause: %v", err)
	}
	if len(clock.waits) != expectedWaits {
		t.Fatalf("waited %d times, want %d", len(clock.waits), expectedWaits)
	}
	f.assertNewest(t, "failure", metadataExpiredTitle, metadataExpiredSummary)
	f.assertNoSuccess(t)
	// One pass to find the lag, then polling only, then the single closing pass.
	if got := f.api.reads[f.workflowPath()] - workflowReads; got != 2 {
		t.Fatalf("full passes = %d, want the first and the closing one", got)
	}
}

func TestGroupRereadTransientJobsReadThenSucceeds(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.api.responses[f.groupJobsPath()] = &githubReadError{path: f.groupJobsPath(), status: 502}
	clock := newGroupClock(t)
	clock.before = func(wait int) {
		if wait == 2 {
			f.groupRefresh()
		}
	}
	if err := f.group(); err != nil {
		t.Fatalf("transient jobs read was not waited out: %v", err)
	}
	if len(clock.waits) != 2 {
		t.Fatalf("waits = %v", clock.waits)
	}
	f.assertNewest(t, "success", "Merge group validated", "")
}

func TestGroupRereadPersistentTransientReadPublishesDistinctRefusal(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.api.responses[f.groupJobsPath()] = &githubReadError{path: f.groupJobsPath(), status: 502}
	clock := newGroupClock(t)
	started := clock.now
	err := f.group()
	var failure *githubReadError
	if !errors.As(err, &failure) || failure.status != 502 {
		t.Fatalf("expired window must still report the read failure: %v", err)
	}
	assertPersistentFailureWaits(t, clock.waits)
	f.assertNewest(t, "failure", metadataExpiredTitle, metadataExpiredSummary)
	if strings.Contains(newestQueueCheck(f.api.checks).Output.Summary, unsafeProofSummary) {
		t.Fatal("transient exhaustion reused the generic unsafe-proof wording")
	}
	// The pass that cannot be followed by another wait is the final decision:
	// no extra pass starts after the window, and none is made just to expire.
	if passes := f.api.reads[f.groupJobsPath()]; passes != len(persistentFailureWaits)+1 {
		t.Fatalf("passes = %d, want %d", passes, len(persistentFailureWaits)+1)
	}
	if elapsed := clock.now.Sub(started); elapsed != 260*time.Second {
		t.Fatalf("last pass ran at %s, want 260s", elapsed)
	}
}

func TestGroupRereadExpiryReusesEarlierProvenIdentity(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	clock := newGroupClock(t)
	workflowPath := f.c.endpoint("actions/workflows/ci-merge-group.yml")
	// The first pass reaches the jobs read; every later pass fails before it can
	// re-prove the group. Expiry may still refuse, after re-verifying the first
	// pass's identity against live state.
	clock.before = func(int) { f.api.responses[workflowPath] = &githubReadError{path: workflowPath, status: 504} }
	err := f.group()
	var failure *githubReadError
	if !errors.As(err, &failure) || failure.status != 504 {
		t.Fatalf("expired window must still report the read failure: %v", err)
	}
	if len(clock.waits) != expectedWaits {
		t.Fatalf("waited %d times, want %d", len(clock.waits), expectedWaits)
	}
	f.assertNewest(t, "failure", metadataExpiredTitle, metadataExpiredSummary)
}

func TestGroupRereadDefinitiveProofFailuresDoNotWait(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*queueFixture)
		summary string
	}{
		{"failed proof job", func(f *queueFixture) {
			f.groupJobs(f.proofJob(203, "validation / ci-required", "completed", "success"), f.proofJob(204, "merge-group-proof", "completed", "failure"))
		}, failedProofSummary},
		{"failed aggregate beside lagging proof", func(f *queueFixture) {
			f.groupJobs(f.proofJob(203, "validation / ci-required", "completed", "failure"), f.proofJob(204, "merge-group-proof", "in_progress", ""))
		}, failedProofSummary},
		{"cancelled proof job", func(f *queueFixture) {
			f.groupJobs(f.proofJob(203, "validation / ci-required", "completed", "success"), f.proofJob(204, "merge-group-proof", "completed", "cancelled"))
		}, failedProofSummary},
		{"proof job for another head", func(f *queueFixture) {
			other := f.proofJob(204, "merge-group-proof", "in_progress", "")
			other.HeadSHA = strings.Repeat("e", 40)
			f.groupJobs(f.proofJob(203, "validation / ci-required", "completed", "success"), other)
		}, failedProofSummary},
		{"unsuccessful run with lagging proof", func(f *queueFixture) {
			f.groupRun.Conclusion = "failure"
			f.groupRefresh()
			f.staleGroup()
		}, failedProofSummary},
		{"duplicated proof job", func(f *queueFixture) {
			f.groupJobs(f.proofJob(203, "validation / ci-required", "completed", "success"), f.proofJob(204, "merge-group-proof", "completed", "success"), f.proofJob(205, "merge-group-proof", "in_progress", ""))
		}, unsafeProofSummary},
		{"duplicated aggregate with missing proof", func(f *queueFixture) {
			f.groupJobs(f.proofJob(203, "validation / ci-required", "completed", "success"), f.proofJob(205, "validation / ci-required", "completed", "success"))
		}, unsafeProofSummary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQueueFixture(t)
			f.pendingGroup(t)
			tc.prepare(f)
			clock := newGroupClock(t)
			if err := f.group(); err == nil && tc.summary == unsafeProofSummary {
				t.Fatal("ambiguous proof was accepted")
			}
			if len(clock.waits) != 0 {
				t.Fatalf("definitive outcome waited: %v", clock.waits)
			}
			f.assertNewest(t, "failure", "PR validation refused", tc.summary)
			f.assertNoSuccess(t)
		})
	}
}

func TestGroupRereadNeverRunsForNonGroupInvocations(t *testing.T) {
	feedbackJobs := func(f *queueFixture) {
		path := f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1")
		f.api.responses[path] = &githubReadError{path: path, status: 503}
	}
	for _, tc := range []struct {
		name  string
		setup func(*queueFixture)
		run   func(*queueFixture) error
	}{
		{"targeted admission event", feedbackJobs, directSnapshotAdmission},
		{"targeted admission worker", feedbackJobs, snapshotAdmission},
		{"group-only repair dispatch", func(f *queueFixture) {
			f.pendingGroup(t)
			f.staleGroup()
			f.q.repairOnly = true
		}, func(f *queueFixture) error {
			f.c.expectedHead = f.groupRun.HeadSHA
			e := f.event
			e.Inputs.PR = "7"
			return f.q.execute(context.Background(), "workflow_dispatch", e)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQueueFixture(t)
			tc.setup(f)
			clock := newGroupClock(t)
			err := tc.run(f)
			if err == nil || !deferrableGroupRead(err) {
				t.Fatalf("fixture must fail with a curable error (else the test proves nothing): %v", err)
			}
			if len(clock.waits) != 0 {
				t.Fatalf("non-group invocation waited: %v", clock.waits)
			}
			f.assertNoTerminalWrites(t)
		})
	}
}

// One predicate decides for both entry points (the group callback and the
// group head worker); the entry points only say whether the work is group-kind.
func TestGroupRereadPredicate(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		isGroup, targeted    bool
		repairOnly, wantWait bool
	}{
		{"group work", true, false, false, true},
		{"group work under targeted admission", true, true, false, false},
		{"group-only repair", true, false, true, false},
		{"other work", false, false, false, false},
	} {
		q := &queueController{targeted: tc.targeted, repairOnly: tc.repairOnly}
		if got := q.rereadsGroup(tc.isGroup); got != tc.wantWait {
			t.Errorf("%s: rereadsGroup = %t, want %t", tc.name, got, tc.wantWait)
		}
	}
}

func TestGroupRereadRefusesIdentityChangeBetweenPasses(t *testing.T) {
	for name, move := range map[string]func(*queueFixture){
		"queue membership": func(f *queueFixture) { f.entry.ID = "REPLACED_ENTRY" },
		"run attempt": func(f *queueFixture) {
			// A superseded attempt keeps its final job listing on GitHub.
			f.groupJobs(f.healthyJobs()...)
			f.groupRun.Attempt++
		},
		"branch": func(f *queueFixture) { f.groupRun.HeadBranch += "-replacement" },
		"policy": func(f *queueFixture) { f.owner["updated_at"] = "changed"; f.policyRefresh() },
	} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			f.pendingGroup(t)
			f.staleGroup()
			clock := newGroupClock(t)
			// The next pass is fully healthy and self-consistent, but it is not the
			// identity the earlier pass observed.
			clock.before = func(int) { move(f); f.groupRefresh() }
			if err := f.group(); err == nil {
				t.Fatal("identity change between re-reads was accepted")
			}
			if len(clock.waits) != 1 {
				t.Fatalf("waits = %v, want the move to end the loop", clock.waits)
			}
			f.assertNoSuccess(t)
			f.assertNoTerminalWrites(t)
		})
	}
}

func TestGroupRereadRevalidatesIdentityOnEveryPass(t *testing.T) {
	t.Run("queue moves during the pass that would validate", func(t *testing.T) {
		f := newQueueFixture(t)
		f.pendingGroup(t)
		f.staleGroup()
		clock := newGroupClock(t)
		moved := false
		clock.before = func(int) { f.groupRefresh() }
		// Reads of the jobs listing: the first pass, one poll, then the full pass
		// that would validate.
		f.api.readHook = func(path string, n int) {
			if path == f.groupJobsPath() && n == 3 && !moved {
				moved = true
				f.entry.ID = "REPLACED_ENTRY"
				f.groupRefresh()
			}
		}
		if err := f.group(); err == nil || !moved {
			t.Fatalf("queue movement before publication was accepted (moved=%t): %v", moved, err)
		}
		f.assertNoSuccess(t)
	})
	t.Run("identity moves before the terminal refusal", func(t *testing.T) {
		f := newQueueFixture(t)
		f.pendingGroup(t)
		f.staleGroup()
		newGroupClock(t)
		moved := false
		// The closing full pass is read number expectedWaits+1: first pass, one poll
		// per wait but the last (the closing pass reads the listing itself), then
		// the pass that reaches the expiry decision.
		f.api.readHook = func(path string, n int) {
			if path == f.groupJobsPath() && n == expectedWaits+1 && !moved {
				moved = true
				f.entry.ID = "REPLACED_ENTRY"
				f.groupRefresh()
				f.staleGroup()
			}
		}
		err := f.group()
		if err == nil || !moved || !strings.Contains(err.Error(), "identity moved before failure publication") {
			t.Fatalf("refusal was published for a moved identity (moved=%t): %v", moved, err)
		}
		f.assertNoTerminalWrites(t)
	})
}

func TestDeferrableGroupRead(t *testing.T) {
	status := func(code int) error { return &githubReadError{path: "p", status: code} }
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"incomplete proof", &groupProofIncomplete{jobs: []string{"merge-group-proof (in_progress)"}}, true},
		{"wrapped incomplete proof", fmt.Errorf("outer: %w", &groupProofIncomplete{}), true},
		{"bad gateway", status(502), true},
		{"rate limit", status(429), true},
		{"not found", status(404), false},
		{"plain failure", errors.New("merge group ref was deleted or rebuilt"), false},
		{"GraphQL rejection", fmt.Errorf("boom: %w", &githubReadError{path: "p", rejected: true}), false},
		{"transient beside permanent", errors.Join(status(502), errors.New("permanent")), false},
		{"transient beside write failure", errors.Join(status(503), errors.New("GitHub PATCH failed; no mutation retry attempted")), false},
		{"two transients", errors.Join(status(502), status(504)), true},
		{"transient with sibling cancellation", errors.Join(status(502), context.Canceled), true},
		{"transient with cancelled sibling read", errors.Join(status(502), &githubReadError{path: "q", cause: context.Canceled}), true},
		{"cancellation alone", context.Canceled, false},
		{"deadline", errors.Join(status(502), context.DeadlineExceeded), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := deferrableGroupRead(tc.err); got != tc.want {
				t.Fatalf("deferrableGroupRead(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

func TestGroupRereadPollsJobsAloneWhileProofLags(t *testing.T) {
	const lag = 10
	healthy := newQueueFixture(t)
	healthy.pendingGroup(t)
	before := healthy.totalReads()
	if err := healthy.group(); err != nil {
		t.Fatal(err)
	}
	fullPass := healthy.totalReads() - before

	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	clock := newGroupClock(t)
	clock.before = func(wait int) {
		if wait == lag {
			f.groupRefresh()
		}
	}
	reads, jobs, workflow := f.totalReads(), f.api.reads[f.groupJobsPath()], f.api.reads[f.workflowPath()]
	if err := f.group(); err != nil {
		t.Fatalf("lag was not waited out: %v", err)
	}
	f.assertNewest(t, "success", "Merge group validated", "")
	if len(clock.waits) != lag {
		t.Fatalf("waits = %d, want %d", len(clock.waits), lag)
	}
	// First pass, one jobs read per wait, then one closing pass.
	if got := f.api.reads[f.groupJobsPath()] - jobs; got != lag+2 {
		t.Fatalf("jobs listing reads = %d, want %d", got, lag+2)
	}
	if got := f.api.reads[f.workflowPath()] - workflow; got != 2 {
		t.Fatalf("full passes = %d, want 2", got)
	}
	// Repeating full passes would cost (lag+1) passes; polling costs about two.
	if total := f.totalReads() - reads; total > 2*fullPass+lag+3 {
		t.Fatalf("a lag of %d polls cost %d reads; one full pass costs %d", lag, total, fullPass)
	}
}

func TestGroupRereadLagThenDefiniteOutcomeRefusesWithoutFurtherWaiting(t *testing.T) {
	for _, tc := range []struct {
		name    string
		jobs    func(*queueFixture) []job
		summary string
	}{
		{"failed proof job", func(f *queueFixture) []job {
			return []job{f.proofJob(203, "validation / ci-required", "completed", "success"), f.proofJob(204, "merge-group-proof", "completed", "failure")}
		}, failedProofSummary},
		{"cancelled proof job", func(f *queueFixture) []job {
			return []job{f.proofJob(203, "validation / ci-required", "completed", "cancelled"), f.proofJob(204, "merge-group-proof", "in_progress", "")}
		}, failedProofSummary},
		{"duplicated proof job", func(f *queueFixture) []job {
			return []job{f.proofJob(203, "validation / ci-required", "completed", "success"), f.proofJob(204, "merge-group-proof", "in_progress", ""), f.proofJob(205, "merge-group-proof", "queued", "")}
		}, unsafeProofSummary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQueueFixture(t)
			f.pendingGroup(t)
			f.staleGroup()
			clock := newGroupClock(t)
			clock.before = func(wait int) {
				if wait == 2 {
					f.groupJobs(tc.jobs(f)...)
				}
			}
			workflow := f.api.reads[f.workflowPath()]
			_ = f.group()
			if len(clock.waits) != 2 {
				t.Fatalf("waits = %v, want the definite listing to end the wait on the poll that saw it", clock.waits)
			}
			f.assertNewest(t, "failure", "PR validation refused", tc.summary)
			f.assertNoSuccess(t)
			if got := f.api.reads[f.workflowPath()] - workflow; got != 2 {
				t.Fatalf("full passes = %d, want the first and one closing pass", got)
			}
		})
	}
}

func TestGroupRereadPollReadProblems(t *testing.T) {
	t.Run("transient failure keeps polling", func(t *testing.T) {
		f := newQueueFixture(t)
		f.pendingGroup(t)
		f.staleGroup()
		probe := f.probe()
		probe.fault = func(_ context.Context, path string, n int) error {
			if path == f.groupJobsPath() && n == 2 {
				return &githubReadError{path: path, status: 502}
			}
			return nil
		}
		clock := newGroupClock(t)
		clock.before = func(wait int) {
			if wait == 3 {
				f.groupRefresh()
			}
		}
		workflow := f.api.reads[f.workflowPath()]
		if err := f.group(); err != nil {
			t.Fatal(err)
		}
		if len(clock.waits) != 3 || f.api.reads[f.workflowPath()]-workflow != 2 {
			t.Fatalf("waits=%v full passes=%d, want the failed poll to cost one interval, not a pass", clock.waits, f.api.reads[f.workflowPath()]-workflow)
		}
		f.assertNewest(t, "success", "Merge group validated", "")
	})
	for name, response := range map[string]any{
		"permanent failure":        &githubReadError{status: 404},
		"identity-invalid listing": map[string]any{"total_count": 1, "jobs": []job{{ID: 9, RunID: 999, Attempt: 1, Name: "merge-group-proof", Status: "in_progress"}}},
	} {
		t.Run(name+" goes to a full pass", func(t *testing.T) {
			f := newQueueFixture(t)
			f.pendingGroup(t)
			f.staleGroup()
			clock := newGroupClock(t)
			workflow := f.api.reads[f.workflowPath()]
			clock.before = func(int) { f.api.responses[f.groupJobsPath()] = response }
			_ = f.group()
			if len(clock.waits) != 1 || f.api.reads[f.workflowPath()]-workflow != 2 {
				t.Fatalf("waits=%v full passes=%d, want the poll to hand over to the full pass at once", clock.waits, f.api.reads[f.workflowPath()]-workflow)
			}
			f.assertNewest(t, "failure", "PR validation refused", unsafeProofSummary)
			f.assertNoSuccess(t)
		})
	}
}

func TestGroupRereadBarrierReadFailureDuringWait(t *testing.T) {
	// The barrier check is the first check-runs read after the jobs listing.
	breakBarrierRead := func(f *queueFixture, failure error) {
		probe := f.probe()
		jobsSeen, failed := false, false
		probe.onRead = func(_ context.Context, path string) {
			if path == f.groupJobsPath() {
				jobsSeen = true
			}
		}
		probe.fault = func(_ context.Context, path string, _ int) error {
			if path == f.checksPath() && jobsSeen && !failed {
				failed = true
				return failure
			}
			return nil
		}
	}
	t.Run("transient failure continues the loop", func(t *testing.T) {
		f := newQueueFixture(t)
		f.pendingGroup(t)
		f.staleGroup()
		breakBarrierRead(f, &githubReadError{path: "checks", status: 502})
		clock := newGroupClock(t)
		clock.before = func(wait int) {
			if wait == 3 {
				f.groupRefresh()
			}
		}
		workflow := f.api.reads[f.workflowPath()]
		if err := f.group(); err != nil {
			t.Fatalf("a transient barrier read ended the loop: %v", err)
		}
		// Pass one could not confirm its barrier, so pass two (not a poll) makes
		// sure of it; then two polls and the closing pass.
		if len(clock.waits) != 3 || f.api.reads[f.workflowPath()]-workflow != 3 {
			t.Fatalf("waits=%v full passes=%d", clock.waits, f.api.reads[f.workflowPath()]-workflow)
		}
		f.assertNewest(t, "success", "Merge group validated", "")
	})
	t.Run("permanent failure ends the loop", func(t *testing.T) {
		f := newQueueFixture(t)
		f.pendingGroup(t)
		f.staleGroup()
		breakBarrierRead(f, &githubReadError{path: "checks", status: 403})
		clock := newGroupClock(t)
		err := f.group()
		var failure *githubReadError
		if !errors.As(err, &failure) || failure.status != 403 {
			t.Fatalf("err = %v", err)
		}
		if len(clock.waits) != 0 {
			t.Fatalf("waits = %v", clock.waits)
		}
		f.assertNoTerminalWrites(t)
	})
}

// Pre-existing rule: a terminal refusal needs a finished-group identity that
// this invocation proved. Without one the record stays pending and blocking
// until the queue's own timeout.
func TestGroupRereadExpiryWithoutProvenIdentityLeavesRecordPending(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.api.responses[f.workflowPath()] = &githubReadError{path: f.workflowPath(), status: 504}
	clock := newGroupClock(t)
	err := f.group()
	var failure *githubReadError
	if !errors.As(err, &failure) || failure.status != 504 {
		t.Fatalf("err = %v", err)
	}
	assertPersistentFailureWaits(t, clock.waits)
	f.assertNoTerminalWrites(t)
}

func TestGroupRereadSecondInvocationAfterExpiryDoesNotWait(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	first := newGroupClock(t)
	_ = f.group()
	if len(first.waits) != expectedWaits {
		t.Fatalf("setup: first invocation waited %d times", len(first.waits))
	}
	f.assertNewest(t, "failure", metadataExpiredTitle, metadataExpiredSummary)

	second := newGroupClock(t)
	writes := len(f.api.writes)
	err := f.group()
	var incomplete *groupProofIncomplete
	if !errors.As(err, &incomplete) {
		t.Fatalf("second invocation lost the cause: %v", err)
	}
	if len(second.waits) != 0 {
		t.Fatalf("second invocation waited: %v", second.waits)
	}
	if len(f.api.writes) != writes {
		t.Fatalf("second invocation wrote: %+v", f.api.writes[writes:])
	}
	f.assertNewest(t, "failure", metadataExpiredTitle, metadataExpiredSummary)
}

// The settled record must belong to this run: a re-run (new attempt) of the
// same group head still waits even though an earlier attempt's refusal is newest.
func TestGroupRereadStillWaitsWhenCompletedRecordBelongsToAnotherAttempt(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.groupJobs(f.proofJob(203, "validation / ci-required", "completed", "success"), f.proofJob(204, "merge-group-proof", "completed", "failure"))
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	f.assertNewest(t, "failure", "PR validation refused", failedProofSummary)

	f.groupRun.Attempt = 2
	f.groupRefresh()
	f.staleGroup()
	clock := newGroupClock(t)
	_ = f.group()
	if len(clock.waits) != expectedWaits {
		t.Fatalf("re-run waited %d times, want %d", len(clock.waits), expectedWaits)
	}
}

func TestGroupRereadFencesLoopedWork(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	probe := f.probe()
	var mu sync.Mutex
	var readDeadlines []time.Time
	probe.onRead = func(ctx context.Context, path string) {
		// Pass reads only: barrier reads run under the separate reserve.
		if path != f.workflowPath() && path != f.groupJobsPath() {
			return
		}
		deadline, ok := ctx.Deadline()
		mu.Lock()
		defer mu.Unlock()
		if !ok {
			deadline = time.Time{}
		}
		readDeadlines = append(readDeadlines, deadline)
	}
	clock := newGroupClock(t)
	start := time.Now()
	_ = f.group()
	end := time.Now()
	if len(readDeadlines) == 0 || len(clock.deadlines) == 0 {
		t.Fatal("no reads or waits observed")
	}
	// Loop start plus 7m30s, on the wall clock that context deadlines use.
	for _, d := range append(append([]time.Time{}, readDeadlines...), clock.deadlines...) {
		if d.IsZero() {
			continue
		}
		if d.Before(start.Add(groupRereadFence)) || d.After(end.Add(groupRereadFence)) {
			t.Fatalf("looped work deadline %s is not loop start + %s", d.Sub(start), groupRereadFence)
		}
	}
	if clock.deadlines[0].IsZero() {
		t.Fatal("waits are not bounded by the fence")
	}
}

func TestGroupRereadExpiryWriteHasItsOwnReserve(t *testing.T) {
	for _, tc := range []struct {
		name     string
		hardStop time.Duration // zero keeps the production cap
	}{
		{"reserve of a minute", 0},
		{"capped by the hard stop", groupRereadFence + time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				oldStop := groupRereadHardStop
				t.Cleanup(func() { groupRereadHardStop = oldStop })
				if tc.hardStop != 0 {
					groupRereadHardStop = tc.hardStop
				}
				f := newQueueFixture(t)
				f.pendingGroup(t)
				f.staleGroup()
				probe := f.probe()
				// The jobs read hangs until the fence cuts the pass, as a stalled gh would.
				probe.fault = func(ctx context.Context, path string, _ int) error {
					if path == f.groupJobsPath() {
						<-ctx.Done()
						return &githubReadError{path: path, cause: ctx.Err()}
					}
					return nil
				}
				type seen struct {
					deadline time.Time
					ok       bool
					err      error
				}
				var writes []seen
				probe.onWrite = func(ctx context.Context, _, _ string) {
					deadline, ok := ctx.Deadline()
					writes = append(writes, seen{deadline, ok, ctx.Err()})
				}
				clock := newGroupClock(t)
				start := time.Now()
				err := f.group()
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("the fence cause was lost: %v", err)
				}
				if len(clock.waits) != 0 {
					t.Fatalf("a fenced pass waited again: %v", clock.waits)
				}
				f.assertNewest(t, "failure", metadataExpiredTitle, metadataExpiredSummary)
				if len(writes) == 0 {
					t.Fatal("no terminal write observed")
				}
				for _, w := range writes {
					if w.err != nil || !w.ok {
						t.Fatalf("write context unusable or unbounded: %+v", w)
					}
					if !w.deadline.After(start.Add(groupRereadFence)) {
						t.Fatalf("write deadline %s is inside the fence", w.deadline.Sub(start))
					}
					if tc.hardStop != 0 && w.deadline.After(start.Add(tc.hardStop)) {
						t.Fatalf("write deadline %s passes the hard stop %s", w.deadline.Sub(start), tc.hardStop)
					}
					if left := time.Until(w.deadline); left > groupRereadReserve {
						t.Fatalf("write may run %s, more than the %s reserve", left, groupRereadReserve)
					}
				}
				if tc.hardStop == 0 {
					if left := time.Until(writes[0].deadline); left != groupRereadReserve {
						t.Fatalf("first write got only %s of the %s reserve", left, groupRereadReserve)
					}
				}
			})
		})
	}
}

func TestNonLoopingReserveKeepsItsBounds(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	q := &queueController{controller: &controller{}}
	reserve, stop := q.reserveContext(cancelled)
	defer stop()
	deadline, ok := reserve.Deadline()
	if reserve.Err() != nil || !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) < 4*time.Second {
		t.Fatalf("default reserve: err=%v deadline=%v ok=%t", reserve.Err(), time.Until(deadline), ok)
	}
	q.queueDeadline = time.Now().Add(time.Second)
	reserve, stop2 := q.reserveContext(cancelled)
	defer stop2()
	if deadline, _ := reserve.Deadline(); !deadline.Equal(q.queueDeadline) {
		t.Fatalf("reserve ignored the admission deadline: %v", deadline)
	}
	q.queueDeadline = time.Time{}
	outer, stop3 := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop3()
	reserve, stop4 := q.reserveContext(outer)
	defer stop4()
	want, _ := outer.Deadline()
	if deadline, _ := reserve.Deadline(); !deadline.Equal(want) {
		t.Fatalf("reserve outlived the invocation's deadline: %v", deadline)
	}
}

type failingOutput struct{ err error }

func (w failingOutput) Write([]byte) (int, error) { return 0, w.err }

func TestGroupRereadReportsProgressWriteFailure(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	boom := errors.New("stdout closed")
	f.c.output = failingOutput{boom}
	clock := newGroupClock(t)
	err := f.group()
	if !errors.Is(err, boom) {
		t.Fatalf("progress write failure was dropped: %v", err)
	}
	if len(clock.waits) != 0 {
		t.Fatalf("waits = %v", clock.waits)
	}
	f.assertNoTerminalWrites(t)
}

func TestExpirableGroupRead(t *testing.T) {
	status := func(code int) error { return &githubReadError{path: "p", status: code} }
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"deadline alone", context.DeadlineExceeded, true},
		{"read cut by the deadline", &githubReadError{path: "p", cause: context.DeadlineExceeded}, true},
		{"transient beside deadline", errors.Join(status(502), context.DeadlineExceeded), true},
		{"incomplete beside deadline", errors.Join(&groupProofIncomplete{}, context.DeadlineExceeded), true},
		{"permanent beside deadline", errors.Join(status(403), context.DeadlineExceeded), false},
		{"plain failure beside deadline", errors.Join(errors.New("merge group ref was deleted or rebuilt"), context.DeadlineExceeded), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := expirableGroupRead(tc.err); got != tc.want {
				t.Fatalf("expirableGroupRead(%v) = %t, want %t", tc.err, got, tc.want)
			}
			if tc.err != nil && errors.Is(tc.err, context.DeadlineExceeded) && deferrableGroupRead(tc.err) {
				t.Fatal("a deadline must stay evidence unless the loop's own fence fired")
			}
		})
	}
}

// The production timing, captured before any test shrinks the variables.
var productionTiming = struct{ fence, hardStop time.Duration }{groupRereadFence, groupRereadHardStop}

// The group job has 10 minutes including about 30s of setup. Every pass and
// wait is fenced inside the window plus one interval, and the terminal write
// may run on until the hard stop, which must still leave the job its margin.
func TestGroupRereadProductionTimingInvariants(t *testing.T) {
	if groupRereadWindow+groupRereadInterval >= productionTiming.fence {
		t.Errorf("window %s + interval %s must stay below the fence %s", groupRereadWindow, groupRereadInterval, productionTiming.fence)
	}
	if productionTiming.fence >= productionTiming.hardStop {
		t.Errorf("fence %s must stay below the hard stop %s", productionTiming.fence, productionTiming.hardStop)
	}
	if productionTiming.hardStop > 9*time.Minute {
		t.Errorf("hard stop %s leaves the 10 minute job no room for its setup", productionTiming.hardStop)
	}
	if groupRereadMaxBackoff < groupRereadInterval || groupRereadMaxBackoff >= groupRereadWindow {
		t.Errorf("backoff cap %s must lie between the interval %s and the window %s", groupRereadMaxBackoff, groupRereadInterval, groupRereadWindow)
	}
}

func TestGroupRereadPassDelayDoublesToCap(t *testing.T) {
	loop := &groupRereads{}
	for streak, want := range []time.Duration{20 * time.Second, 40 * time.Second, 80 * time.Second, 120 * time.Second, 120 * time.Second, 120 * time.Second} {
		loop.streak = streak
		if got := loop.passDelay(); got != want {
			t.Errorf("after %d deferred passes the wait is %s, want %s", streak, got, want)
		}
	}
}

func TestRateLimitedGroupRead(t *testing.T) {
	status := func(code int) error { return &githubReadError{path: "p", status: code} }
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rate limit", status(429), true},
		{"wrapped rate limit", fmt.Errorf("outer: %w", status(429)), true},
		{"bad gateway", status(502), false},
		{"forbidden", status(403), false},
		{"rate limit beside another transient", errors.Join(status(502), status(429)), true},
		{"rate limit last in a nest", errors.Join(status(502), fmt.Errorf("inner: %w", errors.Join(status(504), status(429)))), true},
		{"rate limit beside cancellation", errors.Join(context.Canceled, status(429)), true},
		{"GraphQL rejection", fmt.Errorf("boom: %w", &githubReadError{path: "p", rejected: true}), false},
		{"incomplete proof", &groupProofIncomplete{}, false},
		{"plain failure", errors.New("rate limit"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rateLimitedGroupRead(tc.err); got != tc.want {
				t.Fatalf("rateLimitedGroupRead(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

// A rate limit is not cured by waiting, and each further pass spends more of the
// budget that is already exhausted: one pass, then the expiry refusal.
func TestGroupRereadRateLimitExpiresWithoutFurtherPasses(t *testing.T) {
	healthy := newQueueFixture(t)
	healthy.pendingGroup(t)
	before := healthy.totalReads()
	if err := healthy.group(); err != nil {
		t.Fatal(err)
	}
	fullPass := healthy.totalReads() - before

	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.api.responses[f.groupJobsPath()] = &githubReadError{path: f.groupJobsPath(), status: 429}
	clock := newGroupClock(t)
	reads, workflow := f.totalReads(), f.api.reads[f.workflowPath()]
	err := f.group()
	var failure *githubReadError
	if !errors.As(err, &failure) || failure.status != 429 {
		t.Fatalf("the rate limit was lost: %v", err)
	}
	if len(clock.waits) != 0 {
		t.Fatalf("a rate-limited pass waited: %v", clock.waits)
	}
	if got := f.api.reads[f.workflowPath()] - workflow; got != 1 {
		t.Fatalf("full passes = %d, want 1", got)
	}
	f.assertNewest(t, "failure", metadataExpiredTitle, metadataExpiredSummary)
	f.assertNoSuccess(t)
	// One pass plus the expiry refusal's own verification reads; looping would
	// have cost a pass at every wait.
	if total := f.totalReads() - reads; total > 2*fullPass {
		t.Fatalf("a rate-limited group cost %d reads; one full pass costs %d", total, fullPass)
	}
}

func TestGroupRereadPollRateLimitHandsOverToFullPass(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	clock := newGroupClock(t)
	clock.before = func(int) {
		f.api.responses[f.groupJobsPath()] = &githubReadError{path: f.groupJobsPath(), status: 429}
	}
	workflow := f.api.reads[f.workflowPath()]
	err := f.group()
	var failure *githubReadError
	if !errors.As(err, &failure) || failure.status != 429 {
		t.Fatalf("the rate limit was lost: %v", err)
	}
	// The poll must not keep reading a limited API: one wait, then the full pass
	// that classifies it, which expires at once.
	if len(clock.waits) != 1 || f.api.reads[f.workflowPath()]-workflow != 2 {
		t.Fatalf("waits=%v full passes=%d", clock.waits, f.api.reads[f.workflowPath()]-workflow)
	}
	f.assertNewest(t, "failure", metadataExpiredTitle, metadataExpiredSummary)
}

// failFirstBarrierRead fails the first check-runs read after the jobs listing,
// which is the barrier check that follows a pass's verdict.
func (f *queueFixture) failFirstBarrierRead(failure error) {
	probe := f.probe()
	jobsSeen, failed := false, false
	probe.onRead = func(_ context.Context, path string) {
		if path == f.groupJobsPath() {
			jobsSeen = true
		}
	}
	probe.fault = func(_ context.Context, path string, _ int) error {
		if path == f.checksPath() && jobsSeen && !failed {
			failed = true
			return failure
		}
		return nil
	}
}

func TestGroupRereadRateLimitedBarrierReadExpiresAtOnce(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	f.failFirstBarrierRead(&githubReadError{path: "checks", status: 429})
	clock := newGroupClock(t)
	workflow := f.api.reads[f.workflowPath()]
	_ = f.group()
	if len(clock.waits) != 0 || f.api.reads[f.workflowPath()]-workflow != 1 {
		t.Fatalf("waits=%v full passes=%d, want the limit on the barrier read to end the loop", clock.waits, f.api.reads[f.workflowPath()]-workflow)
	}
	f.assertNewest(t, "failure", metadataExpiredTitle, metadataExpiredSummary)
}

// ensureBlocking takes several sequential calls. If they use up the window, the
// pass in hand is the final decision: no wait and no further pass start.
func TestGroupRereadRechecksWindowAfterEnsuringBarrier(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*queueFixture)
	}{
		{"full pass route", func(f *queueFixture) {
			f.api.responses[f.groupJobsPath()] = &githubReadError{path: f.groupJobsPath(), status: 502}
		}},
		{"poll route", func(f *queueFixture) { f.staleGroup() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQueueFixture(t)
			f.pendingGroup(t)
			tc.prepare(f)
			clock := newGroupClock(t)
			probe := f.probe()
			jobsSeen, spent := false, false
			probe.onRead = func(_ context.Context, path string) {
				if path == f.groupJobsPath() {
					jobsSeen = true
				}
				if path == f.checksPath() && jobsSeen && !spent {
					spent = true
					clock.now = clock.now.Add(groupRereadWindow)
				}
			}
			workflow := f.api.reads[f.workflowPath()]
			_ = f.group()
			if !spent {
				t.Fatal("the barrier read never ran, so the test proves nothing")
			}
			if len(clock.waits) != 0 || f.api.reads[f.workflowPath()]-workflow != 1 {
				t.Fatalf("waits=%v full passes=%d, want the pass in hand to be final", clock.waits, f.api.reads[f.workflowPath()]-workflow)
			}
			f.assertNewest(t, "failure", metadataExpiredTitle, metadataExpiredSummary)
		})
	}
}

func TestGroupRereadPollSkipsReadWhenNoWaitCanFollow(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	clock := newGroupClock(t)
	jobs := f.api.reads[f.groupJobsPath()]
	_ = f.group()
	if len(clock.waits) != expectedWaits {
		t.Fatalf("waits = %d, want %d", len(clock.waits), expectedWaits)
	}
	// First pass, a poll after every wait but the last (the closing pass reads
	// the listing itself), then the closing pass.
	if got := f.api.reads[f.groupJobsPath()] - jobs; got != expectedWaits+1 {
		t.Fatalf("jobs listing reads = %d, want %d", got, expectedWaits+1)
	}
}

// A pass that read enough to see lag shows GitHub is healthy again, so a later
// failure backs off from the interval, not from where the first streak ended.
func TestGroupRereadBackoffRestartsAfterHealthyPass(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	probe := f.probe()
	var broken atomic.Bool
	probe.fault = func(_ context.Context, path string, n int) error {
		if path == f.workflowPath() && (n == 1 || broken.Load()) {
			return &githubReadError{path: path, status: 502}
		}
		return nil
	}
	clock := newGroupClock(t)
	clock.before = func(wait int) {
		if wait == 3 { // the proof settles, then the workflow read starts failing
			broken.Store(true)
			f.groupRefresh()
		}
	}
	_ = f.group()
	want := []time.Duration{20 * time.Second, 20 * time.Second, 20 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second}
	if len(clock.waits) < len(want) || !slices.Equal(clock.waits[:len(want)], want) {
		t.Fatalf("waits = %v, want them to begin %v", clock.waits, want)
	}
}

// Exercise settled-record recognition through the group callback rather than
// constructing the reconciliation's private state. Every non-settling record
// still gets bounded re-reads and an effective blocker, never reused success.
func TestGroupSettledRecognisesOnlyItsOwnVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*check, *binding)
		noProof bool
		absent  bool
		waits   int
	}{
		{name: "expiry refusal on the run-less barrier", waits: 0},
		{name: "unsafe-proof refusal on the run-less barrier", mutate: func(c *check, _ *binding) { c.Output.Title = unsafeProofRefusal.Title }, waits: expectedWaits},
		{name: "retirement-cancelled run-less barrier", mutate: func(c *check, _ *binding) { c.Output.Title, c.Conclusion = "PR validation retired", "cancelled" }, waits: expectedWaits},
		{name: "run-less record with the expiry title still pending", mutate: func(c *check, _ *binding) { c.Status, c.Conclusion = "queued", "" }, waits: expectedWaits},
		{name: "unauthenticated expiry refusal", mutate: func(c *check, _ *binding) { c.App.Slug = "someone-else" }, waits: 0},
		{name: "this run's verdict under another title", mutate: func(c *check, b *binding) { b.Run, b.Attempt, c.Output.Title = 102, 1, "PR validation refused" }, waits: 0},
		{name: "another attempt of this run", mutate: func(_ *check, b *binding) { b.Run, b.Attempt = 102, 2 }, waits: expectedWaits},
		{name: "another run", mutate: func(_ *check, b *binding) { b.Run, b.Attempt = 103, 1 }, waits: expectedWaits},
		{name: "no proof yet", noProof: true, waits: len(persistentFailureWaits)},
		{name: "no record", absent: true, waits: expectedWaits},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQueueFixture(t)
			f.pendingGroup(t)
			prior := f.api.checks[0]
			held, err := decodeBinding(prior.ExternalID)
			if err != nil || held.Run != 0 {
				t.Fatalf("setup: the running group's barrier must be run-less: %+v %v", held, err)
			}
			prior.Status, prior.Conclusion, prior.Output.Title = "completed", "failure", metadataExpiredRefusal.Title
			if tc.mutate != nil {
				tc.mutate(&prior, &held)
			}
			prior.ExternalID = held.externalID()
			f.api.checks = []check{prior}
			if tc.absent {
				f.api.checks = nil
			}
			f.staleGroup()
			if tc.noProof {
				f.api.responses[f.workflowPath()] = &githubReadError{path: f.workflowPath(), status: 502}
			}
			writes := len(f.api.writes)
			clock := newGroupClock(t)
			if err := f.group(); err == nil {
				t.Fatal("incomplete or unauthenticated proof was accepted")
			}
			if len(clock.waits) != tc.waits {
				t.Fatalf("waits = %v, want %d waits", clock.waits, tc.waits)
			}
			if !tc.absent && prior.Status == "completed" && f.api.checks[0] != prior {
				t.Fatal("historical terminal verdict was mutated")
			}
			if tc.waits == 0 && len(f.api.writes) != writes {
				t.Fatal("settled or unauthenticated history was mutated")
			}
			if !blockingQueueCheck(newestQueueCheck(f.api.checks)) {
				t.Fatal("incomplete proof has no effective blocker")
			}
			f.assertNoSuccess(t)
		})
	}
}

func TestGroupSettledUsesPinnedIdentityAfterLaterReadFailure(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	f.staleGroup()
	probe := f.probe()
	probe.fault = func(_ context.Context, path string, n int) error {
		if path == f.workflowPath() && n > 1 {
			return &githubReadError{path: path, status: 502}
		}
		return nil
	}
	clock := newGroupClock(t)
	clock.before = func(wait int) {
		if wait == 1 {
			// Another invocation expires the run-less barrier while this one
			// waits. Its next pass fails before re-observing the finished group.
			f.api.checks[0].Status, f.api.checks[0].Conclusion, f.api.checks[0].Output.Title = "completed", "failure", metadataExpiredRefusal.Title
			f.groupRefresh()
		}
	}
	writes := len(f.api.writes)
	if err := f.group(); err == nil {
		t.Fatal("later transient read failure was erased")
	}
	if len(clock.waits) != 1 || len(f.api.writes) != writes {
		t.Fatalf("settled pinned identity waited or mutated again: waits=%v writes=%+v", clock.waits, f.api.writes[writes:])
	}
	f.assertNewest(t, "failure", metadataExpiredTitle, "")
	f.assertNoSuccess(t)
}

// A run-less completed record that is not the expiry refusal (here a
// retirement-cancelled barrier) is no verdict on this run: the invocation loops.
func TestGroupRereadStillWaitsWhenRunlessRecordIsNotTheExpiryRefusal(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	id := newestQueueCheck(f.api.checks).ID
	for i := range f.api.checks {
		if f.api.checks[i].ID == id {
			c := &f.api.checks[i]
			c.Status, c.Conclusion, c.CompletedAt, c.Output.Title = "completed", "cancelled", "2026-01-01T00:00:00Z", "PR validation retired"
		}
	}
	f.staleGroup()
	clock := newGroupClock(t)
	_ = f.group()
	if len(clock.waits) != expectedWaits {
		t.Fatalf("waited %d times, want %d: the record settled a run it does not belong to", len(clock.waits), expectedWaits)
	}
	f.assertNoSuccess(t)
}
