package configrollout

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"slices"
	"strings"
)

// Rollout authority trust state (pqc-auth/1, DEC-02, DEC-03, DEC-07, DEC-20).
//
// An enrollment without an Authority object is the compatibility profile: one
// Ed25519 key and the v1 command bytes, unchanged. An enrollment with one is the
// strict profile: the verifier derives the required signature set from this
// operator-installed record, never from the presented command, and every
// required algorithm must verify over identical suite-bound bytes. There is no
// in-band transition: the record arrives only by operator re-enrollment (DEC-20
// option c), so the anchor travels over classical cluster access (E-14).
const (
	AlgEd25519 = "ed25519"
	AlgMLDSA65 = "mldsa65"

	authorityVersion = 1
	// commandLabelV2 is the first length-prefixed field of every v2 signature
	// input and the ML-DSA Options.Context (at most 255 bytes).
	commandLabelV2 = "hikyo/config-rollout/command/v2"
	keyIDLabel     = "hikyo/config-rollout/key-id/v1"
	// trustKey lives in the journal Secret next to journalKey, so one Update
	// writes the generation floor and the accepted command atomically, and a
	// controller that predates it ignores the extra key.
	trustKey = "trust.json"
)

var (
	// ErrTrust is the closed refusal class for trust state: a stale or
	// substituted enrollment, keys that do not match their pins, or a journal
	// entry that cannot be anchored to the installed keys.
	ErrTrust = errors.New("config rollout: authority trust state refused")

	requiredDual   = []string{AlgEd25519, AlgMLDSA65}
	requiredPQOnly = []string{AlgMLDSA65}
)

// Authority is the verifier's pinned trust state. Generation only moves forward;
// KeyIDs are in Required order and pin the exact public keys.
type Authority struct {
	Version    int      `json:"version"`
	Generation uint64   `json:"generation"`
	Required   []string `json:"required"`
	KeyIDs     []string `json:"key_ids"`
}

// CommandAuthority is signed inside the command: the trust state the signer
// expects and the target it addresses, so a command cannot be replayed to
// another installation that shares a key, or verified under other key ids.
type CommandAuthority struct {
	Authority
	TargetDigest string `json:"target_digest"`
}

// CommandSignature carries one algorithm's signature over the shared bytes.
type CommandSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Signature []byte `json:"signature"`
}

// PublicKeys is the key material a verifier holds, one key per algorithm.
type PublicKeys struct {
	Ed25519 ed25519.PublicKey
	MLDSA65 *mldsa.PublicKey
}

// Signers is the signing custody the coordinator holds, one key per algorithm.
type Signers struct {
	Ed25519 crypto.Signer
	MLDSA65 crypto.Signer
}

func algorithmSize(algorithm string) int {
	switch algorithm {
	case AlgEd25519:
		return ed25519.SignatureSize
	case AlgMLDSA65:
		return mldsa.MLDSA65().SignatureSize()
	}
	return 0
}

func appendLP(dst, value []byte) []byte {
	dst = binary.BigEndian.AppendUint64(dst, uint64(len(value)))
	return append(dst, value...)
}

// KeyID is the algorithm-bearing identity of a public key. The label, algorithm
// and canonical key bytes are length-prefixed, so no field can borrow another's
// bytes and an Ed25519 key can never share an id with an ML-DSA key.
func KeyID(algorithm string, public []byte) string {
	input := appendLP(nil, []byte(keyIDLabel))
	input = appendLP(input, []byte(algorithm))
	input = appendLP(input, public)
	sum := sha256.Sum256(input)
	return algorithm + "-sha256:" + hex.EncodeToString(sum[:])
}

func validKeyID(algorithm, id string) bool {
	hexDigest, ok := strings.CutPrefix(id, algorithm+"-sha256:")
	if !ok || len(hexDigest) != 64 {
		return false
	}
	raw, err := hex.DecodeString(hexDigest)
	return err == nil && len(raw) == sha256.Size && hex.EncodeToString(raw) == hexDigest
}

// KeyIDs returns the ids of the keys held, keyed by algorithm.
func (k PublicKeys) KeyIDs() map[string]string {
	ids := map[string]string{}
	if len(k.Ed25519) == ed25519.PublicKeySize {
		ids[AlgEd25519] = KeyID(AlgEd25519, k.Ed25519)
	}
	if k.MLDSA65 != nil && k.MLDSA65.Parameters() == mldsa.MLDSA65() {
		ids[AlgMLDSA65] = KeyID(AlgMLDSA65, k.MLDSA65.Bytes())
	}
	return ids
}

