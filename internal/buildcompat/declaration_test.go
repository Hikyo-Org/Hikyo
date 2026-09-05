package buildcompat

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestEmbeddedDeclarationRequiresExactAuthenticatedEnvelope(t *testing.T) {
	oldEncoded, oldDigest := encodedDeclaration, declarationSHA256
	t.Cleanup(func() { encodedDeclaration, declarationSHA256 = oldEncoded, oldDigest })
	f := testfixture.New(t)
	d := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), Engines: []upgradecompat.EngineDeclaration{{Migrations: releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}, SchemaSHA256: releaseidentity.Hash([]byte("schema")), Sources: []upgradecompat.SourceEdge{}}}}
	raw := testfixture.JSON(t, d)
	signed := f.AddStable(t, d.Version, int64(d.Sequence), d.Commit, raw)
	release, err := releasetrust.VerifyStable(f.Snapshot(t), signed.Material)
	if err != nil {
		t.Fatal(err)
	}
	node, err := upgradecompat.Bind(release, raw)
	if err != nil {
		t.Fatal(err)
	}
	encodedDeclaration = ""
	declarationSHA256 = ""
	if Verify(node) == nil {
		t.Fatal("unstamped binary admitted a release")
	}
	encodedDeclaration = base64.StdEncoding.EncodeToString(raw)
	declarationSHA256 = string(releaseidentity.Hash(raw))
	if err := Verify(node); err != nil {
		t.Fatal(err)
	}
	got, _, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	got[0] = 'X'
	if err := Verify(node); err != nil {
		t.Fatal("caller mutated embedded bytes", err)
	}
	declarationSHA256 = string(releaseidentity.Hash([]byte("different")))
	if Verify(node) == nil {
		t.Fatal("embedded digest mismatch accepted")
	}
	d.Version = "1.1.0"
	d.Sequence = 2
	raw = testfixture.JSON(t, d)
	encodedDeclaration = base64.StdEncoding.EncodeToString(raw)
	declarationSHA256 = string(releaseidentity.Hash(raw))
	if Verify(node) == nil {
		t.Fatal("another authenticated release matched running binary")
	}
}
