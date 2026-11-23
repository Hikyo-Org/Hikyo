package main

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestDropStableReleaseEdgesKeepsGenesisAndNightlyEdges(t *testing.T) {
	release := func(profile releaseidentity.Profile) releaseidentity.Source {
		return releaseidentity.Source{Release: releaseidentity.Identity{
			Profile: profile, Version: "1.1.0", Sequence: 2, Commit: strings.Repeat("a", 40),
			CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest")),
		}}
	}
	genesis := upgradecompat.SourceEdge{Source: releaseidentity.Source{Genesis: releaseidentity.FreshGenesisV1}}
	nightly := upgradecompat.SourceEdge{Source: release(releaseidentity.NightlyV1)}
	engines := map[releaseidentity.Engine][]upgradecompat.SourceEdge{
		releaseidentity.SQLite:   {genesis, {Source: release(releaseidentity.StableV1)}, nightly},
		releaseidentity.Postgres: {genesis, {Source: release(releaseidentity.StableV1)}},
	}
	dropStableReleaseEdges(engines)
	if got := engines[releaseidentity.SQLite]; len(got) != 2 || got[0].Source != genesis.Source || got[1].Source != nightly.Source {
		t.Fatalf("sqlite: nightly sources kept a stable edge or dropped another: %+v", got)
	}
	if got := engines[releaseidentity.Postgres]; len(got) != 1 || got[0].Source != genesis.Source {
		t.Fatalf("postgres: nightly sources kept a stable edge or dropped the genesis edge: %+v", got)
	}
}
