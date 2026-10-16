package isolation

import (
	"context"
	"crypto/x509"
	"errors"
	"math"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	dynamicpg "github.com/Hikyo-Org/hikyo/internal/dynamic/postgres"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// This uses the same separate TLS target as the provider integration suite.
// No external effect is doubled: mint, renew, and RoleStatus reach PostgreSQL.
func TestDynamicRenewalRealPostgresValidity(t *testing.T) {
	origin := os.Getenv("HIKYO_TEST_DYNAMIC_PG_DSN")
	if origin == "" {
		if os.Getenv("HIKYO_DYNAMIC_PG_REQUIRED") != "" {
			t.Fatal("HIKYO_TEST_DYNAMIC_PG_DSN required")
		}
		t.Skip("set HIKYO_TEST_DYNAMIC_PG_DSN (+ _PASSWORD, _CA, _GRANT_ROLE) for real provider proof")
	}
	password := os.Getenv("HIKYO_TEST_DYNAMIC_PG_PASSWORD")
	grantRole := os.Getenv("HIKYO_TEST_DYNAMIC_PG_GRANT_ROLE")
	if password == "" || grantRole == "" {
		t.Fatal("dynamic PostgreSQL password and grant role required")
	}
	var roots *x509.CertPool
	if ca := os.Getenv("HIKYO_TEST_DYNAMIC_PG_CA"); ca != "" {
		pem, err := os.ReadFile(ca)
		if err != nil {
			t.Fatal(err)
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			t.Fatal("provider CA has no certificates")
		}
	}
	// Match the existing provider integration harness: loopback is the local
	// test target, and remote targets need an explicit test egress allowance.
	cidrs := os.Getenv("HIKYO_TEST_DYNAMIC_PG_ALLOW_CIDRS")
	if cidrs == "" {
		cidrs = "127.0.0.1/32,::1/128"
	}
	var allowed []netip.Prefix
	for _, raw := range strings.Split(cidrs, ",") {
		cidr, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			t.Fatal(err)
		}
		allowed = append(allowed, cidr)
	}
	providerOpens := 0
	factory := func(_ dynamic.Kind, origin, _ string, password string) (dynamic.Provider, error) {
		providerOpens++
		return dynamicpg.New(dynamicpg.Config{Origin: origin, Password: password, RootCAs: roots, AllowedCIDRs: allowed, Deadline: 5 * time.Second})
	}
	forEngines(t, func(t *testing.T, db *store.DB) {
		// Use whole seconds because PostgreSQL's provider SQL formats VALID UNTIL
		// with second precision. Compare the engine's actual timestamp exactly.
		f := renewalFixtureFor(t, db, factory, origin, grantRole, []byte(password))
		probe, err := factory(dynamic.KindPostgres, origin, "verify-full", password)
		if err != nil {
			t.Fatal(err)
		}
		originalRole := f.lease.Username
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := probe.DropRole(ctx, originalRole); err != nil {
				t.Errorf("drop owned role: %v", err)
			}
			probe.Close()
		})
		check := func(t *testing.T, want time.Time) {
			t.Helper()
			role, err := probe.RoleStatus(t.Context(), f.lease.Username)
			if err != nil {
				t.Fatal(err)
			}
			if !role.Exists || !role.ValidUntil.Equal(want.Truncate(time.Second)) {
				t.Fatalf("provider VALID UNTIL=%s exists=%v want=%s", role.ValidUntil, role.Exists, want.Truncate(time.Second))
			}
			t.Logf("real PostgreSQL VALID UNTIL=%s", role.ValidUntil.Format(time.RFC3339))
		}
		check(t, f.lease.ExpiresAt)
		if _, err := f.svc.RenewLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID, 86400); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("widen renewal: %v", err)
		}
		check(t, f.lease.ExpiresAt)
		// A renewal is a per-period ceiling, not a lifetime cap since issuance.
		f.now = f.now.Add(30 * time.Minute)
		f.renew(t, 0)
		f.sweep(t)
		assertRenewalExpiry(t, f, 3600)
		check(t, f.now.Add(time.Hour))
		f.now = f.now.Add(time.Minute)
		f.renew(t, 600)
		f.sweep(t)
		assertRenewalExpiry(t, f, 600)
		check(t, f.now.Add(10*time.Minute))
		if _, err := f.svc.RenewLease(t.Context(), f.actor, f.scope, f.lease.Lease.ID, 3600); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("restore larger ceiling: %v", err)
		}
		check(t, f.now.Add(10*time.Minute))

		// Keep overflow coverage in this top-level test: its isolation shard
		// provisions the mandatory real TLS PostgreSQL target in CI.
		t.Run("ttl-boundaries", func(t *testing.T) {
			for _, seconds := range []int64{dynamic.MaxTTLSeconds + 1, math.MaxInt64, 0, -1} {
				opens := providerOpens
				leases := queryInt(t, db, "SELECT COUNT(*) FROM dynamic_leases")
				_, err := f.svc.MintLease(t.Context(), f.actor, f.scope, service.MintLeaseRequest{ProviderID: f.lease.Lease.ProviderID, MaxTTLSeconds: seconds})
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("invalid mint TTL %d: %v", seconds, err)
				}
				if providerOpens != opens || queryInt(t, db, "SELECT COUNT(*) FROM dynamic_leases") != leases {
					t.Fatal("invalid mint reached provider or persistence")
				}
			}
			minted, err := f.svc.MintLease(t.Context(), f.actor, f.scope, service.MintLeaseRequest{ProviderID: f.lease.Lease.ProviderID, MaxTTLSeconds: dynamic.MaxTTLSeconds})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := probe.DropRole(ctx, minted.Username); err != nil {
					t.Errorf("drop boundary role: %v", err)
				}
			})
			f.lease = minted
			assertRenewalExpiry(t, f, dynamic.MaxTTLSeconds)
			check(t, minted.ExpiresAt)
			for _, seconds := range []int64{dynamic.MaxTTLSeconds + 1, math.MaxInt64, -1} {
				if _, err := f.svc.RenewLease(t.Context(), f.actor, f.scope, minted.Lease.ID, seconds); !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("invalid renew TTL %d: %v", seconds, err)
				}
				check(t, minted.ExpiresAt)
			}
			for _, requested := range []int64{dynamic.MaxTTLSeconds, 0} {
				f.now = f.now.Add(time.Minute)
				f.renew(t, requested)
				f.sweep(t)
				assertRenewalExpiry(t, f, dynamic.MaxTTLSeconds)
				check(t, f.now.Add(time.Duration(dynamic.MaxTTLSeconds)*time.Second))
			}
			f.renew(t, 60)
			f.sweep(t)
			assertRenewalExpiry(t, f, 60)
			check(t, f.now.Add(time.Minute))
			if _, err := f.svc.RevokeLease(t.Context(), f.actor, f.scope, minted.Lease.ID); err != nil {
				t.Fatal(err)
			}
			f.sweep(t)
			role, err := probe.RoleStatus(t.Context(), minted.Username)
			if err != nil || role.Exists {
				t.Fatalf("revoked boundary role exists=%v err=%v", role.Exists, err)
			}
		})
	})
}
