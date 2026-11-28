package app

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

const (
	defaultSchedulerInterval = time.Hour
	defaultJobDeadline       = 10 * time.Minute
	pruneStaleAfter          = 24 * time.Hour

	// schedulerLeaseName is the singleton_leases row the scheduler competes
	// for under HA. Every singleton job runs only on the node holding it.
	schedulerLeaseName = "scheduler"
	// defaultLeaseTTL and defaultHeartbeat are the HA lease timings the ops
	// spec amendment declares (RTO = lease TTL + probe period). The heartbeat
	// renews well within the TTL so a healthy leader never lapses; a leader
	// that cannot renew within the TTL loses the lease and its in-flight jobs
	// are cancelled (fail closed).
	defaultLeaseTTL  = 30 * time.Second
	defaultHeartbeat = 10 * time.Second
)

// LeaseManager is the fenced singleton lease the scheduler uses under HA. It is
// satisfied by *store.Coordination; the scheduler depends on the interface so
// the app layer never needs a datastore-typed field.
type LeaseManager interface {
	// Now is the datastore clock. All lease-time comparisons read time from
	// here so every node shares one clock, removing per-node skew as a
	// split-brain vector.
	Now(ctx context.Context) (time.Time, error)
	// A nonzero fence returned alongside an error is provisional: it may
	// only be confirmed by RenewLease or cleaned up by exact ReleaseLease.
	ClaimLease(ctx context.Context, name, owner string, now, expires time.Time) (fence int64, held bool, err error)
	RenewLease(ctx context.Context, name, owner string, fence int64, now, expires time.Time) (held bool, err error)
	ReleaseLease(ctx context.Context, name, owner string, fence int64) error
}

// ScheduledJob is one bounded background operation. LastSuccess reads the
// job's persisted health marker; nil means the job has no staleness contract.
type ScheduledJob struct {
	Name        string
	Run         func(context.Context) error
	LastSuccess func(context.Context) (time.Time, bool, error)
	// OnRefused, when set, is called with the runtime-configuration gate's
	// refusal instead of Run, and returns the job's error.
	OnRefused func(context.Context, error) error
}

// Scheduler runs every registered job once on startup and then hourly. Jobs
// share no transaction or deadline, so one failure cannot roll back another or
// silently disable future ticks.
type Scheduler struct {
	Jobs     []ScheduledJob
	Interval time.Duration
	Deadline time.Duration
	Log      *slog.Logger
	Now      func() time.Time

	// Lease, NodeID, LeaseTTL, and Heartbeat drive HA leadership. When Lease
	// is nil the scheduler is single-node and always runs its jobs (v1
	// behaviour, unchanged). When set, jobs run only while this node holds the
	// scheduler lease, so every singleton executes at most once across the
	// cluster and a stale leader that loses the lease has its in-flight jobs
	// cancelled. Its term identity also travels in the job context: tx.Write
	// locks and validates that fence before mutation, so a paused leader cannot
	// commit after takeover even before cancellation reaches it.
	Lease     LeaseManager
	NodeID    string
	LeaseTTL  time.Duration
	Heartbeat time.Duration

	// OnTick runs on every heartbeat on every node, leader or not. HA uses it
	// to refresh this node's registry heartbeat (so nodes_seen stays accurate
	// on standbys) and to sweep expired admission windows. Nil is a no-op.
	OnTick func(context.Context)

	leader atomic.Bool
}

func (s *Scheduler) interval() time.Duration {
	if s.Interval <= 0 {
		return defaultSchedulerInterval
	}
	return s.Interval
}

func (s *Scheduler) deadline() time.Duration {
	if s.Deadline <= 0 {
		return defaultJobDeadline
	}
	return s.Deadline
}

func (s *Scheduler) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

func (s *Scheduler) logger() *slog.Logger {
	if s.Log == nil {
		return slog.Default()
	}
	return s.Log
}

