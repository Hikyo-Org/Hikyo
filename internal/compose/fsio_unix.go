//go:build unix

package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	abs, err := filepath.Abs(runtimeDir)
	if err != nil {
		return fmt.Errorf("compose: resolve runtime dir %s: %w", runtimeDir, err)
	}
	return checkRuntimeAncestors(runtimeDir, filepath.Dir(abs), map[string]bool{}, 0)
}

// checkRuntimeAncestors applies the parent rule to every ancestor of dir: an
// owner of any of them could rename the runtime dir's parent away and put its
// own in place. A symlinked ancestor is owned by the effective uid or root and
// its target hierarchy is checked the same way (as localsocket does), since a
// trusted alias can reach a safe directory through a replaceable middle hop.
func checkRuntimeAncestors(runtimeDir, dir string, checked map[string]bool, depth int) error {
	if depth > 40 {
		return fmt.Errorf("%w: runtime dir %s has an ancestor symlink chain that is too deep", ErrRuntimeCustody, runtimeDir)
	}
	if checked[dir] {
		return nil
	}
	checked[dir] = true
	euid := effectiveUID()
	sep := string(filepath.Separator)
	current := sep
	for _, component := range append([]string{""}, strings.Split(strings.TrimPrefix(dir, sep), sep)...) {
		// Dot segments stay for the kernel to resolve; cleaning a relative link
		// containing ".." could hide the hierarchy it traverses.
		if component != "" {
			current = strings.TrimSuffix(current, sep) + sep + component
		}
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("compose: inspect runtime dir ancestor %s: %w", current, err)
		}
		if uid, _ := ownerOf(info); uid != euid && uid != 0 {
			return fmt.Errorf("%w: ancestor %s of runtime dir %s is owned by uid %d, not the effective uid %d or root", ErrRuntimeCustody, current, runtimeDir, uid, euid)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(current)
			if err != nil {
				return fmt.Errorf("compose: read runtime dir ancestor link %s: %w", current, err)
			}
			if !filepath.IsAbs(target) {
				target = current[:strings.LastIndex(current, sep)+1] + target
			}
			if err := checkRuntimeAncestors(runtimeDir, target, checked, depth+1); err != nil {
				return err
			}
			continue
		}
		if !info.IsDir() || info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("%w: ancestor %s of runtime dir %s lets another user replace its entries", ErrRuntimeCustody, current, runtimeDir)
		}
	}
	return nil
}
