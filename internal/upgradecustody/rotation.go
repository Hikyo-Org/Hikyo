package upgradecustody

import (
	"bytes"
	"errors"
	"os"
)

// StageRootRotation seals the existing record under nextRoot into a staged file
// beside the live vault, and replaces the record's root escrow with nextRoot.
// The live vault stays wrapped under currentRoot, so the instance remains
// operable under either root while the master is dual-wrapped: Open falls back
// to the staged file, so no phase of the rotation strands the keys. Staging is
// idempotent and overwrites an earlier staged file, after completing the
// promotion of one whose commit was interrupted. directory must be local
// (runtime-owned) custody; the instance must come from inspection of the
// installation.
func StageRootRotation(directory string, currentRoot, nextRoot []byte, instance string) error {
	if len(nextRoot) != 32 {
		return errors.New("operator custody requires a 32-byte root escrow")
	}
	current, err := RootKeySecret(currentRoot)
	if err != nil {
		return err
	}
	defer clear(current)
	next, err := RootKeySecret(nextRoot)
	if err != nil {
		return err
	}
	defer clear(next)
	owner := os.Geteuid()
	dir, err := custodyDirectory(directory, false, owner)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	// A previous rotation whose commit did not complete leaves the live file
	// under the retired root and only the staged file under currentRoot. Finish
	// that promotion first; it is the only copy that opens under currentRoot.
	if live, err := checkRootRotation(directory, currentRoot, instance); err != nil {
		return err
	} else if !live {
		if err := promoteStaged(dir); err != nil {
			return err
		}
	}
	r, err := unsealFile(directory, fileName, current, instance, owner)
	if err != nil {
		return err
	}
	defer r.clear()
	clear(r.RootKey)
	r.RootKey = bytes.Clone(nextRoot)
	ciphertext, err := seal(r, next)
	if err != nil {
		return err
	}
	return publish(dir, stagedFileName, ciphertext, true)
}

// CheckRootRotation reports whether custody opens under root, live or staged,
// without changing anything. Callers run it before an irreversible step.
func CheckRootRotation(directory string, root []byte, instance string) error {
	_, err := checkRootRotation(directory, root, instance)
	return err
}

// checkRootRotation reports whether the live file already opens under root.
func checkRootRotation(directory string, root []byte, instance string) (live bool, err error) {
	secret, err := RootKeySecret(root)
	if err != nil {
		return false, err
	}
	defer clear(secret)
	owner := os.Geteuid()
	r, err := unsealFile(directory, fileName, secret, instance, owner)
	r.clear()
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, ErrUnlock) {
		return false, err
	}
	staged, stagedErr := unsealFile(directory, stagedFileName, secret, instance, owner)
	defer staged.clear()
	// The escrow must be the root that opened it: a stale staged file from an
	// abandoned rotation is never promoted into a vault that would hold a
	// different root.
	if stagedErr != nil || !bytes.Equal(staged.RootKey, root) {
		return false, err
	}
	return false, nil
}

// CommitRootRotation makes the vault open under root alone and retires the
// staged file, after the keyring retired the old root wrapper. It is idempotent:
// a live vault that already opens under root is left as is. It fails, changing
// nothing, when neither file opens under root.
func CommitRootRotation(directory string, root []byte, instance string) error {
	live, err := checkRootRotation(directory, root, instance)
	if err != nil {
		return err
	}
	dir, err := custodyDirectory(directory, false, os.Geteuid())
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	if live {
		return discardStaged(dir)
	}
	return promoteStaged(dir)
}
