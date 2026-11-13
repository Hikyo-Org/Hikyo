package migrate

import (
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// A recovery drill runs the CURRENT resolver against a scratch database at the
// archive's SOURCE schema. Schemas 66 through 88 predate the containment
// columns, so the guarded historical resolver must load grants and rules
// without referencing them; the current resolver must not run there at all.
func TestHistoricalRecoveryBeforeContainmentSQLite(t *testing.T) {
	cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "before-containment.db")}
	seedBeforeContainment(t, cfg)
	db := migrationFixtureSQL(t, cfg)
	historical := authn.NewHistoricalRecoverySQLite(db, 87)
	assertBeforeContainmentReads(t, historical)
	if _, err := authn.NewSQLite(db).Grants(t.Context(), "usr_before"); err == nil {
		t.Fatal("the current resolver read a schema that predates the containment columns")
	}
}

func TestHistoricalRecoveryBeforeContainmentPostgres(t *testing.T) {
	cfg := postgresTestConfig(t, "before_containment")
	seedBeforeContainment(t, cfg)
	conn, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(t.Context()) })
	assertBeforeContainmentReads(t, authn.NewHistoricalRecoveryPG(conn, 87))
	if _, err := authn.NewPG(conn).Grants(t.Context(), "usr_before"); err == nil {
		t.Fatal("the current resolver read a schema that predates the containment columns")
	}
}

func seedBeforeContainment(t *testing.T, cfg store.Config) {
	t.Helper()
	if err := RunUpTo(t.Context(), cfg, 87); err != nil {
		t.Fatal(err)
	}
	execUpgrade(t, cfg, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_before', 'human', '2026-01-01T00:00:00Z')`)
	execUpgrade(t, cfg, `INSERT INTO grants (id, principal_id, capability, created_at) VALUES ('g_before', 'usr_before', 'instance-config', '2026-01-01T00:00:00Z')`)
}

func assertBeforeContainmentReads(t *testing.T, r *authn.Resolver) {
	t.Helper()
	grants, err := r.Grants(t.Context(), "usr_before")
	if err != nil || len(grants) != 1 || grants[0].Capability != domain.CapInstanceConfig {
		t.Fatalf("historical grants: %+v, %v", grants, err)
	}
	if _, err := r.Rules(t.Context(), "usr_before"); err != nil {
		t.Fatalf("historical rules: %v", err)
	}
}
