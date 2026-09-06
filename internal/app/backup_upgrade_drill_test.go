package app

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
	"github.com/jackc/pgx/v5"
)

func upgradeDrillDatabase(t *testing.T, engine store.Engine) store.Config {
	t.Helper()
	cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "drill.db")}
	if engine == store.EngineSQLite {
		return cfg
	}
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires HIKYO_TEST_POSTGRES_DSN")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("hikyo_f4_drill_%d", time.Now().UnixNano())
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+ident); err != nil {
		admin.Close(t.Context())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+ident+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		admin.Close(cleanup)
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		t.Fatal("scratch fixture needs a PostgreSQL URL DSN")
	}
	parsed.Path = "/" + name
	cfg.Path = ""
	cfg.DSN = parsed.String()
	owned, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	var actual string
	checkErr := owned.QueryRow(t.Context(), "SELECT current_database()").Scan(&actual)
	closeErr := owned.Close(t.Context())
	if checkErr != nil || closeErr != nil || actual != name {
		t.Fatal("scratch DSN did not select the exclusively allocated database")
	}

	return cfg
}
func drillExec(t *testing.T, db *store.DB, query string, args ...any) {
	t.Helper()
	var err error
	if db.Engine() == store.EngineSQLite {
		_, err = db.SQLiteWrite().ExecContext(t.Context(), query, args...)
	} else {
		_, err = db.PG().Exec(t.Context(), query, args...)
	}
	if err != nil {
		t.Fatal(err)
	}
}

type upgradeDrillFixture struct {
	cfg      store.Config
	bundle   bundlefixture.Fixture
	request  UpgradeDrillRequest
	source   upgrade.InstalledSource
	proposal backupreceipt.LegacyProposal
	signer   *trustfixture.Fixture
	archive  string
	root     []byte
}

