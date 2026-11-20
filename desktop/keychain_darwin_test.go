//go:build darwin

package main

import (
	"errors"
	"os"
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
	first := shell.Secret{Token: "tok-1", SessionID: "ses-1"}
	if err := k.Put(host, first); err != nil {
		t.Fatal(err)
	}
	second := shell.Secret{Token: "tok-2", SessionID: "ses-2"}
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
