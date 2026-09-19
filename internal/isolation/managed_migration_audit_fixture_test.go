package isolation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/devupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// runManagedMigrationAuditLifecycle seeds the historical managed catalogue as
// fixture input at an authenticated schema-applied boundary. The actual fenced
// migration must seal its new snapshot and emit its audit atomically. No audit
// row or runtime admission is manufactured by the fixture.
func runManagedMigrationAuditLifecycle(t *testing.T, engine store.Engine) *store.DB {
	t.Helper()
	ctx := t.Context()
	cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "migration-audit.db")}
	driver, dsn := "sqlite", cfg.Path+"?_pragma=foreign_keys(1)"
	if engine == store.EnginePostgres {
		cfg.DSN = derivedDatabase(t, postgresTestDSN(t), "_managedmigrationaudit")
		driver, dsn = "pgx", cfg.DSN
		reset, err := sql.Open(driver, dsn)
		if err != nil {
			t.Fatal(err)
		}
		_, err = reset.ExecContext(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public")
		reset.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	root := bytes.Repeat([]byte{63}, crypto.KeySize)
	defer crypto.Zero(root)
	material, err := devupgrade.Open(ctx, isolationCustodyDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	request := upgradegate.Request{Store: upgrade.Config{Engine: releaseidentity.Engine(engine), Path: cfg.Path, DSN: cfg.DSN}, BundleDirectory: material.Directory, Pinned: material.Pinned, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(engine), Mode: upgradegate.Migrate, AllowMigrations: true, RootKey: root}
	prepared, err := upgradegate.RunDevelopment(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.SchemaOnly || prepared.State.Pending.Phase != upgrade.SchemaApplied {
		t.Fatal("signed gate omitted migration boundary")
	}
	raw, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	fixture := &migrationAuditKeys{}
	kr, err := crypto.LoadKeyring(ctx, fixture, bytes.Clone(root))
	if err != nil {
		t.Fatal(err)
	}
	project, _, err := kr.PrepareNewProject("org_migration", "prj_migration")
	if err != nil {
		t.Fatal(err)
	}
	fixture.tier3 = append(fixture.tier3, project)
	stamp := "2026-09-19T00:00:00Z"
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := raw.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO master_keys(version,root_key_epoch,state,blob,created_at) VALUES($1,$2,'active',$3,$4)", fixture.master.Version, fixture.master.RootKeyEpoch, fixture.master.Blob, stamp)
	for _, key := range fixture.tier3 {
		exec("INSERT INTO tier3_keys(id,purpose,org_id,project_id,version,master_key_version,state,blob,created_at) VALUES($1,$2,$3,$4,$5,$6,'active',$7,$8)", key.ID, string(key.Purpose), key.OrgID, key.ProjectID, key.Version, key.MasterKeyVersion, key.Blob, stamp)
	}
	exec("INSERT INTO orgs(id,name,active,metadata,created_at) VALUES('org_migration','Migration source',TRUE,'{}',$1)", stamp)
	exec("INSERT INTO projects(id,org_id,name,created_at) VALUES('prj_migration','org_migration','Managed source',$1)", stamp)
	exec("INSERT INTO environments(id,org_id,project_id,name,note,created_at,display_order) VALUES('env_migration','org_migration','prj_migration','runtime','',$1,0)", stamp)
	exec("INSERT INTO project_schema_revisions(org_id,project_id,revision) VALUES('org_migration','prj_migration',27)")
	for _, key := range runtimeconfig.Catalogue() {
		if key.Name == "HIKYO_MCP_WRITE_ENABLED" {
			continue
		}
		compiled, err := schema.CompileClassified(key.Classification, key.Declaration)
		if err != nil {
			t.Fatal(err)
		}
		declaration, err := compiled.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		exec("INSERT INTO keys(id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES($1,'org_migration','prj_migration',$1,'',$2,'',FALSE,'',$3,'none','none',$4)", key.Name, string(key.Classification), string(declaration), stamp)
	}
	exec("INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES('snp_source','org_migration','prj_migration','env_migration',1,27,'',$1)", stamp)
	incarnation, err := prepared.State.RecoveryIncarnation.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO self_config_binding(id,owner_instance_id,adoption_key,adopted_by,org_id,project_id,environment_id,schema_version,generation,desired_revision,desired_snapshot_id,incarnation,created_at,updated_at) VALUES(1,$1,'fixture','fixture','org_migration','prj_migration','env_migration',1,1,1,'snp_source',$2,$3,$3)", prepared.State.InstanceID, string(incarnation), stamp)
	exec("INSERT INTO self_config_retention(slot,snapshot_id) VALUES('desired','snp_source')")
	check := func(_ context.Context, p *upgrade.CandidateConfiguration, values map[string]string) error {
		if p == nil || len(p.Catalogue) != len(runtimeconfig.Catalogue()) || values["HIKYO_MCP_WRITE_ENABLED"] != "false" {
			return errors.New("migration omitted declaration or disabled default")
		}
		_, err := runtimeconfig.Prepare(values)
		return err
	}
	err = upgrade.WithLock(ctx, request.Store, func(session *upgrade.Session) error {
		state, err := session.Resume(ctx, prepared.State)
		if err != nil {
			return err
		}
		if err := session.MigrateConfiguration(ctx, state, bytes.Clone(root), check); err != nil {
			return err
		}
		if err := session.MigrateConfiguration(ctx, state, bytes.Clone(root), check); err != nil {
			return err
		}
		_, err = session.Advance(ctx, state, upgrade.Healthy)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Runtime admission is obtained only through authenticated restart health,
	// which opens the persisted migrated snapshot again under the original root.
	request.Mode = upgradegate.Boot
	request.CheckConfiguration = check
	healthy, err := upgradegate.RunDevelopment(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, cfg, healthy.Admission)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if n := queryInt(t, db, "SELECT count(*) FROM audit_instance_events WHERE type='self_config.migrated'"); n != 1 {
		t.Fatalf("migration emitted %d events, want one after retry", n)
	}
	return db
}

// The fixture constructs encrypted historical input with the production crypto
// implementation, before the maintained session reads it. It grants no SQL or
// upgrade authority and never participates in the migration's audit insertion.
type migrationAuditKeys struct {
	master *crypto.WrappedKey
	tier3  []crypto.WrappedKey
}

func (k *migrationAuditKeys) ActiveMasterWrappers(context.Context) ([]crypto.WrappedKey, error) {
	if k.master == nil {
		return nil, nil
	}
	return []crypto.WrappedKey{*k.master}, nil
}
func (k *migrationAuditKeys) AllOpenableTier3(context.Context) ([]crypto.WrappedKey, error) {
	return k.tier3, nil
}
func (k *migrationAuditKeys) Tier3Versions(_ context.Context, p crypto.Purpose, org, project string) ([]crypto.WrappedKey, error) {
	var rows []crypto.WrappedKey
	for _, key := range k.tier3 {
		if key.Purpose == p && key.OrgID == org && key.ProjectID == project {
			rows = append(rows, key)
		}
	}
	return rows, nil
}
func (k *migrationAuditKeys) ActiveTier3(ctx context.Context, p crypto.Purpose, org, project string) (crypto.WrappedKey, error) {
	rows, err := k.Tier3Versions(ctx, p, org, project)
	if err != nil {
		return crypto.WrappedKey{}, err
	}
	if len(rows) == 0 {
		return crypto.WrappedKey{}, crypto.ErrNoKey
	}
	return rows[0], nil
}
func (k *migrationAuditKeys) CreateHierarchy(_ context.Context, master crypto.WrappedKey, tier3 []crypto.WrappedKey) error {
	if k.master != nil {
		return crypto.ErrKeyExists
	}
	k.master = &master
	k.tier3 = tier3
	return nil
}
func (k *migrationAuditKeys) CreateTier3(_ context.Context, key crypto.WrappedKey) error {
	k.tier3 = append(k.tier3, key)
	return nil
}

func TestManagedMigrationAuditLifecycle(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) { runManagedMigrationAuditLifecycle(t, engine) })
	}
}
