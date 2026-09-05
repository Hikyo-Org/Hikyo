// Package testfixture writes ephemeral, genuinely signed offline bundles for
// integration tests. Callers supply independently inspected source claims.
package testfixture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type Target struct {
	Version      string
	Sequence     uint64
	Commit       string
	Migrations   releaseidentity.MigrationManifest
	SchemaSHA256 releaseidentity.Digest
}

type Fixture struct {
	Directory  string
	Pinned     releasetrust.PinnedTrust
	Target     releaseidentity.Identity
	Identities []releaseidentity.Identity
	Bundle     upgradebundle.Bundle
	Plan       upgradecompat.Plan
	Signer     *trustfixture.Fixture
}

// Write signs each supplied actual target schema and SQL inventory into an
// ordered route from an inspected fresh/legacy genesis. It never invents schema
// facts or bypasses verification. The returned Bundle also supports real
// same-release restart and intermediate-hop planning with inspected claims.
func Write(t testing.TB, source upgradecompat.InstalledSource, targets []Target) Fixture {
	t.Helper()
	if source.Identity.Genesis != releaseidentity.LegacyGenesisV1 && source.Identity.Genesis != releaseidentity.FreshGenesisV1 {
		t.Fatal("bundle fixture requires inspected explicit genesis")
	}
	if len(targets) == 0 {
		t.Fatal("bundle fixture needs actual target declarations")
	}
	f := trustfixture.New(t)
	releases := []trustfixture.SignedRelease{}
	identities := []releaseidentity.Identity{}
	previous := source
	for _, target := range targets {
		declaration := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: target.Version, Sequence: target.Sequence, Commit: target.Commit, Engines: []upgradecompat.EngineDeclaration{{Migrations: target.Migrations.Clone(), SchemaSHA256: target.SchemaSHA256, Sources: []upgradecompat.SourceEdge{{Source: previous.Identity, Migrations: previous.Migrations.Clone(), SchemaSHA256: previous.SchemaSHA256, Mode: upgradecompat.Maintenance}}}}}
		release := f.AddStable(t, target.Version, int64(target.Sequence), target.Commit, trustfixture.JSON(t, declaration))
		releases = append(releases, release)
		identities = append(identities, release.Identity)
		previous = upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: release.Identity}, Migrations: target.Migrations.Clone(), SchemaSHA256: target.SchemaSHA256}
	}
	material := f.Material(t)
	index := upgradebundle.Index{Format: upgradebundle.IndexFormat, PrimaryKeyIDs: []string{"test-primary"}, Releases: []upgradebundle.ReleaseEntry{}, Bridges: []releaseidentity.Digest{}}
	for _, release := range releases {
		index.Releases = append(index.Releases, upgradebundle.ReleaseEntry{Profile: releaseidentity.StableV1, ManifestSHA256: release.Identity.ManifestSHA256})
	}
	directory := t.TempDir()
	write := func(name string, raw []byte) {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, raw := range map[string][]byte{"index.json": trustfixture.JSON(t, index), "keys/test-primary.pub": f.PrimaryPublic, "metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature, "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature} {
		write(name, raw)
	}
	for _, release := range releases {
		for name, raw := range map[string][]byte{"manifest.json": release.Material.Manifest, "manifest.sigstore.json": release.Material.ManifestSignature, "release-candidate.json": release.Material.Candidate, "upgrade-compatibility.json": release.Material.Compatibility} {
			write("releases/"+string(release.Identity.ManifestSHA256)+"/"+name, raw)
		}
		for name, raw := range release.Payloads {
			write("releases/"+string(release.Identity.ManifestSHA256)+"/"+name, raw)
		}
	}
	bundle, err := upgradebundle.Load(t.Context(), directory, f.Pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	last := identities[len(identities)-1]
	plan, err := bundle.Plan(source, last)
	if err != nil {
		t.Fatal(err)
	}
	return Fixture{Directory: directory, Pinned: f.Pinned, Target: last, Identities: identities, Bundle: bundle, Plan: plan, Signer: f}
}
