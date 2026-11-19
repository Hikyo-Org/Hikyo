//go:build unix

package compose

import (
	"fmt"
	"os"
	"syscall"
)

// fsyncRootPath fsyncs a directory named relative to root (name "." is the root
// itself), so a create/rename beneath it is durable. Used for the runtime-dir
// generation transaction, which is confined to an os.Root.
func fsyncRootPath(root *os.Root, name string) error {
	d, err := root.Open(name)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ownerOf returns the uid/gid of an existing file so a rewrite can preserve
// them. Returns (-1, -1) when it cannot be determined.
func ownerOf(fi os.FileInfo) (uid, gid int) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, -1
	}
	return int(st.Uid), int(st.Gid)
}

// effectiveUID is the uid that must own the runtime dir; a seam so tests can
// exercise the foreign-owner refusal without root.
var effectiveUID = os.Geteuid

// checkRuntimeCustody refuses a runtime dir another user could control: the
// dir must be owned by the effective uid, and its parent must be owned by that
// uid or root and not writable by group/other unless sticky. Otherwise a
// foreign uid can pre-create or swap the dir and receive rendered plaintext.
func checkRuntimeCustody(runtimeDir string, dir, parent os.FileInfo) error {
	euid := effectiveUID()
	if uid, _ := ownerOf(dir); uid != euid {
		return fmt.Errorf("%w: runtime dir %s is owned by uid %d, not the effective uid %d", ErrRuntimeCustody, runtimeDir, uid, euid)
	}
	if uid, _ := ownerOf(parent); uid != euid && uid != 0 {
		return fmt.Errorf("%w: parent of runtime dir %s is owned by uid %d, not the effective uid %d or root", ErrRuntimeCustody, runtimeDir, uid, euid)
	}
	if m := parent.Mode(); m&0o022 != 0 && m&os.ModeSticky == 0 {
		return fmt.Errorf("%w: parent of runtime dir %s is writable by group or other without the sticky bit", ErrRuntimeCustody, runtimeDir)
	}
	return nil
}
