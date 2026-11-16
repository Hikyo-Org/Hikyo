package isolation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Scheduled credential rotation executor (#148, slice S2a). The service, the
// authorizer, the store runtime, the publish path and both engines are real; only
// the external PostgreSQL provider is replaced by a recording double, and the
// clock is a fake one the tests advance.

// rotRole is one role the fake provider holds.
type rotRole struct {
	password   string
	validUntil time.Time
}

// rotCrash is the panic a test uses to model a process dying at a hook point: the
// deferred cleanups run, but no further write the worker would have made does.
type rotCrash struct{ at string }

// rotProviderCtl is the shared state behind every provider the executor opens.
// Failures are sticky until a test clears them.
type rotProviderCtl struct {
	mu    sync.Mutex
	roles map[string]rotRole
	calls []string

	createErr error
	extendErr error
	dropErr   error
	statusErr error
	// at is called at "create:before", "create:after", "drop:before", "drop:after",
	// "extend:before" and "status:before", outside the provider lock, and may panic
	// with rotCrash or run another sweep.
	at func(point, name string)
}

func (c *rotProviderCtl) point(p, name string) {
	c.mu.Lock()
	hook := c.at
	c.mu.Unlock()
	if hook != nil {
		hook(p, name)
	}
}

func (c *rotProviderCtl) record(call string) { c.calls = append(c.calls, call) }

func (c *rotProviderCtl) count(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, call := range c.calls {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

func (c *rotProviderCtl) has(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.roles[name]
	return ok
}

func (c *rotProviderCtl) names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.roles))
	for n := range c.roles {
		out = append(out, n)
	}
	return out
}

func (c *rotProviderCtl) set(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f()
}

type rotProvider struct{ ctl *rotProviderCtl }

func (p rotProvider) CreateRole(_ context.Context, r dynamic.CreateRoleRequest) error {
	p.ctl.point("create:before", r.Name)
	p.ctl.mu.Lock()
	p.ctl.record("create:" + r.Name)
	err := p.ctl.createErr
	if err == nil || errors.Is(err, dynamic.ErrAmbiguous) {
		// An ambiguous create may well have happened; the double lets it.
		p.ctl.roles[r.Name] = rotRole{password: r.Password, validUntil: r.ValidUntil}
	}
	p.ctl.mu.Unlock()
	if err == nil {
		p.ctl.point("create:after", r.Name)
	}
	return err
}

func (p rotProvider) ExtendRole(_ context.Context, name string, until time.Time) error {
	p.ctl.point("extend:before", name)
	p.ctl.mu.Lock()
	defer p.ctl.mu.Unlock()
	p.ctl.record("extend:" + name)
	if p.ctl.extendErr != nil {
		return p.ctl.extendErr
	}
	role, ok := p.ctl.roles[name]
	if !ok {
		return dynamic.ErrRefused
	}
	role.validUntil = until
	p.ctl.roles[name] = role
	return nil
}

func (p rotProvider) DropRole(_ context.Context, name string) error {
	p.ctl.point("drop:before", name)
	p.ctl.mu.Lock()
	p.ctl.record("drop:" + name)
	err := p.ctl.dropErr
	if err == nil || errors.Is(err, dynamic.ErrAmbiguous) {
		delete(p.ctl.roles, name)
	}
	p.ctl.mu.Unlock()
	if err == nil {
		p.ctl.point("drop:after", name)
	}
	return err
}

func (p rotProvider) RoleStatus(_ context.Context, name string) (dynamic.RoleStatus, error) {
	p.ctl.point("status:before", name)
	p.ctl.mu.Lock()
	defer p.ctl.mu.Unlock()
	p.ctl.record("status:" + name)
	if p.ctl.statusErr != nil {
		return dynamic.RoleStatus{}, p.ctl.statusErr
	}
	role, ok := p.ctl.roles[name]
	return dynamic.RoleStatus{Exists: ok, ValidUntil: role.validUntil}, nil
}

func (p rotProvider) Close() {}

// rotExec is a fixture with an executor on a fake clock.
type rotExec struct {
	*rotationFixture
	ctl    *rotProviderCtl
	exec   *service.RotationExecutor
	worker string

	clockMu sync.Mutex
	clock   time.Time
	// baseSnapshots is the revision count the environment started with.
	baseSnapshots int64
}