func newUpgradeDrillFixture(t *testing.T, engine store.Engine, secret, hierarchy bool) upgradeDrillFixture {
	t.Helper()
	cfg := upgradeDrillDatabase(t, engine)
	root, err := crypto.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { crypto.Zero(root) })
	authority := gatefixture.Prepare(t, upgrade.Config{Engine: releaseidentity.Engine(engine), Path: cfg.Path, DSN: cfg.DSN}, store.MigrationsFS, "migrations/"+string(engine), slices.Clone(root))
	db, err := store.Open(t.Context(), cfg, authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var kr *crypto.Keyring
	if hierarchy {
		kr, err = crypto.LoadKeyring(t.Context(), &keyring.Store{DB: db}, slices.Clone(root))
		if err != nil {
			t.Fatal(err)
		}
	}
	if secret {
		for _, query := range []string{
			`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_drill','Drill',TRUE,'{}','2026-09-05T00:00:00Z')`,
			`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_drill','org_drill','Drill','2026-09-05T00:00:00Z')`,
			`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_drill','org_drill','prj_drill','drill','','2026-09-05T00:00:00Z',0)`,
			`INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ('key_drill','org_drill','prj_drill','DRILL_SECRET','','secret','',FALSE,'','{}','none','none','2026-09-05T00:00:00Z')`,
			`INSERT INTO principals (id,kind,created_at) VALUES ('usr_drill','human','2026-09-05T00:00:00Z')`,
			`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_drill','usr_drill','manage-identities','org_drill','prj_drill',NULL,'2026-09-05T00:00:00Z')`,
			`INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_drill','gr_drill','manual','usr_drill','2026-09-05T00:00:00Z')`,
		} {
			drillExec(t, db, query)
		}
		sealer, err := kr.ForProject(t.Context(), "org_drill", "prj_drill")
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := sealer.SealValue(crypto.ValueAAD{OrgID: "org_drill", ProjectID: "prj_drill", EnvID: "env_drill", KeyID: "key_drill", RowID: "val_drill", FieldTag: "value"}, []byte("synthetic-never-output-secret"))
		if err != nil {
			t.Fatal(err)
		}
		drillExec(t, db, `INSERT INTO value_entries (id,org_id,project_id,environment_id,key_id,ciphertext,updated_at,updated_by) VALUES ('val_drill','org_drill','prj_drill','env_drill','key_drill',$1,'2026-09-05T00:00:00Z','usr_drill')`, sealed)
	}
	// Construct a historical pre-ledger fixture only after real runtime admission
	// and key writes. Removing the new control storage models the archive produced
	// by the old release; no runtime operation runs after this test-only surgery.
	if !hierarchy {
		drillExec(t, db, "DELETE FROM tier3_keys")
		drillExec(t, db, "DELETE FROM master_keys")
	}
	for _, table := range []string{"upgrade_pending", "upgrade_nonces", "upgrade_control"} {
		drillExec(t, db, "DROP TABLE "+table)
	}
	removePostLegacyAdditionsFixture(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	migrations, err := upgrade.PinnedLegacyManifest(releaseidentity.Engine(engine))
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := upgrade.InspectInstalled(t.Context(), upgrade.Config{Engine: releaseidentity.Engine(engine), Path: cfg.Path, DSN: cfg.DSN}, migrations)
	if err != nil {
		t.Fatal(err)
	}
	source := upgradecompat.InstalledSource{Identity: inspected.Source, Migrations: migrations, SchemaSHA256: inspected.SchemaDigest}
	bundle := bundlefixture.Write(t, source, []bundlefixture.Target{
		{Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), Migrations: migrations, SchemaSHA256: inspected.SchemaDigest},
		{Version: "1.1.0", Sequence: 2, Commit: strings.Repeat("b", 40), Migrations: migrations, SchemaSHA256: inspected.SchemaDigest},
	})
	bundle.Target = bundle.Identities[0]
	bundle.Plan, err = bundle.Bundle.Plan(source, bundle.Target)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := backupreceipt.NewLegacyProposal()
	if err != nil {
		t.Fatal(err)
	}
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	exported := exportDrillPreparation(t, cfg, backup.Options{Recipients: []string{recipient}}, bundle.Plan, &proposal)
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
	t.Cleanup(func() {
		if err := pinned.Close(); err != nil {
			t.Error(err)
		}
	})
	pin, err := backupreceipt.PinOperator(inspected.InstanceID, bundle.Signer.PrimaryPublic)
	if err != nil {
		t.Fatal(err)
	}
	request := UpgradeDrillRequest{Scratch: upgradeDrillDatabase(t, engine), Ciphertext: pinned, Receipt: receipt, Plan: bundle.Plan, Operator: pin, Unlock: backup.Unlock{Identity: identity}, RootKey: slices.Clone(root), Now: time.Now().UTC(), Lifetime: time.Hour}
	if secret {
		request.Principal = domain.PrincipalID("usr_drill")
		request.Scope = domain.Scope{Org: "org_drill", Project: "prj_drill"}
	}
	return upgradeDrillFixture{cfg: cfg, bundle: bundle, request: request, source: inspected, proposal: proposal, signer: bundle.Signer, archive: exported.Path, root: root}
}

