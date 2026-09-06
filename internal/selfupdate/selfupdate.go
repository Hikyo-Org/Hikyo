// Package selfupdate installs a selected Hikyo release over the current binary.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Masterminds/semver/v3"
)

const (
	maxArchiveBytes  = 256 << 20
	maxChecksumBytes = 1 << 20
	maxBinaryBytes   = 256 << 20
	maxTrustBytes    = 4 << 20
)

// Config carries the trust material stamped into published artifacts. Direct
// source builds leave it empty because their default update channel is off.
type Config struct {
	StateDir          string
	TrustRootBase64   string
	RecoveryKeyBase64 string
}

// Installer downloads, validates, and atomically replaces one Hikyo binary.
type Installer struct {
	client         *http.Client
	executablePath func() (string, error)
	config         Config
}

// NewInstaller constructs the production updater with Hikyo's restricted
// release-download client.
func NewInstaller(config Config) (*Installer, error) {
	client, err := updatecheck.NewDownloadHTTPClient(60 * time.Second)
	if err != nil {
		return nil, err
	}
	installer := newInstaller(client, os.Executable)
	installer.config = config
	return installer, nil
}

func newInstaller(client *http.Client, executablePath func() (string, error)) *Installer {
	return &Installer{client: client, executablePath: executablePath}
}

// Apply replaces the resolved current executable with the selected release.
func (i *Installer) Apply(ctx context.Context, status updatecheck.Status) error {
	if i == nil || i.client == nil || i.executablePath == nil {
		return errors.New("selfupdate: installer is not configured")
	}
	if !status.Available || status.LatestVersion == "" {
		return errors.New("selfupdate: no selected update is available")
	}
	if status.Channel != updatecheck.ChannelStable && status.Channel != updatecheck.ChannelNightly {
		return fmt.Errorf("selfupdate: channel %q cannot install updates", status.Channel)
	}
	selectedVersion, err := semver.StrictNewVersion(status.LatestVersion)
	if err != nil {
		return fmt.Errorf("selfupdate: selected version is not SemVer: %w", err)
	}
	isPrerelease := selectedVersion.Prerelease() != ""
	if status.Prerelease != isPrerelease {
		return errors.New("selfupdate: selected release prerelease metadata is inconsistent")
	}
	if status.Channel == updatecheck.ChannelStable && isPrerelease {
		return errors.New("selfupdate: stable channel cannot install a prerelease")
	}

	archiveFile, err := archiveName(status.LatestVersion, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	archiveAsset, err := exactAsset(status.LatestVersion, archiveFile, status.Assets)
	if err != nil {
		return err
	}
	checksumAsset, err := exactAsset(status.LatestVersion, "checksums.txt", status.Assets)
	if err != nil {
		return err
	}
	checksumFile, err := i.download(ctx, checksumAsset, maxChecksumBytes)
	if err != nil {
		return err
	}
	wantArchiveDigest, err := checksumFor(archiveFile, checksumFile)
	if err != nil {
		return err
	}
	archive, err := i.download(ctx, archiveAsset, maxArchiveBytes)
	if err != nil {
		return err
	}
	gotArchiveDigest := sha256.Sum256(archive)
	if !bytes.Equal(wantArchiveDigest, gotArchiveDigest[:]) {
		return fmt.Errorf("selfupdate: archive checksum mismatch for %s", archiveFile)
	}
	if isPrerelease {
		return i.stageNightly(ctx, status)
	}
	if !isPrerelease {
		if err := i.verifyStable(ctx, status, archiveFile, archive); err != nil {
			return err
		}
	}
	binary, err := extractBinary(archiveFile, archive)
	if err != nil {
		return err
	}

	target, err := i.executablePath()
	if err != nil {
		return fmt.Errorf("selfupdate: locate current executable: %w", err)
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("selfupdate: resolve current executable: %w", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("selfupdate: inspect current executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("selfupdate: current executable is not a regular file: %s", target)
	}
	if err := replaceBinary(ctx, target, binary, info.Mode().Perm()); err != nil {
		return fmt.Errorf("selfupdate: replace %s: %w", target, err)
	}
	return nil
}

func archiveName(version, goos, goarch string) (string, error) {
	var osName, extension string
	switch goos {
	case "darwin":
		osName, extension = "Darwin", ".tar.gz"
	case "linux":
		osName, extension = "Linux", ".tar.gz"
	case "windows":
		osName, extension = "Windows", ".zip"
	default:
		return "", fmt.Errorf("selfupdate: unsupported operating system %q", goos)
	}
	archName := goarch
	if goarch == "amd64" {
		archName = "x86_64"
	}
	if archName != "x86_64" && archName != "arm64" {
		return "", fmt.Errorf("selfupdate: unsupported architecture %q", goarch)
	}
	if strings.ContainsAny(version, `/\\`) || version == "" {
		return "", fmt.Errorf("selfupdate: unsafe release version %q", version)
	}
	return fmt.Sprintf("hikyo_%s_%s_%s%s", version, osName, archName, extension), nil
}

func exactAsset(version, name string, assets []updatecheck.Asset) (updatecheck.Asset, error) {
	wantURL := "https://github.com/Hikyo-Org/Hikyo/releases/download/v" + version + "/" + name
	var selected updatecheck.Asset
	found := 0
	for _, asset := range assets {
		if asset.Name == name {
			selected = asset
			found++
		}
	}
	if found != 1 {
		return updatecheck.Asset{}, fmt.Errorf("selfupdate: release %s has %d assets named %s", version, found, name)
	}
	if selected.URL != wantURL {
		return updatecheck.Asset{}, fmt.Errorf("selfupdate: asset %s has unexpected download URL", name)
	}
	if selected.Size <= 0 {
		return updatecheck.Asset{}, fmt.Errorf("selfupdate: asset %s has invalid size", name)
	}
	if _, err := assetDigest(selected); err != nil {
		return updatecheck.Asset{}, err
	}
	return selected, nil
}

func assetDigest(asset updatecheck.Asset) ([]byte, error) {
	algorithm, encoded, ok := strings.Cut(asset.Digest, ":")
	if !ok || algorithm != "sha256" || len(encoded) != sha256.Size*2 {
		return nil, fmt.Errorf("selfupdate: asset %s has no valid GitHub SHA-256 digest", asset.Name)
	}
	digest, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: asset %s has invalid GitHub SHA-256 digest: %w", asset.Name, err)
	}
	return digest, nil
}

func (i *Installer) download(ctx context.Context, asset updatecheck.Asset, limit int64) ([]byte, error) {
	if asset.Size > limit {
		return nil, fmt.Errorf("selfupdate: asset %s exceeds size limit", asset.Name)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "hikyo-self-update")
	response, err := i.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: download %s: %w", asset.Name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("selfupdate: download %s returned HTTP %d", asset.Name, response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: read %s: %w", asset.Name, err)
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("selfupdate: asset %s exceeds size limit", asset.Name)
	}
	if int64(len(raw)) != asset.Size {
		return nil, fmt.Errorf("selfupdate: asset %s size is %d, expected %d", asset.Name, len(raw), asset.Size)
	}
	want, err := assetDigest(asset)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(raw)
	if !bytes.Equal(want, got[:]) {
		return nil, fmt.Errorf("selfupdate: GitHub digest mismatch for %s", asset.Name)
	}
	return raw, nil
}