func newRotExec(t *testing.T, db *store.DB) *rotExec {
	t.Helper()
	f := newRotationFixture(t, db)
	x := &rotExec{rotationFixture: f, ctl: &rotProviderCtl{roles: map[string]rotRole{}}, clock: f.now, worker: "wrk_a"}
	kr := probeKeyring(t, db)
	auth := authService(t, db)
	now := x.now
	x.exec = &service.RotationExecutor{
		DB: db, Runtime: store.NewRotationRuntime(db), Keyring: kr, Now: now,
		Values:    &service.Values{DB: db, Keyring: kr, Auth: auth},
		Revisions: &service.Revisions{DB: db, Keyring: kr, Auth: auth, Now: now},
		ProviderFactory: func(dynamic.Kind, string, string, string) (dynamic.Provider, error) {
			return rotProvider{ctl: x.ctl}, nil
		},
		ProviderDeadline: 5 * time.Second,
	}
	x.baseSnapshots = queryInt(t, db, "SELECT COUNT(*) FROM snapshots WHERE environment_id='"+string(f.env)+"'")
	return x
}

func (x *rotExec) now() time.Time {
	x.clockMu.Lock()
	defer x.clockMu.Unlock()
	return x.clock
}

// advance moves the fake clock.
func (x *rotExec) advance(d time.Duration) {
	x.clockMu.Lock()
	defer x.clockMu.Unlock()
	x.clock = x.clock.Add(d)
}

// sweep runs one executor unit as the given worker.
func (x *rotExec) sweep(worker string) (bool, error) {
	x.t.Helper()
	return x.exec.RunRotationSweep(tctx(x.t), worker)
}

// drain sweeps until the executor reports no work, failing past a bound.
func (x *rotExec) drain() {
	x.t.Helper()
	for i := 0; i < 20; i++ {
		did, err := x.sweep(x.worker)
		if err != nil {
			x.t.Fatalf("sweep: %v", err)
		}
		if !did {
			return
		}
	}
	x.t.Fatal("executor did not settle in 20 sweeps")
}

func (x *rotExec) runPhases(rotationID string) string {
	x.t.Helper()
	return queryStrings(x.t, x.db, "SELECT phase || ',' FROM rotation_runs WHERE rotation_id='"+rotationID+"' ORDER BY started_at, id")
}

func (x *rotExec) lastRun(rotationID, column string) string {
	x.t.Helper()
	return queryString(x.t, x.db, "SELECT COALESCE(CAST("+column+" AS TEXT), '') FROM rotation_runs WHERE rotation_id='"+rotationID+"' ORDER BY started_at DESC, id DESC LIMIT 1")
}

// published reads the live (env) values of the two keys.
func (x *rotExec) deliveredUser() string {
	x.t.Helper()
	return x.lastRun(x.rotationID(), "candidate_handle")
}

func (x *rotExec) rotationID() string {
	return queryString(x.t, x.db, "SELECT id FROM rotations ORDER BY created_at DESC, id DESC LIMIT 1")
}

func TestRotationExecutorHappyPath(t *testing.T) { forEngines(t, runRotationExecutorHappyPath) }

