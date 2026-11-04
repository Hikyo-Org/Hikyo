package releasetrust

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// PQFloor records the highest PQ verification each stage reached and the pin it
// was reached under. It is a SEPARATE record from SnapshotFloor, which is
// unchanged: SnapshotFloor lives in the upgrade control row and the shared
// `release-trust.json`, and a new member there is a migration on both engines.
// Like SnapshotFloor it is an untrusted local claim, not signature authority:
// the verifiers in this package produce the values they record, but nothing
// prevents a hand-written or restored file from stating more (E-17), and
// AuthenticityClaim reports only what the record says. Persisting it on a real
// host is NOT BUILT.
type PQFloor struct {
	Format           int                    `json:"format"`
	Profile          string                 `json:"profile"`
	Generation       int64                  `json:"generation"`
	PinSHA256        releaseidentity.Digest `json:"pin_sha256"`
	KeysetSequence   int64                  `json:"keyset_sequence"`
	KeysetSHA256     releaseidentity.Digest `json:"keyset_sha256"`
	KeysetPinSHA256  releaseidentity.Digest `json:"keyset_pin_sha256"`
	MetadataSequence int64                  `json:"metadata_sequence"`
	MetadataSHA256   releaseidentity.Digest `json:"metadata_sha256"`
	MetadataPin      releaseidentity.Digest `json:"metadata_pin_sha256"`
	CatalogSequence  int64                  `json:"catalog_sequence"`
	CatalogSHA256    releaseidentity.Digest `json:"catalog_sha256"`
	CatalogPin       releaseidentity.Digest `json:"catalog_pin_sha256"`
	ReleaseSequence  int64                  `json:"release_sequence"`
	ReleasePin       releaseidentity.Digest `json:"release_pin_sha256"`
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
	for _, stage := range []struct {
		sequence int64
		digest   releaseidentity.Digest
		pin      releaseidentity.Digest
		hasBytes bool
	}{{f.KeysetSequence, f.KeysetSHA256, f.KeysetPinSHA256, true}, {f.MetadataSequence, f.MetadataSHA256, f.MetadataPin, true}, {f.CatalogSequence, f.CatalogSHA256, f.CatalogPin, true}, {f.ReleaseSequence, "", f.ReleasePin, false}} {
		if (stage.sequence == 0) != (stage.pin == "") || (stage.pin != "" && stage.pin.Validate() != nil) ||
			(stage.hasBytes && ((stage.sequence == 0) != (stage.digest == "") || (stage.digest != "" && stage.digest.Validate() != nil))) {
			return pqErr(ErrPQMalformed, "persisted PQ floor stage")
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

// AuthenticityClaim reports whether this record says that keyset, metadata,
// catalog and a release each verified under exactly this pin. A stage verified
// under another pin, including an earlier generation, does not count, so a
// generation change needs every stage verified again. It does not authenticate
// the record and does not lift E-11 or E-15: how the pin first reached this host
// is outside the check.
func (f PQFloor) AuthenticityClaim(pin PQPin) error {
	if err := f.check(pin); err != nil {
		return err
	}
	if f.zero() || f.Generation != pin.generation {
		return pqErr(ErrPQMissing, "no persisted verification that required every algorithm")
	}
	for _, stagePin := range []releaseidentity.Digest{f.KeysetPinSHA256, f.MetadataPin, f.CatalogPin, f.ReleasePin} {
		if stagePin != pin.digest {
			return pqErr(ErrPQMissing, "a stage was not verified under this pin")
		}
	}
	return nil
}

func EncodePQFloor(f PQFloor) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if f.zero() {
		return nil, pqErr(ErrPQMalformed, "an empty PQ floor is not persisted")
	}
	raw, err := json.Marshal(f)
	return append(raw, '\n'), err
}

func DecodePQFloor(raw []byte) (PQFloor, error) {
	var f PQFloor
	if len(raw) == 0 || len(raw) > 4096 || definitions.DecodeStrict(raw, &f) != nil {
		return PQFloor{}, pqErr(ErrPQMalformed, "persisted PQ floor")
	}
	if err := f.Validate(); err != nil {
		return PQFloor{}, err
	}
	if f.zero() {
		return PQFloor{}, pqErr(ErrPQMalformed, "persisted PQ floor is empty")
	}
	return f, nil
}

// PQFloorPath is the sibling of the shared SnapshotFloor state file, guarded by
// the same verification lock. Nothing writes it on a shipping path yet.
func PQFloorPath(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), strings.TrimSuffix(filepath.Base(statePath), ".json")+"-pq.json")
}

// Advance refuses counter rollback on every sequence and equivocation (the same
// sequence with different bytes) for keyset, metadata and catalog. The release
// stage stores a sequence and a pin but no digest, so release equivocation is not
// detected (R-17).
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

