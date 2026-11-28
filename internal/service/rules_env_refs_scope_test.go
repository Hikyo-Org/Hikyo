package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// namedSQLiteTx records each statement's sqlc name ("-- name: X :kind").
type namedSQLiteTx struct {
	tx    *sql.Tx
	names *[]string
}

func (c namedSQLiteTx) record(q string) {
	name, _, _ := strings.Cut(strings.TrimPrefix(q, "-- name: "), " ")
	*c.names = append(*c.names, name)
}

func (c namedSQLiteTx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	c.record(q)
	return c.tx.ExecContext(ctx, q, args...)
}

func (c namedSQLiteTx) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return c.tx.PrepareContext(ctx, q)
}

func (c namedSQLiteTx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.record(q)
	return c.tx.QueryContext(ctx, q, args...)
}

func (c namedSQLiteTx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c.record(q)
	return c.tx.QueryRowContext(ctx, q, args...)
}

// A rule create binds its names before the caller is proved (#1447). A rule
// over one named project reads only that project's environments, the same
// statement whether the project exists or not, so a refused caller's cost
// does not grow with the organization; a rule over several projects reads
// the organization's.
func TestRuleCreateReadsOneProjectsEnvironmentsBeforeAuthorization(t *testing.T) {
	cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "rule-env-refs.db")}
	db, err := openPreparedServiceFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_refs','Refs',1,'{}','2026-11-28T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_refs_a','org_refs','A','2026-11-28T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_refs_b','org_refs','B','2026-11-28T00:00:00Z')`,
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_refs_a','org_refs','prj_refs_a','dev','','2026-11-28T00:00:00Z',0)`,
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_refs_b','org_refs','prj_refs_b','dev','','2026-11-28T00:00:00Z',0)`,
		`INSERT INTO principals (id,kind,created_at) VALUES ('usr_refs_target','human','2026-11-28T00:00:00Z')`,
		`INSERT INTO principals (id,kind,created_at) VALUES ('usr_refs_caller','human','2026-11-28T00:00:00Z')`,
	} {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	create := func(t *testing.T, projects ...domain.ProjectID) ([]string, error) {
		t.Helper()
		tx, err := db.SQLiteRead().BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		var names []string
		tok := authz.NewTxToken()
		defer tok.Invalidate()
		az := authz.NewTxAuthorizer(authn.NewSQLite(namedSQLiteTx{tx: tx, names: &names}), tok)
		_, err = prepareRuleCreate(t.Context(), az, LocalPrincipal("usr_refs_caller"), RuleSpec{
			Target: "usr_refs_target", Capability: domain.CapRead, Org: "org_refs",
			Where: domain.Where{Projects: projects, EnvMode: domain.AxisOnly, EnvNames: []string{"dev"}, KeyMode: domain.AxisAll},
		}, time.Now())
		return names, err
	}
	one, err := create(t, "prj_refs_a")
	if !errors.Is(err, domain.ErrNotFound) || slices.Contains(one, "ListRuleEnvRefsInOrg") || !slices.Contains(one, "ListRuleEnvRefsInProject") {
		t.Fatalf("refused single-project create: statements %v, err %v; want one project's environments, not found", one, err)
	}
	if missing, err := create(t, "prj_refs_missing"); !errors.Is(err, domain.ErrNotFound) || !slices.Equal(missing, one) {
		t.Fatalf("refused create over a missing project: statements %v, err %v; want %v, not found", missing, err, one)
	}
	// Control: the recorder sees the organization-wide read where it is kept.
	if both, err := create(t, "prj_refs_a", "prj_refs_b"); !errors.Is(err, domain.ErrNotFound) || !slices.Contains(both, "ListRuleEnvRefsInOrg") {
		t.Fatalf("refused two-project create: statements %v, err %v; want the organization's environments, not found", both, err)
	}
}
