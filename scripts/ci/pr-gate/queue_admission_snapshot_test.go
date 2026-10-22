package main

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func snapshotAdmission(f *queueFixture) error {
	e := f.event
	e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, f.pr.Head.SHA)
	start := time.Now()
	b := admissionBudget{Started: start, Deadline: start.Add(time.Minute)}
	return f.q.withAdmissionBudget(context.Background(), b, func(ctx context.Context) error {
		return f.q.runHead(ctx, "workflow_dispatch", e, "admission")
	})
}

func directSnapshotAdmission(f *queueFixture) error {
	start := time.Now()
	b := admissionBudget{Started: start, Deadline: start.Add(time.Minute)}
	return f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), b)
}

func snapshotPriorFailure(f *queueFixture) check {
	b := newBinding(f.c.repo.ID, f.pr, &f.feedbackRun)
	old := check{ID: 99, Name: queueCheckName, HeadSHA: f.pr.Head.SHA, ExternalID: b.externalID(), Status: "completed", Conclusion: "failure", CompletedAt: "2026-10-14T16:38:12Z"}
	old.App.ID, old.App.Slug = actionsAppID, "github-actions"
	f.api.checks = []check{old}
	return old
}

func TestHeadAdmissionReusesInitialSnapshotAndRechecksEveryFinalBoundary(t *testing.T) {
	f := newQueueFixture(t)
	old := snapshotPriorFailure(f)
	verifications, dcoChecks := 0, 0
	f.c.verify = func(context.Context, int, string) (policyDecision, error) {
		verifications++
		return policyDecision{}, nil
	}
	f.q.dco = func(context.Context, pull) (int, error) { dcoChecks++; return 0, nil }
	finalFeedback := false
	jobs := f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1")
	f.api.readHook = func(path string, n int) {
		if path == jobs && n == 2 {
			finalFeedback = true
		}
		if strings.Contains(path, "pulls?state=open") && n == 2 && !finalFeedback {
			t.Fatal("final global inventory preceded fresh final feedback")
		}
	}
	if err := snapshotAdmission(f); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{
		f.c.endpoint(fmt.Sprintf("rulesets/%d", queueRulesetID)): 2,
		f.c.endpoint("rules/branches/main"):                      2,
		f.c.endpoint("pulls?state=open&per_page=100&page=1"):     2,
		f.query:                            2,
		f.c.endpoint("pulls/7"):            2,
		f.c.endpoint("git/ref/heads/main"): 2,
		f.c.endpoint("actions/workflows/ci-feedback.yml"): 2,
		f.c.endpoint(fmt.Sprintf("actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA)): 2,
		jobs: 2,
	} {
		if got := f.api.reads[path]; got != want {
			t.Fatalf("reads for %s: got %d, want %d", path, got, want)
		}
	}
	total := 0
	for _, count := range f.api.reads {
		total += count
	}
	if total != 22 || verifications != 2 || dcoChecks != 2 || !finalFeedback {
		t.Fatalf("reads=%d, authority=%d, canonical DCO=%d, final feedback=%t", total, verifications, dcoChecks, finalFeedback)
	}
	if !reflect.DeepEqual(f.api.checks[0], old) || len(f.api.checks) != 2 || f.api.checks[1].Conclusion != "success" {
		t.Fatal("fresh proof must supersede the preserved terminal failure with a new check")
	}
	if len(f.api.writes) != 2 || f.api.writes[0]["method"] != "POST" || f.api.writes[0]["status"] != "queued" || f.api.writes[1]["method"] != "PATCH" || f.api.writes[1]["id"] == old.ID {
		t.Fatal("reassessment reset an old terminal record or omitted the new blocking generation")
	}
}

func TestDirectAdmissionRetainsFreshReadsAfterAHeadWorker(t *testing.T) {
	f := newQueueFixture(t)
	if err := snapshotAdmission(f); err != nil {
		t.Fatal(err)
	}
	f.api.reads = make(map[string]int)
	f.owner["updated_at"] = "changed between invocations"
	f.policyRefresh()
	start := time.Now()
	b := admissionBudget{Started: start, Deadline: start.Add(time.Minute)}
	if err := f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), b); err != nil {
		t.Fatal(err)
	}
	for path, count := range map[string]int{
		f.c.endpoint(fmt.Sprintf("rulesets/%d", queueRulesetID)): 2,
		f.c.endpoint("rules/branches/main"):                      2,
		f.c.endpoint("pulls?state=open&per_page=100&page=1"):     2,
		f.query: 2,
	} {
		if f.api.reads[path] != count {
			t.Fatalf("direct admission reused a previous worker snapshot: %s got %d reads, want %d", path, f.api.reads[path], count)
		}
	}
	if f.api.checks[len(f.api.checks)-1].Conclusion != "success" {
		t.Fatal("direct admission did not reprove the changed current policy")
	}
}

