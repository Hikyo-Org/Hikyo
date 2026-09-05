package backupreceipt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// Ciphertext owns an exact byte copy. A filename or an earlier hash of an
// operator-controlled inode is insufficient: a later drill must use this copy.
type Ciphertext struct {
	root          *os.Root
	directoryInfo os.FileInfo
	directory     string
	path          string
	digest        releaseidentity.Digest
	size          int64
}

// PinCiphertext opens source once and copies into a new owner-only directory.
// The receipt comparison must succeed before a scratch restore is attempted.
func PinCiphertext(ctx context.Context, source, parent string) (*Ciphertext, error) {
	in, err := openCiphertextSource(source)
	if err != nil {
		return nil, errors.New("upgrade backup could not be opened")
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxCiphertextBytes {
		return nil, errors.New("upgrade backup must be a bounded regular file")
	}
	directory, err := os.MkdirTemp(parent, ".hikyo-upgrade-evidence-")
	if err != nil {
		return nil, errors.New("upgrade evidence staging unavailable")
	}
	directoryInfo, err := os.Lstat(directory)
	if err != nil {
		return nil, errors.New("upgrade staging ownership unavailable")
	}
	root, err := openOwnedRoot(directory, directoryInfo)
	if err != nil {
		_ = removeOwnedDirectory(directory, directoryInfo)
		return nil, errors.New("upgrade staging handle unavailable")
	}
	retained := false
	defer func() {
		if !retained {
			_ = root.Remove("ciphertext.age")
			_ = root.Close()
			_ = removeOwnedDirectory(directory, directoryInfo)
		}
	}()
	path := filepath.Join(directory, "ciphertext.age")
	out, err := root.OpenFile("ciphertext.age", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, errors.New("upgrade evidence file unavailable")
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(out, hash), &contextReader{ctx: ctx, reader: io.LimitReader(in, MaxCiphertextBytes+1)})
	if err := errors.Join(copyErr, out.Sync(), out.Close()); err != nil || size <= 0 || size > MaxCiphertextBytes {
		return nil, errors.New("upgrade evidence copy did not complete")
	}
	ancestry, err := filedurability.DirectoryAncestry(directory)
	if err != nil {
		return nil, errors.New("upgrade evidence ancestry unavailable")
	}
	for _, path := range ancestry {
		if filedurability.SyncDirectory(path) != nil {
			return nil, errors.New("upgrade evidence directory durability unconfirmed")
		}
	}
	retained = true
	return &Ciphertext{root: root, directoryInfo: directoryInfo, directory: directory, path: path, digest: releaseidentity.Digest(hex.EncodeToString(hash.Sum(nil))), size: size}, nil
}

func (c *Ciphertext) Digest() releaseidentity.Digest {
	if c == nil {
		return ""
	}
	return c.digest
}
func (c *Ciphertext) Size() int64 {
	if c == nil {
		return 0
	}
	return c.size
}

// Open returns the owned bytes, never reopens the original operator pathname.
func (c *Ciphertext) Open() (*os.File, error) {
	if c == nil || c.root == nil || c.directory == "" || c.path == "" || c.digest.Validate() != nil || c.size <= 0 {
		return nil, errors.New("missing pinned upgrade ciphertext")
	}
	return openPinnedCiphertext(c.root)
}

// Check rehashes the owned object before consuming it. The supplied receipt is
// public claims, so all size/digest checks remain necessary after parsing.
func (c *Ciphertext) Check(ctx context.Context, receipt Receipt) error {
	if receipt.Validate() != nil || c == nil || c.digest != receipt.CiphertextSHA256 || c.size != receipt.CiphertextBytes {
		return errors.New("upgrade ciphertext does not match receipt")
	}
	in, err := c.Open()
	if err != nil {
		return errors.New("pinned upgrade ciphertext unavailable")
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != c.size {
		return errors.New("pinned upgrade ciphertext changed")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, &contextReader{ctx: ctx, reader: io.LimitReader(in, c.size+1)})
	if err != nil || size != c.size || releaseidentity.Digest(hex.EncodeToString(hash.Sum(nil))) != c.digest {
		return errors.New("pinned upgrade ciphertext changed")
	}
	return nil
}

// Close removes only the uniquely owned staging directory. It never removes
// the original backup, even if a drill fails or the original pathname changes.
func (c *Ciphertext) Close() error {
	if c == nil || c.directory == "" {
		return nil
	}
	if c.root == nil {
		return errors.New("upgrade staging ownership unavailable")
	}
	removeErr := c.root.Remove("ciphertext.age")
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	closeErr := c.root.Close()
	c.root = nil
	current, statErr := os.Lstat(c.directory)
	if statErr != nil || !os.SameFile(c.directoryInfo, current) {
		return errors.Join(removeErr, closeErr, errors.New("upgrade staging path changed; replacement preserved"))
	}
	directoryErr := os.Remove(c.directory)
	if err := errors.Join(removeErr, closeErr, directoryErr); err != nil {
		return err
	}
	c.directory = ""
	c.path = ""
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// ReadPublicArtifact performs a bounded no-follow regular-file read. Opening a
// pipe must not block an unattended local upgrade command before validation.
func ReadPublicArtifact(path string, maximum int64) ([]byte, error) {
	if maximum <= 0 || maximum > MaxSignatureBytes {
		return nil, errors.New("invalid public artifact byte bound")
	}
	file, err := openCiphertextSource(path)
	if err != nil {
		return nil, errors.New("public upgrade artifact unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum {
		return nil, errors.New("public upgrade artifact is not a bounded regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(raw)) > maximum {
		return nil, errors.New("public upgrade artifact exceeds bound")
	}
	return raw, nil
}

// A mutable pathname must still identify the originally captured directory
// before cleanup may remove it. Descriptor-relative file cleanup stays anchored
// to the original root even if the parent path was renamed or replaced.
func removeOwnedDirectory(directory string, expected os.FileInfo) error {
	current, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if expected == nil || !os.SameFile(expected, current) {
		return errors.New("upgrade staging path changed; replacement preserved")
	}
	return os.Remove(directory)
}
func openOwnedRoot(directory string, expected os.FileInfo) (*os.Root, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	current, err := root.Stat(".")
	if err != nil || expected == nil || !os.SameFile(expected, current) {
		_ = root.Close()
		return nil, errors.New("upgrade staging directory changed before handle acquisition")
	}
	return root, nil
}
