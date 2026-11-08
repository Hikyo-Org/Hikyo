package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func preparedNightlyFixture(t *testing.T, archive []byte) (*Installer, updatecheck.Status, string, *testfixture.Fixture, releaseidentity.Identity, map[string][]byte) {
	t.Helper()
	const version = "1.1.0-nightly.1"
	installer, status, installed, responses := installerFixtureForVersion(t, version, true, "")
	status.Immutable = true
	installer.config.StateDir = t.TempDir()
	_, declaration, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	declaration.Profile, declaration.Version, declaration.Sequence, declaration.Commit = releaseidentity.NightlyV1, version, 2, strings.Repeat("a", 40)
	claim := testfixture.JSON(t, declaration)
	base := "https://github.com/Hikyo-Org/Hikyo/releases/download/v" + version + "/"
	name := mustArchiveName(t, version)
	if archive == nil {
		archive = responses[base+name]
	}
	payloads := map[string][]byte{name: archive, "checksums.txt": responses[base+"checksums.txt"], "binary-provenance.json": []byte("{}"), "upgrade-compatibility.json": claim}
	artifacts := []releasetrust.Artifact{{Name: name, Kind: "binary", Platform: runtime.GOOS + "/" + runtime.GOARCH}, {Name: "checksums.txt", Kind: "checksum"}, {Name: "binary-provenance.json", Kind: "binary-provenance"}, {Name: "upgrade-compatibility.json", Kind: "upgrade-compatibility"}}
	for _, platform := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"} {
		if platform == nightlyPlatform() {
			continue
		}
		parts := strings.Split(platform, "/")
		foreign, err := archiveName(version, parts[0], parts[1])
		if err != nil {
			t.Fatal(err)
		}
		payloads[foreign] = []byte("foreign platform payload")
		artifacts = append(artifacts, releasetrust.Artifact{Name: foreign, Kind: "binary", Platform: platform})
	}
	for _, arch := range []string{"amd64", "arm64"} {
		name := "hikyo_" + arch + ".deb"
		payloads[name] = []byte("unused package")
		artifacts = append(artifacts, releasetrust.Artifact{Name: name, Kind: "package", Platform: "linux/" + arch, Format: "deb", Arch: arch})
	}
	trust, material, _ := testfixture.NightlyWithPayloads(t, claim, false, payloads, artifacts)
	installer.config.TrustRootBase64 = base64.StdEncoding.EncodeToString(trust.Pinned.Root)
	installer.config.RecoveryKeyBase64 = base64.StdEncoding.EncodeToString(trust.Pinned.RecoveryPublicKey)
	snapshot := trust.Material(t)
	for name, raw := range map[string][]byte{"metadata.json": snapshot.Metadata, "metadata.sigstore.json": snapshot.MetadataSignature, "catalog.json": snapshot.Catalog, "catalog.sigstore.json": snapshot.CatalogSignature, "nightly/policy.json": snapshot.NightlyPolicy, "primary.pub": trust.PrimaryPublic} {
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
		add(name, raw)
	}
	add("release-manifest.json", material.Manifest)
	add("release-manifest.sigstore.json", material.Bundle)
	identity := releaseidentity.Identity{Profile: declaration.Profile, Version: declaration.Version, Sequence: declaration.Sequence, Commit: declaration.Commit, CompatibilitySHA256: releaseidentity.Hash(claim), ManifestSHA256: releaseidentity.Hash(material.Manifest)}
	return installer, status, installed, trust, identity, responses
}

func TestPrepareNightlyExtractsExactExecutableWithoutInstalling(t *testing.T) {
	installer, status, installed, trust, expected, _ := preparedNightlyFixture(t, nil)
	prepared, err := installer.PrepareNightly(t.Context(), status)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readNightlyFile(prepared.BinaryPath, maxBinaryBytes)
	if err != nil || len(raw) == 0 || prepared.Identity != expected || prepared.BinarySHA256 != releaseidentity.Hash(raw) {
		t.Fatalf("prepared executable identity differs: %+v %v", prepared, err)
	}
	original, err := os.ReadFile(installed)
	if err != nil || string(original) != "old hikyo binary\n" {
		t.Fatalf("installed binary changed: %v", err)
	}
	bundle, err := upgradebundle.Load(t.Context(), prepared.BundleDirectory, trust.Pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Release(expected); err != nil {
		t.Fatal(err)
	}
	again, err := installer.PrepareNightly(t.Context(), status)
	if err != nil || again != prepared {
		t.Fatalf("preparation was not idempotent: %v", err)
	}
	if err := os.WriteFile(prepared.BinaryPath, []byte("modified"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.PrepareNightly(t.Context(), status); err == nil {
		t.Fatal("modified cached executable accepted")
	}
}

func TestPreparedNightlyReusesCacheAndRechecksImmutableInventory(t *testing.T) {
	installer, status, _, _, _, _ := preparedNightlyFixture(t, nil)
	if _, err := installer.PrepareNightly(t.Context(), status); err != nil {
		t.Fatal(err)
	}
	transport := installer.client.Transport
	installer.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "/releases/download/") {
			t.Fatal("cached handoff downloaded release payloads again")
		}
		return transport.RoundTrip(request)
	})
	if _, err := installer.PrepareNightly(t.Context(), status); err != nil {
		t.Fatal(err)
	}
	status.Assets[0].Digest = "sha256:" + strings.Repeat("f", 64)
	if _, err := installer.PrepareNightly(t.Context(), status); err == nil {
		t.Fatal("changed immutable inventory accepted cached bytes")
	}
}

