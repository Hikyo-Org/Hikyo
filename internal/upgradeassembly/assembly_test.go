//go:build darwin || linux

package upgradeassembly

import (
	"context"
	"errors"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicationNeverReplacesAnOutput(t *testing.T) {
	// An empty existing directory must not be overwritten either, even when it
	// appears between the initial absence check and the final atomic rename.
	stage, output := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, "sentinel"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publishDirectory(stage, output); err == nil {
		t.Fatal("replaced existing directory")
	}
	if _, err := os.Stat(filepath.Join(stage, "sentinel")); err != nil {
		t.Fatal(err)
	}
}

func TestAssemblyRejectsUnboundedInventoriesBeforeReadingInputs(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		releases, nightlies, bridges int
	}{
		{"missing release", 0, 0, 0},
		{"too many releases", upgradecompat.MaxReleases + 1, 0, 0},
		{"combined releases exceed bound", upgradecompat.MaxReleases, 1, 0},
		{"too many bridges", 1, 0, upgradecompat.MaxEdges + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{
				SnapshotDirectory: "does-not-exist", KeysDirectory: "does-not-exist",
				OutputDirectory: filepath.Join(t.TempDir(), "bundle"),
				Releases:        make([]string, tc.releases), Nightlies: make([]string, tc.nightlies), Bridges: make([]string, tc.bridges),
			}
			err := Assemble(t.Context(), opts)
			if err == nil || errors.Is(err, os.ErrNotExist) {
				t.Fatalf("inventory bound was not enforced before file reads: %v", err)
			}
			if _, err := os.Lstat(opts.OutputDirectory); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid inventory published: %v", err)
			}
		})
	}
}

func TestAssemblyReturnsCancellationBeforeReadingInputs(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Assemble(ctx, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
}
