package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type immutableCheckAPI struct {
	api
	fake *fakeAPI
}

func (a *immutableCheckAPI) write(ctx context.Context, method, path string, payload, result any) error {
	m := payload.(map[string]any)
	if method == "PATCH" && m["status"] == "queued" {
		for _, old := range a.fake.checks {
			if path == fmt.Sprintf("repos/Hikyo-Org/Hikyo/check-runs/%d", old.ID) && old.Status == "completed" {
				return errors.New("terminal check cannot become pending")
			}
		}
	}
	for _, key := range []string{"conclusion", "completed_at"} {
		if value, present := m[key]; present && value == nil {
			return errors.New("terminal reset fields are not nullable")
		}
	}
	return a.api.write(ctx, method, path, payload, result)
}

func TestImmutableQueueProofHealthyReassessmentHasConstantHistory(t *testing.T) {
	for _, role := range []string{"admission", "group"} {
		t.Run(role, func(t *testing.T) {
			f := newQueueFixture(t)
			f.c.api = &immutableCheckAPI{api: f.api, fake: f.api}
			head := f.pr.Head.SHA
			if role == "group" {
				head = f.groupRun.HeadSHA
				f.c.expectedHead = head
			}
			e := f.event
			e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":%q}]`, head, role)
			initial := 0
			for i := 0; i < 1101; i++ {
				if err := f.q.runHead(context.Background(), "workflow_dispatch", e, role); err != nil {
					t.Fatalf("refresh %d: %v", i, err)
				}
				if i == 0 {
					initial = len(f.api.checks)
				}
				if len(f.api.checks) != initial {
					t.Fatalf("healthy refresh grew history from %d to %d", initial, len(f.api.checks))
				}
			}
		})
	}
}

type ambiguousVerdictAPI struct {
	api
	fake    *fakeAPI
	cancel  context.CancelFunc
	changed bool
}

func (a *ambiguousVerdictAPI) read(ctx context.Context, path string, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.api.read(ctx, path, result)
}
func (a *ambiguousVerdictAPI) write(ctx context.Context, method, path string, payload, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.api.write(ctx, method, path, payload, result); err != nil {
		return err
	}
	if !a.changed && method == "PATCH" && payload.(map[string]any)["conclusion"] == "success" {
		a.changed = true
		a.cancel()
		return errors.New("server completed green but transport lost the response")
	}
	return nil
}
func TestImmutableQueueUncertainTerminalWriteRechecksServerState(t *testing.T) {
	f := newQueueFixture(t)
	b := newBinding(f.c.repo.ID, f.pr, &f.feedbackRun)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &ambiguousVerdictAPI{api: f.api, fake: f.api, cancel: cancel}
	f.c.api = a
	err := f.q.reassess(ctx, func(ctx context.Context) error {
		return f.c.publish(ctx, f.pr, b, nil, outcome{Status: "completed", Conclusion: "success", Title: "verified", Summary: "current proof"})
	})
	if err == nil || !a.changed {
		t.Fatal("uncertain server-side mutation was not exercised/refused")
	}
	newest := newestQueueCheck(f.api.checks)
	if !blockingQueueCheck(newest) || newest.Status != "queued" || len(f.api.checks) != 2 {
		t.Fatalf("cached pre-PATCH pending hid actual server green: %+v", f.api.checks)
	}
	if f.api.checks[0].Conclusion != "success" {
		t.Fatal("test did not preserve the acknowledged server-side terminal mutation")
	}
	if len(f.api.writes) != 3 || f.api.writes[2]["method"] != "POST" {
		t.Fatal("uncertain terminal PATCH retried or fresh blocking generation missing")
	}
}

func TestImmutableQueueQueuedNoopCannotHideRetainedCompletion(t *testing.T) {
	f := newQueueFixture(t)
	b := newBinding(f.c.repo.ID, f.pr, &f.feedbackRun)
	result := pending("identical output")
	v := check{ID: 99, Name: queueCheckName, HeadSHA: b.Head, ExternalID: b.externalID(), Status: "queued", CompletedAt: "2026-10-12T20:48:14Z"}
	v.App.ID, v.App.Slug = actionsAppID, "github-actions"
	v.Output.Title, v.Output.Summary = result.Title, result.Summary
	if _, err := f.c.publishQueueRecord(context.Background(), f.pr, b, &v, result); err == nil {
		t.Fatal("retained completion escaped through equality no-op")
	}
	if len(f.api.writes) != 0 {
		t.Fatal("contradictory pending identity was mutated by the helper")
	}
}

