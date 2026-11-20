//go:build darwin

package main

import (
	"errors"
	"os"
	"os/exec"
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
