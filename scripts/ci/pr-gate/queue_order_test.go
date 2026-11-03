package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
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

func TestDevDCORefusalBeatsAFailedCIDefinitionCheck(t *testing.T) {
	f := newDevFixture(t)
	f.q.dco = func(context.Context, pull) (int, error) { return 1, nil }
	files := f.c.endpoint("pulls/7/files?per_page=100&page=1")
	f.api.responses[files] = errors.New(files)
	if err := f.q.admit(context.Background(), f.pr.Number); err != nil {
		t.Fatal(err)
	}
	f.assertNoSuccess(t)
	if latest := newestQueueCheck(f.api.checks); latest == nil || !strings.Contains(latest.Output.Summary, "Canonical DCO") {
		t.Fatalf("DCO refusal lost: %+v", latest)
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
		for state, wantErr := range map[string]error{"closed": nil, "unreadable": errors.New("final PR read failed"), "open": boom} {
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

// Dev admission starts from the routing read, so only the final comparison can
// catch a move after it.
func TestExecuteDevCatchesMovementAfterRouting(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	moved := strings.Repeat("f", 40)
	for _, scenario := range []string{"head", "retarget", "dev tip"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDevFixture(t)
			f.c.repo = f.pr.Base.Repo
			f.c.repo.DefaultBranch = "main"
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
		})
	}
}
