//go:build linux || darwin

package securitydeployment

import (
	"os"
	"path/filepath"
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
	if err := installedAuthority(credential, false, true); err == nil {
		t.Fatal("public credential input accepted")
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
