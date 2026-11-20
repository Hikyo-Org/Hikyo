package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func captureCosign(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "cosign")
	args := filepath.Join(dir, "args")
	auth := filepath.Join(dir, "auth")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$@" >> "$COSIGN_ARGS"
while [ "$#" -gt 0 ]; do
 case "$1" in --key|--trusted-root) cp "$2" "$COSIGN_AUTH"; break ;; esac
 shift
done
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COSIGN_BIN", binary)
	t.Setenv("COSIGN_ARGS", args)
	t.Setenv("COSIGN_AUTH", auth)
	return args, auth
}

func TestPublishedOCIUsesExactKeylessIdentity(t *testing.T) {
	trust, dir := fullDraft(t)
	loaded, err := loadTrust(trust)
	if err != nil {
		t.Fatal(err)
	}
	material := loaded.material
	for name, target := range map[string]*[]byte{"metadata.json": &material.Metadata, "metadata.sigstore.json": &material.MetadataSignature, "catalog.json": &material.Catalog, "catalog.sigstore.json": &material.CatalogSignature} {
		*target, err = readDocument(dir, name)
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := releasetrust.VerifySnapshot(loaded.pinned, material, loaded.snapshot.Floor())
	if err != nil {
		t.Fatal(err)
	}
	var stable releasetrust.StableMaterial
	for name, target := range map[string]*[]byte{"release-manifest.json": &stable.Manifest, "release-manifest.sigstore.json": &stable.ManifestSignature, "release-candidate.json": &stable.Candidate, "upgrade-compatibility.json": &stable.Compatibility, "build-provenance.json": &stable.Provenance, "build-provenance.json.sigstore.json": &stable.ProvenanceSignature} {
		*target, err = readDocument(dir, name)
		if err != nil {
			t.Fatal(err)
		}
	}
	release, err := releasetrust.VerifyStable(snapshot, stable)
	if err != nil {
		t.Fatal(err)
	}
	argsFile, authFile := captureCosign(t)
	if err := verifyPublishedOCI(snapshot, release, material.StableTrustedRoot); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := snapshot.StablePolicy()
	for _, expected := range []string{"--trusted-root\n", "--certificate-identity\n" + policy.RepositoryURI + "/" + policy.WorkflowPath + "@refs/tags/v1.0.0\n", "--certificate-oidc-issuer\n" + policy.Issuer + "\n", "--certificate-github-workflow-sha\n" + strings.Repeat("a", 40) + "\n", "--certificate-github-workflow-ref\nrefs/tags/v1.0.0\n"} {
		if !strings.Contains(string(args), expected) {
			t.Fatalf("missing exact identity argument %q: %s", expected, args)
		}
	}
	if strings.Contains(string(args), "--insecure-ignore-tlog") || strings.Contains(string(args), "--key\n") {
		t.Fatal("keyless verification weakened to keyed mode")
	}
	auth, err := os.ReadFile(authFile)
	if err != nil || releaseidentity.Hash(auth) != policy.TrustedRootSHA256 {
		t.Fatal("cosign received substituted root")
	}
	for _, artifact := range release.Artifacts() {
		if artifact.Kind == "image" && !strings.Contains(string(args), artifact.Image+"@"+artifact.Digest+"\n") {
			t.Fatal("cosign did not verify immutable image digest")
		}
	}
	if err := verifyPublishedOCI(snapshot, release, []byte("substituted root")); err == nil {
		t.Fatal("accepted substituted Sigstore root")
	}
}

func TestPublishedLegacyOCIUsesAuthenticatedKeyWithoutDelegation(t *testing.T) {
	f := testfixture.New(t)
	signed := f.AddStable(t, "1.0.0", 1, strings.Repeat("a", 40), []byte("compatibility"))
	var manifest releasetrust.Manifest
	if err := json.Unmarshal(signed.Material.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("b", 64)
	manifest.Artifacts = append(manifest.Artifacts, releasetrust.Artifact{Name: "image-index.digest", Kind: "image", SHA256: string(releaseidentity.Hash([]byte(digest))), Image: "ghcr.io/synthetic/hikyo", Digest: digest, Tag: "1.0.0"})
	signed.Material.Manifest = testfixture.JSON(t, manifest)
	signed.Material.ManifestSignature = testfixture.Sign(t, f.PrimarySigner, signed.Material.Manifest)
	f.Metadata.Releases[0].ManifestSHA256 = string(releaseidentity.Hash(signed.Material.Manifest))
	snapshot := f.Snapshot(t)
	release, err := releasetrust.VerifyStable(snapshot, signed.Material)
	if err != nil {
		t.Fatal(err)
	}
	argsFile, authFile := captureCosign(t)
	if err := verifyPublishedOCI(snapshot, release, nil); err != nil {
		t.Fatalf("legacy OCI verification unexpectedly requires a stable delegate: %v", err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "verify\n--insecure-ignore-tlog\n--key\n") || !strings.Contains(string(args), "ghcr.io/synthetic/hikyo@"+digest+"\n") || strings.Contains(string(args), "--trusted-root") {
		t.Fatalf("wrong historical cosign mode: %s", args)
	}
	auth, err := os.ReadFile(authFile)
	if err != nil || string(auth) != string(f.PrimaryPublic) {
		t.Fatal("cosign did not receive authenticated historical primary")
	}
}
