package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradeassembly"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// assembleNightlyBundle retains the already authenticated snapshot and fetches
// its complete bridge inventory. Discovery URLs cannot introduce new authority:
// the shared assembler independently verifies every staged byte before publish.
func (i *Installer) assembleNightlyBundle(ctx context.Context, nightly string, material releasetrust.SnapshotMaterial, snapshot releasetrust.Snapshot, pinned releasetrust.PinnedTrust, identity releaseidentity.Identity) (string, error) {
	return i.assembleNightlyEvidence(ctx, []PreparedNightly{{Directory: nightly, Identity: identity}}, material, snapshot, pinned)
}

func (i *Installer) assembleNightlyEvidence(ctx context.Context, evidence []PreparedNightly, material releasetrust.SnapshotMaterial, snapshot releasetrust.Snapshot, pinned releasetrust.PinnedTrust) (string, error) {
	for _, item := range evidence {
		if item.Identity.Profile != releaseidentity.NightlyV1 {
			return "", errors.New("selfupdate: nightly evidence requires nightly profile")
		}
	}
	return i.assembleReleaseEvidence(ctx, evidence, material, snapshot, pinned)
}

func (i *Installer) assembleReleaseEvidence(ctx context.Context, evidence []PreparedNightly, material releasetrust.SnapshotMaterial, snapshot releasetrust.Snapshot, pinned releasetrust.PinnedTrust) (string, error) {
	defer diagnostics.Time(ctx, "assemble nightly evidence")()
	if len(evidence) == 0 || len(evidence) > upgradecompat.MaxReleases {
		return "", errors.New("selfupdate: nightly route exceeds release bound")
	}
	identities := make([]string, 0, len(evidence))
	seen := map[releaseidentity.Digest]bool{}
	for _, item := range evidence {
		if item.Identity.Validate() != nil || seen[item.Identity.ManifestSHA256] {
			return "", errors.New("selfupdate: invalid or duplicate route release")
		}
		seen[item.Identity.ManifestSHA256] = true
		if err := realNightlyDirectory(item.Directory); err != nil {
			return "", err
		}
		var verified releasetrust.VerifiedRelease
		var err error
		switch item.Identity.Profile {
		case releaseidentity.NightlyV1:
			verified, err = upgradebundle.VerifyNightlyPlatformDirectory(ctx, item.Directory, snapshot, nightlyPlatform())
		case releaseidentity.StableV1:
			verified, err = verifyPreparedStableDirectory(item.Directory, snapshot)
		default:
			return "", errors.New("selfupdate: unsupported route profile")
		}
		if err != nil {
			return "", err
		}
		if verified.Identity() != item.Identity {
			return "", errors.New("selfupdate: route directory differs from requested identity")
		}
		identities = append(identities, string(item.Identity.ManifestSHA256))
	}
	slices.Sort(identities)
	routeDigest := releaseidentity.Hash([]byte(nightlyPlatform() + "\n" + strings.Join(identities, "\n")))
	destination := filepath.Join(i.config.StateDir, "bundle-"+string(routeDigest)+"-"+string(snapshot.Digest()))
	if _, err := os.Lstat(destination); err == nil {
		if err := realNightlyDirectory(destination); err != nil {
			return "", err
		}
		bundle, err := upgradebundle.Load(ctx, destination, pinned, snapshot.Floor())
		if err != nil {
			return "", err
		}
		for _, item := range evidence {
			if _, err := bundle.Release(item.Identity); err != nil {
				return "", err
			}
		}
		return destination, i.pruneOtherBundles(ctx, destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := i.pruneOtherBundles(ctx, destination); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(i.config.StateDir, ".nightly-bundle-inputs-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	write := func(name string, raw []byte) error {
		path := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		return os.WriteFile(path, raw, 0600)
	}
	for name, raw := range map[string][]byte{"metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature, "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature} {
		if err := write("snapshot/"+name, raw); err != nil {
			return "", err
		}
	}
	if len(material.StablePolicy) > 0 {
		for name, raw := range map[string][]byte{"stable-policy.json": material.StablePolicy, "stable-policy.sigstore.json": material.StablePolicySignature, "stable-trusted-root.json": material.StableTrustedRoot} {
			if err := write("snapshot/"+name, raw); err != nil {
				return "", err
			}
		}
	}
	var metadata releasetrust.Metadata
	if err := definitions.DecodeStrict(material.Metadata, &metadata); err != nil {
		return "", err
	}
	for _, key := range metadata.PrimaryKeys {
		if !releaseidentity.SafeName(key.PublicKey) {
			return "", errors.New("unsafe primary public key locator")
		}
		if err := write("keys/"+key.PublicKey, material.PrimaryKeys[key.ID]); err != nil {
			return "", err
		}
	}
	options := upgradeassembly.Options{Pinned: pinned, Floor: snapshot.Floor(), SnapshotDirectory: filepath.Join(stage, "snapshot"), KeysDirectory: filepath.Join(stage, "keys"), OutputDirectory: destination, NightlyPolicy: material.NightlyPolicy, NightlyPlatform: nightlyPlatform()}
	for _, item := range evidence {
		if item.Identity.Profile == releaseidentity.NightlyV1 {
			options.Nightlies = append(options.Nightlies, item.Directory)
			continue
		}
		// Stable assembly accepts a closed metadata directory. Keep executable
		// payloads in their verified download cache, outside that exact inventory.
		directory := "stable/" + string(item.Identity.ManifestSHA256)
		names := []string{"release-manifest.json", "release-manifest.sigstore.json", "release-candidate.json", "upgrade-compatibility.json"}
		if _, err := os.Lstat(filepath.Join(item.Directory, "build-provenance.json")); err == nil {
			names = append(names, "build-provenance.json", "build-provenance.json.sigstore.json")
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		for _, name := range names {
			raw, err := readNightlyFile(filepath.Join(item.Directory, name), releasetrust.MaxDocumentBytes)
			if err != nil {
				return "", err
			}
			if err := write(directory+"/"+name, raw); err != nil {
				return "", err
			}
		}
		options.Releases = append(options.Releases, filepath.Join(stage, filepath.FromSlash(directory)))
	}
	var bridgeBytes int
	for _, digest := range snapshot.BridgeDigests() {
		// VerifySnapshot has already validated each digest's closed encoding.
		directory := "bridges/" + string(digest)
		for _, name := range []string{"statement.json", "statement.sigstore.json"} {
			raw, err := i.downloadURL(ctx, trustURL(directory+"/"+name), maxTrustBytes)
			if err != nil {
				return "", fmt.Errorf("download bridge %s: %w", digest, err)
			}
			bridgeBytes += len(raw)
			if bridgeBytes > 64<<20 {
				return "", errors.New("bridge inventory exceeds byte bound")
			}
			if err := write(directory+"/"+name, raw); err != nil {
				return "", err
			}
		}
		options.Bridges = append(options.Bridges, filepath.Join(stage, filepath.FromSlash(directory)))
	}
	if err := upgradeassembly.Assemble(ctx, options); err != nil {
		return "", err
	}
	return destination, nil
}
