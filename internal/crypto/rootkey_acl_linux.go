//go:build linux

package crypto

import (
	"encoding/binary"
	"os"

	"golang.org/x/sys/unix"
)

// aclGrantsOnlyCurrentUser accepts what systemd 252 and later create for LoadCredential
// under a service User=: a file the user can read only through an ACL entry. Its group
// permission bits then show the ACL mask (mode 0440), which the plain mode check
// would refuse. The ACL must name no one but the owner and the current user.
func aclGrantsOnlyCurrentUser(file *os.File) bool {
	raw := make([]byte, 4+8*32)
	n, err := unix.Fgetxattr(int(file.Fd()), "system.posix_acl_access", raw)
	if err != nil || n < 4+8 || (n-4)%8 != 0 || binary.LittleEndian.Uint32(raw) != 2 {
		return false
	}
	const (
		userObj, user, groupObj, group, mask, other = 0x01, 0x02, 0x04, 0x08, 0x10, 0x20
	)
	for entry := raw[4:n]; len(entry) >= 8; entry = entry[8:] {
		tag, perm, id := binary.LittleEndian.Uint16(entry), binary.LittleEndian.Uint16(entry[2:]), binary.LittleEndian.Uint32(entry[4:])
		switch tag {
		case userObj, mask:
		case user:
			if id != uint32(os.Geteuid()) {
				return false
			}
		case groupObj, other:
			if perm != 0 {
				return false
			}
		case group:
			return false
		default:
			return false
		}
	}
	return true
}
