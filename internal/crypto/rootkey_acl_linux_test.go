//go:build linux

package crypto

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func posixACL(entries ...[3]uint32) []byte {
	raw := binary.LittleEndian.AppendUint32(nil, 2)
	for _, entry := range entries {
		raw = binary.LittleEndian.AppendUint16(raw, uint16(entry[0]))
		raw = binary.LittleEndian.AppendUint16(raw, uint16(entry[1]))
		raw = binary.LittleEndian.AppendUint32(raw, entry[2])
	}
	return raw
}

// systemd 252 and later hand a User= service its credential through an ACL, so the
// file shows the mask as its group bits.
func TestRootKeyFileAcceptsOnlyAnACLNamingTheCurrentUser(t *testing.T) {
	uid := uint32(os.Geteuid())
	const none, read, void = 0, 4, 0xffffffff
	cases := map[string]struct {
		acl  []byte
		want bool
	}{
		"mask and the current user": {posixACL([3]uint32{1, read, void}, [3]uint32{2, read, uid}, [3]uint32{4, none, void}, [3]uint32{0x10, read, void}, [3]uint32{0x20, none, void}), true},
		"group can read":            {posixACL([3]uint32{1, read, void}, [3]uint32{4, read, void}, [3]uint32{0x10, read, void}, [3]uint32{0x20, none, void}), false},
		"another user":              {posixACL([3]uint32{1, read, void}, [3]uint32{2, read, uid + 1}, [3]uint32{4, none, void}, [3]uint32{0x10, read, void}, [3]uint32{0x20, none, void}), false},
		"a named group":             {posixACL([3]uint32{1, read, void}, [3]uint32{4, none, void}, [3]uint32{8, read, 1}, [3]uint32{0x10, read, void}, [3]uint32{0x20, none, void}), false},
		"others can read":           {posixACL([3]uint32{1, read, void}, [3]uint32{2, read, uid}, [3]uint32{4, none, void}, [3]uint32{0x10, read, void}, [3]uint32{0x20, read, void}), false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if err := aclNamesOnlyUser(c.acl, uid); (err == nil) != c.want {
				t.Fatalf("ACL decision = %v, want accepted=%t", err, c.want)
			}
			path := filepath.Join(t.TempDir(), "credential")
			if err := os.WriteFile(path, []byte(EncodeRootKey(make([]byte, KeySize))), 0o400); err != nil {
				t.Fatal(err)
			}
			// Only a filesystem without ACLs may skip: the kernel refuses a misordered fixture with EINVAL.
			if err := unix.Setxattr(path, "system.posix_acl_access", c.acl, 0); errors.Is(err, unix.EOPNOTSUPP) {
				t.Skip("filesystem has no POSIX ACLs:", err)
			} else if err != nil {
				t.Fatal("set ACL fixture:", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal("stat ACL fixture:", err)
			}
			if info.Mode().Perm()&0o077 == 0 {
				t.Fatal("the ACL did not change the permission bits")
			}
			_, err = readRootKeyFile(path)
			if c.want && err != nil {
				t.Fatal("ACL-delivered credential refused:", err)
			}
			if !c.want && (!errors.Is(err, ErrRootKeyPerms) || !strings.Contains(err.Error(), "POSIX ACL")) {
				t.Fatal("unsafe ACL admitted, or refused without its reason:", err)
			}
		})
	}
}
