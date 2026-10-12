package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type queueFixture struct {
	*fixture
	q           *queueController
	rules       []rule
	owner       map[string]any
	feedbackRun run
	groupRun    run
	query       string
	entry       queueEntry
}

func encodedRule(kind string, p any) rule {
	data, _ := json.Marshal(p)
	return rule{Type: kind, Parameters: data}
}
func newQueueFixture(t *testing.T) *queueFixture {
	t.Helper()
	f := newFixture(t)
	f.c.checkName = queueCheckName
	f.api.scopeChecksToHead = true
	q := &queueFixture{fixture: f, q: &queueController{controller: f.c, dco: func(context.Context, pull) (int, error) { return 0, nil }}}
	q.rules = []rule{encodedRule("deletion", nil), encodedRule("non_fast_forward", nil), encodedRule("required_signatures", nil), encodedRule("pull_request", map[string]bool{"required_review_thread_resolution": true, "dismiss_stale_reviews_on_push": true}), encodedRule("required_status_checks", map[string]any{"strict_required_status_checks_policy": false, "required_status_checks": []map[string]string{{"context": queueCheckName}}}), encodedRule("merge_queue", queueParameters{Build: 1, Minimum: 1, Maximum: 1, Wait: 0, Timeout: 60, Strategy: "ALLGREEN", Method: "MERGE"})}
	q.owner = map[string]any{"id": queueRulesetID, "target": "branch", "enforcement": "active", "bypass_actors": []any{}, "conditions": map[string]any{"ref_name": map[string]any{"include": []string{"refs/heads/main"}, "exclude": []string{}}}, "rules": q.rules}
	q.policyRefresh()
	q.feedbackRun = run{ID: 101, Attempt: 1, WorkflowID: 21, Path: feedbackWorkflowPath, Name: "ci-feedback", Event: "pull_request", HeadSHA: f.pr.Head.SHA, Status: "completed", Conclusion: "success", Repository: f.pr.Base.Repo, HeadRepository: f.pr.Head.Repo, Pulls: []pull{f.pr}}
	q.feedbackRefresh()
	q.groupRun = run{ID: 102, Attempt: 1, WorkflowID: 22, Path: queueWorkflowPath, Name: "trusted-merge-ci", Event: "merge_group", HeadSHA: strings.Repeat("d", 40), HeadBranch: "gh-readonly-queue/main/pr-7-abc", Status: "completed", Conclusion: "success", Repository: f.pr.Base.Repo, HeadRepository: f.pr.Base.Repo}
	q.groupRun.References = f.source.References
	q.groupRun.References[0].Path = f.c.repoName + "/.github/workflows/ci.yml@" + q.groupRun.HeadSHA
	q.groupRun.References[0].Ref = "refs/heads/" + q.groupRun.HeadBranch
	q.groupRun.References[0].SHA = q.groupRun.HeadSHA
	q.entry.ID = "ENTRY7"
	q.entry.Position = 1
	q.entry.State = "AWAITING_CHECKS"
	q.entry.Head.OID = q.groupRun.HeadSHA
	q.entry.Base.OID = f.pr.Base.SHA
	q.entry.PR.Number = 7
	q.entry.PR.Head = f.pr.Head.SHA
	q.entry.PR.Base = "main"
	q.query = `graphql:query{repository(owner:"Hikyo-Org",name:"Hikyo"){mergeQueue(branch:"main"){id entries(first:100){nodes{id position state headCommit{oid} baseCommit{oid} pullRequest{number headRefOid baseRefName}} pageInfo{hasNextPage endCursor}}}}}`
	q.groupRefresh()
	return q
}
func (f *queueFixture) policyRefresh() {
	f.owner["rules"] = f.rules
	f.api.responses[f.c.endpoint(fmt.Sprintf("rulesets/%d", queueRulesetID))] = f.owner
	f.api.responses[f.c.endpoint("rules/branches/main")] = f.rules
}
func (f *queueFixture) feedbackRefresh() {
	r := f.feedbackRun
	f.api.responses[f.c.endpoint("actions/workflows/ci-feedback.yml")] = workflow{ID: 21, Path: feedbackWorkflowPath, Name: "ci-feedback"}
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))] = map[string]any{"total_count": 1, "workflow_runs": []run{r}}
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", r.ID, r.Attempt))] = map[string]any{"total_count": 2, "jobs": []job{{ID: 201, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (go)", Status: "completed", Conclusion: "success"}, {ID: 202, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (frontend)", Status: "completed", Conclusion: "success"}}}
}
func (f *queueFixture) groupRefresh() {
	r := f.groupRun
	f.api.responses[f.c.endpoint("actions/workflows/ci-merge-group.yml/runs?event=merge_group&head_sha="+r.HeadSHA+"&per_page=100&page=1")] = map[string]any{"total_count": 1, "workflow_runs": []run{r}}
	f.api.responses[f.c.endpoint("compare/"+f.pr.Base.SHA+"..."+r.HeadSHA)] = map[string]any{"status": "ahead", "base_commit": map[string]string{"sha": f.pr.Base.SHA}, "merge_base_commit": map[string]string{"sha": f.pr.Base.SHA}}
	f.api.responses[f.c.endpoint("actions/workflows/ci-merge-group.yml")] = workflow{ID: 22, Path: queueWorkflowPath, Name: "trusted-merge-ci"}
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/%d", r.ID))] = r
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", r.ID, r.Attempt))] = map[string]any{"total_count": 2, "jobs": []job{{ID: 203, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "validation / ci-required", Status: "completed", Conclusion: "success"}, {ID: 204, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "merge-group-proof", Status: "completed", Conclusion: "success"}}}
	f.api.responses[f.query] = map[string]any{"data": map[string]any{"repository": map[string]any{"mergeQueue": map[string]any{"id": "QUEUE_MAIN", "entries": map[string]any{"nodes": []queueEntry{f.entry}, "pageInfo": map[string]bool{"hasNextPage": false}}}}}}
	f.api.responses[f.c.endpoint("git/ref/heads/gh-readonly-queue%2Fmain%2Fpr-7-abc")] = map[string]any{"ref": "refs/heads/" + r.HeadBranch, "object": map[string]string{"type": "commit", "sha": r.HeadSHA}}
	for _, path := range []string{queueWorkflowPath, ".github/workflows/ci.yml"} {
		for _, revision := range []string{f.pr.Base.SHA, r.HeadSHA} {
			f.api.responses[f.c.endpoint("contents/"+path+"?ref="+revision)] = map[string]string{"type": "file", "path": path, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("read-only full workflow"))}
		}
	}
}
func (f *queueFixture) admit() error {
	if err := f.q.init(context.Background()); err != nil {
		return err
	}
	return f.q.admit(context.Background(), 7)
}
func (f *queueFixture) group() error {
	e := f.event
	e.Run = f.groupRun
	return f.q.execute(context.Background(), "workflow_run", e)
}
func (f *queueFixture) assertNoSuccess(t *testing.T) {
	t.Helper()
	for _, w := range f.api.writes {
		if w["conclusion"] == "success" {
			t.Fatalf("unsafe success: %+v", w)
		}
	}
}

