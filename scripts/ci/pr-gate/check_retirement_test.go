package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func noRetirementRoles(f *queueFixture) {
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{}
	f.api.responses[f.query] = map[string]any{"data": map[string]any{"repository": map[string]any{"mergeQueue": map[string]any{"id": "QUEUE_MAIN", "entries": map[string]any{"nodes": []queueEntry{}, "pageInfo": map[string]bool{"hasNextPage": false}}}}}}
}

func TestMergedQueueAdmissionDoesNotReviveGreen(t *testing.T) {
	for _, callback := range []string{"close", "delayed feedback", "stale worker"} {
		t.Run(callback, func(t *testing.T) {
			f := newQueueFixture(t)
			if err := f.admit(); err != nil {
				t.Fatal(err)
			}
			prior := append([]check(nil), f.api.checks...)
			writes := len(f.api.writes)
			f.pr.State = "closed"
			f.refresh()
			noRetirementRoles(f)
			for i := 0; i < 2; i++ {
				var err error
				switch callback {
				case "close":
					err = f.q.admit(context.Background(), f.pr.Number)
				case "delayed feedback":
					e := f.event
					e.Run = f.feedbackRun
					err = f.q.execute(context.Background(), "workflow_run", e)
				case "stale worker":
					e := f.event
					e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, f.c.expectedHead)
					err = f.q.runHead(context.Background(), "workflow_dispatch", e, "admission")
				}
				if err != nil && !onlyStaleHeadWork(err) {
					t.Fatal(err)
				}
			}
			if len(f.api.writes) != writes || len(f.api.checks) != len(prior) {
				t.Fatalf("retired callback revived check history: %+v", f.api.writes[writes:])
			}
			for i := range prior {
				if f.api.checks[i] != prior[i] {
					t.Fatal("historical terminal verdict changed")
				}
			}
		})
	}
}

// The direct-record seam preserves the existing production API interface while
// the inventory and all paginated role reads execute their actual code paths.
type retirementAPI struct {
	api
	f          *fakeAPI
	fault      string
	reads      int
	beforeRead func()
}

