package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
)

func keygen(t *testing.T, args ...string) (identityFile, stderr string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "identity")
	var out bytes.Buffer
	if err := runBackupKeygen(append([]string{"keygen", "--output-file", path}, args...), &out, nil, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("identity file mode = %v, %v; want 0600", info.Mode(), err)
	}
	return strings.TrimSpace(string(raw)), out.String()
}

// keygen without a flag keeps minting X25519; --hybrid is the only opt-in.
func TestBackupKeygenSelectsAlgorithmOnlyByFlag(t *testing.T) {
	identity, stderr := keygen(t)
	if !strings.HasPrefix(identity, "AGE-SECRET-KEY-1") || strings.Contains(stderr, "age1pq1") {
		t.Fatalf("default keygen did not mint an X25519 identity (stderr %q)", stderr)
	}

	identity, stderr = keygen(t, "--hybrid")
	recipient, err := backup.RecipientOf(identity)
	if err != nil || !strings.HasPrefix(identity, "AGE-SECRET-KEY-PQ-1") || !strings.HasPrefix(recipient, "age1pq1") {
		t.Fatalf("--hybrid did not mint a hybrid identity: %v", err)
	}
	if !strings.Contains(stderr, recipient) {
		t.Fatal("keygen did not print the public recipient")
	}
	if strings.Contains(stderr, identity) || strings.Contains(stderr, "AGE-SECRET-KEY") {
		t.Fatal("keygen leaked the private identity to its diagnostic stream")
	}
}

// The destination is reserved before the key exists, for --hybrid too.
func TestBackupKeygenHybridReservesDestinationFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(path, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runBackupKeygen([]string{"keygen", "--hybrid", "--output-file", path}, &bytes.Buffer{}, nil, nil)
	if !errors.Is(err, disclose.ErrFileExists) {
		t.Fatalf("error = %v, want ErrFileExists", err)
	}
	if body, _ := os.ReadFile(path); string(body) != "owned" {
		t.Fatalf("existing destination changed: %q", body)
	}
}

// Policy refusals happen before the datastore is opened and before any file is
// created in the destination: a refused export leaves nothing behind.
func TestExportRefusesBadHybridPolicyBeforeAnyOutput(t *testing.T) {
	_, hybrid, err := backup.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	_, classical, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	many := make([]string, backup.MaxHybridRecipients+1)
	for i := range many {
		if _, many[i], err = backup.GenerateHybridIdentity(); err != nil {
			t.Fatal(err)
		}
	}
	for name, tc := range map[string]struct {
		args       []string
		configured []string
		want       error
	}{
		"mixed flags":       {args: []string{"--recipient", hybrid, "--recipient", classical}, want: backup.ErrMixedRecipients},
		"mixed config":      {configured: []string{classical, hybrid}, want: backup.ErrMixedRecipients},
		"over the bound":    {configured: many, want: backup.ErrTooManyHybridRecipients},
		"duplicate":         {args: []string{"--recipient", hybrid, "--recipient", hybrid}, want: backup.ErrDuplicateRecipient},
		"hybrid passphrase": {args: []string{"--recipient", hybrid, "--passphrase-file", writeSecret(t, "correct horse battery staple")}, want: backup.ErrRecipientExclusive},
	} {
		t.Run(name, func(t *testing.T) {
			destination := t.TempDir()
			cfg := &config.Config{BackupRecipients: tc.configured}
			err := RunBackup(t.Context(), cfg, quietLogger(), append([]string{"export", "--out", destination}, tc.args...), &bytes.Buffer{}, nil, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("export = %v, want %v", err, tc.want)
			}
			if entries, _ := os.ReadDir(destination); len(entries) != 0 {
				t.Fatalf("a refused export left %d entries behind", len(entries))
			}
		})
	}
}

func writeSecret(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// restore run takes either algorithm's identity file and refuses the other's
// before touching the target.
func TestRestoreUnlockReadsEitherIdentityFile(t *testing.T) {
	for _, generate := range []func() (string, string, error){backup.GenerateIdentity, backup.GenerateHybridIdentity} {
		identity, _, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		unlock, err := restoreUnlock(writeSecret(t, identity), "")
		if err != nil || unlock.Identity != identity {
			t.Fatalf("restoreUnlock: %v", err)
		}
		if _, err := backup.RecipientOf(unlock.Identity); err != nil {
			t.Fatalf("unlock identity unusable: %v", err)
		}
	}
}

// restore run refuses a header holding a hybrid stanza beside any other stanza
// type before it opens the target, whichever identity the operator holds.
func TestRestoreRunRefusesMixedHybridHeaderBeforeTarget(t *testing.T) {
	identity, recipient, err := backup.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	w, err := backup.Encrypt(&archive, backup.Options{Recipients: []string{recipient}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("not a real archive")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := archive.Bytes()
	mac := bytes.Index(raw, []byte("\n---"))
	mixed := append(append(append([]byte{}, raw[:mac+1]...), "-> X25519 AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"...), raw[mac+1:]...)
	from := filepath.Join(t.TempDir(), "mixed.age")
	if err := os.WriteFile(from, mixed, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored.db")
	cfg := &config.Config{Dev: true, Store: config.Datastore{Engine: config.EngineSQLite, Path: target}}
	err = RunRestore(t.Context(), cfg, quietLogger(), []string{"run", "--from", from, "--identity-file", writeSecret(t, identity)}, &bytes.Buffer{}, nil, nil)
	if !errors.Is(err, backup.ErrMixedHybridStanzas) {
		t.Fatalf("restore of a mixed header = %v, want ErrMixedHybridStanzas", err)
	}
	if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the refused restore touched the target: %v", statErr)
	}
}