// The runtime-created fixture includes migrations 45 through 50, while the
// sole admitted legacy genesis ends at 44. Model that historical archive by
// removing only the enumerated, pristine additions. Any recorded diagnostics,
// audit policy, privacy restriction, configuration, ceremony, adapter finding or contact email refuses
// removal. The subsequent pinned catalog inspection proves the exact legacy schema
// and migration digest;
// this test-only surgery adds no runtime downgrade capability.
func removePostLegacyAdditionsFixture(t *testing.T, db *store.DB) {
	t.Helper()
	engine := releaseidentity.Engine(db.Engine())
	legacy, err := upgrade.PinnedLegacyManifest(engine)
	if err != nil {
		t.Fatal(err)
	}
	current, err := releaseidentity.BuildMigrationManifest(store.MigrationsFS, "migrations/"+string(engine), engine)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Entries) != len(legacy.Entries)+6 || !slices.Equal(current.Entries[:len(legacy.Entries)], legacy.Entries) {
		t.Fatal("legacy drill fixture requires the immutable migration prefix plus migrations 45 through 50 only")
	}
	for i, version := range []uint64{45, 46, 47, 48, 49, 50} {
		if current.Entries[len(legacy.Entries)+i].Version != version {
			t.Fatal("legacy drill fixture has an unreviewed post-legacy migration")
		}
	}
	const pristine = `SELECT COUNT(*) FROM ops_diagnostics WHERE singleton=1 AND escrow_verified_at IS NULL AND escrow_instance_id='' AND escrow_incarnation='' AND escrow_root_epoch=0 AND last_reencrypt_success IS NULL`
	var untouched int
	if db.Engine() == store.EngineSQLite {
		err = db.SQLiteRead().QueryRowContext(t.Context(), pristine).Scan(&untouched)
	} else {
		err = db.PG().QueryRow(t.Context(), pristine).Scan(&untouched)
	}
	if err != nil || untouched != 1 {
		t.Fatal("legacy drill fixture cannot discard diagnostic evidence", err)
	}
	for _, query := range []string{
		"SELECT COUNT(*) FROM audit_retention_policy",
		"SELECT COUNT(*) FROM principals WHERE privacy_state <> 'active'",
		"SELECT COUNT(*) FROM self_config_binding",
		"SELECT COUNT(*) FROM self_config_jobs",
		"SELECT COUNT(*) FROM self_config_nodes",
		"SELECT COUNT(*) FROM self_config_retention",
		"SELECT COUNT(*) FROM self_config_seed_attestations",
		"SELECT COUNT(*) FROM cli_reauth_handoffs",
		"SELECT COUNT(*) FROM adapter_effects WHERE finding <> ''",
		"SELECT COUNT(*) FROM accounts WHERE email <> ''",
	} {
		var evidence int
		if db.Engine() == store.EngineSQLite {
			err = db.SQLiteRead().QueryRowContext(t.Context(), query).Scan(&evidence)
		} else {
			err = db.PG().QueryRow(t.Context(), query).Scan(&evidence)
		}
		if err != nil || evidence != 0 {
			t.Fatal("legacy drill fixture cannot discard policy, privacy, configuration, ceremony, adapter finding or contact email evidence", err)
		}
	}
	if db.Engine() == store.EngineSQLite {
		// Recreate the empty table from its immutable legacy declaration so
		// compatibility inspection still compares the actual legacy schema.
		migration, err := store.MigrationsFS.ReadFile("migrations/sqlite/00032_cli_reauth_disclosure.sql")
		if err != nil {
			t.Fatal(err)
		}
		_, declaration, ok := strings.Cut(string(migration), "CREATE TABLE cli_reauth_handoffs_new")
		if !ok {
			t.Fatal("missing legacy handoff declaration")
		}
		declaration, _, ok = strings.Cut(declaration, ";")
		if !ok {
			t.Fatal("unterminated legacy handoff declaration")
		}
		drillExec(t, db, "DROP TABLE cli_reauth_handoffs")
		drillExec(t, db, "CREATE TABLE cli_reauth_handoffs_new"+declaration)
		drillExec(t, db, "ALTER TABLE cli_reauth_handoffs_new RENAME TO cli_reauth_handoffs")
	} else {
		drillExec(t, db, "ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_operation_check")
		drillExec(t, db, "ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_operation_check CHECK (operation IN ('adapter.configure','adapter.credential-set','adapter.adopt','adapter.sync','value.reveal','value.copy-source'))")
		drillExec(t, db, "ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_purpose_check")
		drillExec(t, db, "ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_purpose_check CHECK (purpose IN ('adapter','reveal','copy'))")
	}
	for _, query := range []string{
		"DROP TABLE self_config_retention",
		"DROP TABLE self_config_nodes",
		"DROP TABLE self_config_jobs",
		"DROP TABLE self_config_binding",
		"DROP TABLE self_config_seed_attestations",
		"ALTER TABLE accounts DROP COLUMN email",
		"ALTER TABLE adapter_effects DROP COLUMN finding",
		"ALTER TABLE principals DROP COLUMN privacy_state",
		"DROP INDEX audit_tenant_retention_time",
		"DROP INDEX audit_tenant_retention_unit",
		"DROP INDEX audit_instance_retention_time",
		"DROP INDEX audit_instance_retention_unit",
		"DROP TABLE audit_retention_policy",
		"DROP TABLE ops_diagnostics",
		"DELETE FROM goose_db_version WHERE version_id IN (45,46,47,48,49,50)",
	} {
		drillExec(t, db, query)
	}
}

