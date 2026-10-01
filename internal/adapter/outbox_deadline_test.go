package adapter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"
)

type deadlineProbeModule struct {
	workerModule
	t *testing.T
}

type attemptModule struct {
	workerModule
	sync       func(context.Context, SyncRequest, Journal) (SyncResult, error)
	connection func(context.Context, ConnectionRequest) (Connection, error)
}

func (m attemptModule) TestConnection(ctx context.Context, request ConnectionRequest) (Connection, error) {
	if m.connection != nil {
		return m.connection(ctx, request)
	}
	return m.workerModule.TestConnection(ctx, request)
}

func (m attemptModule) Sync(ctx context.Context, request SyncRequest, journal Journal) (SyncResult, error) {
	return m.sync(ctx, request, journal)
}

type attemptStore struct {
	workerJobStore
	jobs   []Job
	claims int
	cause  error
}

func (s *attemptStore) ClaimDue(ctx context.Context, _ string, _, _ time.Time) (Job, bool, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, false, err
	}
	if s.claims == len(s.jobs) {
		return Job{}, false, nil
	}
	job := s.jobs[s.claims]
	s.claims++
	return job, true, nil
}

func (s *attemptStore) Retry(ctx context.Context, job Job, due time.Time, revision int64, failed []Change, warnings []string, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.cause = cause
	return s.workerJobStore.Retry(ctx, job, due, revision, failed, warnings, cause)
}

type outcomeJournal struct {
	workerJournal
	outcomes []Completion
}

type admissionJournal struct {
	workerJournal
	gate func(context.Context) error
}

type blockedOutcomeJournal struct{ workerJournal }

type revisionJournal struct {
	workerJournal
	revision int64
}

func (j *revisionJournal) Prepare(_ context.Context, effect Effect, _ LedgerState) error {
	j.revision = effect.InputRevision
	return nil
}

type witnessedLoader struct{ module Module }

func (l witnessedLoader) Load(context.Context, Job, Journal) (LoadedSync, error) {
	return LoadedSync{Module: l.module, Revision: 7, Request: SyncRequest{Completed: []Change{{Surface: Secret, EffectiveName: "ACKNOWLEDGED"}}}}, nil
}

func TestWorkerBindsInputRevisionAndPreservesDurableCompletedNames(t *testing.T) {
	journal := &revisionJournal{}
	store := &workerJobStore{job: Job{ID: "revision", Kind: Converge, Attempt: 1}, journal: journal}
	module := attemptModule{sync: func(ctx context.Context, request SyncRequest, journal Journal) (SyncResult, error) {
		if len(request.Completed) != 1 || request.Completed[0].EffectiveName != "ACKNOWLEDGED" {
			t.Fatal("worker discarded the loaded durable acknowledgement")
		}
		return SyncResult{}, journal.Prepare(ctx, Effect{Surface: Secret, EffectiveName: "NEXT", Disposition: Create, InputRevision: 999}, Reserved)
	}}
	worker := Worker{Store: store, Loader: witnessedLoader{module: module}, ID: "worker"}
	if worked, err := worker.RunOnce(t.Context()); !worked || err != nil || journal.revision != 7 || !store.succeeded {
		t.Fatalf("trusted revision binding: worked=%v err=%v revision=%d succeeded=%v", worked, err, journal.revision, store.succeeded)
	}
}

type changingRevisionLoader struct {
	module Module
	loads  int64
}

func (l *changingRevisionLoader) Load(context.Context, Job, Journal) (LoadedSync, error) {
	l.loads++
	return LoadedSync{Module: l.module, Revision: l.loads, Request: SyncRequest{}}, nil
}

func TestWorkerRateWaitNeverCarriesAcknowledgementsIntoNewRevision(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &workerJobStore{job: Job{ID: "changing_source", Kind: Converge, Attempt: 1}, journal: &workerJournal{}}
		calls := 0
		module := attemptModule{sync: func(_ context.Context, request SyncRequest, _ Journal) (SyncResult, error) {
			calls++
			if len(request.Completed) != 0 {
				t.Fatalf("revision %d skipped acknowledgement from earlier source: %v", calls, request.Completed)
			}
			if calls == 1 {
				return SyncResult{Changes: []Change{{Surface: Secret, EffectiveName: "PREFIX", Disposition: Update}}}, testRetryAtError{at: time.Now().Add(time.Second)}
			}
			return SyncResult{}, nil
		}}
		worker := Worker{Store: store, Loader: &changingRevisionLoader{module: module}, ID: "worker"}
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil || calls != 2 || !store.succeeded {
			t.Fatalf("new source reload: worked=%v err=%v calls=%d succeeded=%v", worked, err, calls, store.succeeded)
		}
	})
}

