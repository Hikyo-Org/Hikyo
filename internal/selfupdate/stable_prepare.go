package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Masterminds/semver/v3"
	"github.com/gofrs/flock"
)

// PrepareRelease prepares the selected immutable release without installing it.
// Stable selection deliberately does not follow latest: container callers bind
// the returned proof and executable digest to their own running build.
func (i *Installer) PrepareRelease(ctx context.Context, status updatecheck.Status) (PreparedNightly, error) {
	if status.Channel == updatecheck.ChannelNightly {
		return i.PrepareNightly(ctx, status)
	}
	return i.prepareStableRelease(ctx, status, nil)
}

// PrepareReleaseSource permits historical evidence only for an exact identity
// already selected from authenticated compatibility declarations.
func (i *Installer) PrepareReleaseSource(ctx context.Context, status updatecheck.Status, expected releaseidentity.Identity) (PreparedNightly, error) {
	if expected.Validate() != nil {
		return PreparedNightly{}, errors.New("selfupdate: exact source identity is required")
	}
	if expected.Profile == releaseidentity.NightlyV1 {
		return i.PrepareNightlySource(ctx, status, expected)
	}
	if expected.Profile != releaseidentity.StableV1 {
		return PreparedNightly{}, errors.New("selfupdate: unsupported source profile")
	}
	return i.prepareStableRelease(ctx, status, &expected)
}

func (i *Installer) prepareStableRelease(ctx context.Context, status updatecheck.Status, expected *releaseidentity.Identity) (_ PreparedNightly, err error) {
	if i == nil || i.client == nil || i.config.StateDir == "" {
		return PreparedNightly{}, errors.New("selfupdate: installer is not configured")
	}
	version, err := semver.StrictNewVersion(status.LatestVersion)
	if err != nil || version.Prerelease() != "" || status.Prerelease || status.Channel != updatecheck.ChannelStable || !status.Immutable {
		return PreparedNightly{}, errors.New("selfupdate: select an immutable stable release")
	}
	if len(status.Assets) < 4 || len(status.Assets) > releasetrust.MaxArtifacts+32 {
		return PreparedNightly{}, errors.New("selfupdate: stable asset inventory exceeds bound")
	}
	if err := os.MkdirAll(i.config.StateDir, 0700); err != nil {
		return PreparedNightly{}, err
	}
	if err := realNightlyDirectory(i.config.StateDir); err != nil {
		return PreparedNightly{}, err
	}
	lock := flock.New(filepath.Join(i.config.StateDir, "nightly-trust.lock"))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return PreparedNightly{}, errors.Join(err, errors.New("selfupdate: another release verification is running"))
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	known, err := readPreparedStableState(filepath.Join(i.config.StateDir, "stable-prepared-trust.json"))
	if err != nil {
		return PreparedNightly{}, err
	}
	pinned, err := i.preparedPinnedTrust()
	if err != nil {
		return PreparedNightly{}, err
	}
	floor, err := i.preparedSnapshotFloor(known.Floor)
	if err != nil {
		return PreparedNightly{}, err
	}
	material, snapshot, err := i.downloadSnapshot(ctx, pinned, floor, false)
	if err != nil {
		return PreparedNightly{}, err
	}
	stage, err := os.MkdirTemp(i.config.StateDir, ".stable-download-")
	if err != nil {
		return PreparedNightly{}, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(stage)) }()
	release, err := i.downloadPreparedStable(ctx, status, stage, snapshot)
	if err != nil {
		return PreparedNightly{}, err
	}
	identity := release.Identity()
	if identity.Version != status.LatestVersion || (expected != nil && identity != *expected) {
		return PreparedNightly{}, errors.New("selfupdate: stable manifest differs from selected exact release")
	}
	if (expected == nil && known.Release.Sequence > identity.Sequence) || (known.Release.Sequence == identity.Sequence && known.Release != identity) {
		return PreparedNightly{}, errors.New("selfupdate: stable rollback or equivocation refused")
	}
	destination := filepath.Join(i.config.StateDir, "stable-"+string(identity.ManifestSHA256)+"-"+runtime.GOOS+"-"+runtime.GOARCH)
	if _, statErr := os.Lstat(destination); statErr == nil {
		verified, verifyErr := verifyPreparedStableDirectory(destination, snapshot)
		if verifyErr != nil || verified.Identity() != identity {
			return PreparedNightly{}, errors.New("selfupdate: existing stable staging differs from verified release")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return PreparedNightly{}, statErr
	} else if err := publishNightlyDirectory(stage, destination); err != nil {
		return PreparedNightly{}, err
	}
	if err := filedurability.SyncDirectory(i.config.StateDir); err != nil {
		return PreparedNightly{}, err
	}
	prepared := PreparedNightly{Identity: identity, Directory: destination}
	prepared.BinaryPath, prepared.BinarySHA256, err = i.extractPreparedBinary(ctx, destination, release)
	if err != nil {
		return PreparedNightly{}, err
	}
	prepared.BundleDirectory, err = i.assembleReleaseEvidence(ctx, []PreparedNightly{prepared}, material, snapshot, pinned)
	if err != nil {
		return PreparedNightly{}, err
	}
	if known.Release.Sequence < identity.Sequence {
		known.Release = identity
	}
	known.Floor = snapshot.Floor()
	return prepared, saveNightlyState(filepath.Join(i.config.StateDir, "stable-prepared-trust.json"), known)
}

