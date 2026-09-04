package selfupdate

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Masterminds/semver/v3"
	"github.com/gofrs/flock"
)

var (
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type trustRoot struct {
	Schema           string       `json:"schema"`
	Recovery         trustRootKey `json:"recovery"`
	BootstrapPrimary trustRootKey `json:"bootstrap_primary"`
}

type trustRootKey struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
	SHA256    string `json:"sha256"`
}

type trustRelease struct {
	Version        string `json:"version"`
	Sequence       int64  `json:"sequence"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

type trustPrimary struct {
	ID                          string `json:"id"`
	PublicKey                   string `json:"public_key"`
	SHA256                      string `json:"sha256"`
	ValidFromReleaseSequence    int64  `json:"valid_from_release_sequence"`
	ValidThroughReleaseSequence *int64 `json:"valid_through_release_sequence"`
	Revoked                     bool   `json:"revoked"`
	Pending                     *bool  `json:"pending,omitempty"`
}

type trustMetadata struct {
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
	PrimaryKeys    []trustPrimary `json:"primary_keys"`
	Releases       []trustRelease `json:"releases"`
	PendingRelease *trustRelease  `json:"pending_release"`
}

type releaseManifest struct {
	Schema          string             `json:"schema"`
	Version         string             `json:"version"`
	Tag             string             `json:"tag"`
	SourceCommit    string             `json:"source_commit"`
	ReleaseSequence int64              `json:"release_sequence"`
	SigningKeyID    string             `json:"signing_key_id"`
	Artifacts       []manifestArtifact `json:"artifacts"`
}

type manifestArtifact struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
}

type releaseCandidate struct {
	Version   string `json:"version"`
	Sequence  int64  `json:"sequence"`
	Commit    string `json:"commit"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

// legacySignatureBundle is the cosign `--new-bundle-format=false` bundle the
// release ceremony emits. Only the signature is read; the certificate and Rekor
// entry it also carries are not consulted.
type legacySignatureBundle struct {
	Base64Signature string `json:"base64Signature"`
}

type verificationState struct {
	TrustSequence          int64   `json:"trust_sequence"`
	HighestReleaseSequence *int64  `json:"highest_release_sequence"`
	HighestRelease         *string `json:"highest_release"`
	MetadataSHA256         string  `json:"metadata_sha256"`
}

func (i *Installer) verifyStable(ctx context.Context, status updatecheck.Status, archiveName string, archive []byte) (err error) {
	if !status.Immutable {
		return errors.New("selfupdate: stable release is not immutable")
	}
	if i.config.StateDir == "" {
		return errors.New("selfupdate: stable trust state directory is unavailable")
	}
	if err := os.MkdirAll(i.config.StateDir, 0o700); err != nil {
		return fmt.Errorf("selfupdate: create trust state directory: %w", err)
	}
	lock := flock.New(filepath.Join(i.config.StateDir, "release-trust.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("selfupdate: acquire stable trust lock: %w", err)
	}
	if !locked {
		return errors.New("selfupdate: another Hikyo process is verifying an update")
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	if err := os.Chmod(lock.Path(), 0o600); err != nil {
		return fmt.Errorf("selfupdate: protect stable trust lock: %w", err)
	}
	return i.verifyStableLocked(ctx, status, archiveName, archive)
}

func (i *Installer) verifyStableLocked(ctx context.Context, status updatecheck.Status, archiveName string, archive []byte) error {
	rootRaw, err := decodeStamped("trust root", i.config.TrustRootBase64)
	if err != nil {
		return err
	}
	recoveryKey, err := decodeStamped("recovery public key", i.config.RecoveryKeyBase64)
	if err != nil {
		return err
	}
	var root trustRoot
	if err := json.Unmarshal(rootRaw, &root); err != nil {
		return fmt.Errorf("selfupdate: decode embedded trust root: %w", err)
	}
	if err := validateTrustRoot(root, recoveryKey); err != nil {
		return err
	}

	metadataRaw, err := i.downloadURL(ctx, trustURL("metadata.json"), maxTrustBytes)
	if err != nil {
		return err
	}
	metadataSignature, err := i.downloadURL(ctx, trustURL("metadata.sigstore.json"), maxTrustBytes)
	if err != nil {
		return err
	}
	var metadata trustMetadata
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		return fmt.Errorf("selfupdate: decode trust metadata: %w", err)
	}
	if err := validateTrustMetadata(root, metadata); err != nil {
		return err
	}
	if err := verifyBlobSignature(recoveryKey, metadataSignature, metadataRaw); err != nil {
		return fmt.Errorf("selfupdate: trust metadata signature invalid: %w", err)
	}

	primaryKeys := make(map[string][]byte, len(metadata.PrimaryKeys))
	for _, primary := range metadata.PrimaryKeys {
		raw, err := i.downloadURL(ctx, trustURL(primary.PublicKey), maxTrustBytes)
		if err != nil {
			return err
		}
		if digestHex(raw) != primary.SHA256 {
			return fmt.Errorf("selfupdate: primary public-key hash mismatch: %s", primary.PublicKey)
		}
		primaryKeys[primary.ID] = raw
	}

	manifestRaw, err := i.downloadNamedAsset(ctx, status, "release-manifest.json", maxTrustBytes)
	if err != nil {
		return err
	}
	manifestSignature, err := i.downloadNamedAsset(ctx, status, "release-manifest.sigstore.json", maxTrustBytes)
	if err != nil {
		return err
	}
	candidateRaw, err := i.downloadNamedAsset(ctx, status, "release-candidate.json", maxTrustBytes)
	if err != nil {
		return err
	}
	archiveSignature, err := i.downloadNamedAsset(ctx, status, archiveName+".sigstore.json", maxTrustBytes)
	if err != nil {
		return err
	}

	var manifest releaseManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return fmt.Errorf("selfupdate: decode release manifest: %w", err)
	}
	var candidate releaseCandidate
	if err := json.Unmarshal(candidateRaw, &candidate); err != nil {
		return fmt.Errorf("selfupdate: decode release candidate: %w", err)
	}
	primary, err := validateStableRelease(status, metadata, manifest, candidate, manifestRaw, candidateRaw, archiveName, archive)
	if err != nil {
		return err
	}
	primaryKey := primaryKeys[primary.ID]
	if len(primaryKey) == 0 {
		return fmt.Errorf("selfupdate: authorized primary key %s is unavailable", primary.ID)
	}
	if err := verifyBlobSignature(primaryKey, manifestSignature, manifestRaw); err != nil {
		return fmt.Errorf("selfupdate: release manifest signature invalid: %w", err)
	}
	if err := verifyBlobSignature(primaryKey, archiveSignature, archive); err != nil {
		return fmt.Errorf("selfupdate: release archive signature invalid: %w", err)
	}
	return updateVerificationState(filepath.Join(i.config.StateDir, "release-trust.json"), metadata, metadataRaw)
}

func validateTrustRoot(root trustRoot, recoveryKey []byte) error {
	if root.Schema != "hikyo.dev/trust-root/v1" || !validRootKey(root.Recovery) || !validRootKey(root.BootstrapPrimary) {
		return errors.New("selfupdate: embedded stable trust root is invalid")
	}
	if digestHex(recoveryKey) != root.Recovery.SHA256 {
		return errors.New("selfupdate: embedded recovery public-key hash mismatch")
	}
	return nil
}

func validRootKey(key trustRootKey) bool {
	return key.ID != "" && safeName(key.PublicKey) && sha256Pattern.MatchString(key.SHA256)
}

func validateTrustMetadata(root trustRoot, metadata trustMetadata) error {
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
	allReleases := append([]trustRelease(nil), metadata.Releases...)
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

func validateStableRelease(status updatecheck.Status, metadata trustMetadata, manifest releaseManifest, candidate releaseCandidate, manifestRaw, candidateRaw []byte, archiveName string, archive []byte) (trustPrimary, error) {
	if manifest.Schema != "hikyo.dev/release-manifest/v1" || manifest.Version != status.LatestVersion ||
		manifest.Tag != "v"+manifest.Version || manifest.ReleaseSequence < 1 || !commitPattern.MatchString(manifest.SourceCommit) ||
		manifest.SigningKeyID == "" || len(manifest.Artifacts) == 0 {
		return trustPrimary{}, errors.New("selfupdate: release manifest is invalid")
	}
	if _, err := semver.StrictNewVersion(manifest.Version); err != nil {
		return trustPrimary{}, errors.New("selfupdate: release manifest version is invalid")
	}
	if metadata.HighestRelease == nil || metadata.HighestReleaseSequence == nil ||
		*metadata.HighestRelease != manifest.Version || *metadata.HighestReleaseSequence != manifest.ReleaseSequence {
		return trustPrimary{}, errors.New("selfupdate: selected stable release is not the signed latest release")
	}
	releaseMatches := 0
	for _, release := range metadata.Releases {
		if release.Version == manifest.Version && release.Sequence == manifest.ReleaseSequence && release.ManifestSHA256 == digestHex(manifestRaw) {
			releaseMatches++
		}
	}
	if releaseMatches != 1 {
		return trustPrimary{}, errors.New("selfupdate: release manifest is not authorized by trust metadata")
	}
	if candidate.Version != manifest.Version || candidate.Sequence != manifest.ReleaseSequence || candidate.Commit != manifest.SourceCommit ||
		candidate.KeyID != manifest.SigningKeyID || !commitPattern.MatchString(candidate.Commit) || !safeName(candidate.PublicKey) {
		return trustPrimary{}, errors.New("selfupdate: release candidate does not match manifest identity")
	}
	seenArtifacts := map[string]bool{}
	archiveMatches, candidateMatches := 0, 0
	for _, artifact := range manifest.Artifacts {
		if !safeName(artifact.Name) || artifact.Kind == "" || !sha256Pattern.MatchString(artifact.SHA256) || seenArtifacts[artifact.Name] {
			return trustPrimary{}, errors.New("selfupdate: release manifest artifact list is invalid")
		}
		seenArtifacts[artifact.Name] = true
		if artifact.Name == archiveName && artifact.Kind == "binary" && artifact.SHA256 == digestHex(archive) {
			archiveMatches++
		}
		if artifact.Name == "release-candidate.json" && artifact.Kind == "release-candidate" && artifact.SHA256 == digestHex(candidateRaw) {
			candidateMatches++
		}
	}
	if archiveMatches != 1 || candidateMatches != 1 {
		return trustPrimary{}, errors.New("selfupdate: signed manifest does not bind the selected archive and candidate")
	}
	covering := make([]trustPrimary, 0, 1)
	for _, primary := range metadata.PrimaryKeys {
		if primary.ValidFromReleaseSequence <= manifest.ReleaseSequence &&
			(primary.ValidThroughReleaseSequence == nil || *primary.ValidThroughReleaseSequence >= manifest.ReleaseSequence) {
			covering = append(covering, primary)
		}
	}
	if len(covering) != 1 || covering[0].Revoked || (covering[0].Pending != nil && *covering[0].Pending) ||
		covering[0].ID != candidate.KeyID || covering[0].PublicKey != candidate.PublicKey {
		return trustPrimary{}, errors.New("selfupdate: release candidate is not authorized by an active primary key")
	}
	return covering[0], nil
}

func verifyBlobSignature(publicKeyPEM, bundleRaw, payload []byte) error {
	var bundle legacySignatureBundle
	if err := json.Unmarshal(bundleRaw, &bundle); err != nil || bundle.Base64Signature == "" {
		return errors.New("invalid legacy Cosign bundle")
	}
	signature, err := base64.StdEncoding.DecodeString(bundle.Base64Signature)
	if err != nil || len(signature) == 0 {
		return errors.New("invalid base64 signature")
	}
	block, rest := pem.Decode(publicKeyPEM)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 || block.Type != "PUBLIC KEY" {
		return errors.New("invalid PEM public key")
	}
	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}
	switch key := publicKey.(type) {
	case *ecdsa.PublicKey:
		if key.Curve.Params().BitSize != 256 && key.Curve.Params().BitSize != 384 && key.Curve.Params().BitSize != 521 {
			return errors.New("unsupported ECDSA curve")
		}
		digest := sha256.Sum256(payload)
		if !ecdsa.VerifyASN1(key, digest[:], signature) {
			return errors.New("ECDSA signature mismatch")
		}
	case *rsa.PublicKey:
		if key.Size() < 256 {
			return errors.New("RSA public key is smaller than 2048 bits")
		}
		digest := sha256.Sum256(payload)
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
			return fmt.Errorf("RSA signature mismatch: %w", err)
		}
	case ed25519.PublicKey:
		if !ed25519.Verify(key, payload, signature) {
			return errors.New("Ed25519 signature mismatch")
		}
	default:
		return fmt.Errorf("unsupported public key type %T", publicKey)
	}
	return nil
}

