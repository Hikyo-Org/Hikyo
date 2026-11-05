//go:build ios

package main

import (
	"log"
	"os"
	"path/filepath"
	"syscall"
)

// The simulator keeps no stdout or stderr, so the log goes to a file in the app container, read from the host with `simctl get_app_container ... data`.
func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(home, "Documents", "spike.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_ = syscall.Dup2(int(f.Fd()), 1)
	_ = syscall.Dup2(int(f.Fd()), 2)
	log.SetOutput(f)
}
