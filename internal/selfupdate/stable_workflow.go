package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

// The legacy stable installer path keeps its durable metadata floor. Delegated
// releases use the same shared snapshot/manifest verifier as offline upgrades.
func (i *Installer) verifyStableWorkflow(ctx context.Context, status updatecheck.Status, archiveName string, archive []byte, pinned releasetrust.PinnedTrust) error {
	material, baseline, err := i.downloadSnapshot(ctx, pinned, releaseidentity.SnapshotFloor{}, false)
	if err != nil {
		return err
	}
	floor := baseline.Floor()
	statePath := filepath.Join(i.config.StateDir, "release-trust.json")
	if current, readErr := os.ReadFile(statePath); readErr == nil {
		previous, err := releasetrust.DecodeVerificationFloor(current, baseline.Floor())
		if err != nil {
			return err
		}
		if previous.MetadataSequence > floor.MetadataSequence {
			floor.MetadataSequence, floor.MetadataSHA256 = previous.MetadataSequence, previous.MetadataSHA256
		} else if previous.MetadataSequence == floor.MetadataSequence && previous.MetadataSHA256 != floor.MetadataSHA256 {
			return errors.New("selfupdate: canonical metadata equivocates at persisted sequence")
		}
		if previous.CatalogSequence > floor.CatalogSequence {
			floor.CatalogSequence, floor.CatalogSHA256 = previous.CatalogSequence, previous.CatalogSHA256
		} else if previous.CatalogSequence == floor.CatalogSequence && previous.CatalogSHA256 != floor.CatalogSHA256 {
			return errors.New("selfupdate: canonical catalog equivocates at persisted sequence")
		}
		if previous.HighestReleaseSequence > floor.HighestReleaseSequence {
			floor.HighestReleaseSequence = previous.HighestReleaseSequence
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	for name, target := range map[string]*[]byte{"metadata.json": &material.Metadata, "metadata.sigstore.json": &material.MetadataSignature, "catalog.json": &material.Catalog, "catalog.sigstore.json": &material.CatalogSignature} {
		*target, err = i.downloadNamedAsset(ctx, status, name, maxTrustBytes)
		if err != nil {
			return err
		}
	}
	snapshot, err := releasetrust.VerifySnapshot(pinned, material, floor)
	if err != nil {
		return err
	}
	documents := map[string][]byte{}
	for _, name := range []string{"release-manifest.json", "release-manifest.sigstore.json", "release-candidate.json", releasetrust.CompatibilityArtifact, "build-provenance.json", "build-provenance.json.sigstore.json", archiveName + ".sigstore.json"} {
		documents[name], err = i.downloadNamedAsset(ctx, status, name, maxTrustBytes)
		if err != nil {
			return err
		}
	}
	release, err := releasetrust.VerifyStable(snapshot, releasetrust.StableMaterial{Manifest: documents["release-manifest.json"], ManifestSignature: documents["release-manifest.sigstore.json"], Candidate: documents["release-candidate.json"], Compatibility: documents[releasetrust.CompatibilityArtifact], Provenance: documents["build-provenance.json"], ProvenanceSignature: documents["build-provenance.json.sigstore.json"]})
	if err != nil {
		return err
	}
	if release.Identity().Version != status.LatestVersion {
		return errors.New("selfupdate: authenticated release differs from selected version")
	}
	if err := releasetrust.RequireLatestStable(snapshot, release); err != nil {
		return err
	}
	binary := false
	for _, artifact := range release.Artifacts() {
		if artifact.Name == archiveName && artifact.Kind == "binary" {
			binary = true
		}
	}
	if !binary {
		return errors.New("selfupdate: selected archive is not an authenticated binary")
	}
	if err := release.VerifyArtifact(archiveName, bytes.NewReader(archive)); err != nil {
		return err
	}
	if err := releasetrust.VerifyStableArtifactSignature(snapshot, release, documents[archiveName+".sigstore.json"], archive); err != nil {
		return err
	}
	return updateVerificationState(statePath, snapshot.Floor())
}