func TestUpgradeDrillActualBothEngineRecoveryAndConfigOnlyEscrow(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		for _, secret := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/secret=%t", engine, secret), func(t *testing.T) {
				f := newUpgradeDrillFixture(t, engine, secret, true)
				result, err := DrillUpgrade(t.Context(), f.request)
				if err != nil {
					t.Fatal(err)
				}
				if !result.HierarchyReadable || !bytes.Equal(f.request.RootKey, make([]byte, crypto.KeySize)) {
					t.Fatal("root escrow not proved and consumed")
				}
				want := "authoritatively-no-secret"
				if secret {
					want = "existing-secret-readable"
				}
				if result.SecretProof != want {
					t.Fatal("incorrect secret proof disposition")
				}
				if secret && result.CredentialProof != "reconciled-minted-revoked" {
					t.Fatal("actual recovery credential lifecycle missing")
				}
				a, err := backupreceipt.ParseAttestation(result.Attestation)
				if err != nil {
					t.Fatal(err)
				}
				if a.RecoveryIncarnation != f.proposal.RecoveryIncarnation || a.RestoreEpoch != f.source.RestoreEpoch || a.Authority != backupreceipt.LegacyProposalAuthority {
					t.Fatal("scratch recovery replaced original proposed authority")
				}
				material := backupreceipt.EvidenceMaterial{Receipt: f.request.Receipt, Attestation: result.Attestation, Signature: trustfixture.Sign(t, f.signer.PrimarySigner, result.Attestation)}
				evidence, err := backupreceipt.VerifyLegacyEvidence(t.Context(), f.request.Operator, f.request.Plan, f.request.Ciphertext, material, backupreceipt.LegacyInspection{InstanceID: f.source.InstanceID, Engine: releaseidentity.Engine(engine), SchemaSHA256: f.source.SchemaDigest, MigrationSHA256: f.source.MigrationDigest, RestoreEpoch: f.source.RestoreEpoch}, f.proposal, time.Now())
				if err != nil || !evidence.Valid() {
					t.Fatal("actual drill attestation failed public gate", err)
				}
			})
		}
	}
}

