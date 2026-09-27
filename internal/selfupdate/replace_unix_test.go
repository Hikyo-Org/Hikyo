//go:build !windows

package selfupdate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofrs/flock"
)

func TestReplaceBinaryRefusesAConcurrentUpdaterWithoutRemovingTheTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "hikyo")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := flock.New(target + ".update.lock")
	locked, err := lock.TryLock()
	if err != nil || !locked {
		t.Fatalf("hold replacement lock: locked=%t err=%v", locked, err)
	}
	defer lock.Unlock()

	err = replaceBinary(t.Context(), target, []byte("new"), 0o755)
	if err == nil || !strings.Contains(err.Error(), "another Hikyo process") {
		t.Fatalf("replaceBinary() error = %v, want concurrent-updater refusal", err)
	}
	raw, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(raw) != "old" {
		t.Fatalf("target = %q after lock refusal, want old", raw)
	}
}

func TestPrivilegedUpdateRefusesAUserOwnedExecutable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("needs a non-root file owner")
	}
	target := filepath.Join(t.TempDir(), "hikyo")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkReplacementOwner(target, info, 0); err == nil || !strings.Contains(err.Error(), "without sudo") {
		t.Fatalf("root replacement of user-owned executable: %v", err)
	}
	if err := checkReplacementOwner(target, info, os.Geteuid()); err != nil {
		t.Fatalf("owner replacement refused: %v", err)
	}
}
