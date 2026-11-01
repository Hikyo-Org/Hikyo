package selfupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func stablePruneIdentity(version string, sequence uint64) releaseidentity.Identity {
	return releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: version, Sequence: sequence, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compatibility")), ManifestSHA256: releaseidentity.Hash([]byte(version))}
}

func stablePruneDirectory(identity releaseidentity.Identity) string {
	return "stable-" + string(identity.ManifestSHA256) + "-" + runtime.GOOS + "-" + runtime.GOARCH
}

func TestPruneStableCacheRetainsObservedAndPendingTargets(t *testing.T) {
	state := t.TempDir()
	observed, pending, stale := stablePruneIdentity("1.3.1", 3), stablePruneIdentity("1.2.2", 2), stablePruneIdentity("1.1.1", 1)
	for _, name := range []string{stablePruneDirectory(observed), stablePruneDirectory(pending), stablePruneDirectory(stale), ".stable-download-abandoned", "stable-operator-notes"} {
		if err := os.Mkdir(filepath.Join(state, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	trustPath := filepath.Join(state, "stable-prepared-trust.json")
	if err := saveNightlyState(trustPath, nightlyVerificationState{Release: observed}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	installer := newInstaller(nil, nil)
	installer.config.StateDir = state
	if err := installer.PruneNightlyCache(t.Context(), pending); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{stablePruneDirectory(observed), stablePruneDirectory(pending), "stable-operator-notes", "stable-prepared-trust.json"} {
		if _, err := os.Stat(filepath.Join(state, name)); err != nil {
			t.Fatal("lost retained stable evidence", name, err)
		}
	}
	for _, name := range []string{stablePruneDirectory(stale), ".stable-download-abandoned"} {
		if _, err := os.Lstat(filepath.Join(state, name)); !os.IsNotExist(err) {
			t.Fatal("retained stale stable artifact", name, err)
		}
	}
	after, err := os.ReadFile(trustPath)
	if err != nil || string(before) != string(after) {
		t.Fatal("modified stable rollback state", err)
	}
}

func TestPruneStableScratchKeepsAllInterruptedRouteReleases(t *testing.T) {
	state := t.TempDir()
	first, second := stablePruneIdentity("1.2.2", 2), stablePruneIdentity("1.3.1", 3)
	names := []string{stablePruneDirectory(first), stablePruneDirectory(second), "executable-" + string(first.ManifestSHA256) + "-linux-amd64"}
	for _, name := range append(names, ".stable-download-abandoned") {
		if err := os.Mkdir(filepath.Join(state, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	installer := newInstaller(nil, nil)
	installer.config.StateDir = state
	if err := installer.PruneNightlyScratch(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(state, name)); err != nil {
			t.Fatal("lost an interrupted stable route payload", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(state, ".stable-download-abandoned")); !os.IsNotExist(err) {
		t.Fatal("retained abandoned stable download", err)
	}
}

func TestPruneStableCacheRefusesMalformedStateBeforeDeleting(t *testing.T) {
	state := t.TempDir()
	stale := filepath.Join(state, stablePruneDirectory(stablePruneIdentity("1.1.1", 1)))
	if err := os.Mkdir(stale, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "stable-prepared-trust.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	installer := newInstaller(nil, nil)
	installer.config.StateDir = state
	if err := installer.PruneNightlyCache(t.Context()); err == nil {
		t.Fatal("ignored corrupt stable trust state")
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatal("deleted stable evidence before checking trust state", err)
	}
}

func TestPruneStableCacheDoesNotFollowSymlinks(t *testing.T) {
	state, outside := t.TempDir(), t.TempDir()
	sentinel := filepath.Join(outside, "keep")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(state, stablePruneDirectory(stablePruneIdentity("1.1.1", 1)))
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	installer := newInstaller(nil, nil)
	installer.config.StateDir = state
	if err := installer.PruneNightlyCache(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("retained stale symlink", err)
	}
	if raw, err := os.ReadFile(sentinel); err != nil || string(raw) != "keep" {
		t.Fatal("followed stable cache symlink outside its root", err)
	}
}
