package releasetrust

import (
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// PQRoot is the PQ-capable pinned root. It is a new schema: a v1 reader refuses
// it (`ValidateRoot` requires the v1 identifier and the strict decode refuses
// the extra member), so a client without this reader fails closed instead of
// ignoring the PQ requirement.
type PQRoot struct {
	Root
	PQ PQRootPolicy `json:"pq"`
}

type PQRootPolicy struct {
	Profile    string      `json:"profile"`
	Generation int64       `json:"generation"`
	Recovery   []PQRootKey `json:"recovery"`
}

type PQRootKey struct {
	Algorithm string `json:"algorithm"`
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
	SHA256    string `json:"sha256"`
}

// PQPin is the verifier's own pinned PQ trust state. Its required recovery set
// is derived from the pinned root bytes, never from a presented object. It
// exists only after every pinned key parsed and matched its digest.
type PQPin struct {
	digest     releaseidentity.Digest
	generation int64
	recovery   []pqRequired
	root       Root // the v1 identity this pin extends
}

func (p PQPin) Valid() bool                    { return p.digest != "" }
func (p PQPin) Digest() releaseidentity.Digest { return p.digest }
func (p PQPin) Generation() int64              { return p.generation }

// NewPQPin parses and validates a PQ root. keyFiles supplies the public key
// bytes named by the root (the same independent-fetch pattern as v1); a file
// whose digest differs from the root is refused. The classical member must be
// the v1 recovery key, so the PQ form is "in addition", never instead.
func NewPQPin(rootRaw []byte, keyFiles map[string][]byte) (PQPin, error) {
	var root PQRoot
	if err := decodeDocument(rootRaw, &root); err != nil {
		return PQPin{}, pqErr(ErrPQMalformed, "root")
	}
	if root.Schema != PQRootSchema {
		return PQPin{}, pqErr(ErrPQUnsupported, "root schema")
	}
	if !validRootKey(root.Recovery) || !validRootKey(root.BootstrapPrimary) || root.PQ.Profile != PQProfile || root.PQ.Generation < 1 || len(root.PQ.Recovery) != 2 {
		return PQPin{}, pqErr(ErrPQMalformed, "root fields")
	}
	classical, post := root.PQ.Recovery[0], root.PQ.Recovery[1]
	if classical.Algorithm != AlgorithmECDSAP256 || post.Algorithm != AlgorithmMLDSA65 ||
		classical.ID != root.Recovery.ID || classical.PublicKey != root.Recovery.PublicKey || classical.SHA256 != root.Recovery.SHA256 {
		return PQPin{}, pqErr(ErrPQScope, "recovery set must be the v1 recovery key plus one ML-DSA-65 key")
	}
	if post.ID == classical.ID || post.PublicKey == classical.PublicKey || post.ID == root.BootstrapPrimary.ID || !validRootKey(RootKey{ID: post.ID, PublicKey: post.PublicKey, SHA256: post.SHA256}) {
		return PQPin{}, pqErr(ErrPQScope, "ML-DSA recovery key must be distinct")
	}
	pin := PQPin{digest: releaseidentity.Hash(rootRaw), generation: root.PQ.Generation, root: root.Root}
	for _, entry := range root.PQ.Recovery {
		raw, ok := keyFiles[entry.PublicKey]
		if !ok || digestHex(raw) != entry.SHA256 {
			return PQPin{}, pqErr(ErrPQScope, "recovery key unavailable or digest mismatch")
		}
		key, err := parsePQKey(entry.Algorithm, raw)
		if err != nil {
			return PQPin{}, err
		}
		pin.recovery = append(pin.recovery, pqRequired{algorithm: entry.Algorithm, key: key})
	}
	return pin, nil
}

// PQKeyset is the recovery-signed, sequence-only list of maintainer-held
// release keys. A release key signs manifests and artifacts; it cannot sign
// this keyset, a policy or a bridge, and CI holds none of these keys.
type PQKeyset struct {
	Schema   string         `json:"schema"`
	Sequence int64          `json:"sequence"`
	Keys     []PQReleaseKey `json:"keys"`
}

type PQReleaseKey struct {
	Algorithm                   string `json:"algorithm"`
	ID                          string `json:"id"`
	PublicKey                   string `json:"public_key"`
	SHA256                      string `json:"sha256"`
	ValidFromReleaseSequence    int64  `json:"valid_from_release_sequence"`
	ValidThroughReleaseSequence *int64 `json:"valid_through_release_sequence"`
	Revoked                     bool   `json:"revoked"`
}

type verifiedKeyset struct {
	sequence int64
	digest   releaseidentity.Digest
	// pin is the pin the keyset verified under; later stages refuse a keyset
	// object that belongs to another pin.
	pin  releaseidentity.Digest
	keys []verifiedReleaseKey
}

type verifiedReleaseKey struct {
	meta PQReleaseKey
	key  pqKey
}

// VerifiedPQKeyset is immutable authenticated release-key authority.
type VerifiedPQKeyset struct{ state *verifiedKeyset }

func (k VerifiedPQKeyset) Valid() bool { return k.state != nil }

// VerifyPQKeyset authenticates a keyset under the pinned recovery set (every
// algorithm) and advances only the keyset floor. Revocation and rotation are
// new sequences of this document, so each needs the full recovery set.
func VerifyPQKeyset(pin PQPin, floor PQFloor, keysetRaw, setRaw []byte, keyFiles map[string][]byte) (VerifiedPQKeyset, PQFloor, error) {
	if err := floor.check(pin); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, err
	}
	var keyset PQKeyset
	if err := decodeDocument(keysetRaw, &keyset); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, pqErr(ErrPQMalformed, "keyset")
	}
	if keyset.Schema != PQKeysetSchema {
		return VerifiedPQKeyset{}, PQFloor{}, pqErr(ErrPQUnsupported, "keyset schema")
	}
	if keyset.Sequence < 1 || len(keyset.Keys) == 0 || len(keyset.Keys) > 64 {
		return VerifiedPQKeyset{}, PQFloor{}, pqErr(ErrPQBound, "keyset")
	}
	binding := PQBinding{Kind: PQKindKeyset, Channel: string(releaseidentity.StableV1), Subject: "pq-release-keys.json", Sequence: uint64(keyset.Sequence), Policy: pin.digest}
	if err := verifyPQSet(pin, pin.recovery, binding, sha256Of(keysetRaw), setRaw); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, err
	}
	digest := releaseidentity.Hash(keysetRaw)
	if err := checkFloor(keyset.Sequence, digest, floor.KeysetSequence, floor.KeysetSHA256); err != nil {
		return VerifiedPQKeyset{}, PQFloor{}, pqErr(ErrPQRollback, "keyset sequence")
	}
	state := &verifiedKeyset{sequence: keyset.Sequence, digest: digest, pin: pin.digest}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, entry := range keyset.Keys {
		if entry.Algorithm != AlgorithmMLDSA65 || entry.ID == "" || !validRootKey(RootKey{ID: entry.ID, PublicKey: entry.PublicKey, SHA256: entry.SHA256}) ||
			entry.ValidFromReleaseSequence < 1 || (entry.ValidThroughReleaseSequence != nil && *entry.ValidThroughReleaseSequence < entry.ValidFromReleaseSequence) ||
			ids[entry.ID] || names[entry.PublicKey] {
			return VerifiedPQKeyset{}, PQFloor{}, pqErr(ErrPQMalformed, "keyset entry")
		}
		ids[entry.ID], names[entry.PublicKey] = true, true
		raw, ok := keyFiles[entry.PublicKey]
		if !ok || digestHex(raw) != entry.SHA256 {
			return VerifiedPQKeyset{}, PQFloor{}, pqErr(ErrPQScope, "release key unavailable or digest mismatch")
		}
		key, err := parsePQKey(entry.Algorithm, raw)
		if err != nil {
			return VerifiedPQKeyset{}, PQFloor{}, err
		}
		if slices.ContainsFunc(pin.recovery, func(member pqRequired) bool { return member.key.id == key.id }) {
			return VerifiedPQKeyset{}, PQFloor{}, pqErr(ErrPQScope, "release key reuses a recovery key")
		}
		state.keys = append(state.keys, verifiedReleaseKey{meta: entry, key: key})
	}
	next := floor.withPin(pin)
	next.KeysetSequence, next.KeysetSHA256, next.KeysetPinSHA256 = keyset.Sequence, digest, pin.digest
	return VerifiedPQKeyset{state: state}, next, nil
}

// releaseSet returns the one release key covering sequence. Exactly one entry
// may cover a sequence and it must not be revoked: a revoked entry still counts
// toward coverage, so replacing a revoked key over the same range means removing
// its entry (or closing its range before the sequence), not adding a second
// entry beside it. This mirrors the v1 primary-key rule.
func (k VerifiedPQKeyset) releaseSet(sequence int64) ([]pqRequired, error) {
	if !k.Valid() {
		return nil, pqErr(ErrPQScope, "unverified keyset")
	}
	var covering []verifiedReleaseKey
	for _, entry := range k.state.keys {
		if entry.meta.ValidFromReleaseSequence <= sequence && (entry.meta.ValidThroughReleaseSequence == nil || *entry.meta.ValidThroughReleaseSequence >= sequence) {
			covering = append(covering, entry)
		}
	}
	if len(covering) != 1 || covering[0].meta.Revoked {
		return nil, pqErr(ErrPQScope, "release signer is revoked or ambiguously authorized")
	}
	return []pqRequired{{algorithm: AlgorithmMLDSA65, key: covering[0].key}}, nil
}