func (i *Installer) preparedPinnedTrust() (releasetrust.PinnedTrust, error) {
	root, err := decodeStamped("trust root", i.config.TrustRootBase64)
	if err != nil {
		return releasetrust.PinnedTrust{}, err
	}
	recovery, err := decodeStamped("recovery public key", i.config.RecoveryKeyBase64)
	return releasetrust.PinnedTrust{Root: root, RecoveryPublicKey: recovery}, err
}

func readPreparedStableState(path string) (nightlyVerificationState, error) {
	var state nightlyVerificationState
	raw, err := readNightlyFile(path, maxTrustBytes)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if definitions.DecodeStrict(raw, &state) != nil || state.Floor.Validate() != nil || state.Release.Validate() != nil || state.Release.Profile != releaseidentity.StableV1 {
		return state, errors.New("selfupdate: invalid prepared stable trust state")
	}
	return state, nil
}

func (i *Installer) preparedSnapshotFloor(floor releaseidentity.SnapshotFloor) (releaseidentity.SnapshotFloor, error) {
	nightly, err := readNightlyState(filepath.Join(i.config.StateDir, "nightly-trust.json"))
	if err != nil {
		return floor, err
	}
	stable, err := readPreparedStableState(filepath.Join(i.config.StateDir, "stable-prepared-trust.json"))
	if err != nil {
		return floor, err
	}
	for _, other := range []releaseidentity.SnapshotFloor{nightly.Floor, stable.Floor} {
		if other.MetadataSequence == floor.MetadataSequence && other.MetadataSHA256 != floor.MetadataSHA256 || other.CatalogSequence == floor.CatalogSequence && other.CatalogSHA256 != floor.CatalogSHA256 {
			return floor, errors.New("selfupdate: persisted release snapshots equivocate")
		}
		if other.MetadataSequence > floor.MetadataSequence {
			floor.MetadataSequence, floor.MetadataSHA256 = other.MetadataSequence, other.MetadataSHA256
		}
		if other.CatalogSequence > floor.CatalogSequence {
			floor.CatalogSequence, floor.CatalogSHA256 = other.CatalogSequence, other.CatalogSHA256
		}
		if other.HighestReleaseSequence > floor.HighestReleaseSequence {
			floor.HighestReleaseSequence = other.HighestReleaseSequence
		}
	}
	return floor, nil
}

