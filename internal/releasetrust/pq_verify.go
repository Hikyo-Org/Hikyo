package releasetrust

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// PQFloor is the persisted claim that a verification required every algorithm
// of a pinned set. It is a SEPARATE record from SnapshotFloor, which is
// unchanged: SnapshotFloor lives in the upgrade control row and the shared
// `release-trust.json`, and a new member there is a migration on both engines.
// Like SnapshotFloor it is an untrusted local claim, not signature authority,
// and only the verifiers in this package produce a value above the zero floor.
// Persisting it on a real host is NOT BUILT (E-17): no install path stores it.
type PQFloor struct {
	Format           int                    `json:"format"`
	Profile          string                 `json:"profile"`
	Generation       int64                  `json:"generation"`
	PinSHA256        releaseidentity.Digest `json:"pin_sha256"`
	KeysetSequence   int64                  `json:"keyset_sequence"`
	KeysetSHA256     releaseidentity.Digest `json:"keyset_sha256"`
	MetadataSequence int64                  `json:"metadata_sequence"`
	MetadataSHA256   releaseidentity.Digest `json:"metadata_sha256"`
	CatalogSequence  int64                  `json:"catalog_sequence"`
	CatalogSHA256    releaseidentity.Digest `json:"catalog_sha256"`
	ReleaseSequence  int64                  `json:"release_sequence"`
}

func (f PQFloor) zero() bool { return f == (PQFloor{}) }

// Validate refuses a record this reader cannot interpret: a newer format is
// the format floor, so an older reader fails instead of resetting it.
func (f PQFloor) Validate() error {
	if f.zero() {
		return nil
	}
	if f.Format > PQFormatVersion {
		return pqErr(ErrPQUnsupported, "persisted PQ floor format is newer than this reader")
	}
	if f.Format != PQFormatVersion || f.Profile != PQProfile || f.Generation < 1 || f.PinSHA256.Validate() != nil ||
		f.KeysetSequence < 0 || f.MetadataSequence < 0 || f.CatalogSequence < 0 || f.ReleaseSequence < 0 {
		return pqErr(ErrPQMalformed, "persisted PQ floor")
	}
	for _, pair := range []struct {
		sequence int64
		digest   releaseidentity.Digest
	}{{f.KeysetSequence, f.KeysetSHA256}, {f.MetadataSequence, f.MetadataSHA256}, {f.CatalogSequence, f.CatalogSHA256}} {
		if (pair.sequence == 0) != (pair.digest == "") || (pair.digest != "" && pair.digest.Validate() != nil) {
			return pqErr(ErrPQMalformed, "persisted PQ floor pair")
		}
	}
	return nil
}

// check binds the floor to the verifier's own pin. A pin below the persisted
// generation is a rollback to a verifier below the floor; the same generation
// with different pin bytes is equivocation. A higher generation is an
// operator-installed root replacement (offline recovery) and keeps sequences.
func (f PQFloor) check(pin PQPin) error {
	if !pin.Valid() {
		return pqErr(ErrPQScope, "unpinned PQ trust")
	}
	if err := f.Validate(); err != nil {
		return err
	}
	if f.zero() {
		return nil
	}
	if pin.generation < f.Generation || (pin.generation == f.Generation && pin.digest != f.PinSHA256) {
		return pqErr(ErrPQRollback, "pinned PQ generation or bytes")
	}
	return nil
}

func (f PQFloor) withPin(pin PQPin) PQFloor {
	f.Format, f.Profile, f.Generation, f.PinSHA256 = PQFormatVersion, PQProfile, pin.generation, pin.digest
	return f
}

// AuthenticityClaim reports whether this floor records a complete PQ
// verification (keyset, metadata, catalog and a release) under exactly this
// pin. Anything less cannot support a pqc-auth claim. It does not lift E-11 or
// E-15: how the pin first reached this host is outside the check.
func (f PQFloor) AuthenticityClaim(pin PQPin) error {
	if err := f.check(pin); err != nil {
		return err
	}
	if f.zero() || f.Generation != pin.generation || f.KeysetSequence < 1 || f.MetadataSequence < 1 || f.CatalogSequence < 1 || f.ReleaseSequence < 1 {
		return pqErr(ErrPQMissing, "no persisted verification that required every algorithm")
	}
	return nil
}

