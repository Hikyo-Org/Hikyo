package bridgerevocation

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// No two runs hold the persistence lock at once, including when a lock file is left behind
// by a run that died long ago: the file is not the lock, the operating system's lock on it is.
func TestPersistenceLockIsExclusiveAndIgnoresALeftoverFile(t *testing.T) {
	directory := t.TempDir()
	leftover := filepath.Join(directory, lockFile)
	if err := os.WriteFile(leftover, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	long := time.Now().Add(-time.Hour)
	if err := os.Chtimes(leftover, long, long); err != nil {
		t.Fatal(err)
	}
	checker := Checker{Directory: directory}
	var inside, overlaps, entries atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			release, err := checker.lock()
			if err != nil {
				t.Error(err)
				return
			}
			entries.Add(1)
			if inside.Add(1) > 1 {
				overlaps.Add(1)
			}
			time.Sleep(2 * time.Millisecond)
			inside.Add(-1)
			release()
		})
	}
	wg.Wait()
	if entries.Load() != 16 || overlaps.Load() != 0 {
		t.Fatalf("every run must get the lock in turn: entries %d, overlaps %d", entries.Load(), overlaps.Load())
	}
}

// While one run holds the lock the next waits; it does not take it over.
func TestPersistenceLockHeldByAnotherRunMakesTheNextRunWait(t *testing.T) {
	checker := Checker{Directory: t.TempDir()}
	release, err := checker.lock()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		next, err := checker.lock()
		if err == nil {
			next()
		}
		done <- err
	}()
	select {
	case err := <-done:
		release()
		t.Fatalf("a second run must wait for the holder, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	// The waiter gets the lock once the holder releases it, and is finished before the test's
	// directory is removed (a still-polling waiter would recreate the lock file in it).
	release()
	if err := <-done; err != nil {
		t.Fatalf("the waiting run must get the lock after the holder releases it: %v", err)
	}
}

// A run that cannot get the lock within its wait refuses; it never proceeds unlocked.
func TestPersistenceLockTimeoutRefuses(t *testing.T) {
	directory := t.TempDir()
	release, err := (Checker{Directory: directory}).lock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := (Checker{Directory: directory, lockWait: 50 * time.Millisecond}).lock(); err == nil {
		t.Fatal("a second holder must be refused once its wait passes")
	}
}
