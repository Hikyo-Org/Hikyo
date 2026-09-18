package hostupgrade

import (
	"os"
	"path/filepath"
	"testing"
)

// atomicWrite delegates publication to securefile but keeps its own gate: a
// destination whose directory is not root-owned is refused before anything is
// written, so nothing appears beside the destination.
func TestAtomicWriteRefusesUntrustedDirectoryBeforePublishing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the trusted-directory check passes for root-owned temp directories")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hikyo.env")
	if err := atomicWrite(path, []byte("HIKYO_X=1\n"), 0o600); err == nil {
		t.Fatal("atomicWrite published into a directory that is not root-owned")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("refused publication left entries behind: %v", entries)
	}
}
