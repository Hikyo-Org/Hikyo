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

func TestRootRotationStagingKeepsCustodyOpenUnderEitherRoot(t *testing.T) {
	directory := testDirectory(t)
	oldRoot, newRoot := bytes.Repeat([]byte{0x91}, 32), bytes.Repeat([]byte{0x92}, 32)
	vault, err := CreateLocal(directory, oldRoot, instance)
	if err != nil {
		t.Fatal(err)
	}
	public, recipient := vault.PublicKey(), vault.Recipient()
	vault.Close()
	if err := StageRootRotation(directory, newRoot, newRoot, instance); !errors.Is(err, ErrUnlock) {
		t.Fatalf("staging from a root that does not open the vault = %v", err)
	}
	if err := StageRootRotation(directory, oldRoot, newRoot, instance); err != nil {
		t.Fatal(err)
	}
	// Re-staging is idempotent.
	if err := StageRootRotation(directory, oldRoot, newRoot, instance); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ root, escrow []byte }{
		"retired root before the operator swaps": {oldRoot, oldRoot},
		"new root after the operator swaps":      {newRoot, newRoot},
	} {
		v, err := OpenLocal(directory, tc.root, instance)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(v.PublicKey(), public) || v.Recipient() != recipient || !bytes.Equal(v.RootKey(), tc.escrow) {
			t.Fatalf("%s: identity changed or root escrow is not the opening root", name)
		}
		v.Close()
	}
	if _, err := OpenLocal(directory, bytes.Repeat([]byte{0x93}, 32), instance); !errors.Is(err, ErrUnlock) {
		t.Fatalf("unrelated root accepted: %v", err)
	}
}

func TestCommitRootRotationPromotesRetiresAndIsIdempotent(t *testing.T) {
	directory := testDirectory(t)
	oldRoot, newRoot := bytes.Repeat([]byte{0x94}, 32), bytes.Repeat([]byte{0x95}, 32)
	vault, err := CreateLocal(directory, oldRoot, instance)
	if err != nil {
		t.Fatal(err)
	}
	public := vault.PublicKey()
	vault.Close()
	if err := StageRootRotation(directory, oldRoot, newRoot, instance); err != nil {
		t.Fatal(err)
	}
	// The operator has not installed the new root yet: nothing may change.
	if err := CommitRootRotation(directory, bytes.Repeat([]byte{0x96}, 32), instance); !errors.Is(err, ErrUnlock) {
		t.Fatalf("commit under an unrelated root = %v", err)
	}
	for i := range 2 {
		if err := CommitRootRotation(directory, newRoot, instance); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(directory, stagedFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file survived commit: %v", err)
	}
	v, err := OpenLocal(directory, newRoot, instance)
	if err != nil || !bytes.Equal(v.PublicKey(), public) || !bytes.Equal(v.RootKey(), newRoot) {
		t.Fatalf("committed vault = %v", err)
	}
	v.Close()
	if _, err := OpenLocal(directory, oldRoot, instance); !errors.Is(err, ErrUnlock) {
		t.Fatalf("retired root still opens the committed vault: %v", err)
	}
	// A crash between promotion and the caller's next step is a commit repeat.
	if err := CommitRootRotation(directory, newRoot, instance); err != nil {
		t.Fatal(err)
	}
}

func TestCommitRootRotationNeverPromotesAStaleStagedVault(t *testing.T) {
	directory := testDirectory(t)
	oldRoot, newRoot := bytes.Repeat([]byte{0x97}, 32), bytes.Repeat([]byte{0x98}, 32)
	vault, err := CreateLocal(directory, oldRoot, instance)
	if err != nil {
		t.Fatal(err)
	}
	vault.Close()
	if err := StageRootRotation(directory, oldRoot, newRoot, instance); err != nil {
		t.Fatal(err)
	}
	// A staged file whose escrow is not the root that opens it (a stale or
	// foreign copy) is refused.
	staged, err := unsealFile(directory, stagedFileName, mustSecret(t, newRoot), instance, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	staged.RootKey = bytes.Repeat([]byte{0x99}, 32)
	ciphertext, err := seal(staged, mustSecret(t, newRoot))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := custodyDirectory(directory, false, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	if err := publish(dir, stagedFileName, ciphertext, true); err != nil {
		t.Fatal(err)
	}
	if err := CommitRootRotation(directory, newRoot, instance); !errors.Is(err, ErrUnlock) {
		t.Fatalf("stale staged vault promoted: %v", err)
	}
	if err := CheckRootRotation(directory, newRoot, instance); !errors.Is(err, ErrUnlock) {
		t.Fatalf("stale staged vault passed the check: %v", err)
	}
}

func mustSecret(t *testing.T, root []byte) []byte {
	t.Helper()
	secret, err := RootKeySecret(root)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}