func TestUpgradeDrillRefusesMissingOrWrongPrivateCustody(t *testing.T) {
	for _, name := range []string{"missing hierarchy", "wrong root", "wrong age", "changed receipt", "changed original path"} {
		t.Run(name, func(t *testing.T) {
			f := newUpgradeDrillFixture(t, store.EngineSQLite, false, name != "missing hierarchy")
			switch name {
			case "wrong root":
				f.request.RootKey = bytes.Repeat([]byte{9}, crypto.KeySize)
			case "wrong age":
				identity, _, err := backup.GenerateIdentity()
				if err != nil {
					t.Fatal(err)
				}
				f.request.Unlock = backup.Unlock{Identity: identity}
			case "changed receipt":
				r, err := backupreceipt.ParseReceipt(f.request.Receipt)
				if err != nil {
					t.Fatal(err)
				}
				r.ManifestSHA256 = releaseidentity.Hash([]byte("other manifest"))
				f.request.Receipt = trustfixture.JSON(t, r)
			case "changed original path":
				if err := os.WriteFile(f.archive, []byte("replaced mutable original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := DrillUpgrade(t.Context(), f.request)
			if name == "changed original path" {
				if err != nil || !result.HierarchyReadable {
					t.Fatal("owned pinned ciphertext followed replaced original path", err)
				}
				return
			}
			if err == nil || len(result.Attestation) != 0 || result.HierarchyReadable {
				t.Fatal("unproved custody produced an attestation")
			}
		})
	}
}

func TestUpgradeDrillAppliedLedgerKeepsSourceAuthorityAndRotatesScratch(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			f := newUpgradeDrillFixture(t, engine, true, true)
			initial, err := DrillUpgrade(t.Context(), f.request)
			if err != nil {
				t.Fatal(err)
			}
			material := backupreceipt.EvidenceMaterial{Receipt: f.request.Receipt, Attestation: initial.Attestation, Signature: trustfixture.Sign(t, f.signer.PrimarySigner, initial.Attestation)}
			evidence, err := backupreceipt.VerifyLegacyEvidence(t.Context(), f.request.Operator, f.request.Plan, f.request.Ciphertext, material, backupreceipt.LegacyInspection{InstanceID: f.source.InstanceID, Engine: releaseidentity.Engine(engine), SchemaSHA256: f.source.SchemaDigest, MigrationSHA256: f.source.MigrationDigest, RestoreEpoch: f.source.RestoreEpoch}, f.proposal, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			statement := evidence.Statement()
			var incarnation, nonce upgrade.Incarnation
			if err := incarnation.UnmarshalText([]byte(statement.RecoveryIncarnation)); err != nil {
				t.Fatal(err)
			}
			if err := nonce.UnmarshalText([]byte(statement.Nonce)); err != nil {
				t.Fatal(err)
			}
			manifest, err := f.request.Plan.SourceManifest(releaseidentity.Engine(engine))
			if err != nil {
				t.Fatal(err)
			}
			acceptance := upgrade.Acceptance{Floor: f.bundle.Bundle.Snapshot().Floor(), ReleaseRootDigest: releaseidentity.Hash(f.bundle.Pinned.Root), Attestation: &upgrade.AttestationUse{Authority: string(statement.Authority), Nonce: nonce, EvidenceDigest: evidence.Digest(), OperatorKeyID: statement.OperatorKeyID, InstanceID: statement.InstanceID, RestoreEpoch: statement.RestoreEpoch, RecoveryIncarnation: incarnation, RouteGeneration: statement.RouteGeneration, RouteDigest: statement.RouteSHA256, IssuedAt: statement.IssuedAt, ExpiresAt: statement.ExpiresAt}}
			operation := upgrade.Operation{Kind: upgrade.UpgradeOperation, Source: f.source.Source, RouteSource: f.source.Source, Target: f.request.Plan.Target(), SourceMigrationDigest: f.source.MigrationDigest, TargetMigrationDigest: f.source.MigrationDigest, SourceSchemaDigest: f.source.SchemaDigest, TargetSchemaDigest: f.source.SchemaDigest, RouteDigest: f.request.Plan.Digest(), RouteLength: 1, Generation: 1, RecoveryIncarnation: incarnation, BackupID: string(evidence.Receipt().Snapshot.BackupID), Phase: upgrade.Prepared, Acceptance: acceptance}
			var healthy upgrade.State
			err = upgrade.WithLock(t.Context(), upgrade.Config{Engine: releaseidentity.Engine(engine), Path: f.cfg.Path, DSN: f.cfg.DSN}, func(session *upgrade.Session) error {
				state, err := session.Bootstrap(t.Context(), manifest, operation, upgrade.Production)
				if err != nil {
					return err
				}
				// This signed fixture release changes no domain SQL. The actual source
				// catalog and complete migration bytes are identical at the next release.
				for _, phase := range []upgrade.Phase{upgrade.SchemaWriteStarted, upgrade.SchemaApplied, upgrade.Healthy} {
					state, err = session.Advance(t.Context(), state, phase)
					if err != nil {
						return err
					}
				}
				healthy = state
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			inspected, err := upgrade.InspectInstalled(t.Context(), upgrade.Config{Engine: releaseidentity.Engine(engine), Path: f.cfg.Path, DSN: f.cfg.DSN}, manifest)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := f.bundle.Bundle.Plan(upgradecompat.InstalledSource{Identity: inspected.Source, Migrations: manifest, SchemaSHA256: inspected.SchemaDigest}, f.bundle.Identities[1])
			if err != nil {
				t.Fatal(err)
			}
			identity, recipient, err := backup.GenerateIdentity()
			if err != nil {
				t.Fatal(err)
			}
			exported := exportDrillPreparation(t, f.cfg, backup.Options{Recipients: []string{recipient}}, plan, nil)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(exported.ReceiptPath)
			if err != nil {
				t.Fatal(err)
			}
			pinned, err := backupreceipt.PinCiphertext(t.Context(), exported.Path, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer pinned.Close()
			request := f.request
			request.Scratch = upgradeDrillDatabase(t, engine)
			request.Ciphertext = pinned
			request.Receipt = raw
			request.Plan = plan
			request.Unlock = backup.Unlock{Identity: identity}
			request.RootKey = slices.Clone(f.root)
			request.Now = time.Now().UTC()
			result, err := DrillUpgrade(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if result.SecretProof != "existing-secret-readable" || result.CredentialProof != "reconciled-minted-revoked" {
				t.Fatal("applied-ledger real recovery lifecycle incomplete")
			}
			material = backupreceipt.EvidenceMaterial{Receipt: raw, Attestation: result.Attestation, Signature: trustfixture.Sign(t, f.signer.PrimarySigner, result.Attestation)}
			originalIncarnation, err := healthy.RecoveryIncarnation.MarshalText()
			if err != nil {
				t.Fatal(err)
			}
			live := backupreceipt.LiveSource{InstanceID: healthy.InstanceID, Engine: releaseidentity.Engine(engine), Source: healthy.Applied, SourceSchemaSHA256: healthy.SchemaDigest, MigrationSHA256: healthy.MigrationDigest, RestoreEpoch: healthy.RestoreEpoch, RecoveryIncarnation: backupreceipt.Nonce(originalIncarnation), Generation: healthy.Generation}
			verified, err := backupreceipt.VerifyEvidence(t.Context(), request.Operator, plan, pinned, material, live, time.Now())
			if err != nil || !verified.Valid() {
				t.Fatal("ledger drill failed original-source public verifier", err)
			}
			restored, err := upgrade.InspectInstalled(t.Context(), upgrade.Config{Engine: releaseidentity.Engine(engine), Path: request.Scratch.Path, DSN: request.Scratch.DSN}, manifest)
			if err != nil {
				t.Fatal(err)
			}
			if restored.Ledger == nil || restored.Ledger.RecoveryIncarnation == healthy.RecoveryIncarnation || restored.RestoreEpoch <= healthy.RestoreEpoch || restored.Ledger.Pending == nil || !restored.Ledger.Pending.Invalidated {
				t.Fatal("scratch retained reusable original authority")
			}
			live.RestoreEpoch = restored.RestoreEpoch
			if _, err := backupreceipt.VerifyEvidence(t.Context(), request.Operator, plan, pinned, material, live, time.Now()); err == nil {
				t.Fatal("original attestation accepted against restored authority")
			}
		})
	}
}

func exportDrillPreparation(t *testing.T, cfg store.Config, options backup.Options, plan upgradecompat.Plan, proposal *backupreceipt.LegacyProposal) service.ExportResult {
	t.Helper()
	var out service.ExportResult
	err := upgrade.WithLock(t.Context(), upgrade.Config{Engine: releaseidentity.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}, func(session *upgrade.Session) error {
		authority, err := session.PrepareExport(t.Context(), plan)
		if err != nil {
			return err
		}
		db, err := store.OpenPreparation(t.Context(), cfg, authority)
		if err != nil {
			return err
		}
		defer db.Close()
		out, err = service.ExportPreparedUpgrade(t.Context(), db, options, t.TempDir(), plan, proposal)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
