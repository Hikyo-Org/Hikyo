package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

const nightlyTestVersion = "1.0.1-nightly.20260824.1.gaaaaaaaa"

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func releaseArchive(t *testing.T, version string, binary []byte) (string, []byte) {
	t.Helper()
	name, err := archiveName(version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if runtime.GOOS == "windows" {
		writer := zip.NewWriter(&output)
		file, err := writer.Create("hikyo.exe")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(binary); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return name, output.Bytes()
	}
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "hikyo", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return name, output.Bytes()
}

func installerFixture(t *testing.T, checksumOverride string) (*Installer, updatecheck.Status, string, map[string][]byte) {
	return installerFixtureForVersion(t, nightlyTestVersion, true, checksumOverride)
}

func installerFixtureForVersion(t *testing.T, version string, prerelease bool, checksumOverride string) (*Installer, updatecheck.Status, string, map[string][]byte) {
	t.Helper()
	binary := []byte("new hikyo binary\n")
	archiveName, archive := releaseArchive(t, version, binary)
	archiveDigest := sha256.Sum256(archive)
	checksum := fmt.Sprintf("%x  %s\n", archiveDigest, archiveName)
	if checksumOverride != "" {
		checksum = checksumOverride
	}
	checksumDigest := sha256.Sum256([]byte(checksum))
	base := "https://github.com/Hikyo-Org/Hikyo/releases/download/v" + version + "/"
	responses := map[string][]byte{
		base + archiveName:     archive,
		base + "checksums.txt": []byte(checksum),
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, ok := responses[request.URL.String()]
		if !ok {
			t.Fatalf("unexpected download %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
	target := filepath.Join(t.TempDir(), "hikyo")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	if err := os.WriteFile(target, []byte("old hikyo binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	installer := newInstaller(client, func() (string, error) { return target, nil })
	status := updatecheck.Status{
		Available: true, Channel: updatecheck.ChannelNightly,
		CurrentVersion: "1.0.0", LatestVersion: version, Prerelease: prerelease,
		Assets: []updatecheck.Asset{
			{Name: archiveName, URL: base + archiveName, Size: int64(len(archive)), Digest: fmt.Sprintf("sha256:%x", archiveDigest)},
			{Name: "checksums.txt", URL: base + "checksums.txt", Size: int64(len(checksum)), Digest: fmt.Sprintf("sha256:%x", checksumDigest)},
		},
	}
	return installer, status, target, responses
}

func TestStableInstallerFailsClosedWithoutImmutableSignedTrust(t *testing.T) {
	installer, status, target, _ := installerFixtureForVersion(t, "1.0.1", false, "")
	status.Channel = updatecheck.ChannelStable

	if err := installer.Apply(t.Context(), status); err == nil || !strings.Contains(err.Error(), "not immutable") {
		t.Fatalf("Apply() error = %v, want immutable-release refusal", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "old hikyo binary\n" {
		t.Fatalf("binary changed after stable trust refusal: %q", got)
	}

	status.Immutable = true
	if err := installer.Apply(t.Context(), status); err == nil || !strings.Contains(err.Error(), "trust state directory") {
		t.Fatalf("Apply() error = %v, want absent embedded-trust refusal", err)
	}
}

func TestStableChannelRejectsAPrereleaseBeforeDownload(t *testing.T) {
	installer, status, _, _ := installerFixture(t, "")
	status.Channel = updatecheck.ChannelStable
	if err := installer.Apply(t.Context(), status); err == nil || !strings.Contains(err.Error(), "cannot install a prerelease") {
		t.Fatalf("Apply() error = %v, want stable-channel prerelease refusal", err)
	}
}

func TestNightlyTrackVerifiesAPromotedStableWithThePinnedRoot(t *testing.T) {
	installer, status, target, responses := installerFixtureForVersion(t, "1.0.1", false, "")
	status.Channel = updatecheck.ChannelNightly
	status.Immutable = true
	recoveryPrivate, recoveryPublic := signingKey(t)
	primaryPrivate, primaryPublic := signingKey(t)
	archiveName := mustArchiveName(t, "1.0.1")
	base := "https://github.com/Hikyo-Org/Hikyo/releases/download/v1.0.1/"
	archive := responses[base+archiveName]
	candidate := mustJSON(t, releaseCandidate{
		Version: "1.0.1", Sequence: 1, Commit: strings.Repeat("a", 40),
		KeyID: "primary-1", PublicKey: "primary-1.pub",
	})
	manifest := mustJSON(t, releaseManifest{
		Schema: "hikyo.dev/release-manifest/v1", Version: "1.0.1", Tag: "v1.0.1",
		SourceCommit: strings.Repeat("a", 40), ReleaseSequence: 1, SigningKeyID: "primary-1",
		Artifacts: []manifestArtifact{
			{Name: archiveName, Kind: "binary", SHA256: digestHex(archive)},
			{Name: "release-candidate.json", Kind: "release-candidate", SHA256: digestHex(candidate)},
		},
	})
	metadata := mustJSON(t, trustMetadata{
		Schema: "hikyo.dev/trust-metadata/v1", Sequence: 1,
		HighestRelease: stringPointer("1.0.1"), HighestReleaseSequence: int64Pointer(1),
		Recovery: struct {
			ID     string `json:"id"`
			SHA256 string `json:"sha256"`
		}{ID: "recovery-1", SHA256: digestHex(recoveryPublic)},
		Event: struct {
			Type     string `json:"type"`
			SignedBy string `json:"signed_by"`
		}{Type: "release", SignedBy: "recovery-1"},
		PrimaryKeys: []trustPrimary{{
			ID: "primary-1", PublicKey: "primary-1.pub", SHA256: digestHex(primaryPublic),
			ValidFromReleaseSequence: 1,
		}},
		Releases: []trustRelease{{Version: "1.0.1", Sequence: 1, ManifestSHA256: digestHex(manifest)}},
	})
	root := mustJSON(t, trustRoot{
		Schema:           "hikyo.dev/trust-root/v1",
		Recovery:         trustRootKey{ID: "recovery-1", PublicKey: "recovery-1.pub", SHA256: digestHex(recoveryPublic)},
		BootstrapPrimary: trustRootKey{ID: "primary-1", PublicKey: "primary-1.pub", SHA256: digestHex(primaryPublic)},
	})
	responses[trustURL("metadata.json")] = metadata
	responses[trustURL("metadata.sigstore.json")] = signatureBundle(t, recoveryPrivate, metadata)
	responses[trustURL("primary-1.pub")] = primaryPublic
	addAssetResponse(&status, responses, base, "release-manifest.json", manifest)
	addAssetResponse(&status, responses, base, "release-manifest.sigstore.json", signatureBundle(t, primaryPrivate, manifest))
	addAssetResponse(&status, responses, base, "release-candidate.json", candidate)
	addAssetResponse(&status, responses, base, archiveName+".sigstore.json", signatureBundle(t, primaryPrivate, archive))
	installer.config = Config{
		StateDir: t.TempDir(), TrustRootBase64: base64.StdEncoding.EncodeToString(root),
		RecoveryKeyBase64: base64.StdEncoding.EncodeToString(recoveryPublic),
	}

	if err := installer.Apply(t.Context(), status); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "new hikyo binary\n" {
		t.Fatalf("updated binary = %q", got)
	}
}

func TestInstallerAppliesTheSelectedReleaseInPlace(t *testing.T) {
	installer, status, target, _ := installerFixture(t, "")
	if err := installer.Apply(t.Context(), status); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "new hikyo binary\n" {
		t.Fatalf("updated binary = %q", got)
	}
}

func TestInstallerRefusesChecksumMismatchWithoutChangingTheBinary(t *testing.T) {
	installer, status, target, _ := installerFixture(t,
		strings.Repeat("0", 64)+"  "+mustArchiveName(t, nightlyTestVersion)+"\n")
	if err := installer.Apply(context.Background(), status); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Apply() error = %v, want checksum refusal", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "old hikyo binary\n" {
		t.Fatalf("binary changed after refusal: %q", got)
	}
}

func TestInstallerRefusesAnUnpinnedAssetURLBeforeDownload(t *testing.T) {
	installer, status, target, _ := installerFixture(t, "")
	status.Assets[0].URL = "https://downloads.example/hikyo.tar.gz"
	if err := installer.Apply(t.Context(), status); err == nil || !strings.Contains(err.Error(), "unexpected download URL") {
		t.Fatalf("Apply() error = %v, want URL refusal", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "old hikyo binary\n" {
		t.Fatalf("binary changed after URL refusal: %q", got)
	}
}

func TestNativeCosignVerificationRejectsTamperedPayload(t *testing.T) {
	private, public := signingKey(t)
	payload := []byte("signed release metadata")
	bundle := signatureBundle(t, private, payload)
	if err := verifyBlobSignature(public, bundle, payload); err != nil {
		t.Fatal(err)
	}
	if err := verifyBlobSignature(public, bundle, []byte("tampered release metadata")); err == nil {
		t.Fatal("tampered payload passed native Cosign verification")
	}
}

func mustArchiveName(t *testing.T, version string) string {
	t.Helper()
	name, err := archiveName(version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func signingKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return private, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func signatureBundle(t *testing.T, key *ecdsa.PrivateKey, payload []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(payload)
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return mustJSON(t, legacySignatureBundle{Base64Signature: base64.StdEncoding.EncodeToString(signature)})
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func addAssetResponse(status *updatecheck.Status, responses map[string][]byte, base, name string, raw []byte) {
	responses[base+name] = raw
	status.Assets = append(status.Assets, updatecheck.Asset{
		Name: name, URL: base + name, Size: int64(len(raw)), Digest: "sha256:" + digestHex(raw),
	})
}

func stringPointer(value string) *string { return &value }
func int64Pointer(value int64) *int64    { return &value }
