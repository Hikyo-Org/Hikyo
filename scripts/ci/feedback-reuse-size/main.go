// feedback-reuse-size bounds the expanded size and extraction work of the
// trusted compile-only seed before it is published to short feedback jobs.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const maxBytes int64 = 1536 * 1024 * 1024
const maxFiles = 12000

func inspect(root string, byteLimit int64, fileLimit int) (int64, int, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return 0, 0, err
	}
	if !info.IsDir() {
		return 0, 0, fmt.Errorf("seed root must be a real directory")
	}
	var total int64
	files := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("seed contains non-regular entry %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files++
		if info.Size() > byteLimit-total || files > fileLimit {
			return fmt.Errorf("feedback seed exceeds %d bytes or %d files; narrow the compile seed before publishing", byteLimit, fileLimit)
		}
		total += info.Size()
		return nil
	})
	if err == nil && files == 0 {
		err = fmt.Errorf("feedback seed is empty")
	}
	return total, files, err
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: feedback-reuse-size DIRECTORY")
		os.Exit(2)
	}
	bytes, files, err := inspect(os.Args[1], maxBytes, maxFiles)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("feedback seed: %d expanded bytes, %d files (limits %d bytes, %d files)\n", bytes, files, maxBytes, maxFiles)
}
