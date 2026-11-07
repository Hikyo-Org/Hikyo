//go:build unix && !linux

package crypto

import (
	"errors"
	"os"
)

func aclGrantsOnlyCurrentUser(*os.File) error {
	return errors.New("an ACL-delivered key is accepted only on Linux")
}
