package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// An admission fixture whose head already carries the pending record an earlier
// attempt leaves, on the queue or the dev context.
func unfinishedFixture(t *testing.T, mode string) *queueFixture {
	t.Helper()
	var f *queueFixture
	if mode == "dev" {
		t.Setenv("GITHUB_REF", "refs/heads/main")
		f = newDevFixture(t)
		f.c.repo = f.pr.Base.Repo
		f.c.repo.DefaultBranch = "main"
	} else {
		f = newQueueFixture(t)
	}
	c := check{ID: 100, Name: f.c.checkName, HeadSHA: f.pr.Head.SHA, ExternalID: newBinding(f.c.repo.ID, f.pr, &f.feedbackRun).externalID(), Status: "queued"}
	c.App.ID, c.App.Slug = actionsAppID, "github-actions"
	c.Output.Title, c.Output.Summary = "PR validation pending", "seeded by an earlier attempt"
	f.api.checks = []check{c}
	f.api.nextID = 900
	return f
}

func unfinishedInvoke(mode string, f *queueFixture, b admissionBudget) error {
	if mode == "dev" {
		return f.q.withAdmissionBudget(context.Background(), b, func(work context.Context) error {
			return f.q.executeDev(work, "workflow_dispatch", event{Repository: f.c.repo}, f.pr.Number)
		})
	}
	return f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), b)
}

func freshBudget() admissionBudget {
	return admissionBudget{Started: time.Now(), Deadline: time.Now().Add(admissionSeconds * time.Second)}
}

// workEndsAfter gives the work context d, then the whole reserve.
func workEndsAfter(d time.Duration) admissionBudget {
	return admissionBudget{Started: time.Now().Add(-45 * time.Second), Deadline: time.Now().Add(admissionReserve + d)}
}

func eachUnfinished(t *testing.T, run func(t *testing.T, mode string, attempt int)) {
	t.Helper()
	for _, mode := range []string{"queue", "dev"} {
		for _, attempt := range []int{1, 2, 3} {
			t.Run(mode+"/attempt "+strconv.Itoa(attempt), func(t *testing.T) {
				t.Setenv("GITHUB_RUN_ATTEMPT", strconv.Itoa(attempt))
				run(t, mode, attempt)
			})
		}
	}
}

func (f *queueFixture) newestOnHead() check {
	var newest check
	for _, c := range f.api.checks {
		if c.HeadSHA == f.c.expectedHead && c.ID > newest.ID {
			newest = c
		}
	}
	return newest
}

// cutOnce holds the first read whose path contains sub until its context ends,
// as a request in flight at the work deadline does.
func cutOnce(sub string) func(context.Context, string) error {
	var cut atomic.Bool
	return func(ctx context.Context, path string) error {
		if strings.Contains(path, sub) && cut.CompareAndSwap(false, true) {
			<-ctx.Done()
			return &githubReadError{path: path, cause: ctx.Err()}
		}
		return nil
	}
}

// Live case: run 41685149674 attempt 3. The budget ended inside the feedback jobs
// read, the trace never captured the feedback run, and the refusal read the
// difference as "useful feedback attempt moved", wrote nothing, and left the
// pending record. Attempts 1 and 2 publish a pending record, attempt 3 the
// terminal refusal.
func TestAdmissionCutOffInTheFeedbackReadPublishesOnEveryAttempt(t *testing.T) {
	eachUnfinished(t, func(t *testing.T, mode string, attempt int) {
		f := unfinishedFixture(t, mode)
		f.c.api = deadlineAPI{api: f.c.api, readHook: cutOnce("attempts/1/jobs?")}
		err := unfinishedInvoke(mode, f, workEndsAfter(100*time.Millisecond))
		if err == nil || !strings.Contains(err.Error(), "budget exhausted") || strings.Contains(err.Error(), "moved") {
			t.Fatalf("a cut-off trace was reported as movement: %v", err)
		}
		got, want := f.newestOnHead(), f.q.pendingText(7)
		if attempt == admissionMaxAttempts {
			want = f.q.refusalText(7)
		}
		if got.Status != want.Status || got.Conclusion != want.Conclusion || got.Output.Summary != want.Summary {
			t.Fatalf("attempt %d left %+v, want %+v", attempt, got, want)
		}
	})
}