func EncodePQFloor(f PQFloor) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(f)
	return append(raw, '\n'), err
}

func DecodePQFloor(raw []byte) (PQFloor, error) {
	var f PQFloor
	if len(raw) == 0 || len(raw) > 4096 || definitions.DecodeStrict(raw, &f) != nil {
		return PQFloor{}, pqErr(ErrPQMalformed, "persisted PQ floor")
	}
	return f, f.Validate()
}

// PQFloorPath is the sibling of the shared SnapshotFloor state file, guarded by
// the same verification lock. Nothing writes it on a shipping path yet.
func PQFloorPath(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), strings.TrimSuffix(filepath.Base(statePath), ".json")+"-pq.json")
}

// Advance refuses counter rollback and equivocation on every sequence.
func (f PQFloor) Advance(next PQFloor) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if f.zero() {
		return nil
	}
	if next.Generation < f.Generation || (next.Generation == f.Generation && next.PinSHA256 != f.PinSHA256) {
		return pqErr(ErrPQRollback, "generation")
	}
	for _, pair := range []struct {
		old, new int64
		od, nd   releaseidentity.Digest
	}{{f.KeysetSequence, next.KeysetSequence, f.KeysetSHA256, next.KeysetSHA256}, {f.MetadataSequence, next.MetadataSequence, f.MetadataSHA256, next.MetadataSHA256}, {f.CatalogSequence, next.CatalogSequence, f.CatalogSHA256, next.CatalogSHA256}} {
		if pair.new < pair.old || (pair.new == pair.old && pair.nd != pair.od) {
			return pqErr(ErrPQRollback, "sequence or bytes")
		}
	}
	if next.ReleaseSequence < f.ReleaseSequence {
		return pqErr(ErrPQRollback, "release sequence")
	}
	return nil
}

// PQSnapshotSets is the PQ evidence that rides beside a v1 snapshot's material.
type PQSnapshotSets struct {
	Keyset, KeysetSet []byte
	KeyFiles          map[string][]byte
	MetadataSet       []byte
	CatalogSet        []byte
	StablePolicySet   []byte
	NightlyPolicySet  []byte
}

// linkedRecovery requires the PQ pin to extend exactly the v1 root and recovery
// key the legacy snapshot was verified under, so a snapshot cannot be paired
// with a different PQ root.
func (p PQPin) linkedRecovery(snapshot Snapshot) error {
	if !p.Valid() || !snapshot.Valid() || snapshot.state.root.Recovery != p.root.Recovery || snapshot.state.root.BootstrapPrimary != p.root.BootstrapPrimary {
		return pqErr(ErrPQScope, "PQ pin does not extend the snapshot's pinned root")
	}
	key, err := parsePQKey(AlgorithmECDSAP256, snapshot.state.recoveryKey)
	if err != nil || key.id != p.recovery[0].key.id {
		return pqErr(ErrPQScope, "classical recovery key differs")
	}
	return nil
}