func (a *retirementAPI) read(ctx context.Context, path string, result any) error {
	if strings.Contains(path, "/check-runs/") && !strings.Contains(path, "?") {
		a.reads++
		if a.beforeRead != nil {
			a.beforeRead()
		}
		if a.fault == "readback" && a.reads == 2 {
			return errors.New("readback unavailable")
		}
		for _, v := range a.f.checks {
			if strings.HasSuffix(path, fmt.Sprintf("/check-runs/%d", v.ID)) {
				if a.fault == "foreign readback" && a.reads == 2 {
					v.App.ID++
				}
				return queueHistoryJSON(v, result)
			}
		}
		return errors.New("missing exact check record")
	}
	return a.api.read(ctx, path, result)
}
func retirementFixture(t *testing.T, name string) (*queueFixture, *retirementAPI) {
	t.Helper()
	f := newQueueFixture(t)
	f.c.checkName = name
	v := check{ID: 1, Name: name, HeadSHA: f.pr.Head.SHA, ExternalID: newBinding(f.c.repo.ID, f.pr, &f.feedbackRun).externalID(), Status: "queued"}
	v.App.ID, v.App.Slug = actionsAppID, "github-actions"
	f.api.checks = []check{v}
	noRetirementRoles(f)
	a := &retirementAPI{api: f.api, f: f.api}
	f.c.api = a
	return f, a
}
func TestRetirementCancelsAllOwnedNonterminalHistoryAndNeverRevives(t *testing.T) {
	for _, name := range []string{queueCheckName, "ci-required-next"} {
		t.Run(name, func(t *testing.T) {
			f, _ := retirementFixture(t, name)
			for i := 2; i <= 101; i++ {
				v := f.api.checks[0]
				v.ID = int64(i)
				if i%2 == 0 {
					v.Status = "in_progress"
				}
				f.api.checks = append(f.api.checks, v)
			}
			for _, conclusion := range []string{"success", "failure", "cancelled", "neutral"} {
				v := f.api.checks[0]
				v.ID = int64(len(f.api.checks) + 1)
				v.Status = "completed"
				v.Conclusion = conclusion
				f.api.checks = append(f.api.checks, v)
			}
			prior := append([]check(nil), f.api.checks...)
			for attempt := 0; attempt < 2; attempt++ {
				if err := f.c.retireChecks(context.Background(), f.c.expectedHead); err != nil {
					t.Fatal(err)
				}
			}
			if len(f.api.writes) != 101 || len(f.api.checks) != len(prior) {
				t.Fatalf("retirement did not deduplicate: writes=%d checks=%d", len(f.api.writes), len(f.api.checks))
			}
			for i, v := range f.api.checks {
				if prior[i].Status == "completed" {
					if v != prior[i] {
						t.Fatal("terminal history changed")
					}
				} else if v.Status != "completed" || v.Conclusion != "cancelled" {
					t.Fatalf("unfinished record survived: %+v", v)
				}
			}
			if name == queueCheckName {
				f.pr.State = "closed"
				f.api.responses[f.c.endpoint("pulls/7")] = f.pr
				if err := f.q.admit(context.Background(), 7); err != nil {
					t.Fatal(err)
				}
			} else {
				f.pr.State = "closed"
				f.api.responses[f.c.endpoint("pulls/7")] = f.pr
				if err := f.c.reconcile(context.Background(), 7, nil); err != nil {
					t.Fatal(err)
				}
			}
			if len(f.api.writes) != 101 {
				t.Fatal("closed callback revived cancelled checks")
			}
		})
	}
}
func TestRetirementProtectsLiveRolesAndAmbiguousIdentity(t *testing.T) {
	for _, name := range []string{queueCheckName, "ci-required-next"} {
		for _, scenario := range []string{"same PR", "shared SHA", "other base", "group", "reopened", "unknown queue", "unknown PRs", "foreign app", "wrong head", "foreign binding", "native collision", "truncated legacy", "changed record", "readback", "foreign readback", "uncertain write"} {
			t.Run(name+"/"+scenario, func(t *testing.T) {
				f, a := retirementFixture(t, name)
				failure := false
				switch scenario {
				case "same PR", "shared SHA", "other base":
					p := f.pr
					if scenario == "shared SHA" {
						p.Number++
					}
					if scenario == "other base" {
						p.Base.Ref = "other"
					}
					f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{p}
				case "group":
					f.entry.Head.OID = f.c.expectedHead
					f.groupRefresh()
				case "reopened":
					a.beforeRead = func() { f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr} }
				case "unknown queue":
					f.api.responses[f.query] = errors.New("queue unavailable")
					failure = true
				case "unknown PRs":
					f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = errors.New("PRs unavailable")
					failure = true
				case "foreign app":
					f.api.checks[0].App.ID++
					failure = true
				case "wrong head":
					f.api.scopeChecksToHead = false
					f.api.checks[0].HeadSHA = strings.Repeat("c", 40)
					failure = true
				case "foreign binding":
					b := newBinding(11, f.pr, nil)
					f.api.checks[0].ExternalID = b.externalID()
					failure = true
				case "native collision":
					f.api.checks[0].ExternalID = "native"
					failure = true
				case "truncated legacy":
					f.api.checks[0].ExternalID = legacyExternalID(t, newBinding(f.c.repo.ID, f.pr, nil), strings.Repeat("old/", 100))[:externalIDLimit]
					failure = true
				case "changed record":
					a.beforeRead = func() { f.api.checks[0].Status = "completed"; f.api.checks[0].Conclusion = "success" }
					failure = true
				case "readback", "foreign readback":
					a.fault = scenario
					failure = true
				case "uncertain write":
					f.api.writeErr = errors.New("lost write receipt")
					failure = true
				}
				err := f.c.retireChecks(context.Background(), f.c.expectedHead)
				if (err != nil) != failure {
					t.Fatalf("retirement error=%v want failure=%t", err, failure)
				}
				if scenario == "readback" || scenario == "foreign readback" || scenario == "uncertain write" {
					if len(f.api.writes) != 1 {
						t.Fatal("uncertain mutation retried")
					}
				} else if len(f.api.writes) != 0 {
					t.Fatalf("unsafe retirement writes: %+v", f.api.writes)
				}
			})
		}
	}
}
func TestRetirementReopenAfterCancellationRequiresFreshProof(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	if err := f.c.retireChecks(context.Background(), f.c.expectedHead); err != nil {
		t.Fatal(err)
	}
	retired := f.api.checks[0]
	f.refresh()
	f.feedbackRefresh()
	f.groupRefresh()
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	if f.api.checks[0] != retired || len(f.api.checks) != 2 || f.api.checks[1].Conclusion != "success" {
		t.Fatal("reopen reused retired verdict rather than fresh proof generation")
	}
}

