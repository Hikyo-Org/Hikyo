//go:build unix && !linux

package crypto

import "os"

func aclGrantsOnlyCurrentUser(*os.File) bool { return false }
