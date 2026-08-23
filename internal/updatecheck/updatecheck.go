// Package updatecheck selects update notices from Hikyo's release channel.
package updatecheck

import (
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

// Channel is one installation's release track.
type Channel string

const (
	ChannelStable  Channel = "stable"
	ChannelNightly Channel = "nightly"
	ChannelOff     Channel = "off"
)

// Release is one published release-source record.
type Release struct {
	Version     string    `json:"version"`
	URL         string    `json:"url"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

// Status is the complete notification decision for one installed version.
type Status struct {
	Channel        Channel   `json:"channel"`
	CurrentVersion string    `json:"current_version"`
	LatestVersion  string    `json:"latest_version,omitempty"`
	URL            string    `json:"url,omitempty"`
	Available      bool      `json:"available"`
	Prerelease     bool      `json:"prerelease"`
	PublishedAt    time.Time `json:"published_at,omitempty"`
}

// ParseChannel validates a configured release track.
func ParseChannel(raw string) (Channel, error) {
	channel := Channel(strings.ToLower(strings.TrimSpace(raw)))
	switch channel {
	case ChannelStable, ChannelNightly, ChannelOff:
		return channel, nil
	default:
		return "", fmt.Errorf("updatecheck: channel must be stable, nightly, or off, got %q", raw)
	}
}

// Select returns the highest published release admitted by channel.
func Select(current string, channel Channel, releases []Release) (Status, error) {
	status := Status{Channel: channel, CurrentVersion: strings.TrimPrefix(current, "v")}
	if channel == ChannelOff {
		return status, nil
	}
	if channel != ChannelStable && channel != ChannelNightly {
		return Status{}, fmt.Errorf("updatecheck: unsupported channel %q", channel)
	}
	if status.CurrentVersion == "dev" {
		return status, nil
	}

	installed, err := semver.StrictNewVersion(status.CurrentVersion)
	if err != nil {
		return Status{}, fmt.Errorf("updatecheck: installed version %q is not SemVer: %w", current, err)
	}

	var latest *semver.Version
	var selected Release
	for _, release := range releases {
		candidate, err := semver.StrictNewVersion(strings.TrimPrefix(release.Version, "v"))
		if err != nil || !admitted(channel, release, candidate) {
			continue
		}
		if latest == nil || candidate.GreaterThan(latest) {
			latest = candidate
			selected = release
		}
	}
	if latest == nil {
		return status, nil
	}

	status.LatestVersion = latest.Original()
	status.URL = selected.URL
	status.Prerelease = selected.Prerelease
	status.PublishedAt = selected.PublishedAt
	status.Available = latest.GreaterThan(installed)
	return status, nil
}

func admitted(channel Channel, release Release, version *semver.Version) bool {
	prerelease := version.Prerelease()
	if release.Prerelease != (prerelease != "") {
		return false
	}
	if prerelease == "" {
		return true
	}
	return channel == ChannelNightly && strings.HasPrefix(prerelease, "nightly.")
}
