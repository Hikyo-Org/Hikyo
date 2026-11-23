package releasetrust

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Masterminds/semver/v3"
)

// VerificationLockPath is shared by shell installers and in-process updates.
func VerificationLockPath(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), strings.TrimSuffix(filepath.Base(statePath), ".json")+".lock")
}

// DecodeVerificationFloor preserves the old installer's metadata/highest floor.
// Only the missing catalog component may come from authenticated current trust;
// decoding never treats an older current snapshot as permission to reset state.
func DecodeVerificationFloor(raw []byte, current SnapshotFloor) (SnapshotFloor, error) {
	var floor SnapshotFloor
	if err := definitions.DecodeStrict(raw, &floor); err == nil {
		if floor.MetadataSequence == 0 {
			return SnapshotFloor{}, errors.New("persisted verification state cannot be empty")
		}
		if err := floor.Validate(); err != nil {
			return SnapshotFloor{}, err
		}
		return floor, nil
	}
	var old struct {
		TrustSequence   int64                  `json:"trust_sequence"`
		HighestSequence *int64                 `json:"highest_release_sequence"`
		HighestVersion  *string                `json:"highest_release"`
		MetadataSHA256  releaseidentity.Digest `json:"metadata_sha256"`
		CatalogSequence int64                  `json:"catalog_sequence,omitzero"`
		CatalogSHA256   releaseidentity.Digest `json:"catalog_sha256,omitempty"`
	}
	if err := definitions.DecodeStrict(raw, &old); err != nil {
		return SnapshotFloor{}, err
	}
	if old.TrustSequence < 1 || old.MetadataSHA256.Validate() != nil || (old.HighestSequence == nil) != (old.HighestVersion == nil) || (old.HighestSequence != nil && *old.HighestSequence < 1) || old.CatalogSequence < 0 || (old.CatalogSequence == 0) != (old.CatalogSHA256 == "") {
		return SnapshotFloor{}, errors.New("invalid legacy verification state")
	}
	if old.HighestVersion != nil {
		if _, err := semver.StrictNewVersion(*old.HighestVersion); err != nil {
			return SnapshotFloor{}, err
		}
	}
	floor = SnapshotFloor{MetadataSequence: old.TrustSequence, MetadataSHA256: old.MetadataSHA256, CatalogSequence: old.CatalogSequence, CatalogSHA256: old.CatalogSHA256}
	if old.HighestSequence != nil {
		floor.HighestReleaseSequence = *old.HighestSequence
	}
	if floor.CatalogSequence == 0 {
		floor.CatalogSequence = current.CatalogSequence
		floor.CatalogSHA256 = current.CatalogSHA256
	}
	if err := floor.Validate(); err != nil {
		return SnapshotFloor{}, err
	}
	return floor, nil
}
