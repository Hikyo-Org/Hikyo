package testfixture

import (
	"bytes"
	"crypto/mldsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strconv"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// PQMember is one nonproduction signer. Every key is generated at test time and
// never leaves the process; nothing here is, or may become, a production key.
type PQMember struct {
	Algorithm, KeyID string
	Sign             func(message []byte) []byte
}

// PQFixture extends the keyless stable-workflow fixture with a PQ-capable root,
// recovery and release ML-DSA keys, a signed keyset and standard signature sets.
type PQFixture struct {
	*Fixture
	Material releasetrust.SnapshotMaterial
	Stable   releasetrust.StableMaterial
	RootV2   []byte
	KeyFiles map[string][]byte
	Pin      releasetrust.PQPin

	RecoveryKey, ReleaseKey *mldsa.PrivateKey
	Keyset                  releasetrust.PQKeyset
	KeysetRaw               []byte
	Sets                    releasetrust.PQSnapshotSets
	ManifestSet             []byte
	Version                 string
	Commit                  string
	// SignKeyless is the test-local delegated CI identity (nil on the legacy track).
	SignKeyless func(payload []byte, commit, tag string) []byte
}

func mldsaPEM(t testing.TB, key *mldsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func newMLDSA(t testing.TB) *mldsa.PrivateKey {
	t.Helper()
	key, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// MLDSAMember signs with Options.Context equal to the kind label, as the
// verifier requires. SignDeterministic keeps fixtures reproducible.
func MLDSAMember(t testing.TB, key *mldsa.PrivateKey, kind string) PQMember {
	t.Helper()
	id, err := releasetrust.PQKeyID(releasetrust.AlgorithmMLDSA65, mldsaPEM(t, key))
	if err != nil {
		t.Fatal(err)
	}
	return PQMember{Algorithm: releasetrust.AlgorithmMLDSA65, KeyID: id, Sign: func(message []byte) []byte {
		sig, err := key.SignDeterministic(message, &mldsa.Options{Context: "hikyo/release-trust/" + kind + "/v1"})
		if err != nil {
			t.Fatal(err)
		}
		return sig
	}}
}

// NewPQStableWorkflow returns a delegated (keyless) stable fixture with a PQ
// pin and valid standard evidence for it.
func NewPQStableWorkflow(t testing.TB) *PQFixture {
	t.Helper()
	f, material, stable, sign := StableWorkflow(t)
	fx := attachPQ(t, f, material, stable)
	fx.SignKeyless = sign
	return fx
}

// NewPQLegacy returns the recovery-signed track (metadata and catalog signed by
// the P-256 recovery key, no delegation) with a PQ pin. Its releases are keyed
// and therefore historical: they have no PQ signer (E-12).
func NewPQLegacy(t testing.TB) *PQFixture {
	t.Helper()
	f := New(t)
	signed := f.AddStable(t, "1.0.0", 1, strings.Repeat("a", 40), []byte("compatibility"))
	return attachPQ(t, f, f.Material(t), signed.Material)
}

func attachPQ(t testing.TB, f *Fixture, material releasetrust.SnapshotMaterial, stable releasetrust.StableMaterial) *PQFixture {
	t.Helper()
	fx := &PQFixture{Fixture: f, Material: material, Stable: stable, Version: "1.0.0", Commit: strings.Repeat("a", 40), RecoveryKey: newMLDSA(t), ReleaseKey: newMLDSA(t)}
	var v1 releasetrust.Root
	if err := json.Unmarshal(f.Pinned.Root, &v1); err != nil {
		t.Fatal(err)
	}
	recoveryPEM, releasePEM := mldsaPEM(t, fx.RecoveryKey), mldsaPEM(t, fx.ReleaseKey)
	fx.KeyFiles = map[string][]byte{v1.Recovery.PublicKey: bytes.Clone(f.Pinned.RecoveryPublicKey), "recovery-mldsa-1.pub": recoveryPEM, "release-mldsa-1.pub": releasePEM}
	fx.RootV2 = JSON(t, releasetrust.PQRoot{Root: releasetrust.Root{Schema: releasetrust.PQRootSchema, Recovery: v1.Recovery, BootstrapPrimary: v1.BootstrapPrimary}, PQ: releasetrust.PQRootPolicy{Profile: releasetrust.PQProfile, Generation: 1, Recovery: []releasetrust.PQRootKey{
		{Algorithm: releasetrust.AlgorithmECDSAP256, ID: v1.Recovery.ID, PublicKey: v1.Recovery.PublicKey, SHA256: v1.Recovery.SHA256},
		{Algorithm: releasetrust.AlgorithmMLDSA65, ID: "recovery-mldsa-1", PublicKey: "recovery-mldsa-1.pub", SHA256: string(releaseidentity.Hash(recoveryPEM))},
	}}})
	pin, err := releasetrust.NewPQPin(fx.RootV2, fx.KeyFiles)
	if err != nil {
		t.Fatal(err)
	}
	fx.Pin = pin
	fx.Keyset = releasetrust.PQKeyset{Schema: releasetrust.PQKeysetSchema, Sequence: 1, Keys: []releasetrust.PQReleaseKey{{Algorithm: releasetrust.AlgorithmMLDSA65, ID: "release-mldsa-1", PublicKey: "release-mldsa-1.pub", SHA256: string(releaseidentity.Hash(releasePEM)), ValidFromReleaseSequence: 1}}}
	fx.Resign(t)
	return fx
}

// Recovery returns the classical P-256 and ML-DSA recovery members for kind.
func (fx *PQFixture) Recovery(t testing.TB, kind string) []PQMember {
	t.Helper()
	classicalID, err := releasetrust.PQKeyID(releasetrust.AlgorithmECDSAP256, fx.Pinned.RecoveryPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	classical := PQMember{Algorithm: releasetrust.AlgorithmECDSAP256, KeyID: classicalID, Sign: func(message []byte) []byte {
		sig, err := fx.RecoverySigner.SignMessage(bytes.NewReader(message))
		if err != nil {
			t.Fatal(err)
		}
		return sig
	}}
	return []PQMember{classical, MLDSAMember(t, fx.RecoveryKey, kind)}
}

// ReleaseMembers returns the maintainer-held release member for kind.
func (fx *PQFixture) ReleaseMembers(t testing.TB, kind string) []PQMember {
	return []PQMember{MLDSAMember(t, fx.ReleaseKey, kind)}
}

// Set signs payload under binding with signers, committing to suite.
func (fx *PQFixture) Set(t testing.TB, suite, signers []PQMember, binding releasetrust.PQBinding, payload []byte) []byte {
	t.Helper()
	members := make([]releasetrust.PQSuiteMember, len(suite))
	for i, member := range suite {
		members[i] = releasetrust.PQSuiteMember{Algorithm: member.Algorithm, KeyID: member.KeyID}
	}
	message, err := releasetrust.PQSigningInput(fx.Pin, members, binding, payload)
	if err != nil {
		t.Fatal(err)
	}
	set := releasetrust.PQSignatureSet{Schema: releasetrust.PQSignatureSetSchema}
	for _, signer := range signers {
		set.Signatures = append(set.Signatures, releasetrust.PQSignature{Algorithm: signer.Algorithm, KeyID: signer.KeyID, Signature: base64.StdEncoding.EncodeToString(signer.Sign(message))})
	}
	return JSON(t, set)
}

// TrustBinding mirrors the verifier's derivation from the authenticated
// metadata: highest release as version, the delegated source commit if any.
func (fx *PQFixture) TrustBinding(kind, subject string, sequence int64, policy releaseidentity.Digest) releasetrust.PQBinding {
	var metadata releasetrust.Metadata
	if err := json.Unmarshal(fx.Material.Metadata, &metadata); err != nil {
		panic(err)
	}
	version := ""
	if metadata.HighestRelease != nil {
		version = *metadata.HighestRelease
	}
	return releasetrust.PQBinding{Kind: kind, Channel: string(releaseidentity.StableV1), Version: version, Subject: subject, Source: metadata.SourceCommit, Sequence: uint64(sequence), Policy: policy}
}

// StablePolicyBinding and NightlyPolicyBinding mirror the verifier: policy
// bindings use only the policy's own fields, so they survive new releases.
func (fx *PQFixture) StablePolicyBinding(t testing.TB) releasetrust.PQBinding {
	var policy releasetrust.StablePolicy
	if err := json.Unmarshal(fx.Material.StablePolicy, &policy); err != nil {
		t.Fatal(err)
	}
	return releasetrust.PQBinding{Kind: releasetrust.PQKindStablePolicy, Channel: string(releaseidentity.StableV1), Subject: "stable-policy.json", Sequence: uint64(policy.MinimumReleaseSequence), Policy: releaseidentity.Hash(fx.Pinned.Root)}
}

func (fx *PQFixture) NightlyPolicyBinding() releasetrust.PQBinding {
	return releasetrust.PQBinding{Kind: releasetrust.PQKindNightlyPolicy, Channel: string(releaseidentity.NightlyV1), Subject: "nightly-policy.json", Sequence: 1, Policy: releaseidentity.Hash(fx.Pinned.Root)}
}

// Policy is the policy digest the verifier binds trust documents to.
func (fx *PQFixture) Policy() releaseidentity.Digest {
	if len(fx.Material.StablePolicy) > 0 {
		return releaseidentity.Hash(fx.Material.StablePolicy)
	}
	return releaseidentity.Hash(fx.Pinned.Root)
}

// TrustSigners are the members the verifier requires for metadata and catalog:
// the release role under a keyless delegation, otherwise the recovery set.
func (fx *PQFixture) TrustSigners(t testing.TB, kind string) []PQMember {
	if len(fx.Material.StablePolicy) > 0 {
		return fx.ReleaseMembers(t, kind)
	}
	return fx.Recovery(t, kind)
}

func (fx *PQFixture) ReleaseBinding(kind, subject string, sequence uint64) releasetrust.PQBinding {
	return releasetrust.PQBinding{Kind: kind, Channel: string(releaseidentity.StableV1), Version: fx.Version, Subject: subject, Source: fx.Commit, Sequence: sequence, Policy: fx.Policy()}
}

// Resign rebuilds every standard set from the current documents.
func (fx *PQFixture) Resign(t testing.TB) {
	t.Helper()
	fx.KeysetRaw = JSON(t, fx.Keyset)
	rec := func(kind string) []PQMember { return fx.Recovery(t, kind) }
	keysetBinding := releasetrust.PQBinding{Kind: releasetrust.PQKindKeyset, Channel: string(releaseidentity.StableV1), Subject: "pq-release-keys.json", Sequence: uint64(fx.Keyset.Sequence), Policy: fx.Pin.Digest()}
	var metadata releasetrust.Metadata
	if err := json.Unmarshal(fx.Material.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	var catalog releasetrust.Catalog
	if err := json.Unmarshal(fx.Material.Catalog, &catalog); err != nil {
		t.Fatal(err)
	}
	fx.Sets = releasetrust.PQSnapshotSets{
		Keyset: fx.KeysetRaw, KeysetSet: fx.Set(t, rec(releasetrust.PQKindKeyset), rec(releasetrust.PQKindKeyset), keysetBinding, fx.KeysetRaw), KeyFiles: fx.KeyFiles,
		MetadataSet: fx.Set(t, fx.TrustSigners(t, releasetrust.PQKindMetadata), fx.TrustSigners(t, releasetrust.PQKindMetadata), fx.TrustBinding(releasetrust.PQKindMetadata, "metadata.json", metadata.Sequence, fx.Policy()), fx.Material.Metadata),
		CatalogSet:  fx.Set(t, fx.TrustSigners(t, releasetrust.PQKindCatalog), fx.TrustSigners(t, releasetrust.PQKindCatalog), fx.TrustBinding(releasetrust.PQKindCatalog, "catalog.json", catalog.Sequence, fx.Policy()), fx.Material.Catalog),
	}
	if len(fx.Material.StablePolicy) > 0 {
		fx.Sets.StablePolicySet = fx.Set(t, rec(releasetrust.PQKindStablePolicy), rec(releasetrust.PQKindStablePolicy), fx.StablePolicyBinding(t), fx.Material.StablePolicy)
	}
	if len(fx.Material.NightlyPolicy) > 0 {
		fx.Sets.NightlyPolicySet = fx.Set(t, rec(releasetrust.PQKindNightlyPolicy), rec(releasetrust.PQKindNightlyPolicy), fx.NightlyPolicyBinding(), fx.Material.NightlyPolicy)
	}
	manifest := fx.ReleaseBinding(releasetrust.PQKindReleaseManifest, "release-manifest.json", 1)
	fx.ManifestSet = fx.Set(t, fx.ReleaseMembers(t, releasetrust.PQKindReleaseManifest), fx.ReleaseMembers(t, releasetrust.PQKindReleaseManifest), manifest, fx.Stable.Manifest)
}

// ArtifactSet signs one artifact's exact bytes as the release member.
func (fx *PQFixture) ArtifactSet(t testing.TB, name string, payload []byte) []byte {
	t.Helper()
	return fx.Set(t, fx.ReleaseMembers(t, releasetrust.PQKindArtifact), fx.ReleaseMembers(t, releasetrust.PQKindArtifact), fx.ReleaseBinding(releasetrust.PQKindArtifact, name, 1), payload)
}

// Reissue re-signs metadata and catalog (classically by the delegated test
// identity, and by the PQ release member) at a higher sequence, as a routine
// subsequent update does.
func (fx *PQFixture) Reissue(t testing.TB, sequence int64) {
	t.Helper()
	fx.Metadata.Sequence, fx.Catalog.Sequence = sequence, sequence
	m := fx.Material
	m.Metadata = JSON(t, fx.Metadata)
	m.MetadataSignature = fx.SignKeyless(m.Metadata, fx.Metadata.SourceCommit, fx.Metadata.ReleaseTag)
	fx.Catalog.StableMetadataSHA256 = releaseidentity.Hash(m.Metadata)
	m.Catalog = JSON(t, fx.Catalog)
	m.CatalogSignature = fx.SignKeyless(m.Catalog, fx.Metadata.SourceCommit, fx.Metadata.ReleaseTag)
	fx.Material = m
	fx.Resign(t)
}

// ReplaceRecovery models an operator-installed root replacement after the
// ML-DSA recovery key was lost or compromised: a new key, a higher generation,
// a new pin and a keyset re-signed at the next sequence by the new recovery set.
// The classical recovery key and the release key are unchanged.
func (fx *PQFixture) ReplaceRecovery(t testing.TB) {
	t.Helper()
	var root releasetrust.PQRoot
	if err := json.Unmarshal(fx.RootV2, &root); err != nil {
		t.Fatal(err)
	}
	fx.RecoveryKey = newMLDSA(t)
	pemKey := mldsaPEM(t, fx.RecoveryKey)
	fx.KeyFiles["recovery-mldsa-2.pub"] = pemKey
	root.PQ.Generation++
	root.PQ.Recovery[1] = releasetrust.PQRootKey{Algorithm: releasetrust.AlgorithmMLDSA65, ID: "recovery-mldsa-2", PublicKey: "recovery-mldsa-2.pub", SHA256: string(releaseidentity.Hash(pemKey))}
	fx.RootV2 = JSON(t, root)
	pin, err := releasetrust.NewPQPin(fx.RootV2, fx.KeyFiles)
	if err != nil {
		t.Fatal(err)
	}
	fx.Pin = pin
	fx.Keyset.Sequence++
	fx.Resign(t)
}

// AddNightlyPolicy (recovery-signed track only) publishes a currently authorized
// nightly policy beside the catalog and signs its PQ set.
func (fx *PQFixture) AddNightlyPolicy(t testing.TB) {
	t.Helper()
	policy, _, _ := WorkflowSigner(t)
	nightly := policy.NightlyPolicy
	nightly.Schema, nightly.ProtectedRef = "hikyo.dev/nightly-policy/v1", "refs/heads/main"
	if nightly.RevokedManifests == nil {
		nightly.RevokedManifests = []releaseidentity.Digest{}
	}
	fx.NightlyPolicy = JSON(t, nightly)
	fx.Catalog.NightlyPolicies = append(fx.Catalog.NightlyPolicies, releaseidentity.Hash(fx.NightlyPolicy))
	fx.Material = fx.Fixture.Material(t)
	fx.Resign(t)
}

// SecondRelease (delegated track only) publishes metadata and catalog for a
// later release, 1.0.1 at sequence 2 from another commit, with the policy bytes
// unchanged. The second manifest itself is not built: only trust documents move.
func (fx *PQFixture) SecondRelease(t testing.TB) {
	t.Helper()
	version, sequence := "1.0.1", int64(2)
	fx.Metadata.Releases = append(fx.Metadata.Releases, releasetrust.Release{Version: version, Sequence: sequence, ManifestSHA256: string(releaseidentity.Hash([]byte("second manifest")))})
	fx.Metadata.HighestRelease, fx.Metadata.HighestReleaseSequence = &version, &sequence
	fx.Metadata.SourceCommit, fx.Metadata.ReleaseTag = strings.Repeat("b", 40), "v"+version
	fx.Reissue(t, fx.Metadata.Sequence+1)
}

// ReplaceReleaseKey generates a new release key, makes it the only keyset entry
// from release sequence 1 and advances the keyset sequence. It never un-revokes
// an old key. The caller re-signs with Reissue.
func (fx *PQFixture) ReplaceReleaseKey(t testing.TB) {
	t.Helper()
	fx.ReleaseKey = newMLDSA(t)
	pemKey := mldsaPEM(t, fx.ReleaseKey)
	name := "release-mldsa-" + strconv.Itoa(len(fx.KeyFiles)+1) + ".pub"
	fx.KeyFiles[name] = pemKey
	fx.Keyset.Sequence++
	fx.Keyset.Keys = []releasetrust.PQReleaseKey{{Algorithm: releasetrust.AlgorithmMLDSA65, ID: strings.TrimSuffix(name, ".pub"), PublicKey: name, SHA256: string(releaseidentity.Hash(pemKey)), ValidFromReleaseSequence: 1}}
}
