//go:build unix

package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func validateConfigCustody(path string) error {
	clean := filepath.Clean(path)
	for current := clean; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("updater: inspect config custody %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("updater: config custody path %q must not be a symlink", current)
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("updater: config custody path %q must not be group/world writable", current)
		}
		if current == clean {
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != 0 {
				return fmt.Errorf("updater: config %q must be owned by root", clean)
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}
