package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestNightlyPreparationRequiresRecoveryAuthorization(t *testing.T) {
	_, declaration, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	declaration.Profile, declaration.Version, declaration.Sequence, declaration.Commit = releaseidentity.NightlyV1, "1.1.0-nightly.1", 2, strings.Repeat("a", 40)
	for i, engine := range declaration.Engines {
		declaration.Engines[i].Sources = append(engine.Sources, upgradecompat.SourceEdge{Source: releaseidentity.Source{Genesis: releaseidentity.LegacyGenesisV1}, Migrations: engine.Migrations, SchemaSHA256: engine.SchemaSHA256, Mode: upgradecompat.Maintenance})
	}
	f, material, _ := testfixture.Nightly(t, testfixture.JSON(t, declaration), false)
	trust, download := t.TempDir(), t.TempDir()
	put := func(path string, raw []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, reader := range material.Artifacts {
		raw, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		put(filepath.Join(download, name), raw)
	}
	put(filepath.Join(download, "release-manifest.json"), material.Manifest)
	put(filepath.Join(download, "release-manifest.sigstore.json"), material.Bundle)
	snapshot := f.Material(t)
	for name, raw := range map[string][]byte{"root.json": f.Pinned.Root, "recovery.pub": f.Pinned.RecoveryPublicKey, "primary.pub": f.PrimaryPublic,
		"metadata.json": snapshot.Metadata, "metadata.sigstore.json": snapshot.MetadataSignature, "catalog.json": snapshot.Catalog, "catalog.sigstore.json": snapshot.CatalogSignature,
		"nightly/policy.json": material.Policy, "nightly/trusted-root.json": material.TrustedRoot} {
		put(filepath.Join(trust, name), raw)
	}
	var output bytes.Buffer
	if err := run(t.Context(), []string{"preflight", "--trust", trust}, &output); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"verify", "--trust", trust, "--directory", download}, &output); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"root", "signature", "policy", "trusted root", "unsigned snapshot"} {
		t.Run(change, func(t *testing.T) {
			path, replacement := filepath.Join(trust, "root.json"), []byte("{}")
			switch change {
			case "signature":
				path = filepath.Join(trust, "catalog.sigstore.json")
			case "policy":
				path = filepath.Join(trust, "nightly/policy.json")
				replacement = append(bytes.Clone(material.Policy), ' ')
			case "trusted root":
				path = filepath.Join(trust, "nightly/trusted-root.json")
				replacement = append(bytes.Clone(material.TrustedRoot), ' ')
			case "unsigned snapshot":
				path = filepath.Join(trust, "catalog.json")
				replacement = append(bytes.Clone(snapshot.Catalog), ' ')
			}
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer put(path, original)
			put(path, replacement)
			if err := run(t.Context(), []string{"preflight", "--trust", trust}, &output); err == nil {
				t.Fatal("unauthenticated bootstrap accepted")
			}
		})
	}
	sources := filepath.Join(t.TempDir(), "sources.json")
	engines := map[releaseidentity.Engine][]upgradecompat.SourceEdge{}
	for _, engine := range declaration.Engines {
		engines[engine.Migrations.Engine] = engine.Sources
	}
	put(sources, testfixture.JSON(t, map[string]any{"schema": "hikyo.dev/upgrade-sources/v1", "engines": engines}))
	target := filepath.Join(t.TempDir(), "out.json")
	if err := run(t.Context(), []string{"sources", "--trust", trust, "--directory", download, "--sources", sources, "--out", target}, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Engines map[releaseidentity.Engine][]upgradecompat.SourceEdge `json:"engines"`
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	for engine, edges := range result.Engines {
		last := edges[len(edges)-1]
		if len(edges) != len(engines[engine])+1 || last.Source.Release.ManifestSHA256 != releaseidentity.Hash(material.Manifest) || last.Mode != upgradecompat.Maintenance {
			t.Fatal("source edge did not bind authenticated previous release")
		}
	}
	// A second, older predecessor adds its own exact edge; repeating one is refused.
	older := declaration
	older.Version, older.Sequence = "1.1.0-nightly.0", 1
	olderMaterial := f.SignNightly(testfixture.JSON(t, older), older.Version, older.Sequence)
	olderDownload := t.TempDir()
	for name, reader := range olderMaterial.Artifacts {
		raw, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		put(filepath.Join(olderDownload, name), raw)
	}
	put(filepath.Join(olderDownload, "release-manifest.json"), olderMaterial.Manifest)
	put(filepath.Join(olderDownload, "release-manifest.sigstore.json"), olderMaterial.Bundle)
	for name, raw := range map[string][]byte{"nightly-policy.json": olderMaterial.Policy, "sigstore-trusted-root.json": olderMaterial.TrustedRoot} {
		put(filepath.Join(olderDownload, name), raw)
	}
	multi := filepath.Join(t.TempDir(), "multi.json")
	if err := run(t.Context(), []string{"sources", "--trust", trust, "--directory", download, "--directory", olderDownload, "--sources", sources, "--out", multi}, &output); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(multi)
	if err != nil {
		t.Fatal(err)
	}
	result.Engines = nil
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	for engine, edges := range result.Engines {
		if len(edges) != len(engines[engine])+2 || edges[len(edges)-1].Source.Release.ManifestSHA256 != releaseidentity.Hash(olderMaterial.Manifest) || edges[len(edges)-2].Source.Release.ManifestSHA256 != releaseidentity.Hash(material.Manifest) {
			t.Fatalf("%s: predecessor edges missing or misordered", engine)
		}
	}
	if err := run(t.Context(), []string{"sources", "--trust", trust, "--directory", download, "--directory", download, "--sources", sources, "--out", filepath.Join(t.TempDir(), "dup.json")}, &output); err == nil || !strings.Contains(err.Error(), "duplicate predecessor") {
		t.Fatalf("duplicate predecessor accepted: %v", err)
	}
	if err := run(t.Context(), []string{"verify", "--trust", trust, "--directory", download, "--directory", olderDownload}, &output); err == nil {
		t.Fatal("verify accepted several directories")
	}
	bridgeDir := filepath.Join(t.TempDir(), "bridges")
	if err := run(t.Context(), []string{"legacy-bridges", "--trust", trust, "--directory", download, "--out", bridgeDir}, &output); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(bridgeDir)
	if err != nil || len(entries) != 2 {
		t.Fatal("expected both-engine bridge proposals", err)
	}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(bridgeDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name() != string(releaseidentity.Hash(raw))+".json" {
			t.Fatal("bridge name differs from exact bytes")
		}
		f.Catalog.Bridges = append(f.Catalog.Bridges, releaseidentity.Hash(raw))
		verified, err := releasetrust.VerifyBridge(f.Snapshot(t), releasetrust.BridgeMaterial{Statement: raw, Signature: testfixture.Sign(t, f.RecoverySigner, raw)})
		if err != nil || verified.Statement().SourceGenesis != releaseidentity.LegacyGenesisV1 {
			t.Fatal("bridge proposal cannot be recovery-authorized", err)
		}
	}
}

