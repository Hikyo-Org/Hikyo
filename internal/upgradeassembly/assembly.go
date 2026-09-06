// Package upgradeassembly authenticates public release evidence and durably
// publishes the exact offline bundle consumed by the mandatory runtime gate.
package upgradeassembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// Options identifies a closed set of public evidence directories. Pinned must
// come from an independently trusted source. Floor is the installation's durable
// trust floor; assembly never advances it. OutputDirectory must not exist and its
// parent must already exist. Inputs are read with bounded, no-follow semantics.
type Options struct {
	Pinned            releasetrust.PinnedTrust
	Floor             releaseidentity.SnapshotFloor
	SnapshotDirectory string
	KeysDirectory     string
	OutputDirectory   string
	Releases          []string
	Nightlies         []string
	Bridges           []string
}

// Assemble verifies all input evidence before atomically publishing a new
// runtime bundle. It never signs evidence, replaces output or changes trust state.
func Assemble(ctx context.Context, o Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.SnapshotDirectory == "" || o.KeysDirectory == "" || o.OutputDirectory == "" || len(o.Releases)+len(o.Nightlies) == 0 || len(o.Releases)+len(o.Nightlies) > upgradecompat.MaxReleases || len(o.Bridges) > upgradecompat.MaxEdges {
		return errors.New("require snapshot, keys, output and releases with bounded inventories")
	}
	pinned, floor := o.Pinned, o.Floor
	snapshotFiles, err := readExact(o.SnapshotDirectory, []string{"metadata.json", "metadata.sigstore.json", "catalog.json", "catalog.sigstore.json"})
	if err != nil {
		return err
	}
	var metadata releasetrust.Metadata
	if err := definitions.DecodeStrict(snapshotFiles["metadata.json"], &metadata); err != nil {
		return err
	}
	if len(metadata.PrimaryKeys) == 0 || len(metadata.PrimaryKeys) > 256 {
		return errors.New("invalid primary key count")
	}
	names := []string{}
	index := upgradebundle.Index{Format: upgradebundle.IndexFormat, PrimaryKeyIDs: []string{}, Releases: []upgradebundle.ReleaseEntry{}, Bridges: []releaseidentity.Digest{}}
	idPattern := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	for _, key := range metadata.PrimaryKeys {
		if !safeName(key.PublicKey) || !idPattern.MatchString(key.ID) {
			return errors.New("unsafe primary key locator")
		}
		names = append(names, key.PublicKey)
		index.PrimaryKeyIDs = append(index.PrimaryKeyIDs, key.ID)
	}
	keyFiles, err := readExact(o.KeysDirectory, names)
	if err != nil {
		return err
	}
	keys := map[string][]byte{}
	for _, key := range metadata.PrimaryKeys {
		keys[key.ID] = keyFiles[key.PublicKey]
	}
	material := releasetrust.SnapshotMaterial{Metadata: snapshotFiles["metadata.json"], MetadataSignature: snapshotFiles["metadata.sigstore.json"], Catalog: snapshotFiles["catalog.json"], CatalogSignature: snapshotFiles["catalog.sigstore.json"], PrimaryKeys: keys}
	snapshot, err := releasetrust.VerifySnapshot(pinned, material, floor)
	if err != nil {
		return fmt.Errorf("authenticate trust snapshot: %w", err)
	}
	output, err := filepath.Abs(o.OutputDirectory)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		return errors.New("output already exists or cannot be inspected")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return err
	}
	output = filepath.Join(parent, filepath.Base(output))
	stage, err := os.MkdirTemp(parent, ".hikyo-upgrade-assembly-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	var stagedBytes int
	write := func(name string, raw []byte) error {
		stagedBytes += len(raw)
		if stagedBytes > 64<<20 {
			return errors.New("bundle exceeds aggregate document bound")
		}
		return writeDocument(stage, name, raw)
	}
	for name, raw := range snapshotFiles {
		if err := write(name, raw); err != nil {
			return err
		}
	}
	for id, raw := range keys {
		if err := write("keys/"+id+".pub", raw); err != nil {
			return err
		}
	}
	seen := map[releaseidentity.Digest]bool{}
	for _, directory := range o.Releases {
		release, files, err := readRelease(ctx, directory, snapshot)
		if err != nil {
			return err
		}
		digest := release.Identity().ManifestSHA256
		if seen[digest] {
			return errors.New("duplicate release manifest")
		}
		seen[digest] = true
		index.Releases = append(index.Releases, upgradebundle.ReleaseEntry{Profile: releaseidentity.StableV1, ManifestSHA256: digest})
		for name, raw := range files {
			if err := write("releases/"+string(digest)+"/"+name, raw); err != nil {
				return err
			}
		}
	}
	for _, directory := range o.Nightlies {
		release, err := upgradebundle.CopyNightlyRelease(ctx, directory, filepath.Join(stage, "releases"), snapshot)
		if err != nil {
			return err
		}
		digest := release.Identity().ManifestSHA256
		if seen[digest] {
			return errors.New("duplicate release manifest")
		}
		seen[digest] = true
		index.Releases = append(index.Releases, upgradebundle.ReleaseEntry{Profile: releaseidentity.NightlyV1, ManifestSHA256: digest})
	}
	seen = map[releaseidentity.Digest]bool{}
	for _, directory := range o.Bridges {
		files, err := readExact(directory, []string{"statement.json", "statement.sigstore.json"})
		if err != nil {
			return err
		}
		digest := releaseidentity.Hash(files["statement.json"])
		if seen[digest] {
			return errors.New("duplicate bridge statement")
		}
		seen[digest] = true
		index.Bridges = append(index.Bridges, digest)
		for name, raw := range files {
			if err := write("bridges/"+string(digest)+"/"+name, raw); err != nil {
				return err
			}
		}
	}
	slices.Sort(index.PrimaryKeyIDs)
	slices.Sort(index.Bridges)
	slices.SortFunc(index.Releases, func(a, b upgradebundle.ReleaseEntry) int {
		return strings.Compare(string(a.ManifestSHA256), string(b.ManifestSHA256))
	})
	raw, err := json.Marshal(index)
	if err != nil {
		return err
	}
	if err := write("index.json", raw); err != nil {
		return err
	}
	// This final pass authenticates the exact staged bytes and requires every
	// bridge in the current catalog, using the same reader as production boot.
	if _, err := upgradebundle.Load(ctx, stage, pinned, floor); err != nil {
		return fmt.Errorf("validate assembled runtime bundle: %w", err)
	}
	if err := syncTree(stage); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := publishDirectory(stage, output); err != nil {
		return err
	}
	ancestry, err := filedurability.DirectoryAncestry(parent)
	if err != nil {
		return fmt.Errorf("bundle published at %s; durability unconfirmed: %w", output, err)
	}
	for _, directory := range ancestry {
		if err := filedurability.SyncDirectory(directory); err != nil {
			return fmt.Errorf("bundle published at %s; durability unconfirmed: %w", output, err)
		}
	}
	return nil
}

func safeName(name string) bool {
	return name != "" && name != "." && !strings.Contains(name, "..") && !strings.ContainsAny(name, `/\`)
}

func writeDocument(directory, name string, raw []byte) error {
	path := filepath.Join(directory, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(raw)
	return errors.Join(writeErr, file.Sync(), file.Close())
}

func syncTree(directory string) error {
	var directories []string
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	slices.Reverse(directories)
	for _, path := range directories {
		if err := filedurability.SyncDirectory(path); err != nil {
			return err
		}
	}
	return nil
}