func TestQueueAdmissionRejectsLateNativeCheckCollision(t *testing.T) {
	f := newQueueFixture(t)
	injected := false
	f.api.readHook = func(path string, n int) {
		if strings.Contains(path, "actions/runs/101/attempts/1/jobs?") && n == 2 {
			injected = true
			foreign := check{ID: 2000, Name: queueCheckName, HeadSHA: f.pr.Head.SHA, Status: "completed", Conclusion: "success"}
			foreign.App.ID = actionsAppID
			foreign.App.Slug = "github-actions"
			f.api.checks = append(f.api.checks, foreign)
			f.api.nextID = 2001
		}
	}
	if err := f.admit(); err == nil {
		t.Fatal("late native check collision was accepted")
	}
	if !injected {
		t.Fatal("collision did not occur during final feedback verification")
	}
	f.assertNoSuccess(t)
	for _, write := range f.api.writes {
		if write["id"] == int64(2000) {
			t.Fatal("foreign check was mutated")
		}
	}
}

func TestQueueGroupRejectsFinalWorkflowIdentityMovement(t *testing.T) {
	f := newQueueFixture(t)
	injected := false
	f.api.readHook = func(path string, n int) {
		if strings.Contains(path, "actions/workflows/ci-merge-group.yml/runs?") && n == 2 {
			injected = true
			f.groupRun.WorkflowID = 99
			f.groupRefresh()
		}
	}
	if err := f.group(); err == nil {
		t.Fatal("different final workflow identity was accepted")
	}
	if !injected {
		t.Fatal("workflow identity did not change during final run verification")
	}
	f.assertNoSuccess(t)
}

