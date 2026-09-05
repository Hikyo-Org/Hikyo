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
const MaxUpgradeRecipients = 64

// UpgradeRecipientFingerprints identifies canonical public X25519 recipients.
// Ordinary passphrase exports remain supported; they cannot produce a public
// upgrade receipt because a passphrase has no public recipient identity.
func (o Options) UpgradeRecipientFingerprints() ([]string, error) {
	if o.Passphrase != "" || len(o.Recipients) == 0 || len(o.Recipients) > MaxUpgradeRecipients {
		return nil, errors.New("backup: upgrade export requires 1 to 64 public X25519 recipients; create a fresh public-recipient backup")
	}
	result := make([]string, 0, len(o.Recipients))
	for _, encoded := range o.Recipients {
		r, err := age.ParseX25519Recipient(strings.TrimSpace(encoded))
		if err != nil {
			return nil, errors.New("backup: invalid upgrade public recipient")
		}
		result = append(result, recipientFingerprint(r))
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
		return "", errors.New("backup: upgrade drill requires a separately held X25519 identity")
	}
	i, err := age.ParseX25519Identity(strings.TrimSpace(u.Identity))
	if err != nil {
		return "", errors.New("backup: invalid upgrade age identity")
	}
	return recipientFingerprint(i.Recipient()), nil
}

func recipientFingerprint(recipient *age.X25519Recipient) string {
	digest := sha256.Sum256([]byte(recipient.String()))
	return "age-x25519-sha256:" + hex.EncodeToString(digest[:])
}
