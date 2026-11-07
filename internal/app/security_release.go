package app

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// securityStageCache returns a private cache for one staged target and removes
// any left by other targets. Staging runs beside a serving source, so it must not
// share that source's cache: its rollback high-water mark and bundle pruning would
// stop the source restarting. One cache per target also lets a withdrawal select an
// older target than one staged earlier. The source's own cache stays, because a
// source that arrived through staging still reads its release from there.
//
// A new cache starts with no remembered trust floor. That does not reset rollback
// protection: stage verifies against the floor in the database, the manifest must
// match the catalog's exact selection, and the coordinator's own persistent cache
// has already authenticated the release against its retained floor.
func securityStageCache(directory string, target, source releaseidentity.Digest) (string, error) {
	const prefix = "security-stage-"
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if name := entry.Name(); strings.HasPrefix(name, prefix) && entry.IsDir() && name != prefix+string(target) && name != prefix+string(source) {
			if err := os.RemoveAll(filepath.Join(directory, name)); err != nil {
				return "", err
			}
		}
	}
	cache := filepath.Join(directory, prefix+string(target))
	if err := os.Mkdir(cache, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	return cache, nil
}

func newSecurityImageInstaller(cache string, pinned releasetrust.PinnedTrust) (*selfupdate.Installer, error) {
	return selfupdate.NewInstaller(selfupdate.Config{StateDir: cache, TrustRootBase64: base64.StdEncoding.EncodeToString(pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(pinned.RecoveryPublicKey), TransientBundles: true})
}

// prepareSecurityRelease treats a feed's identity as an untrusted locator.
// Existing independently pinned release authority authenticates every byte.
func prepareSecurityRelease(ctx context.Context, identity releaseidentity.Identity, directory string) (preparedSecurityRelease, error) {
	if identity.Validate() != nil {
		return preparedSecurityRelease{}, errors.New("security maintenance requires an exact release identity")
	}
	return prepareSecurityReleaseVersion(ctx, identity.Version, &identity, directory)
}

// prepareSecurityReleaseVersion derives the exact identity from the signed
// release itself when expected is nil, as publishers do when authoring a target.
func prepareSecurityReleaseVersion(ctx context.Context, version string, expected *releaseidentity.Identity, directory string) (preparedSecurityRelease, error) {
	var result preparedSecurityRelease
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return result, err
	}
	installer, err := newSecurityImageInstaller(directory, pinned)
	if err != nil {
		return result, err
	}
	client, err := updatecheck.NewHTTPClient(60 * time.Second)
	if err != nil {
		return result, err
	}
	source := updatecheck.NewGitHubSource(client)
	release, err := source.ReleaseByVersion(ctx, version)
	if err != nil {
		return result, err
	}
	channel := updatecheck.ChannelStable
	if expected != nil && expected.Profile == releaseidentity.NightlyV1 || expected == nil && release.Prerelease {
		channel = updatecheck.ChannelNightly
	}
	status := updatecheck.Status{Available: true, Channel: channel, LatestVersion: release.Version, Prerelease: release.Prerelease, Immutable: release.Immutable, Assets: release.Assets, URL: release.URL}
	var target selfupdate.PreparedNightly
	if expected == nil {
		target, err = installer.PrepareRelease(ctx, status)
	} else {
		target, err = installer.PrepareReleaseSource(ctx, status, *expected)
	}
	if err != nil {
		return result, err
	}
	if expected != nil && target.Identity != *expected {
		return result, errors.New("independent release verification differs from security target")
	}
	bundle, err := upgradebundle.Load(ctx, target.BundleDirectory, pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		return result, err
	}
	verified, err := bundle.Release(target.Identity)
	if err != nil {
		return result, err
	}
	return preparedSecurityRelease{Installer: installer, Pinned: pinned, Source: source, Target: target, Bundle: bundle, Release: verified}, nil
}