func runRotationExecutorHappyPath(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)

	// Not due yet: nothing to do.
	if did, err := x.sweep(x.worker); err != nil || did {
		t.Fatalf("early sweep = %v, %v", did, err)
	}

	x.advance(24*time.Hour + time.Minute)
	if did, err := x.sweep(x.worker); err != nil || !did {
		t.Fatalf("first sweep = %v, %v", did, err)
	}
	// The first run mints and publishes inside one sweep and parks in overlap.
	if got := x.runPhases(rot.ID); got != "overlap," {
		t.Fatalf("phases after the first sweep = %q", got)
	}
	candidate := x.lastRun(rot.ID, "candidate_handle")
	if !x.ctl.has(candidate) {
		t.Fatalf("candidate %s was not minted: %v", candidate, x.ctl.names())
	}
	if got := x.lastRun(rot.ID, "published_revision"); got == "" || got == "0" {
		t.Fatalf("published_revision = %q", got)
	}
	// The rotation scheduled its next run one interval after the publish.
	next := mustParseStamp(t, x.state(rot.ID).NextRunAt)
	if want := x.now().Add(24 * time.Hour); next.Sub(want) > time.Second || want.Sub(next) > time.Second {
		t.Fatalf("next_run_at = %s, want about %s", next, want)
	}

	// Nothing more before the revocation delay.
	if did, err := x.sweep(x.worker); err != nil || did {
		t.Fatalf("sweep inside the overlap = %v, %v", did, err)
	}
	x.advance(time.Hour + time.Minute)
	x.drain()
	if got := x.runPhases(rot.ID); got != "done," {
		t.Fatalf("phases after the overlap = %q", got)
	}
	if !x.ctl.has(candidate) {
		t.Fatalf("the delivered candidate must outlive the run")
	}

	// A second cycle supersedes the first run's role and drops it after the delay.
	x.advance(24 * time.Hour)
	x.drain()
	x.advance(time.Hour + time.Minute)
	x.drain()
	if got := x.runPhases(rot.ID); got != "done,done," {
		t.Fatalf("phases after two cycles = %q", got)
	}
	if x.ctl.has(candidate) {
		t.Fatalf("the superseded role %s was not revoked", candidate)
	}
	second := x.lastRun(rot.ID, "candidate_handle")
	if got := x.lastRun(rot.ID, "superseded_handle"); got != candidate {
		t.Fatalf("superseded_handle = %q, want %q", got, candidate)
	}
	if !x.ctl.has(second) {
		t.Fatal("the new candidate must be live")
	}

	// Every effect has a matching INTENT then OUTCOME audit and no credential.
	if n := queryInt(t, db, "SELECT COUNT(*) FROM rotation_effects WHERE outcome IS NULL"); n != 0 {
		t.Fatalf("%d effects left open", n)
	}
	intents := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.run_intent'")
	outcomes := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.run_outcome'")
	if intents == 0 || intents != outcomes {
		t.Fatalf("intent/outcome audits = %d/%d", intents, outcomes)
	}
	payloads := queryStrings(t, db, "SELECT payload FROM audit_tenant_events WHERE type LIKE 'rotation.run_%' OR type='rotation.quarantined'")
	for _, pw := range []string{x.ctl.roles[second].password, rotAdminSecret} {
		if pw != "" && strings.Contains(payloads, pw) {
			t.Fatalf("a rotation audit payload contains a credential")
		}
	}
}

// ------------------------------------------------------------------ helpers

// crashSweep runs one sweep and reports whether a rotCrash ended it.
func (x *rotExec) crashSweep(worker string) (crashed bool) {
	x.t.Helper()
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(rotCrash); !ok {
				panic(r)
			}
			crashed = true
		}
	}()
	if _, err := x.sweep(worker); err != nil {
		x.t.Fatalf("sweep: %v", err)
	}
	return false
}

// crashOnce arms the provider to die the first time it reaches point.
func (x *rotExec) crashOnce(point string) {
	x.ctl.set(func() {
		x.ctl.at = func(p, _ string) {
			if p != point {
				return
			}
			x.ctl.set(func() { x.ctl.at = nil })
			panic(rotCrash{at: p})
		}
	})
}

// crashAfterBaseline models a worker dying inside the publish transaction.
type rotPublishProbe struct {
	onBefore   func()
	onBaseline func()
}

func (p *rotPublishProbe) BeforeProjectLock([]string) {
	if p.onBefore != nil {
		p.onBefore()
	}
}

func (p *rotPublishProbe) AfterBaselineRead([]string) {
	if p.onBaseline != nil {
		p.onBaseline()
	}
}

func (x *rotExec) effects(kind, outcome string) int64 {
	x.t.Helper()
	cond := "outcome='" + outcome + "'"
	if outcome == "open" {
		cond = "outcome IS NULL"
	}
	return queryInt(x.t, x.db, "SELECT COUNT(*) FROM rotation_effects WHERE environment_id='"+string(x.env)+"' AND kind='"+kind+"' AND "+cond)
}

func (x *rotExec) snapshots() int64 {
	x.t.Helper()
	return queryInt(x.t, x.db, "SELECT COUNT(*) FROM snapshots WHERE environment_id='"+string(x.env)+"'") - x.baseSnapshots
}

func (x *rotExec) phase(runID string) string {
	x.t.Helper()
	return queryString(x.t, x.db, "SELECT phase FROM rotation_runs WHERE id='"+runID+"'")
}

func (x *rotExec) openRunID(rotationID string) string {
	x.t.Helper()
	return queryString(x.t, x.db, "SELECT id FROM rotation_runs WHERE rotation_id='"+rotationID+"' ORDER BY started_at DESC, id DESC LIMIT 1")
}

func (x *rotExec) requireSweep(worker string, want bool) {
	x.t.Helper()
	did, err := x.sweep(worker)
	if err != nil {
		x.t.Fatalf("sweep as %s: %v", worker, err)
	}
	if did != want {
		x.t.Fatalf("sweep as %s did work = %v, want %v", worker, did, want)
	}
}