func TestQueueAdmissionRequiresMandatoryDistinctProtection(t *testing.T) {
	for name, mutate := range map[string]func(*queueFixture){
		"queue absent": func(f *queueFixture) { f.rules = f.rules[:len(f.rules)-1]; f.policyRefresh() },
		"disabled":     func(f *queueFixture) { f.owner["enforcement"] = "disabled" },
		"bypass": func(f *queueFixture) {
			f.owner["bypass_actors"] = []map[string]any{{"actor_type": "RepositoryRole", "actor_id": 5, "bypass_mode": "always"}}
		},
		"legacy context": func(f *queueFixture) {
			f.rules[4] = encodedRule("required_status_checks", map[string]any{"strict_required_status_checks_policy": false, "required_status_checks": []map[string]string{{"context": "ci-required-next"}}})
			f.policyRefresh()
		},
		"strict branch rebasing": func(f *queueFixture) {
			f.rules[4] = encodedRule("required_status_checks", map[string]any{"strict_required_status_checks_policy": true, "required_status_checks": []map[string]string{{"context": queueCheckName}}})
			f.policyRefresh()
		},
		"effective queue missing": func(f *queueFixture) { f.api.responses[f.c.endpoint("rules/branches/main")] = f.rules[:len(f.rules)-1] },
		"concurrent builds": func(f *queueFixture) {
			f.rules[5] = encodedRule("merge_queue", queueParameters{Build: 2, Minimum: 1, Maximum: 1, Timeout: 60, Strategy: "ALLGREEN", Method: "MERGE"})
			f.policyRefresh()
		},
		"API unavailable": func(f *queueFixture) {
			f.api.responses[f.c.endpoint("rules/branches/main")] = errors.New("API unavailable")
		},
	} {
		t.Run(name, func(t *testing.T) { f := newQueueFixture(t); mutate(f); _ = f.admit(); f.assertNoSuccess(t) })
	}
	f := newQueueFixture(t)
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	if got := f.api.writes[len(f.api.writes)-1]; got["conclusion"] != "success" || got["name"] != queueCheckName {
		t.Fatalf("admission: %+v", got)
	}
	// A queue-only green cannot satisfy the restored full-proof context. No
	// queue code path ever writes ci-required-next, even after policy movement.
	for _, w := range f.api.writes {
		if w["name"] == "ci-required-next" {
			t.Fatal("queue publisher wrote legacy authorization")
		}
	}
}
func TestQueueAdmissionRejectsIgnoredSkippedDuplicateAndFailedFeedback(t *testing.T) {
	for name, mutate := range map[string]func(*queueFixture){
		"metadata-only success": func(f *queueFixture) {
			f.api.responses[f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 2, "jobs": []job{{ID: 201, RunID: 101, Attempt: 1, Name: "metadata (ignored) (go)", Status: "completed", Conclusion: "skipped"}, {ID: 202, RunID: 101, Attempt: 1, Name: "metadata (ignored) (frontend)", Status: "completed", Conclusion: "skipped"}}}
		},
		"missing lanes": func(f *queueFixture) {
			f.api.responses[f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 0, "jobs": []job{}}
		},
		"failed useful run": func(f *queueFixture) { f.feedbackRun.Conclusion = "failure"; f.feedbackRefresh() },
		"wrong attempt": func(f *queueFixture) {
			f.feedbackRun.Attempt = 2
			f.feedbackRefresh()
			f.api.responses[f.c.endpoint("actions/runs/101/attempts/2/jobs?per_page=100&page=1")] = map[string]any{"total_count": 1, "jobs": []job{{ID: 201, RunID: 101, Attempt: 1, Name: "feedback (go)", Status: "completed", Conclusion: "success"}}}
		},
		"vouch or review revoked": func(f *queueFixture) { f.verifyCode = 1 },
	} {
		t.Run(name, func(t *testing.T) { f := newQueueFixture(t); mutate(f); _ = f.admit(); f.assertNoSuccess(t) })
	}
}
func TestQueueFullGroupProofAndNegativeIdentity(t *testing.T) {
	f := newQueueFixture(t)
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	last := f.api.writes[len(f.api.writes)-1]
	if last["conclusion"] != "success" || last["head_sha"] != f.groupRun.HeadSHA {
		t.Fatalf("group proof: %+v", last)
	}
	decoded, err := decodeBinding(last["external_id"].(string))
	if err != nil || decoded.Head != f.groupRun.HeadSHA || decoded.Base != f.pr.Base.SHA || decoded.Run != 102 || decoded.Attempt != 1 || decoded.PR != 7 {
		t.Fatalf("binding %+v %v", decoded, err)
	}
	for name, mutate := range map[string]func(*queueFixture){
		"PR head is not group":  func(f *queueFixture) { f.entry.Head.OID = f.pr.Head.SHA; f.groupRefresh() },
		"current main advanced": func(f *queueFixture) { f.pr.Base.SHA = strings.Repeat("e", 40); f.refresh() },
		"member head changed":   func(f *queueFixture) { f.entry.PR.Head = strings.Repeat("f", 40); f.groupRefresh() },
		"member removed":        func(f *queueFixture) { f.pr.State = "closed"; f.refresh() },
		"partial membership": func(f *queueFixture) {
			f.api.responses[f.query] = map[string]any{"data": map[string]any{"repository": map[string]any{"mergeQueue": map[string]any{"id": "QUEUE_MAIN", "entries": map[string]any{"nodes": []queueEntry{f.entry}, "pageInfo": map[string]bool{"hasNextPage": true}}}}}}
		},
		"reordered membership":   func(f *queueFixture) { f.entry.Position = 2; f.groupRefresh() },
		"wrong source merge ref": func(f *queueFixture) { f.groupRun.References[0].Ref = "refs/pull/7/merge"; f.groupRefresh() },
		"attempt superseded": func(f *queueFixture) {
			r := f.groupRun
			r.Attempt = 2
			f.api.responses[f.c.endpoint("actions/runs/102")] = r
		},
		"unapproved contract": func(f *queueFixture) {
			f.api.responses[f.c.endpoint("contents/"+queueWorkflowPath+"?ref="+f.groupRun.HeadSHA)] = map[string]string{"type": "file", "path": queueWorkflowPath, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("spoof"))}
		},
		"missing full proof": func(f *queueFixture) {
			f.api.responses[f.c.endpoint("actions/runs/102/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 0, "jobs": []job{}}
		},
		"authority revoked": func(f *queueFixture) { f.verifyCode = 1 },
		"API failure":       func(f *queueFixture) { f.api.responses[f.query] = errors.New("API failure") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			mutate(f)
			_ = f.group()
			f.assertNoSuccess(t)
			if len(f.api.writes) == 0 || f.api.writes[0]["status"] != "queued" {
				t.Fatal("no blocking result before mutable group reads")
			}
		})
	}
}
func TestQueueFinalMembershipAndPolicyMovementCannotPublishSuccess(t *testing.T) {
	for name, mutate := range map[string]func(*queueFixture){
		"queue changes": func(f *queueFixture) {
			f.api.readHook = func(path string, n int) {
				if path == f.query && n == 2 {
					f.entry.ID = "REPLACEMENT"
					f.groupRefresh()
				}
			}
		},
		"policy rolls back": func(f *queueFixture) {
			f.api.readHook = func(path string, n int) {
				if strings.Contains(path, "rulesets/") && n == 2 {
					f.rules = f.rules[:5]
					f.policyRefresh()
				}
			}
		},
		"authority revoked": func(f *queueFixture) {
			f.api.readHook = func(path string, n int) {
				if path == f.query && n == 2 {
					f.verifyCode = 1
				}
			}
		},
		"source read unavailable": func(f *queueFixture) {
			f.api.responses[f.c.endpoint("contents/"+queueWorkflowPath+"?ref="+f.pr.Base.SHA)] = errors.New("API failure")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			mutate(f)
			if err := f.group(); err == nil {
				t.Fatal("moving group accepted")
			}
			f.assertNoSuccess(t)
		})
	}
}