func (i *Installer) downloadURL(ctx context.Context, source string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(source, "https://raw.githubusercontent.com/Hikyo-Org/Hikyo/") {
		return nil, errors.New("selfupdate: untrusted stable metadata URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "hikyo-self-update")
	response, err := i.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: download stable trust metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("selfupdate: stable trust metadata returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: read stable trust metadata: %w", err)
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("selfupdate: stable trust metadata exceeds size limit")
	}
	return raw, nil
}

func checksumFor(name string, raw []byte) ([]byte, error) {
	var selected []byte
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != name {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("selfupdate: checksums.txt names %s more than once", name)
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("selfupdate: invalid checksum for %s", name)
		}
		selected = digest
	}
	if selected == nil {
		return nil, fmt.Errorf("selfupdate: checksums.txt does not name %s", name)
	}
	return selected, nil
}

func extractBinary(archiveName string, raw []byte) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		return extractZipBinary(raw)
	}
	if strings.HasSuffix(archiveName, ".tar.gz") {
		return extractTarBinary(raw)
	}
	return nil, fmt.Errorf("selfupdate: unsupported archive %s", archiveName)
}

func extractTarBinary(raw []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: open release archive: %w", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var binary []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("selfupdate: read release archive: %w", err)
		}
		if header.Name != "hikyo" {
			continue
		}
		if binary != nil || !header.FileInfo().Mode().IsRegular() || header.Size <= 0 || header.Size > maxBinaryBytes {
			return nil, errors.New("selfupdate: release archive has an invalid hikyo payload")
		}
		binary, err = io.ReadAll(io.LimitReader(reader, maxBinaryBytes+1))
		if err != nil || int64(len(binary)) != header.Size {
			return nil, errors.New("selfupdate: release archive has an unreadable hikyo payload")
		}
	}
	if binary == nil {
		return nil, errors.New("selfupdate: release archive has no hikyo payload")
	}
	return binary, nil
}

func extractZipBinary(raw []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: open release archive: %w", err)
	}
	var binary []byte
	for _, file := range reader.File {
		if file.Name != "hikyo.exe" {
			continue
		}
		if binary != nil || file.FileInfo().IsDir() || file.UncompressedSize64 == 0 || file.UncompressedSize64 > maxBinaryBytes {
			return nil, errors.New("selfupdate: release archive has an invalid hikyo.exe payload")
		}
		input, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("selfupdate: open hikyo.exe payload: %w", err)
		}
		binary, err = io.ReadAll(io.LimitReader(input, maxBinaryBytes+1))
		closeErr := input.Close()
		if err != nil || closeErr != nil || uint64(len(binary)) != file.UncompressedSize64 {
			return nil, errors.New("selfupdate: release archive has an unreadable hikyo.exe payload")
		}
	}
	if binary == nil {
		return nil, errors.New("selfupdate: release archive has no hikyo.exe payload")
	}
	return binary, nil
}