// cycle drives one full scheduled cycle: due, publish, wait out the overlap and
// settle. It returns the run id.
func (x *rotExec) cycle(rotationID string) string {
	x.t.Helper()
	next := mustParseStamp(x.t, x.state(rotationID).NextRunAt)
	if d := next.Sub(x.now()); d > 0 {
		x.advance(d + time.Minute)
	}
	x.drain()
	run := x.openRunID(rotationID)
	if got := x.phase(run); got != "overlap" {
		x.t.Fatalf("cycle run %s is %s, want overlap", run, got)
	}
	x.advance(time.Hour + time.Minute)
	x.drain()
	if got := x.phase(run); got != "done" {
		x.t.Fatalf("cycle run %s is %s, want done", run, got)
	}
	return run
}

// ------------------------------------------------------------ crash before publish

// Every crash point at or before the publish transaction rolls the candidate back
// under the fenced claim write, never publishes, and leaves the delivered value
// alone. A takeover cannot start before the dead worker's lease expires.
func TestRotationExecutorCrashBeforePublish(t *testing.T) {
	cases := []struct {
		name      string
		arm       func(x *rotExec)
		wantPhase string
		wantRole  bool
	}{
		{"before CreateRole", func(x *rotExec) { x.crashOnce("create:before") }, "minting", false},
		{"during CreateRole, before the outcome", func(x *rotExec) { x.crashOnce("create:after") }, "minting", true},
		{"inside the publish transaction", func(x *rotExec) {
			x.exec.Revisions.PublishProbe = &rotPublishProbe{onBefore: func() {
				x.exec.Revisions.PublishProbe = nil
				panic(rotCrash{at: "publish"})
			}}
		}, "minted", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				x := newRotExec(t, db)
				rot := x.create(rotPasswordKey, rotUsernameKey)
				x.advance(24*time.Hour + time.Minute)
				tc.arm(x)
				if !x.crashSweep("wrk_a") {
					t.Fatal("the worker did not reach the crash point")
				}
				run := x.openRunID(rot.ID)
				if got := x.phase(run); got != tc.wantPhase {
					t.Fatalf("phase after the crash = %s, want %s", got, tc.wantPhase)
				}
				if got := len(x.ctl.names()) > 0; got != tc.wantRole {
					t.Fatalf("candidate role present = %v, want %v", got, tc.wantRole)
				}
				// The dead worker's lease still holds the run.
				x.requireSweep("wrk_b", false)

				x.advance(rotationLeaseTermForTest + time.Second)
				x.requireSweep("wrk_b", true)
				if got := x.phase(run); got != "failed" {
					t.Fatalf("phase after the takeover = %s, want failed", got)
				}
				if names := x.ctl.names(); len(names) != 0 {
					t.Fatalf("candidate roles left behind: %v", names)
				}
				if x.snapshots() != 0 {
					t.Fatal("a rolled-back run published a revision")
				}
				if n := queryInt(t, db, "SELECT COUNT(*) FROM value_entries WHERE environment_id='"+string(x.env)+"'"); n != 0 {
					t.Fatalf("a rolled-back run left %d delivered values", n)
				}
				if x.effects("drop_candidate", "success") != 1 {
					t.Fatal("the candidate drop was not recorded as a success")
				}
				for _, kind := range []string{"mint", "publish", "drop_candidate", "revoke", "extend"} {
					if n := x.effects(kind, "open"); n != 0 {
						t.Fatalf("%d %s effects left without an outcome", n, kind)
					}
				}
				// The rotation retries after a backoff and then succeeds.
				state := x.state(rot.ID)
				if state.State != store.RotationStateActive {
					t.Fatalf("rotation state = %s", state.State)
				}
				if got := mustParseStamp(t, state.NextRunAt).Sub(x.now()); got < 4*time.Minute || got > 6*time.Minute {
					t.Fatalf("retry scheduled %s ahead, want about 5m", got)
				}
				x.advance(6 * time.Minute)
				x.drain()
				if got := x.runPhases(rot.ID); got != "failed,overlap," {
					t.Fatalf("phases after the retry = %q", got)
				}
			})
		})
	}
}

// ------------------------------------------------------------ delayed commit

// Rule 8. A publish transaction still in flight when a takeover looks at the run:
// the takeover's claim write blocks on the run row, and drops nothing until the
// transaction settles. A commit means the run carries on as published and the
// candidate is never dropped; a rollback means the run fails BEFORE the single
// candidate drop.
func TestRotationExecutorDelayedCommit(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "rollback"
		if commit {
			name = "commit"
		}
		t.Run(name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) { runRotationDelayedCommit(t, db, commit) })
		})
	}
}

