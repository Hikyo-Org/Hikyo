package updatecheck

import (
	"testing"
	"time"
)

func TestSelectSeparatesStableAndNightlyChannels(t *testing.T) {
	releases := []Release{
		{Version: "1.0.1", URL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.0.1"},
		{Version: "1.1.0-nightly.20260824.41.gaaaaaaaa", URL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0-nightly.20260824.41.gaaaaaaaa", Prerelease: true},
		{Version: "1.1.0-nightly.20260824.42.gbbbbbbbb", URL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0-nightly.20260824.42.gbbbbbbbb", Prerelease: true},
	}

	stable, err := Select("1.0.0", ChannelStable, releases)
	if err != nil {
		t.Fatal(err)
	}
	if !stable.Available || stable.LatestVersion != "1.0.1" || stable.Prerelease {
		t.Fatalf("stable status = %+v, want stable 1.0.1", stable)
	}

	nightly, err := Select("1.0.0", ChannelNightly, releases)
	if err != nil {
		t.Fatal(err)
	}
	if !nightly.Available || nightly.LatestVersion != "1.1.0-nightly.20260824.42.gbbbbbbbb" || !nightly.Prerelease {
		t.Fatalf("nightly status = %+v, want newest nightly", nightly)
	}
}

func TestSelectNightlyChannelAcceptsNewStableBetweenNightlies(t *testing.T) {
	releases := []Release{
		{Version: "1.1.0"},
		{Version: "1.1.0-nightly.20260824.42.gbbbbbbbb", Prerelease: true},
	}

	status, err := Select("1.1.0-nightly.20260824.42.gbbbbbbbb", ChannelNightly, releases)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.LatestVersion != "1.1.0" || status.Prerelease {
		t.Fatalf("nightly status = %+v, want promoted stable 1.1.0", status)
	}
}

func TestSelectOffAndCurrentReleaseProduceNoNotice(t *testing.T) {
	releases := []Release{{Version: "1.0.1", PublishedAt: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)}}

	off, err := Select("dev", ChannelOff, releases)
	if err != nil {
		t.Fatal(err)
	}
	if off.Available {
		t.Fatalf("off status = %+v, want no update", off)
	}

	current, err := Select("1.0.1", ChannelStable, releases)
	if err != nil {
		t.Fatal(err)
	}
	if current.Available {
		t.Fatalf("current status = %+v, want no update", current)
	}
}

func TestSelectDevelopmentBuildProducesNoNotice(t *testing.T) {
	status, err := Select("dev", ChannelStable, []Release{{Version: "1.0.1"}})
	if err != nil {
		t.Fatal(err)
	}
	if status.Available || status.CurrentVersion != "dev" {
		t.Fatalf("development status = %+v, want no update", status)
	}
}

func TestParseChannelRefusesUnknownValue(t *testing.T) {
	if _, err := ParseChannel("preview"); err == nil {
		t.Fatal("ParseChannel(preview) accepted an unknown channel")
	}
}
