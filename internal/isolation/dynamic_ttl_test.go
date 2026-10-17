package isolation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type ttlProvider struct {
	renewalProvider
	creates []dynamic.CreateRoleRequest
	opens   int
	drops   []string
}

func (p *ttlProvider) CreateRole(_ context.Context, req dynamic.CreateRoleRequest) error {
	p.creates = append(p.creates, req)
	return nil
}

func (p *ttlProvider) DropRole(ctx context.Context, name string) error {
	p.drops = append(p.drops, name)
	return p.renewalProvider.DropRole(ctx, name)
}

func ttlFixture(t *testing.T, db *store.DB) (*renewalFixture, *ttlProvider) {
	t.Helper()
	p := &ttlProvider{renewalProvider: renewalProvider{fakeDynamicProvider: fakeDynamicProvider{ctl: &fakeDynamicControl{}}}}
	f := renewalFixtureFor(t, db, func(dynamic.Kind, string, string, string) (dynamic.Provider, error) {
		p.opens++
		return p, nil
	}, "postgres://admin@ttl.example/app", "reader", []byte("test-only-admin"))
	return f, p
}

func TestDynamicMintCheckedTTL(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, p := ttlFixture(t, db)
		for _, seconds := range []int64{dynamic.MaxTTLSeconds + 1, math.MaxInt64, 0, -1, math.MinInt64} {
			t.Run(fmt.Sprint(seconds), func(t *testing.T) {
				leases := queryInt(t, db, "SELECT COUNT(*) FROM dynamic_leases")
				audits := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events")
				opens, creates := p.opens, len(p.creates)
				got, err := f.svc.MintLease(t.Context(), f.actor, f.scope, service.MintLeaseRequest{ProviderID: f.lease.Lease.ProviderID, MaxTTLSeconds: seconds})
				if !errors.Is(err, domain.ErrInvalid) {
					t.Errorf("TTL %d: err=%v state=%s expiry=%s password_disclosed=%v", seconds, err, got.Lease.State, got.ExpiresAt, got.Password != "")
				}
				if p.opens != opens || len(p.creates) != creates {
					t.Error("invalid TTL opened provider or created a role")
				}
				if queryInt(t, db, "SELECT COUNT(*) FROM dynamic_leases") != leases || queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events") != audits {
					t.Error("invalid TTL persisted a lease or audit event")
				}
			})
		}
	})
}

func TestDynamicTTLLargestAndNormalLifetimes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, p := ttlFixture(t, db)
		for _, seconds := range []int64{dynamic.MaxTTLSeconds, 1, 3600} {
			got, err := f.svc.MintLease(t.Context(), f.actor, f.scope, service.MintLeaseRequest{ProviderID: f.lease.Lease.ProviderID, MaxTTLSeconds: seconds})
			if err != nil {
				t.Fatal(err)
			}
			want := f.now.Add(time.Duration(seconds) * time.Second)
			if got.Password == "" || got.Lease.State != "active" || !got.ExpiresAt.Equal(want) || !got.ExpiresAt.After(f.now) {
				t.Fatalf("TTL %d: state=%s expiry=%s want=%s", seconds, got.Lease.State, got.ExpiresAt, want)
			}
			persisted, err := time.Parse(time.RFC3339Nano, got.Lease.ExpiresAt)
			if err != nil || !persisted.Equal(want) {
				t.Fatalf("persisted expiry %q: %v", got.Lease.ExpiresAt, err)
			}
			if !p.creates[len(p.creates)-1].ValidUntil.Equal(want) {
				t.Fatal("wrong provider mint expiry")
			}
			if seconds == dynamic.MaxTTLSeconds {
				f.lease = got
				for _, requested := range []int64{dynamic.MaxTTLSeconds, 0, 3600} {
					f.now = f.now.Add(time.Minute)
					f.renew(t, requested)
					f.sweep(t)
					ceiling := requested
					if ceiling == 0 {
						ceiling = dynamic.MaxTTLSeconds
					}
					assertRenewalExpiry(t, f, ceiling)
				}
			}
		}
	})
}