func runRotationDelayedCommit(t *testing.T, db *store.DB, commit bool) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.advance(24*time.Hour + time.Minute)

	reached, release := make(chan struct{}), make(chan struct{})
	x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
		x.exec.Revisions.PublishProbe = nil
		close(reached)
		<-release
	}}
	ctxA, cancelA := context.WithCancel(tctx(t))
	defer cancelA()
	doneA := make(chan error, 1)
	go func() {
		_, err := x.exec.RunRotationSweep(ctxA, "wrk_a")
		doneA <- err
	}()
	select {
	case <-reached:
	case <-time.After(20 * time.Second):
		t.Fatal("the publish never reached its baseline read")
	}

	// A's lease lapses while its publish transaction is open.
	x.advance(rotationLeaseTermForTest + time.Second)
	// On SQLite the takeover blocks behind the open write transaction; on PostgreSQL
	// the claim skips the locked row. Either way it must drop nothing while the
	// publish is open, and it must never fail the run out from under it.
	doneB := make(chan error, 1)
	go func() {
		_, err := x.exec.RunRotationSweep(tctx(t), "wrk_b")
		doneB <- err
	}()
	select {
	case err := <-doneB:
		doneB <- err
	case <-time.After(750 * time.Millisecond):
	}
	if n := x.ctl.count("drop:"); n != 0 {
		t.Fatalf("the takeover dropped a role while the publish was open (%d drops)", n)
	}

	if !commit {
		cancelA()
	}
	close(release)
	if err := <-doneA; commit && err != nil {
		t.Fatalf("the publishing worker: %v", err)
	}
	if err := <-doneB; err != nil {
		t.Fatalf("the takeover worker: %v", err)
	}
	if commit {
		// The fenced claim write names the phases it may leave. A worker holding a
		// stale read of `minted` (and a live claim) matches zero rows once the run
		// published, so it can never roll back or drop a published candidate.
		cur, err := x.exec.Runtime.GetRun(tctx(t), domain.Scope{Org: orgA, Project: prjA1, Env: x.env}, x.openRunID(rot.ID))
		if err != nil {
			t.Fatal(err)
		}
		cur.Phase, cur.LeaseOwner = "minted", "wrk_a"
		err = x.exec.Runtime.Settle(tctx(t), cur, store.RotationSettle{
			Now: x.now().Add(-rotationLeaseTermForTest), FromPhases: []string{"minting", "minted"}, Phase: "failed", NextAttempt: x.now(),
		})
		if !errors.Is(err, store.ErrRotationFenceLost) {
			t.Fatalf("a stale claim write over a published run = %v, want a fence loss", err)
		}
	}
	// Whatever the takeover skipped, the next sweeps settle.
	x.worker = "wrk_b"
	x.drain()
	run := x.openRunID(rot.ID)
	if commit {
		if x.ctl.count("drop:") != 0 {
			t.Fatal("a published candidate was dropped")
		}
		// The dead lease cannot settle the overlap wait; `published` is the same
		// wait, claimed again when the revocation delay is up.
		if got := x.phase(run); got != "published" && got != "overlap" {
			t.Fatalf("phase after the delayed commit = %s, want published or overlap", got)
		}
		if x.snapshots() != 1 || !x.ctl.has(x.lastRun(rot.ID, "candidate_handle")) {
			t.Fatal("the published candidate must be live")
		}
		x.advance(time.Hour + time.Minute)
		x.drain()
		if got := x.phase(run); got != "done" {
			t.Fatalf("phase after the overlap = %s, want done", got)
		}
		return
	}
	if got := x.phase(run); got != "failed" {
		t.Fatalf("phase after the rolled-back publish = %s, want failed", got)
	}
	if x.ctl.count("drop:") != 1 || len(x.ctl.names()) != 0 {
		t.Fatalf("calls %v roles %v", x.ctl.calls, x.ctl.names())
	}
	if x.snapshots() != 0 {
		t.Fatal("a rolled-back publish left a revision")
	}
}

// ------------------------------------------------------------ crash after publish

