package releasetrust

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

const StableWorkflowSigner = "github-actions-stable"

// StablePolicy delegates routine stable release signing, but not recovery-key
// rotation, primary-key authority, nightly authority, or bridge authorization.
// Its exact bytes are signed once by the installation's pinned recovery key.
type StablePolicy struct {
	NightlyPolicy
	MinimumReleaseSequence int64                    `json:"minimum_release_sequence"`
	PrimaryKeysSHA256      releaseidentity.Digest   `json:"primary_keys_sha256"`
	NightlyPolicies        []releaseidentity.Digest `json:"nightly_policies"`
	Bridges                []releaseidentity.Digest `json:"bridges"`
}

func (p StablePolicy) Validate() error {
	if p.Schema != "hikyo.dev/stable-policy/v1" || p.ProtectedRef != "refs/tags/v*" || p.MinimumReleaseSequence < 1 || p.PrimaryKeysSHA256.Validate() != nil || p.RequireSCT == nil || !*p.RequireSCT || p.RunnerEnvironment != "github-hosted" {
		return errors.New("invalid stable workflow delegation")
	}
	identity := p.NightlyPolicy
	identity.Schema, identity.ProtectedRef = "hikyo.dev/nightly-policy/v1", "refs/heads/main"
	if err := identity.Validate(); err != nil {
		return err
	}
	if err := digestInventory(p.NightlyPolicies, 256); err != nil {
		return err
	}
	return digestInventory(p.Bridges, 1024)
}

// PrimaryKeysDigest gives policy authors the canonical digest used to prevent
// delegated CI from rewriting primary-key or bootstrap authority.
func PrimaryKeysDigest(keys []Primary) releaseidentity.Digest {
	raw, _ := json.Marshal(keys)
	return releaseidentity.Hash(raw)
}

func verifyStablePolicy(pinned PinnedTrust, material SnapshotMaterial) (*StablePolicy, error) {
	if err := VerifyKeySignature(pinned.RecoveryPublicKey, material.StablePolicySignature, material.StablePolicy); err != nil {
		return nil, err
	}
	var policy StablePolicy
	if err := decodeDocument(material.StablePolicy, &policy); err != nil {
		return nil, err
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if len(material.StableTrustedRoot) == 0 || len(material.StableTrustedRoot) > MaxDocumentBytes || releaseidentity.Hash(material.StableTrustedRoot) != policy.TrustedRootSHA256 {
		return nil, errors.New("stable Sigstore root missing or substituted")
	}
	return &policy, nil
}

func (p StablePolicy) verify(trustedRoot, envelope, payload []byte, commit, tag string) error {
	if !strings.HasPrefix(tag, "v") || releaseidentity.ValidateTarget(releaseidentity.StableV1, strings.TrimPrefix(tag, "v"), uint64(p.MinimumReleaseSequence), commit) != nil {
		return errors.New("stable signature requires exact stable tag and commit")
	}
	return verifyWorkflowEnvelope(p.NightlyPolicy, trustedRoot, envelope, payload, commit, "refs/tags/"+tag)
}

func validateDelegatedMetadata(policy StablePolicy, metadata Metadata) error {
	if metadata.Event.Type != "release" || metadata.Event.SignedBy != StableWorkflowSigner || metadata.PendingRelease != nil || metadata.HighestRelease == nil || metadata.HighestReleaseSequence == nil || *metadata.HighestReleaseSequence < policy.MinimumReleaseSequence || metadata.ReleaseTag != "v"+*metadata.HighestRelease || !commitPattern.MatchString(metadata.SourceCommit) || PrimaryKeysDigest(metadata.PrimaryKeys) != policy.PrimaryKeysSHA256 {
		return errors.New("stable workflow metadata exceeds delegated authority")
	}
	highestFound := false
	for _, release := range metadata.Releases {
		if release.Sequence > *metadata.HighestReleaseSequence {
			return errors.New("stable release exceeds declared highest sequence")
		}
		if release.Sequence == *metadata.HighestReleaseSequence && release.Version == *metadata.HighestRelease {
			highestFound = true
		}
	}
	if !highestFound {
		return errors.New("stable metadata highest release is absent")
	}
	return nil
}

// VerifyStableArtifactSignature verifies an independently signed artifact using
// the same exact identity as an already authenticated stable manifest.
func VerifyStableArtifactSignature(snapshot Snapshot, release VerifiedRelease, signature, payload []byte) error {
	return VerifyStableArtifactSignatureReader(snapshot, release, signature, bytes.NewReader(payload))
}

func VerifyStableArtifactSignatureReader(snapshot Snapshot, release VerifiedRelease, signature []byte, payload io.Reader) error {
	if payload == nil || !snapshot.Valid() || !release.Valid() || release.SnapshotDigest() != snapshot.Digest() || release.Identity().Profile != releaseidentity.StableV1 {
		return errors.New("stable signature requires verified release and snapshot")
	}
	if release.state.signingKeyID != StableWorkflowSigner && release.PolicyDigest() == snapshot.state.legacyStablePolicy {
		limited := &io.LimitedReader{R: payload, N: MaxArtifactBytes + 1}
		err := VerifyKeySignatureReader(snapshot.state.keys[release.state.signingKeyID], signature, limited)
		if limited.N == 0 {
			return errors.New("stable artifact exceeds byte bound")
		}
		return err
	}
	if snapshot.state.workflowPolicy == nil || release.PolicyDigest() != snapshot.state.stablePolicy {
		return errors.New("release does not use delegated stable signing")
	}
	id := release.Identity()
	limited := &io.LimitedReader{R: payload, N: MaxArtifactBytes + 1}
	err := verifyWorkflowEnvelopeReader(snapshot.state.workflowPolicy.NightlyPolicy, snapshot.state.stableTrustedRoot, signature, limited, id.Commit, "refs/tags/v"+id.Version)
	if limited.N == 0 {
		return errors.New("stable artifact exceeds byte bound")
	}
	return err
}

func sameDigests(a, b []releaseidentity.Digest) bool { return slices.Equal(a, b) }

// cloneStableRoot prevents mutable caller-owned bytes becoming authority.
func cloneStableRoot(raw []byte) []byte { return bytes.Clone(raw) }

func (s Snapshot) StableKeylessEnabled() bool { return s.Valid() && s.state.workflowPolicy != nil }

// StablePolicy returns a detached copy of recovery-authorized policy.
func (s Snapshot) StablePolicy() (StablePolicy, bool) {
	if !s.StableKeylessEnabled() {
		return StablePolicy{}, false
	}
	p := *s.state.workflowPolicy
	p.NightlyPolicies = slices.Clone(p.NightlyPolicies)
	p.Bridges = slices.Clone(p.Bridges)
	p.RevokedManifests = slices.Clone(p.RevokedManifests)
	if p.RequireSCT != nil {
		required := *p.RequireSCT
		p.RequireSCT = &required
	}
	return p, true
}
