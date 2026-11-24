package upgradegate_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// TestRecoveryBridgeNeedsAttestationAndThenSucceeds is the positive control for
// the refusal tests: the same valid, throwaway-signed bridge route, on a populated
// installation, is refused without the operator's backup receipt and attestation
// and runs to the target release with them. The only difference between the two
// runs is the evidence. The gate raises no attestation-specific error: the
// missing evidence surfaces as a generic invalid-artifact refusal.
func TestRecoveryBridgeNeedsAttestationAndThenSucceeds(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := upgradegate.GateStoreForProcessTest(t, engine)
			empty := releaseidentity.MigrationManifest{Engine: engine, Entries: []releaseidentity.Migration{}}
			inspected, err := upgrade.Inspect(t.Context(), cfg, empty)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := releaseidentity.BuildMigrationManifest(store.MigrationsFS, "migrations/"+string(engine), engine)
			if err != nil {
				t.Fatal(err)
			}
			schema := upgradegate.GateCurrentSchemaForTest(t, engine)
			declare := func(version string, sequence uint64, commit string, sources []upgradecompat.SourceEdge) []byte {
				return trustfixture.JSON(t, upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: version, Sequence: sequence, Commit: commit,
					Engines: []upgradecompat.EngineDeclaration{{Migrations: manifest.Clone(), SchemaSHA256: schema, Sources: sources}}})
			}
			f := trustfixture.New(t)
			genesis := releaseidentity.Source{Genesis: releaseidentity.FreshGenesisV1}
			a := f.AddStable(t, "1.0.1", 1, strings.Repeat("a", 40), declare("1.0.1", 1, strings.Repeat("a", 40), []upgradecompat.SourceEdge{{Source: genesis, Migrations: empty.Clone(), SchemaSHA256: inspected.CatalogDigest, Mode: upgradecompat.Maintenance}}))
			b := f.AddStable(t, "1.1.0", 2, strings.Repeat("b", 40), declare("1.1.0", 2, strings.Repeat("b", 40), []upgradecompat.SourceEdge{}))
			root := bytes.Repeat([]byte{37}, crypto.KeySize)
			request := upgradegate.Request{Store: cfg, BundleDirectory: upgradegate.WriteBridgeBundleForTest(t, f, []trustfixture.SignedRelease{a}, nil), Pinned: f.Pinned, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(engine), Mode: upgradegate.Boot, AllowMigrations: true, RootKey: root, Target: a.Identity, InitialOperatorPublicKey: f.PrimaryPublic}
			booted, err := upgradegate.RunSignedGateProcessFixture(t, request, a.Material.Compatibility)
			if err != nil {
				t.Fatal(err)
			}
			db, err := store.Open(t.Context(), processStoreConfig(cfg), booted.Admission)
			if err != nil {
				t.Fatal(err)
			}
			keys, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: db}, bytes.Clone(root))
			if err != nil {
				t.Fatal(err)
			}
			populateProcessSource(t, db, keys)

			snapshot := f.Snapshot(t)
			verifiedA, err := releasetrust.VerifyStable(snapshot, a.Material)
			if err != nil {
				t.Fatal(err)
			}
			verifiedB, err := releasetrust.VerifyStable(snapshot, b.Material)
			if err != nil {
				t.Fatal(err)
			}
			raw := trustfixture.JSON(t, releasetrust.BridgeStatement{Schema: "hikyo.dev/recovery-bridge/v1", Source: a.Identity, Target: b.Identity,
				SourcePolicySHA256: verifiedA.PolicyDigest(), TargetPolicySHA256: verifiedB.PolicyDigest(),
				SourceMigrations: manifest.Clone(), TargetMigrations: manifest.Clone(), SourceSchemaSHA256: schema, TargetSchemaSHA256: schema, Mode: "maintenance"})
			f.Catalog.Bridges = append(f.Catalog.Bridges, releaseidentity.Hash(raw))
			f.Catalog.Sequence++
			request.BundleDirectory = upgradegate.WriteBridgeBundleForTest(t, f, []trustfixture.SignedRelease{a, b}, []releasetrust.BridgeMaterial{{Statement: raw, Signature: trustfixture.Sign(t, f.RecoverySigner, raw)}})
			bundle, err := upgradebundle.Load(t.Context(), request.BundleDirectory, f.Pinned, releaseidentity.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := bundle.Plan(upgradecompat.InstalledSource{Identity: booted.State.Applied, Migrations: manifest, SchemaSHA256: schema}, b.Identity)
			if err != nil {
				t.Fatal(err)
			}
			if steps := plan.Steps(); len(steps) != 1 || steps[0].BridgeSHA256 != releaseidentity.Hash(raw) || !plan.RequiresOperatorAttestation() {
				t.Fatalf("the route must be the one attested bridge step, got %+v", steps)
			}

			// The operator's evidence: backup, restore drill and a signed attestation.
			identity, recipient, err := backup.GenerateIdentity()
			if err != nil {
				t.Fatal(err)
			}
			operator, err := backupreceipt.PinOperator(booted.State.InstanceID, f.PrimaryPublic)
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
			defer func() { _ = pinned.Close() }()
			drill, err := app.DrillUpgrade(t.Context(), app.UpgradeDrillRequest{Scratch: processStoreConfig(upgradegate.GateStoreForProcessTest(t, engine)), Ciphertext: pinned, Receipt: receipt, Plan: plan, Operator: operator, Unlock: backup.Unlock{Identity: identity}, RootKey: bytes.Clone(root), Principal: domain.PrincipalID("usr_process"), Scope: domain.Scope{Org: "org_process", Project: "prj_process"}, Now: time.Now().UTC(), Lifetime: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := upgrade.InspectControl(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}

			request.Target, request.Operator, request.Ciphertext = b.Identity, operator, pinned
			// Control: identical request, no operator evidence.
			if _, err := upgradegate.RunSignedGateProcessFixture(t, request, b.Material.Compatibility); err == nil {
				t.Fatal("the bridge route ran without the operator's receipt and attestation")
			} else {
				t.Logf("without evidence: %v", err)
			}
			after, err := upgrade.InspectControl(t.Context(), cfg)
			if err != nil || !after.Equal(before) {
				t.Fatalf("the refusal changed the installation: %v", err)
			}
			// Same request with the evidence: the bridge route runs to the target.
			request.Evidence = backupreceipt.EvidenceMaterial{Receipt: receipt, Attestation: drill.Attestation, Signature: trustfixture.Sign(t, f.PrimarySigner, drill.Attestation)}
			result, err := upgradegate.RunSignedGateProcessFixture(t, request, b.Material.Compatibility)
			if err != nil || !result.Admission.Valid() || result.State.Maintenance || result.State.Applied.Release != b.Identity {
				t.Fatalf("with the attestation the bridge route must reach the target: %v %+v", err, result.State.Applied)
			}
		})
	}
}
