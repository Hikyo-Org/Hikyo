// Package devupgrade owns synthetic, installation-local development custody.
// It grants no production trust. Callers must enforce the separate development
// datastore domain before using its output with the ordinary upgrade gate.
package devupgrade

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/sigstore/sigstore/pkg/signature"
)

const custodyName = "development-upgrade"
const primaryID = "local-development-primary"
const recoveryID = "local-development-recovery"

// Material contains public evidence only. Private custody never leaves its
// installation directory. Reopening returns the same byte-exact signed bundle.
// Directory remains a mutable local path; gate loaders must authenticate it.
type Material struct {
	Directory string
	Pinned    releasetrust.PinnedTrust
}

func newPrivate() ([]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func private(raw []byte) (*ecdsa.PrivateKey, []byte, error) {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(block.Headers) != 0 || len(rest) != 0 {
		return nil, nil, errors.New("invalid development private custody")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, errors.New("invalid development private custody")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, nil, errors.New("development custody requires P-256")
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func sign(key *ecdsa.PrivateKey, raw []byte) ([]byte, error) {
	signer, err := signature.LoadSigner(key, crypto.SHA256)
	if err != nil {
		return nil, err
	}
	sig, err := signer.SignMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return json.Marshal(releasetrust.LegacyBundle{Base64Signature: base64.StdEncoding.EncodeToString(sig)})
}

// documents constructs only the fixed synthetic declaration. No version,
// migration inventory, profile or source claim can be supplied by the caller.
func documents(recoveryRaw, primaryRaw []byte, signatures bool) (map[string][]byte, error) {
	recovery, recoveryPub, err := private(recoveryRaw)
	if err != nil {
		return nil, err
	}
	primary, primaryPub, err := private(primaryRaw)
	if err != nil {
		return nil, err
	}
	declaration, _, err := buildcompat.Development()
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{"recovery.key": recoveryRaw, "primary.key": primaryRaw, "recovery.pub": recoveryPub, "bundle/keys/" + primaryID + ".pub": primaryPub}
	put := func(name string, value any) error {
		raw, err := json.Marshal(value)
		if err == nil {
			files[name] = raw
		}
		return err
	}
	root := releasetrust.Root{Schema: "hikyo.dev/trust-root/v1", Recovery: releasetrust.RootKey{ID: recoveryID, PublicKey: "recovery.pub", SHA256: string(releaseidentity.Hash(recoveryPub))}, BootstrapPrimary: releasetrust.RootKey{ID: primaryID, PublicKey: primaryID + ".pub", SHA256: string(releaseidentity.Hash(primaryPub))}}
	if err := put("root.json", root); err != nil {
		return nil, err
	}
	candidate := releasetrust.Candidate{Version: buildcompat.DevelopmentVersion, Sequence: 1, Commit: buildcompat.DevelopmentCommit, KeyID: primaryID, PublicKey: primaryID + ".pub"}
	candidateRaw, err := json.Marshal(candidate)
	if err != nil {
		return nil, err
	}
	manifest := releasetrust.Manifest{Schema: "hikyo.dev/release-manifest/v1", Version: candidate.Version, Tag: "v" + candidate.Version, SourceCommit: candidate.Commit, ReleaseSequence: 1, SigningKeyID: primaryID, Artifacts: []releasetrust.Artifact{
		{Name: "release-candidate.json", Kind: "release-candidate", SHA256: string(releaseidentity.Hash(candidateRaw))},
		{Name: releasetrust.CompatibilityArtifact, Kind: "upgrade-compatibility", SHA256: string(releaseidentity.Hash(declaration))},
	}}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	digest := releaseidentity.Hash(manifestRaw)
	dir := "bundle/releases/" + string(digest) + "/"
	files[dir+"manifest.json"], files[dir+"release-candidate.json"], files[dir+releasetrust.CompatibilityArtifact] = manifestRaw, candidateRaw, declaration
	metadata := releasetrust.Metadata{Schema: "hikyo.dev/trust-metadata/v1", Sequence: 1, PrimaryKeys: []releasetrust.Primary{{ID: primaryID, PublicKey: primaryID + ".pub", SHA256: root.BootstrapPrimary.SHA256, ValidFromReleaseSequence: 1}}, Releases: []releasetrust.Release{{Version: candidate.Version, Sequence: 1, ManifestSHA256: string(digest)}}}
	metadata.Recovery.ID, metadata.Recovery.SHA256 = recoveryID, root.Recovery.SHA256
	metadata.Event.Type, metadata.Event.SignedBy = "release", recoveryID
	seq := int64(1)
	version := candidate.Version
	metadata.HighestRelease, metadata.HighestReleaseSequence = &version, &seq
	if err := put("bundle/metadata.json", metadata); err != nil {
		return nil, err
	}
	catalog := releasetrust.Catalog{Schema: "hikyo.dev/upgrade-trust/v1", Sequence: 1, StableMetadataSHA256: releaseidentity.Hash(files["bundle/metadata.json"]), NightlyPolicies: []releaseidentity.Digest{}, Bridges: []releaseidentity.Digest{}}
	if err := put("bundle/catalog.json", catalog); err != nil {
		return nil, err
	}
	index := upgradebundle.Index{Format: upgradebundle.IndexFormat, PrimaryKeyIDs: []string{primaryID}, Releases: []upgradebundle.ReleaseEntry{{Profile: releaseidentity.StableV1, ManifestSHA256: digest}}, Bridges: []releaseidentity.Digest{}}
	if err := put("bundle/index.json", index); err != nil {
		return nil, err
	}
	for _, s := range []struct {
		name, source string
		key          *ecdsa.PrivateKey
	}{{"bundle/metadata.sigstore.json", "bundle/metadata.json", recovery}, {"bundle/catalog.sigstore.json", "bundle/catalog.json", recovery}, {dir + "manifest.sigstore.json", dir + "manifest.json", primary}} {
		files[s.name] = nil
		if signatures {
			files[s.name], err = sign(s.key, files[s.source])
			if err != nil {
				return nil, err
			}
		}
	}
	return files, nil
}

func verify(files map[string][]byte) (releasetrust.PinnedTrust, error) {
	expected, err := documents(files["recovery.key"], files["primary.key"], false)
	if err != nil {
		return releasetrust.PinnedTrust{}, err
	}
	if len(expected) != len(files) {
		return releasetrust.PinnedTrust{}, errors.New("unknown or missing development custody members")
	}
	for name, want := range expected {
		got, exists := files[name]
		if !exists || len(got) == 0 || (want != nil && !bytes.Equal(got, want)) {
			return releasetrust.PinnedTrust{}, errors.New("development custody differs from current source declaration or saved identity")
		}
	}
	pinned := releasetrust.PinnedTrust{Root: bytes.Clone(files["root.json"]), RecoveryPublicKey: bytes.Clone(files["recovery.pub"])}
	snapshot, err := releasetrust.VerifySnapshot(pinned, releasetrust.SnapshotMaterial{Metadata: files["bundle/metadata.json"], MetadataSignature: files["bundle/metadata.sigstore.json"], Catalog: files["bundle/catalog.json"], CatalogSignature: files["bundle/catalog.sigstore.json"], PrimaryKeys: map[string][]byte{primaryID: files["bundle/keys/"+primaryID+".pub"]}}, releaseidentity.SnapshotFloor{})
	if err != nil {
		return releasetrust.PinnedTrust{}, err
	}
	for name := range files {
		if len(name) > len("manifest.json") && name[len(name)-len("manifest.json"):] == "manifest.json" {
			dir := name[:len(name)-len("manifest.json")]
			release, err := releasetrust.VerifyStable(snapshot, releasetrust.StableMaterial{Manifest: files[name], ManifestSignature: files[dir+"manifest.sigstore.json"], Candidate: files[dir+"release-candidate.json"], Compatibility: files[dir+releasetrust.CompatibilityArtifact]})
			if err != nil {
				return releasetrust.PinnedTrust{}, err
			}
			node, err := upgradecompat.Bind(release, files[dir+releasetrust.CompatibilityArtifact])
			if err != nil {
				return releasetrust.PinnedTrust{}, err
			}
			if err := buildcompat.VerifyDevelopment(node); err != nil {
				return releasetrust.PinnedTrust{}, err
			}
			return pinned, nil
		}
	}
	return releasetrust.PinnedTrust{}, errors.New("development manifest missing")
}
