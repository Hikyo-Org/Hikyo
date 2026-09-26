package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
)

// sshSweepInterval is how often each node re-decides every live SSH
// certificate's requester authority (#155). A requester whose grant or
// profile membership was pulled is revoked (and enters the KRL) within one
// interval. The sweep's only write is idempotent, so it runs on every node of
// a #146 cluster without a singleton lease.
const sshSweepInterval = 30 * time.Second

type sshSweeper struct {
	svc        *service.SSH
	poll       time.Duration
	log        *slog.Logger
	selfConfig *service.SelfConfig
}

func (w *sshSweeper) Run(ctx context.Context) {
	poll := w.poll
	if poll <= 0 {
		poll = sshSweepInterval
	}
	for {
		var err error
		if w.selfConfig != nil {
			_, err = w.selfConfig.Capture(ctx)
		}
		if err == nil {
			var revoked int
			revoked, err = w.svc.RunSweep(ctx)
			if revoked > 0 {
				w.log.Info("ssh certificates revoked after requester authority lapsed", "count", revoked)
			}
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			w.log.Error("ssh certificate sweeper failed", "err", err)
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

// sshGaugeSource feeds the two label-free /metrics gauges at scrape time.
type sshGaugeSource struct {
	svc *service.SSH
	log *slog.Logger
}

func (s sshGaugeSource) SSHSnapshot() (active, krlEntries int64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	active, krlEntries, err = s.svc.Gauges(ctx)
	if err != nil {
		s.log.Warn("ssh certificate gauge scrape failed", "err", err)
		return 0, 0, err
	}
	return active, krlEntries, nil
}
