package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
)

func TestUnattendedRootCustodyFollowsEnrollment(t *testing.T) {
	if unattendedRootCustody(&config.Config{}) != nil {
		t.Fatal("a host or manual installation must not claim runtime-owned custody")
	}
	state, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Upgrade: config.UpgradeConfiguration{Unattended: true, StateDirectory: state}}
	custody := unattendedRootCustody(cfg)
	oldRoot, newRoot := bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x32}, 32)
	ctx := t.Context()
	// No vault yet: rotation proceeds without custody work.
	if err := custody.Stage(ctx, oldRoot, newRoot); err != nil {
		t.Fatalf("stage without vault: %v", err)
	}
	if err := custody.Check(ctx, newRoot); err != nil {
		t.Fatalf("check without vault: %v", err)
	}
	if err := custody.Commit(ctx, newRoot); err != nil {
		t.Fatalf("commit without vault: %v", err)
	}
	directory := filepath.Join(state, "unattended")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	const instance = "ins_44444444444444444444444444444444"
	vault, err := upgradecustody.CreateLocal(filepath.Join(directory, "custody"), oldRoot, instance)
	if err != nil {
		t.Fatal(err)
	}
	vault.Close()
	// A vault without a bound enrollment is refused, never guessed at.
	if err := custody.Stage(ctx, oldRoot, newRoot); err == nil {
		t.Fatal("stage accepted a vault without a bound enrollment")
	}
	if err := writeUnattendedEnrollment(filepath.Join(directory, "enrollment.json"), unattendedEnrollment{Format: "hikyo.unattended-enrollment/v1", Instance: instance}); err != nil {
		t.Fatal(err)
	}
	if err := custody.Stage(ctx, oldRoot, newRoot); err != nil {
		t.Fatal(err)
	}
	if err := custody.Check(ctx, newRoot); err != nil {
		t.Fatal(err)
	}
	if err := custody.Commit(ctx, newRoot); err != nil {
		t.Fatal(err)
	}
	v, err := upgradecustody.OpenLocal(filepath.Join(directory, "custody"), newRoot, instance)
	if err != nil {
		t.Fatal(err)
	}
	v.Close()
}

// The host unlock failure names both possible causes and keeps ErrUnlock
// matchable, so a rotated root is no longer reported as only a passphrase.
func TestAutomaticCustodyUnlockErrorNamesBothCauses(t *testing.T) {
	if !errors.Is(errUnlockedByNeitherSecret, upgradecustody.ErrUnlock) {
		t.Fatal("unlock failure no longer matches upgradecustody.ErrUnlock")
	}
	for _, cause := range []string{"passphrase", "root key was rotated"} {
		if !strings.Contains(errUnlockedByNeitherSecret.Error(), cause) {
			t.Fatalf("unlock failure does not name %q: %v", cause, errUnlockedByNeitherSecret)
		}
	}
}
