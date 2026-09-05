package chartfixture

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

// TestWriteChartFixture signs reviewed source-owned schema claims with ephemeral
// test keys. Only public material and linker inputs leave the test process.
// The chart's real PostgreSQL boot checks the exact schema and embedded SQL.
// No production key or development-admission bypass participates in this test.
func TestWriteChartFixture(t *testing.T) {
	output := os.Getenv("HIKYO_CHART_FIXTURE_OUTPUT")
	if output == "" {
		output = filepath.Join(t.TempDir(), "fixture")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("chart fixture output must not already exist")
	}
	commit := os.Getenv("HIKYO_CHART_FIXTURE_COMMIT")
	if commit == "" {
		commit = strings.Repeat("a", 40)
	}
	_, declaration, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	declaration.Version, declaration.Commit = "0.0.0-chart.1", commit
	if err := declaration.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, engine := range declaration.Engines {
		actual, err := releaseidentity.BuildMigrationManifest(store.MigrationsFS, "migrations/"+string(engine.Migrations.Engine), engine.Migrations.Engine)
		if err != nil {
			t.Fatal(err)
		}
		actualDigest, _ := actual.Digest()
		declaredDigest, _ := engine.Migrations.Digest()
		if actualDigest != declaredDigest {
			t.Fatal("source-owned chart fixture declaration differs from embedded migrations")
		}
	}
	claim := testfixture.JSON(t, declaration)
	fixture := testfixture.New(t)
	release := fixture.AddStable(t, declaration.Version, int64(declaration.Sequence), commit, claim)
	material := fixture.Material(t)
	index := upgradebundle.Index{Format: upgradebundle.IndexFormat, PrimaryKeyIDs: []string{"test-primary"}, Releases: []upgradebundle.ReleaseEntry{{Profile: releaseidentity.StableV1, ManifestSHA256: release.Identity.ManifestSHA256}}, Bridges: []releaseidentity.Digest{}}
	files := map[string][]byte{
		"operator.pub":                  fixture.PrimaryPublic,
		"bundle/index.json":             testfixture.JSON(t, index),
		"bundle/metadata.json":          material.Metadata,
		"bundle/metadata.sigstore.json": material.MetadataSignature,
		"bundle/catalog.json":           material.Catalog,
		"bundle/catalog.sigstore.json":  material.CatalogSignature,
		"bundle/keys/test-primary.pub":  fixture.PrimaryPublic,
	}
	releaseDirectory := "bundle/releases/" + string(release.Identity.ManifestSHA256) + "/"
	for name, raw := range map[string][]byte{
		"manifest.json":              release.Material.Manifest,
		"manifest.sigstore.json":     release.Material.ManifestSignature,
		"release-candidate.json":     release.Material.Candidate,
		"upgrade-compatibility.json": claim,
	} {
		files[releaseDirectory+name] = raw
	}
	for name, raw := range files {
		path := filepath.Join(output, "public", name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := upgradebundle.Load(t.Context(), filepath.Join(output, "public", "bundle"), fixture.Pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	node, err := bundle.MatchBuild(claim)
	if err != nil || node.Identity() != release.Identity {
		t.Fatal("chart fixture build binding failed")
	}
	wrong := releasetrust.PinnedTrust{Root: fixture.Pinned.Root, RecoveryPublicKey: testfixture.New(t).Pinned.RecoveryPublicKey}
	if _, err := upgradebundle.Load(t.Context(), filepath.Join(output, "public", "bundle"), wrong, releaseidentity.SnapshotFloor{}); err == nil {
		t.Fatal("chart fixture accepted unpinned signing authority")
	}
	encode := base64.StdEncoding.EncodeToString
	flags := []string{
		"-X main.version=" + declaration.Version,
		"-X main.commit=" + commit,
		"-X main.updateChannel=off",
		"-X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedTrustRoot=" + encode(fixture.Pinned.Root),
		"-X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedRecoveryPublicKey=" + encode(fixture.Pinned.RecoveryPublicKey),
		"-X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedDeclaration=" + encode(claim),
		"-X github.com/Hikyo-Org/hikyo/internal/buildcompat.declarationSHA256=" + string(releaseidentity.Hash(claim)),
	}
	if err := os.WriteFile(filepath.Join(output, "ldflags"), []byte(strings.Join(flags, " ")), 0600); err != nil {
		t.Fatal(err)
	}
}
