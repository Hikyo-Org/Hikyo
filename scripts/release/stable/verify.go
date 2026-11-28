package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/gofrs/flock"
)

func verifyTrustOnly(t publicTrust, state string, output io.Writer) error {
	floor, unlock, err := lockFloor(state, t.snapshot.Floor())
	if err != nil {
		return err
	}
	defer unlock()
	if err := floor.Advance(t.snapshot.Floor()); err != nil {
		return err
	}
	if err := saveFloor(state, t.snapshot.Floor()); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "Authenticated stable keyless trust snapshot")
	return err
}

func verifyDirectory(t publicTrust, directory, version, commit, state string, latest, published, historical bool, output io.Writer) error {
	if directory == "" || version == "" || commit == "" {
		return errors.New("directory, expected version and expected commit are required")
	}
	if historical && latest {
		return errors.New("historical verification cannot select latest")
	}
	floor, unlock, err := lockFloor(state, t.snapshot.Floor())
	if err != nil {
		return err
	}
	defer unlock()

	var stable releasetrust.StableMaterial
	for name, dst := range map[string]*[]byte{
		"release-manifest.json": &stable.Manifest, "release-manifest.sigstore.json": &stable.ManifestSignature,
		"release-candidate.json": &stable.Candidate, "upgrade-compatibility.json": &stable.Compatibility,
	} {
		*dst, err = readDocument(directory, name)
		if err != nil {
			return err
		}
	}
	var manifest releasetrust.Manifest
	if err := definitions.DecodeStrict(stable.Manifest, &manifest); err != nil {
		return err
	}
	// This claim selects the required evidence; VerifyStable authenticates it
	// before any artifact is trusted or any persisted floor advances.
	keyless := manifest.SigningKeyID == releasetrust.StableWorkflowSigner
	if keyless {
		stable.Provenance, err = readDocument(directory, "build-provenance.json")
		if err != nil {
			return err
		}
		stable.ProvenanceSignature, err = readDocument(directory, "build-provenance.json.sigstore.json")
		if err != nil {
			return err
		}
	}
	material, snapshot := t.material, t.snapshot
	if !historical || keyless {
		for name, dst := range map[string]*[]byte{
			"metadata.json": &material.Metadata, "metadata.sigstore.json": &material.MetadataSignature,
			"catalog.json": &material.Catalog, "catalog.sigstore.json": &material.CatalogSignature,
		} {
			*dst, err = readDocument(directory, name)
			if err != nil {
				return err
			}
		}
		minimum := t.snapshot.Floor()
		if historical {
			minimum = releasetrust.SnapshotFloor{}
			// Authenticate historical envelopes under their original delegation;
			// current canonical trust still decides release authorization below.
			for name, dst := range map[string]*[]byte{
				"stable-policy.json": &material.StablePolicy, "stable-policy.sigstore.json": &material.StablePolicySignature,
				"stable-trusted-root.json": &material.StableTrustedRoot,
			} {
				*dst, err = readDocument(directory, name)
				if err != nil {
					return err
				}
			}
			material.NightlyPolicy = nil
		}
		embedded, err := releasetrust.VerifySnapshot(t.pinned, material, minimum)
		if err != nil {
			return err
		}
		if !historical {
			// New drafts may advance canonical trust. Historical bundles carry
			// authenticated old evidence, but current revocations remain authority.
			snapshot = embedded
		}
	}
	if err := floor.Advance(snapshot.Floor()); err != nil {
		return err
	}
	release, err := releasetrust.VerifyStable(snapshot, stable)
	if err != nil {
		return err
	}
	if release.Identity().Version != version || release.Identity().Commit != commit {
		return errors.New("verified release differs from expected version or source commit")
	}
	if latest {
		if err := releasetrust.RequireLatestStable(snapshot, release); err != nil {
			return err
		}
	}
	if err := verifyInventory(snapshot, release, directory, keyless); err != nil {
		return err
	}
	if published {
		if err := verifyPublishedOCI(snapshot, release, t.material.StableTrustedRoot); err != nil {
			return err
		}
	}
	if err := saveFloor(state, snapshot.Floor()); err != nil {
		return err
	}
	return jsonwire.Encode(output, struct {
		Identity           releaseidentity.Identity `json:"identity"`
		PublishedInventory bool                     `json:"published_oci_verified"`
	}{release.Identity(), published})
}

