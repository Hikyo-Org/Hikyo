package selfupdate

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

func TestSignedNightlyStagesVerifiedInventoryWithoutReplacingServer(t *testing.T) {
	for _, mutation := range []string{"none", "extra", "signature", "wrong workflow commit", "missing", "rollback", "trust rollback"} {
		t.Run(mutation, func(t *testing.T) {
			const version = "1.1.0-nightly.1"
			installer, status, target, responses := installerFixtureForVersion(t, version, true, "")
			status.Immutable = true
			installer.config.StateDir = t.TempDir()
			_, declaration, err := buildcompat.Development()
			if err != nil {
				t.Fatal(err)
			}
			declaration.Profile, declaration.Version, declaration.Sequence, declaration.Commit = releaseidentity.NightlyV1, version, 2, strings.Repeat("a", 40)
			claim := testfixture.JSON(t, declaration)
			base := "https://github.com/Hikyo-Org/Hikyo/releases/download/v" + version + "/"
			archiveName := mustArchiveName(t, version)
			payloads := map[string][]byte{archiveName: responses[base+archiveName], "checksums.txt": responses[base+"checksums.txt"], "binary-provenance.json": []byte("{}"), "upgrade-compatibility.json": claim}
			artifacts := []releasetrust.Artifact{{Name: archiveName, Kind: "binary", Platform: runtime.GOOS + "/" + runtime.GOARCH}, {Name: "checksums.txt", Kind: "checksum"}, {Name: "binary-provenance.json", Kind: "binary-provenance"}, {Name: "upgrade-compatibility.json", Kind: "upgrade-compatibility"}}
			trust, material, _ := testfixture.NightlyWithPayloads(t, claim, mutation == "wrong workflow commit", payloads, artifacts)
			installer.config.TrustRootBase64 = base64.StdEncoding.EncodeToString(trust.Pinned.Root)
			installer.config.RecoveryKeyBase64 = base64.StdEncoding.EncodeToString(trust.Pinned.RecoveryPublicKey)
			snapshot := trust.Material(t)
			for name, raw := range map[string][]byte{"metadata.json": snapshot.Metadata, "metadata.sigstore.json": snapshot.MetadataSignature, "catalog.json": snapshot.Catalog, "catalog.sigstore.json": snapshot.CatalogSignature, "primary.pub": trust.PrimaryPublic} {
				responses[trustURL(name)] = raw
			}
			status.Assets = nil
			add := func(name string, raw []byte) {
				responses[base+name] = raw
				status.Assets = append(status.Assets, updatecheck.Asset{Name: name, URL: base + name, Size: int64(len(raw)), Digest: "sha256:" + string(releaseidentity.Hash(raw))})
			}
			for name, reader := range material.Artifacts {
				raw, err := io.ReadAll(reader)
				if err != nil {
					t.Fatal(err)
				}
				if mutation == "missing" && name == "binary-provenance.json" {
					continue
				}
				add(name, raw)
			}
			add("release-manifest.json", material.Manifest)
			if mutation == "signature" {
				material.Bundle = []byte("{}")
			}
			add("release-manifest.sigstore.json", material.Bundle)
			if mutation == "extra" {
				add("unbound.exe", []byte("extra"))
			}
			if mutation == "rollback" || mutation == "trust rollback" {
				verified, err := releasetrust.VerifySnapshot(trust.Pinned, snapshot, releaseidentity.SnapshotFloor{})
				if err != nil {
					t.Fatal(err)
				}
				known := nightlyVerificationState{Floor: verified.Floor(), Release: releaseidentity.Identity{Profile: releaseidentity.NightlyV1, Version: "1.1.0-nightly.3", Sequence: 3, Commit: strings.Repeat("b", 40), CompatibilitySHA256: releaseidentity.Hash(claim), ManifestSHA256: releaseidentity.Hash([]byte("later"))}}
				if mutation == "trust rollback" {
					known.Floor.CatalogSequence++
				}
				if err := os.WriteFile(filepath.Join(installer.config.StateDir, "nightly-trust.json"), testfixture.JSON(t, known), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err = installer.Apply(t.Context(), status)
			var staged *StagedNightly
			if mutation == "none" {
				if !errors.As(err, &staged) {
					t.Fatalf("wanted verified manual staging: %v", err)
				}
				if _, err := os.Stat(filepath.Join(staged.Directory, archiveName)); err != nil {
					t.Fatal(err)
				}
				// Same-release download is idempotent and reauthenticates disk bytes.
				if err := installer.Apply(t.Context(), status); !errors.As(err, &staged) {
					t.Fatal(err)
				}
			} else if err == nil || errors.As(err, &staged) {
				t.Fatalf("unsafe %s accepted: %v", mutation, err)
			}
			raw, err := os.ReadFile(target)
			if err != nil || string(raw) != "old hikyo binary\n" {
				t.Fatal("working server binary changed", err)
			}
			if paths, _ := filepath.Glob(filepath.Join(installer.config.StateDir, ".nightly-download-*")); len(paths) != 0 {
				t.Fatal(fmt.Sprint("private staging leaked", paths))
			}
		})
	}
}
