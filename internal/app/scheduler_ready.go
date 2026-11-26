package app

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
)

// captureRetryDelays bounds the wait for runtime configuration at the start of
// a scheduled job: one try plus one retry per delay, about 10 s in total.
var captureRetryDelays = []time.Duration{
	100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond,
	1600 * time.Millisecond, 2400 * time.Millisecond, 4 * time.Second,
}

// captureWithRetry runs capture and retries only ErrSelfConfigUnavailable, which
// reports configuration that is still reconciling (including a failed metadata
// read). The scheduler runs each job once at startup and next an interval later,
// so a job refused during that window would otherwise lose its whole startup run.
func captureWithRetry(ctx context.Context, capture func(context.Context) error) error {
	err := capture(ctx)
	for _, delay := range captureRetryDelays {
		if err == nil || !errors.Is(err, service.ErrSelfConfigUnavailable) {
			return err
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		}
		err = capture(ctx)
	}
	return err
}
