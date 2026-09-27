package repscan

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// skipReason names why an item was not scanned. Every skip is counted in the
// summary so reduced coverage is visible, never silent.
type skipReason int

const (
	scanIt skipReason = iota
	skipBinary
	skipArchive
	skipSymlink
	skipOversize
	skipNotRegular
	skipVanished
	skipSubmodule
)

// Skipped counts the items a run did not scan, by reason.
type Skipped struct {
	Binary     int `json:"binary"`
	Archive    int `json:"archive"`
	Symlink    int `json:"symlink"`
	Oversize   int `json:"oversize"`
	NotRegular int `json:"not_regular"`
	Vanished   int `json:"vanished"`
	Submodule  int `json:"submodule"`
	Excluded   int `json:"excluded"`
}

func (s *Skipped) count(r skipReason) {
	switch r {
	case skipBinary:
		s.Binary++
	case skipArchive:
		s.Archive++
	case skipSymlink:
		s.Symlink++
	case skipOversize:
		s.Oversize++
	case skipNotRegular:
		s.NotRegular++
	case skipVanished:
		s.Vanished++
	case skipSubmodule:
		s.Submodule++
	}
}

// Total is the number of skipped items.
func (s Skipped) Total() int {
	return s.Binary + s.Archive + s.Symlink + s.Oversize + s.NotRegular + s.Vanished + s.Submodule + s.Excluded
}

// sniffBytes is how much of a file the binary check inspects.
const sniffBytes = 8 << 10

// archiveExtensions are skipped by name: archives are never extracted, so a
// secret inside one is out of scope and the skip is counted.
var archiveExtensions = []string{
	".zip", ".jar", ".war", ".ear", ".apk", ".aar", ".whl", ".nupkg",
	".tar", ".tgz", ".gz", ".bz2", ".xz", ".zst", ".lz4", ".7z", ".rar",
}

// archiveMagic are skipped by content, whatever the name says.
var archiveMagic = [][]byte{
	[]byte("PK\x03\x04"), []byte("PK\x05\x06"), // zip family
	[]byte("\x1f\x8b"),           // gzip
	[]byte("BZh"),                // bzip2
	[]byte("\xfd7zXZ\x00"),       // xz
	[]byte("7z\xbc\xaf\x27\x1c"), // 7z
	[]byte("Rar!\x1a\x07"),       // rar
	[]byte("\x28\xb5\x2f\xfd"),   // zstd
}

// classify decides whether content is scanned as text.
func classify(name string, data []byte) skipReason {
	lower := strings.ToLower(path.Base(name))
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(lower, ext) {
			return skipArchive
		}
	}
	for _, magic := range archiveMagic {
		if bytes.HasPrefix(data, magic) {
			return skipArchive
		}
	}
	if len(data) > 262 && string(data[257:262]) == "ustar" {
		return skipArchive
	}
	head := data
	if len(head) > sniffBytes {
		head = head[:sniffBytes]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return skipBinary
	}
	return scanIt
}

// readWithin reads rel through root without following a symlink at the final
// component and without escaping root through any intermediate one.
//
// Lstat refuses a symlink up front; os.Root refuses any path that would leave
// the root, including one swapped in after the Lstat; the opened descriptor is
// re-checked as a regular file; and the read is capped one byte past the
// budget so a file that grows between stat and read is counted oversize, not
// truncated.
func readWithin(root *os.Root, rel string, maxBytes int64) ([]byte, skipReason, error) {
	info, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, skipVanished, nil
		}
		return nil, 0, err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, skipSymlink, nil
	case !info.Mode().IsRegular():
		return nil, skipNotRegular, nil
	case info.Size() > maxBytes:
		return nil, skipOversize, nil
	}
	f, err := root.Open(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, skipVanished, nil
		}
		// Only a re-Lstat that confirms a swap or a disappearance is a skip;
		// any other failure (EMFILE, EIO, permission) leaves the file unread
		// and must stay a refusal.
		now, lerr := root.Lstat(rel)
		switch {
		case lerr == nil && now.Mode()&fs.ModeSymlink != 0:
			return nil, skipSymlink, nil
		case errors.Is(lerr, fs.ErrNotExist):
			return nil, skipVanished, nil
		}
		return nil, 0, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !opened.Mode().IsRegular() {
		return nil, skipNotRegular, nil
	}
	if !os.SameFile(info, opened) {
		// Replaced between Lstat and Open (a symlink that stays inside the
		// root is still a symlink the walk promised not to follow).
		return nil, skipSymlink, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(data)) > maxBytes {
		return nil, skipOversize, nil
	}
	return data, scanIt, nil
}
