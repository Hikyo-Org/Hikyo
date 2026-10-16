package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

type fakeAPI struct {
	values  map[string]any
	reads   map[string]int
	hook    func(string, int)
	writes  int
	payload any
	fault   string
}

func copyJSON(from, to any) error {
	b, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, to)
}

func (a *fakeAPI) read(_ context.Context, path string, out any) error {
	a.reads[path]++
	if a.hook != nil {
		a.hook(path, a.reads[path])
	}
	v, ok := a.values[path]
	if !ok {
		return fmt.Errorf("unexpected read %s", path)
	}
	if a.writes > 0 && a.fault == "readback" {
		return errors.New("lost readback")
	}
	return copyJSON(v, out)
}

func (a *fakeAPI) patch(_ context.Context, path string, payload, out any) error {
	a.writes++
	a.payload = payload
	c := a.values[path].(check)
	c.Status = "completed"
	if a.fault == "output" {
		c.Output = map[string]any{"summary": "changed"}
	}
	a.values[path] = c
	if a.fault == "lost receipt" {
		return errors.New("lost receipt")
	}
	if err := copyJSON(c, out); err != nil {
		return err
	}
	if a.fault == "metadata" {
		out.(*check).Document["unexpected_metadata"] = "changed"
	}
	return nil
}

