package unattendedfixture

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// TestWriteUnattendedContainerFixture exports three real Linux server binaries
// and signed archives under ephemeral test trust. The harness seeds only public
// artifact caches; normal server startup reauthenticates every byte and runs the
// production unattended coordinator. Nothing is published or signed by a live key.
func TestWriteUnattendedContainerFixture(t *testing.T) {
	output := os.Getenv("HIKYO_UNATTENDED_FIXTURE_OUTPUT")
	if output == "" {
		t.Skip("container acceptance fixture is opt-in")
	}
	if !filepath.IsAbs(output) {
		t.Fatal("fixture output must be absolute")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("fixture output must not exist")
	}
	arch := os.Getenv("HIKYO_UNATTENDED_FIXTURE_GOARCH")
	if arch == "" {
		arch = runtime.GOARCH
	}
	if arch != "amd64" && arch != "arm64" {
		t.Fatal("fixture requires amd64 or arm64")
	}
	_, declaration, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	declaration.Profile = releaseidentity.NightlyV1
	declaration.Commit = strings.Repeat("a", 40)
	declaration.Version, declaration.Sequence = "1.1.0-nightly.1", 2
	trust, _, _ := testfixture.Nightly(t, testfixture.JSON(t, declaration), false)
	put := func(name string, raw []byte, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(output, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, mode); err != nil {
			t.Fatal(err)
		}
	}
	root, err := crypto.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	put("root.key", []byte(crypto.EncodeRootKey(root)), 0600)
	put("architecture", []byte(arch+"\n"), 0644)
	put("public/trust-root.json", trust.Pinned.Root, 0644)
	put("public/recovery.pub", trust.Pinned.RecoveryPublicKey, 0644)
	var previous releasetrust.VerifiedRelease
	var releases []struct {
		label string
		id    releaseidentity.Identity
	}
	for i, label := range []string{"a", "b", "c"} {
		if i > 0 {
			for j, engine := range declaration.Engines {
				// Each successive image has a direct authenticated predecessor;
				// replacement does not need a network discovery fixture.
				declaration.Engines[j].Sources = append(engine.Sources, upgradecompat.SourceEdge{
					Source: releaseidentity.Source{Release: previous.Identity()}, Migrations: engine.Migrations,
					SchemaSHA256: engine.SchemaSHA256, Mode: upgradecompat.Maintenance,
				})
			}
		}
		declaration.Version = fmt.Sprintf("1.1.0-nightly.%d", i+1)
		declaration.Sequence = uint64(i + 2)
		claim := testfixture.JSON(t, declaration)
		encode := base64.StdEncoding.EncodeToString
		flags := []string{
			"-X main.version=" + declaration.Version,
			"-X main.commit=" + declaration.Commit,
			"-X main.updateChannel=off",
			"-X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedTrustRoot=" + encode(trust.Pinned.Root),
			"-X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedRecoveryPublicKey=" + encode(trust.Pinned.RecoveryPublicKey),
			"-X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedDeclaration=" + encode(claim),
			"-X github.com/Hikyo-Org/hikyo/internal/buildcompat.declarationSHA256=" + string(releaseidentity.Hash(claim)),
		}
		binaryPath := filepath.Join(output, "release-"+label, "hikyo")
		command := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-tags", "ui", "-ldflags", strings.Join(flags, " "), "-o", binaryPath, "./cmd/hikyo")
		command.Dir = "../../.."
		command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
		if raw, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", label, err, raw)
		}
		binary, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatal(err)
		}
		var archive bytes.Buffer
		gz := gzip.NewWriter(&archive)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: "hikyo", Mode: 0755, Size: int64(len(binary))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(binary); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		nameArch := arch
		if arch == "amd64" {
			nameArch = "x86_64"
		}
		archiveName := "hikyo_" + declaration.Version + "_Linux_" + nameArch + ".tar.gz"
		payloads := map[string][]byte{
			releasetrust.CompatibilityArtifact: claim,
			archiveName:                        archive.Bytes(),
			"binary-provenance.json":           []byte("{}"),
			"checksums.txt":                    []byte(string(releaseidentity.Hash(archive.Bytes())) + "  " + archiveName + "\n"),
		}
		artifacts := []releasetrust.Artifact{
			{Name: releasetrust.CompatibilityArtifact, Kind: "upgrade-compatibility"},
			{Name: archiveName, Kind: "binary", Platform: "linux/" + arch},
			{Name: "binary-provenance.json", Kind: "binary-provenance"},
			{Name: "checksums.txt", Kind: "checksum"},
		}
		material := trust.SignNightlyWithPayloads(claim, declaration.Version, declaration.Sequence, payloads, artifacts)
		directoryName := "public/release-" + label
		for name, reader := range material.Artifacts {
			raw, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			put(directoryName+"/"+name, raw, 0644)
		}
		put(directoryName+"/release-manifest.json", material.Manifest, 0644)
		put(directoryName+"/release-manifest.sigstore.json", material.Bundle, 0644)
		previous, err = upgradebundle.VerifyNightlyPlatformDirectory(t.Context(), filepath.Join(output, directoryName), trust.Snapshot(t), "linux/"+arch)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, struct {
			label string
			id    releaseidentity.Identity
		}{label, previous.Identity()})
		bundleName := "public/bundle-" + label
		index := upgradebundle.Index{Format: upgradebundle.PlatformIndexFormat, NightlyPlatform: "linux/" + arch, PrimaryKeyIDs: []string{"test-primary"}, Releases: []upgradebundle.ReleaseEntry{}, Bridges: []releaseidentity.Digest{}}
		for _, release := range releases {
			_, err := upgradebundle.CopyNightlyPlatformRelease(t.Context(), filepath.Join(output, "public/release-"+release.label), filepath.Join(output, bundleName, "releases"), trust.Snapshot(t), "linux/"+arch)
			if err != nil {
				t.Fatal(err)
			}
			index.Releases = append(index.Releases, upgradebundle.ReleaseEntry{Profile: releaseidentity.NightlyV1, ManifestSHA256: release.id.ManifestSHA256})
		}
		snapshot := trust.Material(t)
		for name, raw := range map[string][]byte{"index.json": testfixture.JSON(t, index), "metadata.json": snapshot.Metadata, "metadata.sigstore.json": snapshot.MetadataSignature, "catalog.json": snapshot.Catalog, "catalog.sigstore.json": snapshot.CatalogSignature, "keys/test-primary.pub": trust.PrimaryPublic} {
			put(bundleName+"/"+name, raw, 0644)
		}
		bundle, err := upgradebundle.Load(t.Context(), filepath.Join(output, bundleName), trust.Pinned, releaseidentity.SnapshotFloor{})
		if err != nil {
			t.Fatal(err)
		}
		node, err := bundle.MatchBuild(claim)
		if err != nil || node.Identity() != previous.Identity() {
			t.Fatal("exported build differs from authenticated target")
		}
		prepared := selfupdate.PreparedNightly{Identity: previous.Identity(), Directory: "/fixtures/release-" + label, BundleDirectory: "/fixtures/bundle-" + label, BinaryPath: "/usr/local/bin/hikyo", BinarySHA256: releaseidentity.Hash(binary)}
		put("descriptor-"+label+".json", testfixture.JSON(t, prepared), 0644)
	}
	// The assembler intentionally stages private directories/documents. This
	// exported subtree contains only signed public evidence, and the actual
	// distroless UID must be able to traverse/read it across a read-only mount.
	if err := makeFixturePublic(filepath.Join(output, "public")); err != nil {
		t.Fatal(err)
	}
}

func makeFixturePublic(directory string) error {
	return filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, 0755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("public fixture contains non-regular artifact %s", path)
		}
		return os.Chmod(path, 0644)
	})
}

func TestFixturePublicPermissions(t *testing.T) {
	output := t.TempDir()
	public := filepath.Join(output, "public")
	directory := filepath.Join(public, "bundle-a", "releases", "fixture")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(manifest, []byte("public signed evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(output, "root.key")
	if err := os.WriteFile(private, []byte("private fixture root"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := makeFixturePublic(public); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(public, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		want := os.FileMode(0644)
		if entry.IsDir() {
			want = 0755
		}
		if info.Mode().Perm() != want {
			return fmt.Errorf("%s mode %o, want %o for distroless reads", path, info.Mode().Perm(), want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(private)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private root permissions changed: %v", err)
	}
}
