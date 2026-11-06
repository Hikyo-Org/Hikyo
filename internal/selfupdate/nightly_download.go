package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

func nightlyPlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }

func nightlyCacheDirectory(identity releaseidentity.Identity) string {
	return "nightly-" + string(identity.ManifestSHA256) + "-" + runtime.GOOS + "-" + runtime.GOARCH
}

// CachedReleaseDirectory is where the verified cache under stateDir keeps one release.
func CachedReleaseDirectory(stateDir string, identity releaseidentity.Identity) string {
	if identity.Profile == releaseidentity.NightlyV1 {
		return filepath.Join(stateDir, nightlyCacheDirectory(identity))
	}
	return filepath.Join(stateDir, "stable-"+string(identity.ManifestSHA256)+"-"+runtime.GOOS+"-"+runtime.GOARCH)
}

// Authenticate metadata before selecting any executable payload. Discovery
// must match the entire signed inventory, but only native bytes are fetched.
func (i *Installer) downloadNightlyPlatform(ctx context.Context, status updatecheck.Status, stage string, cached bool, snapshot releasetrust.Snapshot) (releasetrust.VerifiedRelease, error) {
	documents := map[string][]byte{}
	var total int64
	read := func(name string, limit int64) ([]byte, error) {
		asset, err := exactAsset(status.LatestVersion, name, status.Assets)
		if err != nil {
			return nil, err
		}
		total += asset.Size
		if asset.Size > limit || total > 8<<30 {
			return nil, errors.New("selfupdate: nightly payload inventory exceeds byte bound")
		}
		if cached {
			raw, err := readNightlyFile(filepath.Join(stage, name), limit)
			if err == nil && (int64(len(raw)) != asset.Size || "sha256:"+string(releaseidentity.Hash(raw)) != asset.Digest) {
				err = errors.New("selfupdate: cached nightly differs from immutable release asset inventory")
			}
			return raw, err
		}
		diagnostics.Printf(ctx, 2, "    %s (%s)", name, mebibytes(asset.Size))
		raw, err := i.download(ctx, asset, limit)
		if err != nil {
			return nil, err
		}
		file, err := os.OpenFile(filepath.Join(stage, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		_, err = file.Write(raw)
		return raw, errors.Join(err, file.Sync(), file.Close())
	}
	for _, name := range []string{"release-manifest.json", "release-manifest.sigstore.json", "nightly-policy.json", "sigstore-trusted-root.json", "upgrade-compatibility.json"} {
		raw, err := read(name, releasetrust.MaxDocumentBytes)
		if err != nil {
			return releasetrust.VerifiedRelease{}, err
		}
		documents[name] = raw
	}
	release, err := releasetrust.VerifyNightlyManifest(snapshot, releasetrust.NightlyMaterial{
		Manifest: documents["release-manifest.json"], Bundle: documents["release-manifest.sigstore.json"],
		Policy: documents["nightly-policy.json"], TrustedRoot: documents["sigstore-trusted-root.json"], Compatibility: documents["upgrade-compatibility.json"],
	})
	if err != nil {
		return releasetrust.VerifiedRelease{}, fmt.Errorf("selfupdate: authenticate nightly manifest: %w", err)
	}
	if release.Identity().Version != status.LatestVersion || len(status.Assets) != len(release.Artifacts())+2 {
		return releasetrust.VerifiedRelease{}, errors.New("selfupdate: discovery differs from signed nightly inventory")
	}
	for _, artifact := range release.Artifacts() {
		asset, err := exactAsset(status.LatestVersion, artifact.Name, status.Assets)
		if err != nil {
			return releasetrust.VerifiedRelease{}, err
		}
		if asset.Digest != "sha256:"+artifact.SHA256 {
			return releasetrust.VerifiedRelease{}, errors.New("selfupdate: discovery digest differs from signed nightly inventory")
		}
	}
	selected, err := release.NightlyArtifacts(nightlyPlatform())
	if err != nil {
		return releasetrust.VerifiedRelease{}, err
	}
	diagnostics.Printf(ctx, 1, "  Preparing nightly %s for %s: %d selected assets.", status.LatestVersion, nightlyPlatform(), len(selected)+2)
	for _, artifact := range selected {
		if _, have := documents[artifact.Name]; have {
			continue
		}
		if _, err := read(artifact.Name, maxArchiveBytes); err != nil {
			return releasetrust.VerifiedRelease{}, err
		}
	}
	return upgradebundle.VerifyNightlyPlatformDirectory(ctx, stage, snapshot, nightlyPlatform())
}
