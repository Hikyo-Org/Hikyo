package upgradebundle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type bundleFixture struct {
	directory string
	signer    *testfixture.Fixture
	release   testfixture.SignedRelease
	source    upgradecompat.InstalledSource
	index     Index
}

func writeFixture(t *testing.T, directory, name string, raw []byte) {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func newBundleFixture(t *testing.T) bundleFixture {
	t.Helper()
	f := testfixture.New(t)
	migrations := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{{Version: 1, SHA256: releaseidentity.Hash([]byte("SQL"))}}}
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.LegacyGenesisV1}, Migrations: migrations, SchemaSHA256: releaseidentity.Hash([]byte("actual catalog"))}
	declaration := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), Engines: []upgradecompat.EngineDeclaration{{Migrations: migrations, SchemaSHA256: source.SchemaSHA256, Sources: []upgradecompat.SourceEdge{{Source: source.Identity, Migrations: migrations, SchemaSHA256: source.SchemaSHA256, Mode: upgradecompat.Maintenance}}}}}
	release := f.AddStable(t, "1.0.0", 1, declaration.Commit, testfixture.JSON(t, declaration))
	index := Index{Format: IndexFormat, PrimaryKeyIDs: []string{"test-primary"}, Releases: []ReleaseEntry{{Profile: releaseidentity.StableV1, ManifestSHA256: release.Identity.ManifestSHA256}}, Bridges: []releaseidentity.Digest{}}
	directory := t.TempDir()
	material := f.Material(t)
	for name, raw := range map[string][]byte{"index.json": testfixture.JSON(t, index), "keys/test-primary.pub": f.PrimaryPublic, "metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature, "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature} {
		writeFixture(t, directory, name, raw)
	}
	for name, raw := range map[string][]byte{"manifest.json": release.Material.Manifest, "manifest.sigstore.json": release.Material.ManifestSignature, "release-candidate.json": release.Material.Candidate, "upgrade-compatibility.json": release.Material.Compatibility} {
		writeFixture(t, directory, "releases/"+string(release.Identity.ManifestSHA256)+"/"+name, raw)
	}
	return bundleFixture{directory: directory, signer: f, release: release, source: source, index: index}
}

func TestOfflineBundleUsesRealSignaturesAndActualInspectedSource(t *testing.T) {
	f := newBundleFixture(t)
	bundle, err := Load(context.Background(), f.directory, f.signer.Pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := bundle.Plan(f.source, f.release.Identity)
	if err != nil || !plan.Valid() || len(plan.Steps()) != 1 {
		t.Fatal("real authenticated route unavailable", err)
	}
	candidates := bundle.GenesisManifests(releaseidentity.SQLite)
	if len(candidates) != 1 || candidates[0].SchemaSHA256 != f.source.SchemaSHA256 {
		t.Fatal("signed source candidate missing")
	}
	candidates[0].Migrations.Entries[0].SHA256 = releaseidentity.Hash([]byte("changed"))
	if bundle.GenesisManifests(releaseidentity.SQLite)[0].Migrations.Entries[0].SHA256 != f.source.Migrations.Entries[0].SHA256 {
		t.Fatal("candidate accessor mutated proof")
	}
	changed := f.source
	changed.SchemaSHA256 = releaseidentity.Hash([]byte("uninspected source"))
	if _, err := bundle.Plan(changed, f.release.Identity); err == nil {
		t.Fatal("unsigned previous-version claim supplied installed authority")
	}
	rootReplacement := testfixture.New(t)
	if _, err := Load(context.Background(), f.directory, rootReplacement.Pinned, releaseidentity.SnapshotFloor{}); err == nil {
		t.Fatal("bundle replaced current installation trust root")
	}
	floor := bundle.Snapshot().Floor()
	floor.CatalogSequence++
	if _, err := Load(context.Background(), f.directory, f.signer.Pinned, floor); err == nil {
		t.Fatal("offline evidence reset rollback floor")
	}
}

func TestOfflineBundleRejectsCorruptAndUnboundedTransport(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, bundleFixture){
		"duplicate index": func(t *testing.T, f bundleFixture) {
			raw := testfixture.JSON(t, f.index)
			raw = []byte(strings.Replace(string(raw), `"format":`, `"format":"wrong","format":`, 1))
			writeFixture(t, f.directory, "index.json", raw)
		},
		"path traversal": func(t *testing.T, f bundleFixture) {
			f.index.PrimaryKeyIDs = []string{"../../escape"}
			writeFixture(t, f.directory, "index.json", testfixture.JSON(t, f.index))
		},
		"changed manifest": func(t *testing.T, f bundleFixture) {
			writeFixture(t, f.directory, "releases/"+string(f.release.Identity.ManifestSHA256)+"/manifest.json", []byte("{}"))
		},
		"changed compatibility": func(t *testing.T, f bundleFixture) {
			writeFixture(t, f.directory, "releases/"+string(f.release.Identity.ManifestSHA256)+"/upgrade-compatibility.json", []byte("{}"))
		},
		"changed signer": func(t *testing.T, f bundleFixture) {
			other := testfixture.New(t)
			writeFixture(t, f.directory, "keys/test-primary.pub", other.PrimaryPublic)
		},
		"oversized member": func(t *testing.T, f bundleFixture) {
			writeFixture(t, f.directory, "index.json", []byte(strings.Repeat(" ", releasetrust.MaxDocumentBytes+1)))
		},
		"unknown profile": func(t *testing.T, f bundleFixture) {
			f.index.Releases[0].Profile = "nightly/v900"
			writeFixture(t, f.directory, "index.json", testfixture.JSON(t, f.index))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newBundleFixture(t)
			mutate(t, f)
			if _, err := Load(context.Background(), f.directory, f.signer.Pinned, releaseidentity.SnapshotFloor{}); err == nil {
				t.Fatal("bad offline bundle accepted")
			}
		})
	}
	f := newBundleFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Load(ctx, f.directory, f.signer.Pinned, releaseidentity.SnapshotFloor{}); err == nil {
		t.Fatal("canceled load accepted")
	}
}

func TestOfflineBundleCannotOmitCurrentCatalogBridge(t *testing.T) {
	f := newBundleFixture(t)
	target := f.release.Identity
	target.Version = "1.1.0"
	target.Sequence++
	f.signer.AddBridge(t, releasetrust.BridgeStatement{Schema: "hikyo.dev/recovery-bridge/v1", Source: f.release.Identity, Target: target, SourcePolicySHA256: releaseidentity.Hash([]byte("source policy")), TargetPolicySHA256: releaseidentity.Hash([]byte("target policy")), SourceMigrations: f.source.Migrations, TargetMigrations: f.source.Migrations, SourceSchemaSHA256: f.source.SchemaSHA256, TargetSchemaSHA256: f.source.SchemaSHA256, Mode: "maintenance"})
	material := f.signer.Material(t)
	for name, raw := range map[string][]byte{"metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature, "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature} {
		writeFixture(t, f.directory, name, raw)
	}
	// The signed current catalog now requires a bridge which the unsigned index
	// deliberately omits. Load must refuse before any ordinary route is usable.
	if _, err := Load(context.Background(), f.directory, f.signer.Pinned, releaseidentity.SnapshotFloor{}); err == nil {
		t.Fatal("unsigned index omitted a current exceptional bridge")
	}
}
