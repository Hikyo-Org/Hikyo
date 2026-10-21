package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func TestStableDraftRoundTripAndTamperRejection(t *testing.T) {
	trust, directory := fullDraft(t)
	state := filepath.Join(t.TempDir(), "state.json")
	args := []string{"verify", "--trust", trust, "--directory", directory, "--version", "1.0.0", "--commit", strings.Repeat("a", 40), "--state", state, "--latest"}
	if err := run(args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"payload", "signature", "disk image", "unexpected file", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			name := filepath.Join(directory, "hikyo_1.0.0_Linux_x86_64.tar.gz")
			if mode == "disk image" {
				name = filepath.Join(directory, "hikyo_1.0.0_Darwin_arm64.dmg")
			}
			if mode == "signature" {
				name += ".sigstore.json"
			}
			if mode == "unexpected file" {
				name = filepath.Join(directory, "unreviewed.txt")
			}
			original, readErr := os.ReadFile(name)
			if readErr != nil && mode != "unexpected file" {
				t.Fatal(readErr)
			}
			if mode == "symlink" {
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("checksums.txt", name); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(name, []byte("substituted"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := run(args, &bytes.Buffer{}); err == nil {
				t.Fatal("accepted invalid draft")
			}
			after, err := os.ReadFile(state)
			if err != nil || !bytes.Equal(saved, after) {
				t.Fatal("failed verification changed trust state")
			}
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
			if mode != "unexpected file" {
				if err := os.WriteFile(name, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestStableCandidateIsCanonicalAndCannotOverwrite(t *testing.T) {
	trust, _ := fullDraft(t)
	out := filepath.Join(t.TempDir(), "candidate.json")
	args := []string{"candidate", "--trust", trust, "--version", "1.0.0", "--commit", strings.Repeat("a", 40), "--out", out}
	if err := run(args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"commit":"` + strings.Repeat("a", 40) + `","key_id":"github-actions-stable","public_key":"stable-policy.json","sequence":1,"version":"1.0.0"}` + "\n"
	if string(raw) != expected {
		t.Fatalf("noncanonical candidate: %s", raw)
	}
	if err := run(args, &bytes.Buffer{}); err == nil {
		t.Fatal("overwrote candidate")
	}
	if err := run([]string{"candidate", "--trust", trust, "--version", "1.0.0-nightly.1", "--commit", strings.Repeat("a", 40), "--out", filepath.Join(t.TempDir(), "candidate.json")}, &bytes.Buffer{}); err == nil {
		t.Fatal("authorized nightly as stable")
	}
}

func TestIncompleteDelegationDoesNotFallBack(t *testing.T) {
	trust, _ := fullDraft(t)
	if err := os.Remove(filepath.Join(trust, "stable-policy.sigstore.json")); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"preflight", "--trust", trust}, &bytes.Buffer{}); err == nil {
		t.Fatal("accepted unsigned delegation with recovery-signed baseline")
	}
}

func fullDraft(t *testing.T) (string, string) {
	t.Helper()
	trust, directory, _, _ := fullDraftFixture(t)
	return trust, directory
}

func fullDraftFixture(t *testing.T) (string, string, *testfixture.Fixture, func([]byte, string, string) []byte) {
	t.Helper()
	f, delegated, _, sign := testfixture.StableWorkflow(t)
	// Initial activation still has a recovery-signed snapshot and no stable
	// releases. Exercise candidate -> prepare -> CI signing -> verification.
	f.Metadata.Sequence = 1
	f.Metadata.Event.SignedBy = f.Metadata.Recovery.ID
	f.Metadata.SourceCommit, f.Metadata.ReleaseTag = "", ""
	f.Metadata.Releases = []releasetrust.Release{}
	f.Metadata.HighestRelease, f.Metadata.HighestReleaseSequence = nil, nil
	f.Catalog.Sequence = 1
	f.Catalog.StablePolicySHA256 = ""
	baseline := f.Material(t)
	baseline.StablePolicy, baseline.StablePolicySignature, baseline.StableTrustedRoot = delegated.StablePolicy, delegated.StablePolicySignature, delegated.StableTrustedRoot
	trust, directory := t.TempDir(), t.TempDir()
	var root releasetrust.Root
	if err := json.Unmarshal(f.Pinned.Root, &root); err != nil {
		t.Fatal(err)
	}
	docs := map[string][]byte{"root.json": f.Pinned.Root, root.Recovery.PublicKey: f.Pinned.RecoveryPublicKey,
		root.BootstrapPrimary.PublicKey: f.PrimaryPublic, "metadata.json": baseline.Metadata, "metadata.sigstore.json": baseline.MetadataSignature,
		"catalog.json": baseline.Catalog, "catalog.sigstore.json": baseline.CatalogSignature, "stable-policy.json": baseline.StablePolicy,
		"stable-policy.sigstore.json": baseline.StablePolicySignature, "stable-trusted-root.json": baseline.StableTrustedRoot}
	for name, raw := range docs {
		if err := os.WriteFile(filepath.Join(trust, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	commit := strings.Repeat("a", 40)
	if err := run([]string{"candidate", "--trust", trust, "--version", "1.0.0", "--commit", commit, "--out", filepath.Join(directory, "release-candidate.json")}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	candidate, err := os.ReadFile(filepath.Join(directory, "release-candidate.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := releasetrust.Manifest{Schema: "hikyo.dev/release-manifest/v1", Version: "1.0.0", Tag: "v1.0.0", SourceCommit: commit, ReleaseSequence: 1, SigningKeyID: keylessID}
	payloads := map[string][]byte{}
	add := func(a releasetrust.Artifact, raw []byte) {
		a.SHA256 = string(releaseidentity.Hash(raw))
		manifest.Artifacts = append(manifest.Artifacts, a)
		payloads[a.Name] = raw
	}
	for _, platform := range []string{"Linux", "Darwin", "Windows"} {
		for _, arch := range []string{"x86_64", "arm64"} {
			ext, exe := ".tar.gz", ""
			if platform == "Windows" {
				ext, exe = ".zip", ".exe"
			}
			add(releasetrust.Artifact{Name: "hikyo_1.0.0_" + platform + "_" + arch + ext, Kind: "binary"}, []byte("synthetic binary "+platform+arch))
			add(releasetrust.Artifact{Name: "hikyo-release-verifier_" + platform + "_" + arch + exe, Kind: "release-verifier"}, []byte("synthetic verifier "+platform+arch))
		}
	}
	for _, format := range []string{"deb", "rpm", "apk", "archlinux"} {
		for _, arch := range []string{"amd64", "arm64"} {
			name, err := nativePackageName("1.0.0", format, arch)
			if err != nil {
				t.Fatal(err)
			}
			add(releasetrust.Artifact{Name: name, Kind: "package", Format: format, Arch: arch}, []byte("synthetic package "+format+arch))
		}
	}
	for _, arch := range []string{"arm64", "x86_64"} {
		add(releasetrust.Artifact{Name: "hikyo_1.0.0_Darwin_" + arch + ".dmg", Kind: "macos-dmg"}, []byte("synthetic disk image "+arch))
	}
	var checksums strings.Builder
	for _, a := range manifest.Artifacts {
		if a.Kind == "binary" {
			fmt.Fprintf(&checksums, "%s  %s\n", a.SHA256, a.Name)
		}
	}
	binaryProvenance := []byte(`{"schema":"hikyo.dev/release-binaries/v1","source_commit":"` + commit + `","version":"1.0.0","producer":{"name":"goreleaser","build_id":"hikyo","config":".goreleaser.yaml","config_sha256":"` + strings.Repeat("d", 64) + `"},"packages":[{"goos":"linux","goarch":"amd64","archive_input":{"build_id":"hikyo","sha256":"` + strings.Repeat("e", 64) + `"},"oci_input":{"path":"image-root/amd64/hikyo","sha256":"` + strings.Repeat("e", 64) + `"}},{"goos":"linux","goarch":"arm64","archive_input":{"build_id":"hikyo","sha256":"` + strings.Repeat("f", 64) + `"},"oci_input":{"path":"image-root/arm64/hikyo","sha256":"` + strings.Repeat("f", 64) + `"}}]}`)
	for _, entry := range []struct {
		name, kind string
		raw        []byte
	}{
		{"release-candidate.json", "release-candidate", candidate}, {"upgrade-compatibility.json", "upgrade-compatibility", []byte("compatibility")},
		{"stable-policy.json", "stable-policy", baseline.StablePolicy}, {"stable-policy.sigstore.json", "stable-policy-signature", baseline.StablePolicySignature},
		{"stable-trusted-root.json", "sigstore-trusted-root", baseline.StableTrustedRoot}, {"checksums.txt", "checksum", []byte(checksums.String())},
		{"binary-provenance.json", "binary-provenance", binaryProvenance}, {"source.spdx.json", "sbom", []byte("{}")}, {"install.sh", "installer", []byte("#!/bin/sh\n")},
	} {
		add(releasetrust.Artifact{Name: entry.name, Kind: entry.kind}, entry.raw)
	}
	image, chart := "ghcr.io/example/hikyo", "ghcr.io/example/charts/hikyo"
	imageDigest, chartDigest := "sha256:"+strings.Repeat("b", 64), "sha256:"+strings.Repeat("c", 64)
	add(releasetrust.Artifact{Name: "image-index.digest", Kind: "image", Image: image, Digest: imageDigest, Tag: "1.0.0"}, []byte(imageDigest+"\n"))
	add(releasetrust.Artifact{Name: "chart-index.digest", Kind: "chart-digest", Chart: chart, Digest: chartDigest}, []byte(chartDigest+"\n"))
	add(releasetrust.Artifact{Name: "hikyo-1.0.0.tgz", Kind: "chart", ChartVersion: "1.0.0", AppVersion: "1.0.0", ImageRepository: image, ImageDigest: imageDigest}, chartArchive(t, image, imageDigest))
	for _, a := range []struct{ kind, ref, digest string }{{"image", image, imageDigest}, {"chart", chart, chartDigest}} {
		raw := []byte(`{"critical":{"identity":{"docker-reference":"` + a.ref + `"},"image":{"docker-manifest-digest":"` + a.digest + `"},"type":"cosign container image signature"},"optional":null}`)
		add(releasetrust.Artifact{Name: a.kind + "-index.oci-payload.json", Kind: "oci-payload", SubjectKind: a.kind, Subject: a.ref + "@" + a.digest, Digest: a.digest}, raw)
	}
	var policy releasetrust.StablePolicy
	if err := json.Unmarshal(baseline.StablePolicy, &policy); err != nil {
		t.Fatal(err)
	}
	provenance := testfixture.StableProvenance(t, policy, manifest)
	add(releasetrust.Artifact{Name: "build-provenance.json", Kind: "build-provenance"}, provenance)
	for name, raw := range payloads {
		if err := os.WriteFile(filepath.Join(directory, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeNewJSON(filepath.Join(directory, "release-manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"prepare", "--trust", trust, "--directory", directory}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		if name == "stable-policy.json" || name == "stable-policy.sigstore.json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		sigName := name + ".sigstore.json"
		if name == "metadata.json" || name == "catalog.json" || name == "release-manifest.json" {
			sigName = strings.TrimSuffix(name, ".json") + ".sigstore.json"
		}
		if err := os.WriteFile(filepath.Join(directory, sigName), sign(raw, commit, "v1.0.0"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return trust, directory, f, sign
}

func chartArchive(t *testing.T, image, digest string) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	writer := tar.NewWriter(gz)
	for name, raw := range map[string]string{"hikyo/Chart.yaml": "version: 1.0.0\nappVersion: 1.0.0\n", "hikyo/values.yaml": "image:\n  repository: " + image + "\n  digest: " + digest + "\n"} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
