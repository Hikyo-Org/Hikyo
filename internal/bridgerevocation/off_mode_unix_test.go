//go:build unix

package bridgerevocation_test

import (
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/gofrs/flock"
)

// An off-mode run that read sequence 1 and then waited for the lock (to restore a missing
// floor) must report the newer list stored meanwhile, which revokes the bridge, never its
// own older read. The store is a FIFO, so the test knows exactly when the run has read it.
func TestAnOffModeRunReportsTheNewerStoredList(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	clock := func() time.Time { return testNow }
	bridge := releaseidentity.Hash([]byte("bridge"))
	older, newer := t.TempDir(), t.TempDir()
	l1, s1 := signedList(t, f, 1)
	l2, s2 := signedList(t, f, 2, bridge)
	for directory, list := range map[string][2][]byte{older: {l1, s1}, newer: {l2, s2}} {
		fetcher := fetchFunc(func() ([]byte, []byte, error) { return list[0], list[1], nil })
		if _, err := (bridgerevocation.Checker{Mode: bridgerevocation.Online, Directory: directory, Now: clock, Fetcher: fetcher}).Check(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}
	}
	read := func(directory string) []byte {
		raw, err := os.ReadFile(filepath.Join(directory, "bridge-revocations-store.json"))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	olderStore, newerStore := read(older), read(newer)

	directory := t.TempDir()
	store := filepath.Join(directory, "bridge-revocations-store.json")
	if err := syscall.Mkfifo(store, 0o600); err != nil {
		t.Fatal(err)
	}
	held := flock.New(filepath.Join(directory, ".bridge-revocations.lock"))
	if err := held.Lock(); err != nil {
		t.Fatal(err)
	}
	type answer struct {
		result bridgerevocation.Result
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		r, err := (bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory, Now: clock}).Check(t.Context(), snapshot)
		done <- answer{r, err}
	}()
	// Opening the FIFO for writing blocks until the run opens it to read: once this write is
	// closed, the run holds sequence 1 and can only finish after the lock is released.
	if err := os.WriteFile(store, olderStore, 0o600); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(directory, "staged")
	if err := os.WriteFile(staged, newerStore, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staged, store); err != nil {
		t.Fatal(err)
	}
	if err := held.Unlock(); err != nil {
		t.Fatal(err)
	}
	a := <-done
	if a.err != nil || a.result.Sequence != 2 || !slices.Contains(a.result.Revoked, bridge) {
		t.Fatalf("the off-mode run reported %+v, %v; want sequence 2 revoking the bridge", a.result, a.err)
	}
}