func TestDynamicTTLCorruptQueuedRenewal(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, p := ttlFixture(t, db)
		before := f.current(t)
		failureQuery := fmt.Sprintf("SELECT COUNT(*) FROM dynamic_effects WHERE lease_id='%s' AND kind='renew' AND outcome='failure'", f.lease.Lease.ID)
		for _, seconds := range []int64{dynamic.MaxTTLSeconds + 1, math.MaxInt64} {
			failuresBefore := queryInt(t, db, failureQuery)
			// Queue a valid renewal, then simulate a legacy/corrupt persisted value.
			execRaw(t, db, fmt.Sprintf("UPDATE dynamic_leases SET max_ttl_seconds=3600 WHERE id='%s'", f.lease.Lease.ID))
			f.renew(t, 0)
			execRaw(t, db, fmt.Sprintf("UPDATE dynamic_leases SET max_ttl_seconds=%d WHERE id='%s'", seconds, f.lease.Lease.ID))
			opens, extends := p.opens, len(p.expiries)
			f.sweep(t)
			if p.opens != opens || len(p.expiries) != extends {
				t.Fatalf("corrupt queued TTL provider opens=%d want=%d extensions=%d want=%d", p.opens, opens, len(p.expiries), extends)
			}
			after := f.current(t)
			if after.State != "active" || after.ExpiresAt != before.ExpiresAt {
				t.Fatalf("corrupt TTL changed validity: %+v", after)
			}
			if failuresAfter := queryInt(t, db, failureQuery); failuresAfter != failuresBefore+1 {
				t.Fatalf("TTL %d failure outcomes=%d want=%d", seconds, failuresAfter, failuresBefore+1)
			}
		}
	})
}

func TestDynamicTTLOmittedCorruptCeiling(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, p := ttlFixture(t, db)
		execRaw(t, db, fmt.Sprintf("UPDATE dynamic_leases SET max_ttl_seconds=%d WHERE id='%s'", int64(math.MaxInt64), f.lease.Lease.ID))
		before := f.current(t)
		audits := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events")
		opens := p.opens
		if _, err := f.svc.RenewLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID, 0); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("omitted corrupt TTL: %v", err)
		}
		if after := f.current(t); after != before {
			t.Fatal("refused omitted TTL changed lease")
		}
		if p.opens != opens || queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events") != audits {
			t.Fatal("refused omitted TTL opened provider or wrote intent")
		}
		// An explicit valid tightening repairs the historical ceiling atomically.
		f.renew(t, 600)
		f.sweep(t)
		assertRenewalExpiry(t, f, 600)
	})
}

func TestDynamicTTLCorruptUnknownRenewalWithoutCredential(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, p := ttlFixture(t, db)
		before := f.current(t)
		f.renew(t, 600)
		p.failure = dynamic.ErrAmbiguous
		f.sweep(t)
		if got := f.current(t); got.State != "unknown" {
			t.Fatalf("expected ambiguous renew, got %s", got.State)
		}
		execRaw(t, db, fmt.Sprintf("UPDATE dynamic_leases SET max_ttl_seconds=%d WHERE id='%s'", int64(math.MaxInt64), f.lease.Lease.ID))
		if err := f.svc.RevokeCredential(t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, f.lease.Lease.ProviderID); err != nil {
			t.Fatal(err)
		}
		opens, extends := p.opens, len(p.expiries)
		f.now = f.now.Add(time.Minute)
		f.sweep(t)
		if p.opens != opens || len(p.expiries) != extends {
			t.Fatal("corrupt unknown renewal accessed provider")
		}
		if after := f.current(t); after.State != "active" || after.ExpiresAt != before.ExpiresAt {
			t.Fatalf("corrupt unknown renewal did not settle without credential: %+v", after)
		}
	})
}

func TestDynamicTTLCorruptCeilingStillCleansUp(t *testing.T) {
	for _, state := range []string{"revoking", "active", "minting", "unknown"} {
		t.Run(state, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				f, p := ttlFixture(t, db)
				execRaw(t, db, fmt.Sprintf("UPDATE dynamic_leases SET max_ttl_seconds=%d WHERE id='%s'", int64(math.MaxInt64), f.lease.Lease.ID))
				wantState := "failed"
				switch state {
				case "revoking":
					if _, err := f.svc.RevokeLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID); err != nil {
						t.Fatal(err)
					}
					wantState = "revoked"
				case "active":
					f.now = f.now.Add(time.Hour + time.Minute)
					wantState = "expired"
				default:
					// A crashed mint or an unknown mint with no effect ledger entry.
					execRaw(t, db, fmt.Sprintf("UPDATE dynamic_leases SET state='%s', next_attempt_at=created_at WHERE id='%s'", state, f.lease.Lease.ID))
				}
				f.sweep(t)
				if got := f.current(t); got.State != wantState {
					t.Fatalf("cleanup state=%s want=%s", got.State, wantState)
				}
				if len(p.drops) != 1 || p.drops[0] != f.lease.Username {
					t.Fatalf("cleanup did not drop owned role: %v", p.drops)
				}
			})
		})
	}
}