func TestPrepareExactOlderNightlyPreservesHighestAndChecksIdentity(t *testing.T) {
	installer, status, _, _, expected, _ := preparedNightlyFixture(t, nil)
	if _, err := installer.PrepareNightly(t.Context(), status); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(installer.config.StateDir, "nightly-trust.json")
	known, err := readNightlyState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	known.Release.Sequence++
	known.Release.Version = "1.1.0-nightly.2"
	known.Release.ManifestSHA256 = releaseidentity.Hash([]byte("later observed nightly"))
	if err := saveNightlyState(statePath, known); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.PrepareNightly(t.Context(), status); err == nil {
		t.Fatal("older target accepted")
	}
	status.Available = false // Historical discovery need not claim a new update.
	prepared, err := installer.PrepareNightlySource(t.Context(), status, expected)
	if err != nil || prepared.Identity != expected {
		t.Fatalf("exact older evidence rejected: %v", err)
	}
	after, err := readNightlyState(statePath)
	if err != nil || after != known {
		t.Fatalf("source evidence lowered durable trust: %+v %v", after, err)
	}
	wrong := expected
	wrong.ManifestSHA256 = releaseidentity.Hash([]byte("not the requested source"))
	if _, err := installer.PrepareNightlySource(t.Context(), status, wrong); err == nil {
		t.Fatal("wrong exact source accepted")
	}
}

