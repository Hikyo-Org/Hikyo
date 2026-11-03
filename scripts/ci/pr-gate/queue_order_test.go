package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Overlapped reads all finish before any is judged, so nothing but the judging
// code keeps the serial order of which failure wins. Every read here fails with
// its own error; "want" names the one that must be reported.
func TestAdmissionJudgesFailedReadsInSerialOrder(t *testing.T) {
	const (
		feedback = "actions/workflows/ci-feedback.yml"
		roles    = "pulls?state=open&per_page=100&page=1"
		mainRef  = "git/ref/heads/main"
		devRef   = "git/ref/heads/dev"
		owner    = "rulesets/31"
	)
	files := "pulls/7/files?per_page=100&page=1"
	contents := func(revision string) string { return "contents/" + devGateWorkflowPath + "?ref=" + revision }
	probe := newDevFixture(t)
	main, base, head := strings.Repeat("e", 40), contents(probe.pr.Base.SHA), contents(probe.pr.Head.SHA)
	cases := []struct {
		name     string
		build    func(*testing.T) *queueFixture
		arrange  func(*queueFixture)
		fail     []string
		want     string
		verifies bool // The failure is judged after the first verifier run.
	}{
		{name: "feedback before roles", build: newDevFixture, fail: []string{feedback, roles}, want: feedback},
		{name: "roles before the contract", build: newDevFixture, fail: []string{roles, base}, want: roles},
		{name: "roles before the route", build: newQueueFixture, fail: []string{roles, devRef}, want: roles},
		{name: "route alone", build: newQueueFixture, fail: []string{devRef}, want: devRef},
		{name: "ruleset read before main authority", build: newDevFixture, fail: []string{owner, mainRef}, want: owner},
		{name: "ruleset validation before main authority", build: newDevFixture, fail: []string{mainRef},
			arrange: func(f *queueFixture) { f.owner["enforcement"] = "disabled" },
			want:    "invalid owning dev protection"},
		{name: "rule validation before main authority", build: newDevFixture, fail: []string{mainRef},
			arrange: func(f *queueFixture) { f.owner["rules"] = strictDevRules(f) },
			want:    "dev requires its distinct non-queue fast admission check"},
		{name: "effective rule validation before main authority", build: newDevFixture, fail: []string{mainRef},
			arrange: func(f *queueFixture) { f.api.responses[f.c.endpoint("rules/branches/dev")] = strictDevRules(f) },
			want:    "dev requires its distinct non-queue fast admission check"},
		{name: "invalid main identity", build: newDevFixture,
			arrange: func(f *queueFixture) {
				f.api.responses[f.c.endpoint(mainRef)] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "tag", "sha": main}}
			},
			want: "invalid trusted main identity"},
		{name: "main authority alone", build: newDevFixture, fail: []string{contents(main)}, want: contents(main)},
		{name: "base copy before head copy", build: newDevFixture, fail: []string{base, head}, want: base},
		{name: "differing base copy before head copy", build: newDevFixture, fail: []string{head},
			arrange: func(f *queueFixture) {
				f.api.responses[f.c.endpoint(base)] = map[string]string{"type": "file", "path": devGateWorkflowPath, "encoding": "base64", "content": "b3RoZXI="}
			},
			want: "must land through the full main promotion gate"},
		{name: "head copy alone", build: newDevFixture, fail: []string{head}, want: head},
		{name: "DCO before the CI definition check", build: newDevFixture, fail: []string{files}, verifies: true,
			arrange: func(f *queueFixture) {
				f.q.dco = func(context.Context, pull) (int, error) { return 0, errors.New("dco failed") }
			},
			want: "dco failed"},
		{name: "CI definition check alone", build: newDevFixture, fail: []string{files}, want: files, verifies: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := c.build(t)
			f.q.targeted = true
			if c.arrange != nil {
				c.arrange(f)
			}
			verified := 0
			verify := f.c.verify
			f.c.verify = func(ctx context.Context, n int, h string) (policyDecision, error) {
				verified++
				return verify(ctx, n, h)
			}
			for _, path := range c.fail {
				f.api.responses[f.c.endpoint(path)] = errors.New(path)
			}
			err := f.q.admit(context.Background(), f.pr.Number)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want the failure of %s", err, c.want)
			}
			f.assertNoSuccess(t)
			if !c.verifies && verified != 0 {
				t.Fatalf("a first-pass failure spent %d verifier runs", verified)
			}
		})
	}
}