// VerifyPQSnapshot adds the PQ requirement to a snapshot that VerifySnapshot has
// already authenticated under the unchanged v1 rules. Recovery-signed documents
// (policy, legacy-track metadata and catalog) need the full recovery set.
// Keyless-signed stable metadata and catalog keep Sigstore (E-03) and gain one
// maintainer-held ML-DSA release signature; CI never holds either key.
func VerifyPQSnapshot(pin PQPin, snapshot Snapshot, material SnapshotMaterial, sets PQSnapshotSets, floor PQFloor) (VerifiedPQKeyset, PQFloor, error) {
	if err := pin.linkedRecovery(snapshot); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, err
	}
	keyset, next, err := VerifyPQKeyset(pin, floor, sets.Keyset, sets.KeysetSet, sets.KeyFiles)
	if err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, err
	}
	state := snapshot.state
	if releaseidentity.Hash(material.Metadata) != state.floor.MetadataSHA256 || releaseidentity.Hash(material.Catalog) != state.floor.CatalogSHA256 {
		return VerifiedPQKeyset{}, PQFloor{}, pqErr(ErrPQScope, "PQ evidence differs from the authenticated snapshot")
	}
	version := ""
	if state.metadata.HighestRelease != nil {
		version = *state.metadata.HighestRelease
	}
	trust := func(kind, subject string, sequence int64, policy releaseidentity.Digest) PQBinding {
		return PQBinding{Kind: kind, Channel: string(releaseidentity.StableV1), Version: version, Subject: subject, Source: state.metadata.SourceCommit, Sequence: uint64(sequence), Policy: policy}
	}
	signers := pin.recovery
	if state.metadata.Event.SignedBy == StableWorkflowSigner {
		highest := int64(1)
		if state.metadata.HighestReleaseSequence != nil {
			highest = *state.metadata.HighestReleaseSequence
		}
		if signers, err = keyset.releaseSet(highest); err != nil {
			return VerifiedPQKeyset{}, PQFloor{}, err
		}
	}
	if err := verifyPQSet(pin, signers, trust(PQKindMetadata, "metadata.json", state.metadata.Sequence, state.stablePolicy), sha256Of(material.Metadata), sets.MetadataSet); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, err
	}
	if err := verifyPQSet(pin, signers, trust(PQKindCatalog, "catalog.json", state.catalog.Sequence, state.stablePolicy), sha256Of(material.Catalog), sets.CatalogSet); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, err
	}
	if state.workflowPolicy != nil {
		// A delegation grants CI no authority over itself: only the recovery set signs it.
		binding := trust(PQKindStablePolicy, "stable-policy.json", state.workflowPolicy.MinimumReleaseSequence, state.legacyStablePolicy)
		if err := verifyPQSet(pin, pin.recovery, binding, sha256Of(material.StablePolicy), sets.StablePolicySet); err != nil {
			return VerifiedPQKeyset{}, PQFloor{}, err
		}
	}
	if material.NightlyPolicy != nil {
		binding := trust(PQKindNightlyPolicy, "nightly-policy.json", state.catalog.Sequence, state.stablePolicy)
		if err := verifyPQSet(pin, pin.recovery, binding, sha256Of(material.NightlyPolicy), sets.NightlyPolicySet); err != nil {
			return VerifiedPQKeyset{}, PQFloor{}, err
		}
	}
	if err := checkFloor(state.metadata.Sequence, state.floor.MetadataSHA256, floor.MetadataSequence, floor.MetadataSHA256); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, pqErr(ErrPQRollback, "metadata sequence")
	}
	if err := checkFloor(state.catalog.Sequence, state.floor.CatalogSHA256, floor.CatalogSequence, floor.CatalogSHA256); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, pqErr(ErrPQRollback, "catalog sequence")
	}
	next.MetadataSequence, next.MetadataSHA256 = state.metadata.Sequence, state.floor.MetadataSHA256
	next.CatalogSequence, next.CatalogSHA256 = state.catalog.Sequence, state.floor.CatalogSHA256
	if err := floor.Advance(next); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, err
	}
	return keyset, next, nil
}

