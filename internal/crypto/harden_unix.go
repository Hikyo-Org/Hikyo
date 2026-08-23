//go:build unix

package crypto

import (
	"fmt"
	"syscall"
)

// HardenProcess applies the key-material hardening the encryption-model ADR fixes
// for every process mode that loads the keyring: no core dumps anywhere, and on
// Linux additionally no same-uid ptrace attach or /proc/<pid>/mem read
// (PR_SET_DUMPABLE=0). Called before any key material enters the process.
func HardenProcess() error {
	if err := syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 0, Max: 0}); err != nil {
		return fmt.Errorf("crypto: RLIMIT_CORE=0: %w", err)
	}
	return setNotDumpable()
}
