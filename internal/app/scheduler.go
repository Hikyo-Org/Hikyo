package app

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
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
	// cancelled.
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
	// expiresAt is the deadline of the last SUCCESSFULLY acquired or renewed
	// lease. A leader that reaches this instant without a fresh renewal has
	// lost coordination (datastore loss, partition, a blocked query) and must
	// stop, even if no renew has yet returned an error: the DB lease is about
	// to be claimable by another node, so continuing would be split brain.
	var expiresAt time.Time
	termCancel := func() {}
	var termDone chan struct{}
	defer func() { termCancel() }()
	dropLeadership := func() {
		termCancel()
		termCancel = func() {}
		termDone = nil
		if s.leader.Swap(false) {
			s.logger().Warn("scheduler lost leadership", "node", s.NodeID)
		}
	}

	for {
		select {
		case <-ctx.Done():
			// Cancel the running term and WAIT for it to finish before releasing
			// the lease, so we never hand the lease to a standby while our own
			// singleton job is still committing.
			done := termDone
			dropLeadership()
			if done != nil {
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					s.logger().Warn("scheduler term did not stop before release", "node", s.NodeID)
				}
			}
			if fence != 0 {
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
				default:
					expiresAt = expires
				}
			}
			if !s.leader.Load() {
				claimCtx, cancel := context.WithTimeout(ctx, s.heartbeat())
				gotFence, held, err := s.Lease.ClaimLease(claimCtx, schedulerLeaseName, s.NodeID, now, expires)
				cancel()
				switch {
				case err != nil:
					s.logger().Error("scheduler lease claim failed", "node", s.NodeID, "err", err)
				case held:
					fence = gotFence
					expiresAt = expires
					s.leader.Store(true)
					s.logger().Info("scheduler acquired leadership", "node", s.NodeID, "fence", fence)
					termCtx, cancel := context.WithCancel(ctx)
					termCancel = cancel
					termDone = make(chan struct{})
					done := termDone
					go func() { defer close(done); s.runLoop(termCtx) }()
				}
			}
			if s.OnTick != nil {
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
