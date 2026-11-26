package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
)

func fastCaptureRetries(t *testing.T) {
	t.Helper()
	saved := captureRetryDelays
	captureRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { captureRetryDelays = saved })
}

func TestCaptureWithRetryWaitsForReconciliation(t *testing.T) {
	fastCaptureRetries(t)
	calls := 0
	err := captureWithRetry(t.Context(), func(context.Context) error {
		calls++
		if calls < 3 {
			return service.ErrSelfConfigFenced
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d, want success on the third call", err, calls)
	}
}

func TestCaptureWithRetryGivesUpAfterBound(t *testing.T) {
	fastCaptureRetries(t)
	calls := 0
	err := captureWithRetry(t.Context(), func(context.Context) error {
		calls++
		return service.ErrSelfConfigUnavailable
	})
	if !errors.Is(err, service.ErrSelfConfigUnavailable) || calls != len(captureRetryDelays)+1 {
		t.Fatalf("err=%v calls=%d, want unavailable after %d calls", err, calls, len(captureRetryDelays)+1)
	}
}

func TestCaptureWithRetryDoesNotRetryOtherErrors(t *testing.T) {
	fastCaptureRetries(t)
	calls := 0
	boom := errors.New("boom")
	err := captureWithRetry(t.Context(), func(context.Context) error { calls++; return boom })
	if !errors.Is(err, boom) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want boom after 1 call", err, calls)
	}
}