func (blockedOutcomeJournal) Finish(ctx context.Context, _ Effect, _ Completion) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestWorkerBlockedOutcomeSettlementCannotExceedGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &attemptStore{workerJobStore: workerJobStore{journal: blockedOutcomeJournal{}}, jobs: []Job{
			{ID: "blocked", OrgID: "tenant_a", Kind: Converge, Attempt: 1},
			{ID: "next", OrgID: "tenant_b", Kind: Converge, Attempt: 1},
		}}
		module := attemptModule{sync: func(ctx context.Context, request SyncRequest, journal Journal) (SyncResult, error) {
			if request.JobID == "next" {
				return SyncResult{}, nil
			}
			<-ctx.Done()
			return SyncResult{}, journal.Finish(ctx, Effect{}, Completion{Outcome: OutcomeUnknown, State: Dispatched})
		}}
		worker := Worker{Store: store, Loader: workerLoader{module: module}, ID: "worker"}
		started := time.Now()
		if worked, err := worker.RunOnce(t.Context()); !worked || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked settlement = %v, %v", worked, err)
		}
		if elapsed := time.Since(started); elapsed != 2*time.Minute+5*time.Second || store.retried || store.succeeded {
			t.Fatalf("blocked outcome exceeded grace: elapsed=%s retried=%v succeeded=%v", elapsed, store.retried, store.succeeded)
		}
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil || !store.succeeded {
			t.Fatalf("next tenant after blocked settlement = %v, %v", worked, err)
		}
	})
}

type blockedRetryStore struct{ attemptStore }

func (*blockedRetryStore) Retry(ctx context.Context, _ Job, _ time.Time, _ int64, _ []Change, _ []string, _ error) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestWorkerBlockedRetrySettlementCannotExceedAttemptAndGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &blockedRetryStore{attemptStore: attemptStore{jobs: []Job{{ID: "retry", Kind: Converge, Attempt: 1}}}}
		worker := Worker{Store: store, Loader: workerLoader{module: workerModule{syncErr: context.DeadlineExceeded}}, ID: "worker"}
		started := time.Now()
		if worked, err := worker.RunOnce(t.Context()); !worked || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked retry = %v, %v", worked, err)
		}
		if elapsed := time.Since(started); elapsed != 2*time.Minute+5*time.Second {
			t.Fatalf("blocked retry settlement elapsed=%s", elapsed)
		}
	})
}

func (j admissionJournal) Gate(ctx context.Context, _ Effect) error { return j.gate(ctx) }

func TestWorkerInitialGateIsBoundedAndOperatorReviewIsTerminal(t *testing.T) {
	for _, kind := range JobKinds() {
		for _, held := range []bool{false, true} {
			name := string(kind) + "/timeout"
			if held {
				name = string(kind) + "/operator-review"
			}
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					journal := admissionJournal{gate: func(ctx context.Context) error {
						if held {
							return ErrOperatorReview
						}
						<-ctx.Done()
						return ctx.Err()
					}}
					store := &attemptStore{workerJobStore: workerJobStore{journal: journal}, jobs: []Job{{ID: "gate", Kind: kind, Attempt: 1}}}
					module := attemptModule{
						sync: func(context.Context, SyncRequest, Journal) (SyncResult, error) {
							t.Fatal("Sync ran after admission refusal")
							return SyncResult{}, nil
						},
						connection: func(context.Context, ConnectionRequest) (Connection, error) {
							t.Fatal("TestConnection ran after admission refusal")
							return Connection{}, nil
						},
					}
					var loader Loader = workerLoader{module: module}
					if kind == Activate {
						loader = workerActivationLoader{module: module}
					}
					worker := Worker{Store: store, Loader: loader, ID: "worker"}
					if worked, err := worker.RunOnce(t.Context()); !worked || err != nil || store.failed != held || store.retried == held {
						t.Fatalf("admission settlement: worked=%v err=%v failed=%v retried=%v held=%v", worked, err, store.failed, store.retried, held)
					}
				})
			})
		}
	}
}

func (j *outcomeJournal) Gate(ctx context.Context, _ Effect) error { return ctx.Err() }
func (j *outcomeJournal) Finish(ctx context.Context, _ Effect, outcome Completion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	j.outcomes = append(j.outcomes, outcome)
	return nil
}

