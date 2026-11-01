package securityfeed

import (
	"errors"
	"os"
)

func checkOwner(os.FileInfo) error {
	return errors.New("security feed local authority requires supported POSIX ownership; Windows is not enrolled")
}

func checkAncestorOwner(info os.FileInfo) error { return checkOwner(info) }
