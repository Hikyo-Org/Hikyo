package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestQueueTruncatedLegacyReserveCreatesCanonicalGeneration(t *testing.T) {
	for _, role := range []string{"admission", "group"} {
		for _, count := range []int{1, 100} {
			for _, phase := range []string{"early error", "changed binding"} {
				t.Run(fmt.Sprintf("%s/%d/%s", role, count, phase), func(t *testing.T) {
					f := newQueueFixture(t)
					p := f.pr
					if role == "group" {
						p.Head.SHA = f.groupRun.HeadSHA
						f.c.expectedHead = p.Head.SHA
					}
					b := newBinding(f.c.repo.ID, p, &f.feedbackRun)
					old := check{Name: queueCheckName, HeadSHA: p.Head.SHA, ExternalID: legacyExternalID(t, b, strings.Repeat("historical/", 100))[:externalIDLimit], Status: "completed", Conclusion: "success"}
					old.App.ID, old.App.Slug = actionsAppID, "github-actions"
					if !truncatedLegacyBinding(old.ExternalID, f.c.repo.ID, p.Head.SHA) {
						t.Fatal("fixture is not authenticated truncated legacy identity")
					}
					for i := 1; i <= count; i++ {
						v := old
						v.ID = int64(i)
						f.api.checks = append(f.api.checks, v)
					}
					prior := append([]check(nil), f.api.checks...)
					f.api.nextID = int64(count + 1000)
					work := func(ctx context.Context) error {
						if phase == "changed binding" {
							if err := f.q.reconciliation.changedBinding(ctx, p, b); err != nil {
								return err
							}
						}
						return errors.New("current proof unavailable")
					}
					if err := f.q.reassess(context.Background(), work); err == nil {
						t.Fatal("original proof failure disappeared")
					}
					latest := newestQueueCheck(f.api.checks)
					if len(f.api.checks) != count+1 || latest.ID <= int64(count) || !blockingQueueCheck(latest) || !f.c.authenticatedQueueCheck(latest, p.Head.SHA) {
						t.Fatalf("legacy green was not superseded with one canonical blocker: %+v", latest)
					}
					for i, v := range prior {
						if f.api.checks[i] != v {
							t.Fatal("undecodable historical identity was patched")
						}
					}
					if err := f.q.reassess(context.Background(), work); err == nil {
						t.Fatal("repeat failure disappeared")
					}
					if len(f.api.checks) != count+1 {
						t.Fatal("repeated incomplete legacy repair grows history")
					}
					for _, w := range f.api.writes {
						if w["method"] == "PATCH" {
							t.Fatal("legacy repair should create one blocking generation only")
						}
					}
				})
			}
		}
	}
}

type failedQueueHistoryAPI struct {
	api
	fault string
	rows  []check
}

func (a *failedQueueHistoryAPI) read(ctx context.Context, path string, result any) error {
	if strings.Contains(path, "/check-runs?") {
		switch a.fault {
		case "outage":
			return errors.New("history API unavailable")
		case "partial":
			return queueHistoryJSON(map[string]any{"total_count": 2, "check_runs": a.rows[:1]}, result)
		case "page two":
			if strings.Contains(path, "page=2") {
				return errors.New("second page unavailable")
			}
			return queueHistoryJSON(map[string]any{"total_count": 101, "check_runs": a.rows[:100]}, result)
		}
	}
	return a.api.read(ctx, path, result)
}
func TestQueueFailedHistoryNeverCreatesUnverifiableRecords(t *testing.T) {
	for _, fault := range []string{"outage", "partial", "page two", "native collision"} {
		t.Run(fault, func(t *testing.T) {
			f := newQueueFixture(t)
			b := newBinding(f.c.repo.ID, f.pr, &f.feedbackRun)
			old := check{Name: queueCheckName, HeadSHA: b.Head, ExternalID: b.externalID(), Status: "completed", Conclusion: "success"}
			old.App.ID, old.App.Slug = actionsAppID, "github-actions"
			for i := 1; i <= 101; i++ {
				v := old
				v.ID = int64(i)
				f.api.checks = append(f.api.checks, v)
			}
			if fault == "native collision" {
				f.api.checks[100].ExternalID = "native workflow check"
			}
			a := &failedQueueHistoryAPI{api: f.api, fault: fault, rows: append([]check(nil), f.api.checks...)}
			f.c.api = a
			prior := append([]check(nil), f.api.checks...)
			for i := 0; i < 3; i++ {
				if err := f.q.reassess(context.Background(), func(context.Context) error { return errors.New("source unavailable") }); err == nil {
					t.Fatal("unverified history reported healthy")
				}
			}
			if len(f.api.writes) != 0 || len(f.api.checks) != len(prior) {
				t.Fatal("failed scan created unverified records")
			}
			for i, v := range prior {
				if f.api.checks[i] != v {
					t.Fatal("unverified identity was mutated")
				}
			}
		})
	}
}

