package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestLegacyAttemptAdvanceCannotHideUnfinishedPredecessor(t *testing.T) {
	f := newFixture(t)
	f.c.checkName = "ci-required-next"
	f.source.Status, f.source.Conclusion = "in_progress", ""
	f.refresh()
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	prior := *newestQueueCheck(f.api.checks)
	if prior.Status != "queued" {
		t.Fatal("initial attempt did not publish its pending observation")
	}
	f.source.Attempt++
	f.refresh()
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	f.source.Status, f.source.Conclusion = "completed", "success"
	f.refresh()
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	if len(f.api.checks) != 2 || f.api.checks[0].ID != prior.ID || f.api.checks[0].ExternalID != prior.ExternalID || f.api.checks[0].Status != "completed" || f.api.checks[0].Conclusion != "cancelled" {
		t.Fatalf("new effective verdict hid an unfinished predecessor: %+v", f.api.checks)
	}
	latest := newestQueueCheck(f.api.checks)
	b, err := decodeBinding(latest.ExternalID)
	if err != nil || b.Attempt != f.source.Attempt || latest.Status != "completed" || latest.Conclusion != "success" {
		t.Fatalf("latest attempt lost its independently verified result: %+v/%v", latest, err)
	}
}

func TestLegacySupersessionPreservesTerminalAndForeignRecords(t *testing.T) {
	for _, scenario := range []string{"terminal success", "terminal failure", "other PR", "foreign app", "wrong head"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.source.Attempt = 2
			if err := f.c.init(context.Background()); err != nil {
				t.Fatal(err)
			}
			b := (snapshot{PR: f.pr, Run: &f.source}).binding(f.c.repo.ID)
			old := b
			old.Attempt--
			prior := check{ID: 99, Name: f.c.checkName, HeadSHA: b.Head, ExternalID: old.externalID(), Status: "queued"}
			prior.App.ID, prior.App.Slug = actionsAppID, "github-actions"
			switch scenario {
			case "terminal success", "terminal failure":
				prior.Status = "completed"
				prior.Conclusion = strings.TrimPrefix(scenario, "terminal ")
				// Even a matching binding must not authorize terminal rewriting.
				prior.ExternalID = b.externalID()
			case "other PR":
				old.PR++
				prior.ExternalID = old.externalID()
			case "foreign app":
				prior.App.ID++
			case "wrong head":
				prior.HeadSHA = strings.Repeat("e", 40)
			}
			f.api.checks = []check{prior}
			err := f.c.publish(context.Background(), f.pr, b, &prior, refused("Fresh current proof is refused."))
			if err != nil || len(f.api.writes) != 1 || f.api.writes[0]["method"] != "POST" || f.api.checks[0] != prior {
				t.Fatalf("supersession mutated terminal/foreign provenance: %+v/%v", f.api.writes, err)
			}
		})
	}
}

func TestLegacySupersessionRefusesUncertainRetirement(t *testing.T) {
	f := newFixture(t)
	f.source.Attempt = 2
	if err := f.c.init(context.Background()); err != nil {
		t.Fatal(err)
	}
	b := (snapshot{PR: f.pr, Run: &f.source}).binding(f.c.repo.ID)
	old := b
	old.Attempt--
	prior := check{ID: 99, Name: f.c.checkName, HeadSHA: b.Head, ExternalID: old.externalID(), Status: "queued"}
	prior.App.ID, prior.App.Slug = actionsAppID, "github-actions"
	f.api.checks = []check{prior}
	lost := errors.New("lost predecessor retirement receipt")
	f.api.writeErr = lost
	err := f.c.publish(context.Background(), f.pr, b, &prior, pending("New attempt is pending."))
	if !errors.Is(err, lost) || len(f.api.writes) != 1 || f.api.writes[0]["method"] != "PATCH" || f.api.writes[0]["path"] != f.c.endpoint(fmt.Sprintf("check-runs/%d", prior.ID)) || f.api.checks[0] != prior {
		t.Fatalf("uncertain supersession created another generation or retried: %+v/%v", f.api.writes, err)
	}
}

func TestLegacySupersessionAuthenticatesRecordAndReadback(t *testing.T) {
	for _, scenario := range []string{"terminal race", "foreign race", "readback failure", "foreign readback", "dry run"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.source.Attempt = 2
			if err := f.c.init(context.Background()); err != nil {
				t.Fatal(err)
			}
			b := (snapshot{PR: f.pr, Run: &f.source}).binding(f.c.repo.ID)
			old := b
			old.Attempt--
			prior := check{ID: 99, Name: f.c.checkName, HeadSHA: b.Head, ExternalID: old.externalID(), Status: "queued"}
			prior.App.ID, prior.App.Slug = actionsAppID, "github-actions"
			f.api.checks = []check{prior}
			if strings.HasSuffix(scenario, "race") {
				f.api.readHook = func(path string, n int) {
					if strings.Contains(path, "/check-runs/") && n == 1 {
						if scenario == "terminal race" {
							f.api.checks[0].Status, f.api.checks[0].Conclusion = "completed", "success"
						} else {
							f.api.checks[0].App.ID++
						}
					}
				}
			} else if scenario == "dry run" {
				f.c.dryRun = true
			} else {
				fault := "readback"
				if scenario == "foreign readback" {
					fault = "foreign readback"
				}
				f.c.api = &retirementAPI{api: f.api, f: f.api, fault: fault}
			}
			err := f.c.publish(context.Background(), f.pr, b, &prior, pending("New source is pending."))
			if (err == nil) != (scenario == "dry run") {
				t.Fatalf("supersession accepted uncertain record/readback: %v", err)
			}
			wantWrites := 0
			if strings.Contains(scenario, "readback") {
				wantWrites = 1
			}
			if len(f.api.writes) != wantWrites {
				t.Fatalf("uncertain retirement created a successor: %+v", f.api.writes)
			}
			for _, write := range f.api.writes {
				if write["external_id"] != prior.ExternalID || write["conclusion"] != "cancelled" {
					t.Fatal("retirement changed original provenance or granted proof")
				}
				if _, changed := write["details_url"]; changed {
					t.Fatal("retirement replaced original publication link")
				}
			}
		})
	}
}

