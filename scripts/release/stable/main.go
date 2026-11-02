// stable prepares public release documents and verifies downloaded evidence.
// It never reads a private key or grants publication authority.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Masterminds/semver/v3"
)

const keylessID = "github-actions-stable"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "stable release:", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("require policy, preflight, candidate, prepare, or verify")
	}
	fs := flag.NewFlagSet("stable", flag.ContinueOnError)
	trust := fs.String("trust", "release/trust", "independently pinned public trust directory")
	directory := fs.String("directory", "", "complete signed release directory")
	version := fs.String("version", "", "expected release version without v")
	commit := fs.String("commit", "", "expected full source commit")
	out := fs.String("out", "", "new public document path")
	state := fs.String("state", "", "persisted rollback/equivocation floor")
	latest := fs.Bool("latest", false, "require signed latest release")
	published := fs.Bool("published", false, "verify publication evidence (does not publish)")
	trustOnly := fs.Bool("trust-only", false, "authenticate current trust without selecting a release")
	historical := fs.Bool("historical", false, "verify an authorized older release against current trust")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *historical && *latest {
		return errors.New("latest and historical are mutually exclusive")
	}
	loaded, err := loadTrust(*trust)
	if err != nil {
		return err
	}
	if args[0] == "policy" {
		return createPolicy(loaded, *trust, *out)
	}
	if !loaded.snapshot.StableKeylessEnabled() && !(args[0] == "verify" && *historical) {
		return errors.New("stable keyless policy has not been recovery-authorized; run the one-time setup")
	}
	switch args[0] {
	case "preflight":
		_, err := fmt.Fprintln(output, "Authenticated stable keyless release policy")
		return err
	case "candidate":
		candidate, err := nextCandidate(loaded, *version, *commit)
		if err != nil {
			return err
		}
		return writeCandidate(*out, candidate)
	case "prepare":
		return prepare(loaded, *directory)
	case "verify":
		if *trustOnly {
			return verifyTrustOnly(loaded, *state, output)
		}
		return verifyDirectory(loaded, *directory, *version, *commit, *state, *latest, *published, *historical, output)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type publicTrust struct {
	pinned   releasetrust.PinnedTrust
	material releasetrust.SnapshotMaterial
	snapshot releasetrust.Snapshot
	metadata releasetrust.Metadata
	catalog  releasetrust.Catalog
}

func readDocument(directory, name string) ([]byte, error) {
	if !releaseidentity.SafeName(name) {
		return nil, errors.New("unsafe public document name")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("public document %s is not a regular file", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, releasetrust.MaxDocumentBytes+1))
	if len(raw) > releasetrust.MaxDocumentBytes {
		return nil, errors.New("public document exceeds byte bound")
	}
	return raw, err
}

func loadTrust(directory string) (publicTrust, error) {
	var t publicTrust
	var err error
	t.pinned.Root, err = readDocument(directory, "root.json")
	if err != nil {
		return t, err
	}
	var root releasetrust.Root
	if err := definitions.DecodeStrict(t.pinned.Root, &root); err != nil {
		return t, err
	}
	t.pinned.RecoveryPublicKey, err = readDocument(directory, root.Recovery.PublicKey)
	if err != nil {
		return t, err
	}
	for name, dst := range map[string]*[]byte{
		"metadata.json": &t.material.Metadata, "metadata.sigstore.json": &t.material.MetadataSignature,
		"catalog.json": &t.material.Catalog, "catalog.sigstore.json": &t.material.CatalogSignature,
	} {
		*dst, err = readDocument(directory, name)
		if err != nil {
			return t, err
		}
	}
	if err := definitions.DecodeStrict(t.material.Metadata, &t.metadata); err != nil {
		return t, err
	}
	if err := definitions.DecodeStrict(t.material.Catalog, &t.catalog); err != nil {
		return t, err
	}
	if len(t.metadata.PrimaryKeys) > 256 {
		return t, errors.New("public key inventory exceeds bound")
	}
	t.material.PrimaryKeys = map[string][]byte{}
	for _, key := range t.metadata.PrimaryKeys {
		raw, err := readDocument(directory, key.PublicKey)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && (key.Revoked || (key.Pending != nil && *key.Pending)) {
				continue
			}
			return t, err
		}
		t.material.PrimaryKeys[key.ID] = raw
	}
	for name, dst := range map[string]*[]byte{
		"stable-policy.json": &t.material.StablePolicy, "stable-policy.sigstore.json": &t.material.StablePolicySignature,
		"stable-trusted-root.json": &t.material.StableTrustedRoot,
	} {
		*dst, err = readDocument(directory, name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return t, err
		}
	}
	t.material.NightlyPolicy, err = readDocument(filepath.Join(directory, "nightly"), "policy.json")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return t, err
	}
	t.snapshot, err = releasetrust.VerifySnapshot(t.pinned, t.material, releasetrust.SnapshotFloor{})
	return t, err
}

