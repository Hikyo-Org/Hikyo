package shell

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// publishing is read-held by every publication; FinishDownloads takes it.
var publishing sync.RWMutex

// FinishDownloads waits for the publications in progress and blocks new ones.
// Call it once, at shutdown: a quit during a copy fallback would otherwise leave
// a partly written file under its final name.
func FinishDownloads() { publishing.Lock() }

// PublishDownload gives a staged download its final name and returns the name
// used. If the save panel already saw a file at final (replaceApproved), the
// person approved replacing it and the file is replaced. Otherwise nothing may
// be replaced: a file that appeared after the panel closed is kept, and the
// download takes the next free "name (2).ext". A hard link fails when the name
// exists, so the check and the publication are one step.
func PublishDownload(temp, final string, replaceApproved bool) (string, error) {
	publishing.RLock()
	defer publishing.RUnlock()
	if replaceApproved {
		return final, os.Rename(temp, final)
	}
	ext := filepath.Ext(final)
	stem := strings.TrimSuffix(final, ext)
	for i := 1; i <= 100; i++ {
		name := final
		if i > 1 {
			name = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		err := os.Link(temp, name)
		switch {
		case err == nil:
			_ = os.Remove(temp) // the file is saved; the caller removes a leftover temp
			return name, nil
		case errors.Is(err, fs.ErrExist):
			continue
		}
		// A volume without hard links (exFAT, some network shares): claim the name
		// with an exclusive create, which also fails when the name exists, then fill
		// it in. The claimed file is marked before it gets any content.
		if err := copyExclusive(temp, name); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return "", err
		}
		_ = os.Remove(temp)
		return name, nil
	}
	return "", errors.New("no free file name")
}

// copyExclusive creates name only if it does not exist, marks it as downloaded,
// and copies temp into it. A failure removes the file it created.
func copyExclusive(temp, name string) (err error) {
	src, err := os.Open(temp)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if err = setQuarantine(name); err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}