// A crash while the superseded role is being dropped resumes under the same
// claim path: the stale INTENT closes unknown, a fresh INTENT drops the role (an
// idempotent call) and the run ends done with every effect settled.
func TestRotationExecutorCrashDuringRevoke(t *testing.T) {
	for _, point := range []string{"drop:before", "drop:after"} {
		t.Run(point, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				x := newRotExec(t, db)
				rot := x.create(rotPasswordKey, rotUsernameKey)
				x.advance(24*time.Hour + time.Minute)
				x.drain()
				x.advance(time.Hour + time.Minute)
				x.drain() // run 1 done
				run1Role := x.lastRun(rot.ID, "candidate_handle")

				x.advance(24 * time.Hour)
				x.drain() // run 2 minted and published, parked in overlap
				x.advance(time.Hour + time.Minute)
				x.crashOnce(point)
				if !x.crashSweep("wrk_a") {
					t.Fatal("the revoke never reached the drop")
				}
				run2 := x.openRunID(rot.ID)
				if got := x.phase(run2); got != "revoking" {
					t.Fatalf("phase after the crash = %s, want revoking", got)
				}
				if x.effects("revoke", "open") != 1 {
					t.Fatal("the dropped INTENT is not open")
				}
				x.advance(rotationLeaseTermForTest + time.Second)
				x.drain()
				if got := x.phase(run2); got != "done" {
					t.Fatalf("phase after the takeover = %s, want done", got)
				}
				if x.ctl.has(run1Role) {
					t.Fatal("the superseded role survived")
				}
				if x.effects("revoke", "success") != 1 || x.effects("revoke", "unknown") != 1 || x.effects("revoke", "open") != 0 {
					t.Fatalf("revoke effects: success %d unknown %d open %d", x.effects("revoke", "success"), x.effects("revoke", "unknown"), x.effects("revoke", "open"))
				}
			})
		})
	}
}

// rotationLeaseTermForTest mirrors the executor's two-minute claim lease.
const rotationLeaseTermForTest = 2 * time.Minute

// ------------------------------------------------------------ restore hazard

// parkAuthority downgrades every non-read grant of the rotation's authority and
// returns the function that puts them back.
func (x *rotExec) parkAuthority() func() {
	x.t.Helper()
	rows := queryStrings(x.t, x.db, `SELECT id || '=' || capability || ';' FROM grants WHERE principal_id='usr_alice' AND capability <> 'read' ORDER BY id`)
	var saved [][2]string
	for _, pair := range strings.Split(strings.TrimSuffix(rows, ";"), ";") {
		if id, capability, ok := strings.Cut(pair, "="); ok {
			saved = append(saved, [2]string{id, capability})
		}
	}
	execRaw(x.t, x.db, `UPDATE grants SET capability='read' WHERE principal_id='usr_alice' AND capability <> 'read'`)
	return func() {
		for _, g := range saved {
			execRaw(x.t, x.db, `UPDATE grants SET capability='`+g[1]+`' WHERE id='`+g[0]+`'`)
		}
	}
}

func (x *rotExec) revisions() *service.Revisions {
	kr := probeKeyring(x.t, x.db)
	return &service.Revisions{DB: x.db, Keyring: kr, Auth: authService(x.t, x.db)}
}

func (x *rotExec) runRevision(runID string) int64 {
	return queryInt(x.t, x.db, "SELECT published_revision FROM rotation_runs WHERE id='"+runID+"'")
}

// Rule 7. A revision restore puts the superseded value back: the revoke must
// refuse loudly, drop nothing, and clear itself once the keys serve a version the
// run published again.
func TestRotationExecutorRestoreHazard(t *testing.T) { forEngines(t, runRotationRestoreHazard) }

func runRotationRestoreHazard(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.cycle(rot.ID)
	role1 := x.lastRun(rot.ID, "candidate_handle")

	x.advance(24 * time.Hour)
	x.drain()
	run2 := x.openRunID(rot.ID)
	if got := x.phase(run2); got != "overlap" {
		t.Fatalf("run 2 is %s, want overlap", got)
	}
	// A person publishes their own values over both keys: the delivered versions
	// are no longer ones the run published (a restore or manual publish alike).
	alice := service.LocalPrincipal(alice)
	values := &service.Values{DB: db, Keyring: probeKeyring(t, db)}
	var staged []string
	for name, value := range map[string]string{"ROT_DB_USER": "manual_user", "ROT_DB_PASSWORD": "manual-pw-value"} {
		change, err := values.Set(tctx(t), alice, x.scope, name, value, nil)
		if err != nil {
			t.Fatalf("stage %s: %v", name, err)
		}
		staged = append(staged, change.VersionID)
	}
	if _, err := x.revisions().PublishPlanned(tctx(t), alice, x.scope, service.PublishRequest{VersionIDs: staged}); err != nil {
		t.Fatalf("manual publish: %v", err)
	}

	x.advance(time.Hour + time.Minute)
	x.drain()
	if got := x.phase(run2); got != "revoke_refused" {
		t.Fatalf("phase with another value serving = %s, want revoke_refused", got)
	}
	if got := x.lastRun(rot.ID, "last_error"); got != "published_versions_changed" {
		t.Fatalf("last_error = %q", got)
	}
	if !x.ctl.has(role1) || x.ctl.count("drop:"+role1) != 0 {
		t.Fatal("the superseded role was dropped while it could be serving")
	}
	if x.effects("revoke", "success") != 0 {
		t.Fatal("a refused revoke recorded a success")
	}
	// The hold keeps the rotation from starting another run over it.
	x.advance(10 * time.Hour)
	x.drain()
	if got := x.phase(run2); got != "revoke_refused" || x.openRunID(rot.ID) != run2 {
		t.Fatalf("hold released early: %s", got)
	}

	// Once the superseded role itself has expired, serving it again is no outage:
	// the drop is harmless and the hold lets go.
	x.advance(60 * time.Hour)
	x.drain()
	if got := x.phase(run2); got != "done" {
		t.Fatalf("phase after the superseded role expired = %s, want done", got)
	}
	if x.ctl.has(role1) {
		t.Fatal("the expired superseded role survived")
	}
}

