package upgrade

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// A rollback runs as an unprivileged role: it cannot read another role's backend_type, and it
// must still refuse beside that role's session, then leave "public" empty of every object kind
// the preflight counts. Roles are cluster wide, so they are dropped after their database.
func TestPostgresRollbackAsAnUnprivilegedRole(t *testing.T) {
	adminDSN := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if adminDSN == "" {
		testConfig(t, releaseidentity.Postgres) // skips, or fails under CI
	}
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	suffix := time.Now().UnixNano()
	rb, other := fmt.Sprintf("rb_%d", suffix), fmt.Sprintf("ot_%d", suffix)
	for _, role := range []string{rb, other} {
		if _, err := admin.ExecContext(t.Context(), fmt.Sprintf(`CREATE ROLE "%s" LOGIN PASSWORD 'pw'`, role)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Exec(`DROP ROLE IF EXISTS "` + role + `"`) })
	}
	cfg := testConfig(t, releaseidentity.Postgres)
	as := func(role string) string {
		u, err := url.Parse(cfg.DSN)
		if err != nil {
			t.Fatal(err)
		}
		u.User = url.UserPassword(role, "pw")
		return u.String()
	}
	database := strings.TrimPrefix(mustParse(t, cfg.DSN).Path, "/")
	owner, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{rb, other} {
		for _, statement := range []string{fmt.Sprintf(`GRANT CONNECT ON DATABASE "%s" TO "%s"`, database, role), fmt.Sprintf(`GRANT ALL ON SCHEMA public TO "%s"`, role)} {
			if _, err := owner.ExecContext(t.Context(), statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	rbDB, err := sql.Open("pgx", as(rb))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE t (a int PRIMARY KEY)`, `CREATE TYPE composite AS (x int)`, `CREATE FUNCTION add2(int, int) RETURNS int LANGUAGE sql AS 'SELECT 1'`,
		`CREATE AGGREGATE total(int) (sfunc = int4pl, stype = int)`, `CREATE OPERATOR <<<< (leftarg = int, rightarg = int, function = add2)`,
		`CREATE COLLATION lax (provider = libc, locale = 'C')`, `CREATE TEXT SEARCH DICTIONARY words (template = simple)`,
	} {
		if _, err := rbDB.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	rbCfg := Config{Engine: releaseidentity.Postgres, DSN: as(rb)}
	if err := PreflightPostgresRollback(t.Context(), rbCfg); err != nil {
		t.Fatalf("preflight over objects the role owns: %v", err)
	}
	// An aggregate owned by someone else is counted, as a table would be.
	if _, err := owner.ExecContext(t.Context(), `CREATE AGGREGATE foreign_total(int) (sfunc = int4pl, stype = int)`); err != nil {
		t.Fatal(err)
	}
	if err := PreflightPostgresRollback(t.Context(), rbCfg); err == nil {
		t.Fatal("preflight accepted an aggregate the role does not own")
	}
	if _, err := owner.ExecContext(t.Context(), `DROP AGGREGATE foreign_total(int)`); err != nil {
		t.Fatal(err)
	}
	// A view elsewhere over a public table would go with the reset's CASCADE drops, unchecked.
	if _, err := owner.ExecContext(t.Context(), `CREATE SCHEMA reporting; CREATE VIEW reporting.ids AS SELECT a FROM public.t`); err != nil {
		t.Fatal(err)
	}
	if err := PreflightPostgresRollback(t.Context(), rbCfg); err == nil || !strings.Contains(err.Error(), "reporting.ids") {
		t.Fatalf("preflight accepted a view outside public over a public table: %v", err)
	}

	// Only the other role's session may remain connected.
	if err := errors.Join(owner.Close(), rbDB.Close()); err != nil {
		t.Fatal(err)
	}
	// The other role's session is invisible in backend_type to this role, yet still refuses.
	otherDB, err := sql.Open("pgx", as(other))
	if err != nil {
		t.Fatal(err)
	}
	if err := otherDB.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	basis := RollbackBasis{Instance: "ins", BackupID: "backup", SourceGeneration: 1}
	var identity string
	err = WithRollbackLock(t.Context(), rbCfg, func(rb *Rollback) (err error) {
		if identity, err = rb.Identity(t.Context()); err != nil {
			return err
		}
		basis.Rolling, basis.Database = true, identity
		if needed, err := rb.Decide(t.Context(), basis); err != nil || !needed {
			t.Fatalf("emptied database while rolling back: needed=%t err=%v", needed, err)
		}
		admission, err := rb.Admit(basis)
		if err != nil {
			return err
		}
		if err := admission.ResetPostgres(t.Context()); err == nil || !strings.Contains(err.Error(), "another session") {
			t.Fatalf("a reset ran beside another role's session: %v", err)
		}
		if err := otherDB.Close(); err != nil {
			t.Fatal(err)
		}
		if err := admission.ResetPostgres(t.Context()); err == nil || !strings.Contains(err.Error(), "reporting.ids") {
			t.Fatalf("a reset would have dropped a view outside public: %v", err)
		}
		dropper, err := sql.Open("pgx", cfg.DSN)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dropper.ExecContext(t.Context(), `DROP SCHEMA reporting CASCADE`); err != nil {
			t.Fatal(err)
		}
		if err := dropper.Close(); err != nil {
			t.Fatal(err)
		}
		return admission.ResetPostgres(t.Context())
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(identity, database+"/") || strings.TrimPrefix(identity, database+"/") == "" {
		t.Fatalf("identity %q names no cluster", identity)
	}
	owner, err = sql.Open("pgx", cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var left int
	if err := owner.QueryRowContext(t.Context(), publicLeftovers).Scan(&left); err != nil || left != 0 {
		t.Fatalf("objects left in public: %d %v", left, err)
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// Admit mints the replacement capability only for the basis a successful Decide just required.
func TestRollbackAdmitRequiresTheDecidedBasis(t *testing.T) {
	cfg := testConfig(t, releaseidentity.SQLite)
	basis := RollbackBasis{Instance: "ins", BackupID: "backup", SourceGeneration: 1, Rolling: true}
	err := WithRollbackLock(t.Context(), cfg, func(rb *Rollback) (err error) {
		if _, err := rb.Admit(basis); !errors.Is(err, ErrConflict) {
			t.Fatalf("admitted without a decision: %v", err)
		}
		if basis.Database, err = rb.Identity(t.Context()); err != nil {
			return err
		}
		if needed, err := rb.Decide(t.Context(), basis); err != nil || !needed {
			t.Fatalf("absent database while rolling back: needed=%t err=%v", needed, err)
		}
		other := basis
		other.BackupID = "another"
		if _, err := rb.Admit(other); !errors.Is(err, ErrConflict) {
			t.Fatalf("admitted a basis Decide never saw: %v", err)
		}
		if _, err := rb.Admit(basis); err != nil {
			return err
		}
		other.Database = "elsewhere"
		if _, err := rb.Decide(t.Context(), other); err == nil {
			t.Fatal("an absent database at another identity was decided")
		}
		if _, err := rb.Admit(basis); !errors.Is(err, ErrConflict) {
			t.Fatalf("a refused decision left the earlier one admissible: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