func TestGroupCallbackAfterMainAdvanceSkipsRetiredDCO(t *testing.T) {
	f := newQueueFixture(t)
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	prior := append([]check(nil), f.api.checks...)
	writes := len(f.api.writes)
	path := f.c.endpoint("pulls/7")
	next := f.api.reads[path] + 1
	f.api.readHook = func(got string, n int) {
		if got == path && n == next {
			// The PR metadata still says open while the independently resolved
			// live main already contains the tested group, as in callback 38951096517.
			f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": f.groupRun.HeadSHA}}
			noRetirementRoles(f)
		}
	}
	dcos := 0
	f.q.dco = func(context.Context, pull) (int, error) {
		dcos++
		return 0, errors.New("DCO check: empty commit range")
	}
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	if dcos != 0 || len(f.api.writes) != writes || len(f.api.checks) != len(prior) {
		t.Fatalf("retired callback ran DCO or changed checks: dcos=%d writes=%+v", dcos, f.api.writes[writes:])
	}
	for i := range prior {
		if f.api.checks[i] != prior[i] {
			t.Fatal("terminal group history changed")
		}
	}
}
func TestActiveGroupDCOFailureRemainsBlocking(t *testing.T) {
	f := newQueueFixture(t)
	f.q.dco = func(context.Context, pull) (int, error) { return 0, errors.New("DCO check: empty commit range") }
	err := f.group()
	if err == nil || !strings.Contains(err.Error(), "empty commit range") {
		t.Fatalf("active DCO failure hidden: %v", err)
	}
	if !blockingQueueCheck(newestQueueCheck(f.api.checks)) {
		t.Fatal("active invalid range did not remain blocking")
	}
}
func TestLegacyMergedCallbacksNeverReviveTerminalVerdicts(t *testing.T) {
	for _, callback := range []string{"close", "late candidate"} {
		t.Run(callback, func(t *testing.T) {
			f, _ := retirementFixture(t, "ci-required-next")
			f.api.checks[0].Status = "completed"
			f.api.checks[0].Conclusion = "success"
			prior := f.api.checks[0]
			f.pr.State = "closed"
			f.refresh()
			noRetirementRoles(f)
			e := f.event
			name := "pull_request_target"
			if callback == "close" {
				e.Number = f.pr.Number
				e.Pull = f.pr
				e.Action = "closed"
			} else {
				name = "workflow_run"
				e.Run = f.source
			}
			for i := 0; i < 2; i++ {
				if err := f.c.execute(context.Background(), name, e); err != nil {
					t.Fatal(err)
				}
			}
			if len(f.api.writes) != 0 || len(f.api.checks) != 1 || f.api.checks[0] != prior {
				t.Fatal("legacy closed callback revived terminal history")
			}
		})
	}
}
func TestRetirementRoutingRequiresExactTrustedIdentity(t *testing.T) {
	f := newQueueFixture(t)
	e := targetedFixtureEvent(f)
	e.Action = "synchronize"
	e.Before = strings.Repeat("e", 40)
	if head, err := retirementHead("pull_request_target", e); err != nil || head != e.Before {
		t.Fatalf("valid previous head rejected: %s %v", head, err)
	}
	for _, change := range []string{"action", "number", "repository", "head", "before", "same head", "closed state"} {
		t.Run(change, func(t *testing.T) {
			bad := e
			switch change {
			case "action":
				bad.Action = "opened"
			case "number":
				bad.Number++
			case "repository":
				bad.Pull.Base.Repo.ID++
			case "head":
				bad.Pull.Head.SHA = "bad"
			case "before":
				bad.Before = "bad"
			case "same head":
				bad.Before = bad.Pull.Head.SHA
			case "closed state":
				bad.Pull.State = "closed"
			}
			if _, err := retirementHead("pull_request_target", bad); err == nil {
				t.Fatal("invalid retirement route accepted")
			}
		})
	}
	e.Action = "closed"
	e.Pull.State = "closed"
	if head, err := retirementHead("pull_request_target", e); err != nil || head != f.pr.Head.SHA {
		t.Fatalf("valid close route rejected: %s %v", head, err)
	}
	e = f.event
	e.Ref = "refs/heads/main"
	e.Inputs.PR = ""
	e.Inputs.Head = f.pr.Head.SHA
	if head, err := retirementHead("workflow_dispatch", e); err != nil || head != e.Inputs.Head {
		t.Fatalf("valid backfill rejected: %s %v", head, err)
	}
	for _, ref := range []string{"refs/heads/other", ""} {
		e.Ref = ref
		if _, err := retirementHead("workflow_dispatch", e); err == nil {
			t.Fatal("non-main backfill accepted")
		}
	}
}

