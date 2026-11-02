package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"

	"filippo.io/age"
)

// MaxUpgradeRecipients bounds the public upgrade receipt's custody inventory.
// A hybrid inventory is further bounded by MaxHybridRecipients.
const MaxUpgradeRecipients = 64

// Algorithm names the key class of a recipient inventory.
type Algorithm string

const (
	AlgorithmX25519 Algorithm = "x25519"
	AlgorithmHybrid Algorithm = "mlkem768x25519"

	// FingerprintPrefixX25519 keeps its exact historical meaning: SHA-256 of
	// the canonical X25519 recipient string. It is never reused for another
	// algorithm, and a classical key is never relabeled.
	FingerprintPrefixX25519 = "age-x25519-sha256:"
	// FingerprintPrefixHybrid names SHA-256 of the canonical hybrid recipient
	// string (ML-KEM-768 + X25519).
	FingerprintPrefixHybrid = "age-hybrid-mlkem768x25519-sha256:"
)

// ParseFingerprint splits a public recipient fingerprint into its algorithm
// and lowercase hex SHA-256. It reports false for an unknown namespace or a
// digest that is not exactly 64 lowercase hexadecimal characters.
func ParseFingerprint(fingerprint string) (Algorithm, string, bool) {
	for prefix, algorithm := range map[string]Algorithm{FingerprintPrefixX25519: AlgorithmX25519, FingerprintPrefixHybrid: AlgorithmHybrid} {
		digest, ok := strings.CutPrefix(fingerprint, prefix)
		if !ok {
			continue
		}
		if raw, err := hex.DecodeString(digest); err != nil || len(raw) != sha256.Size || digest != hex.EncodeToString(raw) {
			return "", "", false
		}
		return algorithm, digest, true
	}
	return "", "", false
}

// UpgradeRecipientFingerprints identifies canonical public recipients, all
// X25519 or all hybrid. Ordinary passphrase exports remain supported; they
// cannot produce a public upgrade receipt because a passphrase has no public
// recipient identity.
func (o Options) UpgradeRecipientFingerprints() ([]string, error) {
	if o.Passphrase != "" || len(o.Recipients) == 0 || len(o.Recipients) > MaxUpgradeRecipients {
		return nil, errors.New("backup: upgrade export requires 1 to 64 public age recipients (at most 32 hybrid); create a fresh public-recipient backup")
	}
	parsed, err := o.recipients()
	if errors.Is(err, ErrMixedRecipients) || errors.Is(err, ErrTooManyHybridRecipients) || errors.Is(err, ErrRecipientsTooLarge) || errors.Is(err, ErrDuplicateRecipient) {
		return nil, err
	}
	if err != nil {
		return nil, errors.New("backup: invalid upgrade public recipient")
	}
	result := make([]string, 0, len(parsed))
	for _, r := range parsed {
		fingerprint, ok := recipientFingerprint(r)
		if !ok {
			return nil, errors.New("backup: invalid upgrade public recipient")
		}
		result = append(result, fingerprint)
	}
	slices.Sort(result)
	if len(slices.Compact(slices.Clone(result))) != len(result) {
		return nil, errors.New("backup: duplicate upgrade public recipient")
	}
	return result, nil
}

// UpgradeRecipientFingerprint identifies the public recipient of the held age
// identity. The private identity is never returned, hashed or included in errors.
func (u Unlock) UpgradeRecipientFingerprint() (string, error) {
	if u.Identity == "" || u.Passphrase != "" {
		return "", errors.New("backup: upgrade drill requires a separately held age identity")
	}
	recipient, err := RecipientOf(u.Identity)
	if err != nil {
		return "", errors.New("backup: invalid upgrade age identity")
	}
	r, err := parseRecipient(0, recipient)
	if err != nil {
		return "", errors.New("backup: invalid upgrade age identity")
	}
	fingerprint, _ := recipientFingerprint(r)
	return fingerprint, nil
}

func recipientFingerprint(recipient age.Recipient) (string, bool) {
	switch r := recipient.(type) {
	case *age.X25519Recipient:
		digest := sha256.Sum256([]byte(r.String()))
		return FingerprintPrefixX25519 + hex.EncodeToString(digest[:]), true
	case *age.HybridRecipient:
		digest := sha256.Sum256([]byte(r.String()))
		return FingerprintPrefixHybrid + hex.EncodeToString(digest[:]), true
	}
	return "", false
}
