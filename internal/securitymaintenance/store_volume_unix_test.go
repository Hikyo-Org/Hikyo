//go:build linux || darwin

package securitymaintenance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The coordinator's state refuses a world-writable volume root unless the operator
// accepted it; a group-writable (fsGroup) root needs no override.
func TestStoreVolumeRootWritability(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	volume := filepath.Join(base, "volume")
	state := filepath.Join(volume, "state")
	if err := os.Mkdir(volume, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(volume, 0o2775); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStoreOnVolume(state, volume); err != nil {
		t.Fatalf("group-writable volume root refused: %v", err)
	}
	if err := os.Chmod(volume, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStoreOnVolume(state, volume); err == nil || !strings.Contains(err.Error(), "accept_world_writable_volume_root") {
		t.Fatalf("world-writable volume root error = %v", err)
	}
	if _, err := NewStoreOnWorldWritableVolume(state, volume); err != nil {
		t.Fatalf("world-writable volume root refused despite the override: %v", err)
	}
}
