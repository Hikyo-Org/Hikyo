package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestQueueEveryPublicationRequiresItsHeldHead(t *testing.T) {
	for _, name := range []string{"missing lease", "wrong PR head", "wrong binding head", "wrong selected head", "malformed lease"} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			if err := f.q.init(context.Background()); err != nil {
				t.Fatal(err)
			}
			p := f.pr
			b := newBinding(f.c.repo.ID, p, nil)
			var selected *check
			switch name {
			case "missing lease":
				f.c.expectedHead = ""
			case "malformed lease":
				f.c.expectedHead = "not-a-sha"
			case "wrong PR head":
				p.Head.SHA = strings.Repeat("c", 40)
			case "wrong binding head":
				b.Head = strings.Repeat("c", 40)
			case "wrong selected head":
				selected = &check{HeadSHA: strings.Repeat("c", 40)}
			}
			for _, result := range []outcome{pending("barrier"), refused("refusal"), {Status: "completed", Conclusion: "success"}} {
				if err := f.c.publish(context.Background(), p, b, selected, result); err == nil {
					t.Fatal("unleased cross-head publication accepted")
				}
			}
			if len(f.api.writes) != 0 {
				t.Fatal("guard ran after mutation")
			}
		})
	}
}

func TestStaleHeadCannotHideActualJoinedFailure(t *testing.T) {
	stale := &staleHeadWork{expected: strings.Repeat("a", 40)}
	for _, err := range []error{stale, fmt.Errorf("wrapped: %w", stale), errors.Join(stale, fmt.Errorf("nested: %w", stale))} {
		if !onlyStaleHeadWork(err) {
			t.Fatalf("benign stale work became an operational failure: %v", err)
		}
	}
	for _, err := range []error{nil, errors.New("transport failed"), errors.Join(stale, errors.New("transport failed")), fmt.Errorf("wrapped: %w", errors.Join(stale, context.DeadlineExceeded))} {
		if onlyStaleHeadWork(err) {
			t.Fatalf("real operational error swallowed: %v", err)
		}
	}
}

func TestCrossRoleSHAInitialAndFinalCannotBorrowGreen(t *testing.T) {
	for _, role := range []string{"admission", "group"} {
		for _, phase := range []string{"initial", "final"} {
			t.Run(role+"/"+phase, func(t *testing.T) {
				f := newQueueFixture(t)
				if role == "admission" {
					f.entry.Head.OID = f.pr.Head.SHA
					if phase == "initial" {
						f.groupRefresh()
					} else {
						f.api.readHook = func(path string, n int) {
							if path == f.query && n == 2 {
								f.groupRefresh()
							}
						}
					}
					if err := f.admit(); err != nil {
						t.Fatal(err)
					}
				} else {
					collisionPR := f.pr
					collisionPR.Number = 8
					collisionPR.Head.SHA = f.groupRun.HeadSHA
					f.api.responses[f.c.endpoint("pulls/8")] = collisionPR
					if phase == "initial" {
						f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr, collisionPR}
					} else {
						f.api.readHook = func(path string, n int) {
							if strings.Contains(path, "pulls?state=open") && n == 2 {
								f.api.responses[path] = []pull{f.pr, collisionPR}
							}
						}
					}
					if err := f.group(); err != nil {
						t.Fatal(err)
					}
				}
				f.assertNoSuccess(t)
				if f.api.writes[len(f.api.writes)-1]["conclusion"] != "failure" {
					t.Fatal("ambiguous role SHA did not receive a blocking result")
				}
			})
		}
	}
}

func TestHeadWorkerDoesNotFollowChangedHintOrProducerRole(t *testing.T) {
	for _, name := range []string{"wrong role", "wrong record head", "wrong event", "stale PR head"} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			e := f.event
			e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, f.pr.Head.SHA)
			kind, eventName := "admission", "workflow_dispatch"
			switch name {
			case "wrong role":
				kind = "group"
			case "wrong record head":
				e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, strings.Repeat("c", 40))
			case "wrong event":
				eventName = "pull_request_target"
			case "stale PR head":
				f.pr.Head.SHA = strings.Repeat("c", 40)
				f.refresh()
			}
			if err := f.q.runHead(context.Background(), eventName, e, kind); err == nil {
				t.Fatal("stale/mismatched worker produced a claimed result")
			}
			for _, w := range f.api.writes {
				if name != "stale PR head" || w["head_sha"] != f.c.expectedHead || w["status"] != "queued" {
					t.Fatalf("worker wrote outside its authenticated role/head: %+v", w)
				}
			}
		})
	}
}

