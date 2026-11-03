//go:build unix

package app

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
)

func hybridRecipients(t *testing.T, n int) []string {
	t.Helper()
	out := make([]string, n)
	for i := range out {
		var err error
		if _, out[i], err = backup.GenerateHybridIdentity(); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestValidateBackupPolicyRefusesUnsatisfiableHybridSets(t *testing.T) {
	hybrid := hybridRecipients(t, backup.MaxHybridRecipients+1)
	_, classical, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		recipients []string
		want       error
		generic    bool
	}{
		"none":              {},
		"hybrid only":       {recipients: hybrid[:2]},
		"at the bound":      {recipients: hybrid[:backup.MaxHybridRecipients]},
		"classical only":    {recipients: []string{classical}},
		"classical garbage": {recipients: []string{"not-a-recipient"}},
		"over the bound":    {recipients: hybrid, want: backup.ErrTooManyHybridRecipients},
		"mixed":             {recipients: []string{classical, hybrid[0]}, want: backup.ErrMixedRecipients},
		"duplicate":         {recipients: []string{hybrid[0], hybrid[0]}, want: backup.ErrDuplicateRecipient},
		"malformed hybrid":  {recipients: []string{hybrid[0][:100]}, generic: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateBackupPolicy(&config.Config{BackupRecipients: tc.recipients})
			switch {
			case tc.want != nil:
				if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), "HIKYO_BACKUP_RECIPIENTS") {
					t.Fatalf("error = %v, want %v named by variable", err, tc.want)
				}
			case tc.generic:
				if err == nil || strings.Contains(err.Error(), hybrid[0][:40]) {
					t.Fatalf("malformed hybrid recipient: %v", err)
				}
			case err != nil:
				t.Fatalf("policy refused: %v", err)
			}
		})
	}
	if err := ValidateBackupPolicy(nil); err != nil {
		t.Fatal(err)
	}
}

// The custody check runs against a real vault, which enrollment always mints as
// X25519: a hybrid policy is refused, every other policy is untouched.
func TestCustodyBackupPolicyAgainstRealVault(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(base, "custody")
	vault, err := upgradecustody.CreateLocal(directory, bytes.Repeat([]byte{0x42}, 32), "ins_22222222222222222222222222222222")
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	if strings.HasPrefix(vault.Recipient(), "age1pq1") {
		t.Fatal("enrollment minted a hybrid identity: this check must be revisited")
	}
	_, classical, _ := backup.GenerateIdentity()
	hybrid := hybridRecipients(t, 1)
	for name, tc := range map[string]struct {
		configured []string
		refuse     bool
	}{
		"no policy":            {},
		"x25519 policy":        {configured: []string{classical}},
		"x25519 same as vault": {configured: []string{vault.Recipient()}},
		"hybrid policy":        {configured: hybrid, refuse: true},
		"mixed policy":         {configured: []string{classical, hybrid[0]}, refuse: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := requireCustodyMatchesBackupPolicy(tc.configured, vault.Recipient())
			if tc.refuse != errors.Is(err, ErrHybridPolicyClassicalCustody) || (!tc.refuse && err != nil) {
				t.Fatalf("refusal = %v, want refuse=%t", err, tc.refuse)
			}
		})
	}
	// A hybrid custody recipient satisfies a hybrid policy.
	if err := requireCustodyMatchesBackupPolicy(hybrid, hybrid[0]); err != nil {
		t.Fatalf("hybrid custody refused: %v", err)
	}
	if !strings.Contains(ErrHybridPolicyClassicalCustody.Error(), "upgrade-export") || !strings.Contains(ErrHybridPolicyClassicalCustody.Error(), "upgrade-drill") {
		t.Fatal("the refusal does not point at the manual upgrade route")
	}
}
