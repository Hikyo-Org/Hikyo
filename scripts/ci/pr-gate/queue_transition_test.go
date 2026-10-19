package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestQueueSnapshotRestartsAfterTransientGap(t *testing.T) {
	for _, pages := range []string{"single page", "multiple pages"} {
		t.Run(pages, func(t *testing.T) {
			f := newQueueFixture(t)
			entries := []queueEntry{f.entry}
			second := ""
			if pages == "multiple pages" {
				second, entries = f.paginatedQueue()
				entries[100].Position = 102
				f.api.responses[second] = queuePageResponse("QUEUE_MAIN", entries[100:], false, nil)
			} else {
				moved := f.entry
				moved.Position = 2
				f.api.responses[f.query] = queuePageResponse("QUEUE_MAIN", []queueEntry{moved}, false, nil)
			}
			f.api.readHook = func(path string, n int) {
				if path == f.query && n == 2 {
					if second != "" {
						entries[100].Position = 101
						f.api.responses[second] = queuePageResponse("QUEUE_MAIN", entries[100:], false, nil)
					} else {
						f.api.responses[f.query] = queuePageResponse("QUEUE_MAIN", entries, false, nil)
					}
				}
			}
			live, err := f.q.liveQueue(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(live.Entries.Nodes) != len(entries) || f.api.reads[f.query] != 2 || second != "" && f.api.reads[second] != 2 {
				t.Fatal("queue recovery did not restart the complete traversal from page one")
			}
			if len(f.api.writes) != 0 {
				t.Fatal("snapshot recovery wrote a check")
			}
		})
	}
}

func TestQueueSnapshotRecoveryHonorsDeadline(t *testing.T) {
	f := newQueueFixture(t)
	f.entry.Position = 2
	f.groupRefresh()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	live, err := f.q.liveQueue(ctx)
	if live != nil || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "incomplete paginated queue positions") {
		t.Fatalf("snapshot recovery lost its deadline or original diagnostic: %v", err)
	}
	if f.api.reads[f.query] != 1 || len(f.api.writes) != 0 {
		t.Fatal("cancelled snapshot recovery continued reading or wrote a check")
	}
}

func TestQueuePersistentSnapshotGapRemainsBlocking(t *testing.T) {
	f := newQueueFixture(t)
	f.entry.Position = 2
	f.groupRefresh()
	if err := f.group(); err == nil || !strings.Contains(err.Error(), "incomplete paginated queue positions") {
		t.Fatalf("persistent inconsistency was accepted: %v", err)
	}
	f.assertNoSuccess(t)
	if !blockingQueueCheck(newestQueueCheck(f.api.checks)) || f.api.reads[f.query] > 6 {
		t.Fatal("persistent inconsistency escaped its bounded blocking outcome")
	}
}

func TestQueueSnapshotRecoveryDoesNotRetryInvalidIdentityOrTransport(t *testing.T) {
	for _, failure := range []string{"identity", "transport"} {
		t.Run(failure, func(t *testing.T) {
			f := newQueueFixture(t)
			if failure == "identity" {
				f.entry.ID = ""
				f.groupRefresh()
			} else {
				f.api.responses[f.query] = errors.New("queue API unavailable")
			}
			if live, err := f.q.liveQueue(context.Background()); live != nil || err == nil {
				t.Fatal("invalid or unavailable membership was accepted")
			}
			if f.api.reads[f.query] != 1 || len(f.api.writes) != 0 {
				t.Fatal("snapshot recovery retried an unrelated failure or wrote a check")
			}
		})
	}
}

func TestQueueGroupGapAfterMergePreservesTerminalVerdict(t *testing.T) {
	for _, gapReads := range []int{1, 3} {
		t.Run(map[int]string{1: "settles during snapshot recovery", 3: "settles during retirement"}[gapReads], func(t *testing.T) {
			testQueueGroupGapAfterMerge(t, gapReads)
		})
	}
}

func testQueueGroupGapAfterMerge(t *testing.T, gapReads int) {
	t.Helper()
	f := newQueueFixture(t)
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	prior := append([]check(nil), f.api.checks...)
	writes := len(f.api.writes)
	nextRead := f.api.reads[f.query] + 1
	f.api.readHook = func(path string, n int) {
		if path != f.query {
			return
		}
		if n == nextRead {
			moved := f.entry
			moved.Position = 2
			f.api.responses[f.query] = queuePageResponse("QUEUE_MAIN", []queueEntry{moved}, false, nil)
			f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{}
		}
		if n == nextRead+gapReads {
			noRetirementRoles(f)
		}
	}
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != writes || len(f.api.checks) != len(prior) {
		t.Fatal("late callback created a blocker for an obsolete group")
	}
	for i := range prior {
		if f.api.checks[i] != prior[i] {
			t.Fatal("late callback changed a terminal verdict")
		}
	}
}