func fixture(t *testing.T) (*repairer, *fakeAPI) {
	t.Helper()
	a := &fakeAPI{values: map[string]any{}, reads: map[string]int{}}
	r := &repairer{api: a, repo: repository{ID: 10, Name: "Hikyo-Org/Hikyo", Default: "main"}}
	head, main := strings.Repeat("a", 40), strings.Repeat("b", 40)
	base := "https://api.github.com/repos/Hikyo-Org/Hikyo/"
	html := "https://github.com/Hikyo-Org/Hikyo/actions/runs/100/job/99"
	c := check{ID: 99, Node: "CR_99", Name: "validation / changes", Head: head, External: "95632b3d-7e05-54c9-8130-9cd8c0cf81db", URL: base + "check-runs/99", HTML: html, Details: html, Status: "in_progress", Conclusion: "success", Started: "2026-08-23T11:15:15Z", Completed: "2026-08-23T11:15:21Z", Output: map[string]any{"title": nil, "summary": nil, "text": nil, "annotations_count": float64(0)}}
	c.Suite.ID = 88
	c.App.ID = 15368
	c.App.Slug = "github-actions"
	j := job{ID: 99, Node: c.Node, Name: c.Name, Run: 100, Attempt: 1, Workflow: "trusted-ci", Head: head, URL: base + "actions/jobs/99", HTML: html, CheckURL: c.URL, RunURL: base + "actions/runs/100", Status: c.Status, Conclusion: c.Conclusion, Started: c.Started, Completed: c.Completed}
	if err := json.Unmarshal([]byte(`[{"number":1,"status":"completed","conclusion":"success"}]`), &j.Steps); err != nil {
		t.Fatal(err)
	}
	v := run{ID: 100, Attempt: 1, WorkflowID: 20, Name: j.Workflow, Path: ".github/workflows/ci-control.yml", Event: "pull_request_target", Head: head, Status: "completed", Conclusion: "success", Suite: 88, SuiteURL: base + "check-suites/88", Repo: r.repo, HeadRepo: r.repo}
	a.values[r.endpoint("check-runs/99")] = c
	a.values[r.endpoint("actions/jobs/99")] = j
	a.values[r.endpoint("actions/runs/100")] = v
	a.values[r.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": main}}
	a.values[r.endpoint("compare/"+head+"..."+main+"?per_page=1")] = map[string]any{"status": "ahead", "base_commit": map[string]string{"sha": head}, "merge_base_commit": map[string]string{"sha": head}}
	return r, a
}

func TestStatusRepairPreservesExistingSuccessfulVerdict(t *testing.T) {
	for _, mode := range []string{"repair", "dry run", "already terminal"} {
		t.Run(mode, func(t *testing.T) {
			r, a := fixture(t)
			path := r.endpoint("check-runs/99")
			prior := a.values[path].(check)
			if mode == "already terminal" {
				prior.Status = "completed"
				a.values[path] = prior
			}
			if err := r.repair(context.Background(), 99, mode == "dry run"); err != nil {
				t.Fatal(err)
			}
			want := 0
			if mode == "repair" {
				want = 1
			}
			if a.writes != want {
				t.Fatalf("writes=%d", a.writes)
			}
			if mode == "repair" {
				if !reflect.DeepEqual(a.payload, map[string]string{"status": "completed", "conclusion": prior.Conclusion, "completed_at": prior.Completed}) {
					t.Fatalf("repair changed fields beyond status: %+v", a.payload)
				}
				prior.Status = "completed"
			}
			if !reflect.DeepEqual(a.values[path], prior) {
				t.Fatal("existing verdict/provenance/output changed")
			}
		})
	}
}

func TestStatusRepairRefusesAmbiguousOrUnfinishedAuthority(t *testing.T) {
	for _, fault := range []string{"check ID", "name", "head", "external", "app", "details URL", "check conclusion", "check timestamp", "job ID", "job attempt", "job node", "job check URL", "job run URL", "job conclusion", "job date", "unfinished step", "failed step", "run running", "run failure", "run suite", "run head", "run repo", "run head repo", "run source", "run attempt", "not ancestor", "foreign main"} {
		t.Run(fault, func(t *testing.T) {
			r, a := fixture(t)
			cp, jp, rp := r.endpoint("check-runs/99"), r.endpoint("actions/jobs/99"), r.endpoint("actions/runs/100")
			c, j, v := a.values[cp].(check), a.values[jp].(job), a.values[rp].(run)
			switch fault {
			case "check ID":
				c.ID++
			case "name":
				c.Name = "ci-queue-required"
			case "head":
				c.Head = "bad"
			case "external":
				c.External = "synthetic"
			case "app":
				c.App.ID++
			case "details URL":
				c.Details += "?foreign=1"
			case "check conclusion":
				c.Conclusion = ""
			case "check timestamp":
				c.Completed = ""
			case "job ID":
				j.ID++
			case "job attempt":
				j.Attempt++
			case "job node":
				j.Node = "other"
			case "job check URL":
				j.CheckURL = "other"
			case "job run URL":
				j.RunURL = "other"
			case "job conclusion":
				j.Conclusion = ""
			case "job date":
				j.Completed = "2026-08-23T11:16:21Z"
			case "unfinished step":
				j.Steps[0].Status = "in_progress"
			case "failed step":
				j.Steps[0].Conclusion = "failure"
			case "run running":
				v.Status = "in_progress"
			case "run failure":
				v.Conclusion = "failure"
			case "run suite":
				v.Suite++
			case "run head":
				v.Head = strings.Repeat("c", 40)
			case "run repo":
				v.Repo.ID++
			case "run head repo":
				v.HeadRepo.ID++
			case "run source":
				v.Path = ".github/workflows/ci.yml"
			case "run attempt":
				v.Attempt++
			case "not ancestor":
				for path := range a.values {
					if strings.Contains(path, "/compare/") {
						a.values[path] = map[string]string{"status": "diverged"}
					}
				}
			case "foreign main":
				a.values[r.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/other", "object": map[string]string{"type": "commit", "sha": strings.Repeat("b", 40)}}
			}
			a.values[cp], a.values[jp], a.values[rp] = c, j, v
			if err := r.repair(context.Background(), 99, false); err == nil || a.writes != 0 {
				t.Fatalf("ambiguous authority mutated: writes=%d err=%v", a.writes, err)
			}
		})
	}
}

func TestStatusRepairRenewsAuthorityAndNeverRetriesUncertainMutation(t *testing.T) {
	for _, fault := range []string{"changed source", "changed attempt", "changed output", "lost receipt", "output", "metadata", "readback"} {
		t.Run(fault, func(t *testing.T) {
			r, a := fixture(t)
			a.fault = fault
			a.hook = func(path string, n int) {
				if n != 2 {
					return
				}
				if fault == "changed source" && strings.Contains(path, "/actions/runs/") {
					v := a.values[path].(run)
					v.Conclusion = "failure"
					a.values[path] = v
				}
				if fault == "changed attempt" && strings.Contains(path, "/actions/jobs/") {
					j := a.values[path].(job)
					j.Attempt++
					a.values[path] = j
				}
				if fault == "changed output" && strings.Contains(path, "/check-runs/") {
					c := a.values[path].(check)
					c.Output = map[string]any{"summary": "changed"}
					a.values[path] = c
				}
			}
			err := r.repair(context.Background(), 99, false)
			want := 1
			if strings.HasPrefix(fault, "changed ") {
				want = 0
			}
			if err == nil || a.writes != want {
				t.Fatalf("uncertain mutation retried or authority ignored: writes=%d err=%v", a.writes, err)
			}
		})
	}
}

func TestStatusRepairWorkflowIsManualAndIndependent(t *testing.T) {
	raw, err := os.ReadFile("../../../.github/workflows/ci-check-status-repair.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, required := range []string{"workflow_dispatch:", "required: true", "checks: write", "actions: read", "ref: ${{ github.sha }}", "persist-credentials: false", "cancel-in-progress: false", "--check-id \"$CHECK_ID\""} {
		if !strings.Contains(s, required) {
			t.Fatalf("missing boundary %s", required)
		}
	}
	for _, forbidden := range []string{"schedule:", "push:", "pull_request:", "workflow_run:", "needs:", "secrets.", "actions: write"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("unexpected execution surface %s", forbidden)
		}
	}
}
