package crypto

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Transit (#156, transit ADR, encryption-model declared amendment 2026-09-26):
// policy-bound cryptography as a service over managed keys the caller never
// holds. This file is the whole cryptographic half of transit. The custody
// provider (internal/transit) decides where a version's material lives; the
// service decides whether an operation is permitted. Neither touches a
// primitive: every seal, open, signature and MAC below is computed here, so the
// chokepoint invariant (no primitive import outside this package) is unchanged.

// KindTransit is the caller-facing transit ciphertext envelope. Its AAD schema
// is TransitAAD. Kind bytes are persisted format: never renumber.
const KindTransit Kind = 8

// TransitAlgorithm is the closed algorithm set of transit ADR D2. The algorithm
// fixes the key's purpose for its whole life.
type TransitAlgorithm string

const (
	TransitXChaCha20Poly1305 TransitAlgorithm = "xchacha20-poly1305"
	TransitEd25519           TransitAlgorithm = "ed25519"
	TransitHMACSHA256        TransitAlgorithm = "hmac-sha256"
	// TransitMLDSA65 is pure ML-DSA-65 (FIPS 204) with the context
	// TransitMLDSASignContext: a key identity distinct from ed25519
	// (post-quantum migration DEC-02, transit ADR amendment). No other
	// parameter set is in the closed set.
	TransitMLDSA65 TransitAlgorithm = "mldsa65"
)

// TransitAlgorithms returns the closed algorithm set in a stable order.
func TransitAlgorithms() []TransitAlgorithm {
	return []TransitAlgorithm{TransitXChaCha20Poly1305, TransitEd25519, TransitHMACSHA256, TransitMLDSA65}
}

// TransitMLDSASignContext is the ML-DSA Options.Context of every transit
// signature: the Transit label (DEC-07, `hikyo/<surface>/<purpose>/v<N>`), at
// most 255 bytes. An independent verifier needs exactly this string.
const TransitMLDSASignContext = "hikyo/transit/sign/v1"

// transitSigSchemeMLDSA65 is the two-byte big-endian algorithm identifier that
// opens every ML-DSA-65 signature payload, so a signature carries its algorithm
// and parameter set (DEC-07). It is the TLS SignatureScheme code point for
// ML-DSA-65 listed in the profiles. Ed25519 payloads stay the bare 64 bytes.
const transitSigSchemeMLDSA65 uint16 = 0x0905

// Fixed encoded sizes of the FIPS 204 ML-DSA-65 public key and signature, and
// the signature payload (identifier plus signature).
const (
	transitMLDSA65PublicKeySize = 1952
	transitMLDSA65SignatureSize = 3309
	transitMLDSA65PayloadSize   = 2 + transitMLDSA65SignatureSize
)

// ParseTransitAlgorithm refuses anything outside the closed set.
func ParseTransitAlgorithm(s string) (TransitAlgorithm, error) {
	for _, a := range TransitAlgorithms() {
		if string(a) == s {
			return a, nil
		}
	}
	return "", fmt.Errorf("crypto: unknown transit algorithm %q", s)
}

// TransitMaterialSize is the size of one key version's material: 256 bits of
// fresh randomness, from which every operation key is derived.
const TransitMaterialSize = KeySize

// transitKeyInfoLabel domain-separates transit operation keys from every other
// HKDF use in the module. New labels use the `hikyo/` prefix.
const transitKeyInfoLabel = "hikyo/transit-key/v1"

// transitWirePrefix opens every caller-facing transit value. The decimal key
// version follows it, then a colon, then base64url bytes.
const transitWirePrefix = "hikyo:v"

// Bounds shared by the service's request validation and the parsers below, so
// a parser can never accept what the service would refuse (transit ADR D9).
const (
	// MaxTransitPlaintextBytes bounds plaintext, signed or MACed messages, and
	// decrypted output.
	MaxTransitPlaintextBytes = 64 << 10
	// MaxTransitContextBytes bounds caller-supplied associated data.
	MaxTransitContextBytes = 1 << 10
	// MaxTransitWireBytes bounds a presented ciphertext, signature or MAC
	// string before any decoding happens.
	MaxTransitWireBytes = 128 << 10
)

// ErrTransitFormat is the uniform refusal for a malformed transit value. It
// names no byte and no field.
var ErrTransitFormat = errors.New("crypto: malformed transit value")

// TransitAAD binds a transit ciphertext to one key in one environment and to
// the caller's context: fields org_id, project_id, env_id, transit_key_id,
// context. The key version is already in the authenticated header.
type TransitAAD struct {
	OrgID, ProjectID, EnvID, KeyID string
	Context                        []byte
}

func (a TransitAAD) kind() Kind { return KindTransit }

// fields returns the tenant, key, and context fields in their authenticated order.
func (a TransitAAD) fields() [][]byte {
	return [][]byte{
		[]byte(a.OrgID), []byte(a.ProjectID), []byte(a.EnvID),
		[]byte(a.KeyID), a.Context,
	}
}

// TransitBinding names exactly one key version. Every derived key is bound to
// all of it, so material copied onto another key, version or environment
// derives a different operation key.
type TransitBinding struct {
	Algorithm                      TransitAlgorithm
	OrgID, ProjectID, EnvID, KeyID string
	Version                        uint32
}

// validate rejects unknown algorithms, empty scope or key identifiers, and
// version zero.
func (b TransitBinding) validate() error {
	if _, err := ParseTransitAlgorithm(string(b.Algorithm)); err != nil {
		return err
	}
	if b.OrgID == "" || b.ProjectID == "" || b.EnvID == "" || b.KeyID == "" || b.Version == 0 {
		return errors.New("crypto: transit binding requires org, project, environment, key and version")
	}
	return nil
}

// aad binds caller context to the tenant and key identified by b.
func (b TransitBinding) aad(context []byte) TransitAAD {
	return TransitAAD{OrgID: b.OrgID, ProjectID: b.ProjectID, EnvID: b.EnvID, KeyID: b.KeyID, Context: context}
}

// NewTransitMaterial draws one key version's material. A failed or short read
// returns a wrapped randomness error and no material.
func NewTransitMaterial() ([]byte, error) {
	return readRandom(rand.Reader, TransitMaterialSize)
}

// readRandom reads exactly n bytes or returns a wrapped read error and no material.
func readRandom(rnd io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rnd, b); err != nil {
		return nil, fmt.Errorf("crypto: randomness unavailable, refusing to generate transit material: %w", err)
	}
	return b, nil
}

