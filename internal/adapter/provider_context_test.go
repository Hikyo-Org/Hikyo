package adapter

import (
	"context"
	"testing"
)

func TestModuleLeaseContextCleanupRunsOnceEvenAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	lease, err := NewModuleLeaseWithContext(attemptModule{}, func(got context.Context) {
		calls++
		if got != ctx || got.Err() != context.Canceled {
			t.Fatal("cleanup lost canceled operation context")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	lease.ReleaseContext(ctx)
	lease.ReleaseContext(t.Context())
	lease.Release()
	if calls != 1 {
		t.Fatalf("cleanup calls = %d; want 1", calls)
	}
}

func TestModuleLeaseLegacyCleanupIsNotSkippedByContextRelease(t *testing.T) {
	calls := 0
	lease, err := NewModuleLease(attemptModule{}, func() { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	lease.ReleaseContext(ctx)
	lease.Release()
	if calls != 1 {
		t.Fatalf("legacy cleanup calls = %d; want 1", calls)
	}
}
