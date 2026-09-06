package upgradegate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	bundlefixture "github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestUpgradeConfigurationPreflightPreservesInstalledAuthority(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			request, claim, verify := signedFreshGate(t, engine)
			operator := trustfixture.New(t)
			request.StateDirectory = t.TempDir()
			if err := os.Chmod(request.StateDirectory, 0700); err != nil {
				t.Fatal(err)
			}
			request.InitialOperatorPublicKey = operator.PrimaryPublic
			request.Mode = Boot
			request.AllowMigrations = true
			if _, err := run(t.Context(), request, claim, upgrade.Production, verify); err != nil {
				t.Fatal(err)
			}
			installed, err := InspectCustodyDirectory(request.StateDirectory)
			if err != nil {
				t.Fatal(err)
			}
			// Removing the existing lock proves preflight does not recreate it.
			if err := os.Remove(filepath.Join(request.StateDirectory, ".operator-custody.lock")); err != nil {
				t.Fatal(err)
			}
			before, err := upgrade.InspectControl(t.Context(), request.Store)
			if err != nil {
				t.Fatal(err)
			}
			custody, err := os.ReadFile(filepath.Join(request.StateDirectory, operatorCustodyName))
			if err != nil {
				t.Fatal(err)
			}
			input := ConfigurationPreflight{Request: request, InstalledCustody: installed}
			proof, err := preflightConfiguration(t.Context(), input, claim, verify, true)
			if err != nil || len(proof.MaterialDigest) != 64 || len(proof.StateDigest) != 64 {
				t.Fatalf("preflight: %+v %v", proof, err)
			}
			repeated, err := preflightConfiguration(t.Context(), input, claim, verify, false)
			if err != nil || repeated != proof {
				t.Fatalf("startup material drift: %+v %v", repeated, err)
			}
			for _, tc := range []struct {
				name   string
				change func(*ConfigurationPreflight)
			}{
				{"unknown target", func(in *ConfigurationPreflight) { in.TargetManifest = strings.Repeat("a", 64) }},
				{"missing bundle", func(in *ConfigurationPreflight) { in.Request.BundleDirectory = filepath.Join(t.TempDir(), "absent") }},
				{"operator substitution", func(in *ConfigurationPreflight) {
					in.Request.InitialOperatorPublicKey = trustfixture.New(t).PrimaryPublic
				}},
				{"assertion without evidence", func(in *ConfigurationPreflight) { in.Request.LegacyWritersStopped = true }},
				{"copied custody", func(in *ConfigurationPreflight) {
					in.Request.StateDirectory = t.TempDir()
					if err := os.Chmod(in.Request.StateDirectory, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(in.Request.StateDirectory, operatorCustodyName), custody, 0600); err != nil {
						t.Fatal(err)
					}
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					changed := input
					tc.change(&changed)
					if _, err := preflightConfiguration(t.Context(), changed, claim, verify, true); err == nil {
						t.Fatal("accepted unsafe upgrade configuration")
					}
				})
			}
			if _, err := preflightConfiguration(t.Context(), input, append(bytes.Clone(claim), ' '), verify, true); err == nil {
				t.Fatal("accepted a different executing build")
			}
			after, err := upgrade.InspectControl(t.Context(), request.Store)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("preflight changed database authority")
			}
			afterCustody, err := os.ReadFile(filepath.Join(request.StateDirectory, operatorCustodyName))
			if err != nil || !bytes.Equal(custody, afterCustody) {
				t.Fatal("preflight changed operator custody")
			}
			entries, err := os.ReadDir(request.StateDirectory)
			if err != nil || len(entries) != 1 || entries[0].Name() != operatorCustodyName {
				t.Fatalf("preflight wrote custody artifacts: %v %v", entries, err)
			}
			if err := os.Remove(filepath.Join(request.StateDirectory, operatorCustodyName)); err != nil {
				t.Fatal(err)
			}
			if _, err := preflightConfiguration(t.Context(), input, claim, verify, true); err == nil {
				t.Fatal("recreated missing custody")
			}
			entries, err = os.ReadDir(request.StateDirectory)
			if err != nil || len(entries) != 0 {
				t.Fatal("missing custody was recreated")
			}
		})
	}
}