func TestWorkerTimeoutSettlesPartialCustodyAndYieldsToNextTenant(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		journal := &outcomeJournal{}
		store := &attemptStore{workerJobStore: workerJobStore{journal: journal}, jobs: []Job{
			{ID: "blocked", OrgID: "tenant_a", Kind: Converge, Attempt: 1},
			{ID: "next", OrgID: "tenant_b", Kind: Converge, Attempt: 1},
		}}
		module := attemptModule{sync: func(ctx context.Context, request SyncRequest, journal Journal) (SyncResult, error) {
			if request.JobID == "next" {
				return SyncResult{}, nil
			}
			known := Effect{Surface: Secret, EffectiveName: "APPLIED", Disposition: Create}
			if err := journal.Finish(ctx, known, Completion{Outcome: OutcomeSuccess, State: Owned}); err != nil {
				t.Fatal(err)
			}
			<-ctx.Done()
			// After the synchronous request returned with a timeout, retain its
			// unknown dispatch instead of pretending it succeeded or removing it.
			unknown := Effect{Surface: Secret, EffectiveName: "DISPATCHED", Disposition: Create}
			if err := journal.Finish(ctx, unknown, Completion{Outcome: OutcomeUnknown, State: Dispatched}); err != nil {
				t.Fatalf("durable unknown outcome after timeout: %v", err)
			}
			if err := journal.Gate(ctx, unknown); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("post-timeout dispatch gate = %v", err)
			}
			return SyncResult{Changes: []Change{{Surface: Secret, EffectiveName: "APPLIED"}}, Failed: []Change{{Surface: Secret, EffectiveName: "DISPATCHED"}}}, ctx.Err()
		}}
		loader := &cursorLoader{module: module}
		worker := Worker{Store: store, Loader: loader, ID: "worker", Jitter: func(d time.Duration) time.Duration { return d }}
		started := time.Now()
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
			t.Fatalf("blocked attempt = %v, %v", worked, err)
		}
		if elapsed := time.Since(started); elapsed != 2*time.Minute {
			t.Fatalf("execution elapsed = %s; want exactly 2 minutes", elapsed)
		}
		if !store.retried || store.failed || store.succeeded || !errors.Is(store.cause, context.DeadlineExceeded) || len(store.retryFailures) != 1 || len(journal.outcomes) != 2 || journal.outcomes[1].State != Dispatched || journal.outcomes[1].Outcome != OutcomeUnknown || loader.releases != 1 {
			t.Fatalf("timeout settlement: store=%+v outcomes=%+v releases=%d", store, journal.outcomes, loader.releases)
		}
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil || !store.succeeded || store.claims != 2 {
			t.Fatalf("next tenant progress = %v, %v; claims=%d succeeded=%v", worked, err, store.claims, store.succeeded)
		}
	})
}

func TestWorkerNeverAbandonsModuleAtAttemptDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		providerReturned := make(chan struct{})
		workerDone := make(chan struct{})
		store := &attemptStore{jobs: []Job{{ID: "blocked", Kind: Converge, Attempt: 1}}}
		module := attemptModule{sync: func(ctx context.Context, _ SyncRequest, _ Journal) (SyncResult, error) {
			<-ctx.Done()
			<-providerReturned
			return SyncResult{}, ctx.Err()
		}}
		loader := &cursorLoader{module: module}
		worker := Worker{Store: store, Loader: loader, ID: "worker"}
		go func() {
			defer close(workerDone)
			if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
				t.Errorf("RunOnce() = %v, %v", worked, err)
			}
		}()
		time.Sleep(2*time.Minute + time.Second)
		synctest.Wait()
		if loader.releases != 0 || store.retried || store.failed || store.succeeded {
			t.Fatal("worker abandoned an active module or settled its lease")
		}
		close(providerReturned)
		<-workerDone
		if loader.releases != 1 || !store.retried {
			t.Fatal("worker did not release and retry after synchronous module return")
		}
	})
}

func TestWorkerShutdownDoesNotRecoverCanceledOutcomeContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		parent, cancel := context.WithCancel(t.Context())
		journal := &outcomeJournal{}
		store := &attemptStore{workerJobStore: workerJobStore{journal: journal}, jobs: []Job{{ID: "shutdown", Kind: Converge, Attempt: 1}}}
		module := attemptModule{sync: func(ctx context.Context, _ SyncRequest, journal Journal) (SyncResult, error) {
			<-ctx.Done()
			cancel()
			if err := journal.Finish(ctx, Effect{}, Completion{Outcome: OutcomeUnknown, State: Dispatched}); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("shutdown Finish = %v", err)
			}
			return SyncResult{}, ctx.Err()
		}}
		worker := Worker{Store: store, Loader: workerLoader{module: module}, ID: "worker"}
		if _, err := worker.RunOnce(parent); !errors.Is(err, context.Canceled) || len(journal.outcomes) != 0 || store.retried {
			t.Fatalf("shutdown settlement = %v; outcomes=%+v retried=%v", err, journal.outcomes, store.retried)
		}
	})
}

