package upgradebundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestNightlyOfflineBundleAuthenticatesActualClosedPayloadDirectory(t *testing.T) {
	for _, mutation := range []string{"none", "extra", "tampered", "wrong commit"} {
		t.Run(mutation, func(t *testing.T) {
			manifest := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{{Version: 1, SHA256: releaseidentity.Hash([]byte("actual SQL"))}}}
			schema := releaseidentity.Hash([]byte("actual catalog"))
			source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.LegacyGenesisV1}, Migrations: manifest, SchemaSHA256: schema}
			declaration := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.NightlyV1, Version: "1.1.0-nightly.1", Sequence: 2, Commit: strings.Repeat("a", 40), Engines: []upgradecompat.EngineDeclaration{{Migrations: manifest, SchemaSHA256: schema, Sources: []upgradecompat.SourceEdge{{Source: source.Identity, Migrations: manifest, SchemaSHA256: schema, Mode: upgradecompat.Maintenance}}}}}
			compatibility := testfixture.JSON(t, declaration)
			fixture, nightly, _ := testfixture.Nightly(t, compatibility, mutation == "wrong commit")
			material := fixture.Material(t)
			digest := releaseidentity.Hash(nightly.Manifest)
			index := Index{Format: IndexFormat, PrimaryKeyIDs: []string{"test-primary"}, Releases: []ReleaseEntry{{Profile: releaseidentity.NightlyV1, ManifestSHA256: digest}}, Bridges: []releaseidentity.Digest{}}
			directory := t.TempDir()
			for name, raw := range map[string][]byte{"index.json": testfixture.JSON(t, index), "keys/test-primary.pub": fixture.PrimaryPublic, "metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature, "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature} {
				writeFixture(t, directory, name, raw)
			}
			releaseDir := "releases/" + string(digest) + "/"
			for name, raw := range map[string][]byte{"manifest.json": nightly.Manifest, "manifest.sigstore.json": nightly.Bundle, "policy.json": nightly.Policy, "trusted-root.json": nightly.TrustedRoot, "upgrade-compatibility.json": nightly.Compatibility} {
				writeFixture(t, directory, releaseDir+name, raw)
			}
			for name, reader := range nightly.Artifacts {
				raw, err := io.ReadAll(reader)
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, directory, releaseDir+"payloads/"+name, raw)
			}
			if mutation != "wrong commit" {
				// Assemble from a flat download on the same filesystem. Verify
				// physical storage reuse and that cache pruning leaves a complete
				// independently loadable bundle.
				flat := t.TempDir()
				for name, raw := range map[string][]byte{"release-manifest.json": nightly.Manifest, "release-manifest.sigstore.json": nightly.Bundle} {
					writeFixture(t, flat, name, raw)
				}
				for name := range nightly.Artifacts {
					raw, err := os.ReadFile(filepath.Join(directory, releaseDir, "payloads", name))
					if err != nil {
						t.Fatal(err)
					}
					writeFixture(t, flat, name, raw)
				}
				snapshot, err := releasetrust.VerifySnapshot(fixture.Pinned, material, releaseidentity.SnapshotFloor{})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(filepath.Join(directory, "releases")); err != nil {
					t.Fatal(err)
				}
				if _, err := CopyNightlyRelease(t.Context(), flat, filepath.Join(directory, "releases"), snapshot); err != nil {
					t.Fatal(err)
				}
				for name := range nightly.Artifacts {
					source, err := os.Stat(filepath.Join(flat, name))
					if err != nil {
						t.Fatal(err)
					}
					target, err := os.Stat(filepath.Join(directory, releaseDir, "payloads", name))
					if err != nil || !os.SameFile(source, target) {
						t.Fatalf("payload storage duplicated for %s: %v", name, err)
					}
				}
				if err := os.RemoveAll(flat); err != nil {
					t.Fatal(err)
				}
			}
			if mutation == "extra" {
				writeFixture(t, directory, releaseDir+"payloads/unsigned-extra.txt", []byte("extra"))
			}
			if mutation == "tampered" {
				writeFixture(t, directory, releaseDir+"payloads/hikyo_linux_arm64.tar.gz", []byte("replaced payload"))
			}
			bundle, err := Load(context.Background(), directory, fixture.Pinned, releaseidentity.SnapshotFloor{})
			if mutation != "none" {
				if err == nil {
					t.Fatal("untrusted nightly payload directory accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			node, err := bundle.MatchBuild(compatibility)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := bundle.Plan(source, node.Identity()); err == nil {
				t.Fatal("legacy route admitted without recovery bridge")
			}
			bridge := fixture.AddBridge(t, releasetrust.BridgeStatement{Schema: releasetrust.LegacyNightlyBridgeSchemaV2, NotAfter: time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second).Format(time.RFC3339), SourceGenesis: releaseidentity.LegacyGenesisV1, Target: node.Identity(), TargetPolicySHA256: releaseidentity.Hash(nightly.Policy), SourceMigrations: manifest, TargetMigrations: manifest, SourceSchemaSHA256: schema, TargetSchemaSHA256: schema, Mode: "maintenance"})
			bridgeDigest := releaseidentity.Hash(bridge.Statement)
			// The catalog also lists an expiry-free legacy v1 statement for the same pair, as
			// the production catalog does: the bundle must still load, and it is never a route.
			legacy := fixture.AddBridge(t, releasetrust.BridgeStatement{Schema: "hikyo.dev/legacy-nightly-bridge/v1", SourceGenesis: releaseidentity.LegacyGenesisV1, Target: node.Identity(), TargetPolicySHA256: releaseidentity.Hash(nightly.Policy), SourceMigrations: manifest, TargetMigrations: manifest, SourceSchemaSHA256: schema, TargetSchemaSHA256: schema, Mode: "maintenance"})
			legacyDigest := releaseidentity.Hash(legacy.Statement)
			index.Bridges = []releaseidentity.Digest{bridgeDigest, legacyDigest}
			fixture.Catalog.Sequence++
			material = fixture.Material(t)
			for name, raw := range map[string][]byte{"index.json": testfixture.JSON(t, index), "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature,
				"bridges/" + string(bridgeDigest) + "/statement.json": bridge.Statement, "bridges/" + string(bridgeDigest) + "/statement.sigstore.json": bridge.Signature,
				"bridges/" + string(legacyDigest) + "/statement.json": legacy.Statement, "bridges/" + string(legacyDigest) + "/statement.sigstore.json": legacy.Signature} {
				writeFixture(t, directory, name, raw)
			}
			bundle, err = Load(t.Context(), directory, fixture.Pinned, bundle.Snapshot().Floor())
			if err != nil {
				t.Fatal(err)
			}
			refs, err := bundle.ReferencedReleases(releaseidentity.SQLite)
			if err != nil || len(refs) != 1 || refs[0] != node.Identity() {
				t.Fatalf("legacy bridge target reference missing: %+v %v", refs, err)
			}
			// A bridge can identify a source schema even when no loaded target
			// declaration repeats its legacy genesis. Keep that candidate usable
			// for inspection, without inventing a signed legacy release identity.
			bridgeOnly := Bundle{snapshot: bundle.snapshot, bridges: bundle.bridges}
			candidates := bridgeOnly.Sources(releaseidentity.SQLite)
			if len(candidates) != 1 || candidates[0].Identity != source.Identity || candidates[0].SchemaSHA256 != source.SchemaSHA256 {
				t.Fatalf("bridge source inspection missing: %+v", candidates)
			}
			if candidates := bridgeOnly.Sources(releaseidentity.Postgres); len(candidates) != 0 {
				t.Fatal("bridge source leaked across engines")
			}
			if len(bundle.bridges) != 2 {
				t.Fatalf("both catalogued bridges load, the expiry-free one included: %d", len(bundle.bridges))
			}
			if _, err := bundle.Plan(source, node.Identity()); err == nil || !strings.Contains(err.Error(), "no revocation lookup") {
				t.Fatalf("a bridge route on a bundle with no revocation lookup must be refused: %v", err)
			}
			if _, err := bundle.WithBridgeRevocations(func() ([]releaseidentity.Digest, error) { return []releaseidentity.Digest{bridgeDigest}, nil }).Plan(source, node.Identity()); err == nil {
				t.Fatal("with the v2 bridge revoked, the expiry-free v1 bridge on the same pair must not become the route")
			}
			plan, err := bundle.WithBridgeRevocations(func() ([]releaseidentity.Digest, error) { return nil, nil }).Plan(source, node.Identity())
			if err != nil || !plan.Valid() || !plan.RequiresOperatorAttestation() {
				t.Fatal("recovery-authorized nightly route refused or lost operator proof", err)
			}
			// A rollback to the journaled route is never blocked by the inventory, the
			// expiry-free statement included, with no revocation lookup at all.
			if rolled, err := bundle.PlanRollback(source, node.Identity(), plan.Digest()); err != nil || rolled.Digest() != plan.Digest() {
				t.Fatalf("rollback of the journaled route must not be blocked: %v", err)
			}
			// R2: under a root that still pins recovery-1 the routable v2 bridge and the inert v1
			// bridge both load and count, a new admission is refused with the way out named, and a
			// security rollback of the route journaled earlier is not affected.
			sum := sha256.Sum256(fixture.Pinned.RecoveryPublicKey)
			undo := releasetrust.DenyBridgeSignerForTest(hex.EncodeToString(sum[:]))
			retired, err := Load(t.Context(), directory, fixture.Pinned, bundle.Snapshot().Floor())
			undo()
			if err != nil || len(retired.bridges) != 2 {
				t.Fatalf("both bridges load under the recovery-1 root: %d %v", len(retired.bridges), err)
			}
			retired = retired.WithBridgeRevocations(func() ([]releaseidentity.Digest, error) { return nil, nil })
			if _, err := retired.Plan(source, node.Identity()); !errors.Is(err, releasetrust.ErrRetiredBridgeSigner) || !strings.Contains(err.Error(), "recovery-2") || !strings.Contains(err.Error(), "recovery-bridge-ceremony.md") {
				t.Fatalf("a new admission must say the root pins recovery-1 and name the runbook: %v", err)
			}
			if rolled, err := retired.PlanRollback(source, node.Identity(), plan.Digest()); err != nil || rolled.Digest() != plan.Digest() {
				t.Fatalf("security rollback must not be affected by the recovery-1 denial: %v", err)
			}
			// The legacy nightly bridge expires at its signed not_after like every bridge. An
			// absent list (404) or an unreachable endpoint falls back to that expiry; a present
			// but invalid list refuses; a revocation withdraws the bridge.
			signedList := func(revoked ...releaseidentity.Digest) ([]byte, []byte) {
				raw := testfixture.JSON(t, releasetrust.RevocationList{Schema: releasetrust.RevocationSchema, Sequence: 1, IssuedAt: time.Now().UTC().Add(-time.Hour).Truncate(time.Second).Format(time.RFC3339), TrustRootSHA256: releaseidentity.Hash(fixture.Pinned.Root), Revoked: revoked})
				return raw, testfixture.Sign(t, fixture.RecoverySigner, raw)
			}
			goodList, goodSignature := signedList()
			revokedList, revokedSignature := signedList(bridgeDigest)
			unreachable := func(cause error) scriptedFetcher {
				return scriptedFetcher{err: fmt.Errorf("%w: %w", bridgerevocation.ErrUnreachable, cause)}
			}
			cases := map[string]struct {
				fetcher scriptedFetcher
				want    string // empty: the route plans
			}{
				"list absent (404)":                {unreachable(errors.New("status 404")), ""},
				"connection dropped (EOF)":         {unreachable(io.ErrUnexpectedEOF), ""},
				"connection reset":                 {unreachable(syscall.ECONNRESET), ""},
				"timeout":                          {unreachable(context.DeadlineExceeded), ""},
				"valid list, nothing revoked":      {scriptedFetcher{list: goodList, signature: goodSignature}, ""},
				"bridge revoked":                   {scriptedFetcher{list: revokedList, signature: revokedSignature}, "recovery bridge revoked"},
				"present but badly signed":         {scriptedFetcher{list: goodList, signature: []byte("{}")}, "recovery bridge revocation check"},
				"present but malformed":            {scriptedFetcher{list: []byte("{"), signature: goodSignature}, "recovery bridge revocation check"},
				"reachable, unusable answer (403)": {scriptedFetcher{err: errors.New("revocation endpoint answered status 403")}, "recovery bridge revocation check"},
			}
			for name, c := range cases {
				lookup := func() ([]releaseidentity.Digest, error) {
					checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: c.fetcher, Directory: t.TempDir()}
					return checker.Revoked(t.Context(), bundle.Snapshot())
				}
				_, err := bundle.WithBridgeRevocations(lookup).Plan(source, node.Identity())
				if (c.want == "") != (err == nil) || (c.want != "" && !strings.Contains(err.Error(), c.want)) {
					t.Fatalf("legacy nightly route, %s: want %q, got %v", name, c.want, err)
				}
			}
			// With the list absent, expiry is the only limit: past not_after there is no route.
			expired := bundle.WithBridgeRevocations(func() ([]releaseidentity.Digest, error) {
				checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: unreachable(errors.New("status 404")), Directory: t.TempDir()}
				return checker.Revoked(t.Context(), bundle.Snapshot())
			}).WithClock(func() time.Time { return time.Now().Add(48 * time.Hour) })
			if _, err := expired.Plan(source, node.Identity()); err == nil || !strings.Contains(err.Error(), "no authenticated upgrade route") {
				t.Fatalf("an expired legacy bridge must have no route even with the list absent: %v", err)
			}
			if _, err := bundle.MatchBuild(append(append([]byte{}, compatibility...), '\n')); err == nil {
				t.Fatal("different embedded declaration bytes accepted")
			}
		})
	}
}

// scriptedFetcher answers the revocation endpoint with fixed bytes or an error.
type scriptedFetcher struct {
	list, signature []byte
	err             error
}

func (f scriptedFetcher) Fetch(context.Context) ([]byte, []byte, error) {
	return f.list, f.signature, f.err
}