// TransitDataKeySizes are the permitted data-key lengths in bytes (128, 256
// and 512 bits).
var transitDataKeySizes = map[int]bool{16: true, 32: true, 64: true}

// NewTransitDataKey draws a data key of bits length. Anything but 128, 256 or
// 512 is refused.
func NewTransitDataKey(bits int) ([]byte, error) {
	if bits%8 != 0 || !transitDataKeySizes[bits/8] {
		return nil, fmt.Errorf("crypto: data key size must be 128, 256 or 512 bits")
	}
	return readRandom(rand.Reader, bits/8)
}

// TransitKey is one version's derived operation key, held only for the span of
// one operation. It carries no exported field and redacts itself in every
// formatting surface. Destroy zeroes it.
type TransitKey struct {
	redactor
	b    TransitBinding
	key  []byte             // AEAD or HMAC key
	priv ed25519.PrivateKey // ed25519 only
	pq   *mldsa.PrivateKey  // mldsa65 only
	rnd  io.Reader
}

// OpenTransitKey derives the operation key for one version from its material.
// The material is not retained; the caller zeroes it. Invalid bindings, material
// of the wrong size, and key derivation failures return errors.
func OpenTransitKey(material []byte, b TransitBinding) (*TransitKey, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}
	if len(material) != TransitMaterialSize {
		return nil, errors.New("crypto: transit material has the wrong size")
	}
	info := appendLP(nil, []byte(transitKeyInfoLabel))
	info = appendLP(info, []byte(b.Algorithm))
	info = appendLP(info, []byte(b.OrgID))
	info = appendLP(info, []byte(b.ProjectID))
	info = appendLP(info, []byte(b.EnvID))
	info = appendLP(info, []byte(b.KeyID))
	info = appendLP(info, be32(b.Version))
	derived, err := hkdf.Key(sha256.New, material, nil, string(info), KeySize)
	if err != nil {
		return nil, fmt.Errorf("crypto: derive transit key: %w", err)
	}
	k := &TransitKey{b: b, rnd: rand.Reader}
	switch b.Algorithm {
	case TransitEd25519:
		k.priv = ed25519.NewKeyFromSeed(derived)
		Zero(derived)
		return k, nil
	case TransitMLDSA65:
		// The derived 32 bytes are the FIPS 204 seed, as they are the RFC 8032
		// seed above. A library that cannot serve ML-DSA (the frozen FIPS
		// module) refuses; no other algorithm is substituted.
		pq, err := mldsa.NewPrivateKey(mldsa.MLDSA65(), derived)
		Zero(derived)
		if err != nil {
			return nil, fmt.Errorf("crypto: open ml-dsa transit key: %w", err)
		}
		k.pq = pq
		return k, nil
	}
	k.key = derived
	return k, nil
}

