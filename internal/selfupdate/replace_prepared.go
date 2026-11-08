package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// replaceable resolves the executable an update would replace, then lets
// authorize veto it. Callers use CheckExecutable first so a refusal costs no
// download.
func replaceable(executablePath func() (string, error), authorize func(path string) error) (string, os.FileMode, error) {
	target, mode, err := resolveExecutable(executablePath)
	if err != nil {
		return "", 0, err
	}
	if err := authorize(target); err != nil {
		return "", 0, fmt.Errorf("selfupdate: %s is not replaceable: %w", target, err)
	}
	return target, mode, nil
}

// CheckExecutable reports whether the running executable passes the same
// resolution and ownership checks as an update, plus authorize.
func CheckExecutable(authorize func(path string) error) error {
	_, _, err := replaceable(os.Executable, authorize)
	return err
}

// ReplaceWithPrepared replaces the running executable with the binary a
// PrepareRelease call extracted in this process. The digest recorded at
// extraction is rechecked against the file, so a descriptor loaded from disk
// must go through VerifyCachedRelease first.
func (i *Installer) ReplaceWithPrepared(ctx context.Context, prepared PreparedNightly, authorize func(path string) error) error {
	if i == nil || i.executablePath == nil {
		return errors.New("selfupdate: installer is not configured")
	}
	target, mode, err := replaceable(i.executablePath, authorize)
	if err != nil {
		return err
	}
	binary, err := readNightlyFile(prepared.BinaryPath, maxBinaryBytes)
	if err != nil {
		return err
	}
	if prepared.BinarySHA256.Validate() != nil || releaseidentity.Hash(binary) != prepared.BinarySHA256 {
		return errors.New("selfupdate: prepared executable changed after verification")
	}
	if err := replaceBinary(ctx, target, binary, mode); err != nil {
		return fmt.Errorf("selfupdate: replace %s: %w", target, err)
	}
	return nil
}
