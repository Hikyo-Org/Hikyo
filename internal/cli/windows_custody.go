//go:build windows

package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func privateWindowsAttributes(inherit bool) (*windows.SecurityAttributes, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	flags := ""
	if inherit {
		flags = "OICI"
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;" + flags + ";FA;;;" + sid + ")(A;" + flags + ";FA;;;SY)(A;" + flags + ";FA;;;BA)")
	if err != nil {
		return nil, err
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}

func readPrivateStateFile(dir, name string) ([]byte, error) {
	if name != "trust.json" && name != "sessions.json" {
		return nil, errors.New("unsupported private state file")
	}
	// Reads (including doctor) never create state or repair custody. Only an
	// authorized private-state write may prepare a new safe directory.
	directory, err := openPrivateWindowsFile(dir, true, false)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	file, err := openPrivateWindowsFile(filepath.Join(dir, name), false, false)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, trustFileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > trustFileLimit {
		return nil, errors.New("private state file exceeds 1 MiB")
	}
	return raw, nil
}

// Inspect the final object and its security on ONE handle. Reparse points
// are never followed. Directory deletion/rename sharing is refused in custody.
func openPrivateWindowsFile(path string, directory, tighten bool) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("private state path must be absolute")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access := uint32(windows.GENERIC_READ | windows.READ_CONTROL)
	if tighten {
		access |= windows.WRITE_DAC
	}
	sharing := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE)
	if !directory {
		// Atomic credential replacement is allowed while this verified old
		// inode is read. The directory itself remains pinned against rename.
		sharing |= windows.FILE_SHARE_DELETE
	}
	handle, err := windows.CreateFile(name, access, sharing, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	fail := func(err error) (*os.File, error) { _ = file.Close(); return nil, err }
	var attributes windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &attributes); err != nil {
		return fail(err)
	}
	if attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fail(errors.New("private state cannot be a Windows reparse point"))
	}
	after, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	if !os.SameFile(before, after) || after.IsDir() != directory || (!directory && !after.Mode().IsRegular()) {
		return fail(errors.New("private state object changed or has the wrong type"))
	}
	if err := verifyWindowsCustody(handle, tighten); err != nil {
		return fail(err)
	}
	return file, nil
}

func verifyWindowsCustody(handle windows.Handle, tighten bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return errors.New("private state must be owned by the current Windows user")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return errors.New("private state requires an explicit Windows DACL")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue // This rule does not grant access to the opened object.
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("private state has an unsupported Windows access rule")
		}
		trustee := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !trustee.IsValid() {
			return errors.New("private state has an invalid Windows trustee")
		}
		if trustee.Equals(user.User.Sid) || trustee.IsWellKnown(windows.WinLocalSystemSid) || trustee.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			continue
		}
		// A safely owner-controlled directory can lose inherited outsider READ
		// access before writes. Existing outsider WRITE access is never repaired:
		// it could already have replaced the trust or credential objects.
		const fileDeleteChild = 0x40
		const writes = windows.GENERIC_ALL | windows.GENERIC_WRITE | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES | fileDeleteChild
		if !tighten || uint32(ace.Mask)&uint32(writes) != 0 {
			return errors.New("private state grants access to another Windows user")
		}
	}
	if tighten {
		private, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
		if err != nil {
			return err
		}
		dacl, _, err := private.DACL()
		if err != nil {
			return err
		}
		if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
			return err
		}
		return verifyWindowsCustody(handle, false)
	}
	return nil
}
