package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAdmissionBudgetCannotResetSetupClock(t *testing.T) {
	now := time.Unix(1700000000, 0)
	start := strconv.FormatInt(now.Unix()-20, 10)
	end := strconv.FormatInt(now.Unix()+40, 10)
	if _, err := parseAdmissionBudget(start, end, now); err != nil {
		t.Fatal(err)
	}
	for name, values := range map[string][2]string{
		"missing": {"", ""}, "malformed": {"oops", end}, "noncanonical": {"01700000000", end},
		"extended": {start, strconv.FormatInt(now.Unix()+41, 10)},
		"future":   {strconv.FormatInt(now.Unix()+1, 10), strconv.FormatInt(now.Unix()+61, 10)},
		"expired":  {strconv.FormatInt(now.Unix()-60, 10), strconv.FormatInt(now.Unix(), 10)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAdmissionBudget(values[0], values[1], now); err == nil {
				t.Fatal("invalid or reset publisher budget accepted")
			}
		})
	}
}

func targetedFixtureEvent(f *queueFixture) event {
	e := f.event
	e.Number, e.Pull = f.pr.Number, f.pr
	return e
}

func TestTargetedAdmissionDeadlinePublishesFailureWithinReserve(t *testing.T) {
	f := newQueueFixture(t)
	f.c.verify = func(ctx context.Context, _ int, _ string) (policyDecision, error) {
		<-ctx.Done()
		return policyDecision{}, ctx.Err()
	}
	// Work has only 25ms left, reproducing setup consuming its original budget.
	b := admissionBudget{Started: time.Now().Add(-45 * time.Second), Deadline: time.Now().Add(admissionReserve + 25*time.Millisecond)}
	err := f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), b)
	if err == nil || !strings.Contains(err.Error(), "budget exhausted") {
		t.Fatalf("missing explicit timeout failure: %v", err)
	}
	f.assertNoSuccess(t)
	got := f.api.writes[len(f.api.writes)-1]
	if got["status"] != "completed" || got["conclusion"] != "failure" {
		t.Fatalf("reserve did not publish authenticated terminal failure: %+v", got)
	}
	if got["external_id"] != newBinding(f.c.repo.ID, f.pr, &f.feedbackRun).externalID() {
		t.Fatal("timeout failure lost its exact source attempt binding")
	}
}

func TestTargetedAdmissionFailureNeverRefusesMovedTuple(t *testing.T) {
	for name, move := range map[string]func(*queueFixture){
		"head": func(f *queueFixture) {
			f.pr.Head.SHA = strings.Repeat("c", 40)
			f.api.responses[f.c.endpoint("pulls/7")] = f.pr
		},
		"base": func(f *queueFixture) {
			f.pr.Base.SHA = strings.Repeat("a", 40)
			f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": f.pr.Base.SHA}}
		},
		"closed":  func(f *queueFixture) { f.pr.State = "closed"; f.api.responses[f.c.endpoint("pulls/7")] = f.pr },
		"policy":  func(f *queueFixture) { f.owner["updated_at"] = "changed"; f.policyRefresh() },
		"attempt": func(f *queueFixture) { f.feedbackRun.Attempt++; f.feedbackRefresh() },
	} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			old := check{ID: 100, Name: queueCheckName, HeadSHA: f.pr.Head.SHA, ExternalID: newBinding(f.pr.Base.Repo.ID, f.pr, &f.feedbackRun).externalID(), Status: "completed", Conclusion: "success"}
			old.App.ID, old.App.Slug = actionsAppID, "github-actions"
			f.api.checks = []check{old}
			originalEvent := targetedFixtureEvent(f)
			f.c.verify = func(context.Context, int, string) (policyDecision, error) {
				move(f)
				return policyDecision{}, errors.New("validation stopped")
			}
			b := admissionBudget{Started: time.Now(), Deadline: time.Now().Add(time.Minute)}
			if err := f.q.executeTargeted(context.Background(), "pull_request_target", originalEvent, b); err == nil {
				t.Fatal("stopped validation returned success")
			}
			f.assertNoSuccess(t)
			if len(f.api.writes) == 0 || f.api.writes[0]["status"] != "queued" {
				t.Fatal("the prior green was not revoked before admission stopped")
			}
			for _, w := range f.api.writes {
				if w["status"] == "completed" {
					t.Fatalf("different current tuple was refused: %+v", w)
				}
			}
		})
	}
}

