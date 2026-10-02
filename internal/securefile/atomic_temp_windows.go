//go:build windows

package securefile

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func createAtomicTemp(dir string, mode os.FileMode) (*os.File, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	var attributes *windows.SecurityAttributes
	if mode.Perm()&0o077 == 0 {
		// Set the DACL at creation, before another user can retain a read
		// handle. Tightening an already-created temporary file is too late.
		sid := user.User.Sid.String()
		sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;;FA;;;" + sid + ")(A;;FA;;;SY)(A;;FA;;;BA)")
		if err != nil {
			return nil, err
		}
		attributes = &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	}
	for range 32 {
		// Names require uniqueness, not secrecy. CREATE_NEW never overwrites.
		name := filepath.Join(dir, fmt.Sprintf(".secure-write-%016x", rand.Uint64()))
		path, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return nil, err
		}
		handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, attributes, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(handle), name), nil
	}
	return nil, errors.New("cannot create unique private atomic temporary file")
}
