package selfupdate

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

func preparedStableFixture(t *testing.T, newerPublished ...bool) (*Installer, updatecheck.Status, string, *testfixture.Fixture, releaseidentity.Identity, map[string][]byte) {
	t.Helper()
	installer, status, installed, responses := installerFixtureForVersion(t, "1.0.0", false, "")
	status.Channel, status.Immutable = updatecheck.ChannelStable, true
	_, declaration, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	declaration.Profile, declaration.Version, declaration.Sequence, declaration.Commit = releaseidentity.StableV1, "1.0.0", 1, strings.Repeat("a", 40)
	f, material, stable, sign := testfixture.StableWorkflowWithCompatibility(t, testfixture.JSON(t, declaration))
	base := "https://github.com/Hikyo-Org/Hikyo/releases/download/v1.0.0/"
	name := mustArchiveName(t, "1.0.0")
	archive := responses[base+name]
	var manifest releasetrust.Manifest
	if err := definitions.DecodeStrict(stable.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	artifacts := []releasetrust.Artifact{}
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind != "binary" && artifact.Kind != "build-provenance" {
			artifacts = append(artifacts, artifact)
		}
	}
	// Stable publication binds platform in the canonical signed archive name;
	// unlike nightlies it does not require an explicit platform property.
	manifest.Artifacts = append(artifacts, releasetrust.Artifact{Name: name, Kind: "binary", SHA256: string(releaseidentity.Hash(archive))})
	var policy releasetrust.StablePolicy
	if err := definitions.DecodeStrict(material.StablePolicy, &policy); err != nil {
		t.Fatal(err)
	}
	stable.Provenance = testfixture.StableProvenance(t, policy, manifest)
	stable.ProvenanceSignature = sign(stable.Provenance, declaration.Commit, "v1.0.0")
	manifest.Artifacts = append(manifest.Artifacts, releasetrust.Artifact{Name: "build-provenance.json", Kind: "build-provenance", SHA256: string(releaseidentity.Hash(stable.Provenance))})
	stable.Manifest = testfixture.JSON(t, manifest)
	stable.ManifestSignature = sign(stable.Manifest, declaration.Commit, "v1.0.0")
	f.Metadata.Releases[0].ManifestSHA256 = string(releaseidentity.Hash(stable.Manifest))
	if len(newerPublished) > 0 && newerPublished[0] {
		newerVersion, newerSequence := "1.1.0", int64(2)
		f.Metadata.HighestRelease, f.Metadata.HighestReleaseSequence = &newerVersion, &newerSequence
		f.Metadata.ReleaseTag = "v" + newerVersion
		f.Metadata.Releases = append(f.Metadata.Releases, releasetrust.Release{Version: newerVersion, Sequence: newerSequence, ManifestSHA256: string(releaseidentity.Hash([]byte("newer signed release")))})
	}
	material.Metadata = testfixture.JSON(t, f.Metadata)
	material.MetadataSignature = sign(material.Metadata, declaration.Commit, f.Metadata.ReleaseTag)
	f.Catalog.StableMetadataSHA256 = releaseidentity.Hash(material.Metadata)
	material.Catalog = testfixture.JSON(t, f.Catalog)
	material.CatalogSignature = sign(material.Catalog, declaration.Commit, f.Metadata.ReleaseTag)
	installer.config = Config{StateDir: t.TempDir(), TrustRootBase64: base64.StdEncoding.EncodeToString(f.Pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(f.Pinned.RecoveryPublicKey)}
	for name, raw := range map[string][]byte{"metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature, "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature, "stable-policy.json": material.StablePolicy, "stable-policy.sigstore.json": material.StablePolicySignature, "stable-trusted-root.json": material.StableTrustedRoot, "primary.pub": f.PrimaryPublic} {
		responses[trustURL(name)] = raw
	}
	status.Assets = nil
	for asset, raw := range map[string][]byte{"release-manifest.json": stable.Manifest, "release-manifest.sigstore.json": stable.ManifestSignature, "release-candidate.json": stable.Candidate, "upgrade-compatibility.json": stable.Compatibility, "build-provenance.json": stable.Provenance, "build-provenance.json.sigstore.json": stable.ProvenanceSignature, name: archive, name + ".sigstore.json": sign(archive, declaration.Commit, "v1.0.0")} {
		addAssetResponse(&status, responses, base, asset, raw)
	}
	identity := releaseidentity.Identity{Profile: declaration.Profile, Version: declaration.Version, Sequence: declaration.Sequence, Commit: declaration.Commit, CompatibilitySHA256: releaseidentity.Hash(stable.Compatibility), ManifestSHA256: releaseidentity.Hash(stable.Manifest)}
	return installer, status, installed, f, identity, responses
}

func TestPrepareStableAuthenticatesAndExtractsWithoutInstalling(t *testing.T) {
	i, status, installed, trust, expected, _ := preparedStableFixture(t)
	before, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := i.PrepareRelease(t.Context(), status)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Identity != expected {
		t.Fatal("wrong identity")
	}
	binary, err := os.ReadFile(prepared.BinaryPath)
	if err != nil || releaseidentity.Hash(binary) != prepared.BinarySHA256 {
		t.Fatal("wrong executable", err)
	}
	after, err := os.ReadFile(installed)
	if err != nil || string(after) != string(before) {
		t.Fatal("installed executable changed", err)
	}
	bundle, err := upgradebundle.Load(t.Context(), prepared.BundleDirectory, trust.Pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Release(expected); err != nil {
		t.Fatal(err)
	}
	if _, err := i.AssembleReleaseRoute(t.Context(), prepared, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareStableKeepsSelectedTargetWhenNewerStableIsPublished(t *testing.T) {
	i, status, _, _, expected, _ := preparedStableFixture(t, true)
	prepared, err := i.PrepareRelease(t.Context(), status)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Identity != expected {
		t.Fatal("changed selected target to latest")
	}
}

func TestPrepareStableHistoricalSourcePreservesHighestObservation(t *testing.T) {
	i, status, _, _, expected, _ := preparedStableFixture(t)
	if _, err := i.PrepareRelease(t.Context(), status); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(i.config.StateDir, "stable-prepared-trust.json")
	state, err := readPreparedStableState(path)
	if err != nil {
		t.Fatal(err)
	}
	highest := expected
	highest.Version, highest.Sequence, highest.ManifestSHA256 = "1.1.0", 2, releaseidentity.Hash([]byte("newer observation"))
	state.Release = highest
	if err := saveNightlyState(path, state); err != nil {
		t.Fatal(err)
	}
	if _, err := i.PrepareRelease(t.Context(), status); err == nil {
		t.Fatal("accepted lower candidate without exact historical authority")
	}
	if _, err := i.PrepareReleaseSource(t.Context(), status, expected); err != nil {
		t.Fatal(err)
	}
	after, err := readPreparedStableState(path)
	if err != nil || after.Release != highest {
		t.Fatal("lowered highest observed release", err)
	}
}

func TestGenericPreparedReleaseRetainsNightlyVerification(t *testing.T) {
	i, status, _, _, expected, _ := preparedNightlyFixture(t, nil)
	prepared, err := i.PrepareRelease(t.Context(), status)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Identity != expected {
		t.Fatal("changed nightly identity")
	}
	if _, err := i.AssembleReleaseRoute(t.Context(), prepared, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedStableRefusesRollbackOfOtherChannelTrust(t *testing.T) {
	i, status, _, _, _, _ := preparedStableFixture(t)
	if _, err := i.PrepareRelease(t.Context(), status); err != nil {
		t.Fatal(err)
	}
	state, err := readPreparedStableState(filepath.Join(i.config.StateDir, "stable-prepared-trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	state.Release.Profile, state.Release.Version = releaseidentity.NightlyV1, "1.0.0-nightly.1"
	state.Floor.MetadataSequence++
	state.Floor.MetadataSHA256 = releaseidentity.Hash([]byte("newer nightly trust observation"))
	if err := saveNightlyState(filepath.Join(i.config.StateDir, "nightly-trust.json"), state); err != nil {
		t.Fatal(err)
	}
	if _, err := i.PrepareRelease(t.Context(), status); err == nil {
		t.Fatal("ignored other channel trust floor")
	}
}

func TestPrepareStableRefusesUnauthenticatedInputs(t *testing.T) {
	for _, mutation := range []string{"mutable", "wrong profile", "bad manifest signature", "wrong artifact digest", "bad archive signature", "wrong exact identity", "wrong asset URL", "duplicate asset", "cached archive tamper", "corrupt trust floor"} {
		t.Run(mutation, func(t *testing.T) {
			i, status, _, _, expected, responses := preparedStableFixture(t)
			base := "https://github.com/Hikyo-Org/Hikyo/releases/download/v1.0.0/"
			name := mustArchiveName(t, "1.0.0")
			corruptSignature := func(name string) {
				raw := []byte("invalid signature envelope")
				responses[base+name] = raw
				for index := range status.Assets {
					if status.Assets[index].Name == name {
						status.Assets[index].Size = int64(len(raw))
						status.Assets[index].Digest = "sha256:" + string(releaseidentity.Hash(raw))
					}
				}
			}
			switch mutation {
			case "mutable":
				status.Immutable = false
			case "wrong profile":
				status.Channel = updatecheck.ChannelNightly
			case "bad manifest signature":
				corruptSignature("release-manifest.sigstore.json")
			case "wrong artifact digest":
				for index := range status.Assets {
					if status.Assets[index].Name == name {
						status.Assets[index].Digest = "sha256:" + strings.Repeat("a", 64)
					}
				}
			case "bad archive signature":
				corruptSignature(name + ".sigstore.json")
			case "wrong exact identity":
				expected.ManifestSHA256 = releaseidentity.Hash([]byte("another release"))
			case "wrong asset URL":
				status.Assets[0].URL = "https://untrusted.example/release"
			case "duplicate asset":
				status.Assets = append(status.Assets, status.Assets[0])
			case "cached archive tamper":
				prepared, err := i.PrepareRelease(t.Context(), status)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(prepared.Directory, name), []byte("tampered"), 0600); err != nil {
					t.Fatal(err)
				}
			case "corrupt trust floor":
				if err := os.WriteFile(filepath.Join(i.config.StateDir, "stable-prepared-trust.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := i.PrepareReleaseSource(t.Context(), status, expected); err == nil {
				t.Fatal("accepted invalid evidence")
			}
		})
	}
}
