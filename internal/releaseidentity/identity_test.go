package releaseidentity_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func TestSourceGenesisIsExplicitAndCannotInventARelease(t *testing.T) {
	for _, genesis := range []string{releaseidentity.FreshGenesisV1, releaseidentity.LegacyGenesisV1} {
		source := releaseidentity.Source{Genesis: genesis}
		if err := source.Validate(); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "release") {
			t.Fatal("genesis serialized an invented zero release")
		}
		source.Release.Version = "1.0.0"
		if err := source.Validate(); err == nil {
			t.Fatal("genesis accepted release fields")
		}
	}
	for _, source := range []releaseidentity.Source{{}, {Genesis: "arbitrary/v1"}} {
		if err := source.Validate(); err == nil {
			t.Fatal("unknown or empty source accepted")
		}
	}
}

func TestMigrationInventoryBindsEveryByteAndEngine(t *testing.T) {
	files := fstest.MapFS{
		"migrations/00002_second.sql": {Data: []byte("second\n")},
		"migrations/00001_first.sql":  {Data: []byte("first\n")},
	}
	manifest, err := releaseidentity.BuildMigrationManifest(files, "migrations", releaseidentity.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Entries[0].Version != 1 || manifest.Entries[1].Version != 2 {
		t.Fatal("migration order is not numeric")
	}
	before, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	files["migrations/00001_first.sql"].Data = []byte("first changed\n")
	changed, err := releaseidentity.BuildMigrationManifest(files, "migrations", releaseidentity.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	after, err := changed.Digest()
	if err != nil || before == after {
		t.Fatal("changing an earlier migration did not change inventory digest")
	}
	other := manifest.Clone()
	other.Engine = releaseidentity.Postgres
	engineDigest, err := other.Digest()
	if err != nil || engineDigest == before {
		t.Fatal("engine was not digest-bound")
	}
	other.Entries[0].SHA256 = releaseidentity.Hash([]byte("replacement"))
	if slices.Equal(other.Entries, manifest.Entries) {
		t.Fatal("clone aliases manifest entries")
	}
	files["migrations/01_duplicate.sql"] = &fstest.MapFile{Data: []byte("duplicate")}
	if _, err := releaseidentity.BuildMigrationManifest(files, "migrations", releaseidentity.SQLite); err == nil {
		t.Fatal("duplicate numeric migration version accepted")
	}
}

func TestReleaseIdentityRejectsMissingAndConfusedProfiles(t *testing.T) {
	identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
	if err := identity.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*releaseidentity.Identity){
		func(i *releaseidentity.Identity) { i.Profile = "" },
		func(i *releaseidentity.Identity) { i.Sequence = 0 },
		func(i *releaseidentity.Identity) { i.Commit = "HEAD" },
		func(i *releaseidentity.Identity) { i.Version = "v1.0.0" },
		func(i *releaseidentity.Identity) { i.Version = "1.0.0-nightly.1" },
		func(i *releaseidentity.Identity) { i.CompatibilitySHA256 = "" },
		func(i *releaseidentity.Identity) { i.ManifestSHA256 = releaseidentity.Digest(strings.Repeat("A", 64)) },
	} {
		invalid := identity
		mutate(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatal("invalid release identity accepted")
		}
	}
	identity.Profile = releaseidentity.NightlyV1
	identity.Version = "1.0.0-nightly.1"
	if err := identity.Validate(); err != nil {
		t.Fatal(err)
	}
}
