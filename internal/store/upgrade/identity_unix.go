//go:build !windows

package upgrade

import (
	"errors"
	"os"
	"syscall"
)

func requireSingleLink(_ string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return errors.New("multiply-linked or unrecognized database file refused")
	}
	return nil
}