func TestRetirementCompletePRPaginationProtectsSecondPageRole(t *testing.T) {
	for _, outage := range []bool{false, true} {
		t.Run(fmt.Sprint(outage), func(t *testing.T) {
			f, _ := retirementFixture(t, queueCheckName)
			page := make([]pull, 100)
			for i := range page {
				p := f.pr
				p.Number = 1000 + i
				p.Head.SHA = fmt.Sprintf("%040x", i+1)
				page[i] = p
			}
			f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = page
			p := f.pr
			p.Number = 2000
			var second any = []pull{p}
			if outage {
				second = errors.New("second open-PR page unavailable")
			}
			f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=2")] = second
			err := f.c.retireChecks(context.Background(), f.c.expectedHead)
			if (err != nil) != outage {
				t.Fatalf("paginated role outcome %v", err)
			}
			if len(f.api.writes) != 0 {
				t.Fatal("role beyond first page was retired")
			}
		})
	}
}
func TestRetirementRejectsLateForeignHistoryBeforeAnyMutation(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	for i := 2; i <= 101; i++ {
		v := f.api.checks[0]
		v.ID = int64(i)
		f.api.checks = append(f.api.checks, v)
	}
	f.api.checks[100].ExternalID = "native foreign check on second history page"
	if err := f.c.retireChecks(context.Background(), f.c.expectedHead); err == nil {
		t.Fatal("late history collision accepted")
	}
	if len(f.api.writes) != 0 {
		t.Fatal("partial inventory authorized retirement")
	}
}
func TestAdmissionCloseDuringFinalComparisonRetiresItsPendingBarrier(t *testing.T) {
	f := newQueueFixture(t)
	v := check{ID: 1, Name: queueCheckName, HeadSHA: f.pr.Head.SHA, ExternalID: newBinding(f.c.repo.ID, f.pr, &f.feedbackRun).externalID(), Status: "queued"}
	v.App.ID, v.App.Slug = actionsAppID, "github-actions"
	f.api.checks = []check{v}
	f.c.verify = func(context.Context, int, string) (policyDecision, error) {
		f.pr.State = "closed"
		f.api.responses[f.c.endpoint("pulls/7")] = f.pr
		noRetirementRoles(f)
		return policyDecision{Code: 0}, nil
	}
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	if len(f.api.checks) != 1 || f.api.checks[0].Status != "completed" || f.api.checks[0].Conclusion != "cancelled" {
		t.Fatalf("late close left pending barrier: %+v", f.api.checks)
	}
	writes := len(f.api.writes)
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != writes {
		t.Fatal("late close callback revived barrier")
	}
}
func TestRetiredGroupCallbackCancelsExistingPendingRecord(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	f.c.expectedHead = f.groupRun.HeadSHA
	f.api.checks[0].HeadSHA = f.groupRun.HeadSHA
	p := f.pr
	p.Head.SHA = f.groupRun.HeadSHA
	f.api.checks[0].ExternalID = newBinding(f.c.repo.ID, p, &f.groupRun).externalID()
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	if len(f.api.checks) != 1 || f.api.checks[0].Status != "completed" || f.api.checks[0].Conclusion != "cancelled" {
		t.Fatalf("obsolete group callback stranded pending record: %+v", f.api.checks)
	}
}

