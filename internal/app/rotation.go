package app

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
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

func (w *rotationWorker) Run(ctx context.Context) {
	poll := w.poll
	if poll <= 0 {
		poll = 5 * time.Second
	}
	for {
		var worked bool
		var err error
		if w.selfConfig != nil {
			_, err = w.selfConfig.Capture(ctx)
		}
		if err == nil {
			worked, err = w.svc.RunRotationSweep(ctx, w.id)
			w.sweeps.Add(1)
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			w.log.Error("credential rotation worker failed", "err", err)
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
