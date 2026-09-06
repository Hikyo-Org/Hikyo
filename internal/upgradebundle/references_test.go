package upgradebundle_test

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestReferencesExposeExactAuthenticatedPredecessorWithoutInventingGenesisRelease(t *testing.T) {
	migrations := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{{Version: 1, SHA256: releaseidentity.Hash([]byte("SQL"))}}}
	schema := releaseidentity.Hash([]byte("catalog"))
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.LegacyGenesisV1}, Migrations: migrations, SchemaSHA256: schema}
	fixture := testfixture.Write(t, source, []testfixture.Target{
		{Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), Migrations: migrations, SchemaSHA256: schema},
		{Version: "1.1.0", Sequence: 2, Commit: strings.Repeat("b", 40), Migrations: migrations, SchemaSHA256: schema},
	})
	refs, err := fixture.Bundle.ReferencedReleases(releaseidentity.SQLite)
	if err != nil || len(refs) != 1 || refs[0] != fixture.Identities[0] {
		t.Fatalf("exact predecessor unavailable: %+v %v", refs, err)
	}
	refs[0].ManifestSHA256 = releaseidentity.Hash([]byte("mutated caller copy"))
	again, err := fixture.Bundle.ReferencedReleases(releaseidentity.SQLite)
	if err != nil || again[0] != fixture.Identities[0] {
		t.Fatal("reference accessor mutated authenticated evidence")
	}
	other, err := fixture.Bundle.ReferencedReleases(releaseidentity.Postgres)
	if err != nil || len(other) != 0 {
		t.Fatalf("references leaked across engines: %+v %v", other, err)
	}
	if _, err := (upgradebundle.Bundle{}).ReferencedReleases(releaseidentity.SQLite); err == nil {
		t.Fatal("unverified bundle supplied discovery references")
	}
}