// VerifyPQRelease requires the maintainer-held ML-DSA signature over the exact
// manifest bytes, bound to channel, version, sequence, source commit and policy.
// Historical keyed releases and nightlies have no PQ signer (E-12, E-15) and are
// refused here rather than reported as verified.
func VerifyPQRelease(pin PQPin, keyset VerifiedPQKeyset, snapshot Snapshot, release VerifiedRelease, manifestRaw, setRaw []byte, floor PQFloor) (PQFloor, error) {
	if err := floor.check(pin); err != nil {
		return PQFloor{}, err
	}
	if !snapshot.Valid() || !release.Valid() || release.SnapshotDigest() != snapshot.Digest() {
		return PQFloor{}, pqErr(ErrPQScope, "release does not belong to this snapshot")
	}
	identity := release.Identity()
	if identity.Profile != releaseidentity.StableV1 || release.state.signingKeyID != StableWorkflowSigner {
		return PQFloor{}, pqErr(ErrPQScope, "no PQ signer exists for this channel or historical signer")
	}
	if releaseidentity.Hash(manifestRaw) != identity.ManifestSHA256 {
		return PQFloor{}, pqErr(ErrPQScope, "manifest bytes differ from the verified release")
	}
	signers, err := keyset.releaseSet(int64(identity.Sequence))
	if err != nil {
		return PQFloor{}, err
	}
	binding := PQBinding{Kind: PQKindReleaseManifest, Channel: string(identity.Profile), Version: identity.Version, Subject: "release-manifest.json", Source: identity.Commit, Sequence: identity.Sequence, Policy: release.PolicyDigest()}
	if err := verifyPQSet(pin, signers, binding, sha256Of(manifestRaw), setRaw); err != nil {
		return PQFloor{}, err
	}
	if int64(identity.Sequence) < floor.ReleaseSequence {
		return PQFloor{}, pqErr(ErrPQRollback, "release sequence")
	}
	next := floor.withPin(pin)
	next.ReleaseSequence = int64(identity.Sequence)
	if next.KeysetSequence != keyset.state.sequence || next.KeysetSHA256 != keyset.state.digest {
		return PQFloor{}, pqErr(ErrPQScope, "keyset differs from the persisted verification")
	}
	return next, floor.Advance(next)
}

// VerifyPQArtifact binds exact artifact bytes: the bytes must hash to the
// digest the verified manifest lists, and the release signer must have signed
// that digest under the artifact's name, version, channel, sequence and source.
func VerifyPQArtifact(pin PQPin, keyset VerifiedPQKeyset, release VerifiedRelease, name string, reader io.Reader, setRaw []byte) error {
	if !pin.Valid() || !release.Valid() {
		return pqErr(ErrPQScope, "unverified release")
	}
	identity := release.Identity()
	if identity.Profile != releaseidentity.StableV1 || release.state.signingKeyID != StableWorkflowSigner {
		return pqErr(ErrPQScope, "no PQ signer exists for this channel or historical signer")
	}
	var listed string
	for _, artifact := range release.state.artifacts {
		if artifact.Name == name {
			listed = artifact.SHA256
		}
	}
	if listed == "" {
		return pqErr(ErrPQScope, "artifact absent from verified inventory")
	}
	digest, err := hashReader(reader)
	if err != nil {
		return err
	}
	if hex.EncodeToString(digest[:]) != listed {
		return pqErr(ErrPQScope, "artifact bytes differ from the verified manifest")
	}
	signers, err := keyset.releaseSet(int64(identity.Sequence))
	if err != nil {
		return err
	}
	binding := PQBinding{Kind: PQKindArtifact, Channel: string(identity.Profile), Version: identity.Version, Subject: name, Source: identity.Commit, Sequence: identity.Sequence, Policy: release.PolicyDigest()}
	return verifyPQSet(pin, signers, binding, digest, setRaw)
}

// VerifyPQBridge adds the full recovery set to a bridge that VerifyBridge has
// already authenticated. A release-role signature never authorizes a bridge.
func VerifyPQBridge(pin PQPin, snapshot Snapshot, material BridgeMaterial, setRaw []byte) (VerifiedBridge, error) {
	if err := pin.linkedRecovery(snapshot); err != nil {
		return VerifiedBridge{}, err
	}
	bridge, err := VerifyBridge(snapshot, material)
	if err != nil {
		return VerifiedBridge{}, err
	}
	target := bridge.Statement().Target
	binding := PQBinding{Kind: PQKindBridge, Channel: string(target.Profile), Version: target.Version, Subject: "bridge.json", Source: target.Commit, Sequence: target.Sequence, Policy: bridge.Statement().TargetPolicySHA256}
	if err := verifyPQSet(pin, pin.recovery, binding, sha256Of(material.Statement), setRaw); err != nil {
		return VerifiedBridge{}, err
	}
	return bridge, nil
}
