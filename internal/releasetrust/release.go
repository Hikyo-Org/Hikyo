package releasetrust

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

const CompatibilityArtifact = "upgrade-compatibility.json"
const MaxArtifacts = 512
const MaxArtifactBytes int64 = 2 << 30

type StableMaterial struct {
	Manifest          []byte
	ManifestSignature []byte
	Candidate         []byte
	Compatibility     []byte
}

type verifiedReleaseState struct {
	identity  releaseidentity.Identity
	snapshot  releaseidentity.Digest
	policy    releaseidentity.Digest
	artifacts []Artifact
}

// VerifiedRelease is immutable authenticated release identity and inventory.
// It does not imply the artifact files have been downloaded or grant execution.
// Callers must VerifyArtifact for every staged platform payload before applying.
type VerifiedRelease struct{ state *verifiedReleaseState }

func (r VerifiedRelease) Valid() bool { return r.state != nil }
func (r VerifiedRelease) Identity() releaseidentity.Identity {
	if r.state == nil {
		return releaseidentity.Identity{}
	}
	return r.state.identity
}
func (r VerifiedRelease) PolicyDigest() releaseidentity.Digest {
	if r.state == nil {
		return ""
	}
	return r.state.policy
}
func (r VerifiedRelease) SnapshotDigest() releaseidentity.Digest {
	if r.state == nil {
		return ""
	}
	return r.state.snapshot
}
func (r VerifiedRelease) Artifacts() []Artifact {
	if r.state == nil {
		return nil
	}
	return slices.Clone(r.state.artifacts)
}

func (r VerifiedRelease) VerifyArtifact(name string, reader io.Reader) error {
	if r.state == nil || reader == nil {
		return errors.New("unverified release or missing artifact")
	}
	for _, artifact := range r.state.artifacts {
		if artifact.Name != name {
			continue
		}
		hash := sha256.New()
		count, err := io.Copy(hash, io.LimitReader(reader, MaxArtifactBytes+1))
		if err != nil {
			return err
		}
		if count > MaxArtifactBytes {
			return errors.New("artifact exceeds byte bound")
		}
		if hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
			return errors.New("artifact digest mismatch")
		}
		return nil
	}
	return errors.New("artifact absent from verified inventory")
}

// VerifyStable authenticates an authorized historical OR current release.
// Latest-only installer selection is a separate mandatory policy check.
func VerifyStable(snapshot Snapshot, material StableMaterial) (VerifiedRelease, error) {
	if !snapshot.Valid() {
		return VerifiedRelease{}, errors.New("unverified trust snapshot")
	}
	var manifest Manifest
	if err := decodeDocument(material.Manifest, &manifest); err != nil {
		return VerifiedRelease{}, err
	}
	if manifest.Schema != "hikyo.dev/release-manifest/v1" || manifest.Tag != "v"+manifest.Version || manifest.SigningKeyID == "" {
		return VerifiedRelease{}, errors.New("invalid stable manifest identity")
	}
	if manifest.ReleaseSequence < 1 {
		return VerifiedRelease{}, errors.New("invalid release sequence")
	}
	if err := releaseidentity.ValidateTarget(releaseidentity.StableV1, manifest.Version, uint64(manifest.ReleaseSequence), manifest.SourceCommit); err != nil {
		return VerifiedRelease{}, err
	}
	manifestDigest := releaseidentity.Hash(material.Manifest)
	authorized := false
	for _, release := range snapshot.state.metadata.Releases {
		if release.Version == manifest.Version && release.Sequence == manifest.ReleaseSequence && release.ManifestSHA256 == string(manifestDigest) {
			authorized = true
		}
	}
	if !authorized {
		return VerifiedRelease{}, errors.New("stable manifest absent from current release authorization")
	}
	var candidate Candidate
	if err := decodeDocument(material.Candidate, &candidate); err != nil {
		return VerifiedRelease{}, err
	}
	if candidate.Version != manifest.Version || candidate.Sequence != manifest.ReleaseSequence || candidate.Commit != manifest.SourceCommit || candidate.KeyID != manifest.SigningKeyID || !safeName(candidate.PublicKey) {
		return VerifiedRelease{}, errors.New("release candidate does not match manifest")
	}
	var primary Primary
	covering := 0
	for _, key := range snapshot.state.metadata.PrimaryKeys {
		if key.ValidFromReleaseSequence <= manifest.ReleaseSequence && (key.ValidThroughReleaseSequence == nil || *key.ValidThroughReleaseSequence >= manifest.ReleaseSequence) {
			covering++
			primary = key
		}
	}
	if covering != 1 || primary.Revoked || (primary.Pending != nil && *primary.Pending) || primary.ID != candidate.KeyID || primary.PublicKey != candidate.PublicKey {
		return VerifiedRelease{}, errors.New("release signer is revoked, pending or ambiguously authorized")
	}
	if err := VerifyKeySignature(snapshot.state.keys[primary.ID], material.ManifestSignature, material.Manifest); err != nil {
		return VerifiedRelease{}, err
	}
	if err := validateArtifacts(manifest.Artifacts); err != nil {
		return VerifiedRelease{}, err
	}
	if err := validateArtifactVersions(manifest.Artifacts, manifest.Version); err != nil {
		return VerifiedRelease{}, err
	}
	if len(material.Compatibility) == 0 || len(material.Compatibility) > MaxDocumentBytes {
		return VerifiedRelease{}, errors.New("missing or oversized compatibility declaration")
	}
	compatibilityDigest := releaseidentity.Hash(material.Compatibility)
	if !bindsArtifact(manifest.Artifacts, CompatibilityArtifact, "upgrade-compatibility", compatibilityDigest) || !bindsArtifact(manifest.Artifacts, "release-candidate.json", "release-candidate", releaseidentity.Hash(material.Candidate)) {
		return VerifiedRelease{}, errors.New("manifest does not bind exact compatibility declaration and candidate")
	}
	identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: manifest.Version, Sequence: uint64(manifest.ReleaseSequence), Commit: manifest.SourceCommit, CompatibilitySHA256: compatibilityDigest, ManifestSHA256: manifestDigest}
	return VerifiedRelease{state: &verifiedReleaseState{identity: identity, snapshot: snapshot.Digest(), policy: snapshot.state.stablePolicy, artifacts: slices.Clone(manifest.Artifacts)}}, nil
}

