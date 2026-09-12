package testfixture

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

func StableWorkflow(t testing.TB) (*Fixture, releasetrust.SnapshotMaterial, releasetrust.StableMaterial, func([]byte, string, string) []byte) {
	return StableWorkflowWithCompatibility(t, []byte("compatibility"))
}

func StableWorkflowWithCompatibility(t testing.TB, compatibility []byte) (*Fixture, releasetrust.SnapshotMaterial, releasetrust.StableMaterial, func([]byte, string, string) []byte) {
	t.Helper()
	f := New(t)
	f.Metadata.Sequence = 2
	f.Catalog.Sequence = 2
	policy, root, sign := WorkflowSigner(t)
	policy.PrimaryKeysSHA256 = releasetrust.PrimaryKeysDigest(f.Metadata.PrimaryKeys)
	commit := strings.Repeat("a", 40)
	signed := f.AddStable(t, "1.0.0", 1, commit, compatibility)
	var manifest releasetrust.Manifest
	if err := json.Unmarshal(signed.Material.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.SigningKeyID = releasetrust.StableWorkflowSigner
	signed.Material.Candidate = JSON(t, releasetrust.Candidate{Version: "1.0.0", Sequence: 1, Commit: commit, KeyID: releasetrust.StableWorkflowSigner, PublicKey: "stable-policy.json"})
	for i := range manifest.Artifacts {
		if manifest.Artifacts[i].Kind == "release-candidate" {
			manifest.Artifacts[i].SHA256 = string(releaseidentity.Hash(signed.Material.Candidate))
		}
	}
	signed.Material.Provenance = StableProvenance(t, policy, manifest)
	signed.Material.ProvenanceSignature = sign(signed.Material.Provenance, commit, "v1.0.0")
	manifest.Artifacts = append(manifest.Artifacts, releasetrust.Artifact{Name: "build-provenance.json", Kind: "build-provenance", SHA256: string(releaseidentity.Hash(signed.Material.Provenance))})
	signed.Material.Manifest = JSON(t, manifest)
	signed.Material.ManifestSignature = sign(signed.Material.Manifest, commit, "v1.0.0")
	f.Metadata.Releases[0].ManifestSHA256 = string(releaseidentity.Hash(signed.Material.Manifest))
	f.Metadata.Event.Type, f.Metadata.Event.SignedBy = "release", releasetrust.StableWorkflowSigner
	f.Metadata.SourceCommit, f.Metadata.ReleaseTag = commit, "v1.0.0"
	material := f.Material(t)
	material.StablePolicy = JSON(t, policy)
	material.StablePolicySignature = Sign(t, f.RecoverySigner, material.StablePolicy)
	material.StableTrustedRoot = root
	material.MetadataSignature = sign(material.Metadata, commit, "v1.0.0")
	f.Catalog.StableMetadataSHA256 = releaseidentity.Hash(material.Metadata)
	f.Catalog.StablePolicySHA256 = releaseidentity.Hash(material.StablePolicy)
	material.Catalog = JSON(t, f.Catalog)
	material.CatalogSignature = sign(material.Catalog, commit, "v1.0.0")
	return f, material, signed.Material, sign
}
