package backupreceipt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// CheckEvidenceArtifacts is a read-only advisory check. It returns no verified
// evidence or admission. Execution must pin its own ciphertext and verify the
// current datastore authority and consume the nonce under migration exclusion.
func CheckEvidenceArtifacts(ctx context.Context, pin PinnedOperator, plan upgradecompat.Plan, path string, material EvidenceMaterial, now time.Time) error {
	receipt, err := ParseReceipt(material.Receipt)
	if err != nil {
		return err
	}
	if _, err := checkBoundEvidence(pin, plan, material, receipt, now); err != nil {
		return err
	}
	file, err := openCiphertextSource(path)
	if err != nil {
		return errors.New("upgrade backup could not be opened")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != receipt.CiphertextBytes || info.Size() <= 0 || info.Size() > MaxCiphertextBytes {
		return errors.New("upgrade backup does not match receipt")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, &contextReader{ctx: ctx, reader: io.LimitReader(file, receipt.CiphertextBytes+1)})
	if err != nil || size != receipt.CiphertextBytes || releaseidentity.Digest(hex.EncodeToString(hash.Sum(nil))) != receipt.CiphertextSHA256 {
		return errors.New("upgrade backup does not match receipt")
	}
	return ctx.Err()
}
