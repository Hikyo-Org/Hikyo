package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/gofrs/flock"
)

// StagedNightly reports a verified manual-upgrade download, never replacement
// of the running executable. Installing a server also requires its local gate,
// backup/drill evidence and complete runtime bundle.
type StagedNightly struct {
	Directory       string
	BundleDirectory string
}

func (s *StagedNightly) Error() string {
	return fmt.Sprintf("Signed nightly verified at %s. Runtime bundle assembled at %s. Follow https://hikyo.app/docs/upgrades/ to complete the server upgrade; current binary preserved.", s.Directory, s.BundleDirectory)
}

type nightlyVerificationState struct {
	Floor   releaseidentity.SnapshotFloor `json:"floor"`
	Release releaseidentity.Identity      `json:"release"`
}

func (i *Installer) stageNightly(ctx context.Context, status updatecheck.Status) error {
	var prepared PreparedNightly
	if err := i.prepareNightly(ctx, status, nil, false, &prepared); err != nil {
		return err
	}
	return &StagedNightly{Directory: prepared.Directory, BundleDirectory: prepared.BundleDirectory}
}

func (i *Installer) prepareNightly(ctx context.Context, status updatecheck.Status, expected *releaseidentity.Identity, extract bool, prepared *PreparedNightly) (err error) {
	if !status.Immutable {
		return errors.New("selfupdate: nightly release is not immutable")
	}
	rootRaw, err := decodeStamped("trust root", i.config.TrustRootBase64)
	if err != nil {
		return err
	}
	recovery, err := decodeStamped("recovery public key", i.config.RecoveryKeyBase64)
	if err != nil {
		return err
	}
	if i.config.StateDir == "" {
		return errors.New("selfupdate: nightly trust state directory is unavailable")
	}
	if len(status.Assets) < 3 || len(status.Assets) > releasetrust.MaxArtifacts+2 {
		return errors.New("selfupdate: nightly asset inventory exceeds bound")
	}
	if err := os.MkdirAll(i.config.StateDir, 0700); err != nil {
		return err
	}
	if err := realNightlyDirectory(i.config.StateDir); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(i.config.StateDir, "nightly-trust.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("selfupdate: another nightly verification is running")
	}
	defer func() {
		if unlockErr := lock.Unlock(); unlockErr != nil {
			// A staging notice is treated as success by the CLI. Do not hide a
			// lock failure inside that notice's error chain.
			err = fmt.Errorf("selfupdate: release nightly lock: %w", unlockErr)
		}
	}()
	statePath := filepath.Join(i.config.StateDir, "nightly-trust.json")
	known, err := readNightlyState(statePath)
	if err != nil {
		return err
	}
	pinned := releasetrust.PinnedTrust{Root: rootRaw, RecoveryPublicKey: recovery}
	material, snapshot, err := i.nightlySnapshot(ctx, pinned, known.Floor)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(i.config.StateDir, ".nightly-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	// A target handoff and exact historical route reads reuse immutable cached
	// bytes. They still match every discovery digest and re-run full signature
	// verification against the fresh snapshot below before granting authority.
	cacheIdentity := known.Release
	if expected != nil {
		cacheIdentity = *expected
	}
	cachedDirectory := ""
	if cacheIdentity.Validate() == nil && cacheIdentity.Version == status.LatestVersion {
		candidate := filepath.Join(i.config.StateDir, "nightly-"+string(cacheIdentity.ManifestSHA256))
		if err := realNightlyDirectory(candidate); err == nil {
			cachedDirectory = candidate
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	var total int64
	for _, candidate := range status.Assets {
		if !safeName(candidate.Name) {
			return errors.New("selfupdate: unsafe nightly payload name")
		}
		asset, err := exactAsset(status.LatestVersion, candidate.Name, status.Assets)
		if err != nil {
			return err
		}
		total += asset.Size
		if asset.Size > maxArchiveBytes || total > 8<<30 {
			return errors.New("selfupdate: nightly payload inventory exceeds byte bound")
		}
		var raw []byte
		if cachedDirectory == "" {
			raw, err = i.download(ctx, asset, maxArchiveBytes)
		} else {
			raw, err = readNightlyFile(filepath.Join(cachedDirectory, asset.Name), maxArchiveBytes)
			if err == nil && (int64(len(raw)) != asset.Size || "sha256:"+string(releaseidentity.Hash(raw)) != asset.Digest) {
				err = errors.New("selfupdate: cached nightly differs from immutable release asset inventory")
			}
		}
		if err != nil {
			return err
		}
		file, err := os.OpenFile(filepath.Join(stage, asset.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = file.Write(raw)
		if err := errors.Join(err, file.Sync(), file.Close()); err != nil {
			return err
		}
	}
	release, err := upgradebundle.VerifyNightlyDirectory(ctx, stage, snapshot)
	if err != nil {
		return fmt.Errorf("selfupdate: authenticate complete nightly: %w", err)
	}
	identity := release.Identity()
	if identity.Version != status.LatestVersion {
		return errors.New("selfupdate: nightly manifest differs from selected release")
	}
	if expected != nil && identity != *expected {
		return errors.New("selfupdate: nightly manifest differs from exact requested evidence")
	}
	if (expected == nil && known.Release.Sequence > identity.Sequence) || (known.Release.Sequence == identity.Sequence && known.Release != identity) {
		return errors.New("selfupdate: nightly rollback or equivocation refused")
	}
	if err := filedurability.SyncDirectory(stage); err != nil {
		return err
	}
	destination := filepath.Join(i.config.StateDir, "nightly-"+string(identity.ManifestSHA256))
	if _, statErr := os.Lstat(destination); statErr == nil {
		if err := realNightlyDirectory(destination); err != nil {
			return err
		}
		verified, err := upgradebundle.VerifyNightlyDirectory(ctx, destination, snapshot)
		if err != nil || verified.Identity() != identity {
			return errors.New("selfupdate: existing nightly staging differs from verified release")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	} else if err := publishNightlyDirectory(stage, destination); err != nil {
		return err
	}
	if err := filedurability.SyncDirectory(i.config.StateDir); err != nil {
		return err
	}
	bundleDirectory, err := i.assembleNightlyBundle(ctx, destination, material, snapshot, releasetrust.PinnedTrust{Root: rootRaw, RecoveryPublicKey: recovery}, identity)
	if err != nil {
		return fmt.Errorf("selfupdate: assemble runtime bundle: %w", err)
	}
	prepared.Identity, prepared.Directory, prepared.BundleDirectory = identity, destination, bundleDirectory
	if extract {
		prepared.BinaryPath, prepared.BinarySHA256, err = i.extractPreparedBinary(ctx, destination, release)
		if err != nil {
			return err
		}
	}
	highest := identity
	if known.Release.Sequence > highest.Sequence {
		highest = known.Release
	}
	return saveNightlyState(statePath, nightlyVerificationState{Floor: snapshot.Floor(), Release: highest})
}

func readNightlyState(statePath string) (nightlyVerificationState, error) {
	var known nightlyVerificationState
	raw, err := readNightlyFile(statePath, maxTrustBytes)
	if errors.Is(err, os.ErrNotExist) {
		return known, nil
	}
	if err != nil {
		return known, err
	}
	if definitions.DecodeStrict(raw, &known) != nil || known.Floor.Validate() != nil || known.Release.Validate() != nil || known.Release.Profile != releaseidentity.NightlyV1 {
		return known, errors.New("selfupdate: invalid nightly trust state")
	}
	return known, nil
}

func saveNightlyState(statePath string, state nightlyVerificationState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(statePath), ".nightly-trust-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(raw)
	if err = errors.Join(err, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), statePath); err != nil {
		return err
	}
	if err := filedurability.SyncDirectory(filepath.Dir(statePath)); err != nil {
		return err
	}
	return nil
}

func (i *Installer) nightlySnapshot(ctx context.Context, pinned releasetrust.PinnedTrust, floor releaseidentity.SnapshotFloor) (releasetrust.SnapshotMaterial, releasetrust.Snapshot, error) {
	var err error
	material := releasetrust.SnapshotMaterial{PrimaryKeys: map[string][]byte{}}
	for name, target := range map[string]*[]byte{"metadata.json": &material.Metadata, "metadata.sigstore.json": &material.MetadataSignature, "catalog.json": &material.Catalog, "catalog.sigstore.json": &material.CatalogSignature} {
		*target, err = i.downloadURL(ctx, trustURL(name), maxTrustBytes)
		if err != nil {
			return releasetrust.SnapshotMaterial{}, releasetrust.Snapshot{}, err
		}
	}
	var metadata releasetrust.Metadata
	if err := definitions.DecodeStrict(material.Metadata, &metadata); err != nil {
		return releasetrust.SnapshotMaterial{}, releasetrust.Snapshot{}, err
	}
	if len(metadata.PrimaryKeys) > 256 {
		return releasetrust.SnapshotMaterial{}, releasetrust.Snapshot{}, errors.New("selfupdate: primary inventory exceeds bound")
	}
	for _, key := range metadata.PrimaryKeys {
		if !safeName(key.PublicKey) {
			return releasetrust.SnapshotMaterial{}, releasetrust.Snapshot{}, errors.New("selfupdate: unsafe public key locator")
		}
		material.PrimaryKeys[key.ID], err = i.downloadURL(ctx, trustURL(key.PublicKey), maxTrustBytes)
		if err != nil {
			return releasetrust.SnapshotMaterial{}, releasetrust.Snapshot{}, err
		}
	}
	snapshot, err := releasetrust.VerifySnapshot(pinned, material, floor)
	if err != nil {
		return releasetrust.SnapshotMaterial{}, releasetrust.Snapshot{}, err
	}
	return material, snapshot, nil
}
