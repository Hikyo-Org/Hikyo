package upgradebundle

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestReferencesRefuseOversizedOrConflictingSignedDiscovery(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprintf("conflict=%t", conflict), func(t *testing.T) {
			f := newBundleFixture(t)
			count := upgradecompat.MaxReleases + 1
			if conflict {
				count = 2
			}
			edges := make([]upgradecompat.SourceEdge, 0, count)
			for n := range count {
				manifest := releaseidentity.Hash([]byte(fmt.Sprintf("source %d", n)))
				if conflict {
					manifest = releaseidentity.Hash([]byte("same manifest"))
				}
				identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: fmt.Sprintf("1.%d.0", n), Sequence: uint64(n + 1), Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compatibility")), ManifestSHA256: manifest}
				edges = append(edges, upgradecompat.SourceEdge{Source: releaseidentity.Source{Release: identity}, Migrations: f.source.Migrations, SchemaSHA256: f.source.SchemaSHA256, Mode: upgradecompat.Maintenance})
			}
			declaration := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: "10.0.0", Sequence: 1000, Commit: strings.Repeat("b", 40), Engines: []upgradecompat.EngineDeclaration{{Migrations: f.source.Migrations, SchemaSHA256: f.source.SchemaSHA256, Sources: edges}}}
			signed := f.signer.AddStable(t, declaration.Version, int64(declaration.Sequence), declaration.Commit, testfixture.JSON(t, declaration))
			snapshot, err := releasetrust.VerifySnapshot(f.signer.Pinned, f.signer.Material(t), releaseidentity.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			release, err := releasetrust.VerifyStable(snapshot, signed.Material)
			if err != nil {
				t.Fatal(err)
			}
			node, err := upgradecompat.Bind(release, signed.Material.Compatibility)
			if err != nil {
				t.Fatal(err)
			}
			bundle := Bundle{snapshot: snapshot, nodes: []upgradecompat.VerifiedNode{node}}
			if refs, err := bundle.ReferencedReleases(releaseidentity.SQLite); err == nil || refs != nil {
				t.Fatalf("unsafe signed discovery accepted: %d references, %v", len(refs), err)
			}
		})
	}
}