func TestWorkerTransientFailuresRetryBeyondEightAttemptsAndOneHour(t *testing.T) {
	store := &workerJobStore{job: Job{ID: "indefinite", Kind: Converge, CreatedAt: time.Now().Add(-2 * time.Hour)}}
	worker := Worker{Store: store, Loader: workerLoader{module: workerModule{syncErr: context.DeadlineExceeded}}, ID: "worker", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for attempt := 1; attempt <= 20; attempt++ {
		store.job.Attempt = attempt
		store.retried = false
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil || !store.retried || store.failed || store.succeeded {
			t.Fatalf("attempt %d: worked=%v err=%v retried=%v failed=%v succeeded=%v", attempt, worked, err, store.retried, store.failed, store.succeeded)
		}
	}
}

func TestWorkerActivationTimeoutRecordsRetryOnLiveParent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &attemptStore{jobs: []Job{{ID: "activation", Kind: Activate, Attempt: 1}}}
		module := attemptModule{connection: func(ctx context.Context, _ ConnectionRequest) (Connection, error) {
			<-ctx.Done()
			return Connection{}, ctx.Err()
		}}
		worker := Worker{Store: store, Loader: workerActivationLoader{module: module}, ID: "worker"}
		if worked, err := worker.RunOnce(t.Context()); !worked || err != nil || !store.retried || store.failed || !errors.Is(store.cause, context.DeadlineExceeded) {
			t.Fatalf("activation timeout: worked=%v err=%v retried=%v failed=%v cause=%v", worked, err, store.retried, store.failed, store.cause)
		}
	})
}

func TestWorkerOwnDeadlineDoesNotReviveSeparatelyCanceledModuleContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		journal := &outcomeJournal{}
		store := &attemptStore{workerJobStore: workerJobStore{journal: journal}, jobs: []Job{{ID: "canceled", Kind: Converge, Attempt: 1}}}
		module := attemptModule{sync: func(ctx context.Context, _ SyncRequest, journal Journal) (SyncResult, error) {
			moduleCtx, cancel := context.WithCancel(ctx)
			cancel()
			<-ctx.Done()
			if err := journal.Finish(moduleCtx, Effect{}, Completion{Outcome: OutcomeUnknown, State: Dispatched}); !errors.Is(err, context.Canceled) {
				t.Fatalf("separately canceled Finish = %v", err)
			}
			return SyncResult{}, ctx.Err()
		}}
		worker := Worker{Store: store, Loader: workerLoader{module: module}, ID: "worker"}
		if _, err := worker.RunOnce(t.Context()); err != nil || len(journal.outcomes) != 0 || !store.retried {
			t.Fatalf("canceled module settlement = %v; outcomes=%+v retried=%v", err, journal.outcomes, store.retried)
		}
	})
}

func (m deadlineProbeModule) check(ctx context.Context) {
	m.t.Helper()
	deadline, ok := ctx.Deadline()
	if remaining := time.Until(deadline); !ok || remaining <= 0 || remaining > 2*time.Minute {
		m.t.Fatalf("attempt deadline remaining = %s, present=%v; want at most 2 minutes", remaining, ok)
	}
}

func (m deadlineProbeModule) Sync(ctx context.Context, _ SyncRequest, _ Journal) (SyncResult, error) {
	m.check(ctx)
	return SyncResult{}, nil
}

func (m deadlineProbeModule) TestConnection(ctx context.Context, _ ConnectionRequest) (Connection, error) {
	m.check(ctx)
	return Connection{}, nil
}

func TestWorkerEveryJobKindHasTwoMinuteAttemptDeadline(t *testing.T) {
	for _, kind := range JobKinds() {
		t.Run(string(kind), func(t *testing.T) {
			module := deadlineProbeModule{t: t}
			store := &workerJobStore{job: Job{ID: "job_deadline", Kind: kind, Attempt: 1}}
			var loader Loader = workerLoader{module: module}
			if kind == Activate {
				loader = workerActivationLoader{module: module}
			}
			worker := Worker{Store: store, Loader: loader, ID: "deadline_worker"}
			if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
				t.Fatalf("RunOnce() = %v, %v", worked, err)
			}
		})
	}
}
