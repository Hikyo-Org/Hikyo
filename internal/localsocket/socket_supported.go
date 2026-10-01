//go:build linux || darwin

// Package localsocket provides same-user Unix-domain HTTP transport.
package localsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ValidatePath requires an absolute canonical socket path inside an existing,
// euid-owned 0700 directory. The private parent prevents another local user
// from replacing the pathname between validation and bind or dial.
func ValidatePath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("local CLI socket path must be absolute and canonical")
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("local CLI socket parent: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("local CLI socket parent must be a real directory")
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("local CLI socket parent %s has mode %04o, want 0700", parent, info.Mode().Perm())
	}
	if err := requireOwner(parent, info); err != nil {
		return err
	}
	return nil
}

// Listen creates a 0600 Unix socket and rejects clients whose kernel-reported
// effective UID differs from the server's. Only a verified, refused stale socket
// is removed; a live socket or any other existing filesystem object is refused.
func Listen(path string) (net.Listener, error) {
	if err := ValidatePath(path); err != nil {
		return nil, err
	}
	unlock, err := lockSocketStartup(path)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := clearStaleSocket(path); err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	listener.SetUnlinkOnClose(true)
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("secure local CLI socket: %w", err)
	}
	if err := validateSocket(path); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return &sameUserListener{Listener: listener, uid: uint32(os.Geteuid())}, nil
}

// Keep the lock inode after release: unlinking it would let a concurrent
// starter lock a different inode and remove the newly bound socket.
func lockSocketStartup(path string) (func(), error) {
	fd, err := unix.Open(path+".lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open local CLI socket startup lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), path+".lock")
	info, err := lock.Stat()
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		_ = lock.Close()
		return nil, errors.New("local CLI socket startup lock must be a regular 0600 file")
	}
	if err := requireOwner(lock.Name(), info); err != nil {
		_ = lock.Close()
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock local CLI socket startup: %w", err)
	}
	return func() { _ = lock.Close() }, nil
}

func clearStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect local CLI socket: %w", err)
	}
	if err := validateSocket(path); err != nil {
		return fmt.Errorf("local CLI socket path already exists: %w", err)
	}
	conn, err := net.DialTimeout("unix", path, 250*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return errors.New("local CLI socket is in use by another server")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("probe existing local CLI socket: %w", err)
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) {
		return errors.New("local CLI socket changed during stale-socket recovery")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale local CLI socket: %w", err)
	}
	return nil
}

// DialContext connects only after both pathname custody and peer UID are
// verified. Peer credentials come from the kernel, not application bytes.
func DialContext(ctx context.Context, path string) (net.Conn, error) {
	if err := ValidatePath(path); err != nil {
		return nil, err
	}
	if err := validateSocket(path); err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	if err := requirePeerUID(conn, uint32(os.Geteuid())); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

type sameUserListener struct {
	net.Listener
	uid uint32
}

func (l *sameUserListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if err := requirePeerUID(conn, l.uid); err != nil {
			_ = conn.Close()
			continue
		}
		return conn, nil
	}
}

func validateSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect local CLI socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("local CLI socket path is not a Unix socket")
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("local CLI socket %s has mode %04o, want 0600", path, info.Mode().Perm())
	}
	return requireOwner(path, info)
}

func requireOwner(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine owner of local CLI socket path %s", path)
	}
	if uint32(stat.Uid) != uint32(os.Geteuid()) {
		return fmt.Errorf("local CLI socket path %s is owned by uid %d, want %d", path, stat.Uid, os.Geteuid())
	}
	return nil
}

func requirePeerUID(conn net.Conn, expected uint32) error {
	syscallConn, ok := conn.(syscall.Conn)
	if !ok {
		return errors.New("local CLI socket does not expose kernel peer credentials")
	}
	raw, err := syscallConn.SyscallConn()
	if err != nil {
		return fmt.Errorf("read local CLI peer credentials: %w", err)
	}
	var uid uint32
	var peerErr error
	if err := raw.Control(func(fd uintptr) {
		uid, peerErr = socketPeerUID(int(fd))
	}); err != nil {
		return fmt.Errorf("read local CLI peer credentials: %w", err)
	}
	if peerErr != nil {
		return fmt.Errorf("read local CLI peer credentials: %w", peerErr)
	}
	if uid != expected {
		return fmt.Errorf("local CLI peer uid %d does not match server uid %d", uid, expected)
	}
	return nil
}