func verifyInventory(snapshot releasetrust.Snapshot, release releasetrust.VerifiedRelease, directory string, keyless bool) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	allowed := map[string]bool{
		"release-manifest.json": true, "release-manifest.sigstore.json": true,
	}
	if keyless {
		for _, name := range []string{"metadata.json", "metadata.sigstore.json", "catalog.json", "catalog.sigstore.json"} {
			allowed[name] = true
		}
	}
	artifacts := release.Artifacts()
	for _, artifact := range artifacts {
		if allowed[artifact.Name] {
			return errors.New("recursive or duplicated release envelope")
		}
		allowed[artifact.Name] = true
		if !keyless || (artifact.Name != "stable-policy.sigstore.json" && artifact.Name != "stable-policy.json") {
			allowed[artifact.Name+".sigstore.json"] = true
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	if len(entries) != len(allowed) {
		return errors.New("release download has missing or extra files")
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !allowed[entry.Name()] || !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected or nonregular release asset %s", entry.Name())
		}
	}
	var aggregate int64
	for _, artifact := range artifacts {
		file, err := root.Open(artifact.Name)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return err
		}
		aggregate += info.Size()
		if !info.Mode().IsRegular() || info.Size() > releasetrust.MaxArtifactBytes || aggregate > 16<<30 {
			file.Close()
			return errors.New("release artifact byte bound exceeded")
		}
		err = release.VerifyArtifact(artifact.Name, file)
		if err != nil {
			file.Close()
			return fmt.Errorf("%s: %w", artifact.Name, err)
		}
		if !keyless || (artifact.Name != "stable-policy.sigstore.json" && artifact.Name != "stable-policy.json") {
			signature, err := readDocument(directory, artifact.Name+".sigstore.json")
			if err != nil {
				file.Close()
				return err
			}
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				file.Close()
				return err
			}
			err = releasetrust.VerifyStableArtifactSignatureReader(snapshot, release, signature, file)
			if err != nil {
				file.Close()
				return fmt.Errorf("%s signature: %w", artifact.Name, err)
			}
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	if keyless {
		provenance, err := readDocument(directory, "build-provenance.json")
		if err != nil {
			return err
		}
		signature, err := readDocument(directory, "build-provenance.json.sigstore.json")
		if err != nil {
			return err
		}
		if err := releasetrust.VerifyStableProvenance(snapshot, release, signature, provenance); err != nil {
			return err
		}
	}
	return verifyReleaseLayout(directory, release, keyless)
}

func lockFloor(path string, current releasetrust.SnapshotFloor) (releasetrust.SnapshotFloor, func(), error) {
	var floor releasetrust.SnapshotFloor
	noop := func() {}
	if path == "" {
		return floor, noop, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return floor, noop, err
	}
	lock := flock.New(releasetrust.VerificationLockPath(path))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		_ = lock.Close()
		return floor, noop, errors.New("verification state is locked or unavailable")
	}
	unlock := func() { _ = lock.Unlock(); _ = lock.Close() }
	raw, err := readDocument(filepath.Dir(path), filepath.Base(path))
	if errors.Is(err, os.ErrNotExist) {
		return floor, unlock, nil
	}
	if err == nil {
		floor, err = releasetrust.DecodeVerificationFloor(raw, current)
	}
	if err != nil {
		unlock()
		return floor, noop, err
	}
	return floor, unlock, nil
}

func saveFloor(path string, floor releasetrust.SnapshotFloor) error {
	if path == "" {
		return nil
	}
	raw, err := marshal(floor)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".stable-verification-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = io.Copy(f, bytes.NewReader(raw))
	if err := errors.Join(err, f.Sync(), f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
