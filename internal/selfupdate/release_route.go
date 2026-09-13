package selfupdate

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/gofrs/flock"
)

// AssembleReleaseRoute verifies a mixed stable/nightly route without selecting
// or executing a target. Each identity must already be exact prepared evidence.
func (i *Installer) AssembleReleaseRoute(ctx context.Context, target PreparedNightly, sources []PreparedNightly) (_ string, err error) {
	if i == nil || i.client == nil || i.config.StateDir == "" || len(sources) >= upgradecompat.MaxReleases {
		return "", errors.New("selfupdate: configured installer and bounded release route required")
	}
	if err := realNightlyDirectory(i.config.StateDir); err != nil {
		return "", err
	}
	lock := flock.New(filepath.Join(i.config.StateDir, "nightly-trust.lock"))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return "", errors.Join(err, errors.New("selfupdate: another release verification is running"))
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	pinned, err := i.preparedPinnedTrust()
	if err != nil {
		return "", err
	}
	floor, err := i.preparedSnapshotFloor(releaseidentity.SnapshotFloor{})
	if err != nil {
		return "", err
	}
	evidence := append([]PreparedNightly{target}, sources...)
	nightly := false
	for _, item := range evidence {
		nightly = nightly || item.Identity.Profile == releaseidentity.NightlyV1
	}
	material, snapshot, err := i.downloadSnapshot(ctx, pinned, floor, nightly)
	if err != nil {
		return "", err
	}
	directory, err := i.assembleReleaseEvidence(ctx, evidence, material, snapshot, pinned)
	if err != nil {
		return "", err
	}
	for _, profile := range []releaseidentity.Profile{releaseidentity.StableV1, releaseidentity.NightlyV1} {
		statePath := filepath.Join(i.config.StateDir, "stable-prepared-trust.json")
		read := readPreparedStableState
		if profile == releaseidentity.NightlyV1 {
			statePath, read = filepath.Join(i.config.StateDir, "nightly-trust.json"), readNightlyState
		}
		known, err := read(statePath)
		if err != nil {
			return "", err
		}
		for _, item := range evidence {
			if item.Identity.Profile != profile {
				continue
			}
			if known.Release.Sequence == item.Identity.Sequence && known.Release != item.Identity {
				return "", errors.New("selfupdate: route release equivocation refused")
			}
			if known.Release.Sequence < item.Identity.Sequence {
				known.Release = item.Identity
			}
		}
		if known.Release.Validate() != nil {
			continue
		}
		known.Floor = snapshot.Floor()
		if err := saveNightlyState(statePath, known); err != nil {
			return "", err
		}
	}
	return directory, nil
}
