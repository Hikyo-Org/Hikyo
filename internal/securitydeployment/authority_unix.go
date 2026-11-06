//go:build linux || darwin

package securitydeployment

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// installedAuthority refuses executable/config paths writable or replaceable by
// a less privileged account. Installers supply resolved paths, not search names
// or symlinks through application-writable directories.
func installedAuthority(path string, executable, private bool) error {
	if !safePath(path) {
		return ErrInvalid
	}
	current := string(filepath.Separator)
	components := strings.Split(strings.TrimPrefix(path, current), current)
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("security deployment: installed authority unavailable: %w", err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (stat.Uid != 0 && int(stat.Uid) != os.Geteuid()) || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s must be owned by root and not be a symbolic link", ErrInvalid, current)
		}
		if index < len(components)-1 {
			if !info.IsDir() || (info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0) {
				return fmt.Errorf("%w: parent %s of %s must be a directory that only its owner can write", ErrInvalid, current, path)
			}
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || (private && info.Mode().Perm() != 0600) || (executable && info.Mode().Perm()&0111 == 0) {
			return fmt.Errorf("%w: %s must be a regular file with %s, found mode %04o", ErrInvalid, path, installedMode(executable, private), info.Mode().Perm())
		}
	}
	return nil
}

func installedMode(executable, private bool) string {
	switch {
	case private:
		return "exactly mode 0600"
	case executable:
		return "an executable mode that is not group or world writable"
	}
	return "a mode that is not group or world writable"
}

func configureCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}
