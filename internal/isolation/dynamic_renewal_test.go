package isolation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type renewalFixture struct {
	svc   *service.Dynamic
	actor service.Actor
	scope domain.Scope
	lease service.MintLeaseResult
	now   time.Time
}

// renewalFixtureFor uses real workload credentials, grants, encrypted provider
// custody, storage and worker state. Only factory chooses the external provider.
func renewalFixtureFor(t *testing.T, db *store.DB, factory dynamic.Factory, origin, grantRole string, credential []byte) *renewalFixture {
	t.Helper()
	ctx := tctx(t)
	projScope := domain.Scope{Org: orgA, Project: prjA1}
	envScope := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}

	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_dyn_manage','usr_alice','manage-identities','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_dyn_read','usr_alice','read','org_a','prj_a1','env_a1',`+ts+`)`)
	// The workload that mints: a real machine bearer, so its class resolves to
	// workload and the machine-reveal opt-in (not the human ceremony) is the
	// disclosure authority. The credential is minted BEFORE the workload is
	// granted any reveal-reachable environment, so the credential-mint's own
	// disclosure ceremony is vacuous (the delivery suite's technique).
	ident := identitySvc(db)
	sa, err := ident.CreateServiceAccount(ctx, service.LocalPrincipal(alice), prjScope(), "dyn-workload", domain.ClassWorkload)
	if err != nil {
		t.Fatalf("create workload: %v", err)
	}
	minted, err := ident.MintCredential(ctx, service.LocalPrincipal(alice), prjScope(), sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatalf("mint workload credential: %v", err)
	}
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_dyn_sa_read','`+string(sa.Principal)+`','read','org_a','prj_a1','env_a1',`+ts+`)`)
	// Mint requires read AND reveal over the environment even for a machine; the
	// opt-in enables machine reveal, the grant authorizes it here.
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_dyn_sa_reveal','`+string(sa.Principal)+`','reveal','org_a','prj_a1','env_a1',`+ts+`)`)
	execRaw(t, db, `UPDATE projects SET machine_reveal=true WHERE id='prj_a1'`)
	minter := service.Bearer(minted.Value)

	f := &renewalFixture{actor: minter, scope: envScope, now: store.CanonTime(time.Now())}
	f.svc = &service.Dynamic{DB: db, Keyring: probeKeyring(t, db), Runtime: store.NewDynamicRuntime(db), ProviderFactory: factory, LeaseDeadline: 5 * time.Second, Now: func() time.Time { return f.now }}
	provider, err := f.svc.Configure(ctx, service.LocalPrincipal(alice), projScope, service.CreateDynamicProviderRequest{Kind: string(dynamic.KindPostgres), Origin: origin, GrantRole: grantRole, Credential: credential})
	if err != nil {
		t.Fatal(err)
	}
	f.lease, err = f.svc.MintLease(ctx, minter, envScope, service.MintLeaseRequest{ProviderID: provider.ID, MaxTTLSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

type renewalProvider struct {
	fakeDynamicProvider
	expiries []time.Time
	failure  error
}

func (p *renewalProvider) ExtendRole(_ context.Context, _ string, expiry time.Time) error {
	p.expiries = append(p.expiries, expiry)
	return p.failure
}
func fakeRenewalFixture(t *testing.T, db *store.DB) (*renewalFixture, *renewalProvider) {
	t.Helper()
	p := &renewalProvider{fakeDynamicProvider: fakeDynamicProvider{ctl: &fakeDynamicControl{}}}
	f := renewalFixtureFor(t, db, func(dynamic.Kind, string, string, string) (dynamic.Provider, error) { return p, nil }, "postgres://admin@renewal.example/app", "reader", []byte("test-only-admin"))
	return f, p
}
func (f *renewalFixture) renew(t *testing.T, seconds int64) service.DynamicLeaseView {
	t.Helper()
	got, err := f.svc.RenewLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID, seconds)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func (f *renewalFixture) sweep(t *testing.T) {
	t.Helper()
	if worked, err := f.svc.RunLeaseSweep(t.Context(), "renewal-regression"); err != nil || !worked {
		t.Fatalf("worker worked=%v err=%v", worked, err)
	}
}
func (f *renewalFixture) current(t *testing.T) service.DynamicLeaseView {
	t.Helper()
	got, err := f.svc.GetLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func assertRenewalExpiry(t *testing.T, f *renewalFixture, seconds int64) {
	t.Helper()
	got := f.current(t)
	expiry, err := time.Parse(time.RFC3339Nano, got.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	want := f.now.Add(time.Duration(seconds) * time.Second)
	if got.State != "active" || got.MaxTTLSeconds != seconds || !expiry.Equal(want) {
		t.Fatalf("lease state=%s ceiling=%d expiry=%s, want active/%d/%s", got.State, got.MaxTTLSeconds, expiry, seconds, want)
	}
}

func TestDynamicRenewalCeiling(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, p := fakeRenewalFixture(t, db)
		for _, seconds := range []int64{86400, 3600, 0, 1800, 3600, 0, 900} {
			before := f.current(t)
			auditBefore := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events")
			f.now = f.now.Add(time.Minute)
			got, err := f.svc.RenewLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID, seconds)
			if seconds > before.MaxTTLSeconds {
				if !errors.Is(err, domain.ErrConflict) {
					t.Fatalf("widen %d to %d: %v", before.MaxTTLSeconds, seconds, err)
				}
				if after := f.current(t); after != before {
					t.Fatalf("refused renewal changed row: before=%+v after=%+v", before, after)
				}
				if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events"); n != auditBefore {
					t.Fatal("refused renewal wrote audit intent")
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			want := seconds
			if want == 0 {
				want = before.MaxTTLSeconds
			}
			if got.MaxTTLSeconds != want {
				t.Fatalf("queued ceiling=%d want=%d", got.MaxTTLSeconds, want)
			}
			f.sweep(t)
			assertRenewalExpiry(t, f, want)
			if !p.expiries[len(p.expiries)-1].Equal(f.now.Add(time.Duration(want) * time.Second)) {
				t.Fatal("provider expiry exceeds period")
			}
		}
	})
}

func TestDynamicRenewalConcurrentCeilings(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := fakeRenewalFixture(t, db)
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make(chan error, 3)
		for _, seconds := range []int64{1800, 900, 86400} {
			wg.Go(func() {
				<-start
				_, err := f.svc.RenewLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID, seconds)
				results <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Fatal(err)
			}
		}
		if successes != 1 {
			t.Fatalf("successful concurrent transitions=%d want=1", successes)
		}
		got := f.current(t)
		if got.MaxTTLSeconds != 900 && got.MaxTTLSeconds != 1800 {
			t.Fatalf("ceiling widened: %d", got.MaxTTLSeconds)
		}
		f.sweep(t)
		assertRenewalExpiry(t, f, got.MaxTTLSeconds)
		if _, err := f.svc.RenewLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID, 3600); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("restore original ceiling: %v", err)
		}
	})
}

