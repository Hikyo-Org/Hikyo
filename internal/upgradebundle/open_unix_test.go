//go:build !windows

package upgradebundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"golang.org/x/sys/unix"
)

func TestBundleRefusesSymlinkAndFIFOWithoutBlocking(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			f := newBundleFixture(t)
			path := filepath.Join(f.directory, "index.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if kind == "fifo" {
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				target := filepath.Join(t.TempDir(), "external.json")
				if err := os.WriteFile(target, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Load(context.Background(), f.directory, f.signer.Pinned, releaseidentity.SnapshotFloor{}); err == nil {
				t.Fatal("special file accepted")
			}
		})
	}
}
