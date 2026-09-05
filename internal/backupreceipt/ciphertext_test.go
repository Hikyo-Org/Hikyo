//go:build !windows

package backupreceipt

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"golang.org/x/sys/unix"
)

func encryptedFixture(t *testing.T) ([]byte, backup.Unlock) {
	t.Helper()
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer, err := backup.Encrypt(&output, backup.Options{Recipients: []string{recipient}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "owned synthetic fixture"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes(), backup.Unlock{Identity: identity}
}

func TestPinnedCiphertextSurvivesOriginalRenameAndInPlaceSubstitution(t *testing.T) {
	for _, rename := range []bool{false, true} {
		t.Run(map[bool]string{false: "in-place", true: "renamed"}[rename], func(t *testing.T) {
			original, unlock := encryptedFixture(t)
			foreign, _ := encryptedFixture(t)
			root := t.TempDir()
			source := filepath.Join(root, "source.age")
			if err := os.WriteFile(source, original, 0600); err != nil {
				t.Fatal(err)
			}
			owned, err := PinCiphertext(t.Context(), source, root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := owned.Close(); err != nil {
					t.Error(err)
				}
			})
			if rename {
				if err := os.Rename(source, source+".original"); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(source, foreign, 0600); err != nil {
				t.Fatal(err)
			}
			receipt := fixtureReceipt()
			receipt.CiphertextSHA256 = releaseidentity.Hash(original)
			receipt.CiphertextBytes = int64(len(original))
			if err := owned.Check(t.Context(), receipt); err != nil {
				t.Fatal(err)
			}
			reader, err := owned.Open()
			if err != nil {
				t.Fatal(err)
			}
			var plaintext bytes.Buffer
			if err := backup.ExtractTo(&plaintext, reader, unlock); err != nil {
				reader.Close()
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if plaintext.String() != "owned synthetic fixture" {
				t.Fatal("drill read replacement bytes")
			}
			if err := owned.Close(); err != nil {
				t.Fatal(err)
			}
			persisted, err := os.ReadFile(source)
			if err != nil || !bytes.Equal(persisted, foreign) {
				t.Fatal("cleanup removed or altered unowned source")
			}
		})
	}
}

func TestPinnedCiphertextRehashRefusesModifiedOwnedBytes(t *testing.T) {
	archive, _ := encryptedFixture(t)
	root := t.TempDir()
	source := filepath.Join(root, "source.age")
	if err := os.WriteFile(source, archive, 0600); err != nil {
		t.Fatal(err)
	}
	owned, err := PinCiphertext(t.Context(), source, root)
	if err != nil {
		t.Fatal(err)
	}
	defer owned.Close()
	receipt := fixtureReceipt()
	receipt.CiphertextSHA256 = releaseidentity.Hash(archive)
	receipt.CiphertextBytes = int64(len(archive))
	archive[len(archive)-1] ^= 1
	if err := os.WriteFile(owned.path, archive, 0600); err != nil {
		t.Fatal(err)
	}
	if owned.Check(t.Context(), receipt) == nil {
		t.Fatal("modified owned ciphertext accepted from stale digest")
	}
}

func TestPinCiphertextRejectsSpecialFilesAndCancellationWithoutDebris(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "input.fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PinCiphertext(t.Context(), fifo, root); err == nil {
		t.Fatal("FIFO accepted")
	}
	archive, _ := encryptedFixture(t)
	source := filepath.Join(root, "source.age")
	if err := os.WriteFile(source, archive, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.age")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PinCiphertext(t.Context(), link, root); err == nil {
		t.Fatal("symlink accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := PinCiphertext(ctx, source, root); err == nil {
		t.Fatal("canceled copy accepted")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("failed copy retained a staging directory")
		}
	}
}

func TestPinnedCiphertextCleanupCannotRemoveReplacementDirectory(t *testing.T) {
	source := filepath.Join(t.TempDir(), "archive.age")
	if err := os.WriteFile(source, []byte("owned original archive"), 0600); err != nil {
		t.Fatal(err)
	}
	pinned, err := PinCiphertext(t.Context(), source, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	originalDirectory := pinned.directory
	moved := originalDirectory + "-moved"
	if err := os.Rename(originalDirectory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(originalDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(originalDirectory, "ciphertext.age")
	if err := os.WriteFile(replacement, []byte("unowned replacement data"), 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := pinned.Open()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(raw) != "owned original archive" {
		t.Fatal("pinned handle followed replacement directory")
	}
	if err := pinned.Close(); err == nil {
		t.Fatal("changed staging path silently accepted")
	}
	raw, err = os.ReadFile(replacement)
	if err != nil || string(raw) != "unowned replacement data" {
		t.Fatal("cleanup deleted replacement data")
	}
	if _, err := os.Stat(filepath.Join(moved, "ciphertext.age")); !os.IsNotExist(err) {
		t.Fatal("owned ciphertext was not removed through original directory handle")
	}
}