func TestNightlyPreflightWithWorkflowSignedStableSnapshot(t *testing.T) {
	f, nightly, _ := testfixture.Nightly(t, []byte("compatibility"), false)
	commit := strings.Repeat("a", 40)
	f.AddStable(t, "1.0.0", 1, commit, []byte("compatibility"))
	f.Metadata.Sequence, f.Catalog.Sequence = 2, 2
	f.Metadata.Event.Type, f.Metadata.Event.SignedBy = "release", releasetrust.StableWorkflowSigner
	f.Metadata.SourceCommit, f.Metadata.ReleaseTag = commit, "v1.0.0"
	policy, trustedRoot, sign := testfixture.WorkflowSigner(t)
	policy.PrimaryKeysSHA256 = releasetrust.PrimaryKeysDigest(f.Metadata.PrimaryKeys)
	policy.NightlyPolicies, policy.Bridges = f.Catalog.NightlyPolicies, f.Catalog.Bridges
	material := f.Material(t)
	material.StablePolicy = testfixture.JSON(t, policy)
	material.StablePolicySignature = testfixture.Sign(t, f.RecoverySigner, material.StablePolicy)
	material.StableTrustedRoot = trustedRoot
	material.MetadataSignature = sign(material.Metadata, commit, "v1.0.0")
	f.Catalog.StableMetadataSHA256 = releaseidentity.Hash(material.Metadata)
	f.Catalog.StablePolicySHA256 = releaseidentity.Hash(material.StablePolicy)
	material.Catalog = testfixture.JSON(t, f.Catalog)
	material.CatalogSignature = sign(material.Catalog, commit, "v1.0.0")
	if _, err := releasetrust.VerifySnapshot(f.Pinned, material, releaseidentity.SnapshotFloor{}); err != nil {
		t.Fatalf("fixture is not an authenticated workflow snapshot: %v", err)
	}

	trust := t.TempDir()
	put := func(name string, raw []byte) {
		t.Helper()
		path := filepath.Join(trust, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	documents := map[string][]byte{
		"root.json": f.Pinned.Root, "recovery.pub": f.Pinned.RecoveryPublicKey, "primary.pub": f.PrimaryPublic,
		"metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature,
		"catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature,
		"stable-policy.json": material.StablePolicy, "stable-policy.sigstore.json": material.StablePolicySignature,
		"stable-trusted-root.json": material.StableTrustedRoot,
		"nightly/policy.json":      nightly.Policy, "nightly/trusted-root.json": nightly.TrustedRoot,
	}
	for name, raw := range documents {
		put(name, raw)
	}
	var output bytes.Buffer
	if err := run(t.Context(), []string{"preflight", "--trust", trust}, &output); err != nil {
		t.Fatalf("nightly preflight rejected authenticated stable workflow metadata: %v", err)
	}
	if got, want := output.String(), "Authenticated nightly policy "+string(releaseidentity.Hash(nightly.Policy))+"\n"; got != want {
		t.Fatalf("nightly policy identity differs: got %q, want %q", got, want)
	}
	for _, name := range []string{"stable-policy.json", "stable-policy.sigstore.json", "stable-trusted-root.json"} {
		for _, change := range []string{"missing", "invalid", "directory"} {
			t.Run(name+"/"+change, func(t *testing.T) {
				path := filepath.Join(trust, name)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
					put(name, documents[name])
				})
				switch change {
				case "invalid":
					put(name, []byte("{}"))
				case "directory":
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				var output bytes.Buffer
				if err := run(t.Context(), []string{"preflight", "--trust", trust}, &output); err == nil || output.Len() != 0 {
					t.Fatalf("unauthenticated delegation accepted: err=%v, output=%q", err, output.String())
				}
			})
		}
	}
}
