package main

import (
	"context"
	"fmt"
	"testing"
)

func TestRecurringQueueProofDoesNotGrowOwnedHistory(t *testing.T) {
	for _, role := range []string{"admission", "group"} {
		t.Run(role, func(t *testing.T) {
			f := newQueueFixture(t)
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
					t.Fatalf("refresh %d failed with %d historical checks: %v", i, len(f.api.checks), err)
				}
				if i == 0 {
					initial = len(f.api.checks)
				}
				if len(f.api.checks) != initial {
					t.Fatalf("unchanged refresh %d grew history from %d to %d", i, initial, len(f.api.checks))
				}
				if got := f.api.checks[len(f.api.checks)-1]; got.Status != "completed" || got.Conclusion != "success" {
					t.Fatalf("fresh proof not completed: %+v", got)
				}
			}
		})
	}
}

func TestQueueReassessmentAtExistingHistoryBoundaryRemainsUsable(t *testing.T) {
	for _, role := range []string{"admission", "group"} {
		t.Run(role, func(t *testing.T) {
			f := newQueueFixture(t)
			p, r, count := f.pr, f.feedbackRun, 1000
			if role == "group" {
				r = f.groupRun
				p.Head.SHA, p.Base.Ref = r.HeadSHA, r.HeadBranch
				f.c.expectedHead, count = r.HeadSHA, 100
			}
			b := newBinding(p.Base.Repo.ID, p, &r)
			for i := 1; i <= count; i++ {
				v := check{ID: int64(i), Name: queueCheckName, HeadSHA: p.Head.SHA, ExternalID: b.externalID(), Status: "completed", Conclusion: "success"}
				v.App.ID, v.App.Slug = actionsAppID, "github-actions"
				f.api.checks = append(f.api.checks, v)
			}
			f.api.nextID = int64(count + 1)
			e := f.event
			e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":%q}]`, p.Head.SHA, role)
			if err := f.q.runHead(context.Background(), "workflow_dispatch", e, role); err != nil {
				t.Fatalf("supported history at boundary became unusable: %v", err)
			}
			if len(f.api.checks) != count {
				t.Fatal("reassessment crossed the existing checked history boundary")
			}
		})
	}
}

type cancelledResetAPI struct {
	api
	cancel context.CancelFunc
}

func (a *cancelledResetAPI) write(ctx context.Context, method, path string, payload, result any) error {
	if method == "PATCH" && payload.(map[string]any)["status"] == "queued" {
		a.cancel()
		return fmt.Errorf("ambiguous reset transport failure")
	}
	return a.api.write(ctx, method, path, payload, result)
}
func TestCancelledUncertainResetUsesIndependentBlockingReserve(t *testing.T) {
	f := newQueueFixture(t)
	old := check{ID: 99, Name: queueCheckName, HeadSHA: f.pr.Head.SHA, ExternalID: newBinding(f.pr.Base.Repo.ID, f.pr, &f.feedbackRun).externalID(), Status: "completed", Conclusion: "success"}
	old.App.ID, old.App.Slug = actionsAppID, "github-actions"
	f.api.checks = []check{old}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.c.api = &cancelledResetAPI{api: f.api, cancel: cancel}
	e := f.event
	e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, f.pr.Head.SHA)
	if err := f.q.runHead(ctx, "workflow_dispatch", e, "admission"); err == nil {
		t.Fatal("uncertain reset was treated as success")
	}
	latest := f.api.checks[len(f.api.checks)-1]
	if latest.Status != "queued" || latest.Conclusion != "" || latest.ID == old.ID {
		t.Fatalf("prior green survived uncertain reset: %+v", latest)
	}
	if len(f.api.writes) != 1 || f.api.writes[0]["method"] != "POST" {
		t.Fatal("uncertain PATCH was retried or blocking publication missing")
	}
}