// ------------------------------------------------------------ abandon past publish

// A run abandoned past its publish keeps its delivered credential live and still
// owes the superseded role's revoke. The obligation loop settles it after the
// delay, the candidate reconciler never touches the delivered role, and the
// environment cannot be deleted until it is settled.
func TestRotationExecutorAbandonPastPublish(t *testing.T) {
	forEngines(t, runRotationAbandonPastPublish)
}

func runRotationAbandonPastPublish(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.cycle(rot.ID)
	role1 := x.lastRun(rot.ID, "candidate_handle")
	x.advance(24 * time.Hour)
	x.drain()
	run2 := x.openRunID(rot.ID)
	role2 := x.lastRun(rot.ID, "candidate_handle")

	if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("delete over a run in flight = %v", err)
	}
	if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: true}); err != nil {
		t.Fatalf("delete --abandon: %v", err)
	}
	if got := x.phase(run2); got != "quarantine" {
		t.Fatalf("abandoned run is %s, want quarantine", got)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.quarantined' AND correlation_id='"+run2+"'"); n != 1 {
		t.Fatalf("%d quarantine audits for the abandoned run", n)
	}
	if err := envs.Delete(tctx(t), service.LocalPrincipal(alice), x.scope); !errors.Is(err, service.ErrEnvironmentHasLiveRotation) {
		t.Fatalf("environment delete with an owed revoke = %v", err)
	}

	// Before the delay nothing happens; after it the obligation loop drops the
	// superseded role and only that role.
	x.drain()
	if !x.ctl.has(role1) {
		t.Fatal("the superseded role was dropped before the overlap ended")
	}
	x.advance(time.Hour + time.Minute)
	x.drain()
	if got := x.phase(run2); got != "done" {
		t.Fatalf("abandoned run after the delay = %s, want done", got)
	}
	if x.ctl.has(role1) {
		t.Fatal("the superseded role survived the revoke obligation")
	}
	if !x.ctl.has(role2) || x.ctl.count("drop:"+role2) != 0 {
		t.Fatal("the delivered candidate must survive both loops")
	}
	if err := envs.Delete(tctx(t), service.LocalPrincipal(alice), x.scope); err != nil {
		t.Fatalf("environment delete after the obligation settled: %v", err)
	}
}

// ------------------------------------------------------------ quarantine exits

// A candidate drop with an unknown outcome quarantines the run loudly. It leaves
// quarantine as `failed` once the provider reports the role absent, or once the
// role's VALID UNTIL has passed and it can no longer authenticate.
func TestRotationExecutorQuarantineExits(t *testing.T) {
	for _, expiry := range []bool{false, true} {
		name := "role absent"
		if expiry {
			name = "role expired"
		}
		t.Run(name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) { runRotationQuarantineExit(t, db, expiry) })
		})
	}
}