func (s *Scheduler) leaseTTL() time.Duration {
	if s.LeaseTTL <= 0 {
		return defaultLeaseTTL
	}
	return s.LeaseTTL
}

func (s *Scheduler) heartbeat() time.Duration {
	if s.Heartbeat <= 0 {
		return defaultHeartbeat
	}
	return s.Heartbeat
}

// IsLeader reports whether this node currently holds the scheduler lease. A
// single-node scheduler is always the leader. Health and /metrics read it for
// the hikyo_ha_is_leader gauge.
func (s *Scheduler) IsLeader() bool {
	if s.Lease == nil {
		return true
	}
	return s.leader.Load()
}

// Run blocks until cancellation. Without a lease it is the single-node loop:
// a startup catch-up run then the interval tick. With a lease it runs the HA
// leadership loop instead.
func (s *Scheduler) Run(ctx context.Context) {
	if s.Lease == nil {
		s.leader.Store(true)
		s.runLoop(ctx)
		return
	}
	s.runHA(ctx)
}

// runLoop is the single-node schedule: startup catch-up then interval ticks.
// Under HA it is the body of one leadership term, cancelled when the lease is
// lost.
func (s *Scheduler) runLoop(ctx context.Context) {
	s.runOnce(ctx, "startup")
	ticker := time.NewTicker(s.interval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx, "hourly")
		}
	}
}