func TestNightlyRouteReauthenticatesAllExactEvidence(t *testing.T) {
	for _, transient := range []bool{false, true} {
		t.Run(fmt.Sprintf("transient=%t", transient), func(t *testing.T) {
			installer, status, _, trust, _, _ := preparedNightlyFixture(t, nil)
			installer.config.TransientBundles = transient
			trustFetches := map[string]int{}
			transport := installer.client.Transport
			installer.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host == "raw.githubusercontent.com" {
					trustFetches[request.URL.Path]++
				}
				return transport.RoundTrip(request)
			})
			target, err := installer.PrepareNightly(t.Context(), status)
			if err != nil {
				t.Fatal(err)
			}
			_, declaration, err := buildcompat.Development()
			if err != nil {
				t.Fatal(err)
			}
			declaration.Profile, declaration.Version, declaration.Sequence, declaration.Commit = releaseidentity.NightlyV1, "1.0.0-nightly.1", 1, strings.Repeat("a", 40)
			claim := testfixture.JSON(t, declaration)
			material := trust.SignNightlyWithPayloads(claim, declaration.Version, declaration.Sequence,
				map[string][]byte{"upgrade-compatibility.json": claim, "source.tar.gz": []byte("source binary"), "binary-provenance.json": []byte("{}"), "checksums.txt": []byte("checksums")},
				[]releasetrust.Artifact{{Name: "upgrade-compatibility.json", Kind: "upgrade-compatibility"}, {Name: "source.tar.gz", Kind: "binary", Platform: nightlyPlatform()}, {Name: "binary-provenance.json", Kind: "binary-provenance"}, {Name: "checksums.txt", Kind: "checksum"}})
			source := PreparedNightly{Directory: t.TempDir(), Identity: releaseidentity.Identity{Profile: declaration.Profile, Version: declaration.Version, Sequence: declaration.Sequence, Commit: declaration.Commit, CompatibilitySHA256: releaseidentity.Hash(claim), ManifestSHA256: releaseidentity.Hash(material.Manifest)}}
			for name, reader := range material.Artifacts {
				raw, err := io.ReadAll(reader)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(source.Directory, name), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for name, raw := range map[string][]byte{"release-manifest.json": material.Manifest, "release-manifest.sigstore.json": material.Bundle} {
				if err := os.WriteFile(filepath.Join(source.Directory, name), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			directory, err := installer.AssembleNightlyRoute(t.Context(), target, []PreparedNightly{source})
			if err != nil {
				t.Fatal(err)
			}
			if len(trustFetches) == 0 {
				t.Fatal("route preparation fetched no trust metadata")
			}
			for path, count := range trustFetches {
				if count != 1 {
					t.Fatalf("trust metadata %s fetched %d times in one run", path, count)
				}
			}
			if _, err := os.Stat(target.BundleDirectory); transient != os.IsNotExist(err) {
				t.Fatalf("superseded bundle retention: transient=%t err=%v", transient, err)
			}
			bundle, err := upgradebundle.Load(t.Context(), directory, trust.Pinned, releaseidentity.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			for _, identity := range []releaseidentity.Identity{target.Identity, source.Identity} {
				if _, err := bundle.Release(identity); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := installer.AssembleNightlyRoute(t.Context(), target, []PreparedNightly{target}); err == nil {
				t.Fatal("duplicate route inventory accepted")
			}
			if _, err := installer.AssembleNightlyRoute(t.Context(), target, make([]PreparedNightly, upgradecompat.MaxReleases)); err == nil {
				t.Fatal("unbounded route accepted")
			}
			if err := os.WriteFile(filepath.Join(source.Directory, "checksums.txt"), []byte("tampered"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := installer.AssembleNightlyRoute(t.Context(), target, []PreparedNightly{source}); err == nil {
				t.Fatal("modified source accepted through cached route")
			}
		})
	}
}

func TestNightlyExtractorRejectsUnsafeArchiveMembers(t *testing.T) {
	for _, name := range []string{"../escape", "/escape", "nested/hikyo", "hikyo"} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			gz := gzip.NewWriter(&out)
			writer := tar.NewWriter(gz)
			if err := writer.WriteHeader(&tar.Header{Name: "hikyo", Mode: 0700, Typeflag: tar.TypeReg, Size: 2}); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write([]byte("ok")); err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0700, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}); err != nil {
				t.Fatal(err)
			}
			if err := errors.Join(writer.Close(), gz.Close()); err != nil {
				t.Fatal(err)
			}
			if _, err := extractNightlyBinary("release.tar.gz", out.Bytes()); err == nil {
				t.Fatal("unsafe tar archive accepted")
			}
		})
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	header := &zip.FileHeader{Name: "hikyo.exe"}
	header.SetMode(os.ModeSymlink | 0700)
	file, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("/etc/passwd")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := extractNightlyBinary("release.zip", out.Bytes()); err == nil {
		t.Fatal("zip executable symlink accepted")
	}
}

func TestPreparationRefusesSymlinkedExecutableAndTrustState(t *testing.T) {
	for _, which := range []string{"executable", "trust state"} {
		t.Run(which, func(t *testing.T) {
			installer, status, _, _, _, _ := preparedNightlyFixture(t, nil)
			prepared, err := installer.PrepareNightly(t.Context(), status)
			if err != nil {
				t.Fatal(err)
			}
			path := prepared.BinaryPath
			if which == "trust state" {
				path = filepath.Join(installer.config.StateDir, "nightly-trust.json")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "unchanged")
			if err := os.WriteFile(outside, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			if _, err := installer.PrepareNightly(t.Context(), status); err == nil {
				t.Fatal("symlinked preparation state accepted")
			}
			after, err := os.ReadFile(outside)
			if err != nil || !bytes.Equal(raw, after) {
				t.Fatal("symlink target changed")
			}
		})
	}
}

func TestNightlyExtractorRejectsHardlinks(t *testing.T) {
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	writer := tar.NewWriter(gz)
	if err := writer.WriteHeader(&tar.Header{Name: "hikyo", Typeflag: tar.TypeLink, Linkname: "outside", Mode: 0700}); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(writer.Close(), gz.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := extractNightlyBinary("release.tar.gz", out.Bytes()); err == nil {
		t.Fatal("hardlink accepted as regular executable")
	}
}

func TestAutomaticCacheCleanupKeepsTargetAndReauthenticatesWithoutDownload(t *testing.T) {
	installer, status, _, _, _, responses := preparedNightlyFixture(t, nil)
	installer.config.TransientBundles = true
	prepared, err := installer.PrepareNightly(t.Context(), status)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(installer.config.StateDir, "nightly-trust.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := installer.PruneNightlyCache(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("cleanup modified anti-rollback state", err)
	}
	if _, err := os.Stat(prepared.BundleDirectory); !os.IsNotExist(err) {
		t.Fatal("retained private bundle", err)
	}
	for _, asset := range status.Assets {
		responses[asset.URL] = nil
	}
	if _, err := installer.PrepareNightly(t.Context(), status); err != nil {
		t.Fatal("cache retry downloaded release again", err)
	}
	if err := os.WriteFile(filepath.Join(prepared.Directory, status.Assets[0].Name), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.PrepareNightly(t.Context(), status); err == nil {
		t.Fatal("cache retry skipped authentication")
	}
}