func runRotationQuarantineExit(t *testing.T, db *store.DB, expiry bool) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.advance(24*time.Hour + time.Minute)
	x.crashOnce("create:after")
	if !x.crashSweep("wrk_a") {
		t.Fatal("no crash")
	}
	run := x.openRunID(rot.ID)
	// The rotation must not start a fresh run underneath the test.
	if _, err := x.transition(x.actor, "pause", rot.ID); err != nil {
		t.Fatal(err)
	}
	x.ctl.set(func() { x.ctl.dropErr = dynamic.ErrAmbiguous })
	x.advance(rotationLeaseTermForTest + time.Second)
	x.requireSweep("wrk_b", true)
	if got := x.phase(run); got != "quarantine" {
		t.Fatalf("an unknown candidate drop left the run %s, want quarantine", got)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.quarantined' AND correlation_id='"+run+"'"); n != 1 {
		t.Fatalf("%d quarantine audits", n)
	}
	if x.effects("drop_candidate", "unknown") != 1 {
		t.Fatal("the unknown drop is not recorded")
	}

	if expiry {
		// The provider stays unreachable; only the role's expiry can settle it.
		x.ctl.set(func() {
			x.ctl.dropErr, x.ctl.statusErr = dynamic.ErrUnreachable, dynamic.ErrUnreachable
			x.ctl.roles[x.lastRun(rot.ID, "candidate_handle")] = rotRole{}
		})
		x.advance(7 * time.Hour)
		x.drain()
		if got := x.phase(run); got != "quarantine" {
			t.Fatalf("an unreachable provider moved the run to %s", got)
		}
		x.advance(60 * time.Hour)
		x.drain()
		if got := x.phase(run); got != "failed" {
			t.Fatalf("an expired candidate left the run %s, want failed", got)
		}
		if got := x.lastRun(rot.ID, "last_error"); got != "expired" {
			t.Fatalf("last_error = %q", got)
		}
		return
	}
	x.ctl.set(func() { x.ctl.dropErr = nil })
	x.advance(7 * time.Hour)
	x.drain()
	if got := x.phase(run); got != "failed" {
		t.Fatalf("a run whose candidate is absent = %s, want failed", got)
	}
	if x.effects("drop_candidate", "success") != 1 || x.effects("drop_candidate", "open") != 0 {
		t.Fatal("the reconciler did not record a confirmed drop")
	}
}

// ------------------------------------------------------------ multi-node takeover

// A worker whose lease lapsed mid-CreateRole loses its fence when it settles. It
// never publishes, drops its candidate only after seeing the run rolled back, and
// the candidate reconciler removes a role neither of them could drop.
func TestRotationExecutorOrphanedCandidate(t *testing.T) {
	for _, dropFails := range []bool{false, true} {
		name := "takeover drops"
		if dropFails {
			name = "reconciler drops"
		}
		t.Run(name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) { runRotationOrphan(t, db, dropFails) })
		})
	}
}

func runRotationOrphan(t *testing.T, db *store.DB, dropFails bool) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.advance(24*time.Hour + time.Minute)
	if _, err := x.transition(x.actor, "pause", rot.ID); err != nil {
		t.Fatal(err)
	}
	// Pausing before the run starts would stop it; resume at the same instant and
	// use the hook to run the takeover while worker A is inside CreateRole.
	if _, err := x.transition(x.actor, "resume", rot.ID); err != nil {
		t.Fatal(err)
	}
	x.advance(24*time.Hour + time.Minute)
	x.ctl.set(func() {
		x.ctl.at = func(p, _ string) {
			if p != "create:after" {
				return
			}
			x.ctl.set(func() {
				x.ctl.at = nil
				if dropFails {
					x.ctl.dropErr = dynamic.ErrUnreachable
				}
			})
			x.advance(rotationLeaseTermForTest + time.Second)
			if _, err := x.sweep("wrk_b"); err != nil {
				t.Errorf("takeover sweep: %v", err)
			}
		}
	})
	if _, err := x.sweep("wrk_a"); err != nil {
		t.Fatalf("the fence-losing worker: %v", err)
	}
	run := x.openRunID(rot.ID)
	if got := x.phase(run); got != "failed" {
		t.Fatalf("phase = %s, want failed", got)
	}
	if x.snapshots() != 0 {
		t.Fatal("a fence-losing worker published")
	}
	if dropFails {
		if len(x.ctl.names()) != 1 {
			t.Fatalf("expected the undroppable candidate to remain, roles %v", x.ctl.names())
		}
		x.ctl.set(func() { x.ctl.dropErr = nil })
		x.advance(7 * time.Hour)
		x.worker = "wrk_b"
		x.requireSweep("wrk_b", true)
	}
	if names := x.ctl.names(); len(names) != 0 {
		t.Fatalf("candidate roles left behind: %v", names)
	}
	if x.effects("drop_candidate", "success") == 0 || x.effects("drop_candidate", "open") != 0 {
		t.Fatal("no confirmed candidate drop")
	}
}
