package upgradegate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
// a recovery bridge. Every refusal must leave the control state (applied release,
// schema and migration digests, generation, floor, pending operation) exactly as
// it was.
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

			// An explicit no-op: the check is configured and nothing is revoked.
			lookups := 0
			request.BridgeRevocation = func(context.Context, releasetrust.Snapshot) ([]releaseidentity.Digest, error) {
				lookups++
				return nil, nil
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
			statement := releasetrust.BridgeStatement{Schema: releasetrust.BridgeSchemaV2, NotAfter: "2099-01-01T00:00:00Z", Source: a.Identity, Target: b.Identity,
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

			bridge := func(st releasetrust.BridgeStatement) []byte { return trustfixture.JSON(t, st) }
			catalogue := func(raw []byte) {
				f.Catalog.Bridges, f.Catalog.Sequence = []releaseidentity.Digest{releaseidentity.Hash(raw)}, f.Catalog.Sequence+1
			}
			signed := func(raw []byte) releasetrust.BridgeMaterial {
				return releasetrust.BridgeMaterial{Statement: raw, Signature: trustfixture.Sign(t, f.RecoverySigner, raw)}
			}
			bundleWith := func(m ...releasetrust.BridgeMaterial) string {
				return writeBridgeBundle(t, f, []trustfixture.SignedRelease{a, b}, m)
			}
			raw := bridge(statement)

			// No bridge: the planner never invents a route.
			attempt("no bridge", bundleWith(), "no authenticated upgrade route")
			if lookups != 0 {
				t.Fatalf("a route without a bridge must not consult the revocation list (%d lookups)", lookups)
			}

			// A statement the catalog does not list is refused at the offline index.
			attempt("bridge not in the catalog", bundleWith(signed(raw)), "omits or substitutes")

			// A statement edited after signing no longer matches the catalogued digest.
			catalogue(raw)
			edited := statement
			edited.TargetSchemaSHA256 = releaseidentity.Hash([]byte("edited"))
			attempt("statement edited after signing", bundleWith(releasetrust.BridgeMaterial{Statement: bridge(edited), Signature: signed(raw).Signature}), "omits or substitutes")

			// A catalogued bridge signed by a different throwaway key fails verification.
			forged := trustfixture.New(t)
			attempt("bridge signed by another key", bundleWith(releasetrust.BridgeMaterial{Statement: raw, Signature: trustfixture.Sign(t, forged.RecoverySigner, raw)}), "signature verification failed")

			// Validly signed and catalogued, but for the other engine: no route for this database.
			other := releaseidentity.SQLite
			if engine == releaseidentity.SQLite {
				other = releaseidentity.Postgres
			}
			mismatched := statement
			mismatched.SourceMigrations.Engine, mismatched.TargetMigrations.Engine = other, other
			rawMismatch := bridge(mismatched)
			catalogue(rawMismatch)
			attempt("bridge for another engine", bundleWith(signed(rawMismatch)), "no authenticated upgrade route")

			// Validly signed and catalogued, but its source is not the installed release.
			wrongSource := statement
			wrongSource.Source.Version = "0.9.9"
			rawWrong := bridge(wrongSource)
			catalogue(rawWrong)
			attempt("bridge from a different source version", bundleWith(signed(rawWrong)), "no authenticated upgrade route")

			// A properly signed, catalogued bridge plans a route and is refused later,
			// at the operator evidence; nothing is written. TestRecoveryBridgeNeedsAttestation
			// AndThenSucceeds shows the same route succeeds once the evidence is supplied.
			catalogue(raw)
			attempt("bridge without operator evidence", bundleWith(signed(raw)), "invalid closed upgrade artifact")

			// v2 statements carry a signed expiry. The live one is the control: it
			// plans and is refused only at the operator evidence, like the v1 case.
			live := statement
			live.Schema, live.NotAfter = releasetrust.BridgeSchemaV2, time.Now().UTC().Truncate(time.Second).Add(24*time.Hour).Format(time.RFC3339)
			rawLive := bridge(live)
			catalogue(rawLive)
			attempt("live v2 bridge without operator evidence", bundleWith(signed(rawLive)), "invalid closed upgrade artifact")
			if lookups == 0 {
				t.Fatal("a route through a bridge must consult the revocation list")
			}

			expired := live
			expired.NotAfter = "2020-01-01T00:00:00Z"
			rawExpired := bridge(expired)
			catalogue(rawExpired)
			attempt("expired v2 bridge", bundleWith(signed(rawExpired)), "no authenticated upgrade route")

			// A verified revocation list withdraws the live bridge; a reachable but
			// invalid answer refuses every bridge route. Neither writes anything.
			catalogue(rawLive)
			request.BridgeRevocation = func(context.Context, releasetrust.Snapshot) ([]releaseidentity.Digest, error) {
				return []releaseidentity.Digest{releaseidentity.Hash(rawLive)}, nil
			}
			attempt("revoked v2 bridge", bundleWith(signed(rawLive)), "recovery bridge revoked")
			request.BridgeRevocation = func(context.Context, releasetrust.Snapshot) ([]releaseidentity.Digest, error) {
				return nil, errors.New("revocation list rollback refused")
			}
			attempt("revocation endpoint answered invalidly", bundleWith(signed(rawLive)), "recovery bridge revocation check")
			// No hook configured: a bridge route is refused, not waved through.
			request.BridgeRevocation = nil
			attempt("no revocation check configured", bundleWith(signed(rawLive)), "no revocation check is configured")
		})
	}
}

// WriteBridgeBundleForTest lets the external acceptance package build the same
// throwaway-signed bridge bundles.
func WriteBridgeBundleForTest(t *testing.T, f *trustfixture.Fixture, releases []trustfixture.SignedRelease, bridges []releasetrust.BridgeMaterial) string {
	return writeBridgeBundle(t, f, releases, bridges)
}

// TestInFlightBridgedUpgradeResumesAfterNotAfter pins the behaviour the owner has
// not yet decided (#1422 F3). Today every re-plan, including the gate's resume
// and security-rollback paths (gate.go resume, security_rollback.go,
// automatic_release.go, maintenance_configuration.go), drops an expired bridge,
// so an upgrade that began before not_after cannot resume after it.
//
// Scenario when un-skipped: install A, run the bridge route with the operator
// evidence up to the prepared boundary, let not_after pass (a statement whose
// not_after is a few seconds out), then run the gate again with the same
// evidence. Wanted, pending the decision: the in-flight operation resumes to B.
// Actual today: the re-plan refuses with "no authenticated upgrade route".
func TestInFlightBridgedUpgradeResumesAfterNotAfter(t *testing.T) {
	t.Skip("blocked on the owner decision for #1422 F3: whether an expired or revoked bridge may finish an upgrade already in flight")
}
