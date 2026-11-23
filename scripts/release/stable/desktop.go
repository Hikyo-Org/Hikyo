package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Masterminds/semver/v3"
)

const (
	desktopInventorySchema = "hikyo.dev/desktop-inventory/v1"
	desktopInventoryName   = "desktop-inventory.json"
	desktopBundleName      = "desktop-inventory.sigstore.json"
)

// desktopInventory is the signed list of a desktop-v<version> release's files.
type desktopInventory struct {
	Schema       string        `json:"schema"`
	Version      string        `json:"version"`
	SourceCommit string        `json:"source_commit"`
	Tag          string        `json:"tag"`
	CLITag       string        `json:"cli_tag"`
	Files        []desktopFile `json:"files"`
}

type desktopFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// desktopImageNames is the closed set of disk images a desktop release holds.
func desktopImageNames(version string) []string {
	return []string{
		"Hikyo-Desktop-" + version + "-arm64.dmg",
		"Hikyo-Desktop-" + version + "-x86_64.dmg",
	}
}

// verifyDesktop authenticates a downloaded desktop-v<version> draft or release.
// The inventory is verified against the pinned trust before any of its fields are
// believed; it then closes the directory to exactly the two disk images, the
// inventory and its bundle, and binds each image's size and SHA-256.
func verifyDesktop(t publicTrust, directory, version, commit string, output io.Writer) error {
	if directory == "" || version == "" || commit == "" {
		return errors.New("directory, expected version and expected commit are required")
	}
	parsed, err := semver.StrictNewVersion(version)
	if err != nil || parsed.Prerelease() != "" || parsed.Metadata() != "" || parsed.Major() < 1 {
		return errors.New("desktop releases exist only for stable versions")
	}
	images := desktopImageNames(version)
	want := append([]string{desktopInventoryName, desktopBundleName}, images...)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var got []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return fmt.Errorf("desktop release entry %q is not a regular file", entry.Name())
		}
		got = append(got, entry.Name())
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		return fmt.Errorf("desktop release must hold exactly %v, found %v", want, got)
	}
	raw, err := readDocument(directory, desktopInventoryName)
	if err != nil {
		return err
	}
	bundle, err := readDocument(directory, desktopBundleName)
	if err != nil {
		return err
	}
	if err := t.snapshot.VerifyStableWorkflowBlob(version, commit, bundle, raw); err != nil {
		return fmt.Errorf("desktop inventory signature: %w", err)
	}
	var inventory desktopInventory
	if err := definitions.DecodeStrict(raw, &inventory); err != nil {
		return err
	}
	if inventory.Schema != desktopInventorySchema || inventory.Version != version || inventory.SourceCommit != commit ||
		inventory.Tag != "desktop-v"+version || inventory.CLITag != "v"+version {
		return errors.New("desktop inventory identity differs from the expected version, commit or tags")
	}
	listed := make([]string, 0, len(inventory.Files))
	for _, file := range inventory.Files {
		listed = append(listed, file.Name)
	}
	slices.Sort(listed)
	imageSet := slices.Clone(images)
	slices.Sort(imageSet)
	if !slices.Equal(listed, imageSet) {
		return fmt.Errorf("desktop inventory must list exactly %v", imageSet)
	}
	for _, file := range inventory.Files {
		if file.Size <= 0 || releaseidentity.Digest(file.SHA256).Validate() != nil {
			return fmt.Errorf("desktop inventory entry %q has an invalid size or digest", file.Name)
		}
		if err := verifyDesktopImage(filepath.Join(directory, file.Name), file); err != nil {
			return err
		}
	}
	return jsonwire.Encode(output, struct {
		Version string   `json:"version"`
		Commit  string   `json:"source_commit"`
		Files   []string `json:"files"`
	}{version, commit, imageSet})
}

func verifyDesktopImage(path string, file desktopFile) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // read-only: a close error loses nothing
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, file.Size+1))
	if err != nil {
		return err
	}
	if n != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return fmt.Errorf("desktop image %q differs from the signed inventory", file.Name)
	}
	return nil
}
