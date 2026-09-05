//go:build linux || darwin

package upgradegate

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"golang.org/x/sys/unix"
)

type operatorFile struct {
	directory, lock *os.File
	value           operatorCustody
}

func openOperatorFile(ctx context.Context, directory string, initial []byte, allowCreate bool) (*operatorFile, error) {
	if directory == "" {
		return nil, errors.New("HIKYO_UPGRADE_STATE_DIR must name persistent installation custody")
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(canonical, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := &operatorFile{directory: os.NewFile(uintptr(fd), canonical)}
	fail := func(err error) (*operatorFile, error) { f.close(); return nil, err }
	if err := operatorSecure(f.directory, true); err != nil {
		return fail(err)
	}
	lockfd, err := unix.Openat(fd, ".operator-custody.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return fail(err)
	}
	f.lock = os.NewFile(uintptr(lockfd), ".operator-custody.lock")
	if err := operatorSecure(f.lock, false); err != nil {
		return fail(err)
	}
	for {
		err = unix.Flock(lockfd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return fail(err)
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
	rawfd, err := unix.Openat(fd, operatorCustodyName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) && allowCreate {
		if _, err := backupreceipt.PinOperator("ins_00000000000000000000000000000000", initial); err != nil {
			return fail(err)
		}
		f.value = operatorCustody{Format: "hikyo-operator-custody/v1", PublicKey: initial}
		if err := f.save(); err != nil {
			return fail(err)
		}
		return f, nil
	}
	if err != nil {
		return fail(err)
	}
	rawfile := os.NewFile(uintptr(rawfd), operatorCustodyName)
	defer rawfile.Close()
	if err := operatorSecure(rawfile, false); err != nil {
		return fail(err)
	}
	raw, err := io.ReadAll(io.LimitReader(rawfile, 4<<20))
	if err != nil {
		return fail(err)
	}
	f.value, err = decodeOperatorCustody(raw)
	if err != nil {
		return fail(err)
	}
	if len(initial) > 0 {
		configured, err := backupreceipt.PinOperator("ins_00000000000000000000000000000000", initial)
		if err != nil {
			return fail(err)
		}
		installed, _ := backupreceipt.PinOperator("ins_00000000000000000000000000000000", f.value.PublicKey)
		if configured.KeyID() != installed.KeyID() {
			return fail(errors.New("configured operator public key differs from durable installation pin; use explicit rotation"))
		}
	}
	return f, nil
}
func operatorSecure(file *os.File, directory bool) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return err
	}
	kind := uint32(unix.S_IFREG)
	mode := uint32(0600)
	if directory {
		kind = unix.S_IFDIR
		mode = 0700
	}
	if uint32(stat.Mode)&unix.S_IFMT != kind || uint32(stat.Mode)&0777 != mode || int(stat.Uid) != os.Geteuid() || (!directory && stat.Nlink != 1) {
		return errors.New("operator custody requires owned 0700 directory and single-link 0600 files")
	}
	return nil
}
func (f *operatorFile) save() error {
	raw, err := json.Marshal(f.value)
	if err != nil {
		return err
	}
	if _, err := decodeOperatorCustody(raw); err != nil {
		return err
	}
	name := ".operator-custody-" + rand.Text() + ".partial"
	fd, err := unix.Openat(int(f.directory.Fd()), name, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	defer unix.Unlinkat(int(f.directory.Fd()), name, 0)
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := unix.Renameat(int(f.directory.Fd()), name, int(f.directory.Fd()), operatorCustodyName); err != nil {
		return err
	}
	return f.directory.Sync()
}
func (f *operatorFile) close() {
	if f == nil {
		return
	}
	if f.lock != nil {
		_ = unix.Flock(int(f.lock.Fd()), unix.LOCK_UN)
		_ = f.lock.Close()
	}
	if f.directory != nil {
		_ = f.directory.Close()
	}
}