// sameStablePolicy reports whether the material's stable policy is the one the
// snapshot authenticated (and absent when the snapshot had none).
func sameStablePolicy(state *snapshotState, material SnapshotMaterial) bool {
	if state.workflowPolicy == nil {
		return len(material.StablePolicy) == 0
	}
	return releaseidentity.Hash(material.StablePolicy) == state.stablePolicy
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
//
// Once the keyset has verified under the full recovery set it is authority in its
// own right, so a refusal later in this function (for example because the keyset
// revoked every key covering the release) returns the keyset-advanced floor
// beside the error. A caller persists that floor, so an older keyset cannot be
// replayed to revive a revoked key. On any earlier failure the floor is zero.
func VerifyPQSnapshot(pin PQPin, snapshot Snapshot, material SnapshotMaterial, sets PQSnapshotSets, floor PQFloor) (VerifiedPQKeyset, PQFloor, error) {
	if err := pin.linkedRecovery(snapshot); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, err
	}
	keyset, next, err := VerifyPQKeyset(pin, floor, sets.Keyset, sets.KeysetSet, sets.KeyFiles)
	if err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, err
	}
	keysetFloor := next
	refuse := func(err error) (VerifiedPQKeyset, PQFloor, error) {
		return VerifiedPQKeyset{}, keysetFloor, err
	}
	state := snapshot.state
	// Policy sets are valid across releases, so the only protection against a
	// policy that the snapshot never authenticated is that this call receives the
	// same material the snapshot was verified with.
	if releaseidentity.Hash(material.Metadata) != state.floor.MetadataSHA256 || releaseidentity.Hash(material.Catalog) != state.floor.CatalogSHA256 ||
		!sameStablePolicy(state, material) || (material.NightlyPolicy != nil) != state.nightlySeen ||
		(state.nightlySeen && releaseidentity.Hash(material.NightlyPolicy) != state.nightlyPolicy) {
		return refuse(pqErr(ErrPQScope, "PQ evidence differs from the authenticated snapshot"))
	}
	version := ""
	if state.metadata.HighestRelease != nil {
		version = *state.metadata.HighestRelease
	}
	trust := func(kind, subject string, sequence int64) PQBinding {
		return PQBinding{Kind: kind, Channel: string(releaseidentity.StableV1), Version: version, Subject: subject, Source: state.metadata.SourceCommit, Sequence: uint64(sequence), Policy: state.stablePolicy}
	}
	// The role is chosen from the authenticated metadata's signer field. That is
	// a document claim, but it can only move the requirement between two sets that
	// both need an ML-DSA signature, and the delegated branch also needs a
	// recovery-signed policy below.
	signers := pin.recovery
	if state.metadata.Event.SignedBy == StableWorkflowSigner {
		highest := int64(1)
		if state.metadata.HighestReleaseSequence != nil {
			highest = *state.metadata.HighestReleaseSequence
		}
		if signers, err = keyset.releaseSet(highest); err != nil {
			return refuse(err)
		}
	}
	if err := verifyPQSet(pin, signers, trust(PQKindMetadata, "metadata.json", state.metadata.Sequence), sha256Of(material.Metadata), sets.MetadataSet); err != nil {
		return refuse(err)
	}
	if err := verifyPQSet(pin, signers, trust(PQKindCatalog, "catalog.json", state.catalog.Sequence), sha256Of(material.Catalog), sets.CatalogSet); err != nil {
		return refuse(err)
	}
	// Policies are signed once and stay valid across releases: their bindings use
	// only the policy's own fields, never the current release's version, source
	// commit or catalog sequence, so offline recovery keys do not sign routine
	// releases.
	if state.workflowPolicy != nil {
		// A delegation grants CI no authority over itself: only the recovery set signs it.
		binding := PQBinding{Kind: PQKindStablePolicy, Channel: string(releaseidentity.StableV1), Subject: "stable-policy.json", Sequence: uint64(state.workflowPolicy.MinimumReleaseSequence), Policy: state.legacyStablePolicy}
		if err := verifyPQSet(pin, pin.recovery, binding, sha256Of(material.StablePolicy), sets.StablePolicySet); err != nil {
			return refuse(err)
		}
	}
	if state.nightlySeen {
		// A nightly policy has no sequence of its own; its currency is the
		// catalog's digest inventory, so the binding carries the constant 1.
		binding := PQBinding{Kind: PQKindNightlyPolicy, Channel: string(releaseidentity.NightlyV1), Subject: "nightly-policy.json", Sequence: 1, Policy: state.legacyStablePolicy}
		if err := verifyPQSet(pin, pin.recovery, binding, sha256Of(material.NightlyPolicy), sets.NightlyPolicySet); err != nil {
			return refuse(err)
		}
	}
	if err := checkFloor(state.metadata.Sequence, state.floor.MetadataSHA256, floor.MetadataSequence, floor.MetadataSHA256); err != nil {
		return refuse(pqErr(ErrPQRollback, "metadata sequence"))
	}
	if err := checkFloor(state.catalog.Sequence, state.floor.CatalogSHA256, floor.CatalogSequence, floor.CatalogSHA256); err != nil {
		return refuse(pqErr(ErrPQRollback, "catalog sequence"))
	}
	next.MetadataSequence, next.MetadataSHA256, next.MetadataPin = state.metadata.Sequence, state.floor.MetadataSHA256, pin.digest
	next.CatalogSequence, next.CatalogSHA256, next.CatalogPin = state.catalog.Sequence, state.floor.CatalogSHA256, pin.digest
	if err := floor.Advance(next); err != nil {
		return refuse(err)
	}
	return keyset, next, nil
}

