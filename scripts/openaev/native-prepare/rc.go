package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradeassembly"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

const trustBase = "https://raw.githubusercontent.com/Hikyo-Org/Hikyo/refs/heads/main/release/trust/"
const maxBinaryBytes = 128 << 20

type fetcher func(context.Context, string, int64) ([]byte, error)

func download(client *http.Client) fetcher {
	return func(ctx context.Context, url string, limit int64) ([]byte, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, errors.New("release download unavailable")
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 || int64(len(raw)) > limit {
			return nil, errors.New("release download exceeds byte bound")
		}
		return raw, nil
	}
}

func prepareRC(ctx context.Context, release updatecheck.Release, commit, state string, pinned releasetrust.PinnedTrust) (selfupdate.PreparedNightly, error) {
	client, err := updatecheck.NewDownloadHTTPClient(60 * time.Second)
	if err != nil {
		return selfupdate.PreparedNightly{}, err
	}
	return prepareRCWithFetcher(ctx, release, commit, state, pinned, download(client), runtime.GOOS+"/"+runtime.GOARCH)
}

func prepareRCWithFetcher(ctx context.Context, selected updatecheck.Release, commit, state string, pinned releasetrust.PinnedTrust, fetch fetcher, platform string) (selfupdate.PreparedNightly, error) {
	var empty selfupdate.PreparedNightly
	if platform != "linux/arm64" {
		return empty, errors.New("hosted RC preparation requires native Linux ARM64")
	}
	material, snapshot, err := fetchSnapshot(ctx, pinned, fetch)
	if err != nil {
		return empty, err
	}
	documents := map[string][]byte{}
	read := func(name string, limit int64) ([]byte, error) {
		raw, err := fetchAsset(ctx, selected, name, limit, fetch)
		if err == nil {
			documents[name] = raw
		}
		return raw, err
	}
	var stable releasetrust.StableMaterial
	for name, target := range map[string]*[]byte{"release-manifest.json": &stable.Manifest, "release-manifest.sigstore.json": &stable.ManifestSignature, "release-candidate.json": &stable.Candidate, "upgrade-compatibility.json": &stable.Compatibility} {
		*target, err = read(name, releasetrust.MaxDocumentBytes)
		if err != nil {
			return empty, err
		}
	}
	var claim releasetrust.Manifest
	if err := definitions.DecodeStrict(stable.Manifest, &claim); err != nil {
		return empty, err
	}
	if claim.SigningKeyID == releasetrust.StableWorkflowSigner {
		stable.Provenance, err = read("build-provenance.json", releasetrust.MaxDocumentBytes)
		if err != nil {
			return empty, err
		}
		stable.ProvenanceSignature, err = read("build-provenance.json.sigstore.json", releasetrust.MaxDocumentBytes)
		if err != nil {
			return empty, err
		}
	}
	verified, err := releasetrust.VerifyStable(snapshot, stable)
	if err != nil {
		return empty, err
	}
	identity := verified.Identity()
	if identity.Version != selected.Version || identity.Commit != commit {
		return empty, errors.New("signed RC identity mismatch")
	}
	for _, artifact := range verified.Artifacts() {
		asset, err := exactAsset(selected, artifact.Name)
		if err != nil || asset.Digest != "sha256:"+artifact.SHA256 {
			return empty, errors.New("RC discovery differs from signed inventory")
		}
	}
	archiveName := "hikyo_" + selected.Version + "_Linux_arm64.tar.gz"
	matched := false
	for _, artifact := range verified.Artifacts() {
		if artifact.Name == archiveName && artifact.Kind == "binary" && (artifact.Platform == "" || artifact.Platform == platform) {
			matched = true
		}
	}
	if !matched {
		return empty, errors.New("missing signed Linux ARM64 executable")
	}
	archive, err := fetchAsset(ctx, selected, archiveName, maxBinaryBytes, fetch)
	if err != nil {
		return empty, err
	}
	if err := verified.VerifyArtifact(archiveName, bytes.NewReader(archive)); err != nil {
		return empty, err
	}
	signature, err := fetchAsset(ctx, selected, archiveName+".sigstore.json", releasetrust.MaxDocumentBytes, fetch)
	if err != nil {
		return empty, err
	}
	if err := releasetrust.VerifyStableArtifactSignature(snapshot, verified, signature, archive); err != nil {
		return empty, err
	}
	binary, err := extractARM64(archive)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	inputs := filepath.Join(state, "rc-inputs")
	write := func(relative string, raw []byte) error {
		path := filepath.Join(inputs, relative)
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
	for name, raw := range map[string][]byte{"metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature, "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature} {
		if err := write("snapshot/"+name, raw); err != nil {
			return empty, err
		}
	}
	if len(material.StablePolicy) > 0 {
		for name, raw := range map[string][]byte{"stable-policy.json": material.StablePolicy, "stable-policy.sigstore.json": material.StablePolicySignature, "stable-trusted-root.json": material.StableTrustedRoot} {
			if err := write("snapshot/"+name, raw); err != nil {
				return empty, err
			}
		}
	}
	var metadata releasetrust.Metadata
	if err := definitions.DecodeStrict(material.Metadata, &metadata); err != nil {
		return empty, err
	}
	for _, key := range metadata.PrimaryKeys {
		if err := write("keys/"+key.PublicKey, material.PrimaryKeys[key.ID]); err != nil {
			return empty, err
		}
	}
	for name, raw := range documents {
		if err := write("release/"+name, raw); err != nil {
			return empty, err
		}
	}
	bridges := []string{}
	for _, digest := range snapshot.BridgeDigests() {
		if err := digest.Validate(); err != nil {
			return empty, err
		}
		for _, name := range []string{"statement.json", "statement.sigstore.json"} {
			relative := "bridges/" + string(digest) + "/" + name
			raw, err := fetch(ctx, trustBase+relative, releasetrust.MaxDocumentBytes)
			if err != nil {
				return empty, err
			}
			if err := write(relative, raw); err != nil {
				return empty, err
			}
		}
		bridges = append(bridges, filepath.Join(inputs, "bridges", string(digest)))
	}
	output := filepath.Join(state, "rc-bundle")
	if err := upgradeassembly.Assemble(ctx, upgradeassembly.Options{Pinned: pinned, Floor: snapshot.Floor(), SnapshotDirectory: filepath.Join(inputs, "snapshot"), KeysDirectory: filepath.Join(inputs, "keys"), OutputDirectory: output, Releases: []string{filepath.Join(inputs, "release")}, Bridges: bridges}); err != nil {
		return empty, err
	}
	loaded, err := upgradebundle.Load(ctx, output, pinned, snapshot.Floor())
	if err != nil {
		return empty, err
	}
	if _, err := loaded.Release(identity); err != nil {
		return empty, err
	}
	binaryPath := filepath.Join(state, "executable")
	file, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return empty, err
	}
	_, writeErr := file.Write(binary)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return empty, err
	}
	return selfupdate.PreparedNightly{Identity: identity, Directory: filepath.Join(inputs, "release"), BundleDirectory: output, BinaryPath: binaryPath, BinarySHA256: releaseidentity.Hash(binary)}, nil
}