func RequireLatestStable(snapshot Snapshot, release VerifiedRelease) error {
	if !snapshot.Valid() || !release.Valid() || release.SnapshotDigest() != snapshot.Digest() || release.Identity().Profile != releaseidentity.StableV1 {
		return errors.New("latest selection requires a current verified stable release")
	}
	metadata := snapshot.state.metadata
	if metadata.HighestRelease == nil || metadata.HighestReleaseSequence == nil || *metadata.HighestRelease != release.Identity().Version || uint64(*metadata.HighestReleaseSequence) != release.Identity().Sequence {
		return errors.New("selected stable release is not the signed latest release")
	}
	return nil
}

func bindsArtifact(artifacts []Artifact, name, kind string, digest releaseidentity.Digest) bool {
	for _, artifact := range artifacts {
		if artifact.Name == name && artifact.Kind == kind && artifact.SHA256 == string(digest) {
			return true
		}
	}
	return false
}

func validateArtifacts(artifacts []Artifact) error {
	if len(artifacts) == 0 || len(artifacts) > MaxArtifacts {
		return errors.New("artifact inventory must be nonempty and bounded")
	}
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		if !safeName(artifact.Name) || seen[artifact.Name] || releaseidentity.Digest(artifact.SHA256).Validate() != nil {
			return errors.New("invalid or duplicate artifact identity")
		}
		seen[artifact.Name] = true
		extra := artifact
		extra.Name, extra.Kind, extra.SHA256, extra.Platform = "", "", "", ""
		if artifact.Platform != "" && !slices.Contains([]string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"}, artifact.Platform) {
			return errors.New("unsupported exact artifact platform")
		}
		switch artifact.Kind {
		case "binary", "binary-provenance", "release-candidate", "upgrade-compatibility", "sbom", "checksum", "installer", "nightly-policy", "sigstore-trusted-root", "release-notes":
		case "package":
			if !slices.Contains([]string{"apk", "archlinux", "deb", "rpm"}, artifact.Format) || !slices.Contains([]string{"amd64", "arm64"}, artifact.Arch) || (artifact.Platform != "" && artifact.Platform != "linux/"+artifact.Arch) {
				return errors.New("invalid package identity")
			}
			extra.Format, extra.Arch = "", ""
		case "image":
			if !registryReference.MatchString(artifact.Image) || !ociDigest(artifact.Digest) || artifact.Tag == "" {
				return errors.New("invalid OCI image identity")
			}
			extra.Image, extra.Digest, extra.Tag = "", "", ""
		case "chart-digest":
			if !registryReference.MatchString(artifact.Chart) || !ociDigest(artifact.Digest) {
				return errors.New("invalid OCI chart identity")
			}
			extra.Chart, extra.Digest = "", ""
		case "chart":
			if !registryReference.MatchString(artifact.ImageRepository) || !ociDigest(artifact.ImageDigest) || artifact.ChartVersion == "" || artifact.AppVersion != artifact.ChartVersion {
				return errors.New("invalid chart payload identity")
			}
			extra.ImageRepository, extra.ImageDigest, extra.ChartVersion, extra.AppVersion = "", "", "", ""
		case "oci-payload":
			if !slices.Contains([]string{"image", "chart"}, artifact.SubjectKind) || !ociDigest(artifact.Digest) || !strings.HasSuffix(artifact.Subject, "@"+artifact.Digest) || !registryReference.MatchString(strings.TrimSuffix(artifact.Subject, "@"+artifact.Digest)) {
				return errors.New("invalid OCI signing payload identity")
			}
			extra.SubjectKind, extra.Subject, extra.Digest = "", "", ""
		default:
			return fmt.Errorf("unknown artifact kind %q", artifact.Kind)
		}
		if extra != (Artifact{}) {
			return errors.New("artifact carries fields outside its kind")
		}
	}
	return nil
}

var registryReference = regexp.MustCompile(`^ghcr\.io/[a-z0-9._-]+/[a-z0-9._/-]+$`)

func ociDigest(value string) bool {
	return strings.HasPrefix(value, "sha256:") && releaseidentity.Digest(strings.TrimPrefix(value, "sha256:")).Validate() == nil
}

func validateArtifactVersions(artifacts []Artifact, version string) error {
	for _, artifact := range artifacts {
		if (artifact.Kind == "image" && artifact.Tag != version) || (artifact.Kind == "chart" && (artifact.ChartVersion != version || artifact.AppVersion != version)) {
			return errors.New("OCI/chart artifact version differs from release")
		}
	}
	return nil
}