// Policy and the publisher contract both read main's copy. Each must report its
// own failed read even when the other's would have succeeded.
func TestDevReportsAFailedMainCopyOnEachRead(t *testing.T) {
	for name, occurrence := range map[string]int{"policy": 1, "contract": 2} {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			path := f.c.endpoint("contents/" + devGateWorkflowPath + "?ref=" + strings.Repeat("e", 40))
			good := f.api.responses[path]
			boom := errors.New("main copy failed")
			f.api.readHook = func(got string, n int) {
				if got == path {
					f.api.responses[path] = good
					if n == occurrence {
						f.api.responses[path] = boom
					}
				}
			}
			if err := f.q.admit(context.Background(), f.pr.Number); !errors.Is(err, boom) {
				t.Fatalf("got %v, want the failed main copy", err)
			}
			f.assertNoSuccess(t)
		})
	}
}

func strictDevRules(f *queueFixture) []rule {
	rules := append([]rule{}, f.rules...)
	rules[4] = encodedRule("required_status_checks", map[string]any{"strict_required_status_checks_policy": true, "do_not_enforce_on_create": false, "required_status_checks": []map[string]any{{"context": devCheckName, "integration_id": actionsAppID}}})
	return rules
}

// A stale head or a group collision is a verdict about the head, reached before
// any route or contract result is looked at.
func TestHeadVerdictsBeatFailedRouteAndContractReads(t *testing.T) {
	contract := func(f *queueFixture) string {
		return "contents/" + devGateWorkflowPath + "?ref=" + f.pr.Base.SHA
	}
	route := func(*queueFixture) string { return "git/ref/heads/dev" }
	for name, c := range map[string]struct {
		build func(*testing.T) *queueFixture
		fail  func(*queueFixture) string
	}{
		"contract": {newDevFixture, contract},
		"route":    {newQueueFixture, route},
	} {
		t.Run("stale head beats a failed "+name, func(t *testing.T) {
			f := c.build(t)
			f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{}
			path := c.fail(f)
			f.api.responses[f.c.endpoint(path)] = errors.New(path)
			if err := f.q.admit(context.Background(), f.pr.Number); !onlyStaleHeadWork(err) {
				t.Fatalf("got %v, want stale head work", err)
			}
			f.assertNoSuccess(t)
		})
	}
	collision := func(t *testing.T) *queueFixture {
		f := newQueueFixture(t)
		f.entry.Head.OID = f.pr.Head.SHA
		f.api.responses[f.query] = map[string]any{"data": map[string]any{"repository": map[string]any{"mergeQueue": map[string]any{"id": "QUEUE_MAIN", "entries": map[string]any{"nodes": []queueEntry{f.entry}, "pageInfo": map[string]bool{"hasNextPage": false}}}}}}
		return f
	}
	requireCollision := func(t *testing.T, f *queueFixture, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if latest := newestQueueCheck(f.api.checks); latest == nil || !strings.Contains(latest.Output.Summary, "both an open PR head") {
			t.Fatalf("collision verdict lost: %+v", latest)
		}
	}
	t.Run("collision beats a failed route", func(t *testing.T) {
		f := collision(t)
		path := f.c.endpoint("git/ref/heads/dev")
		f.api.responses[path] = errors.New(path)
		requireCollision(t, f, f.q.admit(context.Background(), f.pr.Number))
	})
	t.Run("collision beats a route refusal", func(t *testing.T) {
		f := collision(t)
		f.api.responses[f.c.endpoint("git/ref/heads/dev")] = map[string]any{"ref": "refs/heads/dev", "object": map[string]string{"type": "commit", "sha": strings.Repeat("f", 40)}}
		f.api.responses[f.c.endpoint("pulls/7/files?per_page=100&page=1")] = []map[string]string{{"filename": "internal/app/app.go"}}
		requireCollision(t, f, f.q.admit(context.Background(), f.pr.Number))
	})
}

