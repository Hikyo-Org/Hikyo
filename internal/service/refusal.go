package service

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

const RefusalDeadline = 100 * time.Millisecond

// RefusalSignals owns bounded work and health, never authentication or quotas.
// Staging and sealing are coordinated by one locked datastore row across nodes.
type RefusalSignals struct {
	// Deadline bounds one accounting attempt; zero means RefusalDeadline.
	// Production never sets it: it is a seam for tests that count every sample
	// on a loaded runner, where the 100 ms bound may legitimately drop one.
	Deadline time.Duration
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

// NewRefusalSignals creates a bounded accounting owner for the application runtime.
func NewRefusalSignals(db *store.DB) *RefusalSignals {
	return &RefusalSignals{db: db, slots: make(chan struct{}, 8), waiters: make(chan struct{}, 64)}
}

// RefusalHealth exposes the last accounting result and current bounded work.
func (s *RefusalSignals) RefusalHealth() RefusalHealth {
	return RefusalHealth{Healthy: s.healthy.Load(), Failures: s.failures.Load(), PendingSince: s.pending.Load(), InFlight: len(s.slots), Waiting: len(s.waiters)}
}

// update bounds queue acquisition and the durable transaction by one deadline.
// Health records unconfirmed results without changing authentication responses.
func (s *RefusalSignals) update(ctx context.Context, sample *audit.RefusalSample) (err error) {
	// The refusal is already decided and its response is written regardless, so
	// the caller hanging up must not cancel its accounting: a client that
	// half-closes after sending would otherwise hide its own refusals. The
	// deadline alone bounds the work. Seal shares this path, so a shutdown waits
	// at most that deadline for a seal already in progress.
	deadline := s.Deadline
	if deadline == 0 {
		deadline = RefusalDeadline
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deadline)
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
	err = authn.RefusalDeadlineError(err) // also covers a COMMIT the server cancelled before committing
	if err == nil {
		s.pending.Store(pending)
		s.healthy.Store(true)
	}
	return err
}

// Record durably stages one request sample or exposes accounting failure.
func (s *RefusalSignals) Record(ctx context.Context, sample audit.RefusalSample) error {
	return s.update(ctx, &sample)
}

// Seal checks and atomically publishes expired staging under the writer owner.
func (s *RefusalSignals) Seal(ctx context.Context) error { return s.update(ctx, nil) }

// Run flushes quiet windows and recovers pending staging on startup. Every
// attempt is synchronous and deadline-bound; cancellation joins this worker.
// Each tick exercises the locked writer path even when staging is empty. A
// read-only pre-check would change that health probe and add another datastore
// protocol; the approved minimum keeps one bounded attempt per second.
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
