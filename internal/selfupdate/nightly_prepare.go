package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Masterminds/semver/v3"
)

// PreparedNightly is authenticated public evidence and a separately extracted
// executable. Preparation never replaces the installed executable or admits a
// database upgrade; the host controller and runtime gate perform those steps.
type PreparedNightly struct {
	Identity        releaseidentity.Identity
	Directory       string
	BundleDirectory string
	BinaryPath      string
	BinarySHA256    releaseidentity.Digest
}

// PrepareNightly authenticates a selected immutable nightly, assembles its
// runtime evidence and extracts this platform's binary without installing it.
// A target older than the highest observed nightly is refused.
func (i *Installer) PrepareNightly(ctx context.Context, status updatecheck.Status) (PreparedNightly, error) {
	return i.prepareSelectedNightly(ctx, status, nil)
}

// PrepareNightlySource fetches an exact source or route-hop identity selected
// from authenticated compatibility evidence. Older evidence is allowed, but
// the durable highest observed nightly and snapshot floor never decrease.
// The caller must still prove a route before executing any prepared binary.
func (i *Installer) PrepareNightlySource(ctx context.Context, status updatecheck.Status, expected releaseidentity.Identity) (PreparedNightly, error) {
	if expected.Validate() != nil || expected.Profile != releaseidentity.NightlyV1 {
		return PreparedNightly{}, errors.New("selfupdate: exact nightly source identity is required")
	}
	return i.prepareSelectedNightly(ctx, status, &expected)
}

func (i *Installer) prepareSelectedNightly(ctx context.Context, status updatecheck.Status, expected *releaseidentity.Identity) (PreparedNightly, error) {
	if i == nil || i.client == nil {
		return PreparedNightly{}, errors.New("selfupdate: installer is not configured")
	}
	version, err := semver.StrictNewVersion(status.LatestVersion)
	if err != nil || version.Prerelease() == "" || !status.Prerelease || status.Channel != updatecheck.ChannelNightly || (expected == nil && !status.Available) {
		return PreparedNightly{}, errors.New("selfupdate: select an available nightly release")
	}
	var prepared PreparedNightly
	if err := i.prepareNightly(ctx, status, expected, true, &prepared); err != nil {
		return PreparedNightly{}, err
	}
	return prepared, nil
}

func (i *Installer) extractPreparedBinary(ctx context.Context, directory string, release releasetrust.VerifiedRelease) (string, releaseidentity.Digest, error) {
	name, err := archiveName(release.Identity().Version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", "", err
	}
	matched := false
	for _, artifact := range release.Artifacts() {
		if artifact.Name == name && artifact.Kind == "binary" && artifact.Platform == runtime.GOOS+"/"+runtime.GOARCH {
			matched = true
		}
	}
	if !matched {
		return "", "", errors.New("selfupdate: no authenticated binary for this platform")
	}
	archive, err := readNightlyFile(filepath.Join(directory, name), maxArchiveBytes)
	if err != nil {
		return "", "", err
	}
	if err := release.VerifyArtifact(name, bytes.NewReader(archive)); err != nil {
		return "", "", err
	}
	binary, err := extractNightlyBinary(name, archive)
	if err != nil {
		return "", "", err
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	digest := releaseidentity.Hash(binary)
	destination := filepath.Join(i.config.StateDir, "executable-"+string(release.Identity().ManifestSHA256)+"-"+runtime.GOOS+"-"+runtime.GOARCH)
	if _, err := os.Lstat(destination); err == nil {
		raw, err := readNightlyFile(destination, maxBinaryBytes)
		if err != nil || releaseidentity.Hash(raw) != digest {
			return "", "", errors.New("selfupdate: cached executable differs from authenticated archive")
		}
		return destination, digest, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	file, err := os.CreateTemp(i.config.StateDir, ".nightly-executable-")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(binary)
	if err = errors.Join(err, file.Chmod(0700), file.Sync(), file.Close()); err != nil {
		return "", "", err
	}
	if err := os.Link(file.Name(), destination); err != nil {
		return "", "", err
	}
	if err := filedurability.SyncDirectory(i.config.StateDir); err != nil {
		return "", "", err
	}
	return destination, digest, nil
}

func readNightlyFile(path string, limit int64) ([]byte, error) {
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("selfupdate: expected real nightly directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("selfupdate: nightly directory changed while opening")
	}
	file, err := openNightlyFile(root, filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, errors.New("selfupdate: expected bounded regular nightly file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || int64(len(raw)) > limit {
		return nil, fmt.Errorf("selfupdate: nightly file exceeds %d byte bound", limit)
	}
	return raw, nil
}

func realNightlyDirectory(path string) error {
	info, err := os.Lstat(filepath.Clean(path))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("selfupdate: expected real nightly directory")
	}
	return nil
}