// Protection has three states when the budget ends. The read never returned: not
// movement. It returned disabled (the queue is off): the trace holds no policy,
// and main's behaviour stays, so the reserve judges nothing and a protection that
// has since been enabled is reported as moved rather than hidden. It returned
// enabled: a change is movement.
func TestAdmissionProtectionReadStates(t *testing.T) {
	t.Run("the read never returned", func(t *testing.T) {
		eachUnfinished(t, func(t *testing.T, mode string, attempt int) {
			f := unfinishedFixture(t, mode)
			f.c.api = deadlineAPI{api: f.c.api, readHook: cutOnce("rulesets/")}
			err := unfinishedInvoke(mode, f, workEndsAfter(100*time.Millisecond))
			if err == nil || strings.Contains(err.Error(), "moved") {
				t.Fatalf("a read that never returned was reported as movement: %v", err)
			}
			want := f.q.pendingText(7)
			if attempt == admissionMaxAttempts {
				want = f.q.refusalText(7)
			}
			if got := f.newestOnHead(); got.Status != want.Status || got.Output.Summary != want.Summary {
				t.Fatalf("attempt %d left %+v", attempt, got)
			}
		})
	})
	t.Run("returned disabled", func(t *testing.T) {
		t.Setenv("GITHUB_RUN_ATTEMPT", "3")
		f := unfinishedFixture(t, "queue")
		f.rules = f.rules[:len(f.rules)-1] // no merge_queue rule: the queue is off
		f.policyRefresh()
		var prReads atomic.Int32
		f.c.api = deadlineAPI{api: f.c.api, readHook: func(_ context.Context, path string) error {
			// The final PR read of the pass fails, so the invocation ends in the reserve.
			if strings.HasSuffix(path, "/pulls/7") && prReads.Add(1) == 2 {
				return &githubReadError{path: path, status: 502}
			}
			return nil
		}}
		err := unfinishedInvoke("queue", f, freshBudget())
		if err == nil || !strings.Contains(err.Error(), "current protection moved or was not captured before admission failure") {
			t.Fatalf("a returned disabled protection must keep main's behaviour: %v", err)
		}
		if got := f.newestOnHead(); got.Status == "completed" {
			t.Fatalf("a refusal was published for a repository with the queue off: %+v", got)
		}
	})
	t.Run("returned disabled and enabled since", func(t *testing.T) {
		t.Setenv("GITHUB_RUN_ATTEMPT", "3")
		f := unfinishedFixture(t, "queue")
		allRules := f.rules
		f.rules = f.rules[:len(f.rules)-1] // no merge_queue rule: the queue is off
		f.policyRefresh()
		// A policy read is two paths and the pass makes two, so read 5 is the reserve's first.
		policyReads, enabled := 0, false
		f.api.readHook = func(path string, _ int) {
			if strings.Contains(path, "rulesets/") || strings.Contains(path, "rules/branches/") {
				if policyReads++; policyReads == 5 {
					f.rules = allRules
					f.policyRefresh()
					enabled = true
				}
			}
		}
		var prReads atomic.Int32
		f.c.api = deadlineAPI{api: f.c.api, readHook: func(_ context.Context, path string) error {
			// The final PR read of the pass fails, so the invocation ends in the reserve.
			if strings.HasSuffix(path, "/pulls/7") && prReads.Add(1) == 2 {
				return &githubReadError{path: path, status: 502}
			}
			return nil
		}}
		err := unfinishedInvoke("queue", f, freshBudget())
		if !enabled {
			t.Fatalf("the queue never came back before the reserve read it: %d policy reads", policyReads)
		}
		if err == nil || !strings.Contains(err.Error(), "current protection moved or was not captured before admission failure") {
			t.Fatalf("a protection enabled since its disabled read must be reported as moved: %v", err)
		}
		if got := f.newestOnHead(); got.Status == "completed" {
			t.Fatalf("a refusal was published although protection moved: %+v", got)
		}
	})
	t.Run("never returned and the queue is off now", func(t *testing.T) {
		t.Setenv("GITHUB_RUN_ATTEMPT", "3")
		f := unfinishedFixture(t, "queue")
		f.rules = f.rules[:len(f.rules)-1]
		f.policyRefresh()
		f.c.api = deadlineAPI{api: f.c.api, readHook: cutOnce("rules/branches/")}
		err := unfinishedInvoke("queue", f, workEndsAfter(100*time.Millisecond))
		if err == nil || !strings.Contains(err.Error(), "current protection moved or was not captured before admission failure") {
			t.Fatalf("a refusal naming reassessments that do nothing was published: %v", err)
		}
		if got := f.newestOnHead(); got.Status == "completed" {
			t.Fatalf("a refusal was published for a repository with the queue off: %+v", got)
		}
	})
	t.Run("returned enabled and moved", func(t *testing.T) {
		eachUnfinished(t, func(t *testing.T, mode string, attempt int) {
			f := unfinishedFixture(t, mode)
			cut := cutOnce("attempts/1/jobs?")
			reads := 0
			f.api.readHook = func(path string, _ int) {
				if strings.Contains(path, "rulesets/") {
					// The first read is the pass's, the second the reserve's.
					if reads++; reads == 2 {
						f.owner["updated_at"] = "changed"
						if mode == "dev" {
							f.api.responses[f.c.endpoint("rulesets/31")] = f.owner
						} else {
							f.policyRefresh()
						}
					}
				}
			}
			f.c.api = deadlineAPI{api: f.c.api, readHook: cut}
			err := unfinishedInvoke(mode, f, workEndsAfter(100*time.Millisecond))
			if err == nil || !strings.Contains(err.Error(), "current protection moved or was not captured before admission failure") {
				t.Fatalf("a moved protection was not reported: %v", err)
			}
			for _, w := range f.api.writes {
				if w["status"] == "completed" {
					t.Fatalf("a moved protection was refused: %+v", w)
				}
			}
		})
	})
}

