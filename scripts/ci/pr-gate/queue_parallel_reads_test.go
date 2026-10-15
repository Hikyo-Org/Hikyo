package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type delayedReadAPI struct {
	api
	delay   time.Duration
	started chan string
	release <-chan struct{}
	active  atomic.Int32
	maximum atomic.Int32
	count   atomic.Int32
}

func (a *delayedReadAPI) read(ctx context.Context, path string, out any) error {
	a.count.Add(1)
	active := a.active.Add(1)
	defer a.active.Add(-1)
	for old := a.maximum.Load(); active > old && !a.maximum.CompareAndSwap(old, active); old = a.maximum.Load() {
	}
	if a.started != nil {
		a.started <- path
	}
	if a.release != nil {
		select {
		case <-a.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if a.delay > 0 {
		timer := time.NewTimer(a.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return a.api.read(ctx, path, out)
}

func TestQueueIndependentAuthorityReadsOverlapWithinEachPhase(t *testing.T) {
	for _, phase := range []string{"policy", "roles"} {
		t.Run(phase, func(t *testing.T) {
			f := newQueueFixture(t)
			release := make(chan struct{})
			a := &delayedReadAPI{api: f.api, started: make(chan string, 2), release: release}
			f.c.api = a
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if phase == "policy" {
					_, err := f.q.policy(ctx)
					done <- err
				} else {
					_, err := f.q.roles(ctx, f.pr.Head.SHA)
					done <- err
				}
			}()
			paths := map[string]bool{}
			for range 2 {
				select {
				case path := <-a.started:
					paths[path] = true
				case <-ctx.Done():
					<-done
					t.Fatal("second independent read could not start until the first finished")
				}
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if len(paths) != 2 || a.maximum.Load() != 2 {
				t.Fatalf("unexpected read overlap: paths=%v max=%d", paths, a.maximum.Load())
			}
		})
	}
}

func TestQueueAdmissionInjectedReadLatency(t *testing.T) {
	f := newQueueFixture(t)
	a := &delayedReadAPI{api: f.api, delay: 30 * time.Millisecond}
	f.c.api = a
	started := time.Now()
	if err := directSnapshotAdmission(f); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if newestQueueCheck(f.api.checks).Conclusion != "success" {
		t.Fatal("latency replay did not establish full admission")
	}
	t.Logf("admission with 30ms injected per read: elapsed=%s reads=%d max_in_flight=%d", elapsed, a.count.Load(), a.maximum.Load())
}

func TestQueuePolicyRequiresBothAuthorityResponses(t *testing.T) {
	// Each policy endpoint must fail closed even when the other response is valid.
	for _, endpoint := range []string{"rulesets/20539346", "rules/branches/main"} {
		t.Run(endpoint, func(t *testing.T) {
			f := newQueueFixture(t)
			failure := errors.New("deliberate authority read failure")
			f.api.responses[f.c.endpoint(endpoint)] = failure
			if _, err := f.q.policy(context.Background()); !errors.Is(err, failure) {
				t.Fatalf("authority failure hidden: %v", err)
			}
			f.assertNoSuccess(t)
		})
	}
}

type failingPairedReadAPI struct {
	api
	failingPath string
	failure     error
	started     atomic.Int32
	active      atomic.Int32
	cancelled   atomic.Int32
	ready       chan struct{}
}

func (a *failingPairedReadAPI) read(ctx context.Context, path string, _ any) error {
	a.active.Add(1)
	defer a.active.Add(-1)
	if a.started.Add(1) == 2 {
		close(a.ready)
	}
	select {
	case <-a.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	if path == a.failingPath {
		return a.failure
	}
	<-ctx.Done()
	a.cancelled.Add(1)
	return ctx.Err()
}

func TestQueueAuthorityFailureCancelsOtherReadBeforePhaseReturns(t *testing.T) {
	for _, endpoint := range []string{"rulesets/20539346", "rules/branches/main"} {
		t.Run(endpoint, func(t *testing.T) {
			f := newQueueFixture(t)
			failure := errors.New("deliberate authority read failure")
			a := &failingPairedReadAPI{api: f.api, failingPath: f.c.endpoint(endpoint), failure: failure, ready: make(chan struct{})}
			f.c.api = a
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := f.q.policy(ctx)
			if !errors.Is(err, failure) || ctx.Err() != nil || a.cancelled.Load() != 1 || a.active.Load() != 0 {
				t.Fatalf("failed read did not cancel and join sibling: err=%v parent=%v cancelled=%d active=%d", err, ctx.Err(), a.cancelled.Load(), a.active.Load())
			}
		})
	}
}

func TestQueueAuthorityCallerCancellationJoinsBothReads(t *testing.T) {
	f := newQueueFixture(t)
	a := &delayedReadAPI{api: f.api, started: make(chan string, 2), release: make(chan struct{})}
	f.c.api = a
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := f.q.policy(ctx); done <- err }()
	for range 2 {
		select {
		case <-a.started:
		case <-ctx.Done():
			t.Fatal("both reads did not start")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || a.active.Load() != 0 {
		t.Fatalf("caller cancellation left a reader running: err=%v active=%d", err, a.active.Load())
	}
}

type orderedFinalRolesAPI struct {
	api
	inventory, queue, first    string
	inventoryReads, queueReads atomic.Int32
	firstDone                  chan struct{}
	completed                  chan string
}

func (a *orderedFinalRolesAPI) read(ctx context.Context, path string, out any) error {
	var count int32
	switch path {
	case a.inventory:
		count = a.inventoryReads.Add(1)
	case a.queue:
		count = a.queueReads.Add(1)
	}
	if count != 2 {
		return a.api.read(ctx, path, out)
	}
	if path != a.first {
		select {
		case <-a.firstDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := a.api.read(ctx, path, out)
	a.completed <- path
	if path == a.first {
		close(a.firstDone)
	}
	return err
}

func TestQueueFinalRoleMutationsBlockInEitherResponseOrder(t *testing.T) {
	for _, first := range []string{"inventory", "queue"} {
		for _, mutation := range []string{"none", "cross-role collision", "shared PR head", "current member actor", "missing member"} {
			t.Run(first+"/"+mutation, func(t *testing.T) {
				f := newQueueFixture(t)
				inventory := f.c.endpoint("pulls?state=open&per_page=100&page=1")
				a := &orderedFinalRolesAPI{api: f.api, inventory: inventory, queue: f.query, firstDone: make(chan struct{}), completed: make(chan string, 2)}
				a.first = inventory
				if first == "queue" {
					a.first = f.query
				}
				f.c.api = a
				injected := false
				f.api.readHook = func(path string, n int) {
					if n != 2 {
						return
					}
					if path == inventory {
						switch mutation {
						case "shared PR head":
							other := f.pr
							other.Number++
							f.api.responses[path] = []pull{f.pr, other}
							injected = true
						case "current member actor":
							changed := f.pr
							changed.User.ID++
							f.api.responses[path] = []pull{changed}
							injected = true
						case "missing member":
							f.api.responses[path] = []pull{}
							injected = true
						}
					}
					if path == f.query && mutation == "cross-role collision" {
						f.entry.Head.OID = f.pr.Head.SHA
						f.groupRefresh()
						injected = true
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				err := f.q.admit(ctx, f.pr.Number)
				if a.inventoryReads.Load() < 2 || a.queueReads.Load() < 2 || len(a.completed) != 2 {
					t.Fatalf("final role responses not both sampled: inventory=%d queue=%d err=%v", a.inventoryReads.Load(), a.queueReads.Load(), err)
				}
				if got := <-a.completed; got != a.first {
					t.Fatalf("first final role response %q, want %q", got, a.first)
				}
				if mutation == "none" {
					if err != nil || newestQueueCheck(f.api.checks).Conclusion != "success" {
						t.Fatalf("healthy ordered responses failed: %v", err)
					}
				} else {
					if !injected {
						t.Fatalf("final %s mutation was not sampled", mutation)
					}
					f.assertNoSuccess(t)
					if !blockingQueueCheck(newestQueueCheck(f.api.checks)) {
						t.Fatal("final role mutation did not retain blocking state")
					}
				}
			})
		}
	}
}
