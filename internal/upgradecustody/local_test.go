package upgradecustody

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalCustodyUsesRootWrappingAndPreservesIdentity(t *testing.T) {
	directory := testDirectory(t)
	root := bytes.Repeat([]byte{0x83}, 32)
	vault, err := CreateLocal(directory, root, instance)
	if err != nil {
		t.Fatal(err)
	}
	public, recipient := vault.PublicKey(), vault.Recipient()
	vault.Close()
	raw, err := os.ReadFile(filepath.Join(directory, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, root) || bytes.Contains(raw, []byte(instance)) || bytes.Contains(raw, public) {
		t.Fatal("private custody persisted without encryption")
	}
	vault, err = OpenLocal(directory, root, instance)
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	if !bytes.Equal(vault.PublicKey(), public) || vault.Recipient() != recipient {
		t.Fatal("restart changed custody identity")
	}
	if _, err := OpenLocal(directory, bytes.Repeat([]byte{0x84}, 32), instance); !errors.Is(err, ErrUnlock) {
		t.Fatalf("wrong root accepted: %v", err)
	}
	if _, err := OpenLocal(directory, root, "ins_33333333333333333333333333333333"); err == nil {
		t.Fatal("different installation accepted")
	}
	if _, err := CreateLocal(directory, root, instance); err == nil {
		t.Fatal("existing custody replaced")
	}
}

func TestLocalCustodyRefusesWeakPermissionsAndInvalidRoot(t *testing.T) {
	directory := testDirectory(t)
	if _, err := CreateLocal(directory, []byte("short"), instance); err == nil {
		t.Fatal("invalid root accepted")
	}
	root := bytes.Repeat([]byte{0x85}, 32)
	vault, err := CreateLocal(directory, root, instance)
	if err != nil {
		t.Fatal(err)
	}
	vault.Close()
	if err := os.Chmod(filepath.Join(directory, fileName), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLocal(directory, root, instance); err == nil {
		t.Fatal("publicly readable custody accepted")
	}
}

func TestLocalEnrollmentResumesAndBindsExactlyOnce(t *testing.T) {
	directory := testDirectory(t)
	root := bytes.Repeat([]byte{0x86}, 32)
	public, err := PrepareLocalEnrollment(directory, root)
	if err != nil {
		t.Fatal(err)
	}
	again, err := PrepareLocalEnrollment(directory, root)
	if err != nil || !bytes.Equal(public, again) {
		t.Fatalf("fresh enrollment retry changed keys: %v", err)
	}
	if _, err := BindLocalEnrollment(directory, root, instance, []byte("different public key")); err == nil {
		t.Fatal("wrong installed operator bound")
	}
	vault, err := BindLocalEnrollment(directory, root, instance, public)
	if err != nil {
		t.Fatal(err)
	}
	recipient := vault.Recipient()
	vault.Close()
	vault, err = BindLocalEnrollment(directory, root, instance, public)
	if err != nil {
		t.Fatal(err)
	}
	if vault.Recipient() != recipient {
		t.Fatal("binding retry changed backup identity")
	}
	vault.Close()
	if _, err := BindLocalEnrollment(directory, root, "ins_33333333333333333333333333333333", public); err == nil {
		t.Fatal("bound custody adopted by different installation")
	}
	if _, err := PrepareLocalEnrollment(directory, root); err == nil {
		t.Fatal("bound custody reverted to fresh enrollment")
	}
}