// runHA competes for the scheduler lease and runs a leadership term only while
// this node holds it. The lease is claimed when unheld or expired and renewed
// on every heartbeat; a renewal that fails (another node took over, or the
// datastore is unreachable) drops leadership and cancels the running term, so
// a stale leader cannot keep executing singleton work.
func (s *Scheduler) runHA(ctx context.Context) {
	ticker := time.NewTicker(s.heartbeat())
	defer ticker.Stop()

	var fence int64
	// A claim can commit while its response times out. Until a fenced renewal
	// confirms that provisional token, no term has started and no jobs may run.
	var unconfirmedClaim bool
	// expiresAt is the deadline of the last SUCCESSFULLY acquired or renewed
	// lease. A leader that reaches this instant without a fresh renewal has
	// lost coordination (datastore loss, partition, a blocked query) and must
	// stop, even if no renew has yet returned an error: the DB lease is about
	// to be claimable by another node, so continuing would be split brain.
	var expiresAt time.Time
	termCancel := func() {}
	// termDone tracks the CURRENT or still-DRAINING leadership term. It is only
	// cleared once the term goroutine has actually exited, so a new term is
	// never started while the previous one is still winding down (no overlap).
	var termDone chan struct{}
	defer func() { termCancel() }()
	dropLeadership := func() {
		termCancel()
		termCancel = func() {}
		if s.leader.Swap(false) {
			s.logger().Warn("scheduler lost leadership", "node", s.NodeID)
		}
	}
	// termDraining reports whether a prior term goroutine has not yet exited,
	// clearing termDone the moment it has. A new lease claim is refused while a
	// term is draining.
	termDraining := func() bool {
		if termDone == nil {
			return false
		}
		select {
		case <-termDone:
			termDone = nil
			return false
		default:
			return true
		}
	}

	// A serialization retry reuses the original now/expires, so a success can
	// land after the lease already lapsed. Such a result confers no authority.
	// Judge it by the datastore clock, never this node's local one.
	fresh := func(expires time.Time) bool {
		nowCtx, cancel := context.WithTimeout(ctx, s.heartbeat())
		now, err := s.Lease.Now(nowCtx)
		cancel()
		if err != nil {
			s.logger().Error("scheduler datastore clock unreachable; failing closed", "node", s.NodeID, "err", err)
			return false
		}
		if now.Before(expires) {
			return true
		}
		s.logger().Error("scheduler lease lapsed before its grant was observed; failing closed", "node", s.NodeID)
		return false
	}

	startTerm := func(expires time.Time) {
		unconfirmedClaim = false
		expiresAt = expires
		s.leader.Store(true)
		s.logger().Info("scheduler acquired leadership", "node", s.NodeID, "fence", fence)
		termCtx, cancel := context.WithCancel(store.WithSingletonLease(ctx, schedulerLeaseName, s.NodeID, fence))
		termCancel = cancel
		termDone = make(chan struct{})
		done := termDone
		go func() { defer close(done); s.runLoop(termCtx) }()
	}

	for {
		select {
		case <-ctx.Done():
			// Cancel the running term and WAIT for it to finish before releasing
			// the lease, so we never hand the lease to a standby while our own
			// singleton job is still committing. If the term will not stop, do
			// NOT release: let the lease expire so a standby only takes over
			// after the TTL, never while our job is still running.
			done := termDone
			dropLeadership()
			joined := done == nil
			if done != nil {
				select {
				case <-done:
					joined = true
				case <-time.After(2 * time.Second):
					s.logger().Warn("scheduler term did not stop; leaving lease to expire rather than releasing", "node", s.NodeID)
				}
			}
			if fence != 0 && joined {
				releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				if err := s.Lease.ReleaseLease(releaseCtx, schedulerLeaseName, s.NodeID, fence); err != nil {
					s.logger().Warn("scheduler lease release failed", "node", s.NodeID, "err", err)
				}
				cancel()
			}
			return
		case <-ticker.C:
			// Read time from the datastore so every node compares against one
			// clock. If it is unreachable, a leader has lost coordination and
			// must fail closed.
			nowCtx, cancelNow := context.WithTimeout(ctx, s.heartbeat())
			now, err := s.Lease.Now(nowCtx)
			cancelNow()
			if err != nil {
				s.logger().Error("scheduler datastore clock unreachable", "node", s.NodeID, "err", err)
				if s.leader.Load() {
					dropLeadership()
				}
				continue
			}
			// Fail-closed FIRST: a leader past its last good lease deadline has
			// lost coordination; drop before attempting anything, so a paused
			// holder cannot revive an expired term.
			if s.leader.Load() && !now.Before(expiresAt) {
				s.logger().Error("scheduler lease expired without renewal; dropping leadership", "node", s.NodeID)
				dropLeadership()
			}
			expires := now.Add(s.leaseTTL())
			if s.leader.Load() {
				renewCtx, cancel := context.WithTimeout(ctx, s.heartbeat())
				held, err := s.Lease.RenewLease(renewCtx, schedulerLeaseName, s.NodeID, fence, now, expires)
				cancel()
				switch {
				case err != nil:
					s.logger().Error("scheduler lease renew failed; dropping leadership", "node", s.NodeID, "err", err)
					dropLeadership()
				case !held:
					dropLeadership()
				case !fresh(expires):
					// The row may still be live under our fence (clock read failed):
					// reconfirm through the provisional renewal instead of idling a TTL.
					dropLeadership()
					unconfirmedClaim = true
				default:
					expiresAt = expires
				}
			}
			// Shutdown owns release and draining. Never start another acquisition
			// after a canceled clock read or renewal returned to this heartbeat.
			if ctx.Err() != nil {
				continue
			}
			// Do not claim while the previous term is still draining: a new
			// runLoop must never overlap the cancelled one.
			if !s.leader.Load() && !termDraining() {
				if unconfirmedClaim {
					renewCtx, cancel := context.WithTimeout(ctx, s.heartbeat())
					held, err := s.Lease.RenewLease(renewCtx, schedulerLeaseName, s.NodeID, fence, now, expires)
					cancel()
					switch {
					case err != nil:
						s.logger().Error("scheduler provisional claim confirmation failed", "node", s.NodeID, "err", err)
					case !held:
						// The transaction rolled back, expired, or a different owner/fence
						// took over. The provisional token confers no authority over it.
						unconfirmedClaim = false
						fence = 0
					case ctx.Err() == nil && fresh(expires):
						startTerm(expires)
					}
				}
				if !s.leader.Load() && !unconfirmedClaim && ctx.Err() == nil {
					claimCtx, cancel := context.WithTimeout(ctx, s.heartbeat())
					gotFence, held, err := s.Lease.ClaimLease(claimCtx, schedulerLeaseName, s.NodeID, now, expires)
					cancel()
					if gotFence != 0 && (held || err != nil) {
						fence = gotFence
						unconfirmedClaim = true
					}
					switch {
					case err != nil:
						s.logger().Error("scheduler lease claim failed", "node", s.NodeID, "err", err)
					case held && ctx.Err() == nil && fresh(expires):
						startTerm(expires)
					}
				}
			}
			if s.OnTick != nil && ctx.Err() == nil {
				tickCtx, cancel := context.WithTimeout(ctx, s.heartbeat())
				s.OnTick(tickCtx)
				cancel()
			}
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context, trigger string) {
	for _, job := range s.Jobs {
		if ctx.Err() != nil {
			return
		}
		jobCtx, cancel := context.WithTimeout(ctx, s.deadline())
		err := job.Run(jobCtx)
		cancel()
		if err != nil {
			s.logger().Error("scheduler job failed", "job", job.Name, "trigger", trigger, "err", err)
		}
		s.checkHealth(ctx, job)
	}
}

func (s *Scheduler) checkHealth(ctx context.Context, job ScheduledJob) {
	if job.LastSuccess == nil {
		return
	}
	at, ok, err := job.LastSuccess(ctx)
	if err != nil {
		s.logger().Error("scheduler job health check failed", "job", job.Name, "err", err)
		return
	}
	if !ok {
		s.logger().Warn("last_prune_success has never been recorded", "job", job.Name)
		return
	}
	age := s.now().Sub(at)
	// This log is the operator narrative beside the same persisted timestamp
	// exposed by doctor, the instance health API, and /metrics.
	s.logger().Info("scheduler job health", "job", job.Name, "last_prune_success", at, "age", age)
	if age > pruneStaleAfter {
		s.logger().Warn("last_prune_success is stale", "job", job.Name, "last_prune_success", at, "age", age)
	}
}

// defaultCaptureGrace bounds how long a gated job waits for a replacement
// generation to publish its runtime bundle.
const defaultCaptureGrace = 3 * time.Second

// A replacement generation starts its scheduler (startup catch-up) inside
// installer activation, just before SelfConfig publishes the active bundle.
// Wait up to grace (polling every grace/30) while capture reports that window:
// not ready, or fenced only because publication is pending. A lasting fence
// is final and never waits. grace <= 0 means a single attempt.
func captureWithGrace(ctx context.Context, grace time.Duration, capture func(context.Context) error) error {
	timer := time.NewTimer(grace)
	defer timer.Stop()
	step := grace / 30
	for {
		err := capture(ctx)
		transient := errors.Is(err, service.ErrSelfConfigUnavailable) &&
			(!errors.Is(err, service.ErrSelfConfigFenced) || errors.Is(err, service.ErrSelfConfigPublicationPending))
		if !transient || grace <= 0 {
			return err
		}
		wait := time.NewTimer(step)
		select {
		case <-ctx.Done():
			wait.Stop()
			return err
		case <-timer.C:
			wait.Stop()
			return err
		case <-wait.C:
		}
	}
}

// gateOnRuntimeConfig runs capture before every job run. A refused run never
// reaches job.Run; a job with OnRefused records its own failure there so the
// refusal is durable, not just a scheduler log line.
func gateOnRuntimeConfig(job ScheduledJob, grace time.Duration, capture func(context.Context) error) ScheduledJob {
	run, refused := job.Run, job.OnRefused
	job.Run = func(ctx context.Context) error {
		if err := captureWithGrace(ctx, grace, capture); err != nil {
			if refused != nil {
				return refused(ctx, err)
			}
			return err
		}
		return run(ctx)
	}
	return job
}
