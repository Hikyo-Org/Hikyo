package main

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func fixtureReadJSON(t *testing.T, path string, dst any) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatal(err)
	}
	return raw
}
func fixtureWrite(t *testing.T, dir, name string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalUsesCurrentAuthorizationWithoutRollingBack(t *testing.T) {
	trust, directory, f, sign := fullDraftFixture(t)
	var metadata releasetrust.Metadata
	fixtureReadJSON(t, filepath.Join(directory, "metadata.json"), &metadata)
	var catalog releasetrust.Catalog
	fixtureReadJSON(t, filepath.Join(directory, "catalog.json"), &catalog)
	manifestRaw, err := os.ReadFile(filepath.Join(directory, "release-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	version, sequence := "1.1.0", int64(2)
	metadata.Sequence++
	metadata.HighestRelease, metadata.HighestReleaseSequence = &version, &sequence
	metadata.SourceCommit, metadata.ReleaseTag = strings.Repeat("b", 40), "v1.1.0"
	metadata.Releases = append(metadata.Releases, releasetrust.Release{Version: version, Sequence: sequence, ManifestSHA256: strings.Repeat("f", 64)})
	metadataRaw := testfixture.JSON(t, metadata)
	catalog.Sequence++
	catalog.StableMetadataSHA256 = releaseidentity.Hash(metadataRaw)
	writeCurrent := func() {
		catalogRaw := testfixture.JSON(t, catalog)
		fixtureWrite(t, trust, "metadata.json", metadataRaw)
		fixtureWrite(t, trust, "metadata.sigstore.json", sign(metadataRaw, metadata.SourceCommit, metadata.ReleaseTag))
		fixtureWrite(t, trust, "catalog.json", catalogRaw)
		fixtureWrite(t, trust, "catalog.sigstore.json", sign(catalogRaw, metadata.SourceCommit, metadata.ReleaseTag))
	}
	writeCurrent()
	state := filepath.Join(t.TempDir(), "release-trust.json")
	args := []string{"verify", "--trust", trust, "--directory", directory, "--version", "1.0.0", "--commit", strings.Repeat("a", 40), "--state", state}
	if err := run(append(args, "--historical"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var floor releasetrust.SnapshotFloor
	saved := fixtureReadJSON(t, state, &floor)
	if floor.MetadataSequence != metadata.Sequence || floor.HighestReleaseSequence != 2 || floor.CatalogSequence != catalog.Sequence {
		t.Fatal("historical verification lowered current floor")
	}
	if err := run(append(args, "--latest"), &bytes.Buffer{}); err == nil {
		t.Fatal("old release presented as latest")
	}
	after, err := os.ReadFile(state)
	if err != nil || !bytes.Equal(saved, after) {
		t.Fatal("failed latest check changed floor")
	}
	var policy releasetrust.StablePolicy
	fixtureReadJSON(t, filepath.Join(trust, "stable-policy.json"), &policy)
	policy.RevokedManifests = append(policy.RevokedManifests, releaseidentity.Hash(manifestRaw))
	policyRaw := testfixture.JSON(t, policy)
	fixtureWrite(t, trust, "stable-policy.json", policyRaw)
	fixtureWrite(t, trust, "stable-policy.sigstore.json", testfixture.Sign(t, f.RecoverySigner, policyRaw))
	catalog.Sequence++
	catalog.StablePolicySHA256 = releaseidentity.Hash(policyRaw)
	writeCurrent()
	err = run(append(args, "--historical"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "signer is not authorized") {
		t.Fatalf("current revocation did not reject historical release: %v", err)
	}
}

func TestHistoricalLegacyBundleNeedsNoWorkflowArtifacts(t *testing.T) {
	trust, directory, f, _ := fullDraftFixture(t)
	var manifest releasetrust.Manifest
	fixtureReadJSON(t, filepath.Join(directory, "release-manifest.json"), &manifest)
	var candidate releasetrust.Candidate
	fixtureReadJSON(t, filepath.Join(directory, "release-candidate.json"), &candidate)
	candidate.KeyID, candidate.PublicKey = "test-primary", "primary.pub"
	candidateRaw := testfixture.JSON(t, candidate)
	fixtureWrite(t, directory, "release-candidate.json", candidateRaw)
	manifest.SigningKeyID = candidate.KeyID
	kept := make([]releasetrust.Artifact, 0, len(manifest.Artifacts))
	for _, a := range manifest.Artifacts {
		switch a.Kind {
		case "release-verifier", "build-provenance", "stable-policy", "stable-policy-signature", "sigstore-trusted-root":
			if err := os.Remove(filepath.Join(directory, a.Name)); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(directory, a.Name+".sigstore.json")); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		default:
			if a.Name == "release-candidate.json" {
				a.SHA256 = string(releaseidentity.Hash(candidateRaw))
			}
			kept = append(kept, a)
			raw, err := os.ReadFile(filepath.Join(directory, a.Name))
			if err != nil {
				t.Fatal(err)
			}
			fixtureWrite(t, directory, a.Name+".sigstore.json", testfixture.Sign(t, f.PrimarySigner, raw))
		}
	}
	manifest.Artifacts = kept
	manifestRaw := testfixture.JSON(t, manifest)
	fixtureWrite(t, directory, "release-manifest.json", manifestRaw)
	fixtureWrite(t, directory, "release-manifest.sigstore.json", testfixture.Sign(t, f.PrimarySigner, manifestRaw))
	for _, name := range []string{"metadata.json", "metadata.sigstore.json", "catalog.json", "catalog.sigstore.json"} {
		if err := os.Remove(filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"stable-policy.json", "stable-policy.sigstore.json", "stable-trusted-root.json"} {
		if err := os.Remove(filepath.Join(trust, name)); err != nil {
			t.Fatal(err)
		}
	}
	version, seq := "1.0.0", int64(1)
	f.Metadata.Sequence = 2
	f.Metadata.HighestRelease = &version
	f.Metadata.HighestReleaseSequence = &seq
	f.Metadata.Releases = []releasetrust.Release{{Version: version, Sequence: seq, ManifestSHA256: string(releaseidentity.Hash(manifestRaw))}}
	f.Catalog.Sequence = 2
	writeCurrent := func() {
		m := f.Material(t)
		for name, raw := range map[string][]byte{"metadata.json": m.Metadata, "metadata.sigstore.json": m.MetadataSignature, "catalog.json": m.Catalog, "catalog.sigstore.json": m.CatalogSignature} {
			fixtureWrite(t, trust, name, raw)
		}
	}
	writeCurrent()
	args := []string{"verify", "--trust", trust, "--directory", directory, "--version", version, "--commit", manifest.SourceCommit, "--historical"}
	if err := run(args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	f.Metadata.PrimaryKeys[0].Revoked = true
	f.Metadata.Sequence++
	f.Catalog.Sequence++
	writeCurrent()
	if err := run(args, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("historical primary revocation ignored: %v", err)
	}
}
