//go:build windows

package filesync

import (
	"fmt"
	"io/fs"
	"os"
)

// Windows destinations are out of scope (#164): the publication relies on
// POSIX symlink semantics, directory locks and ownership. Every entry point
// refuses by name rather than half-working.

func lockDirectory(string, fs.FileInfo) (*os.File, error) {
	return nil, fmt.Errorf("%w: file-sync destinations are Linux and macOS only", ErrUnsupported)
}

func unlockDirectory(*os.File) error { return nil }

func ownedAs(fs.FileInfo, int, int) bool { return false }

func ownerIDs(fs.FileInfo) (int, int) { return -1, -1 }

func syncRootDir(*os.Root, string) error { return nil }
