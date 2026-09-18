//go:build unix

package importer

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A helper that forks a grandchild and outlives the deadline must take the
// grandchild down with it: the timeout kills the whole process group, not
// just the direct child.
func TestSubprocessTimeoutKillsTheGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	// Two seconds is far past a shell fork on a loaded runner and still keeps
	// the test short; the assertion is about who dies, not how fast.
	deadline := time.Now().Add(2 * time.Second)
	spec := subprocessSpec{
		Command:  "/bin/sh",
		Args:     []string{"-c", `sleep 30 & echo $! > "$1"; wait`, "_", pidFile},
		MaxBytes: 1024, RunDeadlineUnixNano: deadline.UnixNano(),
	}
	encoded, err := encodeSubprocessSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(subprocessSpecEnv, encoded)

	started := time.Now()
	handled, code := RunInternalSubprocess([]string{internalSubprocessMode}, io.Discard)
	if !handled || code != subprocessExitTimeout {
		t.Fatalf("handled=%v code=%d, want timeout exit %d", handled, code, subprocessExitTimeout)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("timeout path took %s: Wait hung on the inherited pipe", elapsed)
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("grandchild pid was not recorded before the deadline: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("grandchild pid %q: %v", raw, err)
	}
	// The killed grandchild is reparented and reaped by init; give that a
	// bounded moment rather than asserting the instant after the kill.
	gone := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(gone) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d survived the timeout (kill 0 err=%v)", pid, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
