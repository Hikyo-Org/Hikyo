//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/desktop/internal/shell"
)

// The test writes to the real login keychain, which can show a prompt on a
// locked keychain, so it is opt-in: HIKYO_DESKTOP_KEYCHAIN_TEST=1.
func TestKeychainRoundTrip(t *testing.T) {
	if os.Getenv("HIKYO_DESKTOP_KEYCHAIN_TEST") != "1" {
		t.Skip("set HIKYO_DESKTOP_KEYCHAIN_TEST=1 to touch the login keychain")
	}
	var k keychain
	host := "c-test-" + t.Name()
	t.Cleanup(func() { _ = k.Delete(host) })
	if _, err := k.Get(host); !errors.Is(err, shell.ErrNoSecret) {
		t.Fatalf("missing item: %v", err)
	}
	first := shell.Secret{Token: "tok-1", SessionID: "ses-1", Origin: "https://a.example", TrustMode: "system-ca"}
	if err := k.Put(host, first); err != nil {
		t.Fatal(err)
	}
	second := shell.Secret{Token: "tok-2", SessionID: "ses-2", Origin: "https://a.example", TrustMode: "spki", Pin: "pin", PrincipalID: "usr_x"}
	if err := k.Put(host, second); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Get(host); err != nil || got != second {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := k.Delete(host); err != nil {
		t.Fatal(err)
	}
	if err := k.Delete(host); err != nil {
		t.Errorf("deleting a missing item: %v", err)
	}
}

// An item another process planted under our name, open to any application,
// must be replaced rather than updated: the token we store next is then
// readable only through our own access list, never the planted one.
func TestKeychainPutReplacesAPlantedItem(t *testing.T) {
	if os.Getenv("HIKYO_DESKTOP_KEYCHAIN_TEST") != "1" {
		t.Skip("set HIKYO_DESKTOP_KEYCHAIN_TEST=1 to touch the login keychain")
	}
	var k keychain
	host := "c-test-planted"
	t.Cleanup(func() { _ = k.Delete(host) })
	// -A: any application may read it, as a planting process would arrange.
	if out, err := exec.CommandContext(t.Context(), "security", "add-generic-password", "-A", "-s", "io.hikyo.desktop", "-a", host, "-w", "planted").CombinedOutput(); err != nil {
		t.Fatalf("planting: %v %s", err, out)
	}
	// A reader must refuse the planted item outright, never return its token.
	if _, err := k.Get(host); !errors.Is(err, shell.ErrItemUntrusted) {
		t.Fatalf("planted any-app item: %v", err)
	}
	want := shell.Secret{Token: "tok", SessionID: "ses", Origin: "https://a.example", TrustMode: "system-ca"}
	if err := k.Put(host, want); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Get(host); err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
	// Not asserted here: that another binary can no longer read the item. A
	// read from `security` raises an approval dialog (observed by hand), which
	// a test can't answer.
}

// The cases below run the keychain from copies of this test binary, because the
// item's access list is about which program asks. TestKeychainHelper is the
// copy's entry point: it does one operation on one fixed item and prints it.
const helperHost = "c-test-helper"

func TestKeychainHelper(t *testing.T) {
	op := os.Getenv("HIKYO_KEYCHAIN_HELPER")
	if op == "" {
		t.Skip("entry point of the copies run by the other keychain tests")
	}
	var k keychain
	var err error
	switch op {
	case "put":
		err = k.Put(helperHost, shell.Secret{Token: "tok", SessionID: "ses", Origin: "https://a.example", TrustMode: "system-ca"})
	case "get":
		_, err = k.Get(helperHost)
	case "delete":
		err = k.Delete(helperHost)
	}
	switch {
	case err == nil:
		fmt.Println("RESULT ok")
	case errors.Is(err, shell.ErrItemUntrusted):
		fmt.Println("RESULT untrusted")
	case errors.Is(err, shell.ErrNoSecret):
		fmt.Println("RESULT missing")
	default:
		fmt.Println("RESULT error", err)
	}
}

// program is a copy of this test binary, optionally re-signed.
type program string

// copyTest copies the running test binary to dir/name. sign is nil to keep the
// build's own ad-hoc signature, or the codesign arguments to re-sign with.
func copyTest(t *testing.T, name string, sign ...string) program {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, raw, 0o755); err != nil {
		t.Fatal(err)
	}
	if len(sign) > 0 {
		args := append([]string{"--force"}, sign...)
		if out, err := exec.CommandContext(t.Context(), "codesign", append(args, path)...).CombinedOutput(); err != nil {
			t.Fatalf("codesign %v: %v %s", sign, err, out)
		}
	}
	return program(path)
}

