package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	bundlefixture "github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
	"github.com/jackc/pgx/v5"
)

// This fixture models an archived schema-65 release, without temporary-access
// tables. Only test-owned persisted source data is transformed: every schema
// and migration digest comes from the canonical inspectors, the release bundle
// is genuinely signed, and export, archive authentication, restore and recovery
// admission all execute their ordinary production paths. No authority is forged.
func recoverySchema65Fixture(t *testing.T, engine store.Engine) upgradeDrillFixture {
	t.Helper()
	ctx := t.Context()
	seedCfg := upgradeDrillDatabase(t, engine)
	seedUC := upgrade.Config{Engine: releaseidentity.Engine(engine), Path: seedCfg.Path, DSN: seedCfg.DSN}
	root, err := crypto.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { crypto.Zero(root) })
	authority := gatefixture.Prepare(t, seedUC, store.MigrationsFS, "migrations/"+string(engine), slices.Clone(root))
	db, err := store.Open(ctx, seedCfg, authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := crypto.LoadKeyring(ctx, &keyring.Store{DB: db}, slices.Clone(root)); err != nil {
		t.Fatal(err)
	}
	state, err := upgrade.InspectControl(ctx, seedUC)
	if err != nil {
		t.Fatal(err)
	}
	// Create the historical schema directly from its exact migration prefix,
	// so later adapter migrations cannot leak into this source fixture.
	cfg := upgradeDrillDatabase(t, engine)
	uc := upgrade.Config{Engine: releaseidentity.Engine(engine), Path: cfg.Path, DSN: cfg.DSN}
	if err := migrate.RunUpTo(ctx, cfg, 65); err != nil {
		t.Fatal(err)
	}
	native := openRecoveryFixtureSQL(t, cfg)
	exec := func(query string, args ...any) {
		if _, err := native.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := releaseidentity.BuildMigrationManifest(store.MigrationsFS, "migrations/"+string(engine), uc.Engine)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Entries = slices.DeleteFunc(manifest.Entries, func(e releaseidentity.Migration) bool { return e.Version > 65 })
	migrationDigest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	var catalog upgrade.Catalog
	err = upgrade.WithLock(ctx, uc, func(session *upgrade.Session) error {
		var err error
		catalog, err = session.DomainCatalog(ctx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if engine == store.EngineSQLite {
		transaction, err := native.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := upgrade.PrepareSQLiteRestoreControlSchema(ctx, transaction, manifest, catalog.Digest()); err != nil {
			transaction.Rollback()
			t.Fatal(err)
		}
		if err := transaction.Commit(); err != nil {
			t.Fatal(err)
		}
	} else {
		conn, err := pgx.Connect(ctx, cfg.DSN)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		transaction, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := upgrade.PreparePostgresRestoreControlSchema(ctx, transaction, manifest, catalog.Digest()); err != nil {
			transaction.Rollback(ctx)
			t.Fatal(err)
		}
		if err := transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	sourceSQL := openRecoveryFixtureSQL(t, seedCfg)
	for _, table := range []string{"instance_identity", "auth_instance_state", "master_keys", "tier3_keys", "upgrade_control", "upgrade_pending", "upgrade_nonces"} {
		copyRecoveryFixtureTable(t, sourceSQL, native, table)
	}
	for _, statement := range []string{
		`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_drill','Drill',TRUE,'{}','2026-09-05T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_drill','org_drill','Drill','2026-09-05T00:00:00Z')`,
		`INSERT INTO principals (id,kind,created_at) VALUES ('usr_drill','human','2026-09-05T00:00:00Z')`,
		`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_drill','usr_drill','manage-identities','org_drill','prj_drill',NULL,'2026-09-05T00:00:00Z')`,
		`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_read','usr_drill','read','org_drill','prj_drill',NULL,'2026-09-05T00:00:00Z')`,
		`INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_drill','gr_drill','manual','usr_drill','2026-09-05T00:00:00Z')`,
		`INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_read','gr_read','manual','usr_drill','2026-09-05T00:00:00Z')`,
	} {
		exec(statement)
	}
	freshCfg := upgradeDrillDatabase(t, engine)
	empty := releaseidentity.MigrationManifest{Engine: uc.Engine, Entries: []releaseidentity.Migration{}}
	fresh, err := upgrade.InspectInstalled(ctx, upgrade.Config{Engine: uc.Engine, Path: freshCfg.Path, DSN: freshCfg.DSN}, empty)
	if err != nil {
		t.Fatal(err)
	}
	freshSource := upgradecompat.InstalledSource{Identity: fresh.Source, Migrations: empty, SchemaSHA256: fresh.SchemaDigest}
	bundle := bundlefixture.Write(t, freshSource, []bundlefixture.Target{
		{Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), Migrations: manifest, SchemaSHA256: catalog.Digest()},
		{Version: "1.1.0", Sequence: 2, Commit: strings.Repeat("b", 40), Migrations: manifest, SchemaSHA256: catalog.Digest()},
	})
	initialPlan, err := bundle.Bundle.Plan(freshSource, bundle.Identities[0])
	if err != nil {
		t.Fatal(err)
	}
	// Record the inspected historical release and its signed initial route in
	// this fixture's data. Production admission is obtained only after export.
	state.Applied = releaseidentity.Source{Release: bundle.Identities[0]}
	state.MigrationDigest, state.SchemaDigest = migrationDigest, catalog.Digest()
	state.TrustDomain = upgrade.Production
	state.Floor = bundle.Bundle.Snapshot().Floor()
	state.ReleaseRootDigest = releaseidentity.Hash(bundle.Pinned.Root)
	state.Pending.Target = bundle.Identities[0]
	state.Pending.TargetMigrationDigest, state.Pending.TargetSchemaDigest = migrationDigest, catalog.Digest()
	state.Pending.RouteDigest = initialPlan.Digest()
	state.Pending.Acceptance.Floor, state.Pending.Acceptance.ReleaseRootDigest = state.Floor, state.ReleaseRootDigest
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	encode := func(value any) string {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	exec(`UPDATE upgrade_control SET applied_json=$1,migration_digest=$2,schema_digest=$3,trust_domain=$4,floor_json=$5,release_root_digest=$6 WHERE singleton=1`, encode(state.Applied), string(state.MigrationDigest), string(state.SchemaDigest), string(state.TrustDomain), encode(state.Floor), string(state.ReleaseRootDigest))
	exec(`UPDATE upgrade_pending SET operation_json=$1 WHERE singleton=1`, encode(state.Pending))
	if err := native.Close(); err != nil {
		t.Fatal(err)
	}
	source, err := upgrade.InspectInstalled(ctx, uc, manifest)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := bundle.Bundle.Plan(upgradecompat.InstalledSource{Identity: source.Source, Migrations: manifest, SchemaSHA256: source.SchemaDigest}, bundle.Identities[1])
	if err != nil {
		t.Fatal(err)
	}
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	exported := exportDrillPreparation(t, cfg, backup.Options{Recipients: []string{recipient}}, plan, nil)
	receipt, err := os.ReadFile(exported.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := backupreceipt.PinCiphertext(ctx, exported.Path, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pinned.Close() })
	operator, err := backupreceipt.PinOperator(source.InstanceID, bundle.Signer.PrimaryPublic)
	if err != nil {
		t.Fatal(err)
	}
	return upgradeDrillFixture{cfg: cfg, root: root, source: source, bundle: bundle, request: UpgradeDrillRequest{Scratch: upgradeDrillDatabase(t, engine), Ciphertext: pinned, Receipt: receipt, Plan: plan, Operator: operator, Unlock: backup.Unlock{Identity: identity}, RootKey: slices.Clone(root), Principal: "usr_drill", Scope: domain.Scope{Org: "org_drill", Project: "prj_drill"}, Now: time.Now().UTC(), Lifetime: time.Hour}}
}

func openRecoveryFixtureSQL(t *testing.T, cfg store.Config) *sql.DB {
	t.Helper()
	driver, dsn := "sqlite", cfg.Path
	if cfg.Engine == store.EnginePostgres {
		driver, dsn = "pgx", cfg.DSN
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Copy only custody and installation data from the normally admitted seed.
// Domain schema and goose history always come from migrations through 65.
func copyRecoveryFixtureTable(t *testing.T, source, destination *sql.DB, table string) {
	t.Helper()
	ctx := t.Context()
	shape, err := destination.QueryContext(ctx, "SELECT * FROM "+table+" WHERE 1=0")
	if err != nil {
		t.Fatal(err)
	}
	columns, err := shape.Columns()
	shape.Close()
	if err != nil {
		t.Fatal(err)
	}
	for i := range columns {
		columns[i] = `"` + columns[i] + `"`
	}
	if _, err := destination.ExecContext(ctx, "DELETE FROM "+table); err != nil {
		t.Fatal(err)
	}
	rows, err := source.QueryContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM "+table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		if _, err := destination.ExecContext(ctx, "INSERT INTO "+table+" ("+strings.Join(columns, ",")+") VALUES ("+strings.Join(placeholders, ",")+")", values...); err != nil {
			t.Fatal(err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryTransactionsUseSchema65GrantProjection(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			f := recoverySchema65Fixture(t, engine)
			if _, err := DrillUpgrade(t.Context(), f.request); err != nil {
				t.Fatal(err)
			}
			archive := authenticateDrillFixture(t, f)
			cfg := f.request.Scratch
			err := upgrade.WithLock(t.Context(), upgrade.Config{Engine: releaseidentity.Engine(engine), Path: cfg.Path, DSN: cfg.DSN}, func(session *upgrade.Session) error {
				admission, err := session.ScratchAdmission(t.Context(), archive, f.request.Plan)
				if err != nil {
					return err
				}
				db, err := store.OpenRecovery(t.Context(), cfg, admission)
				if err != nil {
					return err
				}
				defer db.Close()
				version, err := db.SourceMigrationVersion()
				if err != nil {
					return err
				}
				if version != 65 {
					t.Fatalf("source version=%d, want65", version)
				}
				authorize := func(ctx context.Context, az *authz.TxAuthorizer) error {
					_, err := az.Authorize(ctx, authz.Identity{Principal: "usr_drill"}, authz.OpKeyList, domain.Scope{Org: "org_drill", Project: "prj_drill"})
					return err
				}
				if err := tx.RecoveryRead(t.Context(), db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error { return authorize(ctx, az) }); err != nil {
					t.Fatalf("schema65 RecoveryRead: %v", err)
				}
				if err := tx.RecoveryWrite(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error { return authorize(ctx, az) }); err != nil {
					t.Fatalf("schema65 RecoveryWrite: %v", err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
