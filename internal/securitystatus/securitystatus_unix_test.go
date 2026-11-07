//go:build linux || darwin

package securitystatus

import (
	"path/filepath"
	"syscall"
	"testing"
)

// A FIFO with no writer blocks a plain open, so it must be refused without being waited on.
func TestLoadRefusesAFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	if err := syscall.Mkfifo(path, 0644); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got.Reported || !got.Unreadable {
		t.Fatalf("Load = %+v, want unreadable", got)
	}
}
