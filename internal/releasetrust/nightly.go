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

// Artifacts is the actual closed payload inventory, excluding the fixed
// manifest/signature pair. Platform selects the native subset; empty means the
// complete release. VerifyNightly consumes every reader. The caller enumerates
// the actual directory and keeps verified staging bytes immutable until use.
type NightlyMaterial struct {
	Policy, TrustedRoot, Manifest, Bundle, Compatibility []byte
	Artifacts                                            map[string]io.Reader
	Platform                                             string
}

// VerifyNightlyManifest authenticates identity and signed inventory without
// fetching payloads. Each selected artifact must still be verified before use.
func VerifyNightlyManifest(snapshot Snapshot, material NightlyMaterial) (VerifiedRelease, error) {
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
	if err := validateNightlyDiskImages(manifest.Artifacts, manifest.Version); err != nil {
		return VerifiedRelease{}, err
	}
	if len(material.Compatibility) == 0 || len(material.Compatibility) > MaxDocumentBytes || !bindsArtifact(manifest.Artifacts, CompatibilityArtifact, "upgrade-compatibility", releaseidentity.Hash(material.Compatibility)) {
		return VerifiedRelease{}, errors.New("nightly declaration missing or substituted")
	}
	identity := releaseidentity.Identity{Profile: manifest.Profile, Version: manifest.Version, Sequence: manifest.ReleaseSequence, Commit: manifest.SourceCommit, ManifestSHA256: manifestDigest, CompatibilitySHA256: releaseidentity.Hash(material.Compatibility)}
	release := VerifiedRelease{state: &verifiedReleaseState{identity: identity, snapshot: snapshot.Digest(), policy: policyDigest, artifacts: slices.Clone(manifest.Artifacts)}}
	kinds := map[string]bool{}
	for _, artifact := range manifest.Artifacts {
		if artifact.Name == "release-manifest.json" || artifact.Name == "release-manifest.sigstore.json" {
			return VerifiedRelease{}, errors.New("recursive nightly envelope inventory")
		}
		if (artifact.Kind == "binary" || artifact.Kind == "package") && artifact.Platform == "" {
			return VerifiedRelease{}, errors.New("nightly executable lacks exact platform")
		}
		kinds[artifact.Kind] = true
	}
	if !kinds["binary"] || !kinds["binary-provenance"] || !kinds["checksum"] {
		return VerifiedRelease{}, errors.New("nightly binary/provenance/checksum inventory is incomplete")
	}
	// These documents grant authority before any payload is downloaded.
	for name, raw := range map[string][]byte{"nightly-policy.json": material.Policy, "sigstore-trusted-root.json": material.TrustedRoot, CompatibilityArtifact: material.Compatibility} {
		if err := release.VerifyArtifact(name, bytes.NewReader(raw)); err != nil {
			return VerifiedRelease{}, fmt.Errorf("nightly document %s: %w", name, err)
		}
	}
	return release, nil
}

// Older nightlies omitted disk images. When present, require the canonical pair
// for this version; each remains part of complete offline payload verification.
func validateNightlyDiskImages(artifacts []Artifact, version string) error {
	expected := map[string]string{
		"hikyo_" + version + "_Darwin_x86_64.dmg": "darwin/amd64",
		"hikyo_" + version + "_Darwin_arm64.dmg":  "darwin/arm64",
	}
	count := 0
	for _, artifact := range artifacts {
		if artifact.Kind != "macos-dmg" {
			continue
		}
		if platform, ok := expected[artifact.Name]; !ok || artifact.Platform != platform {
			return errors.New("nightly disk image must bind its exact version and Darwin platform")
		}
		count++
	}
	if count != 0 && count != 2 {
		return errors.New("nightly disk image inventory requires both Darwin architectures")
	}
	return nil
}

// VerifyNightly verifies the complete inventory by default. Platform requests
// require exactly the authenticated platform selection, including its metadata.
func VerifyNightly(snapshot Snapshot, material NightlyMaterial) (VerifiedRelease, error) {
	release, err := VerifyNightlyManifest(snapshot, material)
	if err != nil {
		return VerifiedRelease{}, err
	}
	artifacts, err := release.NightlyArtifacts(material.Platform)
	if err != nil {
		return VerifiedRelease{}, err
	}
	if len(material.Artifacts) != len(artifacts) {
		return VerifiedRelease{}, errors.New("nightly payload inventory is incomplete or has extra assets")
	}
	for _, artifact := range artifacts {
		if err := release.VerifyArtifact(artifact.Name, material.Artifacts[artifact.Name]); err != nil {
			return VerifiedRelease{}, fmt.Errorf("nightly asset %s: %w", artifact.Name, err)
		}
	}
	return release, nil
}

// ValidNightlyPlatform recognizes the release platforms supported by the updater.
func ValidNightlyPlatform(platform string) bool {
	return slices.Contains([]string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"}, platform)
}

// NightlyArtifacts selects one native binary archive and shared or matching
// platform metadata. OS packages are not used by the binary self-updater.
// An empty platform preserves complete offline release verification.
func (r VerifiedRelease) NightlyArtifacts(platform string) ([]Artifact, error) {
	if !r.Valid() || r.Identity().Profile != releaseidentity.NightlyV1 {
		return nil, errors.New("verified nightly required for platform selection")
	}
	if platform == "" {
		return r.Artifacts(), nil
	}
	if !ValidNightlyPlatform(platform) {
		return nil, errors.New("unsupported nightly platform")
	}
	var selected []Artifact
	binaries := 0
	metadata := map[string]bool{}
	for _, artifact := range r.Artifacts() {
		if artifact.Platform != "" && artifact.Platform != platform {
			continue
		}
		switch artifact.Kind {
		case "binary":
			binaries++
		case "nightly-policy", "sigstore-trusted-root", "upgrade-compatibility", "binary-provenance", "checksum", "release-notes", "sbom":
			metadata[artifact.Kind] = true
		default:
			continue
		}
		selected = append(selected, artifact)
	}
	if binaries != 1 {
		return nil, errors.New("nightly requires one exact platform binary")
	}
	for _, kind := range []string{"nightly-policy", "sigstore-trusted-root", "upgrade-compatibility", "binary-provenance", "checksum"} {
		if !metadata[kind] {
			return nil, errors.New("nightly platform metadata is incomplete")
		}
	}
	return selected, nil
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
		!strings.HasPrefix(p.WorkflowPath, ".github/workflows/") || !releaseidentity.SafeName(strings.TrimPrefix(p.WorkflowPath, ".github/workflows/")) || !strings.HasSuffix(p.WorkflowPath, ".yml") ||
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
	return verifyWorkflowEnvelope(policy, trustedRoot, rawBundle, artifact, commit, policy.ProtectedRef)
}

func verifyWorkflowEnvelope(policy NightlyPolicy, trustedRoot, rawBundle, artifact []byte, commit, ref string) error {
	return verifyWorkflowEnvelopeReader(policy, trustedRoot, rawBundle, bytes.NewReader(artifact), commit, ref)
}

func verifyWorkflowEnvelopeReader(policy NightlyPolicy, trustedRoot, rawBundle []byte, artifact io.Reader, commit, ref string) error {
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
	workflowURI := policy.RepositoryURI + "/" + policy.WorkflowPath + "@" + ref
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
		SourceRepositoryURI: policy.RepositoryURI, SourceRepositoryDigest: commit, SourceRepositoryRef: ref,
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
	_, err = verifier.Verify(&signed, verify.NewPolicy(verify.WithArtifact(artifact), verify.WithCertificateIdentity(identity)))
	return err
}