func TestUpgradeMaterialFingerprintSurvivesNextVerifiedBuildAndRecoveryFloor(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := gateConfig(t, engine)
			empty := releaseidentity.MigrationManifest{Engine: engine, Entries: []releaseidentity.Migration{}}
			inspected, err := upgrade.Inspect(t.Context(), cfg, empty)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := releaseidentity.BuildMigrationManifest(store.MigrationsFS, "migrations/"+string(engine), engine)
			if err != nil {
				t.Fatal(err)
			}
			schema := GateCurrentSchemaForTest(t, engine)
			source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.FreshGenesisV1}, Migrations: empty, SchemaSHA256: inspected.CatalogDigest}
			bundle := bundlefixture.Write(t, source, []bundlefixture.Target{{Version: "1.0.1", Sequence: 1, Commit: strings.Repeat("a", 40), Migrations: manifest, SchemaSHA256: schema}, {Version: "1.0.2", Sequence: 2, Commit: strings.Repeat("b", 40), Migrations: manifest, SchemaSHA256: schema}})
			claim := func(index int) []byte {
				raw, err := os.ReadFile(filepath.Join(bundle.Directory, "releases", string(bundle.Identities[index].ManifestSHA256), "upgrade-compatibility.json"))
				if err != nil {
					t.Fatal(err)
				}
				return raw
			}
			verify := func(index int) func(upgradecompat.VerifiedNode) error {
				return func(node upgradecompat.VerifiedNode) error {
					if !node.Valid() || node.Identity() != bundle.Identities[index] {
						return errors.New("different test build")
					}
					return nil
				}
			}
			prior, next := trustfixture.New(t), trustfixture.New(t)
			request := Request{Store: cfg, BundleDirectory: bundle.Directory, Pinned: bundle.Pinned, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(engine), Mode: Boot, AllowMigrations: true, RootKey: bytes.Repeat([]byte{37}, crypto.KeySize), StateDirectory: t.TempDir(), InitialOperatorPublicKey: prior.PrimaryPublic}
			if err := os.Chmod(request.StateDirectory, 0700); err != nil {
				t.Fatal(err)
			}
			initial, err := run(t.Context(), request, claim(0), upgrade.Production, verify(0))
			if err != nil {
				t.Fatal(err)
			}
			installed, err := InspectCustodyDirectory(request.StateDirectory)
			if err != nil {
				t.Fatal(err)
			}
			input := ConfigurationPreflight{Request: request, InstalledCustody: installed}
			oldProof, err := preflightConfiguration(t.Context(), input, claim(0), verify(0), true)
			if err != nil {
				t.Fatal(err)
			}
			nextProof, err := preflightConfiguration(t.Context(), input, claim(1), verify(1), false)
			if err != nil {
				t.Fatal(err)
			}
			if nextProof.MaterialDigest != oldProof.MaterialDigest || nextProof.StateDigest == oldProof.StateDigest {
				t.Fatal("executing-build identity leaked into durable artifact fingerprint")
			}
			// The advisory check cannot authorize the next image. Actual execution
			// still demands fresh operator-signed backup evidence before migration.
			if _, err := run(t.Context(), request, claim(1), upgrade.Production, verify(1)); err == nil {
				t.Fatal("preflight bypassed upgrade evidence")
			}
			rotation := rotationRequest(t, request, initial.State, prior, next, backupreceipt.PriorKeyRotation)
			rotated, err := RotateOperator(t.Context(), rotation)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := preflightConfiguration(t.Context(), input, claim(0), verify(0), false); err == nil {
				t.Fatal("old configured key silently followed operator rotation")
			}
			// Two real signed rotations return to the enrolled public key while
			// retaining the newer durable epoch floor and recovery incarnation.
			back := rotationRequest(t, request, rotated, next, prior, backupreceipt.PriorKeyRotation)
			recovered, err := RotateOperator(t.Context(), back)
			if err != nil {
				t.Fatal(err)
			}
			if recovered.RestoreEpoch <= initial.State.RestoreEpoch || !recovered.Pending.Invalidated {
				t.Fatal("fixture did not advance actual recovery authority")
			}
			recoveredProof, err := preflightConfiguration(t.Context(), input, claim(0), verify(0), false)
			if err != nil {
				t.Fatal(err)
			}
			if recoveredProof.MaterialDigest != oldProof.MaterialDigest || recoveredProof.StateDigest == oldProof.StateDigest {
				t.Fatal("mutable custody floor leaked into durable artifact fingerprint")
			}
			if _, err := preflightConfiguration(t.Context(), input, claim(0), verify(0), true); err == nil {
				t.Fatal("configuration Apply revived recovery-required installation")
			}
			if _, err := run(t.Context(), request, claim(0), upgrade.Production, verify(0)); err == nil {
				t.Fatal("preflight revived historical upgrade evidence")
			}
			indexPath := filepath.Join(bundle.Directory, "index.json")
			raw, err := os.ReadFile(indexPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(indexPath, append(raw, ' '), 0600); err != nil {
				t.Fatal(err)
			}
			changed, err := preflightConfiguration(t.Context(), input, claim(0), verify(0), false)
			if err != nil {
				t.Fatal(err)
			}
			if changed.MaterialDigest == oldProof.MaterialDigest {
				t.Fatal("changed bundle document bytes reused material proof")
			}
		})
	}
}
