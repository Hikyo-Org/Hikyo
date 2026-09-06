package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/gofrs/flock"
)

// AssembleNightlyRoute combines exact prepared source/route releases with the
// target under the current authenticated snapshot. Every flat directory is
// independently reverified. It does not infer edges or authorize execution; the
// caller must ask the resulting runtime bundle to plan the actual DB source.
func (i *Installer) AssembleNightlyRoute(ctx context.Context, target PreparedNightly, sources []PreparedNightly) (_ string, err error) {
	if i == nil || i.client == nil || i.config.StateDir == "" {
		return "", errors.New("selfupdate: installer is not configured")
	}
	if len(sources) >= upgradecompat.MaxReleases {
		return "", errors.New("selfupdate: nightly route exceeds release bound")
	}
	if err := realNightlyDirectory(i.config.StateDir); err != nil {
		return "", err
	}
	lock := flock.New(filepath.Join(i.config.StateDir, "nightly-trust.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return "", err
	}
	if !locked {
		return "", errors.New("selfupdate: another nightly verification is running")
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	statePath := filepath.Join(i.config.StateDir, "nightly-trust.json")
	known, err := readNightlyState(statePath)
	if err != nil {
		return "", err
	}
	root, err := decodeStamped("trust root", i.config.TrustRootBase64)
	if err != nil {
		return "", err
	}
	recovery, err := decodeStamped("recovery public key", i.config.RecoveryKeyBase64)
	if err != nil {
		return "", err
	}
	pinned := releasetrust.PinnedTrust{Root: root, RecoveryPublicKey: recovery}
	material, snapshot, err := i.nightlySnapshot(ctx, pinned, known.Floor)
	if err != nil {
		return "", err
	}
	evidence := make([]PreparedNightly, 0, len(sources)+1)
	evidence = append(evidence, target)
	evidence = append(evidence, sources...)
	highest := known.Release
	sequences := map[uint64]releaseidentity.Identity{}
	for _, item := range evidence {
		if previous, ok := sequences[item.Identity.Sequence]; ok && previous != item.Identity {
			return "", errors.New("selfupdate: route contains nightly equivocation")
		}
		sequences[item.Identity.Sequence] = item.Identity
		if highest.Sequence == item.Identity.Sequence && highest != item.Identity {
			return "", errors.New("selfupdate: nightly equivocation refused")
		}
		if item.Identity.Sequence > highest.Sequence {
			highest = item.Identity
		}
	}
	directory, err := i.assembleNightlyEvidence(ctx, evidence, material, snapshot, pinned)
	if err != nil {
		return "", fmt.Errorf("selfupdate: assemble nightly route: %w", err)
	}
	if err := saveNightlyState(statePath, nightlyVerificationState{Floor: snapshot.Floor(), Release: highest}); err != nil {
		return "", err
	}
	return directory, nil
}
