package postgres_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// A revoked lease prevents new logins, but incident response must separately
// drain already-connected consumers. Exercise that boundary on the TLS fixture.
func TestProviderRevocationLeavesExistingConsumerSession(t *testing.T) {
	p, grantRole := targetProvider(t)
	ctx := t.Context()
	name := dynamic.RoleName("dls_" + time.Now().UTC().Format("20060102150405.000000"))
	password, err := dynamic.GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.CreateRole(ctx, dynamic.CreateRoleRequest{Name: name, Password: password, GrantRole: grantRole, ValidUntil: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.DropRole(context.Background(), name); err != nil {
			t.Error(err)
		}
	})
	origin, err := url.Parse(os.Getenv("HIKYO_TEST_DYNAMIC_PG_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	query := origin.Query()
	query.Set("sslmode", "verify-full")
	query.Set("sslrootcert", os.Getenv("HIKYO_TEST_DYNAMIC_PG_CA"))
	origin.RawQuery = query.Encode()
	cfg, err := pgx.ParseConfig(origin.String())
	if err != nil {
		t.Fatal(err)
	}
	cfg.User, cfg.Password = name, password
	consumer, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumer.Close(context.Background()) })
	if err := p.DropRole(ctx, name); err != nil {
		t.Fatal(err)
	}
	var one int
	if err := consumer.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("revocation unexpectedly terminated the existing consumer: %v", err)
	}
	fresh, err := pgx.ConnectConfig(ctx, cfg)
	if err == nil {
		_ = fresh.Close(context.Background())
		t.Fatal("a revoked role established a new consumer session")
	}
	var refused *pgconn.PgError
	if !errors.As(err, &refused) || (refused.Code != "28000" && refused.Code != "28P01") {
		t.Fatalf("fresh login must fail authentication, not transport: %v", err)
	}
}
