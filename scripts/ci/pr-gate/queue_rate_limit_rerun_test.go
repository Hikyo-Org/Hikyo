package main

import (
	"context"
	"errors"
	"testing"
)

// limitedReads makes every read fail the way a spent window does once its reset
// is beyond the job's budget (awaitRateLimitReset's refusal) until lifted.
func (f *queueFixture) limitedReads() (lift func()) {
	limited := true
	f.probe().fault = func(context.Context, string, int) error {
		if limited {
			return &githubReadError{path: "repos", status: 403, rateLimited: true, cause: errors.New("rate limit exhausted beyond this job's remaining budget")}
		}
		return nil
	}
	return func() { limited = false }
}

// A rate-limited job whose reset is beyond its own budget fails and leaves the
// blocking check pending (#1553). The retry workflow reruns exactly that job after
// the reset; this pins that the rerun, with the same inputs and a fresh budget,
// completes the check, so the merge group never waits for its queue timeout.
func TestRateLimitedGroupJobCompletesItsCheckWhenRerunAfterTheReset(t *testing.T) {
	f := newQueueFixture(t)
	f.pendingGroup(t)
	lift := f.limitedReads()
	if err := f.group(); err == nil {
		t.Fatal("a job that cannot read inside its budget must fail so the retry can find it")
	}
	f.assertNoSuccess(t)
	f.assertNoTerminalWrites(t)
	lift()
	if err := f.group(); err != nil {
		t.Fatalf("the rerun after the reset failed: %v", err)
	}
	f.assertNewest(t, "success", "Merge group validated", "")
}

func TestRateLimitedPublishJobCompletesItsCheckWhenRerunAfterTheReset(t *testing.T) {
	f := newQueueFixture(t)
	lift := f.limitedReads()
	if err := f.admit(); err == nil {
		t.Fatal("a publisher that cannot read inside its budget must fail so the retry can find it")
	}
	f.assertNoSuccess(t)
	if len(f.api.writes) != 0 {
		t.Fatalf("a publisher that could not read must not have written: %+v", f.api.writes)
	}
	lift()
	if err := f.admit(); err != nil {
		t.Fatalf("the rerun after the reset failed: %v", err)
	}
	if got := f.newestOnHead(); got.Conclusion != "success" {
		t.Fatalf("the rerun must publish the head's admitted check, got %+v", got)
	}
}