func (p program) run(t *testing.T, op string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), string(p), "-test.run=^TestKeychainHelper$")
	cmd.Env = append(os.Environ(), "HIKYO_KEYCHAIN_HELPER="+op)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v %s", p, op, err, out)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if res, ok := strings.CutPrefix(line, "RESULT "); ok {
			return res
		}
	}
	t.Fatalf("%s %s printed no result: %s", p, op, out)
	return ""
}

func (p program) expect(t *testing.T, op, want string) {
	t.Helper()
	if got := p.run(t, op); got != want {
		t.Errorf("%s %s = %q, want %q", filepath.Base(string(p)), op, got, want)
	}
}

func keychainCopiesTest(t *testing.T) {
	t.Helper()
	if os.Getenv("HIKYO_DESKTOP_KEYCHAIN_TEST") != "1" {
		t.Skip("set HIKYO_DESKTOP_KEYCHAIN_TEST=1 to touch the login keychain")
	}
	t.Cleanup(func() { _ = keychain{}.Delete(helperHost) })
	_ = keychain{}.Delete(helperHost)
}

// A moved or copied app (a launch from Downloads is translocated to a random
// path) is the same program, so its own item must still be accepted, also
// after the original is gone.
func TestKeychainItemSurvivesMovingTheApp(t *testing.T) {
	keychainCopiesTest(t)
	a, b := copyTest(t, "a.test"), copyTest(t, "b.test")
	a.expect(t, "put", "ok")
	a.expect(t, "get", "ok")
	b.expect(t, "get", "ok")
	if err := os.Remove(string(a)); err != nil {
		t.Fatal(err)
	}
	b.expect(t, "get", "ok")
}

// Another build is another program: its item is refused, never read, and
// signing in again from the new build replaces the item and removal deletes it.
func TestKeychainItemOfAnotherBuildIsRefused(t *testing.T) {
	keychainCopiesTest(t)
	a := copyTest(t, "a.test")
	other := copyTest(t, "other.test", "-s", "-", "-i", "io.example.other")
	a.expect(t, "put", "ok")
	other.expect(t, "get", "untrusted")
	other.expect(t, "put", "ok")
	other.expect(t, "get", "ok")
	a.expect(t, "get", "untrusted")
	a.expect(t, "delete", "ok")
	other.expect(t, "get", "missing")
}

// With a real signing identity (HIKYO_DESKTOP_SIGN_IDENTITY) the match is the
// designated requirement: a moved signed app is accepted, while a different
// identifier, an ad-hoc build and an any-app item are refused.
func TestKeychainSignedMatchesByRequirement(t *testing.T) {
	keychainCopiesTest(t)
	identity := os.Getenv("HIKYO_DESKTOP_SIGN_IDENTITY")
	if identity == "" {
		t.Skip("set HIKYO_DESKTOP_SIGN_IDENTITY to a code signing identity")
	}
	a := copyTest(t, "a.test", "-s", identity, "-i", "io.example.signed")
	moved := copyTest(t, "moved.test", "-s", identity, "-i", "io.example.signed")
	otherID := copyTest(t, "otherid.test", "-s", identity, "-i", "io.example.different")
	adhoc := copyTest(t, "adhoc.test", "-s", "-", "-i", "io.example.signed")
	a.expect(t, "put", "ok")
	moved.expect(t, "get", "ok")
	otherID.expect(t, "get", "untrusted")
	adhoc.expect(t, "get", "untrusted")
	// An item open to any application is refused even by the signed app.
	a.expect(t, "delete", "ok")
	if out, err := exec.CommandContext(t.Context(), "security", "add-generic-password", "-A", "-s", "io.hikyo.desktop", "-a", helperHost, "-w", "planted").CombinedOutput(); err != nil {
		t.Fatalf("planting: %v %s", err, out)
	}
	a.expect(t, "get", "untrusted")
	moved.expect(t, "get", "untrusted")
}