func TestRetirementPushSelectsOnlyCurrentAndPreviousMainHeads(t *testing.T) {
	for _, which := range []string{"current", "previous", "foreign", "zero previous", "wrong branch"} {
		t.Run(which, func(t *testing.T) {
			f, _ := retirementFixture(t, queueCheckName)
			e := f.event
			e.Ref = "refs/heads/main"
			e.After = f.c.expectedHead
			e.Before = strings.Repeat("c", 40)
			switch which {
			case "previous":
				f.c.expectedHead = e.Before
				f.api.checks = []check{}
			case "foreign":
				f.c.expectedHead = strings.Repeat("e", 40)
			case "zero previous":
				e.Before = strings.Repeat("0", 40)
				f.c.expectedHead = e.Before
			case "wrong branch":
				e.Ref = "refs/heads/other"
			}
			err := f.c.retireEvent(context.Background(), "push", e)
			wantFailure := which == "foreign" || which == "zero previous" || which == "wrong branch"
			if (err != nil) != wantFailure {
				t.Fatalf("push retirement error=%v want failure=%t", err, wantFailure)
			}
			if wantFailure && len(f.api.writes) != 0 {
				t.Fatal("untrusted push selected retirement lease")
			}
		})
	}
}
func TestRetirementScheduleUsesExactDefaultBranchSnapshot(t *testing.T) {
	for _, ref := range []string{"refs/heads/main", "refs/heads/other"} {
		t.Run(ref, func(t *testing.T) {
			f, _ := retirementFixture(t, queueCheckName)
			t.Setenv("GITHUB_REF", ref)
			t.Setenv("PR_GATE_RETIREMENT_HEAD", f.c.expectedHead)
			if err := f.c.retireEvent(context.Background(), "schedule", f.event); (err != nil) != (ref != "refs/heads/main") {
				t.Fatalf("schedule retirement: %v", err)
			}
		})
	}
}
func TestRetirementRejectsStandaloneSourceCompletion(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	e := f.event
	e.Action = "completed"
	e.Run = f.groupRun
	if err := f.c.retireEvent(context.Background(), "workflow_run", e); err == nil {
		t.Fatal("source completion may not schedule a competing retirement writer")
	}
	if len(f.api.writes) != 0 {
		t.Fatal("unsupported source completion mutated checks")
	}
}

func TestRetirementNoopRenewsRolesAfterEmptyOrTerminalInventory(t *testing.T) {
	for _, inventory := range []string{"empty", "success", "failure"} {
		for _, role := range []string{"reopened PR", "reused group"} {
			t.Run(inventory+"/"+role, func(t *testing.T) {
				f, _ := retirementFixture(t, queueCheckName)
				if inventory == "empty" {
					f.api.checks = []check{}
				} else {
					f.api.checks[0].Status = "completed"
					f.api.checks[0].Conclusion = inventory
				}
				injected := false
				f.api.readHook = func(path string, n int) {
					if strings.Contains(path, "/check-runs?") && n == 2 {
						injected = true
						if role == "reopened PR" {
							f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr}
						} else {
							f.entry.Head.OID = f.c.expectedHead
							f.groupRefresh()
						}
					}
				}
				e := f.event
				e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, f.c.expectedHead)
				err := f.q.runHead(context.Background(), "workflow_dispatch", e, "admission")
				if err != nil && !onlyStaleHeadWork(err) || !injected || !blockingQueueCheck(newestQueueCheck(f.api.checks)) {
					t.Fatalf("reused head bypassed blocking: injected=%t checks=%+v err=%v", injected, f.api.checks, err)
				}
			})
		}
	}
}

