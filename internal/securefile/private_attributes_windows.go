//go:build windows

package securefile

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// PrivateSecurityAttributes returns creation-time security attributes with a
// protected, owner-only DACL: the current user, SYSTEM and Administrators get
// full control and nothing is inherited from the parent. inherit also lets
// children of a directory carry the same rules.
func PrivateSecurityAttributes(inherit bool) (*windows.SecurityAttributes, error) {
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