func TestHeadAdmissionSnapshotRequiresLivePRIdentity(t *testing.T) {
	for _, change := range []string{"none", "REST base SHA lag", "closed", "number", "head SHA", "head ref", "head repository ID", "head repository name", "base ref", "base repository ID", "author ID", "author login"} {
		t.Run(change, func(t *testing.T) {
			f := newQueueFixture(t)
			old := snapshotPriorFailure(f)
			verifyCalls, injected := 0, false
			f.c.verify = func(context.Context, int, string) (policyDecision, error) {
				verifyCalls++
				return policyDecision{}, nil
			}
			if change == "REST base SHA lag" {
				p := f.pr
				p.Base.SHA = strings.Repeat("c", 40)
				f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{p}
			}
			f.api.readHook = func(path string, n int) {
				if path != f.c.endpoint("pulls/7") || n != 1 {
					return
				}
				injected = true
				p := f.pr
				switch change {
				case "closed":
					p.State = "closed"
				case "number":
					p.Number++
				case "head SHA":
					p.Head.SHA = strings.Repeat("c", 40)
				case "head ref":
					p.Head.Ref = "changed"
				case "head repository ID":
					p.Head.Repo.ID++
				case "head repository name":
					p.Head.Repo.FullName = "other/Hikyo"
				case "base ref":
					p.Base.Ref = "release"
				case "base repository ID":
					p.Base.Repo.ID++
				case "author ID":
					p.User.ID++
				case "author login":
					p.User.Login = "changed"
				}
				f.api.responses[path] = p
			}
			err := snapshotAdmission(f)
			if !injected {
				t.Fatal("live identity read was not exercised")
			}
			if change == "none" || change == "REST base SHA lag" {
				if err != nil || verifyCalls != 2 || f.api.checks[len(f.api.checks)-1].Conclusion != "success" {
					t.Fatalf("valid initial snapshot rejected: %v, authority calls=%d", err, verifyCalls)
				}
			} else {
				f.assertNoSuccess(t)
				if verifyCalls != 0 {
					t.Fatal("snapshot identity mismatch reached admission authority verification")
				}
			}
			if !reflect.DeepEqual(f.api.checks[0], old) {
				t.Fatal("identity movement mutated the old terminal record")
			}
		})
	}
}

