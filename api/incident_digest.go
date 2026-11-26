package api

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
)

// IncidentItemsDigest names a set of incident item ids: the lowercase hex
// SHA-256 of the ids sorted bytewise and joined by "\n". An operator confirms an
// incident resume by sending the digest of the open items they listed, and the
// server compares it with the set it would run.
func IncidentItemsDigest(ids []string) string {
	sorted := slices.Sorted(slices.Values(ids))
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(sum[:])
}
