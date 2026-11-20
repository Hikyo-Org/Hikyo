package shell

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PublishDownload gives a staged download its final name and returns the name
// used. If the save panel already saw a file at final (replaceApproved), the
// person approved replacing it and the file is replaced. Otherwise nothing may
// be replaced: a file that appeared after the panel closed is kept, and the
// download takes the next free "name (2).ext". A hard link fails when the name
// exists, so the check and the publication are one step.
func PublishDownload(temp, final string, replaceApproved bool) (string, error) {
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
			return name, os.Remove(temp)
		case errors.Is(err, fs.ErrExist):
			continue
		}
		// shortcut: a volume without hard links (exFAT, some network shares)
		// falls back to a checked rename, which has a small race; upgrade if
		// those volumes matter.
		if _, serr := os.Lstat(name); serr == nil {
			continue
		}
		return name, os.Rename(temp, name)
	}
	return "", errors.New("no free file name")
}
