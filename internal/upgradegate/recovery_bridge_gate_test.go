package upgradegate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// writeBridgeBundle writes the offline bundle layout for the given releases and
// bridge proofs, signed by the fixture's throwaway keys.
func writeBridgeBundle(t *testing.T, f *trustfixture.Fixture, releases []trustfixture.SignedRelease, bridges []releasetrust.BridgeMaterial) string {
	t.Helper()
	material := f.Material(t)
	index := upgradebundle.Index{Format: upgradebundle.IndexFormat, PrimaryKeyIDs: []string{"test-primary"}, Releases: []upgradebundle.ReleaseEntry{}, Bridges: []releaseidentity.Digest{}}
	directory := t.TempDir()
	write := func(name string, raw []byte) {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, release := range releases {
		index.Releases = append(index.Releases, upgradebundle.ReleaseEntry{Profile: releaseidentity.StableV1, ManifestSHA256: release.Identity.ManifestSHA256})
		root := "releases/" + string(release.Identity.ManifestSHA256) + "/"
		for name, raw := range map[string][]byte{"manifest.json": release.Material.Manifest, "manifest.sigstore.json": release.Material.ManifestSignature, "release-candidate.json": release.Material.Candidate, "upgrade-compatibility.json": release.Material.Compatibility} {
			write(root+name, raw)
		}
		for name, raw := range release.Payloads {
			write(root+name, raw)
		}
	}
	for _, bridge := range bridges {
		digest := releaseidentity.Hash(bridge.Statement)
		index.Bridges = append(index.Bridges, digest)
		write("bridges/"+string(digest)+"/statement.json", bridge.Statement)
		write("bridges/"+string(digest)+"/statement.sigstore.json", bridge.Signature)
	}
	for name, raw := range map[string][]byte{"index.json": trustfixture.JSON(t, index), "keys/test-primary.pub": f.PrimaryPublic, "metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature, "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature} {
		write(name, raw)
	}
	return directory
}

// TestRecoveryBridgeRefusalsLeaveTheInstallationUnchanged installs a release
// through the real gate on each engine, then offers a route that exists only as
// a recovery bridge. Every refusal must leave the control row (applied release,
// generation, floor) and the inspected schema exactly as they were.
// Postgres runs when HIKYO_TEST_POSTGRES_DSN is set (CI uses PostgreSQL 18).
func TestRecoveryBridgeRefusalsLeaveTheInstallationUnchanged(t *testing.T) {
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
			declare := func(version string, sequence uint64, commit string, sources []upgradecompat.SourceEdge) []byte {
				return trustfixture.JSON(t, upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: version, Sequence: sequence, Commit: commit,
					Engines: []upgradecompat.EngineDeclaration{{Migrations: manifest.Clone(), SchemaSHA256: schema, Sources: sources}}})
			}
			f := trustfixture.New(t)
			from := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.FreshGenesisV1}, Migrations: empty, SchemaSHA256: inspected.CatalogDigest}
			a := f.AddStable(t, "1.0.1", 1, strings.Repeat("a", 40), declare("1.0.1", 1, strings.Repeat("a", 40), []upgradecompat.SourceEdge{{Source: from.Identity, Migrations: empty.Clone(), SchemaSHA256: from.SchemaSHA256, Mode: upgradecompat.Maintenance}}))
			b := f.AddStable(t, "1.1.0", 2, strings.Repeat("b", 40), declare("1.1.0", 2, strings.Repeat("b", 40), []upgradecompat.SourceEdge{}))
			verifyFor := func(want releaseidentity.Identity) func(upgradecompat.VerifiedNode) error {
				return func(node upgradecompat.VerifiedNode) error {
					if !node.Valid() || node.Identity() != want {
						return errors.New("different test build")
					}
					return nil
				}
			}
			request := Request{Store: cfg, BundleDirectory: writeBridgeBundle(t, f, []trustfixture.SignedRelease{a}, nil), Pinned: f.Pinned, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(engine), Mode: Migrate, RootKey: bytes.Repeat([]byte{37}, crypto.KeySize)}
			if _, err := run(t.Context(), request, a.Material.Compatibility, upgrade.Production, verifyFor(a.Identity)); err != nil {
				t.Fatal(err)
			}
			request.Mode = Boot
			if _, err := run(t.Context(), request, a.Material.Compatibility, upgrade.Production, verifyFor(a.Identity)); err != nil {
				t.Fatalf("install the source release: %v", err)
			}
			wantControl, err := upgrade.InspectControl(t.Context(), cfg)
			if err != nil || wantControl.Applied.Release != a.Identity || wantControl.Maintenance {
				t.Fatalf("source release must be installed and healthy: %+v %v", wantControl, err)
			}

			// The statement is the exact source and target the gate would route between.
			snap := f.Snapshot(t)
			verifiedA, err := releasetrust.VerifyStable(snap, a.Material)
			if err != nil {
				t.Fatal(err)
			}
			verifiedB, err := releasetrust.VerifyStable(snap, b.Material)
			if err != nil {
				t.Fatal(err)
			}
			statement := releasetrust.BridgeStatement{Schema: "hikyo.dev/recovery-bridge/v1", Source: a.Identity, Target: b.Identity,
				SourcePolicySHA256: verifiedA.PolicyDigest(), TargetPolicySHA256: verifiedB.PolicyDigest(),
				SourceMigrations: manifest.Clone(), TargetMigrations: manifest.Clone(), SourceSchemaSHA256: schema, TargetSchemaSHA256: schema, Mode: "maintenance"}

			attempt := func(name string, bundleDirectory string, wantErr string) {
				t.Helper()
				migrate := request
				migrate.Mode, migrate.BundleDirectory = Migrate, bundleDirectory
				_, err := run(t.Context(), migrate, b.Material.Compatibility, upgrade.Production, verifyFor(b.Identity))
				if err == nil || (wantErr != "" && !strings.Contains(err.Error(), wantErr)) {
					t.Fatalf("%s: want refusal %q, got %v", name, wantErr, err)
				}
				control, err := upgrade.InspectControl(t.Context(), cfg)
				if err != nil || !control.Equal(wantControl) {
					t.Fatalf("%s: refusal changed the installation\nbefore %+v\nafter  %+v", name, wantControl, control)
				}
			}

			// No bridge: the planner never invents a route.
			attempt("no bridge", writeBridgeBundle(t, f, []trustfixture.SignedRelease{a, b}, nil), "no authenticated upgrade route")

			// A catalogued bridge signed by a different throwaway key fails verification.
			forged := trustfixture.New(t)
			raw := trustfixture.JSON(t, statement)
			f.Catalog.Bridges = append(f.Catalog.Bridges, releaseidentity.Hash(raw))
			f.Catalog.Sequence++
			attempt("bridge signed by another key", writeBridgeBundle(t, f, []trustfixture.SignedRelease{a, b}, []releasetrust.BridgeMaterial{{Statement: raw, Signature: trustfixture.Sign(t, forged.RecoverySigner, raw)}}), "signature verification failed")

			// A properly signed bridge plans a route, but it requires the operator
			// attestation evidence, which this request does not carry. The route is past
			// "no route" (the bridge was accepted) and still nothing is written.
			good := releasetrust.BridgeMaterial{Statement: raw, Signature: trustfixture.Sign(t, f.RecoverySigner, raw)}
			attempt("bridge without operator attestation", writeBridgeBundle(t, f, []trustfixture.SignedRelease{a, b}, []releasetrust.BridgeMaterial{good}), "invalid closed upgrade artifact")
		})
	}
}

// WriteBridgeBundleForTest lets the external acceptance package build the same
// throwaway-signed bridge bundles.
func WriteBridgeBundleForTest(t *testing.T, f *trustfixture.Fixture, releases []trustfixture.SignedRelease, bridges []releasetrust.BridgeMaterial) string {
	return writeBridgeBundle(t, f, releases, bridges)
}
