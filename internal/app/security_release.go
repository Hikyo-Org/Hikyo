package app

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type preparedSecurityRelease struct {
	Installer *selfupdate.Installer
	Pinned    releasetrust.PinnedTrust
	Source    automaticReleaseSource
	Target    selfupdate.PreparedNightly
	Bundle    upgradebundle.Bundle
	Release   releasetrust.VerifiedRelease
}

func securityRouteImpact(plan upgradecompat.Plan) (securitymaintenance.Impact, error) {
	if !plan.Valid() {
		return securitymaintenance.Impact{}, errors.New("cannot measure an unauthenticated upgrade route")
	}
	impact := securitymaintenance.Impact{Restart: true}
	for _, step := range plan.Steps() {
		source, err := step.SourceMigrations.Digest()
		if err != nil {
			return impact, err
		}
		target, err := step.TargetMigrations.Digest()
		if err != nil {
			return impact, err
		}
		impact.Schema = impact.Schema || source != target || step.SourceSchemaSHA256 != step.TargetSchemaSHA256
		impact.Maintenance = impact.Maintenance || step.Mode == upgradecompat.Maintenance
	}
	return impact, nil
}

const securityRequestTimeout = 60 * time.Second

func newSecurityImageInstaller(directory string, pinned releasetrust.PinnedTrust) (*selfupdate.Installer, error) {
	return selfupdate.NewInstaller(selfupdate.Config{StateDir: filepath.Join(directory, "downloads"), TrustRootBase64: base64.StdEncoding.EncodeToString(pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(pinned.RecoveryPublicKey), TransientBundles: true})
}

// prepareSecurityRelease treats a feed's identity as an untrusted locator.
// Existing independently pinned release authority authenticates every byte.
func prepareSecurityRelease(ctx context.Context, identity releaseidentity.Identity, directory string) (preparedSecurityRelease, error) {
	var result preparedSecurityRelease
	if identity.Validate() != nil {
		return result, errors.New("security maintenance requires an exact release identity")
	}
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return result, err
	}
	installer, err := selfupdate.NewInstaller(selfupdate.Config{StateDir: directory, TrustRootBase64: base64.StdEncoding.EncodeToString(pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(pinned.RecoveryPublicKey), TransientBundles: true})
	if err != nil {
		return result, err
	}
	client, err := updatecheck.NewHTTPClient(60 * time.Second)
	if err != nil {
		return result, err
	}
	source := updatecheck.NewGitHubSource(client)
	release, err := source.ReleaseByVersion(ctx, identity.Version)
	if err != nil {
		return result, err
	}
	channel := updatecheck.ChannelStable
	if identity.Profile == releaseidentity.NightlyV1 {
		channel = updatecheck.ChannelNightly
	}
	status := updatecheck.Status{Available: true, Channel: channel, LatestVersion: release.Version, Prerelease: release.Prerelease, Immutable: release.Immutable, Assets: release.Assets, URL: release.URL}
	target, err := installer.PrepareReleaseSource(ctx, status, identity)
	if err != nil {
		return result, err
	}
	if target.Identity != identity {
		return result, errors.New("independent release verification differs from security target")
	}
	bundle, err := upgradebundle.Load(ctx, target.BundleDirectory, pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		return result, err
	}
	verified, err := bundle.Release(identity)
	if err != nil {
		return result, err
	}
	return preparedSecurityRelease{Installer: installer, Pinned: pinned, Source: source, Target: target, Bundle: bundle, Release: verified}, nil
}
