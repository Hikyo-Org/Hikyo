package releasetrust

// PQ-capable trust form (#1132, DEC-02, DEC-03, DEC-07, DEC-14).
//
// This file holds readers and verifiers only. No install, update or startup
// path calls it, no signer exists, and nothing requires a PQ signature: the v1
// documents, their decode, `Snapshot.Digest()` and `SnapshotFloor` are
// byte-identical to before. A reader that exists and is not required is the
// whole goal. `parsePinnedKey` is deliberately not widened: its callers are
// classical operator and primary-key surfaces (E-16), and ML-DSA is reachable
// only through the new schema identifiers below.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

const (
	PQProfile            = "pqc-auth/1"
	PQFormatVersion      = 1
	PQRootSchema         = "hikyo.dev/trust-root/v2"
	PQSignatureSetSchema = "hikyo.dev/pq-signature-set/v1"
	PQKeysetSchema       = "hikyo.dev/pq-release-keys/v1"

	// AlgorithmECDSAP256 is the existing P-256 recovery key; AlgorithmMLDSA65 is
	// DEC-02. The set is closed: ML-DSA-44/87, SLH-DSA, RSA and Ed25519 are not
	// accepted in the PQ-capable form.
	AlgorithmECDSAP256 = "ecdsa-p256-sha256"
	AlgorithmMLDSA65   = "mldsa65"

	mldsa65SignatureSize = 3309
	pqSetMaxBytes        = 64 << 10
	pqMaxSignatures      = 4
)

// Closed error classes (profiles.md, DEC-09). Detail text never carries key,
// signature or payload bytes.
var (
	ErrPQUnsupported = errors.New("pq trust: unsupported algorithm, schema or version")
	ErrPQMalformed   = errors.New("pq trust: malformed document")
	ErrPQBound       = errors.New("pq trust: over bound")
	ErrPQScope       = errors.New("pq trust: wrong key, role or binding")
	ErrPQSignature   = errors.New("pq trust: invalid signature")
	ErrPQMissing     = errors.New("pq trust: required signature missing")
	ErrPQRollback    = errors.New("pq trust: rollback or equivocation refused")
)

func pqErr(class error, detail string) error { return fmt.Errorf("%w: %s", class, detail) }

// Document kinds. A kind has exactly one label; the label is the ML-DSA
// context and the first length-prefixed field of every signature input.
const (
	PQKindKeyset          = "pq-keyset"
	PQKindMetadata        = "metadata"
	PQKindCatalog         = "catalog"
	PQKindStablePolicy    = "stable-policy"
	PQKindNightlyPolicy   = "nightly-policy"
	PQKindBridge          = "bridge"
	PQKindReleaseManifest = "release-manifest"
	PQKindArtifact        = "artifact"
)

var pqKinds = []string{PQKindKeyset, PQKindMetadata, PQKindCatalog, PQKindStablePolicy, PQKindNightlyPolicy, PQKindBridge, PQKindReleaseManifest, PQKindArtifact}

func pqLabel(kind string) string { return "hikyo/release-trust/" + kind + "/v1" }

// PQBinding is what a signer commits to beside the payload digest. The verifier
// derives every field from documents it already authenticated, never from the
// presented signature set.
type PQBinding struct {
	Kind     string
	Channel  string // releaseidentity.Profile value: stable/v1 or nightly/v1
	Version  string
	Subject  string // document or artifact name
	Source   string // 40-hex source commit, or empty for documents without one
	Sequence uint64
	Policy   releaseidentity.Digest
}

func (b PQBinding) validate() error {
	if !slices.Contains(pqKinds, b.Kind) {
		return pqErr(ErrPQUnsupported, "document kind")
	}
	if releaseidentity.Profile(b.Channel).Validate() != nil {
		return pqErr(ErrPQScope, "channel")
	}
	if b.Sequence < 1 || b.Policy.Validate() != nil || !releaseidentity.SafeName(b.Subject) {
		return pqErr(ErrPQScope, "sequence, policy or subject")
	}
	if b.Source != "" && !commitPattern.MatchString(b.Source) {
		return pqErr(ErrPQScope, "source identity")
	}
	if b.Kind == PQKindReleaseManifest || b.Kind == PQKindArtifact {
		if b.Version == "" || b.Source == "" {
			return pqErr(ErrPQScope, "release bindings need version and source")
		}
	}
	return nil
}

// pqKey is a parsed public key of the closed algorithm set.
type pqKey struct {
	algorithm string
	id        string // "<algorithm>:" + hex SHA-256 over LP(domain) LP(algorithm) LP(SPKI DER)
	ecdsa     *ecdsa.PublicKey
	mldsa     *mldsa.PublicKey
}

