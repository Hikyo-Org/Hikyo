package releasetrust

import (
	"errors"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// RevocationSchema is the recovery-signed list of revoked bridge statements.
const RevocationSchema = "hikyo.dev/recovery-revocations/v1"

// MaxRevocations bounds one list so a hostile endpoint cannot exhaust memory.
const MaxRevocations = 256

// RevocationList names bridge statement digests that must no longer be used.
// It is bound to the pinned trust root and ordered by Sequence; the transport
// that delivered it is never trusted.
type RevocationList struct {
	Schema          string                   `json:"schema"`
	Sequence        int64                    `json:"sequence"`
	IssuedAt        string                   `json:"issued_at"`
	TrustRootSHA256 releaseidentity.Digest   `json:"trust_root_sha256"`
	Revoked         []releaseidentity.Digest `json:"revoked"`
}

type revocationState struct {
	sequence int64
	issuedAt time.Time
	digest   releaseidentity.Digest
	revoked  []releaseidentity.Digest
}

// VerifiedRevocations can only be created by VerifyRevocations.
type VerifiedRevocations struct{ state *revocationState }

func (v VerifiedRevocations) Valid() bool { return v.state != nil }

// Sequence is the monotonic list counter used for rollback protection.
func (v VerifiedRevocations) Sequence() int64 {
	if v.state == nil {
		return 0
	}
	return v.state.sequence
}

// IssuedAt is the signed instant the recovery key issued this list.
func (v VerifiedRevocations) IssuedAt() time.Time {
	if v.state == nil {
		return time.Time{}
	}
	return v.state.issuedAt
}

// Digest is the hash of the exact signed bytes, to detect equivocation.
func (v VerifiedRevocations) Digest() releaseidentity.Digest {
	if v.state == nil {
		return ""
	}
	return v.state.digest
}

// Revoked returns the sorted, unique revoked statement digests.
func (v VerifiedRevocations) Revoked() []releaseidentity.Digest {
	if v.state == nil {
		return nil
	}
	return slices.Clone(v.state.revoked)
}

// VerifyRevocations authenticates a revocation list under the recovery key the
// snapshot was verified with and the root the binary pins.
// ErrRevocationRoot marks a list whose signature verified under the recovery key but
// that is bound to another trust root (for instance one from before a root rotation).
// It is returned only after the signature and schema checks passed.
var ErrRevocationRoot = errors.New("revocation list is bound to a different trust root")

func (s Snapshot) VerifyRevocations(raw, signature []byte) (VerifiedRevocations, error) {
	if !s.Valid() {
		return VerifiedRevocations{}, errors.New("unverified trust snapshot")
	}
	if err := VerifyKeySignature(s.state.recoveryKey, signature, raw); err != nil {
		return VerifiedRevocations{}, err
	}
	var list RevocationList
	if err := decodeDocument(raw, &list); err != nil {
		return VerifiedRevocations{}, err
	}
	if list.Schema != RevocationSchema || list.Sequence < 1 {
		return VerifiedRevocations{}, errors.New("invalid revocation list schema or sequence")
	}
	issued, err := time.Parse(time.RFC3339, list.IssuedAt)
	if err != nil || issued.UTC().Format(time.RFC3339) != list.IssuedAt {
		return VerifiedRevocations{}, errors.New("revocation list issued_at must be canonical UTC RFC 3339")
	}
	if list.TrustRootSHA256 != s.state.legacyStablePolicy {
		return VerifiedRevocations{}, ErrRevocationRoot
	}
	if len(list.Revoked) > MaxRevocations {
		return VerifiedRevocations{}, errors.New("revocation list is too long")
	}
	for i, digest := range list.Revoked {
		if digest.Validate() != nil || (i > 0 && list.Revoked[i-1] >= digest) {
			return VerifiedRevocations{}, errors.New("revocation list must hold sorted, unique, valid digests")
		}
	}
	return VerifiedRevocations{state: &revocationState{sequence: list.Sequence, issuedAt: issued, digest: releaseidentity.Hash(raw), revoked: slices.Clone(list.Revoked)}}, nil
}
