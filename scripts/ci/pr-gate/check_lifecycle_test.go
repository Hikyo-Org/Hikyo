package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type lifecycleAPI struct {
	api
	fake   *fakeAPI
	mutate func(string, string, map[string]any, *check) error
}

func (a *lifecycleAPI) read(ctx context.Context, path string, result any) error {
	if strings.Contains(path, "/check-runs/") && !strings.Contains(path, "?") {
		for _, v := range a.fake.checks {
			if strings.HasSuffix(path, fmt.Sprintf("/check-runs/%d", v.ID)) {
				b, _ := json.Marshal(v)
				return decode(b, result)
			}
		}
		return errors.New("unknown canary ID")
	}
	return a.api.read(ctx, path, result)
}
func (a *lifecycleAPI) write(ctx context.Context, method, path string, payload, result any) error {
	b, _ := json.Marshal(payload)
	var m map[string]any
	if err := decode(b, &m); err != nil {
		return err
	}
	if err := a.api.write(ctx, method, path, payload, result); err != nil {
		return err
	}
	if a.mutate != nil {
		return a.mutate(method, path, m, result.(*check))
	}
	return nil
}
func newLifecycleFixture(t *testing.T) (*fixture, *lifecycleAPI, event) {
	t.Helper()
	q := newQueueFixture(t)
	f := q.fixture
	t.Setenv("GITHUB_RUN_ID", "501")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	t.Setenv("GITHUB_SHA", f.pr.Base.SHA)
	f.api.responses[f.c.endpoint("actions/workflows/ci-check-lifecycle-probe.yml")] = workflow{ID: 31, Path: lifecyclePath, Name: lifecycleWorkflowName}
	f.api.responses[f.c.endpoint("actions/runs/501")] = run{ID: 501, Attempt: 1, WorkflowID: 31, Path: lifecyclePath, Event: "workflow_dispatch", HeadSHA: f.pr.Base.SHA, HeadBranch: "main", Status: "in_progress", Repository: f.pr.Base.Repo, HeadRepository: f.pr.Base.Repo}
	f.api.responses[f.c.endpoint("actions/runs/501/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 1, "jobs": []map[string]any{{"id": 41, "run_id": 501, "run_attempt": 1, "name": "lifecycle-probe", "head_sha": f.pr.Base.SHA, "status": "in_progress", "started_at": time.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339)}}}
	a := &lifecycleAPI{api: f.api, fake: f.api}
	f.c.api = a
	e := f.event
	e.Ref = "refs/heads/main"
	e.Inputs.PR, e.Inputs.Work = "", ""
	return f, a, e
}
func TestLifecycleProbeUsesExactProductionPayloadAndCleansItsOwnedID(t *testing.T) {
	f, _, e := newLifecycleFixture(t)
	var receipt lifecycleReceipt
	if err := f.c.checkLifecycle(context.Background(), "workflow_dispatch", e, &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.ResetAccepted || !receipt.CleanupVerified || receipt.Canary.Name != lifecycleName || receipt.Source.SHA != f.pr.Base.SHA || receipt.Source.RunID != 501 || receipt.Source.Attempt != 1 {
		t.Fatalf("incomplete receipt: %+v", receipt)
	}
	if len(f.api.checks) != 1 || len(f.api.writes) != 3 {
		t.Fatalf("canary did not retain one ID: checks%d writes%d", len(f.api.checks), len(f.api.writes))
	}
	if f.api.checks[0].ID != receipt.Canary.ID || f.api.checks[0].Status != "completed" || f.api.checks[0].Conclusion != "neutral" {
		t.Fatal("canary not cleaned up neutral")
	}
	queued := f.api.writes[1]
	if _, ok := queued["conclusion"]; ok {
		t.Fatal("reset supplied conclusion instead of omitting it")
	}
	if _, ok := queued["completed_at"]; ok {
		t.Fatal("reset supplied completion timestamp")
	}
	binding, err := decodeBinding(receipt.Canary.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := f.c.checkPayload(binding, pending("Exact production queued reset payload. No prior success supplies authority."))
	if err != nil {
		t.Fatal(err)
	}
	delete(queued, "method")
	delete(queued, "path")
	got, _ := json.Marshal(queued)
	expected, _ := json.Marshal(want)
	if string(got) != string(expected) {
		t.Fatalf("probe payload drift: %s vs %s", got, expected)
	}
}
func TestLifecycleProbeInvalidAuthorityOrJobClockCannotMutate(t *testing.T) {
	for _, name := range []string{"PR event", "other ref", "caller inputs", "wrong attempt", "wrong source", "wrong workflow", "expired setup", "duplicate job", "future clock", "missing clock", "required canary"} {
		t.Run(name, func(t *testing.T) {
			f, _, e := newLifecycleFixture(t)
			eventName := "workflow_dispatch"
			jobPath := f.c.endpoint("actions/runs/501/attempts/1/jobs?per_page=100&page=1")
			jobs := f.api.responses[jobPath].(map[string]any)
			rows := jobs["jobs"].([]map[string]any)
			switch name {
			case "PR event":
				eventName = "pull_request_target"
			case "other ref":
				e.Ref = "refs/heads/feature"
			case "caller inputs":
				e.Inputs.PR = "893"
			case "wrong attempt":
				t.Setenv("GITHUB_RUN_ATTEMPT", "2")
			case "wrong source":
				t.Setenv("GITHUB_SHA", strings.Repeat("c", 40))
			case "wrong workflow":
				r := f.api.responses[f.c.endpoint("actions/runs/501")].(run)
				r.Path = ".github/workflows/ci-feedback.yml"
				f.api.responses[f.c.endpoint("actions/runs/501")] = r
			case "expired setup":
				rows[0]["started_at"] = time.Now().Add(-121 * time.Second).UTC().Format(time.RFC3339)
			case "future clock":
				rows[0]["started_at"] = time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
			case "missing clock":
				rows[0]["started_at"] = nil
			case "required canary":
				f.api.responses[f.c.endpoint("rules/branches/main")] = []rule{encodedRule("required_status_checks", map[string]any{"strict_required_status_checks_policy": false, "required_status_checks": []map[string]string{{"context": lifecycleName}}})}
			case "duplicate job":
				jobs["jobs"] = []map[string]any{rows[0], rows[0]}
				jobs["total_count"] = 2
			}
			var receipt lifecycleReceipt
			if err := f.c.checkLifecycle(context.Background(), eventName, e, &receipt); err == nil {
				t.Fatal("invalid source/clock accepted")
			}
			if len(f.api.writes) != 0 {
				t.Fatal("mutation preceded source/clock authentication")
			}
		})
	}
}
func TestLifecycleProbeRefusalsKeepFalseReceiptAndAttemptOwnedCleanup(t *testing.T) {
	for _, name := range []string{"reset transport", "retained success", "different ID", "foreign initial identity", "cleanup failure", "main movement"} {
		t.Run(name, func(t *testing.T) {
			f, a, e := newLifecycleFixture(t)
			a.mutate = func(method, path string, m map[string]any, v *check) error {
				if method == "POST" && name == "foreign initial identity" {
					v.App.ID++
					return nil
				}
				if method == "PATCH" && m["status"] == "queued" {
					switch name {
					case "reset transport":
						return errors.New("lost reset receipt")
					case "retained success":
						v.Status = "completed"
						v.Conclusion = "success"
					case "different ID":
						v.ID++
					case "main movement":
						f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": strings.Repeat("c", 40)}}
					}
				}
				if method == "PATCH" && m["conclusion"] == "neutral" && name == "cleanup failure" {
					return errors.New("cleanup unavailable")
				}
				return nil
			}
			var receipt lifecycleReceipt
			if err := f.c.checkLifecycle(context.Background(), "workflow_dispatch", e, &receipt); err == nil {
				t.Fatal("invalid lifecycle passed")
			}
			if name == "cleanup failure" {
				if receipt.CleanupVerified {
					t.Fatal("cleanup error hidden")
				}
			} else if receipt.ResetAccepted {
				t.Fatal("false lifecycle acceptance")
			}
			if name == "foreign initial identity" {
				if len(f.api.writes) != 1 {
					t.Fatal("foreign created identity was mutated")
				}
			} else if name != "cleanup failure" && !receipt.CleanupVerified {
				t.Fatal("known owned canary was not cleaned up")
			}
			resets := 0
			for _, w := range f.api.writes {
				if w["status"] == "queued" {
					resets++
				}
			}
			if resets > 1 {
				t.Fatal("uncertain reset mutation retried")
			}
		})
	}
}
