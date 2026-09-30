//go:build unix

package crypto

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRootKeyFileRefusesSymlinksNonRegularAndOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte(EncodeRootKey(make([]byte, KeySize))), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "symlink")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(dir, "oversized")
	if err := os.WriteFile(oversized, []byte(strings.Repeat("a", rootKeyFileLimit+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{symlink, fifo, oversized} {
		if _, err := ReadRootKey(path, ""); err == nil ||
			(!errors.Is(err, ErrRootKeyPerms) && !errors.Is(err, ErrRootKeyFormat)) {
			t.Fatalf("ReadRootKey(%s) = %v, want custody or format refusal", filepath.Base(path), err)
		}
	}
}