// VerifyPQRelease requires the maintainer-held ML-DSA signature over the exact
// manifest bytes, bound to channel, version, sequence, source commit and policy.
// Historical keyed releases and nightlies have no PQ signer (E-12, E-15) and are
// refused here rather than reported as verified. The floor argument must be the
// one VerifyPQSnapshot returned for this snapshot. On a refusal other than a
// floor that fails its own check, the input floor comes back beside the error.
func VerifyPQRelease(pin PQPin, keyset VerifiedPQKeyset, snapshot Snapshot, release VerifiedRelease, manifestRaw, setRaw []byte, floor PQFloor) (PQFloor, error) {
	if err := floor.check(pin); err != nil {
		return PQFloor{}, err
	}
	// Every later refusal returns the input floor: it already holds the keyset
	// and snapshot stages, so a caller that persists it never forgets them.
	if !snapshot.Valid() || !release.Valid() || release.SnapshotDigest() != snapshot.Digest() {
		return floor, pqErr(ErrPQScope, "release does not belong to this snapshot")
	}
	// The floor must come from VerifyPQSnapshot over this same snapshot, so a
	// snapshot that skipped the PQ snapshot stage is refused here.
	if floor.MetadataSequence != snapshot.state.metadata.Sequence || floor.MetadataSHA256 != snapshot.state.floor.MetadataSHA256 ||
		floor.CatalogSequence != snapshot.state.catalog.Sequence || floor.CatalogSHA256 != snapshot.state.floor.CatalogSHA256 {
		return floor, pqErr(ErrPQScope, "snapshot has not passed the PQ snapshot stage")
	}
	identity := release.Identity()
	if identity.Profile != releaseidentity.StableV1 || release.state.signingKeyID != StableWorkflowSigner {
		return floor, pqErr(ErrPQScope, "no PQ signer exists for this channel or historical signer")
	}
	if releaseidentity.Hash(manifestRaw) != identity.ManifestSHA256 {
		return floor, pqErr(ErrPQScope, "manifest bytes differ from the verified release")
	}
	signers, err := keyset.releaseSet(int64(identity.Sequence))
	if err != nil {
		return floor, err
	}
	binding := PQBinding{Kind: PQKindReleaseManifest, Channel: string(identity.Profile), Version: identity.Version, Subject: "release-manifest.json", Source: identity.Commit, Sequence: identity.Sequence, Policy: release.PolicyDigest()}
	if err := verifyPQSet(pin, signers, binding, sha256Of(manifestRaw), setRaw); err != nil {
		return floor, err
	}
	if int64(identity.Sequence) < floor.ReleaseSequence {
		return floor, pqErr(ErrPQRollback, "release sequence")
	}
	next := floor.withPin(pin)
	next.ReleaseSequence, next.ReleasePin = int64(identity.Sequence), pin.digest
	if keyset.state.pin != pin.digest || next.KeysetPinSHA256 != pin.digest || next.KeysetSequence != keyset.state.sequence || next.KeysetSHA256 != keyset.state.digest {
		return floor, pqErr(ErrPQScope, "keyset differs from the persisted verification")
	}
	if err := floor.Advance(next); err != nil {
		return floor, err
	}
	return next, nil
}

// VerifyPQArtifact binds exact artifact bytes: the bytes must hash to the
// digest the verified manifest lists, and the release signer must have signed
// that digest under the artifact's name, version, channel, sequence and source.
func VerifyPQArtifact(pin PQPin, keyset VerifiedPQKeyset, release VerifiedRelease, name string, reader io.Reader, setRaw []byte) error {
	if !pin.Valid() || !release.Valid() || !keyset.Valid() || keyset.state.pin != pin.digest {
		return pqErr(ErrPQScope, "unverified release or a keyset verified under another pin")
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
		return VerifiedBridge{}, fmt.Errorf("%w: %v", ErrPQScope, err)
	}
	target := bridge.Statement().Target
	binding := PQBinding{Kind: PQKindBridge, Channel: string(target.Profile), Version: target.Version, Subject: "bridge.json", Source: target.Commit, Sequence: target.Sequence, Policy: bridge.Statement().TargetPolicySHA256}
	if err := verifyPQSet(pin, pin.recovery, binding, sha256Of(material.Statement), setRaw); err != nil {
		return VerifiedBridge{}, err
	}
	return bridge, nil
}
