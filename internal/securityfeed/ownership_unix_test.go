//go:build !windows

package securityfeed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Kubernetes projects the coordinator's ConfigMap files as root while it runs
// unprivileged, so a root-owned public file is accepted. A root coordinator
// already owns what it reads, so its rule is unchanged.
func TestPublicFileMayBeRootOwned(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a root process owns every file it reads")
	}
	if _, err := readRegular("/etc/passwd", 1<<20, false); err != nil {
		t.Fatalf("root-owned public file refused: %v", err)
	}
}

func TestVolumeRootMayBeGroupWritableButNotItsAncestors(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	volume := filepath.Join(base, "volume")
	state := filepath.Join(volume, "state")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(volume, 0o2775); err != nil {
		t.Fatal(err)
	}
	if checkAncestors(state, "") == nil {
		t.Fatal("group-writable volume root accepted without being named")
	}
	if err := checkAncestors(state, volume); err != nil {
		t.Fatalf("named volume root refused: %v", err)
	}
	if err := os.Chmod(base, 0o775); err != nil {
		t.Fatal(err)
	}
	if checkAncestors(state, volume) == nil {
		t.Fatal("a writable ancestor above the volume root was exempted")
	}
}

// A world-writable volume root without the sticky bit needs the explicit override.
func TestWorldWritableVolumeRootNeedsTheOverride(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	volume := filepath.Join(base, "volume")
	state := filepath.Join(volume, "state")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(volume, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := checkAncestors(state, volume); err == nil || !strings.Contains(err.Error(), "accept_world_writable_volume_root") {
		t.Fatalf("world-writable volume root error = %v", err)
	}
	if err := checkVolumeAncestors(state, volume, true); err != nil {
		t.Fatalf("world-writable volume root refused despite the override: %v", err)
	}
	if err := os.Chmod(volume, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := checkAncestors(state, volume); err != nil {
		t.Fatalf("sticky world-writable volume root refused: %v", err)
	}
}
