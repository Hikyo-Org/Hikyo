package service

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

const RefusalDeadline = 100 * time.Millisecond

// RefusalSignals owns bounded work and health, never authentication or quotas.
// Staging and sealing are coordinated by one locked datastore row across nodes.
type RefusalSignals struct {
	db       *store.DB
	slots    chan struct{}
	waiters  chan struct{}
	healthy  atomic.Bool
	failures atomic.Uint64
	pending  atomic.Int64
}

type RefusalHealth struct {
	Healthy           bool
	Failures          uint64
	PendingSince      int64
	InFlight, Waiting int
}

func NewRefusalSignals(db *store.DB) *RefusalSignals {
	return &RefusalSignals{db: db, slots: make(chan struct{}, 8), waiters: make(chan struct{}, 64)}
}

func (s *RefusalSignals) RefusalHealth() RefusalHealth {
	return RefusalHealth{Healthy: s.healthy.Load(), Failures: s.failures.Load(), PendingSince: s.pending.Load(), InFlight: len(s.slots), Waiting: len(s.waiters)}
}

func (s *RefusalSignals) update(ctx context.Context, sample *audit.RefusalSample) (err error) {
	ctx, cancel := context.WithTimeout(ctx, RefusalDeadline)
	defer cancel()
	defer func() {
		if err != nil {
			s.failures.Add(1)
			s.healthy.Store(false)
		}
	}()
	select {
	case s.slots <- struct{}{}:
	default:
		select {
		case s.waiters <- struct{}{}:
		default:
			return context.DeadlineExceeded
		}
		select {
		case s.slots <- struct{}{}:
			<-s.waiters
		case <-ctx.Done():
			<-s.waiters
			return ctx.Err()
		}
	}
	defer func() { <-s.slots }()
	var pending int64
	// Row locking plus the existing SERIALIZABLE retry loop coordinates nodes.
	// Do not use the session-lock helper: its independent unlock grace would
	// extend this signal's approved total deadline after cancellation.
	err = tx.Write(ctx, s.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		var err error
		pending, err = az.UpdateRequestRefusals(ctx, sample)
		return err
	})
	if err == nil {
		s.pending.Store(pending)
		s.healthy.Store(true)
	}
	return err
}

func (s *RefusalSignals) Record(ctx context.Context, sample audit.RefusalSample) error {
	return s.update(ctx, &sample)
}
func (s *RefusalSignals) Seal(ctx context.Context) error { return s.update(ctx, nil) }

// Run flushes quiet windows and recovers pending staging on startup. Every
// attempt is synchronous and deadline-bound; cancellation joins this worker.
func (s *RefusalSignals) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	_ = s.Seal(ctx)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Seal(ctx)
		}
	}
}
