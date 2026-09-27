package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// pkiWorker is the private-PKI loop (#154, pki ADR D6-D8). Like the dynamic
// lease worker it runs on every node: stale `issuing` rows become `unknown`,
// expired leaves become `expired`, and due CRLs are re-signed, each under a
// row-level compare-and-swap, so it composes with #146 multi-node HA without
// the singleton scheduler lease.
type pkiWorker struct {
	svc        *service.PKI
	poll       time.Duration
	log        *slog.Logger
	selfConfig *service.SelfConfig
}

// Run sweeps PKI state until ctx is canceled, retrying errors on subsequent
// iterations. It repeats immediately after reported work, otherwise waiting
// for poll (five seconds when nonpositive). A self-config capture failure
// skips that iteration's sweep.
func (w *pkiWorker) Run(ctx context.Context) {
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
			worked, err = w.svc.RunPKISweep(ctx)
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			w.log.Error("pki worker failed", "err", err)
		}
		if worked && ctx.Err() == nil {
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

// pkiGaugeSource feeds the label-free /metrics gauges at scrape time. A
// datastore hiccup is reported as an error so the collector marks the gauges
// unknown instead of rendering zeros.
type pkiGaugeSource struct {
	runtime *store.PKIRuntime
	log     *slog.Logger
}

// PKISnapshot reads instance-wide live, unknown, and held counts with a
// two-second timeout. Read failures return zero counts and an error; callers
// must treat those counts as unavailable.
func (s pkiGaugeSource) PKISnapshot() (live, unknown, held int64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gauges, err := s.runtime.Gauges(ctx, time.Now().UTC())
	if err != nil {
		s.log.Warn("pki gauge scrape failed", "err", err)
		return 0, 0, 0, err
	}
	return gauges.LiveCertificates, gauges.UnknownCertificates, gauges.HeldIssuers, nil
}
