package isolation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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

// rotProviderCtl is the shared state behind every provider the executor opens.
// Failures are sticky until a test clears them.
type rotProviderCtl struct {
	mu    sync.Mutex
	roles map[string]rotRole
	calls []string

	createErr   error
	extendErr   error
	dropErr     error
	statusErr   error
	afterCreate func(name string)
	// beforeDrop runs ahead of a DropRole and may veto it with an error.
	beforeDrop func(name string) error
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

type rotProvider struct{ ctl *rotProviderCtl }

func (p rotProvider) CreateRole(_ context.Context, r dynamic.CreateRoleRequest) error {
	p.ctl.mu.Lock()
	p.ctl.record("create:" + r.Name)
	err := p.ctl.createErr
	hook := p.ctl.afterCreate
	if err == nil || errors.Is(err, dynamic.ErrAmbiguous) {
		// An ambiguous create may well have happened; the double lets it.
		p.ctl.roles[r.Name] = rotRole{password: r.Password, validUntil: r.ValidUntil}
	}
	p.ctl.mu.Unlock()
	if err == nil && hook != nil {
		hook(r.Name)
	}
	return err
}

func (p rotProvider) ExtendRole(_ context.Context, name string, until time.Time) error {
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
	p.ctl.mu.Lock()
	veto := p.ctl.beforeDrop
	p.ctl.mu.Unlock()
	if veto != nil {
		if err := veto(name); err != nil {
			return err
		}
	}
	p.ctl.mu.Lock()
	defer p.ctl.mu.Unlock()
	p.ctl.record("drop:" + name)
	if p.ctl.dropErr != nil {
		if errors.Is(p.ctl.dropErr, dynamic.ErrAmbiguous) {
			delete(p.ctl.roles, name)
		}
		return p.ctl.dropErr
	}
	delete(p.ctl.roles, name)
	return nil
}

func (p rotProvider) RoleStatus(_ context.Context, name string) (dynamic.RoleStatus, error) {
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
	clock  time.Time
	worker string
}

func newRotExec(t *testing.T, db *store.DB) *rotExec {
	t.Helper()
	f := newRotationFixture(t, db)
	x := &rotExec{rotationFixture: f, ctl: &rotProviderCtl{roles: map[string]rotRole{}}, clock: f.now, worker: "wrk_a"}
	kr := probeKeyring(t, db)
	auth := authService(t, db)
	now := func() time.Time { return x.clock }
	x.exec = &service.RotationExecutor{
		DB: db, Runtime: store.NewRotationRuntime(db), Keyring: kr, Now: now,
		Values:    &service.Values{DB: db, Keyring: kr, Auth: auth},
		Revisions: &service.Revisions{DB: db, Keyring: kr, Auth: auth, Now: now},
		ProviderFactory: func(dynamic.Kind, string, string, string) (dynamic.Provider, error) {
			return rotProvider{ctl: x.ctl}, nil
		},
		ProviderDeadline: 5 * time.Second,
	}
	return x
}

// advance moves the fake clock.
func (x *rotExec) advance(d time.Duration) { x.clock = x.clock.Add(d) }

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
	next := mustParseStamp(t, queryString(t, db, "SELECT next_run_at FROM rotations WHERE id='"+rot.ID+"'"))
	if want := x.clock.Add(24 * time.Hour); next.Sub(want) > time.Second || want.Sub(next) > time.Second {
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
