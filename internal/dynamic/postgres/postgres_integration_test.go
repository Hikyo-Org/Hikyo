package postgres_test

import (
	"context"
	"crypto/x509"
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/dynamic/postgres"
)

// testAllowedCIDRs are the operator egress exceptions the integration target
// needs: a real target is on a private address, which the default-deny public
// dialer refuses without an explicit allow. HIKYO_TEST_DYNAMIC_PG_ALLOW_CIDR
// overrides; the default covers loopback and RFC1918.
func testAllowedCIDRs(t *testing.T) []netip.Prefix {
	raw := os.Getenv("HIKYO_TEST_DYNAMIC_PG_ALLOW_CIDR")
	if raw == "" {
		raw = "127.0.0.0/8,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
	}
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		p, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			t.Fatalf("bad allow CIDR %q: %v", part, err)
		}
		out = append(out, p)
	}
	return out
}

// The dynamic-secret provider mints roles at a SEPARATE PostgreSQL target from
// Hikyo's own datastore, over TLS verify-full. Its integration test therefore
// needs its own DSN and CA bundle:
//
//	HIKYO_TEST_DYNAMIC_PG_DSN  postgres://<admin>@<host>:<port>/<db>  (no password)
//	HIKYO_TEST_DYNAMIC_PG_PASSWORD  the admin password
//	HIKYO_TEST_DYNAMIC_PG_CA   path to the PEM the server's cert chains to
//	HIKYO_TEST_DYNAMIC_PG_GRANT_ROLE  a pre-existing role to grant leases (default: the admin)
//
// The admin must have CREATEROLE and membership of the grant role. It is
// skipped locally when unset and fails loud under CI, exactly as the isolation
// harness gates HIKYO_TEST_POSTGRES_DSN.
func targetProvider(t *testing.T) (*postgres.Provider, string) {
	t.Helper()
	dsn := os.Getenv("HIKYO_TEST_DYNAMIC_PG_DSN")
	if dsn == "" {
		if os.Getenv("HIKYO_DYNAMIC_PG_REQUIRED") != "" {
			t.Fatal("HIKYO_TEST_DYNAMIC_PG_DSN is unset but HIKYO_DYNAMIC_PG_REQUIRED is set; the provider integration test must run here")
		}
		t.Skip("set HIKYO_TEST_DYNAMIC_PG_DSN (+ _PASSWORD, _CA) to run the provider integration test")
	}
	password := os.Getenv("HIKYO_TEST_DYNAMIC_PG_PASSWORD")
	grantRole := os.Getenv("HIKYO_TEST_DYNAMIC_PG_GRANT_ROLE")
	if grantRole == "" {
		t.Fatal("HIKYO_TEST_DYNAMIC_PG_GRANT_ROLE is required")
	}
	var roots *x509.CertPool
	if ca := os.Getenv("HIKYO_TEST_DYNAMIC_PG_CA"); ca != "" {
		pem, err := os.ReadFile(ca)
		if err != nil {
			t.Fatalf("read CA: %v", err)
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			t.Fatal("HIKYO_TEST_DYNAMIC_PG_CA contained no certificates")
		}
	}
	p, err := postgres.New(postgres.Config{Origin: dsn, Password: password, RootCAs: roots, AllowedCIDRs: testAllowedCIDRs(t), Deadline: 10 * time.Second})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	t.Cleanup(p.Close)
	return p, grantRole
}

