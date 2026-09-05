package releasetrust

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// PinnedTrust comes from the installation/build trust configuration, never a
// key or root chosen by downloaded evidence. The verifier copies all material.
type PinnedTrust struct {
	Root              []byte
	RecoveryPublicKey []byte
}

type SnapshotMaterial struct {
	Metadata          []byte
	MetadataSignature []byte
	PrimaryKeys       map[string][]byte
	Catalog           []byte
	CatalogSignature  []byte
}

// SnapshotFloor is persisted by the installer/gate. The offline verifier
// checks it but never writes or silently resets rollback/equivocation state.
type SnapshotFloor = releaseidentity.SnapshotFloor

// Catalog is independently recovery-signed and binds the current stable
// metadata plus authorized nightly policies and exceptional bridge bytes.
type Catalog struct {
	Schema               string                   `json:"schema"`
	Sequence             int64                    `json:"sequence"`
	StableMetadataSHA256 releaseidentity.Digest   `json:"stable_metadata_sha256"`
	NightlyPolicies      []releaseidentity.Digest `json:"nightly_policies"`
	Bridges              []releaseidentity.Digest `json:"bridges"`
}

type snapshotState struct {
	root         Root
	metadata     Metadata
	keys         map[string][]byte
	recoveryKey  []byte
	catalog      Catalog
	floor        SnapshotFloor
	id           releaseidentity.Digest
	stablePolicy releaseidentity.Digest
}

// Snapshot can only be created by successful signature and policy validation.
// No accessor exposes mutable metadata, key material or authority maps.
type Snapshot struct{ state *snapshotState }

func (s Snapshot) Valid() bool { return s.state != nil }
func (s Snapshot) Digest() releaseidentity.Digest {
	if s.state == nil {
		return ""
	}
	return s.state.id
}
func (s Snapshot) Floor() SnapshotFloor {
	if s.state == nil {
		return SnapshotFloor{}
	}
	return s.state.floor
}

// BridgeDigests is the complete current exceptional-edge inventory. Planning
// must authenticate all statements so omitting one cannot bypass precedence.
func (s Snapshot) BridgeDigests() []releaseidentity.Digest {
	if !s.Valid() {
		return nil
	}
	return slices.Clone(s.state.catalog.Bridges)
}