func TestRepeatedHeadReconciliationReusesItsIncompleteBarrier(t *testing.T) {
	f := newQueueFixture(t)
	e := f.event
	e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, f.pr.Head.SHA)
	for i := 0; i < 6; i++ {
		before := len(f.api.checks)
		if err := f.q.runHead(context.Background(), "workflow_dispatch", e, "admission"); err != nil {
			t.Fatal(err)
		}
		allowed := 1
		if i == 0 {
			allowed = 2
		} // First discovery placeholder has no known PR binding.
		if growth := len(f.api.checks) - before; growth > allowed {
			t.Fatalf("unchanged reconciliation added %d checks, want <=%d", growth, allowed)
		}
		if f.api.checks[len(f.api.checks)-1].Conclusion != "success" {
			t.Fatal("fresh proof was not completed")
		}
	}
	// A stopped pre-policy lookup leaves the latest record incomplete. An
	// identical retry must patch it instead of appending another placeholder.
	f.api.responses[f.c.endpoint(fmt.Sprintf("rulesets/%d", queueRulesetID))] = errors.New("policy unavailable")
	if err := f.q.runHead(context.Background(), "workflow_dispatch", e, "admission"); err == nil {
		t.Fatal("unknown policy passed")
	}
	before := len(f.api.checks)
	if f.api.checks[before-1].Status != "queued" {
		t.Fatal("old green was not revoked")
	}
	if err := f.q.runHead(context.Background(), "workflow_dispatch", e, "admission"); err == nil {
		t.Fatal("unknown policy retry passed")
	}
	if len(f.api.checks) != before {
		t.Fatal("repeated unknown-policy recovery grew incomplete barriers")
	}
}

func TestQueueFinalRoleReadCannotHideNativeCheckCollision(t *testing.T) {
	for _, role := range []string{"admission", "group"} {
		t.Run(role, func(t *testing.T) {
			f := newQueueFixture(t)
			read, head := 2, f.pr.Head.SHA
			if role == "group" {
				read, head = 4, f.groupRun.HeadSHA
			}
			injected := false
			f.api.readHook = func(path string, n int) {
				if path == f.query && n == read {
					injected = true
					foreign := check{ID: 2000, Name: queueCheckName, HeadSHA: head, Status: "completed", Conclusion: "success"}
					foreign.App.ID = actionsAppID
					foreign.App.Slug = "github-actions"
					f.api.checks = append(f.api.checks, foreign)
					f.api.nextID = 2001
				}
			}
			var err error
			if role == "group" {
				err = f.group()
			} else {
				err = f.admit()
			}
			if !injected {
				t.Fatal("native collision was not injected during final role read")
			}
			if err == nil {
				t.Fatal("native collision hidden by final role read")
			}
			f.assertNoSuccess(t)
			for _, write := range f.api.writes {
				if write["id"] == int64(2000) {
					t.Fatal("native check was mutated")
				}
			}
		})
	}
}

func TestQueueFinalRoleSnapshotRefusesNewSharedPRHead(t *testing.T) {
	f := newQueueFixture(t)
	injected, finalFeedback := false, false
	f.api.readHook = func(path string, n int) {
		if path == f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1") && n == 2 {
			finalFeedback = true
		}
		if strings.Contains(path, "pulls?state=open") && n == 2 {
			if !finalFeedback {
				t.Fatal("shared-head mutation preceded fresh final feedback")
			}
			injected = true
			other := f.pr
			other.Number = 8
			f.api.responses[path] = []pull{f.pr, other}
		}
	}
	_ = f.admit()
	if !injected {
		t.Fatal("shared head was not introduced in final role snapshot")
	}
	f.assertNoSuccess(t)
}

func TestQueueFinalRoleSnapshotRetainsExactGroupMembership(t *testing.T) {
	for _, change := range []string{"removed", "changed member"} {
		t.Run(change, func(t *testing.T) {
			f := newQueueFixture(t)
			injected := false
			f.api.readHook = func(path string, n int) {
				if path == f.query && n == 4 {
					injected = true
					if change == "removed" {
						f.api.responses[path] = map[string]any{"data": map[string]any{"repository": map[string]any{"mergeQueue": map[string]any{"id": "QUEUE", "entries": map[string]any{"nodes": []queueEntry{}, "pageInfo": map[string]any{"hasNextPage": false}}}}}}
					} else {
						f.entry.ID = "CHANGED"
						f.groupRefresh()
					}
				}
			}
			_ = f.group()
			if !injected {
				t.Fatal("group membership did not move during final roles")
			}
			f.assertNoSuccess(t)
		})
	}
}

