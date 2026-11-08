//go:build linux || darwin

package securitydeployment

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledAuthorityRejectsReplaceableProgramsAndCredentials(t *testing.T) {
	directory := privateDirectory(t)
	program := filepath.Join(directory, "installed-program")
	if err := os.WriteFile(program, []byte("installed executable fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installedAuthority(program, true, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(program, 0775); err != nil {
		t.Fatal(err)
	}
	if err := installedAuthority(program, true, false); err == nil {
		t.Fatal("group-writable executable accepted")
	}
	if err := os.Chmod(program, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(directory, "program-alias")
	if err := os.Symlink(program, alias); err != nil {
		t.Fatal(err)
	}
	if err := installedAuthority(alias, true, false); err == nil {
		t.Fatal("replaceable executable alias accepted")
	}
	credential := filepath.Join(directory, "credentials")
	if err := os.WriteFile(credential, []byte("protected fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	// WriteFile's mode is masked by the process umask, which a job queue may tighten.
	if err := os.Chmod(credential, 0644); err != nil {
		t.Fatal(err)
	}
	err := installedAuthority(credential, false, true)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), credential) || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("public credential refusal does not name the path and mode: %v", err)
	}
	if err := os.Chmod(credential, 0600); err != nil {
		t.Fatal(err)
	}
	if err := installedAuthority(credential, false, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0770); err != nil {
		t.Fatal(err)
	}
	if err := installedAuthority(credential, false, true); err == nil {
		t.Fatal("replaceable credential ancestry accepted")
	}
}
