package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func writeNightlyTrustFixture(t *testing.T, fixture *testfixture.Fixture, material releasetrust.NightlyMaterial) string {
	t.Helper()
	directory := t.TempDir()
	snapshot := fixture.Material(t)
	for name, raw := range map[string][]byte{
		"root.json": fixture.Pinned.Root, "recovery.pub": fixture.Pinned.RecoveryPublicKey, "primary.pub": fixture.PrimaryPublic,
		"metadata.json": snapshot.Metadata, "metadata.sigstore.json": snapshot.MetadataSignature,
		"catalog.json": snapshot.Catalog, "catalog.sigstore.json": snapshot.CatalogSignature,
		"nightly/policy.json": material.Policy, "nightly/trusted-root.json": material.TrustedRoot,
	} {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func TestDevNightlyPreparationRequiresAuthorizedSuccessorPolicy(t *testing.T) {
	legacyFixture, legacyMaterial, _ := testfixture.Nightly(t, []byte("compatibility"), false)
	legacy := writeNightlyTrustFixture(t, legacyFixture, legacyMaterial)
	var output bytes.Buffer
	if err := run(t.Context(), []string{"preflight", "--trust", legacy}, &output); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"preflight", "--trust", legacy, "--require-main-policy"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"preflight", "--trust", legacy, "--require-dev-policy"}, &output); err == nil {
		t.Fatal("legacy recovery authorization accepted for dev nightlies")
	}
	fixture, material, _ := testfixture.DevNightly(t, []byte("compatibility"), testfixture.NightlyOptions{})
	trust := writeNightlyTrustFixture(t, fixture, material)
	if err := run(t.Context(), []string{"preflight", "--trust", trust, "--require-dev-policy"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"preflight", "--trust", trust, "--require-main-policy"}, &output); err == nil {
		t.Fatal("dev policy was accepted for a legacy main reader bridge")
	}
	if err := run(t.Context(), []string{"preflight", "--trust", trust, "--require-main-policy", "--require-dev-policy"}, &output); err == nil {
		t.Fatal("conflicting nightly source-policy requirements were accepted")
	}
	raw, err := io.ReadAll(material.Artifacts[releasetrust.NightlySourceValidationArtifact])
	if err != nil {
		t.Fatal(err)
	}
	proof := filepath.Join(t.TempDir(), "source-proof.json")
	if err := os.WriteFile(proof, raw, 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "bound-proof.json")
	args := []string{"source-validation", "--trust", trust, "--source-proof", proof, "--source-commit", strings.Repeat("a", 40), "--publisher-commit", strings.Repeat("c", 40), "--out", target}
	if err := run(t.Context(), args, &output); err != nil {
		t.Fatal(err)
	}
	boundRaw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var bound releasetrust.NightlySourceValidation
	if err := json.Unmarshal(boundRaw, &bound); err != nil {
		t.Fatal(err)
	}
	if bound.ProofSHA256 != releaseidentity.Hash(raw) || bound.Commit != strings.Repeat("a", 40) || bound.AuthorityCommit != strings.Repeat("c", 40) {
		t.Fatal("normalized proof did not retain the exact source, publisher and receipt bytes")
	}
	if err := run(t.Context(), args, &output); err == nil {
		t.Fatal("prior normalized proof was overwritten")
	}
	for _, mutation := range []string{"source", "publisher", "proof", "legacy policy"} {
		t.Run(mutation, func(t *testing.T) {
			bad := append([]string(nil), args...)
			bad[len(bad)-1] = filepath.Join(t.TempDir(), "out.json")
			switch mutation {
			case "source":
				bad[6] = strings.Repeat("d", 40)
			case "publisher":
				bad[8] = strings.Repeat("d", 40)
			case "proof":
				path := filepath.Join(t.TempDir(), "malformed.json")
				if err := os.WriteFile(path, append([]byte(`{"unreviewed":true,`), raw[1:]...), 0600); err != nil {
					t.Fatal(err)
				}
				bad[4] = path
			case "legacy policy":
				bad[2] = legacy
			}
			if err := run(t.Context(), bad, &output); err == nil {
				t.Fatal("invalid proof binding was accepted")
			}
			if _, err := os.Stat(bad[len(bad)-1]); !os.IsNotExist(err) {
				t.Fatal("refused proof left a success output")
			}
		})
	}
}
