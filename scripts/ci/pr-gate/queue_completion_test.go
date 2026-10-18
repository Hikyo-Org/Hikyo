package main

import (
	"context"
	"encoding/base64"
	"testing"
)

func TestQueueCompletedGroupRefusalFinishesItsExistingCheck(t *testing.T) {
	f := newQueueFixture(t)
	f.groupRun.Status, f.groupRun.Conclusion = "in_progress", ""
	f.groupRefresh()
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	queued := newestQueueCheck(f.api.checks)
	if queued == nil || queued.Status != "queued" {
		t.Fatal("running validation did not establish its pending check")
	}
	f.groupRun.Status, f.groupRun.Conclusion = "completed", "success"
	f.groupRefresh()
	f.api.responses[f.c.endpoint("contents/.github/workflows/ci.yml?ref="+f.groupRun.HeadSHA)] = map[string]string{
		"type": "file", "path": ".github/workflows/ci.yml", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("unapproved validation contract")),
	}
	for range 2 {
		if err := f.group(); err == nil {
			t.Fatal("unapproved validation contract was not refused")
		}
		latest := newestQueueCheck(f.api.checks)
		if latest == nil || latest.ID != queued.ID || latest.Status != "completed" || latest.Conclusion != "failure" {
			t.Fatalf("completed source refusal left a stranded or different check: %+v", latest)
		}
	}
}

func TestQueueFinishedValidationCannotWaitForAnotherPolicyCallback(t *testing.T) {
	f := newQueueFixture(t)
	f.c.verify = func(context.Context, int, string) (policyDecision, error) {
		return policyDecision{Code: 75}, nil
	}
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	latest := newestQueueCheck(f.api.checks)
	if latest == nil || latest.Status != "completed" || latest.Conclusion != "failure" {
		t.Fatalf("finished validation retained a pending policy verdict: %+v", latest)
	}
}

func TestQueueForkFeedbackWithExactRefsDoesNotWaitForLazyMergeability(t *testing.T) {
	f := newQueueFixture(t)
	f.sourceBoundFork()
	f.pr.Mergeable = nil
	f.refresh()
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	latest := newestQueueCheck(f.api.checks)
	if latest == nil || latest.Status != "completed" || latest.Conclusion != "success" {
		t.Fatalf("exact source-ref proof was blocked by nullable mergeability: %+v", latest)
	}
}

func TestQueueCompletedFailureCannotRefuseAReplacedGroup(t *testing.T) {
	for _, movement := range []string{"attempt", "workflow", "branch", "conclusion", "queue membership", "policy"} {
		t.Run(movement, func(t *testing.T) {
			f := newQueueFixture(t)
			f.groupRun.Status, f.groupRun.Conclusion = "in_progress", ""
			f.groupRefresh()
			if err := f.group(); err != nil {
				t.Fatal(err)
			}
			f.groupRun.Status, f.groupRun.Conclusion = "completed", "success"
			f.groupRefresh()
			path := f.c.endpoint("contents/.github/workflows/ci.yml?ref=" + f.groupRun.HeadSHA)
			moved := false
			f.api.readHook = func(current string, count int) {
				if current != path || count != 1 {
					return
				}
				moved = true
				switch movement {
				case "attempt":
					f.groupRun.Attempt++
				case "workflow":
					f.groupRun.WorkflowID++
				case "branch":
					f.groupRun.HeadBranch += "-replacement"
				case "conclusion":
					f.groupRun.Conclusion = "cancelled"
				case "queue membership":
					f.entry.ID = "REPLACED_ENTRY"
				case "policy":
					f.owner["updated_at"] = "changed"
					f.policyRefresh()
				}
				f.groupRefresh()
				f.api.responses[path] = map[string]string{"type": "file", "path": ".github/workflows/ci.yml", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("unapproved contract"))}
			}
			if err := f.group(); err == nil || !moved {
				t.Fatalf("group movement was not observed and refused: %v", err)
			}
			latest := newestQueueCheck(f.api.checks)
			if latest == nil || latest.Status != "queued" || latest.Conclusion != "" {
				t.Fatalf("failure borrowed the replacement group's identity: %+v", latest)
			}
		})
	}
}