func fetchSnapshot(ctx context.Context, pinned releasetrust.PinnedTrust, fetch fetcher) (releasetrust.SnapshotMaterial, releasetrust.Snapshot, error) {
	material := releasetrust.SnapshotMaterial{PrimaryKeys: map[string][]byte{}}
	var empty releasetrust.Snapshot
	for name, target := range map[string]*[]byte{"metadata.json": &material.Metadata, "metadata.sigstore.json": &material.MetadataSignature, "catalog.json": &material.Catalog, "catalog.sigstore.json": &material.CatalogSignature} {
		raw, err := fetch(ctx, trustBase+name, releasetrust.MaxDocumentBytes)
		if err != nil {
			return material, empty, err
		}
		*target = raw
	}
	var metadata releasetrust.Metadata
	var catalog releasetrust.Catalog
	if err := definitions.DecodeStrict(material.Metadata, &metadata); err != nil {
		return material, empty, err
	}
	if err := definitions.DecodeStrict(material.Catalog, &catalog); err != nil {
		return material, empty, err
	}
	if len(metadata.PrimaryKeys) == 0 || len(metadata.PrimaryKeys) > 256 {
		return material, empty, errors.New("invalid primary key inventory")
	}
	if metadata.Event.SignedBy == releasetrust.StableWorkflowSigner || catalog.StablePolicySHA256 != "" {
		for name, target := range map[string]*[]byte{"stable-policy.json": &material.StablePolicy, "stable-policy.sigstore.json": &material.StablePolicySignature, "stable-trusted-root.json": &material.StableTrustedRoot} {
			raw, err := fetch(ctx, trustBase+name, releasetrust.MaxDocumentBytes)
			if err != nil {
				return material, empty, err
			}
			*target = raw
		}
	}
	seen := map[string]bool{}
	for _, key := range metadata.PrimaryKeys {
		if !releaseidentity.SafeName(key.PublicKey) || seen[key.ID] {
			return material, empty, errors.New("unsafe or duplicate primary key locator")
		}
		seen[key.ID] = true
		raw, err := fetch(ctx, trustBase+key.PublicKey, releasetrust.MaxDocumentBytes)
		if err != nil {
			return material, empty, err
		}
		material.PrimaryKeys[key.ID] = raw
	}
	snapshot, err := releasetrust.VerifySnapshot(pinned, material, releaseidentity.SnapshotFloor{})
	return material, snapshot, err
}

func exactAsset(release updatecheck.Release, name string) (updatecheck.Asset, error) {
	var result updatecheck.Asset
	count := 0
	for _, asset := range release.Assets {
		if asset.Name == name {
			result = asset
			count++
		}
	}
	if count != 1 || result.Size <= 0 || !releaseidentity.SafeName(name) {
		return result, errors.New("missing or ambiguous release asset")
	}
	return result, nil
}

func fetchAsset(ctx context.Context, release updatecheck.Release, name string, limit int64, fetch fetcher) ([]byte, error) {
	asset, err := exactAsset(release, name)
	if err != nil {
		return nil, err
	}
	if asset.Size > limit {
		return nil, errors.New("release asset exceeds byte bound")
	}
	raw, err := fetch(ctx, asset.URL, limit)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != asset.Size || asset.Digest != "sha256:"+string(releaseidentity.Hash(raw)) {
		return nil, errors.New("release download digest or size mismatch")
	}
	return raw, nil
}

func extractARM64(raw []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: maxBinaryBytes + (16 << 20)}
	reader := tar.NewReader(limited)
	seen := map[string]bool{}
	var binary []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !releaseidentity.SafeName(header.Name) || seen[header.Name] || len(seen) >= 32 || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size < 0 || header.Size > maxBinaryBytes {
			return nil, errors.New("unsafe release archive member")
		}
		seen[header.Name] = true
		if header.Name == "hikyo" {
			binary, err = io.ReadAll(io.LimitReader(reader, maxBinaryBytes+1))
			if err != nil || len(binary) == 0 || int64(len(binary)) != header.Size {
				return nil, errors.New("invalid release executable")
			}
		}
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return nil, err
	}
	if limited.N <= 0 || len(binary) == 0 {
		return nil, fmt.Errorf("release archive incomplete or exceeds expansion bound")
	}
	return binary, nil
}