func TestDynamicRenewalRetryAndAuthorization(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, p := fakeRenewalFixture(t, db)
		f.renew(t, 900)
		p.failure = dynamic.ErrUnreachable
		f.sweep(t)
		if got := f.current(t); got.State != "renewing" || got.MaxTTLSeconds != 900 {
			t.Fatalf("retry lost ceiling: %+v", got)
		}
		if _, err := f.svc.RenewLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID, 1800); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("in-flight renew: %v", err)
		}
		p.failure = nil
		f.now = f.now.Add(time.Minute)
		f.sweep(t)
		assertRenewalExpiry(t, f, 900)
		before := f.current(t)
		f.renew(t, 600)
		execRaw(t, db, "DELETE FROM grants WHERE id='g_dyn_sa_reveal'")
		calls := len(p.expiries)
		f.sweep(t)
		if len(p.expiries) != calls {
			t.Fatal("worker extended after reveal authority withdrawn")
		}
		after := f.current(t)
		if after.State != "active" || after.ExpiresAt != before.ExpiresAt || after.MaxTTLSeconds != 600 {
			t.Fatalf("authorization refusal changed expiry or lost ceiling: %+v", after)
		}
		// Request authorization still conceals a lease from a sibling environment.
		sibling := domain.Scope{Org: orgA, Project: prjA1, Env: envA2}
		if _, err := f.svc.RenewLease(t.Context(), f.actor, sibling, f.lease.Lease.ID, 86400); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("sibling renewal: %v", err)
		}
	})
}