func TestQueueLegitimatePrefixAdvanceKeepsFullProof(t *testing.T) {
	f := newQueueFixture(t)
	old := f.pr.Base.SHA
	f.pr.Base.SHA = strings.Repeat("e", 40)
	f.refresh()
	f.groupRefresh()
	for _, path := range []string{"scripts/ci/classify-changed-paths.sh", "scripts/ci/ci-job-registry.json", "scripts/ci/check-required-jobs.sh", "scripts/ci/analysis-shards-go/main.go", "scripts/ci/test-race-packages.sh"} {
		for _, sha := range []string{old, f.pr.Base.SHA} {
			f.api.responses[f.c.endpoint("contents/"+path+"?ref="+sha)] = map[string]string{"type": "file", "path": path, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("same trusted full validation definition"))}
		}
	}
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	last := f.api.writes[len(f.api.writes)-1]
	if last["conclusion"] != "success" {
		t.Fatalf("valid prefix: %+v", last)
	}
	b, err := decodeBinding(last["external_id"].(string))
	if err != nil || b.Base != f.pr.Base.SHA {
		t.Fatalf("not bound to advanced main: %+v %v", b, err)
	}
	for _, name := range []string{"unrelated main", "changed registry", "missing current required step"} {
		t.Run(name, func(t *testing.T) {
			g := newQueueFixture(t)
			old := g.pr.Base.SHA
			g.pr.Base.SHA = strings.Repeat("e", 40)
			g.refresh()
			g.groupRefresh()
			for _, path := range []string{"scripts/ci/classify-changed-paths.sh", "scripts/ci/ci-job-registry.json", "scripts/ci/check-required-jobs.sh", "scripts/ci/analysis-shards-go/main.go", "scripts/ci/test-race-packages.sh"} {
				for _, sha := range []string{old, g.pr.Base.SHA} {
					g.api.responses[g.c.endpoint("contents/"+path+"?ref="+sha)] = map[string]string{"type": "file", "path": path, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("same full definition"))}
				}
			}
			switch name {
			case "unrelated main":
				g.api.responses[g.c.endpoint("compare/"+g.pr.Base.SHA+"..."+g.groupRun.HeadSHA)] = map[string]any{"status": "diverged", "base_commit": map[string]string{"sha": g.pr.Base.SHA}, "merge_base_commit": map[string]string{"sha": old}}
			case "changed registry":
				path := "scripts/ci/ci-job-registry.json"
				g.api.responses[g.c.endpoint("contents/"+path+"?ref="+g.pr.Base.SHA)] = map[string]string{"type": "file", "path": path, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("new mandatory domain"))}
			case "missing current required step":
				path := ".github/workflows/ci.yml"
				g.api.responses[g.c.endpoint("contents/"+path+"?ref="+g.pr.Base.SHA)] = map[string]string{"type": "file", "path": path, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("new mandatory full step"))}
			}
			if err := g.group(); err == nil && name != "unrelated main" {
				t.Fatal("invalid old-base reuse accepted")
			}
			g.assertNoSuccess(t)
		})
	}
}
func TestQueueDelayedOldAttemptReconcilesNewestAttempt(t *testing.T) {
	f := newQueueFixture(t)
	old := f.groupRun
	f.groupRun.Attempt = 2
	f.groupRefresh()
	e := f.event
	e.Run = old
	if err := f.q.execute(context.Background(), "workflow_run", e); err != nil {
		t.Fatal(err)
	}
	b, err := decodeBinding(f.api.writes[len(f.api.writes)-1]["external_id"].(string))
	if err != nil || b.Attempt != 2 {
		t.Fatalf("stale attempt: %+v %v", b, err)
	}
}
func TestQueueForkVouchIsRequiredAtAdmissionAndGroupPublication(t *testing.T) {
	for _, where := range []string{"admission", "group", "final group"} {
		t.Run(where, func(t *testing.T) {
			f := newQueueFixture(t)
			f.pr.Head.Repo = repository{ID: 50, FullName: "fork/Hikyo"}
			f.refresh()
			f.feedbackRun.HeadRepository = f.pr.Head.Repo
			f.feedbackRun.Pulls = []pull{f.pr}
			f.feedbackRefresh()
			f.groupRefresh()
			if where != "final group" {
				f.api.responses[f.c.endpoint("contents/.github/VOUCHED.td?ref=main")] = map[string]string{"type": "file", "path": ".github/VOUCHED.td", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("github:someoneelse"))}
			}
			if where == "final group" {
				f.pr.User.Login = "valentinhl"
				f.api.readHook = func(path string, n int) {
					if path == f.query && n == 2 {
						f.pr.User.Login = "not-vouched"
						f.refresh()
					}
				}
			}
			if where == "admission" {
				_ = f.admit()
			} else {
				_ = f.group()
			}
			f.assertNoSuccess(t)
		})
	}
}
func TestQueueFinalRefAndForeignIdentityRacesStayBlocked(t *testing.T) {
	for _, name := range []string{"ref rebuilt", "foreign check", "check API failure", "missing page flag", "invalid state", "duplicate useful lane"} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			switch name {
			case "ref rebuilt":
				f.api.readHook = func(path string, n int) {
					if strings.Contains(path, "git/ref/heads/gh-readonly") && n == 2 {
						f.api.responses[path] = map[string]any{"ref": "refs/heads/" + f.groupRun.HeadBranch, "object": map[string]string{"type": "commit", "sha": strings.Repeat("f", 40)}}
					}
				}
			case "foreign check":
				f.api.readHook = func(path string, n int) {
					if strings.Contains(path, "/check-runs?") && n == 2 {
						v := check{ID: 2000, Name: queueCheckName, HeadSHA: f.groupRun.HeadSHA, Status: "completed", Conclusion: "success"}
						v.App.ID = actionsAppID
						v.App.Slug = "github-actions"
						f.api.checks = append(f.api.checks, v)
						f.api.nextID = 2001
					}
				}
			case "check API failure":
				f.c.api = failedQueueRead{api: f.api, contains: "/check-runs?"}
			case "missing page flag":
				f.api.responses[f.query] = map[string]any{"data": map[string]any{"repository": map[string]any{"mergeQueue": map[string]any{"id": "QUEUE_MAIN", "entries": map[string]any{"nodes": []queueEntry{f.entry}, "pageInfo": map[string]any{}}}}}}
			case "invalid state":
				f.entry.State = "UNKNOWN"
				f.groupRefresh()
			case "duplicate useful lane":
				f.api.responses[f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 2, "jobs": []job{{ID: 201, RunID: 101, Attempt: 1, HeadSHA: f.pr.Head.SHA, Name: "feedback (go)", Status: "completed", Conclusion: "success"}, {ID: 202, RunID: 101, Attempt: 1, HeadSHA: f.pr.Head.SHA, Name: "feedback (go)", Status: "completed", Conclusion: "success"}}}
			}
			if name == "duplicate useful lane" {
				_ = f.admit()
			} else {
				_ = f.group()
			}
			f.assertNoSuccess(t)
		})
	}
}
func TestQueueIgnoresActualSkippedMatrixMetadataRun(t *testing.T) {
	f := newQueueFixture(t)
	metadata := f.feedbackRun
	metadata.ID = 103
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))] = map[string]any{"total_count": 2, "workflow_runs": []run{metadata, f.feedbackRun}}
	f.api.responses[f.c.endpoint("actions/runs/103/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 1, "jobs": []job{{ID: 301, RunID: 103, Attempt: 1, Name: skippedFeedbackName, Status: "completed", Conclusion: "skipped"}}}
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	if f.api.writes[len(f.api.writes)-1]["conclusion"] != "success" {
		t.Fatal("metadata skip hid successful useful feedback")
	}
}

type failedQueueRead struct {
	api
	contains string
}

func (f failedQueueRead) read(ctx context.Context, path string, result any) error {
	if strings.Contains(path, f.contains) {
		return errors.New("deliberate queue API read failure")
	}
	return f.api.read(ctx, path, result)
}

func TestQueueAdmissionReusesUsefulHeadFeedbackAfterMainAdvance(t *testing.T) {
	f := newQueueFixture(t)
	old := f.feedbackRun.Pulls[0].Base.SHA
	f.pr.Base.SHA = strings.Repeat("e", 40)
	f.refresh()
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	last := f.api.writes[len(f.api.writes)-1]
	b, err := decodeBinding(last["external_id"].(string))
	if last["conclusion"] != "success" || err != nil || b.Base != f.pr.Base.SHA || old == b.Base {
		t.Fatalf("behind-branch feedback admission %+v/%v", b, err)
	}
}
func TestQueueApprovalPendingAndRedactedBypassRemainExplicit(t *testing.T) {
	f := newQueueFixture(t)
	f.feedbackRun.Conclusion = "action_required"
	f.feedbackRefresh()
	delete(f.api.responses, f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1"))
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	f.assertNoSuccess(t)
	g := newQueueFixture(t)
	delete(g.owner, "bypass_actors")
	if err := g.admit(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.c.output.(*bytes.Buffer).String(), "bypass actors observable=false") {
		t.Fatal("redacted actors were represented as known empty")
	}
}
func TestQueueRejectsAmbiguousGroupHeadAndMalformedCompare(t *testing.T) {
	f := newQueueFixture(t)
	second := f.entry
	second.ID = "ENTRY8"
	second.Position = 2
	second.PR.Number = 8
	second.PR.Head = strings.Repeat("f", 40)
	f.api.responses[f.query] = map[string]any{"data": map[string]any{"repository": map[string]any{"mergeQueue": map[string]any{"id": "QUEUE_MAIN", "entries": map[string]any{"nodes": []queueEntry{f.entry, second}, "pageInfo": map[string]bool{"hasNextPage": false}}}}}}
	if err := f.group(); err == nil {
		t.Fatal("ambiguous authority prefix accepted")
	}
	f.assertNoSuccess(t)
	g := newQueueFixture(t)
	g.api.responses[g.c.endpoint("compare/"+g.pr.Base.SHA+"..."+g.groupRun.HeadSHA)] = map[string]any{"status": "diverged"}
	if err := g.group(); err == nil {
		t.Fatal("malformed compare swallowed as ordinary stale state")
	}
	g.assertNoSuccess(t)
}

func TestQueueAdmissionRefusesHeadSharedDuringFinalFeedback(t *testing.T) {
	f := newQueueFixture(t)
	injected := false
	f.api.readHook = func(path string, n int) {
		if strings.Contains(path, "actions/runs/101/attempts/1/jobs?") && n == 2 {
			injected = true
			other := f.pr
			other.Number = 8
			f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr, other}
		}
	}
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	if !injected {
		t.Fatal("second PR was not opened during final feedback verification")
	}
	f.assertNoSuccess(t)
	last := f.api.writes[len(f.api.writes)-1]
	if last["conclusion"] != "failure" {
		t.Fatalf("shared head did not remain refused: %+v", last)
	}
}

func queuePageResponse(id string, entries []queueEntry, more bool, cursor any) map[string]any {
	return map[string]any{"data": map[string]any{"repository": map[string]any{"mergeQueue": map[string]any{"id": id, "entries": map[string]any{"nodes": entries, "pageInfo": map[string]any{"hasNextPage": more, "endCursor": cursor}}}}}}
}

func (f *queueFixture) paginatedQueue() (string, []queueEntry) {
	entries := []queueEntry{f.entry}
	for position := 2; position <= 101; position++ {
		entry := queueEntry{ID: fmt.Sprintf("WAITING%d", position), Position: position, State: "QUEUED"}
		entry.PR.Number = position + 6
		entry.PR.Head = fmt.Sprintf("%040x", position)
		entry.PR.Base = "main"
		entries = append(entries, entry)
	}
	second := strings.Replace(f.query, "entries(first:100)", `entries(first:100,after:"cursor100")`, 1)
	f.api.responses[f.query] = queuePageResponse("QUEUE_MAIN", entries[:100], true, "cursor100")
	f.api.responses[second] = queuePageResponse("QUEUE_MAIN", entries[100:], false, "cursor101")
	return second, entries
}

func TestQueueCompletePaginationPublishesAndRepairsLargeQueue(t *testing.T) {
	for _, mode := range []string{"group", "repair"} {
		t.Run(mode, func(t *testing.T) {
			f := newQueueFixture(t)
			second, _ := f.paginatedQueue()
			var err error
			if mode == "group" {
				err = f.group()
			} else if err = f.q.init(context.Background()); err == nil {
				err = f.q.repairGroups(context.Background())
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.api.reads[second] < 2 {
				t.Fatal("complete membership was not retrieved again before publication")
			}
			last := f.api.writes[len(f.api.writes)-1]
			if last["conclusion"] != "success" || last["head_sha"] != f.groupRun.HeadSHA {
				t.Fatalf("complete group was not proved: %+v", last)
			}
		})
	}
}

func TestQueuePaginationRefusesIncompleteOrInconsistentPages(t *testing.T) {
	for _, name := range []string{"missing cursor", "repeated cursor", "empty continued page", "queue identity changed", "duplicate entry", "duplicate PR", "duplicate position", "missing position", "second page transport", "oversized page", "missing page flag"} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			second, entries := f.paginatedQueue()
			switch name {
			case "missing cursor":
				f.api.responses[f.query] = queuePageResponse("QUEUE_MAIN", entries[:100], true, nil)
			case "repeated cursor":
				f.api.responses[second] = queuePageResponse("QUEUE_MAIN", entries[100:], true, "cursor100")
			case "empty continued page":
				f.api.responses[second] = queuePageResponse("QUEUE_MAIN", []queueEntry{}, true, "cursor101")
			case "queue identity changed":
				f.api.responses[second] = queuePageResponse("REPLACED_QUEUE", entries[100:], false, "cursor101")
			case "duplicate entry":
				entries[100].ID = entries[0].ID
				f.api.responses[second] = queuePageResponse("QUEUE_MAIN", entries[100:], false, "cursor101")
			case "duplicate PR":
				entries[100].PR.Number = entries[0].PR.Number
				f.api.responses[second] = queuePageResponse("QUEUE_MAIN", entries[100:], false, "cursor101")
			case "duplicate position":
				entries[100].Position = entries[0].Position
				f.api.responses[second] = queuePageResponse("QUEUE_MAIN", entries[100:], false, "cursor101")
			case "missing position":
				entries[100].Position = 102
				f.api.responses[second] = queuePageResponse("QUEUE_MAIN", entries[100:], false, "cursor101")
			case "second page transport":
				f.api.responses[second] = errors.New("queue transport unavailable")
			case "oversized page":
				f.api.responses[f.query] = queuePageResponse("QUEUE_MAIN", entries, false, "cursor101")
			case "missing page flag":
				f.api.responses[second] = map[string]any{"data": map[string]any{"repository": map[string]any{"mergeQueue": map[string]any{"id": "QUEUE_MAIN", "entries": map[string]any{"nodes": entries[100:], "pageInfo": map[string]any{"endCursor": "cursor101"}}}}}}
			}
			if err := f.group(); err == nil {
				t.Fatal("incomplete queue membership was accepted")
			}
			f.assertNoSuccess(t)
		})
	}
}

