package upgradegate

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

func assertManagedPreview(t *testing.T) ConfigurationCheck {
	t.Helper()
	return func(_ context.Context, p *upgrade.CandidateConfiguration, values map[string]string) error {
		if p == nil || len(p.Catalogue) != len(runtimeconfig.Catalogue()) {
			return errors.New("old catalogue did not receive migration preview")
		}
		if values["HIKYO_MCP_WRITE_ENABLED"] != "false" || values["HIKYO_MAIL_PASSWORD"] != " retained secret\n" {
			return errors.New("preview lost retained value or explicit default")
		}
		return nil
	}
}

// Seed only the old catalogue in an admitted source, then return an assertion
// that backup and maintenance previews leave its durable data byte-identical.
func seedOldManagedPreview(t *testing.T, cfg upgrade.Config, booted Result, root []byte) func() {
	t.Helper()
	ctx := t.Context()
	runtimeDB, err := store.Open(ctx, store.Config{Engine: store.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}, booted.Admission)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeDB.Close()
	kr, err := crypto.LoadKeyring(ctx, &keyring.Store{DB: runtimeDB}, bytes.Clone(root))
	if err != nil {
		t.Fatal(err)
	}
	driver, dsn := "pgx", cfg.DSN
	if cfg.Engine == releaseidentity.SQLite {
		driver, dsn = "sqlite", cfg.Path
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	execute := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	execute("INSERT INTO orgs(id,name,active,metadata,created_at) VALUES('preview_org','preview',TRUE,'{}','2026-09-19T00:00:00Z')")
	execute("INSERT INTO projects(id,org_id,name,created_at) VALUES('preview_project','preview_org','self','2026-09-19T00:00:00Z')")
	execute("INSERT INTO environments(id,org_id,project_id,name,note,created_at,display_order) VALUES('preview_env','preview_org','preview_project','runtime','','2026-09-19T00:00:00Z',0)")
	execute("INSERT INTO project_schema_revisions VALUES('preview_org','preview_project',27)")
	execute("INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES('preview_snapshot','preview_org','preview_project','preview_env',1,27,'operator','2026-09-19T00:00:00Z')")
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
		execute("INSERT INTO keys(id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES($1,'preview_org','preview_project',$1,'',$2,'',FALSE,'',$3,'none','none','2026-09-19T00:00:00Z')", key.Name, string(key.Classification), string(declaration))
	}
	sealer, err := kr.ForProject(ctx, "preview_org", "preview_project")
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := sealer.SealField(crypto.ProjectFieldAAD{OrgID: "preview_org", ProjectID: "preview_project", EnvironmentID: "preview_env", SnapshotID: "preview_snapshot", KeyID: "HIKYO_MAIL_PASSWORD", OwnerTable: "snapshot_entries", OwnerRowID: "preview_entry", FieldTag: "snapshot_value"}, []byte(" retained secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	execute("INSERT INTO snapshot_entries(id,org_id,project_id,environment_id,snapshot_id,key_id,key_name,classification,ciphertext,value_entry_id) VALUES('preview_entry','preview_org','preview_project','preview_env','preview_snapshot','HIKYO_MAIL_PASSWORD','HIKYO_MAIL_PASSWORD','secret',$1,'preview_value')", ciphertext)
	execute("INSERT INTO self_config_binding(id,owner_instance_id,adoption_key,adopted_by,org_id,project_id,environment_id,schema_version,generation,desired_revision,desired_snapshot_id,incarnation,created_at,updated_at) VALUES(1,$1,'adoption','operator','preview_org','preview_project','preview_env',1,1,1,'preview_snapshot','incarnation','2026-09-19T00:00:00Z','2026-09-19T00:00:00Z')", booted.State.InstanceID)
	execute("INSERT INTO self_config_retention VALUES('desired','preview_snapshot')")
	snapshot := func() []any {
		t.Helper()
		var generation, revision, version, keys, snapshots, entries int64
		var desired string
		var retained []byte
		if err := db.QueryRowContext(ctx, "SELECT generation,desired_revision,migration_version,desired_snapshot_id FROM self_config_binding WHERE id=1").Scan(&generation, &revision, &version, &desired); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM keys WHERE org_id='preview_org'").Scan(&keys); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM snapshots WHERE org_id='preview_org'").Scan(&snapshots); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM snapshot_entries WHERE org_id='preview_org'").Scan(&entries); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, "SELECT ciphertext FROM snapshot_entries WHERE id='preview_entry'").Scan(&retained); err != nil {
			t.Fatal(err)
		}
		return []any{generation, revision, version, desired, keys, snapshots, entries, retained}
	}
	before := snapshot()
	return func() {
		t.Helper()
		if !reflect.DeepEqual(before, snapshot()) {
			t.Fatal("read-only migration preview changed source data")
		}
	}
}