func TestDynamicRenewalCheckedTTL(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, p := fakeRenewalFixture(t, db)
		before := f.current(t)
		for _, seconds := range []int64{-1, dynamic.MaxTTLSeconds + 1, math.MaxInt64} {
			if _, err := f.svc.RenewLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID, seconds); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("invalid ttl %d: %v", seconds, err)
			}
		}
		if after := f.current(t); after != before {
			t.Fatal("invalid request changed lease")
		}
		// Previously queued values must not overflow when the worker resumes.
		f.renew(t, 0)
		execRaw(t, db, fmt.Sprintf("UPDATE dynamic_leases SET max_ttl_seconds=%d WHERE id='%s'", int64(math.MaxInt64), f.lease.Lease.ID))
		f.sweep(t)
		if len(p.expiries) != 0 {
			t.Fatal("invalid persisted ceiling reached provider")
		}
		if after := f.current(t); after.State != "active" || after.ExpiresAt != before.ExpiresAt {
			t.Fatal("invalid persisted ceiling changed expiry")
		}
	})
}

func TestDynamicRenewalClaimFence(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := fakeRenewalFixture(t, db)
		f.renew(t, 900)
		runtime := f.svc.Runtime
		old, ok, err := runtime.ClaimDueLease(t.Context(), "same-worker", f.now, f.now.Add(time.Minute))
		if err != nil || !ok {
			t.Fatalf("first claim: %v %v", ok, err)
		}
		effect, err := runtime.RecordIntent(t.Context(), old, "renew")
		if err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(2 * time.Minute)
		current, ok, err := runtime.ClaimDueLease(t.Context(), "same-worker", f.now, f.now.Add(time.Minute))
		if err != nil || !ok {
			t.Fatalf("replacement claim: %v %v", ok, err)
		}
		if old.MaxTTLSeconds != 900 || current.MaxTTLSeconds != 900 || current.ClaimToken <= old.ClaimToken {
			t.Fatalf("claim lost ceiling/fence: old=%+v current=%+v", old, current)
		}
		if err := runtime.RecordOutcome(t.Context(), old, effect, "renew", "success", "active", old.IssuedAt, f.now.Add(time.Hour)); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("stale outcome: %v", err)
		}
		if err := runtime.Retry(t.Context(), old, f.now); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("stale retry: %v", err)
		}
		if err := runtime.Retry(t.Context(), current, f.now); err != nil {
			t.Fatal(err)
		}
		f.sweep(t)
		assertRenewalExpiry(t, f, 900)
	})
}

func TestDynamicRenewalAmbiguityAndOptIn(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, p := fakeRenewalFixture(t, db)
		f.renew(t, 900)
		p.failure = dynamic.ErrAmbiguous
		f.sweep(t)
		if got := f.current(t); got.State != "unknown" || got.MaxTTLSeconds != 900 {
			t.Fatalf("ambiguous renewal lost ceiling: %+v", got)
		}
		p.failure = nil
		f.now = f.now.Add(time.Minute)
		f.sweep(t)
		assertRenewalExpiry(t, f, 900)
		before := f.current(t)
		f.renew(t, 600)
		execRaw(t, db, "UPDATE projects SET machine_reveal=false WHERE id='prj_a1'")
		calls := len(p.expiries)
		f.sweep(t)
		if len(p.expiries) != calls {
			t.Fatal("worker extended after machine opt-in withdrawn")
		}
		if got := f.current(t); got.State != "active" || got.ExpiresAt != before.ExpiresAt {
			t.Fatalf("opt-in refusal changed expiry: %+v", got)
		}
	})
}
