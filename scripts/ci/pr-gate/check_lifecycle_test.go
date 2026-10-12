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
	fake           *fakeAPI
	mutate         func(string, string, map[string]any, *check) error
	beforeRead     func(string)
	latestResponse func([]check) any
}

func (a *lifecycleAPI) read(ctx context.Context, path string, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.beforeRead != nil {
		a.beforeRead(path)
	}
	if strings.Contains(path, "/check-runs/") && !strings.Contains(path, "?") {
		for _, v := range a.fake.checks {
			if strings.HasSuffix(path, fmt.Sprintf("/check-runs/%d", v.ID)) {
				b, _ := json.Marshal(v)
				return decode(b, result)
			}
		}
		return errors.New("unknown canary ID")
	}
	if strings.Contains(path, "/check-runs?") && strings.Contains(path, "check_name="+lifecycleName) {
		var rows []check
		for _, v := range a.fake.checks {
			if v.Name == lifecycleName {
				rows = append(rows, v)
			}
		}
		if rows == nil {
			rows = []check{}
		}
		var response any
		if strings.Contains(path, "filter=latest") {
			if a.latestResponse != nil {
				response = a.latestResponse(rows)
			} else {
				var latest *check
				for i := range rows {
					if latest == nil || rows[i].ID > latest.ID {
						latest = &rows[i]
					}
				}
				rows = []check{}
				if latest != nil {
					rows = append(rows, *latest)
				}
			}
		}
		if response == nil {
			response = map[string]any{"total_count": len(rows), "check_runs": rows}
		}
		b, _ := json.Marshal(response)
		return decode(b, result)
	}
	return a.api.read(ctx, path, result)
}
func (a *lifecycleAPI) write(ctx context.Context, method, path string, payload, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, _ := json.Marshal(payload)
	var body map[string]any
	if err := decode(b, &body); err != nil {
		return err
	}
	if err := a.api.write(ctx, method, path, payload, result); err != nil {
		return err
	}
	v := result.(*check)
	if v.Status == "completed" && v.CompletedAt == "" {
		v.CompletedAt = time.Now().UTC().Format(time.RFC3339)
		for i := range a.fake.checks {
			if a.fake.checks[i].ID == v.ID {
				a.fake.checks[i].CompletedAt = v.CompletedAt
			}
		}
	}
	if a.mutate != nil {
		return a.mutate(method, path, body, v)
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
func TestLifecycleProbeUsesSharedImmutableVerdictsAndCleansAllIDs(t *testing.T) {
	f, _, e := newLifecycleFixture(t)
	var receipt lifecycleReceipt
	if err := f.c.checkLifecycle(context.Background(), "workflow_dispatch", e, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Version != 2 || receipt.Protocol != lifecycleProtocol || !receipt.LifecycleAccepted || !receipt.CleanupVerified || receipt.Source.SHA != f.pr.Base.SHA || receipt.Source.RunID != 501 || receipt.Source.Attempt != 1 {
		t.Fatalf("incomplete receipt: %+v", receipt)
	}
	if len(f.api.checks) != 2 || len(f.api.writes) != 7 || len(receipt.Canaries) != 2 || len(receipt.Cleanups) != 2 || receipt.HistoryAfter != receipt.HistoryBefore+2 {
		t.Fatalf("unexpected history/counts: %+v writes%d", receipt, len(f.api.writes))
	}
	a, b := receipt.Stages.Initial.ID, receipt.Stages.Barrier.ID
	if a == b || receipt.EffectivePending.ID != b || receipt.Stages.Reuse.ID != b || receipt.Stages.Finalized.ID != b || receipt.Stages.Noop.ID != b {
		t.Fatal("probe did not retain distinct generations and reuse only the pending ID")
	}
	want := []lifecycleCounts{{1, 1}, {2, 1}, {2, 2}, {2, 3}, {2, 3}, {2, 5}}
	got := []lifecycleCounts{receipt.Operations.Initial, receipt.Operations.Barrier, receipt.Operations.Reuse, receipt.Operations.Finalized, receipt.Operations.Noop, receipt.Operations.Cleanup}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("stage%d counts%+v want%+v", i, got[i], want[i])
		}
	}
	for _, v := range f.api.checks {
		if v.Status != "completed" || v.Conclusion != "neutral" || v.CompletedAt == "" {
			t.Fatalf("canary not neutral: %+v", v)
		}
	}
	for _, w := range f.api.writes {
		if w["status"] == "queued" {
			for _, key := range []string{"conclusion", "completed_at"} {
				if _, present := w[key]; present {
					t.Fatalf("pending payload supplied terminal field %s", key)
				}
			}
			if w["method"] == "PATCH" && w["path"] != f.c.endpoint(fmt.Sprintf("check-runs/%d", b)) {
				t.Fatal("terminal ID was patched back to queued")
			}
		}
	}
	if f.api.writes[2]["method"] != "POST" || f.api.writes[3]["method"] != "PATCH" || f.api.writes[4]["path"] != f.c.endpoint(fmt.Sprintf("check-runs/%d", b)) {
		t.Fatal("probe publication path drifted")
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
func TestLifecycleProbeFailuresNeverAcceptAndCleanEveryKnownID(t *testing.T) {
	for _, name := range []string{"fresh transport", "retained success", "retained completion", "wrong fresh ID", "foreign initial App", "finalize transport", "first cleanup failure", "old green changed", "main movement"} {
		t.Run(name, func(t *testing.T) {
			f, a, e := newLifecycleFixture(t)
			a.mutate = func(method, path string, body map[string]any, v *check) error {
				if method == "POST" && len(f.api.checks) == 1 && name == "foreign initial App" {
					v.App.ID++
					return nil
				}
				if method == "POST" && len(f.api.checks) == 2 {
					switch name {
					case "fresh transport":
						return errors.New("lost fresh creation receipt")
					case "retained success":
						v.Status = "completed"
						v.Conclusion = "success"
					case "retained completion":
						v.CompletedAt = "2026-10-12T20:48:14Z"
					case "wrong fresh ID":
						v.ID++
					case "old green changed":
						f.api.checks[0].Conclusion = "failure"
					case "main movement":
						f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": strings.Repeat("c", 40)}}
					}
				}
				if method == "PATCH" && len(f.api.checks) == 2 && body["conclusion"] == "success" && name == "finalize transport" {
					return errors.New("lost finalization receipt")
				}
				if method == "PATCH" && body["conclusion"] == "neutral" && len(f.api.checks) > 0 && path == f.c.endpoint(fmt.Sprintf("check-runs/%d", f.api.checks[0].ID)) && name == "first cleanup failure" {
					return errors.New("first cleanup refused")
				}
				return nil
			}
			var receipt lifecycleReceipt
			if err := f.c.checkLifecycle(context.Background(), "workflow_dispatch", e, &receipt); err == nil || receipt.LifecycleAccepted {
				t.Fatal("invalid lifecycle passed")
			}
			if name == "foreign initial App" {
				if len(f.api.writes) != 1 || len(receipt.Cleanups) != 0 {
					t.Fatal("foreign identity was guessed or patched")
				}
				return
			}
			if name == "wrong fresh ID" || name == "first cleanup failure" {
				if receipt.CleanupVerified {
					t.Fatal("failed cleanup accepted")
				}
			}
			if name == "first cleanup failure" {
				if len(receipt.Cleanups) != 1 || receipt.Cleanups[0].ID != f.api.checks[1].ID {
					t.Fatal("second owned ID cleanup was skipped after first failure")
				}
			}
			posts := 0
			for _, w := range f.api.writes {
				if w["method"] == "POST" {
					posts++
				}
			}
			if posts > 2 {
				t.Fatal("uncertain create was retried")
			}
			for _, v := range receipt.Cleanups {
				if v.Status != "completed" || v.Conclusion != "neutral" {
					t.Fatal("owned ID cleanup not confirmed")
				}
			}
		})
	}
}
func TestLifecycleProbeRequiresProviderLatestPendingNotOnlyHighestID(t *testing.T) {
	for _, name := range []string{"old green", "missing count", "partial list", "duplicate rows", "foreign latest", "retained timestamp"} {
		t.Run(name, func(t *testing.T) {
			f, a, e := newLifecycleFixture(t)
			a.latestResponse = func(rows []check) any {
				latest := rows[len(rows)-1]
				switch name {
				case "old green":
					latest = rows[0]
				case "missing count":
					return map[string]any{"check_runs": []check{latest}}
				case "partial list":
					return map[string]any{"total_count": 2, "check_runs": []check{latest}}
				case "duplicate rows":
					return map[string]any{"total_count": 2, "check_runs": []check{latest, latest}}
				case "foreign latest":
					latest.App.ID++
				case "retained timestamp":
					latest.CompletedAt = "2026-10-12T20:48:14Z"
				}
				return map[string]any{"total_count": 1, "check_runs": []check{latest}}
			}
			var receipt lifecycleReceipt
			if err := f.c.checkLifecycle(context.Background(), "workflow_dispatch", e, &receipt); err == nil || receipt.LifecycleAccepted || !receipt.CleanupVerified {
				t.Fatal("unproved provider selection accepted or known cleanup lost")
			}
		})
	}
}
func TestLifecycleProbeCancellationUsesFreshCommonCleanupReserve(t *testing.T) {
	f, a, e := newLifecycleFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.mutate = func(method, path string, body map[string]any, v *check) error {
		if method == "POST" && len(f.api.checks) == 2 {
			cancel()
		}
		return nil
	}
	var receipt lifecycleReceipt
	if err := f.c.checkLifecycle(ctx, "workflow_dispatch", e, &receipt); !errors.Is(err, context.Canceled) || receipt.LifecycleAccepted || !receipt.CleanupVerified || len(receipt.Cleanups) != 2 {
		t.Fatalf("cleanup reserve not independent: %+v err%v", receipt, err)
	}
}
func TestLifecycleProbeForeignHistoryCannotCreateCanaries(t *testing.T) {
	f, _, e := newLifecycleFixture(t)
	v := check{ID: 99, Name: lifecycleName, HeadSHA: f.pr.Base.SHA, ExternalID: "native", Status: "completed", Conclusion: "success"}
	v.App.ID = actionsAppID
	v.App.Slug = "github-actions"
	f.api.checks = append(f.api.checks, v)
	var receipt lifecycleReceipt
	if err := f.c.checkLifecycle(context.Background(), "workflow_dispatch", e, &receipt); err == nil || len(f.api.writes) != 0 || receipt.LifecycleAccepted {
		t.Fatal("foreign same-name history was ignored")
	}
}

func TestLifecycleProbeReceiptWireContractRejectsOldResetProtocol(t *testing.T) {
	f, _, e := newLifecycleFixture(t)
	var receipt lifecycleReceipt
	if err := f.c.checkLifecycle(context.Background(), "workflow_dispatch", e, &receipt); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := decode(data, &wire); err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"canary", "initial", "reset", "reset_response", "reset_accepted", "cleanup"} {
		if _, present := wire[old]; present {
			t.Fatalf("historical reset protocol field retained: %s", old)
		}
	}
	for _, required := range []string{"version", "protocol", "repository", "source", "canaries", "stages", "effective_pending", "history_before", "history_after", "operations", "cleanups", "lifecycle_accepted", "cleanup_verified"} {
		if _, present := wire[required]; !present {
			t.Fatalf("missing wire contract field: %s", required)
		}
	}
	t.Log(string(data))
}