func TestQueuePaginationLimitNeverPublishesTruncatedMembership(t *testing.T) {
	f := newQueueFixture(t)
	for page := 1; page <= 20; page++ {
		query := f.query
		if page > 1 {
			query = strings.Replace(query, "entries(first:100)", fmt.Sprintf(`entries(first:100,after:"cursor%d")`, page-1), 1)
		}
		entry := f.entry
		entry.ID = fmt.Sprintf("PAGE%d", page)
		entry.Position = page
		entry.PR.Number += page - 1
		entry.Head.OID = fmt.Sprintf("%040x", page)
		f.api.responses[query] = queuePageResponse("QUEUE_MAIN", []queueEntry{entry}, true, fmt.Sprintf("cursor%d", page))
	}
	if err := f.group(); err == nil || !strings.Contains(err.Error(), "pagination limit") {
		t.Fatalf("unbounded or truncated membership accepted: %v", err)
	}
	reads := 0
	for path, count := range f.api.reads {
		if strings.HasPrefix(path, "graphql:") {
			reads += count
		}
	}
	if reads != 20 {
		t.Fatalf("queue read bound changed: %d", reads)
	}
	f.assertNoSuccess(t)
}

func (f *queueFixture) sourceBoundFork() {
	f.pr.Head.Repo = repository{ID: 50, FullName: "fork/Hikyo"}
	f.refresh()
	f.feedbackRun.HeadRepository = f.pr.Head.Repo
	f.feedbackRun.HeadBranch = f.pr.Head.Ref
	f.feedbackRun.Pulls = nil
	f.feedbackRefresh()
	for kind, sha := range map[string]string{"head": f.pr.Head.SHA, "merge": f.pr.MergeCommitSHA} {
		path := fmt.Sprintf("pull/%d/%s", f.pr.Number, kind)
		f.api.responses[f.c.endpoint("git/ref/"+path)] = map[string]any{"ref": "refs/" + path, "object": map[string]string{"type": "commit", "sha": sha}}
	}
}

