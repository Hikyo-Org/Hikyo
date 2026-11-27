//go:build unix

package bridgerevocation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/gofrs/flock"
)

var tornNow = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

type listFetcher struct {
	list, signature []byte
	err             error
}

func (l listFetcher) Fetch(context.Context) ([]byte, []byte, error) {
	return l.list, l.signature, l.err
}

// chanFetcher answers once the test sends the list, then reports that it was asked.
type chanFetcher struct {
	answers chan [2][]byte
	asked   chan struct{}
}

func (c chanFetcher) Fetch(context.Context) ([]byte, []byte, error) {
	v := <-c.answers
	if c.asked != nil {
		close(c.asked)
	}
	return v[0], v[1], nil
}

type answer struct {
	result Result
	err    error
}

// place replaces path with a regular file holding raw, by one rename.
func place(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path+".staged", raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".staged", path); err != nil {
		t.Fatal(err)
	}
}

// tornRead starts a check while the test holds the persistence lock as a writer that has
// raised the floor to list 2 but not yet stored list 2. The store and floor are FIFOs, so
// the test knows the run's unlocked read is exactly list 1 beside floor 2, and regular files
// (the same torn pair) take their place before that read returns. Both lists are issued age
// before the clock. It returns list 2's store.
func tornRead(t *testing.T, mode Mode, fetcher Fetcher, wait, age time.Duration) (string, []byte, *flock.Flock, <-chan answer) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	clock := func() time.Time { return tornNow }
	files := map[int64]map[string][]byte{}
	for _, sequence := range []int64{1, 2} {
		raw := testfixture.JSON(t, releasetrust.RevocationList{Schema: releasetrust.RevocationSchema, Sequence: sequence, IssuedAt: tornNow.Add(-age).Format(time.RFC3339), TrustRootSHA256: releaseidentity.Hash(f.Pinned.Root)})
		directory := t.TempDir()
		accept := Checker{Mode: Online, Directory: directory, Now: clock, Fetcher: listFetcher{list: raw, signature: testfixture.Sign(t, f.RecoverySigner, raw)}}
		if _, err := accept.Check(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}
		files[sequence] = map[string][]byte{}
		for _, name := range []string{storeFile, floorFile} {
			raw, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				t.Fatal(err)
			}
			files[sequence][name] = raw
		}
	}
	directory := t.TempDir()
	for _, name := range []string{storeFile, floorFile} {
		if err := syscall.Mkfifo(filepath.Join(directory, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	held := flock.New(filepath.Join(directory, lockFile))
	if err := held.Lock(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Unlock() })
	done := make(chan answer, 1)
	go func() {
		r, err := (Checker{Mode: mode, Fetcher: fetcher, Directory: directory, Now: clock, lockWait: wait}).Check(t.Context(), snapshot)
		done <- answer{r, err}
	}()
	// load reads the store, then the floor. Opening each FIFO to write blocks until the run
	// opens it to read; the regular file is renamed in before the bytes are written, so the
	// run's next read finds files on disk, never a FIFO it would block on.
	for _, feed := range []struct {
		name string
		raw  []byte
	}{{storeFile, files[1][storeFile]}, {floorFile, files[2][floorFile]}} {
		path := filepath.Join(directory, feed.name)
		pipe, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		place(t, path, feed.raw)
		if _, err := pipe.Write(feed.raw); err != nil {
			t.Fatal(err)
		}
		if err := pipe.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return directory, files[2][storeFile], held, done
}

// A run without a fresh list (off, or an unreachable endpoint) can read the store and the
// floor while a concurrent persist is between its floor and store writes. Only the read
// under the lock may decide the state is unusable: a run that read the torn pair waits for
// the writer and reports its list, a store genuinely behind the floor still refuses with
// advice naming both files, and nothing is decided while the writer holds the lock.
func TestATornReadIsDecidedOnlyUnderTheLock(t *testing.T) {
	for name, m := range map[string]struct {
		mode    Mode
		fetcher Fetcher
		outcome Outcome
		advice  string
	}{
		"off":         {Off, nil, Disabled, "HIKYO_UPGRADE_REVOCATION_CHECK"},
		"unreachable": {Online, listFetcher{err: ErrUnreachable}, Unreachable, "the endpoint is unreachable"},
	} {
		t.Run(name+"/the writer finishes", func(t *testing.T) {
			directory, store, held, done := tornRead(t, m.mode, m.fetcher, 0, time.Hour)
			place(t, filepath.Join(directory, storeFile), store)
			if err := held.Unlock(); err != nil {
				t.Fatal(err)
			}
			a := <-done
			if a.err != nil || a.result.Outcome != m.outcome || a.result.Sequence != 2 {
				t.Fatalf("a run that read a half-written pair must report the finished list 2: %+v %v", a.result, a.err)
			}
		})
		t.Run(name+"/the writer died after the floor", func(t *testing.T) {
			directory, _, held, done := tornRead(t, m.mode, m.fetcher, 0, time.Hour)
			if err := held.Unlock(); err != nil {
				t.Fatal(err)
			}
			a := <-done
			if !errors.Is(a.err, ErrUnusableState) || !strings.Contains(a.err.Error(), "is behind the floor") || !strings.Contains(a.err.Error(), m.advice) || !strings.Contains(a.err.Error(), "a stale list cannot") || !strings.Contains(a.err.Error(), filepath.Join(directory, storeFile)) || !strings.Contains(a.err.Error(), filepath.Join(directory, floorFile)) {
				t.Fatalf("a store truly behind the floor must refuse, naming what to repair: %+v %v", a.result, a.err)
			}
		})
		t.Run(name+"/the writer still holds the lock", func(t *testing.T) {
			_, _, held, done := tornRead(t, m.mode, m.fetcher, 50*time.Millisecond, time.Hour)
			a := <-done
			if err := held.Unlock(); err != nil {
				t.Fatal(err)
			}
			if a.err == nil || errors.Is(a.err, ErrUnusableState) || !strings.Contains(a.err.Error(), "locked by another run") {
				t.Fatalf("the run must wait for the lock and never judge the half-written pair it read unlocked: %+v %v", a.result, a.err)
			}
		})
	}
	// Lists are not re-signed on a schedule, so the endpoint routinely serves a stale list: here
	// the very list the writer is storing. The torn read must not make it a refusal.
	t.Run("online/a stale fetch of the list being stored", func(t *testing.T) {
		fetcher := chanFetcher{answers: make(chan [2][]byte, 1)}
		directory, store, held, done := tornRead(t, Online, fetcher, 0, MaxListAge+24*time.Hour)
		list, signature := parseStored(store)
		fetcher.answers <- [2][]byte{list, signature}
		place(t, filepath.Join(directory, storeFile), store)
		if err := held.Unlock(); err != nil {
			t.Fatal(err)
		}
		a := <-done
		if a.err != nil || a.result.Outcome != Stale || a.result.Sequence != 2 {
			t.Fatalf("a torn read followed by a stale fetch of list 2 must report list 2 as stale: %+v %v", a.result, a.err)
		}
	})
}

// The floor lands before the store, so a failed floor write leaves the store as it was: a store
// ahead of its floor would let a later loss of the store lower the floor. The floor is a FIFO
// until persist has read it under the lock, then a directory, so only raising it fails.
func TestAFailedFloorWriteLeavesTheStoreUnchanged(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	clock := func() time.Time { return tornNow }
	signed := func(sequence int64) listFetcher {
		raw := testfixture.JSON(t, releasetrust.RevocationList{Schema: releasetrust.RevocationSchema, Sequence: sequence, IssuedAt: tornNow.Add(-time.Hour).Format(time.RFC3339), TrustRootSHA256: releaseidentity.Hash(f.Pinned.Root)})
		return listFetcher{list: raw, signature: testfixture.Sign(t, f.RecoverySigner, raw)}
	}
	directory := t.TempDir()
	if _, err := (Checker{Mode: Online, Directory: directory, Now: clock, Fetcher: signed(1)}).Check(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	storePath, floorPath := filepath.Join(directory, storeFile), filepath.Join(directory, floorFile)
	store, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	floor, err := os.ReadFile(floorPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(floorPath); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(floorPath, 0o600); err != nil {
		t.Fatal(err)
	}
	two := signed(2)
	fetcher := chanFetcher{answers: make(chan [2][]byte, 1), asked: make(chan struct{})}
	fetcher.answers <- [2][]byte{two.list, two.signature}
	done := make(chan answer, 1)
	go func() {
		r, err := (Checker{Mode: Online, Fetcher: fetcher, Directory: directory, Now: clock}).Check(t.Context(), snapshot)
		done <- answer{r, err}
	}()
	// Every floor read before the fetch gets the floor. The first one after it is persist's read
	// under the lock: the FIFO is then replaced by a directory, so raising the floor fails.
	for last := false; !last; {
		pipe, err := os.OpenFile(floorPath, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-fetcher.asked:
			last = true
			if err := os.Remove(floorPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(floorPath, 0o700); err != nil {
				t.Fatal(err)
			}
		default:
		}
		if _, err := pipe.Write(floor); err != nil {
			t.Fatal(err)
		}
		if err := pipe.Close(); err != nil {
			t.Fatal(err)
		}
	}
	a := <-done
	after, err := os.ReadFile(storePath)
	if a.err == nil || err != nil || string(after) != string(store) {
		t.Fatalf("a failed floor write must refuse and leave the store at list 1: %+v %v (store read: %v)", a.result, a.err, err)
	}
}