func createPolicy(t publicTrust, directory, out string) error {
	if len(t.material.StablePolicy) != 0 {
		return errors.New("stable policy already exists; policy rotation requires explicit review")
	}
	raw, err := readDocument(filepath.Join(directory, "nightly"), "policy.json")
	if err != nil {
		return err
	}
	var nightly releasetrust.NightlyPolicy
	if err := definitions.DecodeStrict(raw, &nightly); err != nil {
		return err
	}
	if err := nightly.Validate(); err != nil {
		return err
	}
	if !slices.Contains(t.catalog.NightlyPolicies, releaseidentity.Hash(raw)) {
		return errors.New("nightly policy lacks current recovery authorization")
	}
	trustedRoot, err := readDocument(filepath.Join(directory, "nightly"), "trusted-root.json")
	if err != nil {
		return err
	}
	if releaseidentity.Hash(trustedRoot) != nightly.TrustedRootSHA256 {
		return errors.New("Sigstore root hash mismatch")
	}
	nightly.Schema = "hikyo.dev/stable-policy/v1"
	nightly.WorkflowPath = ".github/workflows/release.yml"
	nightly.ProtectedRef = "refs/tags/v*"
	nightly.RevokedManifests = []releaseidentity.Digest{}
	minimum := int64(1)
	if t.metadata.HighestReleaseSequence != nil {
		minimum = *t.metadata.HighestReleaseSequence + 1
	}
	policy := releasetrust.StablePolicy{NightlyPolicy: nightly, MinimumReleaseSequence: minimum,
		PrimaryKeysSHA256: releasetrust.PrimaryKeysDigest(t.metadata.PrimaryKeys),
		NightlyPolicies:   slices.Clone(t.catalog.NightlyPolicies), Bridges: slices.Clone(t.catalog.Bridges)}
	if err := policy.Validate(); err != nil {
		return err
	}
	return writeNewJSON(out, policy)
}

func nextCandidate(t publicTrust, version, commit string) (releasetrust.Candidate, error) {
	var result releasetrust.Candidate
	v, err := semver.StrictNewVersion(version)
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(commit) {
		return result, errors.New("strict version and full source commit required")
	}
	sequence := int64(1)
	if t.metadata.HighestReleaseSequence != nil {
		sequence = *t.metadata.HighestReleaseSequence + 1
		previous, err := semver.StrictNewVersion(*t.metadata.HighestRelease)
		if err != nil || !v.GreaterThan(previous) {
			return result, errors.New("new version must be greater than signed latest")
		}
	}
	if sequence < 1 || sequence > 9007199254740991 || sequence == math.MaxInt64 {
		return result, errors.New("release sequence exhausted")
	}
	policy, enabled := t.snapshot.StablePolicy()
	if !enabled || sequence < policy.MinimumReleaseSequence {
		return result, errors.New("release sequence is outside delegated stable policy")
	}
	if err := releaseidentity.ValidateTarget(releaseidentity.StableV1, version, uint64(sequence), commit); err != nil {
		return result, err
	}
	for _, existing := range t.metadata.Releases {
		if existing.Version == version || existing.Sequence == sequence {
			return result, errors.New("release version or sequence already authorized")
		}
	}
	result = releasetrust.Candidate{Version: version, Sequence: sequence, Commit: commit, KeyID: keylessID, PublicKey: "stable-policy.json"}
	return result, nil
}

