package app

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// rotationWorker drains due scheduled-rotation work (#148, S2a). Like the
// dynamic-lease worker it runs on every node: each run is claimed under its own
// row lease (lease_owner, lease_expires_at, claim token, SKIP LOCKED, per-org
// cap), so it composes with multi-node HA without the singleton scheduler lease.
type rotationWorker struct {
	svc        *service.RotationExecutor
	id         string
	poll       time.Duration
	log        *slog.Logger
	selfConfig *service.SelfConfig
	// sweeps counts completed executor sweeps; the wiring test reads it to prove
	// the loop was started by the owning generation.
	sweeps atomic.Int64
}

const stageSelfConfig = "self_config"

// logFailure logs a failed loop iteration with the stage it failed in. The
// self-config capture and a misconfigured executor hold no tenant or provider
// data, so their error is logged as is, like the sibling workers do. Every other
// stage may carry tenant, provider or driver detail (a claim or select error is a
// driver error, a run's error can quote a provider), so only a fixed-word class is
// logged, and the executor already logs the run an error belongs to.
func (w *rotationWorker) logFailure(stage string, err error) {
	switch stage {
	case stageSelfConfig, service.SweepStageMisconfigured:
		w.log.Error("credential rotation worker failed", "stage", stage, "error", err)
	default:
		w.log.Error("credential rotation worker failed", "stage", stage, "class", service.RotationErrorClass(err))
	}
}

func (w *rotationWorker) Run(ctx context.Context) {
	poll := w.poll
	if poll <= 0 {
		poll = 5 * time.Second
	}
	for {
		var worked bool
		var err error
		stage := ""
		if w.selfConfig != nil {
			if _, err = w.selfConfig.Capture(ctx); err != nil {
				stage = stageSelfConfig
			}
		}
		if err == nil {
			worked, err = w.svc.RunRotationSweep(ctx, w.id)
			w.sweeps.Add(1)
			if err != nil {
				stage = service.RotationSweepStage(err)
			}
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			w.logFailure(stage, err)
		}
		if worked && err == nil && ctx.Err() == nil {
			continue
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// rotationGaugeSource feeds the label-free /metrics gauges at scrape time. A
// datastore hiccup is reported as an error so the collector marks the gauges
// unknown instead of rendering zeros.
type rotationGaugeSource struct {
	runtime *store.RotationRuntime
	log     *slog.Logger
}

// RotationSnapshot reads the instance-wide rotation counts with a two-second
// timeout. A failed read returns an error; the counts are then unavailable.
func (s rotationGaugeSource) RotationSnapshot() (server.RotationCounts, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	g, err := s.runtime.Gauges(ctx, time.Now().UTC())
	if err != nil {
		s.log.Warn("rotation gauge scrape failed", "class", service.RotationErrorClass(err))
		return server.RotationCounts{}, err
	}
	return server.RotationCounts{Quarantined: g.Quarantined, UnknownOutcome: g.UnknownOutcome, RevokeRefused: g.RevokeRefused, Halted: g.Halted, RestoreHeld: g.RestoreHeld, Overdue: g.Overdue, CredentialsExpiring: g.Expiring}, nil
}
