package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestQueueCrossHeadLegacyManualRepairPreservesFreshHeldProof(t *testing.T) {
	f := newQueueFixture(t)
	if err := queueCrossHeadRun(f, "admission", context.Background()); err != nil {
		t.Fatal(err)
	}
	before := len(f.api.writes)
	count := len(f.api.checks)
	for i := 0; i < 3; i++ {
		err := f.q.execute(context.Background(), "workflow_dispatch", f.event)
		if err != nil && !onlyStaleHeadWork(err) {
			t.Fatalf("unrelated repair returned material error: %v", err)
		}
	}
	if len(f.api.writes) != before || len(f.api.checks) != count || queueCrossHeadLatest(f, "admission").Conclusion != "success" {
		t.Fatalf("unrelated group repair invalidated current proved head: oldrows%d newrows%d writes%d", count, len(f.api.checks), len(f.api.writes)-before)
	}
}
func TestQueueCrossHeadWorkerDoesNotVisitUnrelatedRepair(t *testing.T) {
	f := newQueueFixture(t)
	path := f.c.endpoint("actions/workflows/ci-merge-group.yml/runs?event=merge_group&head_sha=" + f.groupRun.HeadSHA + "&per_page=100&page=1")
	f.api.responses[path] = errors.New("unrelated group lookup must not be consulted")
	for i := 0; i < 3; i++ {
		if err := queueCrossHeadRun(f, "admission", context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if f.api.reads[path] != 0 {
		t.Fatal("headworker consulted unrelated group repair")
	}
}
func TestQueueCrossHeadMixedHeldErrorStillBlocks(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "joined"}[wrapped], func(t *testing.T) {
			f := newQueueFixture(t)
			if err := queueCrossHeadRun(f, "admission", context.Background()); err != nil {
				t.Fatal(err)
			}
			err := f.q.reassess(context.Background(), func(ctx context.Context) error {
				if err := f.q.admit(ctx, 7); err != nil {
					return err
				}
				held := errors.New("held head source became unavailable after proof")
				if wrapped {
					return errors.Join(&staleHeadWork{expected: strings.Repeat("d", 40)}, held)
				}
				return held
			})
			if err == nil {
				t.Fatal("held error lost")
			}
			queueCrossHeadBlocked(t, f, "admission")
		})
	}
}

func TestQueueCrossHeadRepairReadScope(t *testing.T) {
	for _, scope := range []string{"unrelated-api", "held-api", "malformed-identity"} {
		t.Run(scope, func(t *testing.T) {
			f := newQueueFixture(t)
			role := "admission"
			if scope == "held-api" {
				role = "group"
			}
			if err := queueCrossHeadRun(f, role, context.Background()); err != nil {
				t.Fatal(err)
			}
			before := len(f.api.writes)
			count := len(f.api.checks)
			path := f.c.endpoint("actions/workflows/ci-merge-group.yml/runs?event=merge_group&head_sha=" + f.groupRun.HeadSHA + "&per_page=100&page=1")
			f.api.responses[path] = errors.New("independent group lookup unavailable")
			reads := f.api.reads[path]
			if scope == "held-api" {
				f.q.repairOnly = true
			}
			if scope == "malformed-identity" {
				f.entry.Head.OID = "not-a-sha"
				f.groupRefresh()
			}
			err := f.q.execute(context.Background(), "workflow_dispatch", f.event)
			if scope == "unrelated-api" {
				if err != nil || f.api.reads[path] != reads || len(f.api.writes) != before || len(f.api.checks) != count {
					t.Fatalf("unrelatedlookup was consulted or invalidated current proof: %v reads%d writes%d", err, f.api.reads[path]-reads, len(f.api.writes)-before)
				}
			} else {
				if err == nil {
					t.Fatal("held/unknown error disappeared")
				}
				queueCrossHeadBlocked(t, f, role)
			}
		})
	}
}

func queueCrossHeadRun(f *queueFixture, role string, ctx context.Context) error {
	head := f.pr.Head.SHA
	if role == "group" {
		head = f.groupRun.HeadSHA
	}
	f.c.expectedHead = head
	e := f.event
	e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":%q}]`, head, role)
	return f.q.runHead(ctx, "workflow_dispatch", e, role)
}
func queueCrossHeadLatest(f *queueFixture, role string) *check {
	head := f.pr.Head.SHA
	if role == "group" {
		head = f.groupRun.HeadSHA
	}
	var own []check
	for _, v := range f.api.checks {
		if v.HeadSHA == head {
			own = append(own, v)
		}
	}
	return newestQueueCheck(own)
}
func queueCrossHeadBlocked(t *testing.T, f *queueFixture, role string) {
	t.Helper()
	if !blockingQueueCheck(queueCrossHeadLatest(f, role)) {
		t.Fatal("held error left its effective proof passing")
	}
}

func TestQueueCrossHeadPostProofHeldPolicyErrorStillBlocks(t *testing.T) {
	f := newQueueFixture(t)
	if err := queueCrossHeadRun(f, "admission", context.Background()); err != nil {
		t.Fatal(err)
	}
	path := f.c.endpoint(fmt.Sprintf("rulesets/%d", queueRulesetID))
	base := f.api.reads[path]
	observed := false
	f.api.readHook = func(p string, n int) {
		if p == path && n == base+3 {
			observed = true
			f.api.responses[path] = errors.New("held policy refresh unavailable after proof")
		}
	}
	if err := f.q.execute(context.Background(), "workflow_dispatch", f.event); err == nil {
		t.Fatal("post-proof held policy error disappeared")
	}
	if !observed {
		t.Fatal("actual maintenance policy boundary was not exercised")
	}
	queueCrossHeadBlocked(t, f, "admission")
}