func TestQueueFinalRoleSnapshotRefusesChangedCurrentMember(t *testing.T) {
	for _, change := range []string{"advanced", "closed", "retargeted"} {
		t.Run(change, func(t *testing.T) {
			f := newQueueFixture(t)
			injected := false
			f.api.readHook = func(path string, n int) {
				if strings.Contains(path, "pulls?state=open") && n == 2 {
					injected = true
					p := f.pr
					switch change {
					case "advanced":
						p.Head.SHA = strings.Repeat("c", 40)
					case "retargeted":
						p.Base.Ref = "other"
					case "closed":
						f.api.responses[path] = []pull{}
						return
					}
					f.api.responses[path] = []pull{p}
				}
			}
			_ = f.group()
			if !injected {
				t.Fatal("member did not move during final role metadata read")
			}
			f.assertNoSuccess(t)
		})
	}
}

func TestQueueFinalRoleSnapshotRefusesChangedAdmissionIdentity(t *testing.T) {
	for _, change := range []string{"retargeted", "source ref", "source repository"} {
		t.Run(change, func(t *testing.T) {
			f := newQueueFixture(t)
			injected, finalFeedback := false, false
			f.api.readHook = func(path string, n int) {
				if path == f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1") && n == 2 {
					finalFeedback = true
				}
				if strings.Contains(path, "pulls?state=open") && n == 2 {
					if !finalFeedback {
						t.Fatal("identity mutation preceded fresh final feedback")
					}
					injected = true
					p := f.pr
					switch change {
					case "retargeted":
						p.Base.Ref = "release"
					case "source ref":
						p.Head.Ref = "other"
					case "source repository":
						p.Head.Repo.ID++
					}
					f.api.responses[path] = []pull{p}
				}
			}
			_ = f.admit()
			if !injected {
				t.Fatal("identity did not change during final admission role read")
			}
			f.assertNoSuccess(t)
		})
	}
}

func TestQueueFinalRoleSnapshotRefusesChangedAuthorIdentity(t *testing.T) {
	for _, role := range []string{"admission", "group"} {
		for _, field := range []string{"id", "login"} {
			t.Run(role+"/"+field, func(t *testing.T) {
				f := newQueueFixture(t)
				read := 2
				injected, finalFeedback := false, false
				f.api.readHook = func(path string, n int) {
					if path == f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1") && n == 2 {
						finalFeedback = true
					}
					if strings.Contains(path, "pulls?state=open") && n == read {
						if role == "admission" && !finalFeedback {
							t.Fatal("author mutation preceded fresh final feedback")
						}
						injected = true
						p := f.pr
						if field == "id" {
							p.User.ID++
						} else {
							p.User.Login = "other"
						}
						f.api.responses[path] = []pull{p}
					}
				}
				if role == "group" {
					_ = f.group()
				} else {
					_ = f.admit()
				}
				if !injected {
					t.Fatal("author identity did not change in final role snapshot")
				}
				f.assertNoSuccess(t)
			})
		}
	}
}

func TestAdmissionFinalRolesStillValidatesInventoryAfterFeedback(t *testing.T) {
	for _, change := range []string{"none", "duplicate", "missing", "invalid inventory"} {
		t.Run(change, func(t *testing.T) {
			f := newQueueFixture(t)
			finalFeedback, finalInventory, finalQueue := false, false, false
			feedbackJobs := f.c.endpoint("actions/runs/101/attempts/1/jobs?per_page=100&page=1")
			f.api.readHook = func(path string, n int) {
				if path == feedbackJobs && n == 2 {
					finalFeedback = true
				}
				if strings.Contains(path, "pulls?state=open") && n == 2 {
					if !finalFeedback {
						t.Fatal("final inventory read before final feedback")
					}
					finalInventory = true
					switch change {
					case "duplicate":
						other := f.pr
						other.Number = 8
						f.api.responses[path] = []pull{f.pr, other}
					case "missing":
						f.api.responses[path] = []pull{}
					case "invalid inventory":
						f.api.responses[path] = map[string]string{"message": "unavailable"}
					}
				}
				if path == f.query && n == 2 {
					if !finalInventory {
						t.Fatal("final queue-role read preceded final inventory")
					}
					finalQueue = true
				}
			}
			err := f.admit()
			if !finalFeedback || !finalInventory {
				t.Fatal("fresh final feedback/inventory not observed")
			}
			if change == "none" {
				if err != nil || !finalQueue {
					t.Fatalf("positive finalroles: %v queue=%v", err, finalQueue)
				}
				if got := f.api.checks[len(f.api.checks)-1].Conclusion; got != "success" {
					t.Fatal(got)
				}
			} else {
				f.assertNoSuccess(t)
			}
		})
	}
}