func VerifySnapshot(pinned PinnedTrust, material SnapshotMaterial, floor SnapshotFloor) (Snapshot, error) {
	if err := floor.Validate(); err != nil {
		return Snapshot{}, err
	}
	total := len(pinned.Root) + len(pinned.RecoveryPublicKey) + len(material.Metadata) + len(material.MetadataSignature) + len(material.Catalog) + len(material.CatalogSignature)
	if len(material.PrimaryKeys) > 256 {
		return Snapshot{}, errors.New("trust key inventory exceeds bound")
	}
	for _, raw := range material.PrimaryKeys {
		if len(raw) > MaxDocumentBytes {
			return Snapshot{}, errors.New("trust key exceeds byte bound")
		}
		total += len(raw)
	}
	if total > 64<<20 {
		return Snapshot{}, errors.New("trust snapshot exceeds aggregate byte bound")
	}
	var root Root
	if err := decodeDocument(pinned.Root, &root); err != nil {
		return Snapshot{}, err
	}
	if err := ValidateRoot(root, pinned.RecoveryPublicKey); err != nil {
		return Snapshot{}, err
	}
	if _, err := parsePinnedKey(pinned.RecoveryPublicKey); err != nil {
		return Snapshot{}, err
	}
	if err := VerifyKeySignature(pinned.RecoveryPublicKey, material.MetadataSignature, material.Metadata); err != nil {
		return Snapshot{}, fmt.Errorf("metadata signature: %w", err)
	}
	var metadata Metadata
	if err := decodeDocument(material.Metadata, &metadata); err != nil {
		return Snapshot{}, err
	}
	if err := ValidateMetadata(root, metadata); err != nil {
		return Snapshot{}, err
	}
	metadataDigest := releaseidentity.Hash(material.Metadata)
	if err := checkFloor(metadata.Sequence, metadataDigest, floor.MetadataSequence, floor.MetadataSHA256); err != nil {
		return Snapshot{}, fmt.Errorf("metadata: %w", err)
	}
	highest := int64(0)
	if metadata.HighestReleaseSequence != nil {
		highest = *metadata.HighestReleaseSequence
	}
	if highest < floor.HighestReleaseSequence {
		return Snapshot{}, errors.New("highest-release rollback refused")
	}
	if len(metadata.PrimaryKeys) > 256 {
		return Snapshot{}, errors.New("trust key inventory exceeds bound")
	}
	keys := make(map[string][]byte, len(metadata.PrimaryKeys))
	for _, primary := range metadata.PrimaryKeys {
		raw, exists := material.PrimaryKeys[primary.ID]
		if !exists && (primary.Revoked || (primary.Pending != nil && *primary.Pending)) {
			continue
		}
		if !exists || len(raw) > MaxDocumentBytes || digestHex(raw) != primary.SHA256 {
			return Snapshot{}, fmt.Errorf("primary key unavailable or digest mismatch: %s", primary.ID)
		}
		if _, err := parsePinnedKey(raw); err != nil {
			return Snapshot{}, err
		}
		keys[primary.ID] = bytes.Clone(raw)
	}
	var catalog Catalog
	if err := VerifyKeySignature(pinned.RecoveryPublicKey, material.CatalogSignature, material.Catalog); err != nil {
		return Snapshot{}, fmt.Errorf("upgrade catalog signature: %w", err)
	}
	if err := decodeDocument(material.Catalog, &catalog); err != nil {
		return Snapshot{}, err
	}
	if catalog.Schema != "hikyo.dev/upgrade-trust/v1" || catalog.Sequence < 1 || catalog.StableMetadataSHA256 != metadataDigest {
		return Snapshot{}, errors.New("upgrade catalog does not bind current stable metadata")
	}
	if err := digestInventory(catalog.NightlyPolicies, 256); err != nil {
		return Snapshot{}, err
	}
	if err := digestInventory(catalog.Bridges, 1024); err != nil {
		return Snapshot{}, err
	}
	catalogDigest := releaseidentity.Hash(material.Catalog)
	if err := checkFloor(catalog.Sequence, catalogDigest, floor.CatalogSequence, floor.CatalogSHA256); err != nil {
		return Snapshot{}, fmt.Errorf("upgrade catalog: %w", err)
	}
	nextFloor := SnapshotFloor{MetadataSequence: metadata.Sequence, MetadataSHA256: metadataDigest, HighestReleaseSequence: highest, CatalogSequence: catalog.Sequence, CatalogSHA256: catalogDigest}
	stablePolicy := releaseidentity.Hash(pinned.Root)
	identityBytes := []byte(string(stablePolicy) + ":" + string(metadataDigest) + ":" + string(catalogDigest))
	return Snapshot{state: &snapshotState{root: root, metadata: metadata, keys: keys, recoveryKey: bytes.Clone(pinned.RecoveryPublicKey), catalog: catalog, floor: nextFloor, id: releaseidentity.Hash(identityBytes), stablePolicy: stablePolicy}}, nil
}

func checkFloor(sequence int64, digest releaseidentity.Digest, known int64, knownDigest releaseidentity.Digest) error {
	if known < 0 || (known == 0) != (knownDigest == "") {
		return errors.New("invalid persisted trust floor")
	}
	if known > 0 && knownDigest.Validate() != nil {
		return errors.New("invalid persisted trust digest")
	}
	if sequence < known {
		return errors.New("trust rollback refused")
	}
	if sequence == known && digest != knownDigest {
		return errors.New("conflicting trust bytes at known sequence")
	}
	return nil
}

func digestInventory(entries []releaseidentity.Digest, limit int) error {
	if entries == nil || len(entries) > limit {
		return errors.New("digest inventory must be a bounded non-null array")
	}
	seen := map[releaseidentity.Digest]bool{}
	for _, digest := range entries {
		if digest.Validate() != nil || seen[digest] {
			return errors.New("digest inventory contains invalid or duplicate entries")
		}
		seen[digest] = true
	}
	return nil
}
