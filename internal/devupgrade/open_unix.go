//go:build linux || darwin

package devupgrade

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var ErrDurabilityUnconfirmed = errors.New("development custody published but durability unconfirmed; preserve it and retry")

// Open creates or verifies one installation's development custody under an
// existing, canonical, euid-owned 0700 parent. It never creates or repairs the
// parent, adopts an existing empty child, or rotates malformed custody.
// All path components are opened without following symlinks. On publication
// sync failure it preserves the complete child and returns no usable material.
func Open(ctx context.Context, parentDirectory string) (Material, error) {
	return open(ctx, parentDirectory, func(f *os.File) error { return f.Sync() })
}

func open(ctx context.Context, parentDirectory string, syncFile func(*os.File) error) (Material, error) {
	if err := ctx.Err(); err != nil {
		return Material{}, err
	}
	ancestors, err := openParent(parentDirectory)
	if err != nil {
		return Material{}, err
	}
	defer func() {
		for _, f := range ancestors {
			f.Close()
		}
	}()
	parent := ancestors[len(ancestors)-1]
	// Exclusive creation separates the creator from reopeners. Concurrent
	// O_CREAT|O_NOFOLLOW on Darwin may return ENOENT during another create.
	fd, err := unix.Openat(int(parent.Fd()), ".development-upgrade.lock", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if errors.Is(err, os.ErrExist) {
		fd, err = unix.Openat(int(parent.Fd()), ".development-upgrade.lock", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	}
	if err != nil {
		return Material{}, fmt.Errorf("open development custody lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), ".development-upgrade.lock")
	defer lock.Close()
	if err := secure(lock, false); err != nil {
		return Material{}, err
	}
	if info, err := lock.Stat(); err != nil || info.Size() != 0 {
		return Material{}, errors.New("unknown nonempty development custody lock")
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return Material{}, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Material{}, ctx.Err()
		case <-timer.C:
		}
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	// A crash during private staging never becomes authority and must be
	// inspected explicitly. Do not silently create a new trust root beside it.
	entries, err := readDirectory(parent)
	if err != nil {
		return Material{}, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".development-upgrade-stage-") {
			return Material{}, errors.New("incomplete development custody staging requires inspection")
		}
	}
	child, err := openDir(parent, custodyName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Material{}, err
	}
	if errors.Is(err, os.ErrNotExist) {
		recovery, err := newPrivate()
		if err != nil {
			return Material{}, err
		}
		primary, err := newPrivate()
		if err != nil {
			return Material{}, err
		}
		files, err := documents(recovery, primary, true)
		if err != nil {
			return Material{}, err
		}
		if _, err := verify(files); err != nil {
			return Material{}, err
		}
		stage := ".development-upgrade-stage-" + rand.Text()
		if err := unix.Mkdirat(int(parent.Fd()), stage, 0700); err != nil {
			return Material{}, err
		}
		staging, err := openDir(parent, stage)
		if err != nil {
			_ = unix.Unlinkat(int(parent.Fd()), stage, unix.AT_REMOVEDIR)
			return Material{}, err
		}
		published := false
		defer func() {
			if !published {
				removeStage(parent, stage, staging, files)
			}
			staging.Close()
		}()
		if err := writeTree(staging, files, syncFile); err != nil {
			return Material{}, err
		}
		if err := ctx.Err(); err != nil {
			return Material{}, err
		}
		if err := publish(parent, stage, custodyName); err != nil {
			return Material{}, err
		}
		published = true
		child = staging
		// Keep one descriptor owner in the caller; the deferred staging close is
		// harmless after our duplicated child is closed below.
		dup, err := unix.Dup(int(staging.Fd()))
		if err != nil {
			return Material{}, err
		}
		child = os.NewFile(uintptr(dup), custodyName)
	}
	defer child.Close()
	files, dirs, err := readTree(child)
	if err != nil {
		return Material{}, err
	}
	pinned, err := verify(files)
	if err != nil {
		return Material{}, err
	}
	if !slices.Equal(dirs, directories(files)) {
		return Material{}, errors.New("unknown development custody directory")
	}
	// Repeat full directory sync on every successful open. Existence does not
	// prove a previous attempt durably published all ancestors.
	for i := len(dirs) - 1; i >= 0; i-- {
		dir, err := openDir(child, dirs[i])
		if err != nil {
			return Material{}, err
		}
		err = syncFile(dir)
		dir.Close()
		if err != nil {
			return Material{}, fmt.Errorf("%w: %v", ErrDurabilityUnconfirmed, err)
		}
	}
	if err := syncFile(child); err != nil {
		return Material{}, fmt.Errorf("%w: %v", ErrDurabilityUnconfirmed, err)
	}
	for i := len(ancestors) - 1; i >= 0; i-- {
		if err := syncFile(ancestors[i]); err != nil {
			return Material{}, fmt.Errorf("%w: %v", ErrDurabilityUnconfirmed, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return Material{}, err
	}
	return Material{Directory: filepath.Join(parentDirectory, custodyName, "bundle"), Pinned: pinned}, nil
}

func secure(file *os.File, directory bool) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return err
	}
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	wantMode := mode
	if directory {
		wantMode |= os.ModeDir
	}
	if st.Uid != uint32(os.Geteuid()) || info.Mode() != wantMode || (!directory && st.Nlink != 1) {
		return errors.New("development custody must be owned 0700 directories and single-link 0600 regular files")
	}
	return nil
}

func openParent(name string) ([]*os.File, error) {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return nil, errors.New("development custody parent must be an absolute canonical path")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	files := []*os.File{os.NewFile(uintptr(fd), "/")}
	for _, part := range strings.Split(strings.TrimPrefix(name, "/"), "/") {
		if part == "" {
			continue
		}
		next, err := unix.Openat(int(files[len(files)-1].Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			for _, f := range files {
				f.Close()
			}
			return nil, errors.New("development custody parent path must contain only real directories")
		}
		files = append(files, os.NewFile(uintptr(next), part))
	}
	if err := secure(files[len(files)-1], true); err != nil {
		for _, f := range files {
			f.Close()
		}
		return nil, err
	}
	return files, nil
}

func openDir(parent *os.File, name string) (*os.File, error) {
	if name == "." {
		fd, err := unix.Dup(int(parent.Fd()))
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(fd), name), nil
	}
	var opened []*os.File
	defer func() {
		for _, f := range opened {
			f.Close()
		}
	}()
	current := parent
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return nil, errors.New("invalid development directory component")
		}
		fd, err := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		current = os.NewFile(uintptr(fd), part)
		if err := secure(current, true); err != nil {
			current.Close()
			return nil, err
		}
		opened = append(opened, current)
	}
	fd, err := unix.Dup(int(current.Fd()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func readDirectory(dir *os.File) ([]os.DirEntry, error) {
	// A separately opened descriptor has an independent directory offset.
	fd, err := unix.Openat(int(dir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), ".")
	defer f.Close()
	entries, err := f.ReadDir(1025)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > 1024 {
		return nil, errors.New("development custody directory exceeds bound")
	}
	return entries, nil
}

func directories(files map[string][]byte) []string {
	seen := map[string]bool{}
	for name := range files {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			seen[dir] = true
		}
	}
	dirs := make([]string, 0, len(seen))
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	return dirs
}

func writeTree(root *os.File, files map[string][]byte, syncFile func(*os.File) error) error {
	dirs := directories(files)
	for _, name := range dirs {
		parent, err := openDir(root, path.Dir(name))
		if err != nil {
			return err
		}
		err = unix.Mkdirat(int(parent.Fd()), path.Base(name), 0700)
		parent.Close()
		if err != nil {
			return err
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		parent, err := openDir(root, path.Dir(name))
		if err != nil {
			return err
		}
		fd, err := unix.Openat(int(parent.Fd()), path.Base(name), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
		parent.Close()
		if err != nil {
			return err
		}
		f := os.NewFile(uintptr(fd), name)
		_, err = f.Write(files[name])
		if err == nil {
			err = syncFile(f)
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		dir, err := openDir(root, dirs[i])
		if err != nil {
			return err
		}
		err = syncFile(dir)
		dir.Close()
		if err != nil {
			return err
		}
	}
	return syncFile(root)
}

func readTree(root *os.File) (map[string][]byte, []string, error) {
	files := map[string][]byte{}
	dirs := []string{}
	total := 0
	var visit func(*os.File, string) error
	visit = func(dir *os.File, prefix string) error {
		entries, err := readDirectory(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := path.Join(prefix, entry.Name())
			if entry.IsDir() {
				if len(dirs) >= 8 || strings.Count(name, "/") > 4 {
					return errors.New("development directory inventory exceeds bound")
				}
				next, err := openDir(dir, entry.Name())
				if err != nil {
					return err
				}
				dirs = append(dirs, name)
				err = visit(next, name)
				next.Close()
				if err != nil {
					return err
				}
				continue
			}
			if len(files) >= 32 || !entry.Type().IsRegular() {
				return errors.New("invalid development file inventory")
			}
			fd, err := unix.Openat(int(dir.Fd()), entry.Name(), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if err != nil {
				return err
			}
			f := os.NewFile(uintptr(fd), name)
			err = secure(f, false)
			if err != nil {
				f.Close()
				return err
			}
			raw, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
			f.Close()
			total += len(raw)
			if err != nil {
				return err
			}
			if len(raw) == 0 || len(raw) > 4<<20 || total > 16<<20 {
				return errors.New("development document bytes exceed bound")
			}
			files[name] = raw
		}
		return nil
	}
	err := visit(root, "")
	slices.Sort(dirs)
	return files, dirs, err
}

// Cleanup addresses only names created by this attempt. It never removes the
// published custody or unrelated parent contents, including crash leftovers.
func removeStage(parent *os.File, stage string, root *os.File, files map[string][]byte) {
	// root is the descriptor created by this attempt, never reopened through
	// the mutable stage name. A replacement is not ours to inspect or remove.
	var original unix.Stat_t
	if unix.Fstat(int(root.Fd()), &original) != nil {
		return
	}
	for name := range files {
		dir, err := openDir(root, path.Dir(name))
		if err == nil {
			_ = unix.Unlinkat(int(dir.Fd()), path.Base(name), 0)
			dir.Close()
		}
	}
	dirs := directories(files)
	for i := len(dirs) - 1; i >= 0; i-- {
		dir, err := openDir(root, path.Dir(dirs[i]))
		if err == nil {
			_ = unix.Unlinkat(int(dir.Fd()), path.Base(dirs[i]), unix.AT_REMOVEDIR)
			dir.Close()
		}
	}
	var current unix.Stat_t
	if unix.Fstatat(int(parent.Fd()), stage, &current, unix.AT_SYMLINK_NOFOLLOW) == nil && original.Dev == current.Dev && original.Ino == current.Ino {
		_ = unix.Unlinkat(int(parent.Fd()), stage, unix.AT_REMOVEDIR)
	}
}
