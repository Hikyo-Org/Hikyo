package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SCIMSubjectDigest is a blinded, per-event subject commitment. The private
// blinding key is never persisted or emitted, so even predictable subjects
// cannot be guessed from an audit export. Immutable resource/account IDs, not
// this commitment, provide audit correlation. Its spelling retains the v1
// payload's 64 lowercase hexadecimal characters.
func SCIMSubjectDigest(org, binding, issuer, subject string) (string, error) {
	key, err := RandomBytes(KeySize)
	if err != nil {
		return "", fmt.Errorf("crypto: blind SCIM audit subject: %w", err)
	}
	defer Zero(key)
	return scimSubjectDigest(key, org, binding, issuer, subject), nil
}

func scimSubjectDigest(key []byte, org, binding, issuer, subject string) string {
	message := appendLP(nil, []byte("hikyo/scim-subject-audit/v1"))
	for _, part := range []string{org, binding, issuer, subject} {
		message = appendLP(message, []byte(part))
	}
	defer Zero(message)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return hex.EncodeToString(mac.Sum(nil))
}
