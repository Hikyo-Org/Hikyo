//go:build windows

package securefile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsProtectedAtomicTempCustodyBeforeWrite(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	checked := false
	err = WriteAtomicPrepared(filepath.Join(t.TempDir(), "private.json"), []byte("private-token"), 0o600, func(file *os.File) error {
		sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			return err
		}
		owner, _, err := sd.Owner()
		if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
			return fmt.Errorf("temporary file is not current-user owned")
		}
		acl, _, err := sd.DACL()
		if err != nil || acl == nil {
			return fmt.Errorf("temporary file lacks private DACL")
		}
		for i := uint32(0); i < uint32(acl.AceCount); i++ {
			var ace *windows.ACCESS_ALLOWED_ACE
			if err := windows.GetAce(acl, i, &ace); err != nil {
				return err
			}
			if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
				return fmt.Errorf("unexpected temporary-file access rule")
			}
			trustee := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if !trustee.Equals(user.User.Sid) && !trustee.IsWellKnown(windows.WinLocalSystemSid) && !trustee.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
				return fmt.Errorf("temporary file grants outsider access")
			}
		}
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.Size() != 0 {
			return fmt.Errorf("custody check occurred after private bytes were written")
		}
		checked = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("private temporary file was not checked before write")
	}
}
