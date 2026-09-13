package selfupdate

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

// VerifyCachedRelease reauthenticates an offline prepared release without
// discovery or network requests. The descriptor's executable path and digest
// are untrusted hints: both are replaced by extraction of the signed archive.
// The caller still binds the returned identity and payload to its running build
// and enforces the installation's durable migration/admission gate.
func (i *Installer) VerifyCachedRelease(ctx context.Context, prepared PreparedNightly, floor releaseidentity.SnapshotFloor) (PreparedNightly, error) {
	if i == nil || i.config.StateDir == "" || prepared.Identity.Validate() != nil || floor.Validate() != nil {
		return PreparedNightly{}, errors.New("selfupdate: configured cache and exact release identity required")
	}
	if err := realNightlyDirectory(i.config.StateDir); err != nil {
		return PreparedNightly{}, err
	}
	pinned, err := i.preparedPinnedTrust()
	if err != nil {
		return PreparedNightly{}, err
	}
	bundle, err := upgradebundle.Load(ctx, prepared.BundleDirectory, pinned, floor)
	if err != nil {
		return PreparedNightly{}, err
	}
	if _, err := bundle.Release(prepared.Identity); err != nil {
		return PreparedNightly{}, err
	}
	var release releasetrust.VerifiedRelease
	switch prepared.Identity.Profile {
	case releaseidentity.StableV1:
		release, err = verifyPreparedStableDirectory(prepared.Directory, bundle.Snapshot())
	case releaseidentity.NightlyV1:
		release, err = upgradebundle.VerifyNightlyPlatformDirectory(ctx, prepared.Directory, bundle.Snapshot(), nightlyPlatform())
	default:
		return PreparedNightly{}, errors.New("selfupdate: unsupported cached release profile")
	}
	if err != nil {
		return PreparedNightly{}, err
	}
	if release.Identity() != prepared.Identity {
		return PreparedNightly{}, errors.New("selfupdate: cached archive differs from exact release identity")
	}
	prepared.BinaryPath, prepared.BinarySHA256, err = i.extractPreparedBinary(ctx, prepared.Directory, release)
	if err != nil {
		return PreparedNightly{}, err
	}
	return prepared, nil
}
