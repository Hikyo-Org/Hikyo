package selfupdate

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/gofrs/flock"
)

func TestPruneNightlyCacheKeepsRetainedIdentityAndTrustState(t *testing.T) {
	state := t.TempDir()
	keep := releaseidentity.Identity{Profile: releaseidentity.NightlyV1, Version: "1.1.0-nightly.3", Sequence: 3, Commit: strings.Repeat("b", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("claim")), ManifestSHA256: releaseidentity.Hash([]byte("keep"))}
	stale := releaseidentity.Hash([]byte("stale"))
	for _, dir := range []string{nightlyCacheDirectory(keep), "nightly-" + string(stale) + "-linux-amd64", "nightly-" + string(keep.ManifestSHA256), "nightly-" + string(stale), "bundle-" + strings.Repeat("a", 64) + "-" + strings.Repeat("b", 64), ".nightly-download-abandoned", ".nightly-bundle-inputs-abandoned"} {
		if err := os.MkdirAll(filepath.Join(state, dir, "inner"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"executable-" + string(keep.ManifestSHA256) + "-linux-amd64", "executable-" + string(stale) + "-linux-amd64", "nightly-trust.lock", "unrelated"} {
		if err := os.WriteFile(filepath.Join(state, file), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var progress bytes.Buffer
	installer := newInstaller(nil, nil)
	installer.config = Config{StateDir: state}
	if err := installer.PruneNightlyCache(diagnostics.With(t.Context(), 1, &progress), keep); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(state)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	want := []string{"executable-" + string(keep.ManifestSHA256) + "-linux-amd64", "nightly-" + string(keep.ManifestSHA256), nightlyCacheDirectory(keep), "nightly-trust.lock", "unrelated"}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("after prune: %v, want %v", names, want)
	}
	if !strings.Contains(progress.String(), "Removed 6 cached release artifacts") {
		t.Fatalf("progress: %q", progress.String())
	}
	if err := installer.PruneNightlyCache(t.Context(), releaseidentity.Identity{}); err == nil {
		t.Fatal("accepted an invalid retained identity")
	}
}

func TestPruneNightlyCacheRefusesBusyCacheAndPreservesSymlinkTargets(t *testing.T) {
	state := t.TempDir()
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	name := "nightly-" + strings.Repeat("a", 64)
	if err := os.Symlink(outside, filepath.Join(state, name)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nightly-not-generated", "bundle-operator-notes"} {
		if err := os.WriteFile(filepath.Join(state, name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	installer := newInstaller(nil, nil)
	installer.config.StateDir = state
	lock := flock.New(filepath.Join(state, "nightly-trust.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Unlock() })
	if err := installer.PruneNightlyCache(t.Context()); err == nil {
		t.Fatal("pruned active cache")
	}
	if _, err := os.Lstat(filepath.Join(state, name)); err != nil {
		t.Fatal(err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if err := installer.PruneNightlyCache(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(state, name)); !os.IsNotExist(err) {
		t.Fatal("stale symlink retained", err)
	}
	for _, path := range []string{sentinel, filepath.Join(state, "nightly-not-generated"), filepath.Join(state, "bundle-operator-notes")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPruneNightlyCachePreservesMalformedTrustState(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(state, ".hikyo-upgrade-assembly-abandoned")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "nightly-trust.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	installer := newInstaller(nil, nil)
	installer.config.StateDir = state
	if err := installer.PruneNightlyCache(t.Context()); err == nil {
		t.Fatal("ignored malformed trust state")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("deleted before trust validation", err)
	}
}

func TestPruneNightlyScratchKeepsInterruptedRouteReleases(t *testing.T) {
	state := t.TempDir()
	installer := newInstaller(nil, nil)
	installer.config.StateDir = state
	for _, name := range []string{"nightly-" + strings.Repeat("a", 64), "nightly-" + strings.Repeat("b", 64), "executable-" + strings.Repeat("a", 64) + "-linux-amd64", "bundle-" + strings.Repeat("a", 64) + "-" + strings.Repeat("b", 64), ".hikyo-upgrade-assembly-abandoned"} {
		if err := os.Mkdir(filepath.Join(state, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := installer.PruneNightlyScratch(t.Context()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("lost interrupted route or retained scratch: %v", entries)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "bundle-") || strings.HasPrefix(entry.Name(), ".") {
			t.Fatal("retained derived bundle", entry.Name())
		}
	}
}