type legacySupersessionWriteFault struct {
	api
	calls int
	fail  int
	lost  error
}

func (a *legacySupersessionWriteFault) write(ctx context.Context, method, path string, payload, result any) error {
	a.calls++
	if a.calls == a.fail && method == "POST" {
		return a.lost
	}
	if err := a.api.write(ctx, method, path, payload, result); err != nil {
		return err
	}
	if a.calls == a.fail {
		return a.lost
	}
	return nil
}

func TestLegacySupersessionUncertainWritesKeepBlockingWithoutRetry(t *testing.T) {
	for _, fail := range []int{1, 2} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			f := newFixture(t)
			f.source.Attempt = 2
			if err := f.c.init(context.Background()); err != nil {
				t.Fatal(err)
			}
			b := (snapshot{PR: f.pr, Run: &f.source}).binding(f.c.repo.ID)
			old := b
			old.Attempt--
			prior := check{ID: 99, Name: f.c.checkName, HeadSHA: b.Head, ExternalID: old.externalID(), Status: "queued"}
			prior.App.ID, prior.App.Slug = actionsAppID, "github-actions"
			f.api.checks = []check{prior}
			lost := errors.New("uncertain supersession write")
			a := &legacySupersessionWriteFault{api: f.api, fail: fail, lost: lost}
			f.c.api = a
			err := f.c.publish(context.Background(), f.pr, b, &prior, pending("New attempt is pending."))
			if !errors.Is(err, lost) || a.calls != fail || len(f.api.checks) != 1 || f.api.checks[0].ExternalID != prior.ExternalID || !blockingQueueCheck(&f.api.checks[0]) {
				t.Fatalf("uncertain write retried or left a passing generation: calls=%d checks=%+v err=%v", a.calls, f.api.checks, err)
			}
		})
	}
}

func TestLegacyFinalInventoryRejectsLaggingSourceGeneration(t *testing.T) {
	for _, source := range []string{"newer attempt", "newer run"} {
		for _, status := range []string{"queued", "completed"} {
			t.Run(source+"/"+status, func(t *testing.T) {
				f := newFixture(t)
				f.c.checkName = "ci-required-next"
				b := (snapshot{PR: f.pr, Run: &f.source}).binding(10)
				if source == "newer attempt" {
					b.Attempt++
				} else {
					b.Run++
				}
				prior := check{ID: 99, Name: f.c.checkName, HeadSHA: b.Head, ExternalID: b.externalID(), Status: status}
				prior.App.ID, prior.App.Slug = actionsAppID, "github-actions"
				if status == "completed" {
					prior.Conclusion = "success"
				}
				f.api.checks = []check{prior}
				err := f.execute()
				if err == nil || !strings.Contains(err.Error(), "newer source generation") || f.api.checks[0] != prior {
					t.Fatalf("lagging source superseded known newer proof: checks=%+v err=%v", f.api.checks, err)
				}
				for _, write := range f.api.writes {
					if write["method"] != "POST" || write["status"] != "queued" {
						t.Fatalf("stale source mutated newer proof or supplied terminal verdict: %+v", f.api.writes)
					}
				}
			})
		}
	}
}

func TestLegacyDifferentBaseRefDoesNotBlockCurrentSource(t *testing.T) {
	for _, status := range []string{"queued", "completed"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t)
			f.c.checkName = "ci-required-next"
			current := (snapshot{PR: f.pr, Run: &f.source}).binding(10)
			otherTarget := current
			otherTarget.BaseRef = sha256.Sum256([]byte("release"))
			otherTarget.Run++
			prior := check{ID: 99, Name: f.c.checkName, HeadSHA: current.Head, ExternalID: otherTarget.externalID(), Status: status}
			prior.App.ID, prior.App.Slug = actionsAppID, "github-actions"
			if status == "completed" {
				prior.Conclusion = "success"
			}
			f.api.checks = []check{prior}
			// The higher-ID run from the intervening release target is no longer
			// listed. The original main candidate still has fresh complete proof.
			if err := f.execute(); err != nil {
				t.Fatalf("unrelated target binding blocked current main proof: %v", err)
			}
			latest := newestQueueCheck(f.api.checks)
			if latest.ExternalID != current.externalID() || latest.Status != "completed" || latest.Conclusion != "success" {
				t.Fatalf("current source did not publish its own proof: %+v", latest)
			}
			if status == "completed" && f.api.checks[0] != prior {
				t.Fatal("retargeting rewrote the other target's terminal provenance")
			}
			if status == "queued" && (f.api.checks[0].ExternalID != prior.ExternalID || f.api.checks[0].Conclusion != "cancelled") {
				t.Fatal("old target's pending observation was hidden rather than retired")
			}
		})
	}
}
