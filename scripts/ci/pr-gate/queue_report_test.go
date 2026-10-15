package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type queueReportAPI struct {
	*fakeAPI
	statuses []map[string]string
	path     string
}

func (a *queueReportAPI) write(_ context.Context, method, path string, payload, out any) error {
	if method != "POST" || !strings.Contains(path, "/statuses/") {
		return errors.New("reporter tried a non-status mutation")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var status map[string]string
	if err := decode(data, &status); err != nil {
		return err
	}
	a.statuses = append(a.statuses, status)
	a.path = path
	data, err = json.Marshal(map[string]any{"id": 9, "context": status["context"], "state": status["state"]})
	if err != nil {
		return err
	}
	return decode(data, out)
}

func queueReportFixture(t *testing.T) (*fixture, *queueReportAPI, event, run) {
	t.Helper()
	f := newFixture(t)
	a := &queueReportAPI{fakeAPI: f.api}
	f.c.api = a
	r := f.source
	r.ID, r.WorkflowID, r.Event, r.Path = 300, 22, "merge_group", queueWorkflowPath
	r.HeadBranch = "gh-readonly-queue/main/pr-7-" + f.pr.Base.SHA
	r.HeadSHA, r.Conclusion = strings.Repeat("d", 40), "failure"
	f.api.responses[f.c.endpoint("actions/workflows/ci-merge-group.yml")] = workflow{ID: 22, Name: "trusted-merge-ci", Path: queueWorkflowPath}
	f.api.responses[f.c.endpoint("actions/runs/300")] = r
	f.api.responses[f.c.endpoint("git/commits/"+r.HeadSHA)] = map[string]any{"sha": r.HeadSHA, "parents": []map[string]string{{"sha": f.pr.Base.SHA}, {"sha": f.pr.Head.SHA}}}
	f.api.responses[f.c.endpoint("actions/workflows/ci-merge-group.yml/runs?event=merge_group&per_page=100&page=1")] = map[string]any{"workflow_runs": []run{r}}
	e := f.event
	e.Run = r
	return f, a, e, r
}

func TestQueueReportSurvivesEviction(t *testing.T) {
	for _, state := range []string{"failure", "success", "cancelled", "in_progress"} {
		t.Run(state, func(t *testing.T) {
			f, a, e, r := queueReportFixture(t)
			r.Conclusion = state
			want := "failure"
			if state == "in_progress" {
				r.Status, r.Conclusion, want = "in_progress", "", "pending"
			} else if state == "success" {
				want = "success"
			}
			f.api.responses[f.c.endpoint("actions/runs/300")] = r
			if err := f.c.reportQueue(context.Background(), "workflow_run", e); err != nil {
				t.Fatal(err)
			}
			if len(a.statuses) != 1 || a.path != f.c.endpoint("statuses/"+f.pr.Head.SHA) || a.statuses[0]["context"] != queueReportContext || a.statuses[0]["state"] != want || a.statuses[0]["target_url"] != "https://github.com/Hikyo-Org/Hikyo/actions/runs/300/attempts/1" {
				t.Fatalf("wrong PR visibility: %s %+v", a.path, a.statuses)
			}
			if f.verifyCalls != 0 || len(f.api.writes) != 0 {
				t.Fatal("advisory report touched protected publication")
			}
		})
	}
}

func TestQueueReportRejectsWrongOrSupersededSource(t *testing.T) {
	for _, scenario := range []string{"wrong event", "wrong repository", "wrong workflow", "wrong workflow path", "foreign run repository", "foreign head repository", "not merge group", "malformed branch", "wrong base parent", "wrong merge identity", "not a two-parent merge", "closed PR", "retargeted PR", "moved head", "new attempt", "newer group", "late move", "late attempt", "history unavailable", "dry run"} {
		t.Run(scenario, func(t *testing.T) {
			f, a, e, r := queueReportFixture(t)
			name := "workflow_run"
			wantError := false
			switch scenario {
			case "wrong event":
				name, wantError = "pull_request_target", true
			case "wrong repository":
				e.Repository.ID, wantError = 99, true
			case "wrong workflow":
				r.WorkflowID, wantError = 99, true
			case "wrong workflow path":
				r.Path, wantError = ".github/workflows/attacker.yml", true
			case "foreign run repository":
				r.Repository.ID, wantError = 99, true
			case "foreign head repository":
				r.HeadRepository.ID, wantError = 99, true
			case "not merge group":
				r.Event, wantError = "pull_request", true
			case "malformed branch":
				r.HeadBranch, wantError = "gh-readonly-queue/main/pr-7-unknown", true
			case "wrong base parent":
				r.HeadBranch, wantError = "gh-readonly-queue/main/pr-7-"+strings.Repeat("e", 40), true
			case "wrong merge identity":
				wantError = true
				f.api.responses[f.c.endpoint("git/commits/"+r.HeadSHA)] = map[string]any{"sha": strings.Repeat("e", 40), "parents": []map[string]string{{"sha": f.pr.Base.SHA}, {"sha": f.pr.Head.SHA}}}
			case "not a two-parent merge":
				wantError = true
				f.api.responses[f.c.endpoint("git/commits/"+r.HeadSHA)] = map[string]any{"sha": r.HeadSHA, "parents": []map[string]string{{"sha": f.pr.Head.SHA}}}
			case "closed PR":
				f.pr.State = "closed"
				f.refresh()
			case "retargeted PR":
				f.pr.Base.Ref = "other"
				f.refresh()
			case "moved head":
				f.pr.Head.SHA = strings.Repeat("f", 40)
				f.refresh()
			case "new attempt":
				r.Attempt++
			case "newer group":
				newer := r
				newer.ID++
				newer.HeadBranch = "gh-readonly-queue/main/pr-7-" + strings.Repeat("e", 40)
				f.api.responses[f.c.endpoint("actions/workflows/ci-merge-group.yml/runs?event=merge_group&per_page=100&page=1")] = map[string]any{"workflow_runs": []run{newer, r}}
			case "late move":
				f.api.readHook = func(path string, count int) {
					if path == f.c.endpoint("pulls/7") && count == 2 {
						f.pr.Head.SHA = strings.Repeat("f", 40)
						f.refresh()
					}
				}
			case "late attempt":
				f.api.readHook = func(path string, count int) {
					if path == f.c.endpoint("actions/runs/300") && count == 3 {
						changed := r
						changed.Attempt++
						f.api.responses[path] = changed
					}
				}
			case "history unavailable":
				wantError = true
				f.api.responses[f.c.endpoint("actions/workflows/ci-merge-group.yml/runs?event=merge_group&per_page=100&page=1")] = errors.New("API unavailable")
			case "dry run":
				f.c.dryRun = true
			}
			f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/%d", r.ID))] = r
			err := f.c.reportQueue(context.Background(), name, e)
			if (err != nil) != wantError {
				t.Fatalf("error = %v, wantError=%t", err, wantError)
			}
			if len(a.statuses) != 0 || len(f.api.writes) != 0 {
				t.Fatalf("wrong source was published: %+v", a.statuses)
			}
		})
	}
}
