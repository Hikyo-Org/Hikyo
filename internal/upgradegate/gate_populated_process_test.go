package upgradegate_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	bundlefixture "github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
	"github.com/jackc/pgx/v5"
)

func processStoreConfig(cfg upgrade.Config) store.Config {
	return store.Config{Engine: store.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}
}
func populateProcessSource(t *testing.T, db *store.DB, kr *crypto.Keyring) {
	t.Helper()
	queries := []string{
		`INSERT INTO orgs(id,name,active,metadata,created_at) VALUES ('org_process','Process',TRUE,'{}','2026-09-05T00:00:00Z')`,
		`INSERT INTO projects(id,org_id,name,created_at) VALUES ('prj_process','org_process','Process','2026-09-05T00:00:00Z')`,
		`INSERT INTO environments(id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_process','org_process','prj_process','process','','2026-09-05T00:00:00Z',0)`,
		`INSERT INTO keys(id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ('key_process','org_process','prj_process','PROCESS_SECRET','','secret','',FALSE,'','{}','none','none','2026-09-05T00:00:00Z')`,
		`INSERT INTO principals(id,kind,created_at) VALUES ('usr_process','human','2026-09-05T00:00:00Z')`,
		`INSERT INTO grants(id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_process','usr_process','manage-identities','org_process','prj_process',NULL,'2026-09-05T00:00:00Z')`,
		`INSERT INTO grant_origins(id,grant_id,kind,subject,created_at) VALUES ('gor_process','gr_process','manual','usr_process','2026-09-05T00:00:00Z')`,
	}
	execute := func(query string, args ...any) {
		t.Helper()
		if db.Engine() == store.EnginePostgres {
			tx, err := db.BeginPostgres(t.Context(), pgx.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(t.Context())
			if _, err := tx.Exec(t.Context(), query, args...); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
		} else {
			tx, err := db.BeginSQLite(t.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(t.Context(), query, args...); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, q := range queries {
		execute(q)
	}
	sealer, err := kr.ForProject(t.Context(), "org_process", "prj_process")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealer.SealValue(crypto.ValueAAD{OrgID: "org_process", ProjectID: "prj_process", EnvID: "env_process", KeyID: "key_process", RowID: "val_process", FieldTag: "value"}, []byte("synthetic-process-recovery-proof"))
	if err != nil {
		t.Fatal(err)
	}
	execute(`INSERT INTO value_entries(id,org_id,project_id,environment_id,key_id,ciphertext,updated_at,updated_by) VALUES ('val_process','org_process','prj_process','env_process','key_process',$1,'2026-09-05T00:00:00Z','usr_process')`, sealed)
}

func TestGatePopulatedProcessCrashRoutes(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		for _, scenario := range []string{"direct-sql-complete", "multihop-first-healthy", "multihop-final-schema-applied"} {
			t.Run(string(engine)+"/"+scenario, func(t *testing.T) {
				cfg := upgradegate.GateStoreForProcessTest(t, engine)
				manifest, err := releaseidentity.BuildMigrationManifest(store.MigrationsFS, "migrations/"+string(engine), engine)
				if err != nil {
					t.Fatal(err)
				}
				empty := releaseidentity.MigrationManifest{Engine: engine, Entries: []releaseidentity.Migration{}}
				inspected, err := upgrade.Inspect(t.Context(), cfg, empty)
				if err != nil {
					t.Fatal(err)
				}
				schema := upgradegate.GateCurrentSchemaForTest(t, engine)
				source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.FreshGenesisV1}, Migrations: empty, SchemaSHA256: inspected.CatalogDigest}
				fixture := bundlefixture.Write(t, source, []bundlefixture.Target{
					{Version: "1.0.1", Sequence: 1, Commit: strings.Repeat("a", 40), Migrations: manifest, SchemaSHA256: schema},
					{Version: "1.0.2", Sequence: 2, Commit: strings.Repeat("b", 40), Migrations: manifest, SchemaSHA256: schema},
					{Version: "1.0.3", Sequence: 3, Commit: strings.Repeat("c", 40), Migrations: manifest, SchemaSHA256: schema},
				})
				claim := func(index int) []byte {
					raw, err := os.ReadFile(filepath.Join(fixture.Directory, "releases", string(fixture.Identities[index].ManifestSHA256), "upgrade-compatibility.json"))
					if err != nil {
						t.Fatal(err)
					}
					return raw
				}
				root := bytes.Repeat([]byte{37}, crypto.KeySize)
				request := upgradegate.Request{Store: cfg, BundleDirectory: fixture.Directory, Pinned: fixture.Pinned, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(engine), Mode: upgradegate.Boot, AllowMigrations: true, RootKey: root, Target: fixture.Identities[0], InitialOperatorPublicKey: fixture.Signer.PrimaryPublic}
				booted, err := upgradegate.RunSignedGateProcessFixture(t, request, claim(0))
				if err != nil {
					t.Fatal(err)
				}
				db, err := store.Open(t.Context(), processStoreConfig(cfg), booted.Admission)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				kr, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: db}, bytes.Clone(root))
				if err != nil {
					t.Fatal(err)
				}
				populateProcessSource(t, db, kr)
				target := 1
				if scenario != "direct-sql-complete" {
					target = 2
				}
				applied := upgradecompat.InstalledSource{Identity: booted.State.Applied, Migrations: manifest, SchemaSHA256: schema}
				plan, err := fixture.Bundle.Plan(applied, fixture.Identities[target])
				if err != nil {
					t.Fatal(err)
				}
				identity, recipient, err := backup.GenerateIdentity()
				if err != nil {
					t.Fatal(err)
				}
				exported, err := (&service.Backup{DB: db, Options: backup.Options{Recipients: []string{recipient}}}).ExportUpgrade(t.Context(), t.TempDir(), plan, nil)
				if err != nil {
					t.Fatal(err)
				}
				receipt, err := os.ReadFile(exported.ReceiptPath)
				if err != nil {
					t.Fatal(err)
				}
				pinned, err := backupreceipt.PinCiphertext(t.Context(), exported.Path, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer pinned.Close()
				operator, err := backupreceipt.PinOperator(booted.State.InstanceID, fixture.Signer.PrimaryPublic)
				if err != nil {
					t.Fatal(err)
				}
				drill, err := app.DrillUpgrade(t.Context(), app.UpgradeDrillRequest{Scratch: processStoreConfig(upgradegate.GateStoreForProcessTest(t, engine)), Ciphertext: pinned, Receipt: receipt, Plan: plan, Operator: operator, Unlock: backup.Unlock{Identity: identity}, RootKey: bytes.Clone(root), Principal: domain.PrincipalID("usr_process"), Scope: domain.Scope{Org: "org_process", Project: "prj_process"}, Now: time.Now().UTC(), Lifetime: time.Hour})
				if err != nil {
					t.Fatal(err)
				}
				if !drill.HierarchyReadable || drill.SecretProof != "existing-secret-readable" || drill.CredentialProof != "reconciled-minted-revoked" {
					t.Fatal("populated route lacks actual recovery proof")
				}
				request.Target, request.Operator, request.Ciphertext = fixture.Identities[target], operator, pinned
				request.Evidence = backupreceipt.EvidenceMaterial{Receipt: receipt, Attestation: drill.Attestation, Signature: trustfixture.Sign(t, fixture.Signer.PrimarySigner, drill.Attestation)}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				current, boundary := 1, "sql-complete"
				if scenario == "multihop-first-healthy" {
					boundary = "healthy"
				}
				if scenario == "multihop-final-schema-applied" {
					if _, err := upgradegate.RunSignedGateProcessFixture(t, request, claim(1)); !errors.Is(err, upgradegate.ErrNextBinary) {
						t.Fatalf("first route hop did not retain maintenance: %v", err)
					}
					current, boundary = 2, "schema-applied"
				}
				upgradegate.KillSignedGateProcessFixture(t, request, claim(current), boundary)
				crashed, err := upgrade.InspectControl(t.Context(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				if !crashed.Maintenance || crashed.Generation != booted.State.Generation+1 || crashed.Pending.RouteDigest != plan.Digest() {
					t.Fatal("crash lost route/maintenance/generation")
				}
				if stale, err := store.Open(t.Context(), processStoreConfig(cfg), booted.Admission); err == nil {
					stale.Close()
					t.Fatal("source runtime admission survived crash maintenance")
				}
				if _, err := upgradegate.RunSignedGateProcessFixture(t, request, claim(0)); err == nil {
					t.Fatal("old exact build resumed newer route")
				}
				unchanged, err := upgrade.InspectControl(t.Context(), cfg)
				if err != nil || !reflect.DeepEqual(crashed, unchanged) {
					t.Fatal("refused old binary changed durable route", err)
				}
				resumed, err := upgradegate.RunSignedGateProcessFixture(t, request, claim(current))
				if scenario == "multihop-first-healthy" {
					if !errors.Is(err, upgradegate.ErrNextBinary) {
						t.Fatalf("completed intermediate binary became serving: %v", err)
					}
					resumed, err = upgradegate.RunSignedGateProcessFixture(t, request, claim(2))
				}
				if err != nil || !resumed.Admission.Valid() || resumed.State.Maintenance || resumed.State.Applied.Release != fixture.Identities[target] || resumed.State.Generation != crashed.Generation {
					t.Fatalf("route crash resume: %v", err)
				}
				final, err := store.Open(t.Context(), processStoreConfig(cfg), resumed.Admission)
				if err != nil {
					t.Fatal(err)
				}
				defer final.Close()
				keys, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: final}, bytes.Clone(root))
				if err != nil {
					t.Fatal(err)
				}
				readable, err := (&service.Backup{DB: final}).ProveValuesReadable(t.Context(), keys)
				if err != nil || !readable {
					t.Fatal("upgrade lost original protected value", err)
				}
				restart, err := upgradegate.RunSignedGateProcessFixture(t, request, claim(target))
				if err != nil || restart.State.Generation != resumed.State.Generation {
					t.Fatal("completed route restart consumed evidence twice", err)
				}
			})
		}
	}
}
