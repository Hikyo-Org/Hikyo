package backupreceipt

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// AuthenticatedArchive proves full container authentication and exact receipt
// and route binding. It does not attest that an actual database was restored:
// scratch admission must independently inspect that database under exclusion.
// The plaintext is unlinked before this object is returned. Only read-only,
// independent cursors are exposed, never a pathname or writable descriptor.
type AuthenticatedArchive struct {
	file          *os.File
	size          int64
	snapshot      Snapshot
	planDigest    releaseidentity.Digest
	receiptDigest releaseidentity.Digest
}

func (a *AuthenticatedArchive) Valid() bool {
	return a != nil && a.file != nil && a.size > 0 && a.planDigest.Validate() == nil && a.receiptDigest.Validate() == nil
}
func (a *AuthenticatedArchive) Snapshot() Snapshot {
	if !a.Valid() {
		return Snapshot{}
	}
	snapshot := a.snapshot
	snapshot.RecipientFingerprints = slices.Clone(snapshot.RecipientFingerprints)
	return snapshot
}
func (a *AuthenticatedArchive) PlanDigest() releaseidentity.Digest {
	if !a.Valid() {
		return ""
	}
	return a.planDigest
}
func (a *AuthenticatedArchive) ReceiptDigest() releaseidentity.Digest {
	if !a.Valid() {
		return ""
	}
	return a.receiptDigest
}
func (a *AuthenticatedArchive) Open() (io.ReadSeeker, error) {
	if !a.Valid() {
		return nil, errors.New("authenticated upgrade archive unavailable")
	}
	return io.NewSectionReader(a.file, 0, a.size), nil
}
func (a *AuthenticatedArchive) Close() error {
	if a == nil || a.file == nil {
		return nil
	}
	err := a.file.Close()
	a.file = nil
	return err
}

// AuthenticateArchive consumes the actual pinned ciphertext through age's final
// authentication chunk. Public claims, a supplied hash or an earlier decrypt
// cannot mint this proof. Root escrow is deliberately absent from this API.
func AuthenticateArchive(ctx context.Context, ciphertext *Ciphertext, receiptRaw []byte, plan upgradecompat.Plan, unlock backup.Unlock, parent string) (*AuthenticatedArchive, error) {
	receipt, err := ParseReceipt(receiptRaw)
	if err != nil {
		return nil, err
	}
	if err := CheckReceiptPlan(receipt, plan); err != nil {
		return nil, err
	}
	recipient, err := unlock.UpgradeRecipientFingerprint()
	if err != nil || !slices.Contains(receipt.Snapshot.RecipientFingerprints, recipient) {
		return nil, errors.New("backup identity absent from public recipient inventory")
	}
	if err := ciphertext.Check(ctx, receipt); err != nil {
		return nil, err
	}
	sealed, err := ciphertext.Open()
	if err != nil {
		return nil, err
	}
	defer sealed.Close()
	directory, err := os.MkdirTemp(parent, ".hikyo-authenticated-archive-")
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	root, err := openOwnedRoot(directory, info)
	if err != nil {
		_ = removeOwnedDirectory(directory, info)
		return nil, err
	}
	defer func() {
		_ = root.Remove("archive.tar")
		_ = root.Close()
		if current, err := os.Lstat(directory); err == nil && os.SameFile(info, current) {
			_ = removeOwnedDirectory(directory, info)
		}
	}()
	out, err := root.OpenFile("archive.tar", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	// Detach the empty inode before writing any plaintext. A replaced staging
	// pathname cannot redirect decryption or later proof cursors.
	writtenInfo, statErr := out.Stat()
	plain, err := openOwnedReadonly(root, "archive.tar")
	if err != nil {
		_ = out.Close()
		return nil, err
	}
	retained := false
	defer func() {
		if !retained {
			_ = plain.Close()
		}
	}()
	openedInfo, openedErr := plain.Stat()
	if statErr != nil || openedErr != nil || !os.SameFile(writtenInfo, openedInfo) {
		_ = out.Close()
		return nil, errors.New("plaintext staging inode changed")
	}
	if err := root.Remove("archive.tar"); err != nil {
		_ = out.Close()
		return nil, errors.New("could not detach plaintext staging inode")
	}
	hash := sha256.New()
	stream := &contextReader{ctx: ctx, reader: io.TeeReader(io.LimitReader(sealed, receipt.CiphertextBytes+1), hash)}
	decryptErr := backup.ExtractTo(out, stream, unlock)
	if err := errors.Join(decryptErr, out.Close()); err != nil {
		return nil, errors.New("upgrade archive did not fully decrypt and authenticate")
	}
	if releaseidentity.Digest(hex.EncodeToString(hash.Sum(nil))) != receipt.CiphertextSHA256 {
		return nil, errors.New("ciphertext changed while decrypting")
	}
	stat, err := plain.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > MaxCiphertextBytes {
		return nil, errors.New("invalid authenticated archive size")
	}
	if err := matchAuthenticatedManifest(plain, receipt); err != nil {
		return nil, err
	}
	retained = true
	return &AuthenticatedArchive{file: plain, size: stat.Size(), snapshot: receipt.Snapshot, planDigest: plan.Digest(), receiptDigest: releaseidentity.Hash(receiptRaw)}, nil
}

func matchAuthenticatedManifest(plain io.Reader, receipt Receipt) error {
	archive := tar.NewReader(plain)
	header, err := archive.Next()
	if err != nil || header.Name != "manifest.json" || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > MaxArtifactBytes {
		return errors.New("authenticated archive manifest unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(archive, MaxArtifactBytes+1))
	if err != nil || releaseidentity.Hash(raw) != receipt.ManifestSHA256 {
		return errors.New("public receipt differs from exact encrypted manifest")
	}
	// The owning store parser validates the complete envelope and payload before
	// restore. Here only the nested authority is interpreted, without duplicating
	// or weakening that closed schema in this leaf package.
	var members map[string]json.RawMessage
	if definitions.DecodeStrict(raw, &members) != nil {
		return errors.New("invalid authenticated manifest JSON")
	}
	var format string
	var engine releaseidentity.Engine
	var created time.Time
	var snapshot Snapshot
	if json.Unmarshal(members["format"], &format) != nil || format != "hikyo-upgrade-backup/v2" || json.Unmarshal(members["engine"], &engine) != nil || json.Unmarshal(members["created_at"], &created) != nil || json.Unmarshal(members["upgrade"], &snapshot) != nil || !reflect.DeepEqual(snapshot, receipt.Snapshot) || engine != snapshot.Engine || !created.Equal(snapshot.CreatedAt) {
		return errors.New("authenticated manifest authority differs from receipt")
	}
	return nil
}
