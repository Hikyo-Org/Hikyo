package releasetrust

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Masterminds/semver/v3"
)

// These exported structures are untrusted wire claims. Only VerifySnapshot
// and the release verification functions can construct verified authority.
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Root struct {
	Schema           string  `json:"schema"`
	Recovery         RootKey `json:"recovery"`
	BootstrapPrimary RootKey `json:"bootstrap_primary"`
}

type RootKey struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
	SHA256    string `json:"sha256"`
}

type Release struct {
	Version        string `json:"version"`
	Sequence       int64  `json:"sequence"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

type Primary struct {
	ID                          string `json:"id"`
	PublicKey                   string `json:"public_key"`
	SHA256                      string `json:"sha256"`
	ValidFromReleaseSequence    int64  `json:"valid_from_release_sequence"`
	ValidThroughReleaseSequence *int64 `json:"valid_through_release_sequence"`
	Revoked                     bool   `json:"revoked"`
	Pending                     *bool  `json:"pending,omitempty"`
}

type Metadata struct {
	Schema                 string  `json:"schema"`
	Sequence               int64   `json:"sequence"`
	HighestRelease         *string `json:"highest_release"`
	HighestReleaseSequence *int64  `json:"highest_release_sequence"`
	Recovery               struct {
		ID     string `json:"id"`
		SHA256 string `json:"sha256"`
	} `json:"recovery"`
	Event struct {
		Type     string `json:"type"`
		SignedBy string `json:"signed_by"`
	} `json:"event"`
	PrimaryKeys    []Primary `json:"primary_keys"`
	Releases       []Release `json:"releases"`
	PendingRelease *Release  `json:"pending_release"`
}

type Manifest struct {
	Schema          string     `json:"schema"`
	Version         string     `json:"version"`
	Tag             string     `json:"tag"`
	SourceCommit    string     `json:"source_commit"`
	ReleaseSequence int64      `json:"release_sequence"`
	SigningKeyID    string     `json:"signing_key_id"`
	Artifacts       []Artifact `json:"artifacts"`
}

type Artifact struct {
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	SHA256          string `json:"sha256"`
	Format          string `json:"format,omitempty"`
	Arch            string `json:"arch,omitempty"`
	Platform        string `json:"platform,omitempty"`
	Image           string `json:"image,omitempty"`
	Tag             string `json:"tag,omitempty"`
	Digest          string `json:"digest,omitempty"`
	Chart           string `json:"chart,omitempty"`
	ChartVersion    string `json:"chart_version,omitempty"`
	AppVersion      string `json:"app_version,omitempty"`
	ImageRepository string `json:"image_repository,omitempty"`
	ImageDigest     string `json:"image_digest,omitempty"`
	SubjectKind     string `json:"subject_kind,omitempty"`
	Subject         string `json:"subject,omitempty"`
}

type Candidate struct {
	Version   string `json:"version"`
	Sequence  int64  `json:"sequence"`
	Commit    string `json:"commit"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

func ValidateRoot(root Root, recoveryKey []byte) error {
	if root.Schema != "hikyo.dev/trust-root/v1" || !validRootKey(root.Recovery) || !validRootKey(root.BootstrapPrimary) {
		return errors.New("selfupdate: embedded stable trust root is invalid")
	}
	if digestHex(recoveryKey) != root.Recovery.SHA256 {
		return errors.New("selfupdate: embedded recovery public-key hash mismatch")
	}
	return nil
}

func validRootKey(key RootKey) bool {
	return key.ID != "" && safeName(key.PublicKey) && sha256Pattern.MatchString(key.SHA256)
}

func ValidateMetadata(root Root, metadata Metadata) error {
	validEvents := map[string]bool{"bootstrap": true, "release-candidate": true, "release": true, "rotation": true, "revocation": true}
	if metadata.Schema != "hikyo.dev/trust-metadata/v1" || metadata.Sequence < 1 ||
		metadata.Recovery.ID != root.Recovery.ID || metadata.Recovery.SHA256 != root.Recovery.SHA256 ||
		metadata.Event.SignedBy != root.Recovery.ID || !validEvents[metadata.Event.Type] || len(metadata.PrimaryKeys) == 0 ||
		(metadata.HighestRelease == nil) != (metadata.HighestReleaseSequence == nil) {
		return errors.New("selfupdate: current trust metadata is invalid")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	bootstrapMatches := 0
	for _, primary := range metadata.PrimaryKeys {
		if primary.ID == "" || !safeName(primary.PublicKey) || !sha256Pattern.MatchString(primary.SHA256) ||
			primary.ValidFromReleaseSequence < 1 ||
			(primary.ValidThroughReleaseSequence != nil && *primary.ValidThroughReleaseSequence < primary.ValidFromReleaseSequence) ||
			ids[primary.ID] || names[primary.PublicKey] {
			return errors.New("selfupdate: current trust metadata is invalid")
		}
		ids[primary.ID], names[primary.PublicKey] = true, true
		if primary.ID == root.BootstrapPrimary.ID && primary.PublicKey == root.BootstrapPrimary.PublicKey &&
			primary.SHA256 == root.BootstrapPrimary.SHA256 && primary.ValidFromReleaseSequence == 1 {
			bootstrapMatches++
		}
	}
	if bootstrapMatches != 1 {
		return errors.New("selfupdate: bootstrap primary does not match pinned root")
	}
	versions, sequences := map[string]bool{}, map[int64]bool{}
	allReleases := append([]Release(nil), metadata.Releases...)
	if metadata.PendingRelease != nil {
		allReleases = append(allReleases, *metadata.PendingRelease)
	}
	for _, release := range allReleases {
		if _, err := semver.StrictNewVersion(release.Version); err != nil || release.Sequence < 1 ||
			!sha256Pattern.MatchString(release.ManifestSHA256) || versions[release.Version] || sequences[release.Sequence] {
			return errors.New("selfupdate: current trust metadata is invalid")
		}
		versions[release.Version], sequences[release.Sequence] = true, true
	}
	if metadata.HighestRelease != nil {
		if _, err := semver.StrictNewVersion(*metadata.HighestRelease); err != nil || *metadata.HighestReleaseSequence < 1 {
			return errors.New("selfupdate: current trust metadata is invalid")
		}
	}
	return nil
}

func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.Contains(name, "..") && !strings.ContainsAny(name, `/\`)
}
func digestHex(raw []byte) string { return string(releaseidentity.Hash(raw)) }