// Destroy zeroes the derived key. Best effort, like every Zero.
func (k *TransitKey) Destroy() {
	if k == nil {
		return
	}
	Zero(k.key)
	Zero(k.priv)
	// shortcut: crypto/mldsa exposes no way to zero a private key, so the seed
	// copy inside the library lives until the garbage collector reclaims it;
	// revisit if the standard library adds a destroy method.
	k.pq = nil
}

// require rejects a nil key or a key with a different algorithm.
func (k *TransitKey) require(a TransitAlgorithm) error {
	if k == nil || k.b.Algorithm != a {
		return fmt.Errorf("crypto: transit operation requires a %s key", a)
	}
	return nil
}

// PublicKey returns a copy of the signing public key: 32 bytes for Ed25519, the
// 1952-byte FIPS 204 encoding for ML-DSA-65. It is public metadata: storing and
// returning it discloses nothing about the private half. A nil key or a key
// that is not a signing key returns an error.
func (k *TransitKey) PublicKey() ([]byte, error) {
	if k != nil && k.pq != nil {
		return k.pq.PublicKey().Bytes(), nil
	}
	if err := k.require(TransitEd25519); err != nil {
		return nil, err
	}
	return append([]byte(nil), k.priv.Public().(ed25519.PublicKey)...), nil
}

// Encrypt seals plaintext as a transit envelope record under this version,
// binding context as associated data. A nil or non-XChaCha20-Poly1305 key is an
// error. Plaintext over MaxTransitPlaintextBytes or context over
// MaxTransitContextBytes returns ErrTransitFormat; sealing errors propagate.
func (k *TransitKey) Encrypt(plaintext, context []byte) ([]byte, error) {
	if err := k.require(TransitXChaCha20Poly1305); err != nil {
		return nil, err
	}
	if len(plaintext) > MaxTransitPlaintextBytes || len(context) > MaxTransitContextBytes {
		return nil, ErrTransitFormat
	}
	return seal(k.rnd, k.key, []byte(k.b.KeyID), k.b.Version, k.b.aad(context), plaintext)
}

// Decrypt opens a transit envelope record. The header must name this key and
// this version, and the AAD must match this binding and context. A nil or
// non-XChaCha20-Poly1305 key returns an algorithm error. Oversized context or
// plaintext and all record parsing or authentication failures return ErrDecrypt.
func (k *TransitKey) Decrypt(record, context []byte) ([]byte, error) {
	if err := k.require(TransitXChaCha20Poly1305); err != nil {
		return nil, err
	}
	if len(context) > MaxTransitContextBytes {
		return nil, ErrDecrypt
	}
	pt, err := open(k.key, []byte(k.b.KeyID), k.b.Version, k.b.aad(context), record)
	if err != nil {
		return nil, ErrDecrypt
	}
	if len(pt) > MaxTransitPlaintextBytes {
		Zero(pt)
		return nil, ErrDecrypt
	}
	return pt, nil
}

// Sign returns the signature payload for message. Ed25519 is pure RFC 8032 over
// the message bytes. ML-DSA-65 is pure FIPS 204 (hedged, no pre-hash) with the
// context TransitMLDSASignContext, behind its two-byte algorithm identifier;
// either can be checked by an independent verifier holding the public key. A
// nil or non-signing key returns an error; messages over
// MaxTransitPlaintextBytes return ErrTransitFormat.
func (k *TransitKey) Sign(message []byte) ([]byte, error) {
	if k != nil && k.pq != nil {
		if len(message) > MaxTransitPlaintextBytes {
			return nil, ErrTransitFormat
		}
		sig, err := k.pq.Sign(nil, message, &mldsa.Options{Context: TransitMLDSASignContext})
		if err != nil || len(sig) != transitMLDSA65SignatureSize {
			return nil, errors.New("crypto: ml-dsa signing failed")
		}
		out := binary.BigEndian.AppendUint16(make([]byte, 0, transitMLDSA65PayloadSize), transitSigSchemeMLDSA65)
		return append(out, sig...), nil
	}
	if err := k.require(TransitEd25519); err != nil {
		return nil, err
	}
	if len(message) > MaxTransitPlaintextBytes {
		return nil, ErrTransitFormat
	}
	return ed25519.Sign(k.priv, message), nil
}