// Feedback callbacks reach the queue gate for dev and stacked PRs too, and
// retirement skips completed records, so a refusal there would stay red on the head
// for good, with text that is false for it. The pending barrier stays and the next
// pass retires it.
func TestAdmissionCutOffLeavesNoQueueRefusalOutsideMain(t *testing.T) {
	for _, tc := range []struct {
		base    string
		attempt int
	}{{"dev", 1}, {"dev", 3}, {"feat/stacked-base", 1}, {"feat/stacked-base", 3}} {
		attempt := tc.attempt
		t.Run(tc.base+"/attempt "+strconv.Itoa(attempt), func(t *testing.T) {
			t.Setenv("GITHUB_RUN_ATTEMPT", strconv.Itoa(attempt))
			f := unfinishedFixture(t, "queue")
			f.pr.Base.Ref = tc.base
			f.refresh()
			f.feedbackRun.Pulls = []pull{f.pr}
			f.feedbackRefresh()
			f.c.api = deadlineAPI{api: f.c.api, readHook: cutOnce("attempts/1/jobs?")}
			err := unfinishedInvoke("queue", f, workEndsAfter(100*time.Millisecond))
			if err == nil || !strings.Contains(err.Error(), "work budget exhausted") || !strings.Contains(err.Error(), "a PR outside the default branch holds no queue role") {
				t.Fatalf("expected a real cut and no refusal for a PR to %s: %v", tc.base, err)
			}
			for _, w := range f.api.writes {
				if w["status"] == "completed" {
					t.Fatalf("a queue refusal reached a PR to %s: %+v", tc.base, w)
				}
			}
		})
	}
}

// The records name no cause, say a rerun follows only while attempts remain, name
// the events that reassess the head, and are correct for their context.
func TestUnfinishedTextPerContext(t *testing.T) {
	for mode, tc := range map[string]struct{ command, required, absent string }{
		"queue": {"gh workflow run gate-queue -f pr_number=7", "Feedback and full queue validation remain required.", "Fast feedback, DCO and policy"},
		"dev":   {"gh workflow run gate-dev -f pr_number=7", "Fast feedback, DCO and policy remain required.", "full queue validation"},
	} {
		t.Run(mode, func(t *testing.T) {
			f := unfinishedFixture(t, mode)
			pending, refusal := f.q.pendingText(7), f.q.refusalText(7)
			if pending.Status != "queued" || refusal.Conclusion != "failure" {
				t.Fatalf("wrong outcomes: %+v %+v", pending, refusal)
			}
			for _, o := range []outcome{pending, refusal} {
				for _, want := range []string{"The publisher did not finish", "Merging stays blocked", "a new push to this PR", "a feedback result", "a push to main", "the schedule", tc.command} {
					if !strings.Contains(o.Summary, want) {
						t.Errorf("%q lacks %q", o.Summary, want)
					}
				}
				for _, bad := range []string{"budget failure", "No verdict", "push to dev", "automatically", "setup-inclusive", tc.absent} {
					if strings.Contains(o.Summary, bad) {
						t.Errorf("%q must not contain %q", o.Summary, bad)
					}
				}
			}
			if !strings.Contains(pending.Summary, "rerun only while attempts remain") || strings.Contains(refusal.Summary, "rerun") {
				t.Errorf("the rerun is promised wrongly: %q / %q", pending.Summary, refusal.Summary)
			}
			if !strings.Contains(refusal.Summary, tc.required) || !strings.Contains(refusal.Summary, "fix it before reassessing") {
				t.Errorf("%q lacks %q or the next step for a persistent error", refusal.Summary, tc.required)
			}
		})
	}
}

// The job log line for a moved dev admission does not state a rerun as fact.
func TestMovedAdmissionLogLineDoesNotPromiseARerun(t *testing.T) {
	f := unfinishedFixture(t, "dev")
	if err := f.q.initRepository(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := f.q.queueMovement(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "admission state moved") || strings.Contains(err.Error(), "until the rerun") || !strings.Contains(err.Error(), "a rerun while attempts remain") {
		t.Fatal(fmt.Sprint("unexpected log line: ", err))
	}
}