func TestQueueDryRunPreservesScanAndWorkFailures(t *testing.T) {
	for _, entry := range []string{"direct", "head", "event"} {
		for _, fault := range []string{"history", "policy"} {
			t.Run(entry+"/"+fault, func(t *testing.T) {
				f := newQueueFixture(t)
				f.c.dryRun = true
				f.q.targeted = true
				if fault == "history" {
					f.c.api = &failedQueueHistoryAPI{api: f.api, fault: "outage"}
				} else {
					f.api.responses[f.c.endpoint(fmt.Sprintf("rulesets/%d", queueRulesetID))] = errors.New("policy unavailable")
				}
				var err error
				switch entry {
				case "direct":
					err = f.q.reassess(context.Background(), func(ctx context.Context) error { _, e := f.q.policy(ctx); return e })
				case "head":
					e := f.event
					e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, f.pr.Head.SHA)
					err = f.q.runHead(context.Background(), "workflow_dispatch", e, "admission")
				case "event":
					err = f.q.execute(context.Background(), "workflow_dispatch", f.event)
				}
				if err == nil {
					t.Fatal("dry run erased validation failure")
				}
				if len(f.api.writes) != 0 {
					t.Fatal("dry run performed API mutations")
				}
			})
		}
	}
}

func queueHistoryJSON(value, result any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return decode(data, result)
}

func TestQueueQueuedLegacyMigrationRecoversWithoutPatchingOpaqueIdentity(t *testing.T) {
	for _, waiting := range []bool{false, true} {
		t.Run(fmt.Sprint(waiting), func(t *testing.T) {
			f := newQueueFixture(t)
			b := newBinding(f.c.repo.ID, f.pr, nil)
			old := check{ID: 99, Name: queueCheckName, HeadSHA: b.Head, ExternalID: legacyExternalID(t, b, strings.Repeat("historical/", 100))[:externalIDLimit], Status: "queued"}
			old.App.ID, old.App.Slug = actionsAppID, "github-actions"
			f.api.checks = []check{old}
			if waiting {
				f.verifyCode = 75
			}
			if err := f.admit(); err != nil {
				t.Fatal(err)
			}
			if len(f.api.checks) != 2 || f.api.checks[0] != old {
				t.Fatal("legacy pending migration did not preserve opaque owned history")
			}
			latest := newestQueueCheck(f.api.checks)
			if !f.c.authenticatedQueueCheck(latest, b.Head) || latest.ID == old.ID {
				t.Fatal("migration did not create a canonical generation")
			}
			if waiting && !blockingQueueCheck(latest) {
				t.Fatal("pending authority became green")
			}
			f.verifyCode = 0
			if err := f.admit(); err != nil {
				t.Fatal(err)
			}
			if len(f.api.checks) != 2 || newestQueueCheck(f.api.checks).Conclusion != "success" {
				t.Fatal("pending canonical generation did not recover in place")
			}
			before := len(f.api.writes)
			if err := f.admit(); err != nil {
				t.Fatal(err)
			}
			if len(f.api.writes) != before {
				t.Fatal("unchanged recovered proof grew history")
			}
			for _, w := range f.api.writes {
				if w["path"] == f.c.endpoint("check-runs/99") {
					t.Fatal("opaque legacy identity was patched")
				}
			}
		})
	}
}