func TestDevDCORefusalBeatsTheCIDefinitionCheck(t *testing.T) {
	files := "pulls/7/files?per_page=100&page=1"
	for name, response := range map[string]any{
		"failed read": errors.New(files),
		"refusal":     []map[string]string{{"filename": ".github/workflows/ci-dev.yml"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			f.q.dco = func(context.Context, pull) (int, error) { return 1, nil }
			f.api.responses[f.c.endpoint(files)] = response
			if err := f.q.admit(context.Background(), f.pr.Number); err != nil {
				t.Fatal(err)
			}
			f.assertNoSuccess(t)
			if latest := newestQueueCheck(f.api.checks); latest == nil || !strings.Contains(latest.Output.Summary, "Canonical DCO") {
				t.Fatalf("DCO refusal lost: %+v", latest)
			}
		})
	}
}

// A publisher or main move changes the policy digest, which is what makes the final
// comparison catch either one.
func TestDevPolicyDigestBindsMainAndItsPublisher(t *testing.T) {
	main := strings.Repeat("e", 40)
	contents := func(f *queueFixture, revision string) string {
		return f.c.endpoint("contents/" + devGateWorkflowPath + "?ref=" + revision)
	}
	for name, move := range map[string]func(*queueFixture){
		"main tip": func(f *queueFixture) {
			next := strings.Repeat("9", 40)
			f.api.responses[contents(f, next)] = f.api.responses[contents(f, main)]
			f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": next}}
		},
		"publisher bytes": func(f *queueFixture) {
			f.api.responses[contents(f, main)] = map[string]string{"type": "file", "path": devGateWorkflowPath, "encoding": "base64", "content": "b3RoZXI="}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			before, err := f.q.policy(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			move(f)
			after, err := f.q.policy(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if before == after {
				t.Fatal("the policy digest did not bind this move")
			}
		})
	}
}

// Routing discards a PR that no longer matches its lease before admission spends anything.
func TestExecuteDevRoutingRefusesAMovedPR(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	for name, move := range map[string]func(*pull){
		"closed":     func(p *pull) { p.State = "closed" },
		"head moved": func(p *pull) { p.Head.SHA = strings.Repeat("f", 40) },
		"retargeted": func(p *pull) { p.Base.Ref = "main" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			f.c.repo = f.pr.Base.Repo
			f.c.repo.DefaultBranch = "main"
			next := f.pr
			move(&next)
			f.api.responses[f.c.endpoint("pulls/7")] = next
			f.c.verify = func(context.Context, int, string) (policyDecision, error) {
				t.Error("routing let a moved PR reach the verifier")
				return policyDecision{}, nil
			}
			if err := f.q.executeDev(context.Background(), "workflow_dispatch", event{Repository: f.c.repo}, 7); !onlyStaleHeadWork(err) {
				t.Fatalf("got %v, want stale head work", err)
			}
			f.assertNoSuccess(t)
		})
	}
}

func TestDevContractJudgesMainCopyBeforeBaseCopy(t *testing.T) {
	f := newDevFixture(t)
	main := f.c.endpoint("contents/" + devGateWorkflowPath + "?ref=" + strings.Repeat("e", 40))
	base := f.c.endpoint("contents/" + devGateWorkflowPath + "?ref=" + f.pr.Base.SHA)
	good, mainFailed := f.api.responses[main], errors.New("main copy failed")
	f.api.responses[base] = errors.New("base copy failed")
	f.api.readHook = func(path string, n int) {
		if path == main {
			f.api.responses[main] = good
			if n == 2 {
				f.api.responses[main] = mainFailed
			}
		}
	}
	if err := f.q.admit(context.Background(), f.pr.Number); !errors.Is(err, mainFailed) {
		t.Fatalf("got %v, want the failed main copy", err)
	}
}

// heldReadAPI holds one read until its context ends, like a hung gh subprocess.
type heldReadAPI struct {
	api
	path      string
	cancelled atomic.Bool
}

func (a *heldReadAPI) read(ctx context.Context, path string, out any) error {
	if path != a.path {
		return a.api.read(ctx, path, out)
	}
	select {
	case <-ctx.Done():
		time.Sleep(10 * time.Millisecond) // A join must wait for this; a mere cancel would not.
		a.cancelled.Store(true)
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return errors.New("policy left its authority read running")
	}
}

