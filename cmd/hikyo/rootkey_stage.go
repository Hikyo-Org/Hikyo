package main

import (
	"fmt"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

const rootKeyStageMode = "__hikyo-stage-root-key"

func runRootKeyStageMode(args []string) (bool, int) {
	if len(args) == 0 || args[0] != rootKeyStageMode {
		return false, 0
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "hikyo internal root-key stage: no arguments accepted")
		return true, 2
	}
	if err := crypto.StageRootKey(
		"/run/hikyo-root-key-source/root-key",
		"/run/hikyo-root-key/root-key",
	); err != nil {
		fmt.Fprintln(os.Stderr, "hikyo internal root-key stage:", err)
		return true, 1
	}
	return true, 0
}
