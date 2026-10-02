//go:build unix

package filesync

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestIdenticalRetryFinishesInterruptedPruneAndCollection(t *testing.T) {
	f := newFixture(t)
	f.pol.OnRemoved = RemovedPrune
	obsolete := f.mustPublish(file("a", "first"), file("b", "removed"))
	previous := f.mustPublish(file("a", "second"), file("b", "removed"))
	current, err := f.publish(&faultProbe{afterErr: errCrash}, file("a", "third"))
	if !errors.Is(err, errCrash) || !current.Changed {
		t.Fatalf("postcommit interruption = %+v, %v", current, err)
	}
	if _, err := os.Lstat(filepath.Join(f.dir, "b")); err != nil {
		t.Fatal("fixture did not interrupt before pruning")
	}
	foreign := filepath.Join(f.dir, "foreign")
	if err := os.Symlink("elsewhere", foreign); err != nil {
		t.Fatal(err)
	}
	retry := f.mustPublish(file("a", "third"))
	if retry.Changed || retry.Generation != current.Generation || !slices.Equal(retry.Pruned, []string{"b"}) {
		t.Fatalf("identical retry did not complete cleanup: %+v", retry)
	}
	if _, err := os.Lstat(filepath.Join(f.dir, "b")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("removed managed symlink survived: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.dir, genDir, obsolete.Generation)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("obsolete generation survived: %v", err)
	}
	assertOnlyGenerations(t, f.dir, current.Generation, previous.Generation)
	if target, err := os.Readlink(foreign); err != nil || target != "elsewhere" {
		t.Fatal("cleanup touched foreign symlink")
	}
	if got := f.read("a"); got != "third" {
		t.Fatalf("current generation was disturbed: %q", got)
	}
}

func TestIdenticalLegacyGenerationMigratesWithoutPredecessorGuess(t *testing.T) {
	f := newFixture(t)
	legacy := f.mustPublish(file("a", "unchanged"))
	if err := os.Remove(filepath.Join(f.dir, genDir, legacy.Generation, previousName)); err != nil {
		t.Fatal(err)
	}
	migrated := f.mustPublish(file("a", "unchanged"))
	if !migrated.Changed || migrated.Generation == legacy.Generation || f.read("a") != "unchanged" {
		t.Fatalf("legacy migration = %+v", migrated)
	}
	assertOnlyGenerations(t, f.dir, migrated.Generation, legacy.Generation)
	if retry := f.mustPublish(file("a", "unchanged")); retry.Changed {
		t.Fatal("legacy migration repeated on next identical publication")
	}
}