// TestProviderEgressRefusedWithoutAllowance proves the default-deny public
// dialer refuses a private target when no CIDR is allowed — the operator's
// egress allow-list is load-bearing, not decorative.
func TestProviderEgressRefusedWithoutAllowance(t *testing.T) {
	dsn := os.Getenv("HIKYO_TEST_DYNAMIC_PG_DSN")
	if dsn == "" {
		if os.Getenv("HIKYO_DYNAMIC_PG_REQUIRED") != "" {
			t.Fatal("HIKYO_TEST_DYNAMIC_PG_DSN is unset but HIKYO_DYNAMIC_PG_REQUIRED is set")
		}
		t.Skip("set HIKYO_TEST_DYNAMIC_PG_DSN to run the provider integration test")
	}
	p, err := postgres.New(postgres.Config{Origin: dsn, Password: os.Getenv("HIKYO_TEST_DYNAMIC_PG_PASSWORD"), Deadline: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	err = p.CreateRole(context.Background(), dynamic.CreateRoleRequest{
		Name: "hikyo_egressrefused", Password: mustPassword(t), GrantRole: "app_reader", ValidUntil: time.Now().Add(time.Hour),
	})
	if !errors.Is(err, dynamic.ErrUnreachable) {
		t.Fatalf("private target without an allowed CIDR = %v, want ErrUnreachable", err)
	}
}

func TestProviderLeaseLifecycle(t *testing.T) {
	p, grantRole := targetProvider(t)
	ctx := context.Background()

	name := dynamic.RoleName("dls_" + time.Now().UTC().Format("20060102150405.000000"))
	pw, err := dynamic.GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	// Roles are cluster-level and outlive any schema; always clean up.
	t.Cleanup(func() { _ = p.DropRole(context.Background(), name) })

	valid := time.Now().Add(time.Hour).UTC()
	if err := p.CreateRole(ctx, dynamic.CreateRoleRequest{Name: name, Password: pw, GrantRole: grantRole, ValidUntil: valid}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	st, err := p.RoleStatus(ctx, name)
	if err != nil {
		t.Fatalf("RoleStatus after create: %v", err)
	}
	if !st.Exists {
		t.Fatal("role does not exist after CreateRole")
	}
	if st.ValidUntil.Before(valid.Add(-time.Minute)) || st.ValidUntil.After(valid.Add(time.Minute)) {
		t.Errorf("VALID UNTIL = %v, want ~%v (the engine must enforce expiry)", st.ValidUntil, valid)
	}

	// Extend moves the expiry forward.
	extended := time.Now().Add(3 * time.Hour).UTC()
	if err := p.ExtendRole(ctx, name, extended); err != nil {
		t.Fatalf("ExtendRole: %v", err)
	}
	st, err = p.RoleStatus(ctx, name)
	if err != nil {
		t.Fatalf("RoleStatus after extend: %v", err)
	}
	if st.ValidUntil.Before(extended.Add(-time.Minute)) {
		t.Errorf("VALID UNTIL after extend = %v, want ~%v", st.ValidUntil, extended)
	}

	// Drop is idempotent: the role is gone, and a second drop is a success.
	if err := p.DropRole(ctx, name); err != nil {
		t.Fatalf("DropRole: %v", err)
	}
	if err := p.DropRole(ctx, name); err != nil {
		t.Fatalf("second DropRole (idempotent): %v", err)
	}
	st, err = p.RoleStatus(ctx, name)
	if err != nil {
		t.Fatalf("RoleStatus after drop: %v", err)
	}
	if st.Exists {
		t.Fatal("role still exists after DropRole")
	}
}

func TestProviderUnreachableIsDefinite(t *testing.T) {
	// Skip/CI-gate on the same env so this only runs where a real target exists.
	_, grantRole := targetProvider(t)
	_ = grantRole
	// A syntactically valid but unroutable target must classify as a definite
	// unreachable failure, never ambiguous — nothing was sent.
	p, err := postgres.New(postgres.Config{
		Origin: "postgres://nobody@127.0.0.1:1/nodb", Password: "x", Deadline: 2 * time.Second,
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	err = p.CreateRole(context.Background(), dynamic.CreateRoleRequest{
		Name: "hikyo_unreachable", Password: mustPassword(t), GrantRole: "app_reader", ValidUntil: time.Now().Add(time.Hour),
	})
	if !errors.Is(err, dynamic.ErrUnreachable) {
		t.Fatalf("CreateRole against an unreachable target = %v, want ErrUnreachable", err)
	}
	// Connect failure -> ErrUnreachable (definite), the port-1 target refuses.
	// (A blackhole would time out to ErrAmbiguous; 127.0.0.1:1 refuses fast.)
}

func mustPassword(t *testing.T) string {
	t.Helper()
	pw, err := dynamic.GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	return pw
}
