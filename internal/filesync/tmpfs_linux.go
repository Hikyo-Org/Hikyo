//go:build linux

package filesync

import "github.com/Hikyo-Org/hikyo/internal/compose"

// isTmpfs is the Compose path's statfs check (TMPFS_MAGIC or RAMFS_MAGIC).
func isTmpfs(dir string) (bool, error) { return compose.IsTmpfs(dir) }