func validAuthority(a Authority) bool {
	if a.Version != authorityVersion || a.Generation == 0 || len(a.KeyIDs) != len(a.Required) {
		return false
	}
	if !slices.Equal(a.Required, requiredDual) && !slices.Equal(a.Required, requiredPQOnly) {
		return false
	}
	for i, algorithm := range a.Required {
		if !validKeyID(algorithm, a.KeyIDs[i]) {
			return false
		}
	}
	return true
}

func cloneAuthority(a Authority) Authority {
	a.Required = slices.Clone(a.Required)
	a.KeyIDs = slices.Clone(a.KeyIDs)
	return a
}

func sameAuthority(a, b Authority) bool {
	return a.Version == b.Version && a.Generation == b.Generation && slices.Equal(a.Required, b.Required) && slices.Equal(a.KeyIDs, b.KeyIDs)
}

// CheckKeys proves the held keys are exactly the enrolled ones: the compatibility
// profile holds one Ed25519 key; the strict profile holds one key per required
// algorithm with the pinned id and nothing else. A strict verifier never keeps
// an extra key, so an unpinned algorithm cannot become acceptable.
func CheckKeys(e Enrollment, keys PublicKeys) error {
	held := keys.KeyIDs()
	if e.Authority == nil {
		if len(held) != 1 || held[AlgEd25519] == "" {
			return ErrInvalid
		}
		return nil
	}
	if (keys.Ed25519 != nil && len(keys.Ed25519) != ed25519.PublicKeySize) || len(held) != len(e.Authority.Required) {
		return ErrTrust
	}
	for i, algorithm := range e.Authority.Required {
		if held[algorithm] != e.Authority.KeyIDs[i] {
			return ErrTrust
		}
	}
	return nil
}

func expectedBinding(e Enrollment) *CommandAuthority {
	if e.Authority == nil {
		return nil
	}
	return &CommandAuthority{Authority: cloneAuthority(*e.Authority), TargetDigest: digest(e.Target)}
}

func sameBinding(a, b *CommandAuthority) bool {
	if a == nil || b == nil {
		return a == b
	}
	return sameAuthority(a.Authority, b.Authority) && a.TargetDigest == b.TargetDigest
}

// commandInputV2 is the exact signed byte string: the label, then the command
// (which carries the binding with generation, required set, key ids and target).
func commandInputV2(command Command) ([]byte, bool) {
	raw, err := json.Marshal(command)
	if err != nil || len(raw) > maxRecordBytes {
		return nil, false
	}
	return appendLP(appendLP(nil, []byte(commandLabelV2)), raw), true
}

func commandInputV1(command Command) ([]byte, bool) {
	raw, err := json.Marshal(command)
	if err != nil || len(raw) > maxRecordBytes {
		return nil, false
	}
	return append([]byte(commandDomain), raw...), true
}

// validEnvelope is the structural check made before any verification and by the
// producer's mailbox: it needs no key. Compatibility carries exactly the v1
// field; strict carries exactly one signature per required algorithm, in order,
// with the pinned key id and the exact signature length.
func validEnvelope(signed SignedCommand, e Enrollment) bool {
	if e.Authority == nil {
		return signed.Command.Authority == nil && len(signed.Signatures) == 0 && len(signed.Signature) == ed25519.SignatureSize
	}
	return envelopeMatches(signed, e.Authority.Required, e.Authority.KeyIDs, len(signed.Signature) == 0)
}

func envelopeMatches(signed SignedCommand, required, ids []string, noLegacy bool) bool {
	if !noLegacy || len(signed.Signatures) != len(required) {
		return false
	}
	for i, signature := range signed.Signatures {
		if signature.Algorithm != required[i] || signature.KeyID != ids[i] || len(signature.Signature) != algorithmSize(signature.Algorithm) {
			return false
		}
	}
	return true
}

func verifyAlgorithm(algorithm string, keys PublicKeys, input, signature []byte) bool {
	switch algorithm {
	case AlgEd25519:
		return len(keys.Ed25519) == ed25519.PublicKeySize && ed25519.Verify(keys.Ed25519, input, signature)
	case AlgMLDSA65:
		return keys.MLDSA65 != nil && mldsa.Verify(keys.MLDSA65, input, signature, &mldsa.Options{Context: commandLabelV2}) == nil
	}
	return false
}

