package backupreceipt

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func TestAuthenticatedArchiveRequiresActualCompleteContainer(t *testing.T) {
	f := newSignedEvidenceFixture(t, true)
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	unlock := backup.Unlock{Identity: identity}
	options := backup.Options{Recipients: []string{recipient}}
	receipt, err := ParseReceipt(f.material.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Snapshot.RecipientFingerprints, err = options.UpgradeRecipientFingerprints()
	if err != nil {
		t.Fatal(err)
	}
	manifest := testfixture.JSON(t, map[string]any{"format": "hikyo-upgrade-backup/v2", "engine": receipt.Snapshot.Engine, "created_at": receipt.Snapshot.CreatedAt, "upgrade": receipt.Snapshot})
	var plaintext bytes.Buffer
	archive := tar.NewWriter(&plaintext)
	if err := archive.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(manifest)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(manifest); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	writer, err := backup.Encrypt(&encrypted, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(plaintext.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	receipt.ManifestSHA256 = releaseidentity.Hash(manifest)
	for _, name := range []string{"complete", "truncated", "corrupt", "wrong receipt manifest", "wrong snapshot", "wrong identity"} {
		t.Run(name, func(t *testing.T) {
			ciphertext := bytes.Clone(encrypted.Bytes())
			claimed := receipt
			held := unlock
			switch name {
			case "truncated":
				ciphertext = ciphertext[:len(ciphertext)-1]
			case "corrupt":
				ciphertext[len(ciphertext)-10] ^= 1
			case "wrong receipt manifest":
				claimed.ManifestSHA256 = releaseidentity.Hash([]byte("other manifest"))
			case "wrong snapshot":
				claimed.Snapshot.RestoreEpoch++
			case "wrong identity":
				other, _, err := backup.GenerateIdentity()
				if err != nil {
					t.Fatal(err)
				}
				held.Identity = other
			}
			path := filepath.Join(t.TempDir(), "backup.age")
			if err := os.WriteFile(path, ciphertext, 0600); err != nil {
				t.Fatal(err)
			}
			pinned, err := PinCiphertext(context.Background(), path, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer pinned.Close()
			// Even matching public hashes of truncated/corrupt bytes cannot mint proof.
			claimed.CiphertextSHA256 = pinned.Digest()
			claimed.CiphertextBytes = pinned.Size()
			staging := t.TempDir()
			raw := testfixture.JSON(t, claimed)
			authenticated, err := AuthenticateArchive(context.Background(), pinned, raw, f.plan, held, staging)
			entries, readErr := os.ReadDir(staging)
			if readErr != nil || len(entries) != 0 {
				t.Fatal("plaintext pathname survived factory", readErr)
			}
			if name != "complete" {
				if err == nil || authenticated != nil {
					t.Fatal("invalid archive minted proof")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !authenticated.Valid() || authenticated.PlanDigest() != f.plan.Digest() || authenticated.ReceiptDigest() != releaseidentity.Hash(raw) {
				t.Fatal("proof binding missing")
			}
			exposed := authenticated.Snapshot()
			exposed.RecipientFingerprints[0] = "mutated"
			if authenticated.Snapshot().RecipientFingerprints[0] == "mutated" {
				t.Fatal("proof mutable")
			}
			for range 2 {
				cursor, err := authenticated.Open()
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(cursor)
				if err != nil || !bytes.Equal(got, plaintext.Bytes()) {
					t.Fatal("read-only independent archive cursor differs", err)
				}
				if _, writable := cursor.(io.Writer); writable {
					t.Fatal("plaintext writable")
				}
			}
			if err := authenticated.Close(); err != nil {
				t.Fatal(err)
			}
			if authenticated.Valid() {
				t.Fatal("closed proof remained usable")
			}
		})
	}
}

func TestAuthenticatedArchiveRejectsSparseMetadata(t *testing.T) {
	if err := validateArchiveHeader(&tar.Header{
		Typeflag:   tar.TypeReg,
		PAXRecords: map[string]string{"GNU.sparse.realsize": "1099511627776"},
	}); err == nil {
		t.Fatal("GNU sparse PAX metadata was accepted")
	}
	if err := validateArchiveHeader(&tar.Header{Typeflag: tar.TypeGNUSparse}); err == nil {
		t.Fatal("GNU sparse member type was accepted")
	}
}

func TestAuthenticatedManifestRejectsCaseVariantAuthorityMember(t *testing.T) {
	f := newSignedEvidenceFixture(t, true)
	receipt, err := ParseReceipt(f.material.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	manifest := testfixture.JSON(t, map[string]any{"format": "hikyo-upgrade-backup/v2", "engine": receipt.Snapshot.Engine, "created_at": receipt.Snapshot.CreatedAt, "upgrade": receipt.Snapshot})
	manifest = append(bytes.Clone(manifest[:len(manifest)-1]), []byte(`,"UPGRADE":null}`)...)
	var plaintext bytes.Buffer
	archive := tar.NewWriter(&plaintext)
	if err := archive.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(manifest)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(manifest); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	receipt.ManifestSHA256 = releaseidentity.Hash(manifest)
	if err := matchAuthenticatedManifest(bytes.NewReader(plaintext.Bytes()), receipt); err == nil || !strings.Contains(err.Error(), "invalid authenticated manifest JSON") {
		t.Fatalf("case-variant authenticated authority accepted: %v", err)
	}
}
