package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
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

// Every failed iteration logs the stage it failed in, in a fixed word. The
// stages that name no run log a fixed-word class instead of the driver's text, and
// no tenant, provider or credential string reaches the log.
func TestRotationWorkerLogsStageAndNoSecrets(t *testing.T) {
	const secret = "hunter2-provider-credential"
	cases := []struct {
		name  string
		stage string
		err   error
		want  []string
		deny  []string
	}{
		{"self config", stageSelfConfig, errors.New("capture failed"), []string{"stage=self_config", "capture failed"}, nil},
		{"misconfigured", service.SweepStageMisconfigured, &service.SweepError{Stage: service.SweepStageMisconfigured, Err: errors.New("executor misconfigured")}, []string{"stage=misconfigured", "executor misconfigured"}, nil},
		{"claim", service.SweepStageClaim, &service.SweepError{Stage: service.SweepStageClaim, Err: errors.New("driver: password=" + secret)}, []string{"stage=claim", "class=datastore"}, []string{secret, "driver"}},
		{"select", service.SweepStageSelect, &service.SweepError{Stage: service.SweepStageSelect, Err: errors.New("driver: password=" + secret)}, []string{"stage=select", "class=datastore"}, []string{secret, "driver"}},
		{"run", service.SweepStageRun, &service.SweepError{Stage: service.SweepStageRun, Err: errors.New("provider said token=" + secret)}, []string{"stage=run", "class=internal"}, []string{secret, "provider said"}},
		{"classified run", service.SweepStageRun, &service.SweepError{Stage: service.SweepStageRun, Err: fmt.Errorf("%w: %s", dynamic.ErrUnreachable, secret)}, []string{"stage=run", "class=provider_unreachable"}, []string{secret}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := &rotationWorker{log: slog.New(slog.NewTextHandler(&buf, nil))}
			w.logFailure(c.stage, c.err)
			out := buf.String()
			for _, want := range c.want {
				if !strings.Contains(out, want) {
					t.Fatalf("log %q lacks %q", out, want)
				}
			}
			for _, deny := range c.deny {
				if strings.Contains(out, deny) {
					t.Fatalf("log %q leaks %q", out, deny)
				}
			}
		})
	}
}

// The executor tags its own failures with the stage they came from: a
// misconfigured executor and a claim against a closed datastore.
func TestRotationSweepErrorsCarryTheirStage(t *testing.T) {
	if _, err := (&service.RotationExecutor{}).RunRotationSweep(t.Context(), "w"); service.RotationSweepStage(err) != service.SweepStageMisconfigured {
		t.Fatalf("a misconfigured executor failed at stage %q (%v)", service.RotationSweepStage(err), err)
	}
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	wired := srv.owner.current.graph.rotationWorker
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = wired.svc.RunRotationSweep(t.Context(), "w")
	if err == nil {
		t.Skip("the closed datastore still served the sweep")
	}
	if stage := service.RotationSweepStage(err); stage != service.SweepStageClaim {
		t.Fatalf("a claim against a closed datastore failed at stage %q (%v)", stage, err)
	}
	if class := service.RotationErrorClass(err); class != "datastore" {
		t.Fatalf("class = %q, want datastore", class)
	}
}