func (i *Installer) downloadPreparedStable(ctx context.Context, status updatecheck.Status, stage string, snapshot releasetrust.Snapshot) (releasetrust.VerifiedRelease, error) {
	read := func(name string, limit int64) ([]byte, error) {
		raw, err := i.downloadNamedAsset(ctx, status, name, limit)
		if err != nil {
			return nil, err
		}
		file, err := os.OpenFile(filepath.Join(stage, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		_, err = file.Write(raw)
		return raw, errors.Join(err, file.Sync(), file.Close())
	}
	documents := map[string][]byte{}
	for _, name := range []string{"release-manifest.json", "release-manifest.sigstore.json", "release-candidate.json", "upgrade-compatibility.json"} {
		raw, err := read(name, releasetrust.MaxDocumentBytes)
		if err != nil {
			return releasetrust.VerifiedRelease{}, err
		}
		documents[name] = raw
	}
	var claim releasetrust.Manifest
	if err := definitions.DecodeStrict(documents["release-manifest.json"], &claim); err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	if claim.SigningKeyID == releasetrust.StableWorkflowSigner {
		for _, name := range []string{"build-provenance.json", "build-provenance.json.sigstore.json"} {
			if _, err := read(name, releasetrust.MaxDocumentBytes); err != nil {
				return releasetrust.VerifiedRelease{}, err
			}
		}
	}
	release, err := readPreparedStableManifest(stage, snapshot)
	if err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	if release.Identity().Version != status.LatestVersion {
		return releasetrust.VerifiedRelease{}, errors.New("selfupdate: selected stable version differs from manifest")
	}
	// Inventory names and digests come from signed metadata, not guessed URLs.
	for _, artifact := range release.Artifacts() {
		asset, err := exactAsset(status.LatestVersion, artifact.Name, status.Assets)
		if err != nil {
			return releasetrust.VerifiedRelease{}, err
		}
		if asset.Digest != "sha256:"+artifact.SHA256 {
			return releasetrust.VerifiedRelease{}, errors.New("selfupdate: stable discovery digest differs from signed inventory")
		}
	}
	name, err := preparedStableArchive(release)
	if err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	if _, err := read(name, maxArchiveBytes); err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	if _, err := read(name+".sigstore.json", releasetrust.MaxDocumentBytes); err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	if err := filedurability.SyncDirectory(stage); err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	return verifyPreparedStableDirectory(stage, snapshot)
}

func readPreparedStableManifest(directory string, snapshot releasetrust.Snapshot) (releasetrust.VerifiedRelease, error) {
	var material releasetrust.StableMaterial
	for name, output := range map[string]*[]byte{"release-manifest.json": &material.Manifest, "release-manifest.sigstore.json": &material.ManifestSignature, "release-candidate.json": &material.Candidate, "upgrade-compatibility.json": &material.Compatibility} {
		raw, err := readNightlyFile(filepath.Join(directory, name), releasetrust.MaxDocumentBytes)
		if err != nil {
			return releasetrust.VerifiedRelease{}, err
		}
		*output = raw
	}
	var claim releasetrust.Manifest
	if err := definitions.DecodeStrict(material.Manifest, &claim); err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	if claim.SigningKeyID == releasetrust.StableWorkflowSigner {
		for name, output := range map[string]*[]byte{"build-provenance.json": &material.Provenance, "build-provenance.json.sigstore.json": &material.ProvenanceSignature} {
			raw, err := readNightlyFile(filepath.Join(directory, name), releasetrust.MaxDocumentBytes)
			if err != nil {
				return releasetrust.VerifiedRelease{}, err
			}
			*output = raw
		}
	}
	return releasetrust.VerifyStable(snapshot, material)
}

func preparedStableArchive(release releasetrust.VerifiedRelease) (string, error) {
	name, err := archiveName(release.Identity().Version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	for _, artifact := range release.Artifacts() {
		if artifact.Kind == "binary" && artifact.Name == name && (artifact.Platform == "" || artifact.Platform == nightlyPlatform()) {
			return name, nil
		}
	}
	return "", errors.New("selfupdate: no signed stable binary for native platform")
}

func verifyPreparedStableDirectory(directory string, snapshot releasetrust.Snapshot) (releasetrust.VerifiedRelease, error) {
	release, err := readPreparedStableManifest(directory, snapshot)
	if err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	name, err := preparedStableArchive(release)
	if err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	archive, err := readNightlyFile(filepath.Join(directory, name), maxArchiveBytes)
	if err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	if err := release.VerifyArtifact(name, bytes.NewReader(archive)); err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	signature, err := readNightlyFile(filepath.Join(directory, name+".sigstore.json"), releasetrust.MaxDocumentBytes)
	if err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	if err := releasetrust.VerifyStableArtifactSignature(snapshot, release, signature, archive); err != nil {
		return releasetrust.VerifiedRelease{}, fmt.Errorf("selfupdate: stable archive signature: %w", err)
	}
	return release, nil
}