func TestQueueQueuedPredecessorCannotSupplyActiveGroupProof(t *testing.T) {
	f := newQueueFixture(t)
	waiting := f.entry
	waiting.ID, waiting.State, waiting.PR.Number = "PREDECESSOR8", "QUEUED", 8
	waiting.Head.OID, waiting.Base.OID = "", ""
	f.entry.Position = 2
	f.api.responses[f.query] = queuePageResponse("QUEUE_MAIN", []queueEntry{waiting, f.entry}, false, nil)
	if err := f.group(); err == nil || !strings.Contains(err.Error(), "invalid ordered merge queue entry") {
		t.Fatalf("unmaterialized predecessor supplied active group proof: %v", err)
	}
	f.assertNoSuccess(t)
}

func TestQueueSnapshotMovementDiagnosticIsBoundedAndOmitsOpaqueStrings(t *testing.T) {
	f := newQueueFixture(t)
	_, entries := f.paginatedQueue()
	live := &queueData{ID: "opaque-server-value-must-not-be-logged\n"}
	live.Entries.Nodes = entries
	err := incompleteQueuePositions(live, 2, 101)
	if strings.Contains(err.Error(), live.ID) || strings.Contains(err.Error(), "opaque-server-value") || len(err.Error()) > 1024 {
		t.Fatal("snapshot diagnostic leaked opaque data or exceeded its bound")
	}
	for _, want := range []string{"page=2", "entries=101", "missing_position=101", "positions=[1 2 3", "PRs=[7 8 9", "truncated=true"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("snapshot diagnostic omitted %q: %v", want, err)
		}
	}
}

func TestQueueAbsentGroupWithQueuedSuccessorRetires(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending check", true: "terminal check"}[terminal], func(t *testing.T) {
			f := newQueueFixture(t)
			if !terminal {
				f.groupRun.Status, f.groupRun.Conclusion = "in_progress", ""
				f.groupRefresh()
			}
			if err := f.group(); err != nil {
				t.Fatal(err)
			}
			prior := newestQueueCheck(f.api.checks)
			f.groupRun.Status, f.groupRun.Conclusion = "completed", "success"
			f.groupRefresh()
			nextRead := f.api.reads[f.query] + 2
			f.api.readHook = func(path string, n int) {
				if path == f.query && n == nextRead {
					waiting := f.entry
					waiting.ID, waiting.State = "SUCCESSOR8", "QUEUED"
					waiting.PR.Number = 8
					waiting.Head.OID, waiting.Base.OID = "", ""
					f.api.responses[f.query] = queuePageResponse("QUEUE_MAIN", []queueEntry{waiting}, false, nil)
					f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{}
				}
			}
			if err := f.group(); err != nil {
				t.Fatal(err)
			}
			latest := newestQueueCheck(f.api.checks)
			want := "cancelled"
			if terminal {
				want = "success"
			}
			if len(f.api.checks) != 1 || latest.ID != prior.ID || latest.Status != "completed" || latest.Conclusion != want {
				t.Fatalf("obsolete group was not retired without replacing terminal history: %+v", latest)
			}
		})
	}
}

func TestQueueFinalMembershipMovementRenewsRetirementProof(t *testing.T) {
	for _, activeRole := range []bool{false, true} {
		t.Run(map[bool]string{false: "landed group", true: "same head open PR"}[activeRole], func(t *testing.T) {
			f := newQueueFixture(t)
			if err := f.group(); err != nil {
				t.Fatal(err)
			}
			writes, count := len(f.api.writes), len(f.api.checks)
			nextRead := f.api.reads[f.query] + 3
			moved := false
			f.api.readHook = func(path string, n int) {
				if path == f.query && n == nextRead {
					moved = true
					f.entry.ID = "REPLACED_AFTER_MERGE"
					f.groupRefresh()
					f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": f.groupRun.HeadSHA}}
					pulls := []pull{}
					if activeRole {
						p := f.pr
						p.Number, p.Head.SHA = 8, f.groupRun.HeadSHA
						pulls = append(pulls, p)
					}
					f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = pulls
				}
			}
			err := f.group()
			if !moved {
				t.Fatal("final membership never moved")
			}
			if activeRole {
				latest := newestQueueCheck(f.api.checks)
				if err == nil || !blockingQueueCheck(latest) || latest.Conclusion == "cancelled" {
					t.Fatalf("movement retired or accepted an active same-head PR: %v %+v", err, latest)
				}
			} else if err != nil || len(f.api.writes) != writes || len(f.api.checks) != count {
				t.Fatalf("landed group was blocked instead of freshly retired: %v", err)
			}
		})
	}
}
