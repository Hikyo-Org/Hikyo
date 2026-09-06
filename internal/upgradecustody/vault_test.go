//go:build unix

package upgradecustody

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

const instance = "ins_22222222222222222222222222222222"

func testDirectory(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "custody")
}

func testVault(t *testing.T) (string, *Vault) {
	t.Helper()
	dir := testDirectory(t)
	v, err := create(dir, []byte("correct horse battery staple"), bytes.Repeat([]byte{0x7b}, 32), instance, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	return dir, v
}

func TestEncryptedCustodyRoundTripAndClose(t *testing.T) {
	dir, v := testVault(t)
	raw, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{v.RootKey(), v.identity, []byte(base64.StdEncoding.EncodeToString(v.RootKey())), []byte("correct horse battery staple"), []byte("root_escrow")} {
		if bytes.Contains(raw, secret) {
			t.Fatal("plaintext secret present in encrypted custody")
		}
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != fileName {
		t.Fatalf("unexpected custody files: %v %v", files, err)
	}
	got, err := open(dir, []byte("correct horse battery staple"), instance, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	if !bytes.Equal(got.RootKey(), v.RootKey()) || !bytes.Equal(got.PublicKey(), v.PublicKey()) || got.Recipient() != v.Recipient() || got.Pin().KeyID() != v.Pin().KeyID() {
		t.Fatal("reopened custody identity changed")
	}
	recipient, err := backup.RecipientOf(got.BackupUnlock().Identity)
	if err != nil || recipient != got.Recipient() {
		t.Fatal("backup unlock differs from recipient")
	}
	rootCopy, pubCopy := got.RootKey(), got.PublicKey()
	clear(rootCopy)
	clear(pubCopy)
	if !bytes.Equal(got.RootKey(), v.RootKey()) || !bytes.Equal(got.PublicKey(), v.PublicKey()) {
		t.Fatal("accessors exposed owned buffers")
	}
	if fmt.Sprintf("%v", got) != "[operator custody]" || fmt.Sprintf("%#v", got) != "[operator custody]" {
		t.Fatal("formatted vault can expose private fields")
	}
	ownedIdentity, ownedRoot, key := got.identity, got.root, got.key
	got.Close()
	got.Close()
	if len(got.RootKey()) != 0 || got.BackupUnlock().Identity != "" || key.D.Sign() != 0 || !bytes.Equal(ownedIdentity, make([]byte, len(ownedIdentity))) || !bytes.Equal(ownedRoot, make([]byte, len(ownedRoot))) {
		t.Fatal("Close left owned secret material usable")
	}
}

func TestUnlockRejectsWrongPassphraseInstanceCorruptionAndCost(t *testing.T) {
	dir, _ := testVault(t)
	path := filepath.Join(dir, fileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, password, instance string
		raw                      []byte
	}{
		{"wrong password", "wrong", instance, raw},
		{"wrong installation", "correct horse battery staple", "ins_33333333333333333333333333333333", raw},
		{"truncated ciphertext", "correct horse battery staple", instance, raw[:len(raw)-1]},
		{"excessive work", "correct horse battery staple", instance, bytes.Replace(raw, []byte(" 18\n"), []byte(" 30\n"), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, tc.raw, 0600); err != nil {
				t.Fatal(err)
			}
			v, err := open(dir, []byte(tc.password), tc.instance, os.Geteuid())
			if err == nil {
				v.Close()
				t.Fatal("invalid custody unlocked")
			}
			if strings.Contains(err.Error(), tc.password) {
				t.Fatal("unlock error revealed passphrase")
			}
		})
	}
}

func validAttestation(v *Vault, now time.Time) backupreceipt.Attestation {
	return backupreceipt.Attestation{
		Authority: backupreceipt.LedgerAuthority, Format: backupreceipt.AttestationFormat,
		ReceiptSHA256: releaseidentity.Hash([]byte("receipt")), RouteSHA256: releaseidentity.Hash([]byte("route")), BridgeSHA256: []releaseidentity.Digest{},
		TargetIdentity: releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compatibility")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))},
		InstanceID:     instance, RecoveryIncarnation: backupreceipt.Nonce(strings.Repeat("3", 64)), SourceGeneration: 1, RouteGeneration: 2,
		OperatorKeyID: v.Pin().KeyID(), IssuedAt: now, ExpiresAt: now.Add(time.Hour), Nonce: backupreceipt.Nonce(strings.Repeat("4", 64)),
	}
}

func TestAttestationSigningBindsInstallationKeyAndWindow(t *testing.T) {
	_, v := testVault(t)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	a := validAttestation(v, now)
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := v.SignAttestation(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := releasetrust.VerifyOperatorSignature(v.PublicKey(), sig, raw); err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(raw, []byte("\"restore_epoch\":0"), []byte("\"restore_epoch\":1"), 1)
	if err := releasetrust.VerifyOperatorSignature(v.PublicKey(), sig, tampered); err == nil {
		t.Fatal("modified statement verified")
	}
	for _, tc := range []struct {
		name  string
		alter func(*backupreceipt.Attestation)
		at    time.Time
	}{
		{"wrong instance", func(a *backupreceipt.Attestation) { a.InstanceID = "ins_33333333333333333333333333333333" }, now},
		{"wrong key", func(a *backupreceipt.Attestation) { a.OperatorKeyID = releaseidentity.Hash([]byte("other key")) }, now},
		{"invalid statement", func(a *backupreceipt.Attestation) { a.ReceiptSHA256 = "bad" }, now},
		{"future", func(*backupreceipt.Attestation) {}, now.Add(-time.Second)},
		{"expired", func(*backupreceipt.Attestation) {}, now.Add(time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := a
			tc.alter(&changed)
			raw, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := v.SignAttestation(raw, tc.at); err == nil {
				t.Fatal("unbound or unusable attestation signed")
			}
		})
	}
	v.Close()
	if _, err := v.SignAttestation(raw, now); err == nil {
		t.Fatal("closed custody signed")
	}
}

func TestCustodyNeverReplacesExistingKeys(t *testing.T) {
	dir, v := testVault(t)
	before, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := create(dir, []byte("different password"), bytes.Repeat([]byte{0x21}, 32), instance, os.Geteuid()); err == nil {
		t.Fatal("existing custody replaced")
	}
	after, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed create changed existing custody")
	}
	if !v.Pin().Valid() {
		t.Fatal("existing pin invalidated")
	}
}

func TestCustodyRefusesUnsafePathsAndFiles(t *testing.T) {
	dir, _ := testVault(t)
	path := filepath.Join(dir, fileName)
	for _, mode := range []os.FileMode{0644, 0400, 0660} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := open(dir, []byte("correct horse battery staple"), instance, os.Geteuid()); err == nil {
			t.Fatal("unsafe custody file permissions accepted")
		}
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := open(dir, []byte("correct horse battery staple"), instance, os.Geteuid()); err == nil {
		t.Fatal("unsafe custody directory permissions accepted")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(dir), "linked-custody")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := open(link, []byte("correct horse battery staple"), instance, os.Geteuid()); err == nil {
		t.Fatal("symlink directory accepted")
	}
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if _, err := open(dir, []byte("correct horse battery staple"), instance, os.Geteuid()); err == nil {
		t.Fatal("symlink file accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if _, err := open(dir, []byte("correct horse battery staple"), instance, os.Geteuid()); err == nil {
		t.Fatal("hardlinked custody accepted")
	}
	if os.Geteuid() != 0 {
		if _, err := Create(testDirectory(t), []byte("passphrase"), make([]byte, 32), instance); err == nil {
			t.Fatal("non-root production custody accepted")
		}
	}
}

func TestCustodyRefusesUnsafeAncestorAndOversizedFile(t *testing.T) {
	dir, _ := testVault(t)
	parent := filepath.Dir(dir)
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := open(dir, []byte("correct horse battery staple"), instance, os.Geteuid()); err == nil {
		t.Fatal("writable ancestor accepted")
	}
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), make([]byte, maxCiphertext+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := open(dir, []byte("correct horse battery staple"), instance, os.Geteuid()); err == nil {
		t.Fatal("oversized ciphertext accepted")
	}
}
