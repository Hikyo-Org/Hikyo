package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// All inputs are a closed inventory of bounded, flat public documents. Root
// confinement and no-follow opens also refuse a symlink substituted after listing.
func openDirectory(path string) (*os.Root, error) {
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("input is not a real directory: %s", path)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, errors.New("input directory changed while opening")
	}
	return root, nil
}

func readMember(root *os.Root, name string) ([]byte, error) {
	if !safeName(name) {
		return nil, errors.New("unsafe input member")
	}
	file, err := openDocument(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > releasetrust.MaxDocumentBytes {
		return nil, fmt.Errorf("input is not a bounded regular document: %s", name)
	}
	raw, err := io.ReadAll(io.LimitReader(file, releasetrust.MaxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > releasetrust.MaxDocumentBytes {
		return nil, errors.New("document exceeds byte bound")
	}
	return raw, nil
}

func readPath(path string) ([]byte, error) {
	root, err := openDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readMember(root, filepath.Base(path))
}

func readExact(directory string, names []string) (map[string][]byte, error) {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		if !safeName(name) || wanted[name] {
			return nil, errors.New("unsafe or duplicate input member")
		}
		wanted[name] = true
	}
	root, err := openDirectory(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	listing, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer listing.Close()
	entries, err := listing.ReadDir(len(names) + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) != len(names) {
		return nil, fmt.Errorf("incomplete or extra members in %s", directory)
	}
	files := make(map[string][]byte, len(names))
	total := 0
	for _, entry := range entries {
		if !wanted[entry.Name()] || entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unexpected or symlink member: %s", entry.Name())
		}
		raw, err := readMember(root, entry.Name())
		if err != nil {
			return nil, err
		}
		total += len(raw)
		if total > 64<<20 {
			return nil, errors.New("input inventory exceeds aggregate document bound")
		}
		files[entry.Name()] = raw
	}
	return files, nil
}

func readRelease(ctx context.Context, directory string, snapshot releasetrust.Snapshot) (releasetrust.VerifiedRelease, map[string][]byte, error) {
	if err := ctx.Err(); err != nil {
		return releasetrust.VerifiedRelease{}, nil, err
	}
	files, err := readExact(directory, []string{"release-manifest.json", "release-manifest.sigstore.json", "release-candidate.json", "upgrade-compatibility.json"})
	if err != nil {
		return releasetrust.VerifiedRelease{}, nil, err
	}
	release, err := releasetrust.VerifyStable(snapshot, releasetrust.StableMaterial{
		Manifest: files["release-manifest.json"], ManifestSignature: files["release-manifest.sigstore.json"],
		Candidate: files["release-candidate.json"], Compatibility: files["upgrade-compatibility.json"],
	})
	if err != nil {
		return releasetrust.VerifiedRelease{}, nil, fmt.Errorf("authenticate release proofs: %w", err)
	}
	return release, map[string][]byte{
		"manifest.json": files["release-manifest.json"], "manifest.sigstore.json": files["release-manifest.sigstore.json"],
		"release-candidate.json": files["release-candidate.json"], "upgrade-compatibility.json": files["upgrade-compatibility.json"],
	}, nil
}
