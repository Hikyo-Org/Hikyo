package releasetrust

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/sigstore/rekor/pkg/util"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// NightlyPolicy is recovery-authorized by its exact digest in the current
// catalog. TrustedRootSHA256 pins offline Fulcio, Rekor and checkpoint keys.
type NightlyPolicy struct {
	Schema             string                   `json:"schema"`
	TrustedRootSHA256  releaseidentity.Digest   `json:"trusted_root_sha256"`
	Issuer             string                   `json:"issuer"`
	RepositoryURI      string                   `json:"repository_uri"`
	RepositoryID       string                   `json:"repository_id"`
	RepositoryOwnerURI string                   `json:"repository_owner_uri"`
	RepositoryOwnerID  string                   `json:"repository_owner_id"`
	WorkflowPath       string                   `json:"workflow_path"`
	ProtectedRef       string                   `json:"protected_ref"`
	RunnerEnvironment  string                   `json:"runner_environment"`
	RequireSCT         *bool                    `json:"require_sct"`
	RekorLogID         releaseidentity.Digest   `json:"rekor_log_id"`
	CheckpointOrigin   string                   `json:"checkpoint_origin"`
	RevokedManifests   []releaseidentity.Digest `json:"revoked_manifests"`
}

type NightlyManifest struct {
	Schema          string                  `json:"schema"`
	Profile         releaseidentity.Profile `json:"profile"`
	Version         string                  `json:"version"`
	Tag             string                  `json:"tag"`
	SourceCommit    string                  `json:"source_commit"`
	ReleaseSequence uint64                  `json:"release_sequence"`
	Artifacts       []Artifact              `json:"artifacts"`
}

// Artifacts is the actual closed payload inventory, excluding only the fixed
// release-manifest.json and release-manifest.sigstore.json envelope pair.
// Every reader is consumed and checked. The caller must enumerate the actual
// asset directory and keep verified staging bytes immutable until use.
type NightlyMaterial struct {
	Policy, TrustedRoot, Manifest, Bundle, Compatibility []byte
	Artifacts                                            map[string]io.Reader
}