func updateVerificationState(path string, metadata trustMetadata, metadataRaw []byte) error {
	if raw, err := os.ReadFile(path); err == nil {
		var current verificationState
		if err := json.Unmarshal(raw, &current); err != nil || current.TrustSequence < 1 || !sha256Pattern.MatchString(current.MetadataSHA256) ||
			(current.HighestRelease == nil) != (current.HighestReleaseSequence == nil) {
			return errors.New("selfupdate: stable trust verification state is invalid")
		}
		if metadata.Sequence < current.TrustSequence {
			return errors.New("selfupdate: trust metadata rollback refused")
		}
		if metadata.Sequence == current.TrustSequence && digestHex(metadataRaw) != current.MetadataSHA256 {
			return errors.New("selfupdate: conflicting trust metadata at known sequence")
		}
		if current.HighestReleaseSequence != nil &&
			(metadata.HighestReleaseSequence == nil || *metadata.HighestReleaseSequence < *current.HighestReleaseSequence) {
			return errors.New("selfupdate: highest-release rollback refused")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("selfupdate: read stable trust state: %w", err)
	}
	state := verificationState{
		TrustSequence: metadata.Sequence, HighestReleaseSequence: metadata.HighestReleaseSequence,
		HighestRelease: metadata.HighestRelease, MetadataSHA256: digestHex(metadataRaw),
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".release-trust-*")
	if err != nil {
		return fmt.Errorf("selfupdate: create stable trust state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("selfupdate: persist stable trust state: %w", err)
	}
	return nil
}

func (i *Installer) downloadNamedAsset(ctx context.Context, status updatecheck.Status, name string, limit int64) ([]byte, error) {
	asset, err := exactAsset(status.LatestVersion, name, status.Assets)
	if err != nil {
		return nil, err
	}
	return i.download(ctx, asset, limit)
}

func decodeStamped(name, encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, fmt.Errorf("selfupdate: %s is not embedded; use the signed installer", name)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("selfupdate: embedded %s is invalid", name)
	}
	return raw, nil
}

func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name &&
		!strings.Contains(name, "..") && !strings.ContainsAny(name, `/\\`)
}

func trustURL(name string) string {
	return "https://raw.githubusercontent.com/Hikyo-Org/Hikyo/refs/heads/main/release/trust/" + name
}

func digestHex(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