func TestHeadAdmissionSnapshotNeverReplacesFreshFinalProof(t *testing.T) {
	for _, change := range []string{"new duplicate", "missing PR", "invalid inventory", "moved head", "source ref", "source repository", "base ref", "author ID", "author login", "queue role", "native check", "feedback attempt", "feedback lane", "policy", "authority", "canonical DCO"} {
		t.Run(change, func(t *testing.T) {
			f := newQueueFixture(t)
			old := snapshotPriorFailure(f)
			injected, finalFeedback := false, false
			verifyCalls, dcoChecks := 0, 0
			f.c.verify = func(context.Context, int, string) (policyDecision, error) {
				verifyCalls++
				if change == "policy" && verifyCalls == 1 {
					injected = true
					f.owner["updated_at"] = "changed"
					f.policyRefresh()
				}
				if change == "authority" && verifyCalls == 2 {
					injected = true
					return policyDecision{Code: 1}, nil
				}
				return policyDecision{}, nil
			}
			f.q.dco = func(context.Context, pull) (int, error) {
				dcoChecks++
				if change == "canonical DCO" && dcoChecks == 2 {
					injected = true
					return 1, nil
				}
				return 0, nil
			}
			jobs := f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1")
			f.api.readHook = func(path string, n int) {
				if strings.Contains(path, "actions/workflows/ci-feedback.yml/runs?") && n == 2 && change == "feedback attempt" {
					injected = true
					f.feedbackRun.Attempt++
					f.feedbackRefresh()
				}
				if path != jobs || n != 2 {
					return
				}
				finalFeedback = true
				p := f.pr
				inventory := []pull{p}
				switch change {
				case "new duplicate":
					other := p
					other.Number++
					inventory = append(inventory, other)
				case "missing PR":
					inventory = []pull{}
				case "invalid inventory":
					f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = map[string]string{"message": "unavailable"}
					injected = true
					return
				case "moved head":
					inventory[0].Head.SHA = strings.Repeat("c", 40)
				case "source ref":
					inventory[0].Head.Ref = "changed"
				case "source repository":
					inventory[0].Head.Repo.ID++
				case "base ref":
					inventory[0].Base.Ref = "release"
				case "author ID":
					inventory[0].User.ID++
				case "author login":
					inventory[0].User.Login = "changed"
				case "queue role":
					f.entry.Head.OID = p.Head.SHA
					f.groupRefresh()
				case "native check":
					foreign := check{ID: 2000, Name: queueCheckName, HeadSHA: p.Head.SHA, Status: "completed", Conclusion: "success"}
					foreign.App.ID, foreign.App.Slug = actionsAppID, "github-actions"
					f.api.checks = append(f.api.checks, foreign)
					f.api.nextID = 2001
				case "feedback lane":
					f.api.responses[path] = map[string]any{"total_count": 2, "jobs": []job{{ID: 201, RunID: 101, Attempt: 1, HeadSHA: p.Head.SHA, Name: "feedback (go)", Status: "completed", Conclusion: "success"}, {ID: 202, RunID: 101, Attempt: 1, HeadSHA: p.Head.SHA, Name: "feedback (frontend)", Status: "completed", Conclusion: "failure"}}}
				default:
					return
				}
				f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = inventory
				injected = true
			}
			_ = snapshotAdmission(f)
			if !injected {
				t.Fatal("late proof movement was not exercised")
			}
			if change != "policy" && change != "authority" && change != "canonical DCO" && change != "feedback attempt" && !finalFeedback {
				t.Fatal("final proof did not reread complete feedback before global roles")
			}
			f.assertNoSuccess(t)
			if !reflect.DeepEqual(f.api.checks[0], old) {
				t.Fatal("late movement rewrote the old terminal failure")
			}
		})
	}
}

func TestHeadAdmissionSnapshotBlocksMovedProofAfterTerminalSuccess(t *testing.T) {
	for _, change := range []string{"final live main", "final base ref", "initial actor on old green", "final duplicate on old green", "final queue role on old green"} {
		t.Run(change, func(t *testing.T) {
			f := newQueueFixture(t)
			old := snapshotPriorFailure(f)
			old.Conclusion = "success"
			f.api.checks[0] = old
			f.c.api = &immutableCheckAPI{api: f.api, fake: f.api}
			injected := false
			f.api.readHook = func(path string, n int) {
				if change == "final live main" && path == f.c.endpoint("git/ref/heads/main") && n == 2 {
					injected = true
					f.api.responses[path] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": strings.Repeat("c", 40)}}
				}
				if change == "final base ref" && path == f.c.endpoint("pulls/7") && n == 2 {
					injected = true
					p := f.pr
					p.Base.Ref = "release"
					f.api.responses[path] = p
				}
				if change == "initial actor on old green" && path == f.c.endpoint("pulls/7") && n == 1 {
					injected = true
					p := f.pr
					p.User.ID++
					f.api.responses[path] = p
				}
				if n == 2 && path == f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1") {
					if change == "final duplicate on old green" {
						injected = true
						other := f.pr
						other.Number++
						f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr, other}
					}
					if change == "final queue role on old green" {
						injected = true
						f.entry.Head.OID = f.pr.Head.SHA
						f.groupRefresh()
					}
				}
			}
			_ = snapshotAdmission(f)
			if !injected {
				t.Fatal("movement was not exercised")
			}
			f.assertNoSuccess(t)
			if !reflect.DeepEqual(f.api.checks[0], old) {
				t.Fatal("prior terminal green mutated")
			}
			latest := newestQueueCheck(f.api.checks)
			if latest == nil || latest.ID == old.ID || !blockingQueueCheck(latest) {
				t.Fatalf("new effective blocking generation absent: %+v", f.api.checks)
			}
		})
	}
}

