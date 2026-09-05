package upgradebundle

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
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
			plan, err := bundle.Plan(source, node.Identity())
			if err != nil || !plan.Valid() {
				t.Fatal("real nightly route refused", err)
			}
			if _, err := bundle.MatchBuild(append(append([]byte{}, compatibility...), '\n')); err == nil {
				t.Fatal("different embedded declaration bytes accepted")
			}
		})
	}
}