func TestImmutableQueueAcknowledgedUnexpectedCreateRemainsKnown(t *testing.T) {
	f := newQueueFixture(t)
	b := newBinding(f.c.repo.ID, f.pr, &f.feedbackRun)
	a := &unexpectedCreationAPI{api: f.api}
	f.c.api = a
	f.c.queueRefresh = &queueReassessment{}
	written, err := f.c.publishQueueRecord(context.Background(), f.pr, b, nil, pending("current proof incomplete"))
	if err == nil || written == nil || f.c.queueRefresh.latest == nil || f.c.queueRefresh.latest.ID != written.ID {
		t.Fatal("acknowledged owned identity was lost before state rejection")
	}
}

type unexpectedCreationAPI struct{ api }

func (a *unexpectedCreationAPI) write(ctx context.Context, method, path string, payload, result any) error {
	if err := a.api.write(ctx, method, path, payload, result); err != nil {
		return err
	}
	v := result.(*check)
	v.Status, v.Conclusion = "completed", "success"
	return nil
}

func TestImmutableQueueFailedCreationFallsBackOnlyToAuthenticatedOwnedID(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint(foreign), func(t *testing.T) {
			f := newQueueFixture(t)
			b := newBinding(f.c.repo.ID, f.pr, &f.feedbackRun)
			old := check{ID: 99, Name: queueCheckName, HeadSHA: b.Head, ExternalID: b.externalID(), Status: "completed", Conclusion: "success"}
			old.App.ID, old.App.Slug = actionsAppID, "github-actions"
			if foreign {
				old.App.ID++
			}
			f.api.checks = []check{old}
			f.c.api = &rejectCreationAPI{api: f.api}
			err := f.q.reassess(context.Background(), func(context.Context) error { return errors.New("current authority unavailable") })
			if err == nil {
				t.Fatal("unconfirmed creation reported success")
			}
			if foreign {
				if len(f.api.writes) != 0 || f.api.checks[0] != old {
					t.Fatal("foreign identity was patched by fallback")
				}
			} else {
				if len(f.api.writes) != 1 || f.api.writes[0]["method"] != "PATCH" || !blockingQueueCheck(&f.api.checks[0]) {
					t.Fatal("confirmed owned prior green did not receive genuine failure fallback")
				}
			}
		})
	}
}

type rejectCreationAPI struct{ api }

func (a *rejectCreationAPI) write(ctx context.Context, method, path string, payload, result any) error {
	if method == "POST" {
		return errors.New("queued creation transport failed")
	}
	return a.api.write(ctx, method, path, payload, result)
}

func TestImmutableQueueIncompleteExitsSupersedePriorGreen(t *testing.T) {
	for _, scenario := range []string{"policy read", "inactive policy", "missing useful run", "running useful run", "closed PR with active same-SHA sibling", "PR API", "workflow initialization"} {
		t.Run(scenario, func(t *testing.T) {
			f := newQueueFixture(t)
			if err := f.admit(); err != nil {
				t.Fatal(err)
			}
			initial := len(f.api.checks)
			f.q.targeted = true
			switch scenario {
			case "policy read":
				f.api.responses[f.c.endpoint(fmt.Sprintf("rulesets/%d", queueRulesetID))] = errors.New("policy unavailable")
			case "inactive policy":
				f.rules = f.rules[:len(f.rules)-1]
				f.policyRefresh()
			case "missing useful run":
				f.api.responses[f.c.endpoint(fmt.Sprintf("actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))] = map[string]any{"total_count": 0, "workflow_runs": []run{}}
			case "running useful run":
				f.feedbackRun.Status, f.feedbackRun.Conclusion = "in_progress", ""
				f.feedbackRefresh()
			case "closed PR with active same-SHA sibling":
				other := f.pr
				other.Number++
				f.pr.State = "closed"
				f.refresh()
				f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{other}
			case "PR API":
				f.api.responses[f.c.endpoint("pulls/7")] = errors.New("PR unavailable")
			case "workflow initialization":
				f.api.responses[f.c.endpoint("actions/workflows/ci-candidate.yml")] = errors.New("workflow metadata unavailable")
			}
			err := f.q.execute(context.Background(), "workflow_dispatch", f.event)
			if scenario == "policy read" || scenario == "PR API" || scenario == "workflow initialization" {
				if err == nil {
					t.Fatal("API failure erased")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			newest := newestQueueCheck(f.api.checks)
			if !blockingQueueCheck(newest) || len(f.api.checks) != initial+1 {
				t.Fatalf("early exit retained effective green or grew more than one generation: %+v", f.api.checks)
			}
			// Repeated incomplete reconciliation reuses the authenticated blocker.
			_ = f.q.execute(context.Background(), "workflow_dispatch", f.event)
			if len(f.api.checks) != initial+1 {
				t.Fatal("incomplete reassessment grows owned history")
			}
		})
	}
}
