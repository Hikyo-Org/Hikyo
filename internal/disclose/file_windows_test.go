//go:build windows

package disclose

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsTestReader struct {
	*strings.Reader
	closeCount int
}

func (r *windowsTestReader) Close() error {
	r.closeCount++
	return nil
}

type windowsTestWriter struct {
	bytes.Buffer
	closeCount int
}

func (w *windowsTestWriter) Close() error {
	w.closeCount++
	return nil
}

func TestWindowsTerminalSessionOwnsSeparateConsoleHandles(t *testing.T) {
	in := &windowsTestReader{Reader: strings.NewReader("y\n")}
	out := &windowsTestWriter{}
	session, err := NewTerminalSession(newWindowsTerminal(in, out))
	if err != nil {
		t.Fatal(err)
	}
	ok, err := session.Confirm("continue?")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("confirmation was declined")
	}
	if err := session.WriteDisclosure("Token", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if in.closeCount != 1 || out.closeCount != 1 {
		t.Fatalf("console close counts = input %d, output %d; want 1 each", in.closeCount, out.closeCount)
	}
	if got := out.String(); !strings.Contains(got, "continue?") || !strings.Contains(got, "secret") {
		t.Fatalf("console output = %q, want prompt and disclosure", got)
	}
}

var _ io.ReadCloser = (*windowsTestReader)(nil)
var _ io.WriteCloser = (*windowsTestWriter)(nil)

func TestWindowsPreparedFileOwnsHandleUntilAbort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	sink, err := Prepare(Options{OutputFile: path}, nil)
	if err != nil {
		t.Fatal(err)
	}

	competing, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		_ = competing.Close()
		t.Fatal("a competing writer opened the prepared Windows destination")
	}
	if err := sink.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unused Windows reservation remains after Abort: %v", err)
	}
}

func TestWindowsPreparedFileWritesThroughReservedHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	sink, err := Prepare(Options{OutputFile: path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sink.WriteOnce("Token", "hik_1_bs_secret"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hik_1_bs_secret\n" {
		t.Fatalf("Windows disclosure file = %q", body)
	}
}

// A parent that grants Everyone full control to its children must not leak
// into the disclosure file: the DACL is set at creation and does not inherit.
func TestWindowsPreparedFileHasProtectedOwnerOnlyDACL(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	open, err := windows.SecurityDescriptorFromString("D:(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(t.TempDir(), "open-parent")
	name, err := windows.UTF16PtrFromString(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.CreateDirectory(name, &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: open}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "token")
	sink, err := Prepare(Options{OutputFile: path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sink.Abort() })
	handle, err := windows.CreateFile(mustUTF16(t, path), windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("disclosure file DACL is not protected (control %#x)", control)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		t.Fatalf("disclosure file lacks an explicit DACL: %v", err)
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
			t.Fatalf("ACE %d is inherited from the parent", i)
		}
		trustee := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !trustee.Equals(user.User.Sid) && !trustee.IsWellKnown(windows.WinLocalSystemSid) && !trustee.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			t.Fatalf("ACE %d grants access to %s", i, trustee)
		}
	}
}

func mustUTF16(t *testing.T, s string) *uint16 {
	t.Helper()
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