func lp(buf *bytes.Buffer, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	buf.Write(length[:])
	buf.Write(value)
}

func u64(buf *bytes.Buffer, value uint64) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], value)
	buf.Write(raw[:])
}

// parsePQKey accepts one canonical SPKI PEM of exactly the named algorithm.
func parsePQKey(algorithm string, publicKeyPEM []byte) (pqKey, error) {
	if len(publicKeyPEM) == 0 || len(publicKeyPEM) > MaxDocumentBytes {
		return pqKey{}, pqErr(ErrPQBound, "public key")
	}
	block, rest := pem.Decode(publicKeyPEM)
	// pem.Decode skips leading text, so also require the canonical re-encoding:
	// one key, no preamble, no headers, no alternate line wrapping.
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 || block.Type != "PUBLIC KEY" || len(block.Headers) != 0 ||
		!bytes.Equal(bytes.TrimSpace(publicKeyPEM), bytes.TrimSpace(pem.EncodeToMemory(block))) {
		return pqKey{}, pqErr(ErrPQMalformed, "public key PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return pqKey{}, pqErr(ErrPQMalformed, "public key encoding")
	}
	key := pqKey{algorithm: algorithm}
	switch public := parsed.(type) {
	case *ecdsa.PublicKey:
		if algorithm != AlgorithmECDSAP256 || public.Curve != elliptic.P256() {
			return pqKey{}, pqErr(ErrPQScope, "key does not match its declared algorithm")
		}
		key.ecdsa = public
	case *mldsa.PublicKey:
		if algorithm != AlgorithmMLDSA65 || public.Parameters() != mldsa.MLDSA65() {
			return pqKey{}, pqErr(ErrPQScope, "key does not match its declared algorithm")
		}
		key.mldsa = public
	default:
		return pqKey{}, pqErr(ErrPQUnsupported, "public key algorithm")
	}
	canonical, err := x509.MarshalPKIXPublicKey(parsed)
	if err != nil || !bytes.Equal(canonical, block.Bytes) {
		return pqKey{}, pqErr(ErrPQMalformed, "public key is not canonical")
	}
	var input bytes.Buffer
	lp(&input, []byte("hikyo/release-trust/key-id/v1"))
	lp(&input, []byte(algorithm))
	lp(&input, canonical)
	sum := sha256.Sum256(input.Bytes())
	key.id = algorithm + ":" + hex.EncodeToString(sum[:])
	return key, nil
}

// pqRequired is one member of a verifier-held required set. The slice order is
// part of the signed suite.
type pqRequired struct {
	algorithm string
	key       pqKey
}

// PQSignatureSet is the sidecar envelope. It names algorithms and key ids so a
// reader can refuse substitution; which algorithms are required comes only
// from the verifier's pinned state.
type PQSignatureSet struct {
	Schema     string        `json:"schema"`
	Signatures []PQSignature `json:"signatures"`
}

type PQSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

// pqMessage is the identical canonical input every required algorithm signs:
// label, profile, pin, ordered suite, binding and payload digest.
func pqMessage(pin PQPin, suite []PQSuiteMember, binding PQBinding, payload [sha256.Size]byte) []byte {
	var buf bytes.Buffer
	lp(&buf, []byte(pqLabel(binding.Kind)))
	lp(&buf, []byte(PQProfile))
	lp(&buf, []byte(pin.digest))
	u64(&buf, uint64(pin.generation))
	u64(&buf, uint64(len(suite)))
	for _, member := range suite {
		lp(&buf, []byte(member.Algorithm))
		lp(&buf, []byte(member.KeyID))
	}
	for _, field := range []string{binding.Kind, binding.Channel, binding.Version, binding.Subject, binding.Source, string(binding.Policy)} {
		lp(&buf, []byte(field))
	}
	u64(&buf, binding.Sequence)
	buf.Write(payload[:])
	return buf.Bytes()
}

// PQSuiteMember names one required algorithm and key in signing order.
type PQSuiteMember struct{ Algorithm, KeyID string }

// PQSigningInput is the exact byte string every member of a suite signs. It
// holds no key and performs no signing: it exists so a future signer, and the
// nonproduction test fixtures, cannot drift from what the verifier checks.
func PQSigningInput(pin PQPin, suite []PQSuiteMember, binding PQBinding, payload []byte) ([]byte, error) {
	if err := binding.validate(); err != nil {
		return nil, err
	}
	if !pin.Valid() || len(suite) == 0 {
		return nil, pqErr(ErrPQScope, "unpinned or empty suite")
	}
	return pqMessage(pin, suite, binding, sha256Of(payload)), nil
}

// PQKeyID is the algorithm-bearing identity of a canonical SPKI PEM key.
func PQKeyID(algorithm string, publicKeyPEM []byte) (string, error) {
	key, err := parsePQKey(algorithm, publicKeyPEM)
	return key.id, err
}

func parsePQSet(raw []byte) (map[string]PQSignature, error) {
	if len(raw) == 0 {
		return nil, pqErr(ErrPQMissing, "no signature set")
	}
	if len(raw) > pqSetMaxBytes {
		return nil, pqErr(ErrPQBound, "signature set")
	}
	var set PQSignatureSet
	if err := definitions.DecodeStrict(raw, &set); err != nil {
		return nil, pqErr(ErrPQMalformed, "signature set")
	}
	if set.Schema != PQSignatureSetSchema {
		return nil, pqErr(ErrPQUnsupported, "signature set schema")
	}
	if len(set.Signatures) == 0 || len(set.Signatures) > pqMaxSignatures {
		return nil, pqErr(ErrPQBound, "signature count")
	}
	byAlgorithm := make(map[string]PQSignature, len(set.Signatures))
	for _, signature := range set.Signatures {
		if signature.Algorithm != AlgorithmECDSAP256 && signature.Algorithm != AlgorithmMLDSA65 {
			return nil, pqErr(ErrPQUnsupported, "signature algorithm")
		}
		if _, dup := byAlgorithm[signature.Algorithm]; dup {
			return nil, pqErr(ErrPQMalformed, "duplicate algorithm")
		}
		byAlgorithm[signature.Algorithm] = signature
	}
	return byAlgorithm, nil
}

// verifyPQSet requires exactly one valid signature per member of the
// verifier-held required set, all over the same suite-bound bytes.
func verifyPQSet(pin PQPin, required []pqRequired, binding PQBinding, payload [sha256.Size]byte, setRaw []byte) error {
	if err := binding.validate(); err != nil {
		return err
	}
	if len(required) == 0 {
		return pqErr(ErrPQScope, "no required signature set")
	}
	presented, err := parsePQSet(setRaw)
	if err != nil {
		return err
	}
	for algorithm := range presented {
		if !slices.ContainsFunc(required, func(member pqRequired) bool { return member.algorithm == algorithm }) {
			return pqErr(ErrPQScope, "signature algorithm outside the required set")
		}
	}
	suite := make([]PQSuiteMember, len(required))
	for i, member := range required {
		suite[i] = PQSuiteMember{Algorithm: member.algorithm, KeyID: member.key.id}
	}
	message := pqMessage(pin, suite, binding, payload)
	for _, member := range required {
		signature, ok := presented[member.algorithm]
		if !ok {
			return pqErr(ErrPQMissing, member.algorithm)
		}
		if signature.KeyID != member.key.id {
			return pqErr(ErrPQScope, "substituted key id")
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(signature.Signature)
		if err != nil || len(decoded) == 0 {
			return pqErr(ErrPQMalformed, "signature encoding")
		}
		switch member.algorithm {
		case AlgorithmMLDSA65:
			if len(decoded) != mldsa65SignatureSize {
				return pqErr(ErrPQBound, "ML-DSA-65 signature size")
			}
			if mldsa.Verify(member.key.mldsa, message, decoded, &mldsa.Options{Context: pqLabel(binding.Kind)}) != nil {
				return pqErr(ErrPQSignature, member.algorithm)
			}
		case AlgorithmECDSAP256:
			if len(decoded) > 80 {
				return pqErr(ErrPQBound, "ECDSA signature size")
			}
			digest := sha256.Sum256(message)
			if !ecdsa.VerifyASN1(member.key.ecdsa, digest[:], decoded) {
				return pqErr(ErrPQSignature, member.algorithm)
			}
		}
	}
	return nil
}

func sha256Of(raw []byte) [sha256.Size]byte { return sha256.Sum256(raw) }

// hashReader streams at most MaxArtifactBytes and returns the digest.
func hashReader(reader io.Reader) ([sha256.Size]byte, error) {
	var out [sha256.Size]byte
	if reader == nil {
		return out, pqErr(ErrPQMalformed, "missing artifact reader")
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(reader, MaxArtifactBytes+1))
	if err != nil {
		return out, pqErr(ErrPQMalformed, "artifact read")
	}
	if count > MaxArtifactBytes {
		return out, pqErr(ErrPQBound, "artifact bytes")
	}
	copy(out[:], hash.Sum(nil))
	return out, nil
}