// verifyCommand accepts a command only under the verifier's own trust state.
func verifyCommand(signed SignedCommand, e Enrollment, keys PublicKeys) bool {
	if !validCommand(signed.Command, e) || !validEnvelope(signed, e) {
		return false
	}
	if e.Authority == nil {
		input, ok := commandInputV1(signed.Command)
		return ok && len(keys.Ed25519) == ed25519.PublicKeySize && ed25519.Verify(keys.Ed25519, input, signed.Signature)
	}
	input, ok := commandInputV2(signed.Command)
	if !ok {
		return false
	}
	for _, signature := range signed.Signatures {
		if !verifyAlgorithm(signature.Algorithm, keys, input, signature.Signature) {
			return false
		}
	}
	return true
}

// anchored authenticates a journal entry accepted earlier, possibly under an
// older generation. It anchors the retained plan and grants no authority to
// execute anything new. A pre-strict v1 entry needs the Ed25519 key to be
// required. A v2 entry must carry a verifying ML-DSA signature under the current
// ML-DSA key; its Ed25519 signature is checked whenever that key is unchanged
// and skipped when the classical key was rotated, because the PQ signature
// already anchors it. A replaced ML-DSA key or enrollment id cannot be anchored
// here: there is no in-band re-anchor (DEC-20, registered gap).
func anchored(signed SignedCommand, e Enrollment, keys PublicKeys) bool {
	if !validCommandShape(signed.Command, e) {
		return false
	}
	binding := signed.Command.Authority
	if e.Authority == nil {
		return binding == nil && validEnvelope(signed, e) && verifyCommand(signed, e, keys)
	}
	if binding == nil {
		if !slices.Contains(e.Authority.Required, AlgEd25519) || len(signed.Signatures) != 0 || len(signed.Signature) != ed25519.SignatureSize {
			return false
		}
		input, ok := commandInputV1(signed.Command)
		return ok && ed25519.Verify(keys.Ed25519, input, signed.Signature)
	}
	// The target digest is deliberately not compared: a retained plan is bound to
	// the target by its own check, and an enrollment edit that adds a source alias
	// must not make an authentic older entry unanchorable.
	if !validAuthority(binding.Authority) || binding.Generation > e.Authority.Generation || !envelopeMatches(signed, binding.Required, binding.KeyIDs, len(signed.Signature) == 0) {
		return false
	}
	input, ok := commandInputV2(signed.Command)
	if !ok {
		return false
	}
	current := keys.KeyIDs()
	pq := false
	for _, signature := range signed.Signatures {
		if signature.KeyID != current[signature.Algorithm] {
			if signature.Algorithm == AlgMLDSA65 {
				return false
			}
			continue
		}
		if !verifyAlgorithm(signature.Algorithm, keys, input, signature.Signature) {
			return false
		}
		pq = pq || signature.Algorithm == AlgMLDSA65
	}
	return pq
}

// SignCommandFor is the producer entry point. Under the compatibility profile it
// is exactly SignCommand over the Ed25519 key. Under the strict profile it binds
// the trust state and target into the command and signs the same bytes with every
// required algorithm. It refuses signers whose keys are not the pinned ones and
// signers for any algorithm the profile does not require.
func SignCommandFor(ctx context.Context, e Enrollment, signers Signers, command Command) (SignedCommand, error) {
	if err := ctx.Err(); err != nil {
		return SignedCommand{}, err
	}
	if e.Authority == nil {
		if signers.MLDSA65 != nil || signers.Ed25519 == nil || command.Authority != nil {
			return SignedCommand{}, ErrInvalid
		}
		return SignCommand(ctx, signers.Ed25519, command)
	}
	if err := CheckKeys(e, signers.Public()); err != nil {
		return SignedCommand{}, ErrInvalid
	}
	command.Authority = expectedBinding(e)
	input, ok := commandInputV2(command)
	if !ok {
		return SignedCommand{}, ErrInvalid
	}
	signed := SignedCommand{Command: command}
	for i, algorithm := range e.Authority.Required {
		var signer crypto.Signer
		var options crypto.SignerOpts = crypto.Hash(0)
		switch algorithm {
		case AlgEd25519:
			signer = signers.Ed25519
		case AlgMLDSA65:
			signer, options = signers.MLDSA65, &mldsa.Options{Context: commandLabelV2}
		}
		signature, err := signer.Sign(rand.Reader, input, options)
		if err != nil {
			return SignedCommand{}, ErrUnavailable
		}
		signed.Signatures = append(signed.Signatures, CommandSignature{Algorithm: algorithm, KeyID: e.Authority.KeyIDs[i], Signature: signature})
	}
	return signed, nil
}

