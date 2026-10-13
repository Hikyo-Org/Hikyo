// native-prepare authenticates released artifacts without executing a candidate.
// Stable RCs use the same signature and offline-bundle verifier as production,
// without changing which releases the unattended updater admits.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradeassembly"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Masterminds/semver/v3"
)

type options struct{ version, commit, published, trust, state, releaseJSON string }
type result struct {
	Identity        releaseidentity.Identity `json:"identity"`
	BundleDirectory string                   `json:"bundle_directory"`
	BinaryPath      string                   `json:"binary_path"`
	BinarySHA256    releaseidentity.Digest   `json:"binary_sha256"`
	Artifacts       []releasetrust.Artifact  `json:"artifacts"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		// Download errors can contain HTTP response details. Keep diagnostics fixed.
		fmt.Fprintln(os.Stderr, "native release preparation failed")
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	var o options
	fs := flag.NewFlagSet("native-prepare", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.version, "version", "", "exact published version without v")
	fs.StringVar(&o.commit, "source-sha", "", "exact released source SHA")
	fs.StringVar(&o.published, "published-at", "", "exact RFC3339 publication time")
	fs.StringVar(&o.trust, "trust", "", "independently reviewed trust directory")
	fs.StringVar(&o.state, "state", "", "fresh private preparation directory")
	fs.StringVar(&o.releaseJSON, "release-json", "", "published GitHub release snapshot")
	if err := fs.Parse(args); err != nil {
		return err
	}
	v, err := semver.StrictNewVersion(o.version)
	published, timeErr := time.Parse(time.RFC3339, o.published)
	if err != nil || timeErr != nil || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(o.commit) || o.trust == "" || o.state == "" || o.releaseJSON == "" || fs.NArg() != 0 {
		return errors.New("invalid exact release preparation inputs")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" {
		return errors.New("hosted release preparation requires native Linux ARM64")
	}
	client, err := updatecheck.NewHTTPClient(60 * time.Second)
	if err != nil {
		return err
	}
	release, err := updatecheck.NewGitHubSource(client).ReleaseByVersion(ctx, o.version)
	if err != nil {
		return err
	}
	if !release.Immutable || !release.PublishedAt.Equal(published) {
		return errors.New("published release binding mismatch")
	}
	if err := bindSnapshot(o.releaseJSON, release); err != nil {
		return err
	}
	rootRaw, err := upgradeassembly.ReadDocument(filepath.Join(o.trust, "root.json"))
	if err != nil {
		return err
	}
	var root releasetrust.Root
	if err := definitions.DecodeStrict(rootRaw, &root); err != nil {
		return err
	}
	if !releaseidentity.SafeName(root.Recovery.PublicKey) {
		return errors.New("unsafe recovery key locator")
	}
	recovery, err := upgradeassembly.ReadDocument(filepath.Join(o.trust, root.Recovery.PublicKey))
	if err != nil {
		return err
	}
	pinned := releasetrust.PinnedTrust{Root: rootRaw, RecoveryPublicKey: recovery}
	state, err := filepath.Abs(o.state)
	if err != nil {
		return err
	}
	if err := os.Mkdir(state, 0700); err != nil {
		return err
	}
	var prepared selfupdate.PreparedNightly
	if usesStablePrereleaseAdapter(v, release) {
		prepared, err = prepareRC(ctx, release, o.commit, state, pinned)
	} else {
		installer, createErr := selfupdate.NewInstaller(selfupdate.Config{StateDir: state, TrustRootBase64: base64.StdEncoding.EncodeToString(rootRaw), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(recovery)})
		if createErr != nil {
			return createErr
		}
		channel := updatecheck.ChannelStable
		if strings.HasPrefix(v.Prerelease(), "nightly.") {
			channel = updatecheck.ChannelNightly
		}
		prepared, err = installer.PrepareRelease(ctx, updatecheck.Status{Available: true, Channel: channel, LatestVersion: release.Version, Prerelease: release.Prerelease, Immutable: release.Immutable, PublishedAt: release.PublishedAt, Assets: release.Assets, URL: release.URL})
	}
	if err != nil {
		return err
	}
	if prepared.Identity.Version != o.version || prepared.Identity.Commit != o.commit {
		return errors.New("signed release identity mismatch")
	}
	bundle, err := upgradebundle.Load(ctx, prepared.BundleDirectory, pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		return err
	}
	verified, err := bundle.Release(prepared.Identity)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result{prepared.Identity, prepared.BundleDirectory, prepared.BinaryPath, prepared.BinarySHA256, verified.Artifacts()})
}

func usesStablePrereleaseAdapter(version *semver.Version, release updatecheck.Release) bool {
	// Hikyo also labels all 0.x core versions as GitHub prereleases. The signed
	// stable profile permits those builds even though updater discovery excludes
	// GitHub prereleases. Lab preparation must not alter that updater policy.
	return (release.Prerelease || version.Prerelease() != "") && !strings.HasPrefix(version.Prerelease(), "nightly.")
}

func bindSnapshot(path string, release updatecheck.Release) error {
	raw, err := upgradeassembly.ReadDocument(path)
	if err != nil {
		return err
	}
	// GitHub adds API fields over time. Decode the documented identity fields;
	// none of the other fields can choose authority or code to execute.
	var snapshot struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Immutable  bool   `json:"immutable"`
		Prerelease bool   `json:"prerelease"`
		Published  string `json:"published_at"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	published, err := time.Parse(time.RFC3339, snapshot.Published)
	if err != nil || snapshot.Tag != "v"+release.Version || snapshot.Draft || !snapshot.Immutable || snapshot.Prerelease != release.Prerelease || !published.Equal(release.PublishedAt) || len(snapshot.Assets) != len(release.Assets) {
		return errors.New("release snapshot binding mismatch")
	}
	assets := map[string]updatecheck.Asset{}
	for _, asset := range release.Assets {
		assets[asset.Name] = asset
	}
	for _, asset := range snapshot.Assets {
		current, found := assets[asset.Name]
		if !found || current.URL != asset.URL || current.Digest != asset.Digest || current.Size != asset.Size {
			return errors.New("release snapshot inventory changed")
		}
		delete(assets, asset.Name)
	}
	if len(assets) != 0 {
		return errors.New("release snapshot inventory incomplete")
	}
	return nil
}
