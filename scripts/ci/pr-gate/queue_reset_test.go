package main

import (
	"context"
	"errors"
	"testing"
)

type resetFailureAPI struct {
	api
	retainCompletion bool
	resets           int
}

func (a *resetFailureAPI) write(ctx context.Context, method, path string, payload, result any) error {
	m := payload.(map[string]any)
	reset := method == "PATCH" && m["status"] == "queued"
	if reset {
		a.resets++
		if !a.retainCompletion {
			return errors.New("HTTP 422: nullable terminal fields rejected")
		}
	}
	if err := a.api.write(ctx, method, path, payload, result); err != nil {
		return err
	}
	if reset {
		result.(*check).CompletedAt = "2026-10-12T20:48:14Z"
	}
	return nil
}

func TestQueueTransitionPreservesCreationAndLegacyPayload(t *testing.T) {
	for _, role := range []string{"queue transition", "queue create", "legacy reset"} {
		t.Run(role, func(t *testing.T) {
			f := newQueueFixture(t)
			b := newBinding(f.pr.Base.Repo.ID, f.pr, &f.feedbackRun)
			old := check{ID: 99, Name: queueCheckName, HeadSHA: f.pr.Head.SHA, ExternalID: b.externalID(), Status: "completed", Conclusion: "success", CompletedAt: "2026-10-12T20:48:14Z"}
			old.App.ID, old.App.Slug = actionsAppID, "github-actions"
			var selected *check
			if role != "queue create" {
				selected = &old
				f.api.checks = []check{old}
			}
			if role == "legacy reset" {
				f.c.checkName, old.Name = "ci-required-next", "ci-required-next"
				f.api.checks[0] = old
			}
			if err := f.c.publish(context.Background(), f.pr, b, selected, pending("new proof required")); err != nil {
				t.Fatal(err)
			}
			w := f.api.writes[0]
			for _, key := range []string{"conclusion", "completed_at"} {
				if _, present := w[key]; present || w["method"] != "POST" {
					t.Fatal("pending generations and legacy creation must omit terminal fields")
				}
			}
			if role == "queue transition" && f.api.checks[0].Status != "completed" {
				t.Fatal("prior terminal verdict was mutated")
			}

		})
	}
}

func TestUnsupportedOrIncompletePendingUpdateRemainsBlocking(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected", true: "completion-retained"}[retained], func(t *testing.T) {
			f := newQueueFixture(t)
			b := newBinding(f.pr.Base.Repo.ID, f.pr, &f.feedbackRun)
			old := check{ID: 99, Name: queueCheckName, HeadSHA: b.Head, ExternalID: b.externalID(), Status: "queued"}
			old.App.ID, old.App.Slug = actionsAppID, "github-actions"
			f.api.checks = []check{old}
			a := &resetFailureAPI{api: f.api, retainCompletion: retained}
			f.c.api = a
			if err := f.q.reassess(context.Background(), func(ctx context.Context) error {
				return f.c.publish(ctx, f.pr, b, &old, pending("fresh proof required"))
			}); err == nil || a.resets != 1 {
				t.Fatal("unsupported/incomplete production reset accepted or retried")
			}
			latest := f.api.checks[len(f.api.checks)-1]
			if !blockingQueueCheck(&latest) {
				t.Fatal("blocking uncertainty barrier missing")
			}
		})
	}
}
