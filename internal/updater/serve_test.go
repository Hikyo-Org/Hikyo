package updater

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHelperStartupRefusesBeforeTouchingConfigSocketOrQueuedJournal(t *testing.T) {
	dir := t.TempDir()
	journal := &Journal{Path: filepath.Join(dir, "jobs.json")}
	if err := journal.Create(Job{ID: "upd_old", State: StateQueued, Phase: PhaseQueued}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(journal.Path)
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "updater.sock")
	if err := os.WriteFile(socket, []byte("operator data"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"--config", filepath.Join(dir, "missing.json")}} {
		if err := Run(t.Context(), args, nil); !errors.Is(err, ErrRemoteApplyDisabled) {
			t.Fatalf("helper entry error=%v, want disabled before reading config", err)
		}
	}
	after, err := os.ReadFile(journal.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("historical journal changed: %v", err)
	}
	data, err := os.ReadFile(socket)
	if err != nil || string(data) != "operator data" {
		t.Fatalf("socket path changed: %v", err)
	}
}