func TestDirectAdmissionInitialInventoryRequiresCurrentIdentityAndUniqueness(t *testing.T) {
	for _, change := range []string{"control", "REST base SHA lag", "sole other PR", "current and other PR", "missing current PR", "head SHA", "head ref", "source repository", "base ref", "author ID", "author login"} {
		t.Run(change, func(t *testing.T) {
			f := newQueueFixture(t)
			old := snapshotPriorFailure(f)
			old.Conclusion = "success"
			f.api.checks[0] = old
			f.c.api = &immutableCheckAPI{api: f.api, fake: f.api}
			verifyCalls, injected := 0, false
			f.c.verify = func(context.Context, int, string) (policyDecision, error) {
				verifyCalls++
				return policyDecision{}, nil
			}
			list := f.c.endpoint("pulls?state=open&per_page=100&page=1")
			f.api.readHook = func(path string, n int) {
				if path != list || n != 1 {
					return
				}
				injected = true
				p := f.pr
				inventory := []pull{p}
				switch change {
				case "REST base SHA lag":
					inventory[0].Base.SHA = strings.Repeat("c", 40)
				case "sole other PR":
					inventory[0].Number++
				case "current and other PR":
					other := p
					other.Number++
					inventory = append(inventory, other)
				case "missing current PR":
					inventory = []pull{}
				case "head SHA":
					inventory[0].Head.SHA = strings.Repeat("c", 40)
				case "head ref":
					inventory[0].Head.Ref = "changed"
				case "source repository":
					inventory[0].Head.Repo.ID++
				case "base ref":
					inventory[0].Base.Ref = "release"
				case "author ID":
					inventory[0].User.ID++
				case "author login":
					inventory[0].User.Login = "changed"
				}
				f.api.responses[path] = inventory
			}
			err := directSnapshotAdmission(f)
			if !injected {
				t.Fatal("initial complete inventory was not exercised")
			}
			if !reflect.DeepEqual(f.api.checks[0], old) {
				t.Fatal("initial inventory movement mutated the old terminal green")
			}
			if change == "control" || change == "REST base SHA lag" {
				if err != nil || verifyCalls != 2 || newestQueueCheck(f.api.checks).Conclusion != "success" {
					t.Fatalf("valid identity was refused: %v, authority calls=%d", err, verifyCalls)
				}
				total := 0
				for _, count := range f.api.reads {
					total += count
				}
				if total != 22 || f.api.reads[list] != 2 {
					t.Fatalf("direct admission reads=%d, complete inventories=%d", total, f.api.reads[list])
				}
			} else {
				f.assertNoSuccess(t)
				wantAuthority := 0
				if change == "current and other PR" {
					wantAuthority = 1
				}
				if verifyCalls != wantAuthority {
					t.Fatalf("initial identity/collision reached %d authority calls, want %d", verifyCalls, wantAuthority)
				}
				latest := newestQueueCheck(f.api.checks)
				if latest.ID == old.ID || !blockingQueueCheck(latest) {
					t.Fatal("initial inventory refusal retained an effective green")
				}
			}
		})
	}
}

func TestDirectAdmissionForeignGreenAndFinalPRReadFailureKeepsOwnershipBarrier(t *testing.T) {
	f := newQueueFixture(t)
	old := snapshotPriorFailure(f)
	old.Conclusion = "success"
	f.api.checks[0] = old
	f.c.api = &immutableCheckAPI{api: f.api, fake: f.api}
	var foreign check
	dcoCalls := 0
	f.q.dco = func(context.Context, pull) (int, error) {
		dcoCalls++
		if dcoCalls == 1 {
			foreign = check{ID: 2000, Name: queueCheckName, HeadSHA: f.pr.Head.SHA, Status: "completed", Conclusion: "success"}
			foreign.App.ID, foreign.App.Slug = actionsAppID, "github-actions"
			f.api.checks = append(f.api.checks, foreign)
			f.api.nextID = 2001
			f.api.responses[f.c.endpoint("pulls/7")] = fmt.Errorf("PR unavailable after first canonical DCO")
		}
		return 0, nil
	}
	if err := directSnapshotAdmission(f); err == nil || dcoCalls != 1 {
		t.Fatalf("combined foreign-check/API failure not exercised: error=%v, DCO calls=%d", err, dcoCalls)
	}
	f.assertNoSuccess(t)
	if !reflect.DeepEqual(f.api.checks[0], old) || !reflect.DeepEqual(f.api.checks[1], foreign) {
		t.Fatal("recovery mutated an old terminal or foreign record")
	}
	latest := newestQueueCheck(f.api.checks)
	if latest.ID <= foreign.ID || !blockingQueueCheck(latest) {
		t.Fatalf("foreign green remained effective after PR read failure: %+v", latest)
	}
	if len(f.api.writes) != 1 || f.api.writes[0]["method"] != "POST" || f.api.writes[0]["status"] != "queued" {
		t.Fatal("ownership recovery failed to create a new immutable blocking generation")
	}
}
