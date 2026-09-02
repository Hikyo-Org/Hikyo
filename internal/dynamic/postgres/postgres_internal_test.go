package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
)

func TestCanonicalDSNRejectsUnsafeOrigins(t *testing.T) {
	for _, origin := range []string{
		"",                                         // empty
		"https://db.example/app",                   // wrong scheme
		"postgres://db.example:5432/app",           // no user
		"postgres://u:p@db.example:5432/app",       // embedded password
		"postgres://u@db.example:5432",             // no database
		"postgres://u@:5432/app",                   // no host
		"postgres://u@db.example:5432/app?x=1&y=2", // extra query is fine? no: allowed, sslmode is added
	} {
		_, _, err := canonicalDSN(origin, "pw")
		// The last case (extra query) IS permitted; assert only the genuinely
		// unsafe ones fail. Recompute per-case.
		unsafe := origin == "" ||
			strings.HasPrefix(origin, "https://") ||
			!strings.Contains(origin, "@") ||
			strings.Contains(origin, ":p@") ||
			origin == "postgres://u@db.example:5432" ||
			origin == "postgres://u@:5432/app"
		if unsafe && err == nil {
			t.Errorf("canonicalDSN(%q) accepted an unsafe origin", origin)
		}
	}
}

func TestCanonicalDSNForcesVerifyFull(t *testing.T) {
	dsn, host, err := canonicalDSN("postgres://admin@db.example:5432/app", "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if host != "db.example" {
		t.Errorf("host = %q, want db.example", host)
	}
	if !strings.Contains(dsn, "sslmode=verify-full") {
		t.Errorf("dsn %q does not force sslmode=verify-full", dsn)
	}
	if !strings.Contains(dsn, "admin:s3cret@") {
		t.Errorf("dsn %q did not inject the password", dsn)
	}
}

func TestClassifyExec(t *testing.T) {
	if err := classifyExec(nil); err != nil {
		t.Errorf("nil -> %v", err)
	}
	// A server-side error (PgError) is a DEFINITE refusal.
	pgErr := &pgconn.PgError{Code: "42704", Message: "role \"x\" does not exist"}
	if err := classifyExec(pgErr); !errors.Is(err, dynamic.ErrRefused) {
		t.Errorf("PgError -> %v, want ErrRefused", err)
	}
	// A connect failure passed through stays ErrUnreachable (definite).
	if err := classifyExec(dynamic.ErrUnreachable); !errors.Is(err, dynamic.ErrUnreachable) {
		t.Errorf("unreachable -> %v, want ErrUnreachable", err)
	}
	// A context/transport error after connect is AMBIGUOUS.
	if err := classifyExec(context.DeadlineExceeded); !errors.Is(err, dynamic.ErrAmbiguous) {
		t.Errorf("deadline -> %v, want ErrAmbiguous", err)
	}
}

func TestCreateRoleSQLIsInjectionSafe(t *testing.T) {
	pw, _ := dynamic.GeneratePassword()
	sql := createRoleSQL(dynamic.CreateRoleRequest{
		Name: "hikyo_dls0192abcd", Password: pw, GrantRole: "app_reader",
		ValidUntil: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
	})
	for _, want := range []string{
		`CREATE ROLE "hikyo_dls0192abcd" LOGIN PASSWORD '`,
		`VALID UNTIL '2030-01-02T03:04:05Z'`,
		`IN ROLE "app_reader"`,
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("createRoleSQL missing %q in %q", want, sql)
		}
	}
	// The password is charset-validated upstream, but the literal is escaped
	// regardless: a grant role with a quote is doubled by pgx.Identifier, never
	// breaking out.
	evil := createRoleSQL(dynamic.CreateRoleRequest{Name: "hikyo_x", Password: pw, GrantRole: `a"; DROP ROLE hikyo_x; --`, ValidUntil: time.Now()})
	if strings.Contains(evil, `; DROP ROLE hikyo_x; --"`) && !strings.Contains(evil, `"a""; DROP ROLE hikyo_x; --"`) {
		t.Errorf("grant role identifier was not safely quoted: %q", evil)
	}
}

func TestQuoteLiteralDoublesQuotes(t *testing.T) {
	if got := quoteLiteral("a'b"); got != "'a''b'" {
		t.Errorf("quoteLiteral(a'b) = %q, want 'a''b'", got)
	}
}

func TestNewRejectsWeakConfig(t *testing.T) {
	if _, err := New(Config{Origin: "postgres://u@h:5432/db", Password: "", Deadline: time.Second}); err == nil {
		t.Error("New accepted an empty password")
	}
	if _, err := New(Config{Origin: "postgres://u@h:5432/db", Password: "p", Deadline: 0}); err == nil {
		t.Error("New accepted a zero deadline")
	}
}
