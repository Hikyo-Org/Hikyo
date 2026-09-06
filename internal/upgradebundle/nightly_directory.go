package upgradebundle

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// VerifyNightlyDirectory authenticates a complete flat GitHub release download.
// Only the manifest and its signature are excluded from the signed inventory.
// Policy and Sigstore roots are payloads too; the recovery-signed snapshot must
// independently authorize their exact policy digest.
func VerifyNightlyDirectory(ctx context.Context, directory string, snapshot releasetrust.Snapshot) (releasetrust.VerifiedRelease, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	defer root.Close()
	r := documentReader{ctx: ctx, root: root}
	release, _, err := r.flatNightly(snapshot)
	return release, err
}

func (r *documentReader) flatNightly(snapshot releasetrust.Snapshot) (releasetrust.VerifiedRelease, map[string][]byte, error) {
	documents := map[string][]byte{}
	for _, name := range []string{"release-manifest.json", "release-manifest.sigstore.json", "nightly-policy.json", "sigstore-trusted-root.json", "upgrade-compatibility.json"} {
		raw, err := r.read(name)
		if err != nil {
			return releasetrust.VerifiedRelease{}, nil, err
		}
		documents[name] = raw
	}
	assets, closers, err := r.payloads(".")
	if err != nil {
		return releasetrust.VerifiedRelease{}, nil, err
	}
	defer func() {
		for _, closer := range closers {
			closer.Close()
		}
	}()
	delete(assets, "release-manifest.json")
	delete(assets, "release-manifest.sigstore.json")
	release, err := releasetrust.VerifyNightly(snapshot, releasetrust.NightlyMaterial{
		Manifest: documents["release-manifest.json"], Bundle: documents["release-manifest.sigstore.json"],
		Policy: documents["nightly-policy.json"], TrustedRoot: documents["sigstore-trusted-root.json"],
		Compatibility: documents["upgrade-compatibility.json"], Artifacts: assets,
	})
	return release, documents, err
}

// CopyNightlyRelease writes beneath a private assembler staging directory.
// Each copied payload is rehashed through the verified envelope. The caller
// authenticates the complete staged bundle before atomic public publication.
func CopyNightlyRelease(ctx context.Context, directory, releasesDirectory string, snapshot releasetrust.Snapshot) (releasetrust.VerifiedRelease, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	defer root.Close()
	r := documentReader{ctx: ctx, root: root}
	release, documents, err := r.flatNightly(snapshot)
	if err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	destination := filepath.Join(releasesDirectory, string(release.Identity().ManifestSHA256))
	if err := os.MkdirAll(releasesDirectory, 0700); err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	for source, target := range map[string]string{
		"release-manifest.json": "manifest.json", "release-manifest.sigstore.json": "manifest.sigstore.json",
		"nightly-policy.json": "policy.json", "sigstore-trusted-root.json": "trusted-root.json", "upgrade-compatibility.json": "upgrade-compatibility.json",
	} {
		file, err := os.OpenFile(filepath.Join(destination, target), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return releasetrust.VerifiedRelease{}, err
		}
		_, err = file.Write(documents[source])
		if err := errors.Join(err, file.Sync(), file.Close()); err != nil {
			return releasetrust.VerifiedRelease{}, err
		}
	}
	if err := os.Mkdir(filepath.Join(destination, "payloads"), 0700); err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	var copied int64
	for _, artifact := range release.Artifacts() {
		input := &payloadReader{ctx: ctx, root: root, name: artifact.Name, aggregate: &copied}
		output, err := os.OpenFile(filepath.Join(destination, "payloads", artifact.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return releasetrust.VerifiedRelease{}, err
		}
		err = release.VerifyArtifact(artifact.Name, io.TeeReader(input, output))
		err = errors.Join(err, input.Close(), output.Sync(), output.Close())
		if err != nil {
			return releasetrust.VerifiedRelease{}, err
		}
	}
	return release, nil
}