// Public derives the verification keys from the signing custody.
func (s Signers) Public() PublicKeys {
	var keys PublicKeys
	if s.Ed25519 != nil {
		keys.Ed25519, _ = s.Ed25519.Public().(ed25519.PublicKey)
	}
	if s.MLDSA65 != nil {
		keys.MLDSA65, _ = s.MLDSA65.Public().(*mldsa.PublicKey)
	}
	return keys
}

// pemBlocks decodes consecutive blocks of one type and refuses anything else:
// preamble, headers, a different type or trailing bytes.
func pemBlocks(raw []byte, blockType string) ([]*pem.Block, bool) {
	var blocks []*pem.Block
	rest := bytes.TrimSpace(raw)
	for len(rest) > 0 {
		if !bytes.HasPrefix(rest, []byte("-----BEGIN "+blockType+"-----")) {
			return nil, false
		}
		block, next := pem.Decode(rest)
		if block == nil || block.Type != blockType || len(block.Headers) != 0 {
			return nil, false
		}
		blocks = append(blocks, block)
		rest = bytes.TrimSpace(next)
	}
	return blocks, len(blocks) > 0 && len(blocks) <= 2
}

// ParsePublicKeys reads the installed authority public key file: one or two
// PUBLIC KEY blocks, at most one Ed25519 and one ML-DSA-65 key. ML-DSA-44 and
// ML-DSA-87 (DEC-02), every other key type and duplicates are refused.
func ParsePublicKeys(raw []byte) (PublicKeys, error) {
	blocks, ok := pemBlocks(raw, "PUBLIC KEY")
	if !ok {
		return PublicKeys{}, ErrInvalid
	}
	var keys PublicKeys
	for _, block := range blocks {
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return PublicKeys{}, ErrInvalid
		}
		switch key := parsed.(type) {
		case ed25519.PublicKey:
			if keys.Ed25519 != nil {
				return PublicKeys{}, ErrInvalid
			}
			keys.Ed25519 = key
		case *mldsa.PublicKey:
			if keys.MLDSA65 != nil || key.Parameters() != mldsa.MLDSA65() {
				return PublicKeys{}, ErrInvalid
			}
			keys.MLDSA65 = key
		default:
			return PublicKeys{}, ErrInvalid
		}
	}
	return keys, nil
}

// ParseSigners reads the protected signer file: one or two PRIVATE KEY blocks,
// at most one Ed25519 and one seed-only ML-DSA-65 key (Go refuses the expanded
// PKCS#8 forms). Intermediate key bytes are zeroed.
func ParseSigners(raw []byte) (Signers, error) {
	blocks, ok := pemBlocks(raw, "PRIVATE KEY")
	if !ok {
		return Signers{}, ErrInvalid
	}
	var signers Signers
	for _, block := range blocks {
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		clear(block.Bytes)
		if err != nil {
			return Signers{}, ErrInvalid
		}
		switch key := parsed.(type) {
		case ed25519.PrivateKey:
			if signers.Ed25519 != nil || len(key) != ed25519.PrivateKeySize {
				return Signers{}, ErrInvalid
			}
			signers.Ed25519 = key
		case *mldsa.PrivateKey:
			if signers.MLDSA65 != nil || key.PublicKey().Parameters() != mldsa.MLDSA65() {
				return Signers{}, ErrInvalid
			}
			signers.MLDSA65 = key
		default:
			return Signers{}, ErrInvalid
		}
	}
	return signers, nil
}

// trustRecord is the generation floor persisted in the journal Secret.
type trustRecord struct {
	Authority
}

func trustFor(e Enrollment) *trustRecord {
	if e.Authority == nil {
		return nil
	}
	return &trustRecord{Authority: cloneAuthority(*e.Authority)}
}

// checkFloor refuses a trust state older than or different from the persisted
// floor: rollback to the compatibility profile, a lower generation (a stale
// enrollment restored from backup), or another key set at the same generation.
func checkFloor(recorded *trustRecord, current *trustRecord) error {
	switch {
	case recorded == nil:
		return nil
	case current == nil || current.Generation < recorded.Generation:
		return ErrTrust
	case current.Generation == recorded.Generation && !sameAuthority(current.Authority, recorded.Authority):
		return ErrTrust
	}
	return nil
}

func parseTrustRecord(raw []byte) (*trustRecord, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var record trustRecord
	if decode(raw, &record) != nil || !validAuthority(record.Authority) {
		return nil, ErrTrust
	}
	return &record, nil
}