func prepare(t publicTrust, directory string) error {
	raw, err := readDocument(directory, "release-manifest.json")
	if err != nil {
		return err
	}
	var manifest releasetrust.Manifest
	if err := definitions.DecodeStrict(raw, &manifest); err != nil {
		return err
	}
	expected, err := nextCandidate(t, manifest.Version, manifest.SourceCommit)
	if err != nil {
		return err
	}
	candidateRaw, err := readDocument(directory, "release-candidate.json")
	if err != nil {
		return err
	}
	var candidate releasetrust.Candidate
	if err := definitions.DecodeStrict(candidateRaw, &candidate); err != nil {
		return err
	}
	if candidate != expected || manifest.ReleaseSequence != candidate.Sequence || manifest.SigningKeyID != keylessID || manifest.Schema != "hikyo.dev/release-manifest/v1" || manifest.Tag != "v"+candidate.Version {
		return errors.New("manifest does not match the next authorized candidate")
	}
	if t.metadata.Sequence >= 9007199254740991 || t.catalog.Sequence >= 9007199254740991 {
		return errors.New("trust sequence exhausted")
	}
	metadata := t.metadata
	metadata.Sequence++
	metadata.Event.Type, metadata.Event.SignedBy = "release", keylessID
	metadata.SourceCommit, metadata.ReleaseTag = candidate.Commit, "v"+candidate.Version
	metadata.HighestRelease, metadata.HighestReleaseSequence = &candidate.Version, &candidate.Sequence
	metadata.PendingRelease = nil
	metadata.Releases = append(slices.Clone(metadata.Releases), releasetrust.Release{Version: candidate.Version, Sequence: candidate.Sequence, ManifestSHA256: string(releaseidentity.Hash(raw))})
	metadataRaw, err := marshal(metadata)
	if err != nil {
		return err
	}
	catalog := t.catalog
	catalog.Sequence++
	catalog.StableMetadataSHA256 = releaseidentity.Hash(metadataRaw)
	catalog.StablePolicySHA256 = releaseidentity.Hash(t.material.StablePolicy)
	if err := writeNew(filepath.Join(directory, "metadata.json"), metadataRaw); err != nil {
		return err
	}
	if err := writeNewJSON(filepath.Join(directory, "catalog.json"), catalog); err != nil {
		return errors.Join(err, os.Remove(filepath.Join(directory, "metadata.json")))
	}
	return nil
}

func marshal(value any) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	return append(raw, '\n'), err
}

func writeNewJSON(path string, value any) error {
	raw, err := marshal(value)
	if err != nil {
		return err
	}
	return writeNew(path, raw)
}

func writeCandidate(path string, candidate releasetrust.Candidate) error {
	// Match jq -cS canonical candidate bytes used by the shell release tools.
	ordered := struct {
		Commit    string `json:"commit"`
		KeyID     string `json:"key_id"`
		PublicKey string `json:"public_key"`
		Sequence  int64  `json:"sequence"`
		Version   string `json:"version"`
	}{candidate.Commit, candidate.KeyID, candidate.PublicKey, candidate.Sequence, candidate.Version}
	raw, err := json.Marshal(ordered)
	if err != nil {
		return err
	}
	return writeNew(path, append(raw, '\n'))
}

func writeNew(path string, raw []byte) error {
	if path == "" {
		return errors.New("new output path is required")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, bytes.NewReader(raw))
	return errors.Join(err, f.Sync(), f.Close())
}
