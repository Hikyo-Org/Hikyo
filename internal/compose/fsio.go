package compose

import (
	"os"

	"github.com/Hikyo-Org/hikyo/internal/securefile"
)

// Filesystem durability helpers. The compose-integration ADR is explicit about
// fsync ordering: a value must be on disk before anything references it, and a
// directory entry is only durable once the directory itself is fsynced. The
// single atomic-rename stamp file is the commit point; generation directories
// are made durable file-by-file then marked complete.

// writeFileFsync creates path (truncating), writes data, chmods to perm
// explicitly (umask-independent), and fsyncs the file. It does NOT fsync the
// parent — the caller fsyncs the directory once after writing all its files.
func writeFileFsync(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// atomicWriteEnv writes a project `.env`-style file, PRESERVING an existing
// file's mode and (best-effort) ownership and NEVER widening them: an existing
// project `.env` routinely holds plaintext secrets (compose-integration ADR
// § Adoption), so replacing a 0600 file with a fresh 0644 one would leak it. A
// newly created file is 0600. Ownership is preserved where the platform can
// (unix); it is best-effort because a non-owning writer cannot chown and that
// is not a fatal condition for the render. The publication mechanics are
// securefile's.
func atomicWriteEnv(path string, data []byte) error {
	perm := os.FileMode(0o600)
	uid, gid := -1, -1
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
		uid, gid = ownerOf(fi)
	}
	return securefile.WriteAtomicPrepared(path, data, perm, func(tmp *os.File) error {
		if uid >= 0 {
			// Best-effort: a non-owning writer cannot chown, which is not fatal.
			_ = tmp.Chown(uid, gid)
		}
		return nil
	})
}

// atomicWrite is securefile.WriteAtomic: temp file in the same directory,
// fsynced, renamed over path (the atomic commit), directory fsynced so the
// rename is durable. perm is applied explicitly.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	return securefile.WriteAtomic(path, data, perm)
}
