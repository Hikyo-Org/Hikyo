package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

const importCommitmentDomain = "hikyo-import-values-commitment-v2\x00"

// NewImportCommitmentKey returns fresh private blinding material. It belongs
// only in a protected values artifact, never its committable manifest.
func NewImportCommitmentKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		Zero(key)
		return nil, fmt.Errorf("crypto: generate import commitment key: %w", err)
	}
	return key, nil
}

// ImportValuesCommitment authenticates canonical imported values under a
// private 256-bit key and a v2-only domain. Callers own canonical encoding;
// crypto owns this primitive and never returns the private key in the digest.
func ImportValuesCommitment(key, canonical []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, errors.New("crypto: import commitment requires a 256-bit private key")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(importCommitmentDomain))
	_, _ = mac.Write(canonical)
	return mac.Sum(nil), nil
}
