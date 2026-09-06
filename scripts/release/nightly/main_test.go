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
