package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/gofrs/flock"
)

// PruneNightlyCache removes verified release directories, assembled bundles,
// extracted executables and abandoned staging directories that no kept
// identity references. Every full release is several hundred MiB, and a route
// with intermediate hops multiplies that, so the cache is trimmed after each
// completed upgrade. The trust state and lock files are never touched: the
// state carries the rollback floor and the highest observed release.
func (i *Installer) PruneNightlyCache(ctx context.Context, keep ...releaseidentity.Identity) error {
	return i.pruneNightlyCache(ctx, false, keep...)
}

// PruneNightlyScratch keeps all verified releases and executables while an
// unfinished journal may still require their exact bytes to resume.
// Derived private bundles can be rebuilt from those retained releases.
func (i *Installer) PruneNightlyScratch(ctx context.Context) error {
	return i.pruneNightlyCache(ctx, true)
}

func (i *Installer) pruneNightlyCache(ctx context.Context, scratchOnly bool, keep ...releaseidentity.Identity) (err error) {
	if i == nil || i.config.StateDir == "" {
		return errors.New("selfupdate: installer is not configured")
	}
	if err := realNightlyDirectory(i.config.StateDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	lock := flock.New(filepath.Join(i.config.StateDir, "nightly-trust.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("selfupdate: another release verification is running")
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	// Preserve the most recently observed release for target handoff/retry.
	// Reading malformed trust state fails closed before deleting anything.
	known, err := readNightlyState(filepath.Join(i.config.StateDir, "nightly-trust.json"))
	if err != nil {
		return err
	}
	if known.Release.Validate() == nil {
		keep = append(keep, known.Release)
	}
	stable, err := readPreparedStableState(filepath.Join(i.config.StateDir, "stable-prepared-trust.json"))
	if err != nil {
		return err
	}
	if stable.Release.Validate() == nil {
		keep = append(keep, stable.Release)
	}
	retained := map[string]bool{}
	for _, identity := range keep {
		if identity.Validate() != nil {
			return errors.New("selfupdate: retained release identity is invalid")
		}
		retained["nightly-"+string(identity.ManifestSHA256)] = true
		retained[nightlyCacheDirectory(identity)] = true
		retained["stable-"+string(identity.ManifestSHA256)+"-"+runtime.GOOS+"-"+runtime.GOARCH] = true
		retained["executable-"+string(identity.ManifestSHA256)+"-"] = true
	}
	return i.pruneNightlyEntries(ctx, func(name string) bool {
		if scratchOnly && (strings.HasPrefix(name, "nightly-") || strings.HasPrefix(name, "stable-") || strings.HasPrefix(name, "executable-")) {
			return false
		}
		return generatedNightlyEntry(name) && !retained[name] && !retainedExecutable(retained, name)
	})
}

var nightlyCacheName = regexp.MustCompile(`^(nightly-[0-9a-f]{64}(-[a-z0-9]+-[a-z0-9]+)?|stable-[0-9a-f]{64}-[a-z0-9]+-[a-z0-9]+|bundle-[0-9a-f]{64}-[0-9a-f]{64}|executable-[0-9a-f]{64}-[a-z0-9]+-[a-z0-9]+)$`)

func generatedNightlyEntry(name string) bool {
	if nightlyCacheName.MatchString(name) {
		return true
	}
	for _, prefix := range []string{".nightly-download-", ".stable-download-", ".nightly-bundle-inputs-", ".nightly-executable-", ".nightly-trust-", ".hikyo-upgrade-assembly-"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// Called while the nightly trust lock is held. Root-relative removal never
// follows a symlink outside the cache; unrelated names and trust state survive.
func (i *Installer) pruneNightlyEntries(ctx context.Context, remove func(string) bool) error {
	root, err := os.OpenRoot(i.config.StateDir)
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := os.ReadDir(i.config.StateDir)
	if err != nil {
		return err
	}
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if !remove(name) {
			continue
		}
		diagnostics.Printf(ctx, 2, "Removing cached artifact %s", name)
		if err := root.RemoveAll(name); err != nil {
			return err
		}
		removed++
	}
	if removed == 0 {
		return nil
	}
	diagnostics.Printf(ctx, 1, "Removed %d cached release artifacts no longer needed.", removed)
	return filedurability.SyncDirectory(i.config.StateDir)
}

// pruneOtherBundles runs under the nightly trust lock, only for the private
// automatic coordinator cache. Public runtime and manual staging are separate.
func (i *Installer) pruneOtherBundles(ctx context.Context, keep string) error {
	if !i.config.TransientBundles {
		return nil
	}
	return i.pruneNightlyEntries(ctx, func(name string) bool {
		return strings.HasPrefix(name, "bundle-") && nightlyCacheName.MatchString(name) && name != filepath.Base(keep)
	})
}

// retainedExecutable matches executable-<manifest>-<os>-<arch> against the
// retained executable-<manifest>- prefixes.
func retainedExecutable(retained map[string]bool, name string) bool {
	for prefix := range retained {
		if strings.HasPrefix(prefix, "executable-") && strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
