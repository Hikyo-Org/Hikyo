package app

import (
	"context"
	"testing"
	"time"
)

// A booted application starts the scheduled-rotation worker with a fully wired
// executor: the owning generation runs its loop, and one sweep on an empty
// instance reaches the datastore without the executor refusing for a missing
// dependency (#148, S2a).
func TestRotationWorkerIsWiredAndStartedByTheGeneration(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.ServeWithReady(ctx, nil) }()
	t.Cleanup(func() { cancel(); _ = <-done })
	worker := srv.owner.current.graph.rotationWorker
	if worker == nil || worker.svc == nil || worker.svc.Log == nil {
		t.Fatal("the rotation worker or its executor is not wired")
	}
	if did, err := worker.svc.RunRotationSweep(t.Context(), "wiring-test"); err != nil || did {
		t.Fatalf("a sweep on an empty instance = %v, %v", did, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for worker.sweeps.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the generation never ran the rotation loop")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A loop that is demonstrably running stops when its context is cancelled: the
// test waits for completed sweeps before it cancels, so a loop that never
// started, or one that ignores cancellation, cannot pass.
func TestRotationWorkerStopsWithItsContext(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()
	wired := srv.owner.current.graph.rotationWorker
	worker := &rotationWorker{svc: wired.svc, id: "stop-test", poll: time.Millisecond, log: testLogger()}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { worker.Run(ctx); close(done) }()
	deadline := time.Now().Add(10 * time.Second)
	for worker.sweeps.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatal("the loop never ran its sweeps")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the running rotation loop did not stop with its context")
	}
	settled := worker.sweeps.Load()
	time.Sleep(50 * time.Millisecond)
	if after := worker.sweeps.Load(); after != settled {
		t.Fatalf("the loop swept %d more times after it stopped", after-settled)
	}
}
