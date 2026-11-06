//go:build unix

package securitymaintenance

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPublicStatusDirectoryStaysTraversableUnderTheUnitUmask(t *testing.T) {
	previous := syscall.Umask(0o077)
	defer syscall.Umask(previous)
	e, _, _ := fixtureEngine(t)
	shared := t.TempDir()
	if err := os.Chmod(shared, 0o750); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(shared, "status")
	e.Store.PublicStatusFile = filepath.Join(created, "status.json")
	if _, err := e.RunOnce(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(created); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("created directory mode = %v, err = %v", info.Mode(), err)
	}
	// A pre-existing directory is not widened.
	e.Store.PublicStatusFile = filepath.Join(shared, "status.json")
	if _, err := e.RunOnce(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(shared); err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("existing directory mode = %v, err = %v", info.Mode(), err)
	}
}

func TestPublicStatusNeverReplacesThePrivateStateThroughAnAlias(t *testing.T) {
	e, _, _ := fixtureEngine(t)
	enrollFixture(t, e)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(e.Store.directory, alias); err != nil {
		t.Fatal(err)
	}
	e.Store.PublicStatusFile = filepath.Join(alias, "status.json")
	if _, err := e.RunOnce(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if saved, err := e.Store.Load(); err != nil || saved.Enrollment == nil {
		t.Fatalf("public status replaced the private state through an alias: %+v %v", saved, err)
	}
}
