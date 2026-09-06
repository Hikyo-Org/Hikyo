package selfupdate

import (
	"encoding/base64"
	"encoding/json"
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
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

func TestSignedNightlyStagesVerifiedInventoryWithoutReplacingServer(t *testing.T) {
	for _, mutation := range []string{"none", "extra", "signature", "wrong workflow commit", "missing", "rollback", "trust rollback", "bridge signature", "bridge missing", "current policy revocation", "current policy missing"} {
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
			identity := releaseidentity.Identity{Profile: declaration.Profile, Version: declaration.Version, Sequence: declaration.Sequence, Commit: declaration.Commit, CompatibilitySHA256: releaseidentity.Hash(claim), ManifestSHA256: releaseidentity.Hash(material.Manifest)}
			engine := declaration.Engines[0]
			bridge := trust.AddBridge(t, releasetrust.BridgeStatement{Schema: "hikyo.dev/legacy-nightly-bridge/v1", SourceGenesis: releaseidentity.LegacyGenesisV1, Target: identity, TargetPolicySHA256: releaseidentity.Hash(material.Policy), SourceMigrations: engine.Migrations, TargetMigrations: engine.Migrations, SourceSchemaSHA256: engine.SchemaSHA256, TargetSchemaSHA256: engine.SchemaSHA256, Mode: "maintenance"})
			bridgePath := "bridges/" + string(releaseidentity.Hash(bridge.Statement)) + "/"
			responses[trustURL(bridgePath+"statement.json")] = bridge.Statement
			responses[trustURL(bridgePath+"statement.sigstore.json")] = bridge.Signature
			if mutation == "bridge signature" {
				responses[trustURL(bridgePath+"statement.sigstore.json")] = []byte("{}")
			}
			if mutation == "bridge missing" {
				responses[trustURL(bridgePath+"statement.json")] = nil
			}
			installer.config.TrustRootBase64 = base64.StdEncoding.EncodeToString(trust.Pinned.Root)
			installer.config.RecoveryKeyBase64 = base64.StdEncoding.EncodeToString(trust.Pinned.RecoveryPublicKey)
			if mutation == "current policy revocation" {
				// The release bundles a clean, still-authorized policy; only the
				// currently published policy revokes its manifest.
				var current releasetrust.NightlyPolicy
				if err := json.Unmarshal(material.Policy, &current); err != nil {
					t.Fatal(err)
				}
				current.RevokedManifests = append(current.RevokedManifests, releaseidentity.Hash(material.Manifest))
				trust.NightlyPolicy = testfixture.JSON(t, current)
				trust.Catalog.NightlyPolicies = append(trust.Catalog.NightlyPolicies, releaseidentity.Hash(trust.NightlyPolicy))
			}
			snapshot := trust.Material(t)
			for name, raw := range map[string][]byte{"metadata.json": snapshot.Metadata, "metadata.sigstore.json": snapshot.MetadataSignature, "catalog.json": snapshot.Catalog, "catalog.sigstore.json": snapshot.CatalogSignature, "nightly/policy.json": snapshot.NightlyPolicy, "primary.pub": trust.PrimaryPublic} {
				responses[trustURL(name)] = raw
			}
			if mutation == "current policy missing" {
				responses[trustURL("nightly/policy.json")] = nil
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
				bundle, err := upgradebundle.Load(t.Context(), staged.BundleDirectory, trust.Pinned, releaseidentity.SnapshotFloor{})
				if err != nil || !bundle.Valid() {
					t.Fatalf("runtime bundle was not assembled: %v", err)
				}
				if len(bundle.Snapshot().BridgeDigests()) != 1 {
					t.Fatal("runtime bundle omitted legacy bridge")
				}
				// Same-release download is idempotent and reauthenticates disk bytes.
				if err := installer.Apply(t.Context(), status); !errors.As(err, &staged) {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(staged.BundleDirectory, "catalog.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := installer.Apply(t.Context(), status); err == nil || errors.As(err, &staged) {
					t.Fatal("modified cached runtime bundle accepted")
				}
			} else if err == nil || errors.As(err, &staged) {
				t.Fatalf("unsafe %s accepted: %v", mutation, err)
			} else if mutation == "current policy revocation" && !strings.Contains(err.Error(), "revoked") {
				t.Fatalf("revocation refused for another reason: %v", err)
			}
			raw, err := os.ReadFile(target)
			if err != nil || string(raw) != "old hikyo binary\n" {
				t.Fatal("working server binary changed", err)
			}
			if paths, _ := filepath.Glob(filepath.Join(installer.config.StateDir, ".nightly-download-*")); len(paths) != 0 {
				t.Fatal(fmt.Sprint("private staging leaked", paths))
			}
			if paths, _ := filepath.Glob(filepath.Join(installer.config.StateDir, ".nightly-bundle-inputs-*")); len(paths) != 0 {
				t.Fatal("bundle inputs leaked", paths)
			}
			if strings.HasPrefix(mutation, "bridge ") {
				if _, err := os.Stat(filepath.Join(installer.config.StateDir, "nightly-trust.json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed bundle assembly advanced trust floor", err)
				}
			}
		})
	}
}
