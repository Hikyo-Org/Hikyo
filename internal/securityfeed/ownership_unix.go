//go:build !windows

package securityfeed

import (
	"errors"
	"os"
	"syscall"
)

func checkOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("security feed authority must be owned by the current operator")
	}
	return nil
}

func checkAncestorOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) {
		return errors.New("security feed authority has an ancestor owned by another operator")
	}
	return nil
}
