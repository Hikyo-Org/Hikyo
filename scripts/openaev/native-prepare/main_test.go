package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Masterminds/semver/v3"
)

func TestCanonicalCorePrereleasesUseStableSignatureAdapter(t *testing.T) {
	for _, tc := range []struct {
		version             string
		prerelease, adapter bool
	}{
		{"0.1.0", true, true},
		{"1.0.0-rc.1", true, true},
		{"1.0.0-foo-nightly.1", true, true},
		{"1.0.0", false, false},
		{"0.0.1-nightly.20261012.75.g868703c7", true, false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			version, err := semver.StrictNewVersion(tc.version)
			if err != nil {
				t.Fatal(err)
			}
			if usesStablePrereleaseAdapter(version, updatecheck.Release{Prerelease: tc.prerelease}) != tc.adapter {
				t.Fatal("wrong native trust preparation path")
			}
		})
	}
}

func archiveFixture(t *testing.T, headers []*tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tarWriter := tar.NewWriter(gz)
	for _, h := range headers {
		if err := tarWriter.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := tarWriter.Write(bytes.Repeat([]byte("x"), int(h.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := errors.Join(tarWriter.Close(), gz.Close()); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func rcFixture(t *testing.T) (updatecheck.Release, *testfixture.Fixture, map[string][]byte, releaseidentity.Identity) {
	return stablePrereleaseFixture(t, "1.0.0-rc.1")
}

func stablePrereleaseFixture(t *testing.T, version string) (updatecheck.Release, *testfixture.Fixture, map[string][]byte, releaseidentity.Identity) {
	t.Helper()
	commit := strings.Repeat("a", 40)
	_, declaration, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	declaration.Profile, declaration.Version, declaration.Sequence, declaration.Commit = releaseidentity.StableV1, version, 1, commit
	f := testfixture.New(t)
	release := f.AddStable(t, version, 1, commit, testfixture.JSON(t, declaration))
	var manifest releasetrust.Manifest
	if err := definitions.DecodeStrict(release.Material.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	archive := archiveFixture(t, []*tar.Header{{Name: "hikyo", Typeflag: tar.TypeReg, Size: 4, Mode: 0700}})
	name := "hikyo_" + version + "_Linux_arm64.tar.gz"
	manifest.Artifacts[len(manifest.Artifacts)-1] = releasetrust.Artifact{Name: name, Kind: "binary", SHA256: string(releaseidentity.Hash(archive))}
	release.Material.Manifest = testfixture.JSON(t, manifest)
	release.Material.ManifestSignature = testfixture.Sign(t, f.PrimarySigner, release.Material.Manifest)
	release.Identity.ManifestSHA256 = releaseidentity.Hash(release.Material.Manifest)
	f.Metadata.Releases[0].ManifestSHA256 = string(release.Identity.ManifestSHA256)
	material := f.Material(t)
	responses := map[string][]byte{}
	for name, raw := range map[string][]byte{"metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature, "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature, "primary.pub": f.PrimaryPublic} {
		responses[trustBase+name] = raw
	}
	selected := updatecheck.Release{Version: version, Prerelease: true, Immutable: true, PublishedAt: time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)}
	for asset, raw := range map[string][]byte{"release-manifest.json": release.Material.Manifest, "release-manifest.sigstore.json": release.Material.ManifestSignature, "release-candidate.json": release.Material.Candidate, "upgrade-compatibility.json": release.Material.Compatibility, name: archive, name + ".sigstore.json": testfixture.Sign(t, f.PrimarySigner, archive)} {
		url := "https://github.com/Hikyo-Org/Hikyo/releases/download/v" + version + "/" + asset
		selected.Assets = append(selected.Assets, updatecheck.Asset{Name: asset, URL: url, Digest: "sha256:" + string(releaseidentity.Hash(raw)), Size: int64(len(raw))})
		responses[url] = raw
	}
	return selected, f, responses, release.Identity
}

func TestSignedRCPreparesOfflineBundleWithoutExecutingCandidate(t *testing.T) {
	for _, version := range []string{"1.0.0-rc.1", "0.1.0"} {
		t.Run(version, func(t *testing.T) {
			selected, fixture, responses, expected := stablePrereleaseFixture(t, version)
			state := t.TempDir()
			fetch := func(ctx context.Context, url string, limit int64) ([]byte, error) {
				raw, found := responses[url]
				if !found || int64(len(raw)) > limit {
					return nil, errors.New("missing fixture")
				}
				return raw, nil
			}
			prepared, err := prepareRCWithFetcher(t.Context(), selected, expected.Commit, state, fixture.Pinned, fetch, "linux/arm64")
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Identity != expected {
				t.Fatalf("wrong RC identity: %v", prepared.Identity)
			}
			raw, err := os.ReadFile(prepared.BinaryPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != "xxxx" || prepared.BinarySHA256 != releaseidentity.Hash(raw) {
				t.Fatal("archive executable differs from proof")
			}
			bundle, err := upgradebundle.Load(t.Context(), prepared.BundleDirectory, fixture.Pinned, releaseidentity.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := bundle.Release(expected); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRCRejectsTamperedSignatureAndWrongSourceBeforePublishingProof(t *testing.T) {
	for _, failure := range []string{"signature", "source"} {
		t.Run(failure, func(t *testing.T) {
			selected, fixture, responses, expected := rcFixture(t)
			commit := expected.Commit
			if failure == "signature" {
				url := "https://github.com/Hikyo-Org/Hikyo/releases/download/v" + selected.Version + "/release-manifest.sigstore.json"
				raw := []byte(`{"base64Signature":"AAAA"}`)
				responses[url] = raw
				for i := range selected.Assets {
					if selected.Assets[i].URL == url {
						selected.Assets[i].Size = int64(len(raw))
						selected.Assets[i].Digest = "sha256:" + string(releaseidentity.Hash(raw))
					}
				}
			} else {
				commit = strings.Repeat("b", 40)
			}
			state := t.TempDir()
			fetch := func(ctx context.Context, url string, limit int64) ([]byte, error) {
				raw, found := responses[url]
				if !found {
					return nil, errors.New("missing fixture")
				}
				return raw, nil
			}
			if _, err := prepareRCWithFetcher(t.Context(), selected, commit, state, fixture.Pinned, fetch, "linux/arm64"); err == nil {
				t.Fatal("invalid signed RC accepted")
			}
			files, err := os.ReadDir(state)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 0 {
				t.Fatal("failed verification published output")
			}
		})
	}
}

func TestReleaseArchiveRejectsLinksTraversalDuplicatesAndBrokenChecksum(t *testing.T) {
	valid := &tar.Header{Name: "hikyo", Typeflag: tar.TypeReg, Size: 4, Mode: 0700}
	for name, headers := range map[string][]*tar.Header{"traversal": {{Name: "../hikyo", Typeflag: tar.TypeReg, Size: 4}}, "link": {{Name: "hikyo", Typeflag: tar.TypeSymlink, Linkname: "target"}}, "duplicate": {valid, valid}} {
		t.Run(name, func(t *testing.T) {
			if _, err := extractARM64(archiveFixture(t, headers)); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	raw := archiveFixture(t, []*tar.Header{valid})
	raw[len(raw)-5] ^= 1
	if _, err := extractARM64(raw); err == nil {
		t.Fatal("broken gzip checksum accepted")
	}
}

func TestSnapshotBindingRejectsChangedInventoryAndPublication(t *testing.T) {
	selected, _, _, _ := rcFixture(t)
	snapshot := map[string]any{"tag_name": "v" + selected.Version, "draft": false, "immutable": true, "prerelease": true, "published_at": selected.PublishedAt.Format(time.RFC3339)}
	assets := []map[string]any{}
	for _, a := range selected.Assets {
		assets = append(assets, map[string]any{"name": a.Name, "browser_download_url": a.URL, "digest": a.Digest, "size": a.Size})
	}
	snapshot["assets"] = assets
	path := filepath.Join(t.TempDir(), "release.json")
	write := func() {
		raw, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if err := bindSnapshot(path, selected); err != nil {
		t.Fatal(err)
	}
	assets[0]["digest"] = "sha256:" + strings.Repeat("0", 64)
	write()
	if err := bindSnapshot(path, selected); err == nil {
		t.Fatal("changed asset accepted")
	}
	assets[0]["digest"] = selected.Assets[0].Digest
	snapshot["published_at"] = "2026-10-13T02:01:00Z"
	write()
	if err := bindSnapshot(path, selected); err == nil {
		t.Fatal("changed publication accepted")
	}
}