func VerifyNightly(snapshot Snapshot, material NightlyMaterial) (VerifiedRelease, error) {
	if !snapshot.Valid() {
		return VerifiedRelease{}, errors.New("unverified trust snapshot")
	}
	policyDigest := releaseidentity.Hash(material.Policy)
	if !slices.Contains(snapshot.state.catalog.NightlyPolicies, policyDigest) {
		return VerifiedRelease{}, errors.New("nightly policy is not currently recovery-authorized")
	}
	var policy NightlyPolicy
	if err := decodeDocument(material.Policy, &policy); err != nil {
		return VerifiedRelease{}, err
	}
	if err := policy.Validate(); err != nil {
		return VerifiedRelease{}, err
	}
	if len(material.TrustedRoot) == 0 || len(material.TrustedRoot) > MaxDocumentBytes || releaseidentity.Hash(material.TrustedRoot) != policy.TrustedRootSHA256 {
		return VerifiedRelease{}, errors.New("nightly trusted root missing or substituted")
	}
	var manifest NightlyManifest
	if err := decodeDocument(material.Manifest, &manifest); err != nil {
		return VerifiedRelease{}, err
	}
	if manifest.Schema != "hikyo.dev/nightly-manifest/v1" || manifest.Profile != releaseidentity.NightlyV1 || manifest.Tag != "v"+manifest.Version {
		return VerifiedRelease{}, errors.New("invalid nightly manifest identity")
	}
	if err := releaseidentity.ValidateTarget(manifest.Profile, manifest.Version, manifest.ReleaseSequence, manifest.SourceCommit); err != nil {
		return VerifiedRelease{}, err
	}
	manifestDigest := releaseidentity.Hash(material.Manifest)
	if slices.Contains(policy.RevokedManifests, manifestDigest) || slices.Contains(snapshot.state.nightlyRevoked, manifestDigest) {
		return VerifiedRelease{}, errors.New("nightly manifest revoked by current policy")
	}
	if err := verifyNightlyEnvelope(policy, material.TrustedRoot, material.Bundle, material.Manifest, manifest.SourceCommit); err != nil {
		return VerifiedRelease{}, err
	}
	if err := validateArtifacts(manifest.Artifacts); err != nil {
		return VerifiedRelease{}, err
	}
	if err := validateArtifactVersions(manifest.Artifacts, manifest.Version); err != nil {
		return VerifiedRelease{}, err
	}
	if len(material.Compatibility) == 0 || len(material.Compatibility) > MaxDocumentBytes || !bindsArtifact(manifest.Artifacts, CompatibilityArtifact, "upgrade-compatibility", releaseidentity.Hash(material.Compatibility)) {
		return VerifiedRelease{}, errors.New("nightly declaration missing or substituted")
	}
	identity := releaseidentity.Identity{Profile: manifest.Profile, Version: manifest.Version, Sequence: manifest.ReleaseSequence, Commit: manifest.SourceCommit, ManifestSHA256: manifestDigest, CompatibilitySHA256: releaseidentity.Hash(material.Compatibility)}
	release := VerifiedRelease{state: &verifiedReleaseState{identity: identity, snapshot: snapshot.Digest(), policy: policyDigest, artifacts: slices.Clone(manifest.Artifacts)}}
	if len(material.Artifacts) != len(manifest.Artifacts) {
		return VerifiedRelease{}, errors.New("nightly payload inventory is incomplete or has extra assets")
	}
	kinds := map[string]bool{}
	for _, artifact := range manifest.Artifacts {
		if artifact.Name == "release-manifest.json" || artifact.Name == "release-manifest.sigstore.json" {
			return VerifiedRelease{}, errors.New("recursive nightly envelope inventory")
		}
		if (artifact.Kind == "binary" || artifact.Kind == "package") && artifact.Platform == "" {
			return VerifiedRelease{}, errors.New("nightly executable lacks exact platform")
		}
		kinds[artifact.Kind] = true
		reader, ok := material.Artifacts[artifact.Name]
		if !ok {
			return VerifiedRelease{}, errors.New("nightly payload missing")
		}
		if err := release.VerifyArtifact(artifact.Name, reader); err != nil {
			return VerifiedRelease{}, fmt.Errorf("nightly asset %s: %w", artifact.Name, err)
		}
	}
	if !kinds["binary"] || !kinds["binary-provenance"] || !kinds["checksum"] {
		return VerifiedRelease{}, errors.New("nightly binary/provenance/checksum inventory is incomplete")
	}
	return release, nil
}

func (p NightlyPolicy) Validate() error {
	if p.RequireSCT == nil {
		return errors.New("nightly policy must explicitly choose SCT verification")
	}
	if p.RekorLogID.Validate() != nil || p.CheckpointOrigin == "" || len(p.CheckpointOrigin) > 512 || strings.ContainsAny(p.CheckpointOrigin, "\r\n") {
		return errors.New("nightly policy lacks exact checkpoint identity")
	}
	if p.Schema != "hikyo.dev/nightly-policy/v1" || p.TrustedRootSHA256.Validate() != nil || p.Issuer != "https://token.actions.githubusercontent.com" ||
		!strings.HasPrefix(p.RepositoryURI, "https://github.com/") || strings.Count(strings.TrimPrefix(p.RepositoryURI, "https://github.com/"), "/") != 1 ||
		!decimalID(p.RepositoryID) || !decimalID(p.RepositoryOwnerID) ||
		p.RepositoryOwnerURI != p.RepositoryURI[:strings.LastIndex(p.RepositoryURI, "/")] ||
		!strings.HasPrefix(p.WorkflowPath, ".github/workflows/") || !safeName(strings.TrimPrefix(p.WorkflowPath, ".github/workflows/")) || !strings.HasSuffix(p.WorkflowPath, ".yml") ||
		p.ProtectedRef != "refs/heads/main" || (p.RunnerEnvironment != "github-hosted" && p.RunnerEnvironment != "self-hosted") {
		return errors.New("invalid exact nightly issuer/repository/workflow/ref policy")
	}
	return digestInventory(p.RevokedManifests, 256)
}

