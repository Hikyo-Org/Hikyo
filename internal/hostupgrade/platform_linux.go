//go:build linux

package hostupgrade

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func fileOwner(info os.FileInfo) int {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return -1
	}
	return int(stat.Uid)
}

func runCommand(ctx context.Context, request command) ([]byte, error) {
	cmd := exec.CommandContext(ctx, request.path, request.args...)
	cmd.Env = request.env
	cmd.Dir = request.directory
	if request.runtime {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: request.uid, Gid: request.gid, Groups: []uint32{request.gid}}}
		key, err := runtimeCredential(request.rootKey, request.uid, request.gid)
		if err != nil {
			return nil, err
		}
		defer key.Close()
		cmd.ExtraFiles = []*os.File{key}
	}
	// Error output can contain operational configuration. The adapter returns
	// stdout only; callers get the bounded operation and exit status on failure.
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return output.bytes, nil
}

func runtimeCredential(path string, uid, gid uint32) (*os.File, error) {
	if err := trustedFile(path); err != nil {
		return nil, err
	}
	source, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("runtime root key must be mode 0600 or stricter")
	}
	fd, err := unix.MemfdCreate("hikyo-runtime-root-key", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "hikyo-runtime-root-key")
	if err = f.Chmod(0600); err == nil {
		err = f.Chown(int(uid), int(gid))
	}
	if err == nil {
		var n int64
		n, err = io.Copy(f, io.LimitReader(source, 4097))
		if n > 4096 {
			err = errors.New("runtime root key exceeds 4096 bytes")
		}
	}
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