func TestRetiredDispositionCannotCarryAcrossRenewedLiveRole(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	f.api.checks[0].Status, f.api.checks[0].Conclusion = "completed", "success"
	prior := f.api.checks[0]
	f.pr.State = "closed"
	f.refresh()
	noRetirementRoles(f)
	if err := f.q.execute(context.Background(), "workflow_dispatch", f.event); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != 0 || f.api.checks[0] != prior {
		t.Fatal("inactive head rewrote historical proof")
	}
	f.pr.State = "open"
	f.refresh()
	f.c.verify = func(context.Context, int, string) (policyDecision, error) {
		return policyDecision{}, errors.New("renewed role authority unavailable")
	}
	e := f.event
	e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, f.c.expectedHead)
	if err := f.q.runHead(context.Background(), "workflow_dispatch", e, "admission"); err == nil {
		t.Fatal("new invocation reused retired disposition instead of failing fresh authority")
	}
	if len(f.api.checks) != 2 || f.api.checks[0] != prior || !blockingQueueCheck(newestQueueCheck(f.api.checks)) {
		t.Fatal("renewed live role retained effective historical green")
	}
}

// One callback can route to several PRs at the held SHA. A later closed PR
// must renew retirement after an earlier route already proved absence: that
// earlier disposition cannot hide a newly active sibling's old green.
func TestRetiredDispositionCannotSurviveRenewedRoleInSameCallback(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	f.api.checks[0].Status, f.api.checks[0].Conclusion = "completed", "success"
	prior := f.api.checks[0]
	first, second, renewed := f.pr, f.pr, f.pr
	second.Number, renewed.Number = 8, 9
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{first, second}
	first.State, second.State = "closed", "closed"
	f.api.responses[f.c.endpoint("pulls/7")] = first
	f.api.responses[f.c.endpoint("pulls/8")] = second
	renewedAfterRetirement := false
	f.api.readHook = func(path string, n int) {
		if n != 1 {
			return
		}
		switch path {
		case f.c.endpoint("pulls/7"):
			noRetirementRoles(f)
		case f.c.endpoint("pulls/8"):
			history := f.c.endpoint(fmt.Sprintf("commits/%s/check-runs?check_name=%s&filter=all&per_page=100&page=1", f.c.expectedHead, queueCheckName))
			renewedAfterRetirement = f.api.reads[history] == 2
			f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{renewed}
		}
	}
	e := f.event
	e.Run = f.feedbackRun
	if err := f.q.execute(context.Background(), "workflow_run", e); err != nil {
		t.Fatal(err)
	}
	if !renewedAfterRetirement || len(f.api.checks) != 2 || f.api.checks[0] != prior || !blockingQueueCheck(newestQueueCheck(f.api.checks)) {
		t.Fatal("earlier retirement disposition hid renewed same-head role or rewrote its historical proof")
	}
}

func TestRetirementWorkerAcceptsOnlyExactHeadFromManualDiscovery(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	e := f.event
	e.Ref = "refs/heads/main"
	e.Inputs.PR = ""
	e.Inputs.Head = ""
	t.Setenv("PR_GATE_RETIREMENT_HEAD", f.c.expectedHead)
	if err := f.c.retireEvent(context.Background(), "workflow_dispatch", e); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != 1 || f.api.checks[0].Conclusion != "cancelled" {
		t.Fatal("manual discovery worker failed exact-head owned cancellation")
	}
}

