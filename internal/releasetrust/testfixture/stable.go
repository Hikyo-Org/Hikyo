// Package testfixture creates ephemeral signed release evidence for tests.
// It never constructs verified authority or supplies a production trust root.
package testfixture

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
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/sigstore/sigstore/pkg/signature"
)

// Fixture exposes mutable untrusted claims so tests can sign malformed evidence
// and demonstrate that the real verifier refuses it.
type Fixture struct {
	Pinned         releasetrust.PinnedTrust
	Metadata       releasetrust.Metadata
	Catalog        releasetrust.Catalog
	PrimaryPublic  []byte
	RecoverySigner signature.Signer
	PrimarySigner  signature.Signer
}

type SignedRelease struct {
	Identity releaseidentity.Identity
	Material releasetrust.StableMaterial
	Payloads map[string][]byte
}

func JSON(t testing.TB, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func Sign(t testing.TB, signer signature.Signer, raw []byte) []byte {
	t.Helper()
	sig, err := signer.SignMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return JSON(t, releasetrust.LegacyBundle{Base64Signature: base64.StdEncoding.EncodeToString(sig)})
}

func key(t testing.TB) (signature.Signer, []byte) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := signature.LoadSigner(private, crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func New(t testing.TB) *Fixture {
	t.Helper()
	recoverySigner, recoveryPublic := key(t)
	primarySigner, primaryPublic := key(t)
	root := releasetrust.Root{Schema: "hikyo.dev/trust-root/v1",
		Recovery:         releasetrust.RootKey{ID: "test-recovery", PublicKey: "recovery.pub", SHA256: string(releaseidentity.Hash(recoveryPublic))},
		BootstrapPrimary: releasetrust.RootKey{ID: "test-primary", PublicKey: "primary.pub", SHA256: string(releaseidentity.Hash(primaryPublic))}}
	f := &Fixture{Pinned: releasetrust.PinnedTrust{Root: JSON(t, root), RecoveryPublicKey: recoveryPublic}, PrimaryPublic: primaryPublic, RecoverySigner: recoverySigner, PrimarySigner: primarySigner}
	f.Metadata.Schema, f.Metadata.Sequence = "hikyo.dev/trust-metadata/v1", 1
	f.Metadata.Recovery.ID, f.Metadata.Recovery.SHA256 = root.Recovery.ID, root.Recovery.SHA256
	f.Metadata.Event.Type, f.Metadata.Event.SignedBy = "release", root.Recovery.ID
	f.Metadata.PrimaryKeys = []releasetrust.Primary{{ID: root.BootstrapPrimary.ID, PublicKey: root.BootstrapPrimary.PublicKey, SHA256: root.BootstrapPrimary.SHA256, ValidFromReleaseSequence: 1}}
	f.Metadata.Releases = []releasetrust.Release{}
	f.Catalog = releasetrust.Catalog{Schema: "hikyo.dev/upgrade-trust/v1", Sequence: 1, NightlyPolicies: []releaseidentity.Digest{}, Bridges: []releaseidentity.Digest{}}
	return f
}

func (f *Fixture) AddStable(t testing.TB, version string, sequence int64, commit string, compatibility []byte) SignedRelease {
	t.Helper()
	candidate := JSON(t, releasetrust.Candidate{Version: version, Sequence: sequence, Commit: commit, KeyID: "test-primary", PublicKey: "primary.pub"})
	payload := []byte("synthetic platform payload for " + version)
	manifest := JSON(t, releasetrust.Manifest{Schema: "hikyo.dev/release-manifest/v1", Version: version, Tag: "v" + version, SourceCommit: commit, ReleaseSequence: sequence, SigningKeyID: "test-primary", Artifacts: []releasetrust.Artifact{
		{Name: "release-candidate.json", Kind: "release-candidate", SHA256: string(releaseidentity.Hash(candidate))},
		{Name: releasetrust.CompatibilityArtifact, Kind: "upgrade-compatibility", SHA256: string(releaseidentity.Hash(compatibility))},
		{Name: "hikyo_linux_arm64.tar.gz", Kind: "binary", SHA256: string(releaseidentity.Hash(payload))},
	}})
	identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: version, Sequence: uint64(sequence), Commit: commit, CompatibilitySHA256: releaseidentity.Hash(compatibility), ManifestSHA256: releaseidentity.Hash(manifest)}
	f.Metadata.Releases = append(f.Metadata.Releases, releasetrust.Release{Version: version, Sequence: sequence, ManifestSHA256: string(identity.ManifestSHA256)})
	if f.Metadata.HighestReleaseSequence == nil || sequence > *f.Metadata.HighestReleaseSequence {
		f.Metadata.HighestRelease, f.Metadata.HighestReleaseSequence = &version, &sequence
	}
	return SignedRelease{Identity: identity, Material: releasetrust.StableMaterial{Manifest: manifest, ManifestSignature: Sign(t, f.PrimarySigner, manifest), Candidate: candidate, Compatibility: bytes.Clone(compatibility)}, Payloads: map[string][]byte{"hikyo_linux_arm64.tar.gz": payload}}
}

func (f *Fixture) Material(t testing.TB) releasetrust.SnapshotMaterial {
	t.Helper()
	metadata := JSON(t, f.Metadata)
	catalog := f.Catalog
	catalog.StableMetadataSHA256 = releaseidentity.Hash(metadata)
	rawCatalog := JSON(t, catalog)
	return releasetrust.SnapshotMaterial{Metadata: metadata, MetadataSignature: Sign(t, f.RecoverySigner, metadata), Catalog: rawCatalog, CatalogSignature: Sign(t, f.RecoverySigner, rawCatalog), PrimaryKeys: map[string][]byte{"test-primary": bytes.Clone(f.PrimaryPublic)}}
}

func (f *Fixture) Snapshot(t testing.TB) releasetrust.Snapshot {
	t.Helper()
	snapshot, err := releasetrust.VerifySnapshot(f.Pinned, f.Material(t), releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func (f *Fixture) AddBridge(t testing.TB, statement releasetrust.BridgeStatement) releasetrust.BridgeMaterial {
	t.Helper()
	raw := JSON(t, statement)
	f.Catalog.Bridges = append(f.Catalog.Bridges, releaseidentity.Hash(raw))
	return releasetrust.BridgeMaterial{Statement: raw, Signature: Sign(t, f.RecoverySigner, raw)}
}