func decimalID(value string) bool {
	if value == "" || value[0] == '0' || len(value) > 20 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func verifyNightlyEnvelope(policy NightlyPolicy, trustedRoot, rawBundle, artifact []byte, commit string) error {
	if len(rawBundle) == 0 || len(rawBundle) > MaxDocumentBytes {
		return errors.New("complete offline nightly Sigstore bundle required")
	}
	if err := definitions.RejectDuplicateMembers(rawBundle); err != nil {
		return err
	}
	if err := definitions.RejectDuplicateMembers(trustedRoot); err != nil {
		return err
	}
	trusted, err := root.NewTrustedRootFromJSON(trustedRoot)
	if err != nil {
		return err
	}
	var signed bundle.Bundle
	if err := signed.UnmarshalJSON(rawBundle); err != nil {
		return err
	}
	entries := signed.GetVerificationMaterial().GetTlogEntries()
	// Exactly one log entry ensures the signed timestamp and inclusion proof
	// refer to the same evidence. Rekor v2 TSA-only profiles are not this ADR.
	if len(entries) != 1 || entries[0].GetIntegratedTime() <= 0 || len(entries[0].GetInclusionPromise().GetSignedEntryTimestamp()) == 0 || entries[0].GetInclusionProof().GetCheckpoint().GetEnvelope() == "" {
		return errors.New("nightly requires same-entry signed integrated time and inclusion proof/checkpoint")
	}
	workflowURI := policy.RepositoryURI + "/" + policy.WorkflowPath + "@" + policy.ProtectedRef
	proof := entries[0].GetInclusionProof()
	var checkpoint util.SignedCheckpoint
	if err := checkpoint.UnmarshalText([]byte(proof.GetCheckpoint().GetEnvelope())); err != nil {
		return err
	}
	if hex.EncodeToString(entries[0].GetLogId().GetKeyId()) != string(policy.RekorLogID) || checkpoint.Origin != policy.CheckpointOrigin || proof.GetTreeSize() < 1 || checkpoint.Size != uint64(proof.GetTreeSize()) {
		return errors.New("nightly checkpoint identity or tree size differs from pinned evidence")
	}
	// Rekor v1's SET authenticates a global index, but its Merkle proof uses
	// a shard-local index. They need not match. The maintained verifier below
	// verifies both against this entry's same canonicalized body, including
	// the signed global index and the proof's local index and checkpoint.
	san, err := verify.NewSANMatcher(workflowURI, "")
	if err != nil {
		return err
	}
	issuer, err := verify.NewIssuerMatcher(policy.Issuer, "")
	if err != nil {
		return err
	}
	identity, err := verify.NewCertificateIdentity(san, issuer, certificate.Extensions{
		BuildSignerURI: workflowURI, BuildSignerDigest: commit, RunnerEnvironment: policy.RunnerEnvironment,
		SourceRepositoryURI: policy.RepositoryURI, SourceRepositoryDigest: commit, SourceRepositoryRef: policy.ProtectedRef,
		SourceRepositoryIdentifier: policy.RepositoryID, SourceRepositoryOwnerURI: policy.RepositoryOwnerURI, SourceRepositoryOwnerIdentifier: policy.RepositoryOwnerID,
		BuildConfigURI: workflowURI, BuildConfigDigest: commit,
	})
	if err != nil {
		return err
	}
	opts := []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithIntegratedTimestamps(1)}
	if *policy.RequireSCT {
		opts = append(opts, verify.WithSignedCertificateTimestamps(1))
	}
	verifier, err := verify.NewVerifier(trusted, opts...)
	if err != nil {
		return err
	}
	_, err = verifier.Verify(&signed, verify.NewPolicy(verify.WithArtifact(bytes.NewReader(artifact)), verify.WithCertificateIdentity(identity)))
	return err
}