// An early ruleset failure must not wait on a hung authority read or leave it running.
func TestDevPolicyCancelsAndJoinsItsAuthorityRead(t *testing.T) {
	f := newDevFixture(t)
	held := &heldReadAPI{api: f.c.api, path: f.c.endpoint("git/ref/heads/main")}
	f.c.api = held
	boom := errors.New("ruleset discovery failed")
	f.api.responses[f.c.endpoint("rulesets?per_page=100&page=1")] = boom
	if _, err := f.q.policy(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the ruleset failure", err)
	}
	if !held.cancelled.Load() {
		t.Fatal("policy returned before its authority read was cancelled and joined")
	}
}

func TestFinalPRIsJudgedBeforeFinalPolicy(t *testing.T) {
	boom := errors.New("final policy read failed")
	for name, c := range map[string]struct {
		build  func(*testing.T) *queueFixture
		policy string
	}{
		"dev":   {newDevFixture, "rulesets?per_page=100&page=1"},
		"queue": {newQueueFixture, fmt.Sprintf("rulesets/%d", queueRulesetID)},
	} {
		for state, wantErr := range map[string]error{"closed": nil, "unreadable": errors.New("final PR read failed"), "open": boom, "moved": boom} {
			t.Run(name+" "+state, func(t *testing.T) {
				f := c.build(t)
				pr, policy := f.c.endpoint("pulls/7"), f.c.endpoint(c.policy)
				closed := f.pr
				closed.State = "closed"
				f.api.readHook = func(path string, n int) {
					if n != 2 {
						return
					}
					if path == policy {
						f.api.responses[path] = boom
					}
					if path == pr && state == "unreadable" {
						f.api.responses[path] = wantErr
					} else if path == pr && state == "closed" {
						f.api.responses[path] = closed
					} else if path == pr && state == "moved" {
						next := f.pr
						next.Head.SHA = strings.Repeat("f", 40)
						f.api.responses[path] = next
					}
				}
				err := f.q.admit(context.Background(), 7)
				if !errors.Is(err, wantErr) && (wantErr != nil || err != nil) {
					t.Fatalf("got %v, want %v", err, wantErr)
				}
				f.assertNoSuccess(t)
			})
		}
	}
}

// Dev admission starts from the routing read, so the final comparison must catch a
// move after it, before the final verifier run spends its budget.
func TestExecuteDevCatchesMovementAfterRouting(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	moved := strings.Repeat("f", 40)
	for _, scenario := range []string{"head", "retarget", "dev tip"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDevFixture(t)
			f.c.repo = f.pr.Base.Repo
			f.c.repo.DefaultBranch = "main"
			verified := 0
			verify := f.c.verify
			f.c.verify = func(ctx context.Context, n int, head string) (policyDecision, error) {
				verified++
				return verify(ctx, n, head)
			}
			pr, ref := f.c.endpoint("pulls/7"), f.c.endpoint("git/ref/heads/dev")
			f.api.readHook = func(path string, n int) {
				if n != 2 {
					return
				}
				next := f.pr
				switch {
				case scenario == "head" && path == pr:
					next.Head.SHA = moved
					f.api.responses[path] = next
				case scenario == "retarget" && path == pr:
					next.Base.Ref = "main"
					f.api.responses[path] = next
				case scenario == "dev tip" && path == ref:
					f.api.responses[path] = map[string]any{"ref": "refs/heads/dev", "object": map[string]string{"type": "commit", "sha": moved}}
				}
			}
			_ = f.q.executeDev(context.Background(), "workflow_dispatch", event{Repository: f.c.repo}, 7)
			f.assertNoSuccess(t)
			latest := newestQueueCheck(f.api.checks)
			if latest == nil || latest.Status == "completed" {
				t.Fatalf("movement left no pending barrier: %+v", latest)
			}
			if verified != 1 {
				t.Fatalf("the final comparison let %d verifier runs through", verified)
			}
			// A moved head's barrier belongs to the held head, so it stays generic.
			if scenario != "head" && !strings.Contains(latest.Output.Summary, "final comparison") {
				t.Fatalf("the final comparison did not publish this barrier: %+v", latest)
			}
		})
	}
}