func TestQueueReusesSourceBoundForkFeedbackWithoutHistoricalAssociation(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(fmt.Sprintf("main advanced=%t", advanced), func(t *testing.T) {
			f := newQueueFixture(t)
			if advanced {
				f.pr.Base.SHA = strings.Repeat("e", 40)
				f.pr.MergeCommitSHA = strings.Repeat("f", 40)
			}
			f.sourceBoundFork()
			if err := f.admit(); err != nil {
				t.Fatal(err)
			}
			last := f.api.writes[len(f.api.writes)-1]
			if last["conclusion"] != "success" {
				t.Fatalf("vouched exact source feedback was not reusable: %+v", last)
			}
			for _, kind := range []string{"head", "merge"} {
				if f.api.reads[f.c.endpoint("git/ref/pull/7/"+kind)] != 2 {
					t.Fatal("current PR refs were not reassessed during final admission")
				}
			}
		})
	}
}

func TestQueueForkFeedbackRefusesContradictoryOrUnprovedSources(t *testing.T) {
	for _, name := range []string{"wrong repository", "wrong repository name", "wrong source head", "wrong branch", "missing branch", "wrong head ref", "wrong head type", "wrong merge ref", "wrong merge object", "wrong base parent", "wrong head parent", "missing parent", "unknown mergeability", "ref transport", "contradictory association", "duplicate association", "shared current head", "vouch revoked", "DCO refused"} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			f.sourceBoundFork()
			switch name {
			case "wrong repository":
				f.feedbackRun.HeadRepository.ID++
				f.feedbackRefresh()
			case "wrong repository name":
				f.feedbackRun.HeadRepository.FullName = "another/Hikyo"
				f.feedbackRefresh()
			case "wrong source head":
				f.feedbackRun.HeadSHA = strings.Repeat("f", 40)
				f.feedbackRefresh()
			case "wrong branch":
				f.feedbackRun.HeadBranch = "different"
				f.feedbackRefresh()
			case "missing branch":
				f.feedbackRun.HeadBranch = ""
				f.feedbackRefresh()
			case "wrong head ref":
				f.api.responses[f.c.endpoint("git/ref/pull/7/head")] = map[string]any{"ref": "refs/pull/8/head", "object": map[string]string{"type": "commit", "sha": f.pr.Head.SHA}}
			case "wrong head type":
				f.api.responses[f.c.endpoint("git/ref/pull/7/head")] = map[string]any{"ref": "refs/pull/7/head", "object": map[string]string{"type": "tag", "sha": f.pr.Head.SHA}}
			case "wrong merge ref":
				f.api.responses[f.c.endpoint("git/ref/pull/7/merge")] = map[string]any{"ref": "refs/pull/7/merge", "object": map[string]string{"type": "commit", "sha": strings.Repeat("f", 40)}}
			case "wrong merge object", "wrong base parent", "wrong head parent", "missing parent":
				sha, base, head := f.pr.MergeCommitSHA, f.pr.Base.SHA, f.pr.Head.SHA
				if name == "wrong merge object" {
					sha = strings.Repeat("f", 40)
				}
				if name == "wrong base parent" {
					base = strings.Repeat("f", 40)
				}
				if name == "wrong head parent" {
					head = strings.Repeat("f", 40)
				}
				parents := []map[string]string{{"sha": base}, {"sha": head}}
				if name == "missing parent" {
					parents = parents[:1]
				}
				f.api.responses[f.c.endpoint("git/commits/"+f.pr.MergeCommitSHA)] = map[string]any{"sha": sha, "parents": parents}
			case "unknown mergeability":
				f.pr.Mergeable = nil
				f.refresh()
			case "ref transport":
				f.api.responses[f.c.endpoint("git/ref/pull/7/head")] = errors.New("PR ref transport unavailable")
			case "contradictory association":
				other := f.pr
				other.Number = 8
				f.feedbackRun.Pulls = []pull{other}
				f.feedbackRefresh()
			case "duplicate association":
				f.feedbackRun.Pulls = []pull{f.pr, f.pr}
				f.feedbackRefresh()
			case "shared current head":
				other := f.pr
				other.Number = 8
				f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr, other}
			case "vouch revoked":
				f.api.responses[f.c.endpoint("contents/.github/VOUCHED.td?ref=main")] = map[string]string{"type": "file", "path": ".github/VOUCHED.td", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("github:someoneelse"))}
			case "DCO refused":
				f.q.dco = func(context.Context, pull) (int, error) { return 1, nil }
			}
			err := f.admit()
			if name == "ref transport" && err == nil {
				t.Fatal("ref API failure was swallowed")
			}
			f.assertNoSuccess(t)
		})
	}
}
