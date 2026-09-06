package app

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/dynamic/postgres"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// dynamicProviderDeadline bounds every provider round-trip. It is shorter than
// the worker's lease term (2 min) and the request-path claim, so a slow or
// hung provider cannot outlive the crash fence that protects the lease row.
const dynamicProviderDeadline = 10 * time.Second

// newDynamicFactory builds the dynamic-secret provider factory, closing over the
// operator egress policy. The service and worker call it without naming a
// concrete engine package; only postgres is wired in v1.
func newDynamicFactory(egress map[string][]netip.Prefix) dynamic.Factory {
	return func(kind dynamic.Kind, origin, tlsMode, credential string) (dynamic.Provider, error) {
		switch kind {
		case dynamic.KindPostgres:
			return postgres.New(postgres.Config{
				Origin: origin, Password: credential,
				AllowedCIDRs: append([]netip.Prefix(nil), egress[origin]...),
				Deadline:     dynamicProviderDeadline,
			})
		default:
			return nil, errors.New("app: unsupported dynamic secret provider")
		}
	}
}

// dynamicWorker drains due lease transitions. Like the adapter worker it runs on
// every node: each transition is claimed under the lease row's own crash fence
// (lease_owner + lease_expires_at, SKIP LOCKED, per-org cap), so it composes
// with #146 multi-node HA without the singleton scheduler lease.
type dynamicWorker struct {
	svc        *service.Dynamic
	id         string
	poll       time.Duration
	log        *slog.Logger
	selfConfig *service.SelfConfig
}

// dynamicGaugeSource feeds the two label-free /metrics gauges at scrape time.
// A datastore hiccup renders zeros rather than failing the scrape.
type dynamicGaugeSource struct {
	runtime *store.DynamicRuntime
	log     *slog.Logger
}

func (s dynamicGaugeSource) DynamicSnapshot() (activeLeases, unknownEffects int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	active, unknown, err := s.runtime.Gauges(ctx)
	if err != nil {
		s.log.Warn("dynamic-secret gauge scrape failed", "err", err)
		return 0, 0
	}
	return active, unknown
}

func (w *dynamicWorker) Run(ctx context.Context) {
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
			worked, err = w.svc.RunLeaseSweep(ctx, w.id)
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			w.log.Error("dynamic lease worker failed", "err", err)
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
