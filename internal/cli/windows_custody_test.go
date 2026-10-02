//go:build windows

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Hikyo-Org/hikyo/internal/securefile"
)

func privateWindowsDirectory(t *testing.T) string {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "private")
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	if err := windows.CreateDirectory(name, sa); err != nil {
		t.Fatal(err)
	}
	return dir
}

func broadenWindowsDACL(t *testing.T, path, permissions string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;" + permissions + ";;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsSessionCustodyAndUnsafeACLRefusal(t *testing.T) {
	// Exercise real private directory and lock creation, including a process
	// whose default file owner may otherwise be the Administrators group.
	dir := filepath.Join(t.TempDir(), "new-private-state")
	state := &State{dir: dir}
	if err := state.PutSession(SessionArtifact{Instance: "local", Token: "private-token"}); err != nil {
		t.Fatal(err)
	}
	if got, err := state.Sessions(); err != nil || got["local"].Token != "private-token" {
		t.Fatalf("private session: %#v, %v", got, err)
	}
	broadenWindowsDACL(t, state.sessionsPath(), "GR")
	if got, err := state.Sessions(); err == nil || got != nil {
		t.Fatal("world-readable bearer accepted")
	}
	if err := state.PutSession(SessionArtifact{Instance: "other", Token: "replacement"}); err == nil {
		t.Fatal("unsafe existing bearer ACL was laundered by directory hardening")
	}
}

func TestWindowsTrustCustodyRefusesInjectedACL(t *testing.T) {
	dir := privateWindowsDirectory(t)
	path := filepath.Join(dir, "trust.json")
	if err := securefile.WriteAtomic(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrustFile(dir); err != nil {
		t.Fatal(err)
	}
	broadenWindowsDACL(t, path, "GW")
	if _, err := readTrustFile(dir); err == nil {
		t.Fatal("externally writable trust entry accepted")
	}
}

func TestWindowsStateCustodyRefusesExternallyWritableDirectory(t *testing.T) {
	dir := privateWindowsDirectory(t)
	broadenWindowsDACL(t, dir, "GW")
	if err := (&State{dir: dir}).PutSession(SessionArtifact{Instance: "local", Token: "private-token"}); err == nil {
		t.Fatal("unsafe directory accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions.json")); !os.IsNotExist(err) {
		t.Fatalf("bearer written before directory custody: %v", err)
	}
}

func TestWindowsSessionCustodyRefusesReparsePoint(t *testing.T) {
	dir := privateWindowsDirectory(t)
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := securefile.WriteAtomic(outside, []byte(`{"local":{"token":"outside-token"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "sessions.json")); err != nil {
		if errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
			t.Skip("Windows runner lacks symlink privilege; other native custody tests remain required")
		}
		t.Fatal(err)
	}
	if _, err := (&State{dir: dir}).Sessions(); err == nil {
		t.Fatal("session reparse point accepted")
	}
}

func TestWindowsSessionCustodyReadDoesNotCreateState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent-state")
	sessions, err := (&State{dir: dir}).Sessions()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("absent private sessions: %v", err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read created state directory: %v", err)
	}
}