func TestTargetedAdmissionCannotRunMaintenance(t *testing.T) {
	for _, name := range []string{"push", "schedule", "workflow_run"} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			e := f.event
			e.Run, e.Ref = f.groupRun, "refs/heads/main"
			if err := f.q.executeTargeted(context.Background(), name, e, admissionBudget{Started: time.Now(), Deadline: time.Now().Add(time.Minute)}); err == nil {
				t.Fatal("targeted publisher entered a group or all-PR repair")
			}
			if len(f.api.writes) != 0 {
				t.Fatal("targeted invalid route published checks")
			}
		})
	}
	for _, name := range []string{"pull_request_target", "push", "schedule", "workflow_run"} {
		f := newQueueFixture(t)
		f.q.repairOnly = true
		if err := f.q.execute(context.Background(), name, f.event); err == nil || len(f.api.writes) != 0 {
			t.Fatalf("group-only mode accepted nonmanual %s", name)
		}
	}
}

func TestTargetedAdmissionRechecksOldUsefulRunWithoutAgeBan(t *testing.T) {
	f := newQueueFixture(t)
	f.feedbackRun.CreatedAt = "2020-01-01T00:00:00Z"
	f.feedbackRefresh()
	if err := f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), admissionBudget{Started: time.Now(), Deadline: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if f.api.writes[len(f.api.writes)-1]["conclusion"] != "success" {
		t.Fatal("a valid old useful run was permanently age-refused")
	}
}

type deadlineAPI struct {
	api
	readHook func(context.Context, string) error
}

func (a deadlineAPI) read(ctx context.Context, path string, out any) error {
	if a.readHook != nil {
		if err := a.readHook(ctx, path); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.api.read(ctx, path, out)
}

func TestAdmissionReserveNeverExtendsItsContextOrPublishesAfterIt(t *testing.T) {
	f := newQueueFixture(t)
	if err := f.q.init(context.Background()); err != nil {
		t.Fatal(err)
	}
	policy, err := f.q.policy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	trace := &admissionTrace{PR: f.pr, Policy: &policy, Run: &f.feedbackRun}
	f.c.api = deadlineAPI{api: f.api, readHook: func(ctx context.Context, _ string) error { <-ctx.Done(); return ctx.Err() }}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := f.q.refuseAdmission(ctx, trace); err == nil {
		t.Fatal("reserve API hang returned success")
	}
	if time.Since(started) > time.Second || len(f.api.writes) != 0 {
		t.Fatal("reserve was extended or wrote without fresh identity")
	}
}

func TestAdmissionDeadlineCannotTurnIncompleteFeedbackIntoSuccess(t *testing.T) {
	for _, status := range []string{"queued", "in_progress"} {
		t.Run(status, func(t *testing.T) {
			f := newQueueFixture(t)
			f.feedbackRun.Status, f.feedbackRun.Conclusion = status, ""
			f.feedbackRefresh()
			f.c.verify = func(ctx context.Context, _ int, _ string) (policyDecision, error) {
				<-ctx.Done()
				return policyDecision{}, ctx.Err()
			}
			b := admissionBudget{Started: time.Now().Add(-45 * time.Second), Deadline: time.Now().Add(admissionReserve + 25*time.Millisecond)}
			if err := f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), b); err == nil {
				t.Fatal("incomplete feedback timeout passed")
			}
			f.assertNoSuccess(t)
			if f.api.writes[len(f.api.writes)-1]["conclusion"] != "failure" {
				t.Fatal("publisher deadline failed to produce a blocking terminal result")
			}
		})
	}
}

func TestAdmissionFailurePublicationRejectsForeignCheckAndLostWrite(t *testing.T) {
	for _, kind := range []string{"foreign final check", "write transport failure", "read transport failure"} {
		t.Run(kind, func(t *testing.T) {
			f := newQueueFixture(t)
			f.c.verify = func(context.Context, int, string) (policyDecision, error) {
				if kind == "write transport failure" {
					f.api.writeErr = errors.New("write response lost")
				}
				if kind == "read transport failure" {
					f.api.responses[f.c.endpoint("pulls/7")] = errors.New("read unavailable")
				}
				return policyDecision{}, errors.New("admission stopped")
			}
			if kind == "foreign final check" {
				f.api.readHook = func(path string, n int) {
					if strings.Contains(path, "/check-runs?") && n == 3 {
						foreign := check{ID: 2000, Name: queueCheckName, HeadSHA: f.pr.Head.SHA, Status: "completed", Conclusion: "success"}
						foreign.App.ID, foreign.App.Slug = actionsAppID, "github-actions"
						f.api.checks = append(f.api.checks, foreign)
						f.api.nextID = 2001
					}
				}
			}
			if err := f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), admissionBudget{Started: time.Now(), Deadline: time.Now().Add(time.Minute)}); err == nil {
				t.Fatal("failure recovery passed")
			}
			f.assertNoSuccess(t)
			for _, w := range f.api.writes {
				if w["id"] == int64(2000) {
					t.Fatal("foreign check mutated")
				}
			}
			if kind == "write transport failure" && len(f.api.writes) != 2 {
				t.Fatalf("lost write was retried: %d writes", len(f.api.writes))
			}
		})
	}
}