// MAC is HMAC-SHA256 under the derived key over the message bytes. A nil or
// non-HMAC key returns an error; messages over MaxTransitPlaintextBytes return
// ErrTransitFormat.
func (k *TransitKey) MAC(message []byte) ([]byte, error) {
	if err := k.require(TransitHMACSHA256); err != nil {
		return nil, err
	}
	if len(message) > MaxTransitPlaintextBytes {
		return nil, ErrTransitFormat
	}
	m := hmac.New(sha256.New, k.key)
	m.Write(message)
	return m.Sum(nil), nil
}

// VerifyTransitSignature checks a signature payload under the algorithm of the
// key that is being verified against. The caller passes the algorithm recorded
// on the key row, never one read from the presented signature, so a payload
// cannot select its own verifier (DEC-03). The identifier on an ML-DSA payload
// must equal the key's own. Wrong algorithms, wrong-sized keys or payloads, a
// public key the library refuses, and oversized messages are a plain false,
// never a panic.
func VerifyTransitSignature(alg TransitAlgorithm, publicKey, message, payload []byte) bool {
	if len(message) > MaxTransitPlaintextBytes {
		return false
	}
	switch alg {
	case TransitEd25519:
		if len(publicKey) != ed25519.PublicKeySize || len(payload) != ed25519.SignatureSize {
			return false
		}
		return ed25519.Verify(ed25519.PublicKey(publicKey), message, payload)
	case TransitMLDSA65:
		if len(publicKey) != transitMLDSA65PublicKeySize || len(payload) != transitMLDSA65PayloadSize ||
			binary.BigEndian.Uint16(payload) != transitSigSchemeMLDSA65 {
			return false
		}
		pk, err := mldsa.NewPublicKey(mldsa.MLDSA65(), publicKey)
		if err != nil {
			return false
		}
		return mldsa.Verify(pk, message, payload[2:], &mldsa.Options{Context: TransitMLDSASignContext}) == nil
	}
	return false
}

// EqualTransitMAC compares two MACs in constant time.
func EqualTransitMAC(a, b []byte) bool {
	return hmac.Equal(a, b)
}

// FormatTransitValue renders a caller-facing transit value:
// "hikyo:v" ‖ decimal(version) ‖ ":" ‖ base64url(payload), unpadded.
func FormatTransitValue(version uint32, payload []byte) string {
	return transitWirePrefix + strconv.FormatUint(uint64(version), 10) + ":" +
		base64.RawURLEncoding.EncodeToString(payload)
}

// ParseTransitValue splits a caller-facing transit value into its version and
// decoded payload. The version is a canonical positive decimal (no sign, no
// leading zero, fits uint32) and the payload strict unpadded base64url. The
// whole string is limited to MaxTransitWireBytes before decoding. Empty
// payloads and all malformed or oversized values return ErrTransitFormat.
func ParseTransitValue(s string) (uint32, []byte, error) {
	if len(s) > MaxTransitWireBytes || !strings.HasPrefix(s, transitWirePrefix) {
		return 0, nil, ErrTransitFormat
	}
	rest := s[len(transitWirePrefix):]
	colon := strings.IndexByte(rest, ':')
	if colon <= 0 || colon > 10 {
		return 0, nil, ErrTransitFormat
	}
	digits := rest[:colon]
	if digits[0] == '0' {
		return 0, nil, ErrTransitFormat
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, nil, ErrTransitFormat
		}
	}
	v, err := strconv.ParseUint(digits, 10, 32)
	if err != nil || v == 0 {
		return 0, nil, ErrTransitFormat
	}
	encoded := rest[colon+1:]
	// The decoder silently skips CR and LF even in strict mode, which would let
	// two strings name one value; admit the base64url alphabet only.
	for i := 0; i < len(encoded); i++ {
		c := encoded[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return 0, nil, ErrTransitFormat
		}
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(payload) == 0 {
		return 0, nil, ErrTransitFormat
	}
	return uint32(v), payload, nil
}

// ParseTransitCiphertext returns the version and decoded record after checking
// the transit header, algorithm, key ID, and agreement with the textual version.
// Parse failures and mismatches return ErrTransitFormat. It does not validate
// nonce length or authenticate the record; the caller must still decrypt it.
func ParseTransitCiphertext(s, keyID string) (uint32, []byte, error) {
	version, record, err := ParseTransitValue(s)
	if err != nil {
		return 0, nil, err
	}
	h, _, err := parseHeader(record)
	if err != nil {
		return 0, nil, ErrTransitFormat
	}
	if h.kind != KindTransit || h.alg != algXChaCha20Poly1305 || h.keyVersion != version ||
		string(h.keyID) != keyID {
		return 0, nil, ErrTransitFormat
	}
	return version, record, nil
}