func TestGroupCallbackMainAdvanceWithStaleQueueDoesNotReviveGreen(t *testing.T) {
	f := newQueueFixture(t)
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	prior := append([]check(nil), f.api.checks...)
	writes := len(f.api.writes)
	path := f.c.endpoint("pulls/7")
	next := f.api.reads[path] + 1
	f.api.readHook = func(got string, n int) {
		if got == path && n == next {
			// Keep both the old materialized queue entry and stale open member
			// metadata while the independent live main ref has already landed.
			f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": f.groupRun.HeadSHA}}
		}
	}
	dcos := 0
	f.q.dco = func(context.Context, pull) (int, error) {
		dcos++
		return 0, errors.New("DCO check: empty commit range")
	}
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	if dcos != 0 || len(f.api.writes) != writes || len(f.api.checks) != len(prior) {
		t.Fatalf("landed callback with stale queue revived green: dcos=%d writes=%+v", dcos, f.api.writes[writes:])
	}
	for i := range prior {
		if f.api.checks[i] != prior[i] {
			t.Fatal("landed terminal group verdict changed")
		}
	}
}

func TestLandedGroupRetirementPreservesEveryOpenPRRole(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	f.c.expectedHead = f.groupRun.HeadSHA
	p := f.pr
	p.Number = 8
	p.Head.SHA = f.c.expectedHead
	f.api.checks[0].HeadSHA = p.Head.SHA
	f.api.checks[0].ExternalID = newBinding(f.c.repo.ID, p, nil).externalID()
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{p}
	f.groupRefresh()
	f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": f.c.expectedHead}}
	if err := f.q.reassess(context.Background(), func(ctx context.Context) error {
		return f.q.reconciliation.retireHead(ctx, true)
	}); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != 0 || f.api.checks[0].Status != "queued" {
		t.Fatal("landed group exemption retired active same-SHA PR")
	}
}
func TestLandedGroupRetirementRenewsMainAfterInventory(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	f.c.expectedHead = f.groupRun.HeadSHA
	p := f.pr
	p.Head.SHA = f.c.expectedHead
	f.api.checks[0].HeadSHA = p.Head.SHA
	f.api.checks[0].ExternalID = newBinding(f.c.repo.ID, p, nil).externalID()
	f.api.checks[0].Status = "completed"
	f.api.checks[0].Conclusion = "success"
	f.groupRefresh()
	f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": f.c.expectedHead}}
	injected := false
	f.api.readHook = func(path string, n int) {
		if strings.Contains(path, "/check-runs?") && n == 2 {
			injected = true
			f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": strings.Repeat("e", 40)}}
		}
	}
	if err := f.q.reassess(context.Background(), func(ctx context.Context) error {
		return f.q.reconciliation.retireHead(ctx, true)
	}); err != nil {
		t.Fatal(err)
	}
	if !injected || !blockingQueueCheck(newestQueueCheck(f.api.checks)) || len(f.api.checks) != 2 || f.api.checks[0].Conclusion != "success" {
		t.Fatal("landed-group disposition retained stale main proof or changed historical verdict")
	}
}
func TestGenericRetirementCannotUseLandedGroupException(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	f.entry.Head.OID = f.c.expectedHead
	f.groupRefresh()
	f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": f.c.expectedHead}}
	if err := f.c.retireChecks(context.Background(), f.c.expectedHead); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != 0 || f.api.checks[0].Status != "queued" {
		t.Fatal("generic cleanup borrowed stale-group callback authority")
	}
}
func TestStaleLandedGroupCallbackRetiresPendingRecord(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	f.c.expectedHead = f.groupRun.HeadSHA
	p := f.pr
	p.Head.SHA = f.c.expectedHead
	f.api.checks[0].HeadSHA = p.Head.SHA
	f.api.checks[0].ExternalID = newBinding(f.c.repo.ID, p, &f.groupRun).externalID()
	f.groupRefresh()
	f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": f.c.expectedHead}}
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	if len(f.api.checks) != 1 || f.api.checks[0].Status != "completed" || f.api.checks[0].Conclusion != "cancelled" {
		t.Fatalf("landed stale queue stranded pending group: %+v", f.api.checks)
	}
}
