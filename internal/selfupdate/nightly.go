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
type StagedNightly struct{ Directory string }

func (s *StagedNightly) Error() string {
	return fmt.Sprintf("Signed nightly verified at %s. Follow https://hikyo.app/docs/upgrades/ to assemble the bundle and complete the server upgrade; current binary preserved.", s.Directory)
}

type nightlyVerificationState struct {
	Floor   releaseidentity.SnapshotFloor `json:"floor"`
	Release releaseidentity.Identity      `json:"release"`
}

func (i *Installer) stageNightly(ctx context.Context, status updatecheck.Status) (err error) {
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
	var known nightlyVerificationState
	if info, statErr := os.Lstat(statePath); statErr == nil {
		if !info.Mode().IsRegular() || info.Size() > maxTrustBytes {
			return errors.New("selfupdate: invalid nightly trust state")
		}
		raw, readErr := os.ReadFile(statePath)
		if readErr != nil {
			return readErr
		}
		if definitions.DecodeStrict(raw, &known) != nil || known.Floor.Validate() != nil || known.Release.Validate() != nil || known.Release.Profile != releaseidentity.NightlyV1 {
			return errors.New("selfupdate: invalid nightly trust state")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	material := releasetrust.SnapshotMaterial{PrimaryKeys: map[string][]byte{}}
	for name, target := range map[string]*[]byte{"metadata.json": &material.Metadata, "metadata.sigstore.json": &material.MetadataSignature, "catalog.json": &material.Catalog, "catalog.sigstore.json": &material.CatalogSignature} {
		*target, err = i.downloadURL(ctx, trustURL(name), maxTrustBytes)
		if err != nil {
			return err
		}
	}
	var metadata releasetrust.Metadata
	if err := definitions.DecodeStrict(material.Metadata, &metadata); err != nil {
		return err
	}
	if len(metadata.PrimaryKeys) > 256 {
		return errors.New("selfupdate: primary inventory exceeds bound")
	}
	for _, key := range metadata.PrimaryKeys {
		if !safeName(key.PublicKey) {
			return errors.New("selfupdate: unsafe public key locator")
		}
		material.PrimaryKeys[key.ID], err = i.downloadURL(ctx, trustURL(key.PublicKey), maxTrustBytes)
		if err != nil {
			return err
		}
	}
	snapshot, err := releasetrust.VerifySnapshot(releasetrust.PinnedTrust{Root: rootRaw, RecoveryPublicKey: recovery}, material, known.Floor)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(i.config.StateDir, ".nightly-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
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
		raw, err := i.download(ctx, asset, maxArchiveBytes)
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
	if known.Release.Sequence > identity.Sequence || (known.Release.Sequence == identity.Sequence && known.Release != identity) {
		return errors.New("selfupdate: nightly rollback or equivocation refused")
	}
	if err := filedurability.SyncDirectory(stage); err != nil {
		return err
	}
	destination := filepath.Join(i.config.StateDir, "nightly-"+string(identity.ManifestSHA256))
	if _, statErr := os.Lstat(destination); statErr == nil {
		verified, err := upgradebundle.VerifyNightlyDirectory(ctx, destination, snapshot)
		if err != nil || verified.Identity() != identity {
			return errors.New("selfupdate: existing nightly staging differs from verified release")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	} else if err := os.Rename(stage, destination); err != nil {
		return err
	}
	if err := filedurability.SyncDirectory(i.config.StateDir); err != nil {
		return err
	}
	raw, err := json.Marshal(nightlyVerificationState{Floor: snapshot.Floor(), Release: identity})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(i.config.StateDir, ".nightly-trust-")
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
	if err := filedurability.SyncDirectory(i.config.StateDir); err != nil {
		return err
	}
	return &StagedNightly{Directory: destination}
}
