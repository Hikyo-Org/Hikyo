package sealedhook

// Protocol version 2 (docs/spec/sealed-webhook.md section 11, proposed): the
// payload is age-encrypted to a hybrid X25519 + ML-KEM-768 recipient and every
// envelope and acknowledgement carries two required signatures, Ed25519 and
// ML-DSA-65, over identical suite-bound bytes. Version 1 code paths are not
// shared with it except for the injective encoding and the scope validators,
// so version 1 bytes cannot be reinterpreted as version 2 or the reverse.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

const (
	// VersionV2 is the protocol version this file speaks.
	VersionV2 = 2
	// SuiteV2 is the only version 2 signature suite. Both signatures are
	// required; there is no single-algorithm version 2 message.
	SuiteV2 = "ed25519+mldsa65"
	// SyncPathV2, ContentTypeV2 and AckContentTypeV2 separate version 2 from
	// version 1 on the wire.
	SyncPathV2       = "/hikyo/v2/sync"
	ContentTypeV2    = "application/vnd.hikyo.sealed-envelope.v2+json"
	AckContentTypeV2 = "application/vnd.hikyo.sealed-ack.v2+json"

	// MLDSA65PublicKeySize and MLDSA65SignatureSize are the DEC-08 field bounds.
	MLDSA65PublicKeySize = 1952
	MLDSA65SignatureSize = 3309

	// MaxCiphertextBase64 is the encoded form of MaxCiphertext.
	MaxCiphertextBase64 = (MaxCiphertext + 2) / 3 * 4
	// MaxEnvelopeV2 bounds a version 2 request body: the encoded ciphertext
	// plus 128 KiB for 256 names, identifiers and both signatures.
	MaxEnvelopeV2 = MaxCiphertextBase64 + 128<<10

	// MinResponseBytesV2 is the smallest acknowledgement bound a protocol 2
	// target may configure: the largest legal acknowledgement (two signatures
	// of 4,412 and 88 characters, a 64 character reason) is asserted to fit
	// below it. A smaller bound would refuse every real acknowledgement as
	// oversized after the receiver had already applied the value.
	MinResponseBytesV2 = 5120

	// MaxHeaderV2 bounds the age header before any key is tried. One hybrid
	// stanza is about 1.6 KiB.
	MaxHeaderV2 = 4096

	hybridStanzaType         = "mlkem768x25519"
	hybridEncapsulationBytes = 1120
	hybridWrappedKeyBytes    = 32
	ed25519SigBase64         = 88
	mldsa65SigBase64         = (MLDSA65SignatureSize + 2) / 3 * 4

	envelopeDomainV2    = "hikyo/sealed-webhook/envelope/v2"
	ackDomainV2         = "hikyo/sealed-webhook/ack/v2"
	fingerprintDomainV2 = "hikyo/sealed-webhook/fingerprint/v2"
	destinationDomainV2 = "hikyo/sealed-webhook/destination/v2"
	idempotencyDomainV2 = "hikyo/sealed-webhook/idempotency/v2"
	keyIDDomainV2       = "hikyo/sealed-webhook/key-id/v2"
)

// ErrRecipient marks an envelope addressed to another recipient.
var ErrRecipient = errors.New("sealedhook: envelope is addressed to another recipient")

var (
	edKeyIDV2    = regexp.MustCompile(`^ed25519-sha256:[0-9a-f]{64}$`)
	pqKeyIDV2    = regexp.MustCompile(`^mldsa65-sha256:[0-9a-f]{64}$`)
	recipientIDV = regexp.MustCompile(`^mlkem768x25519-sha256:[0-9a-f]{64}$`)
	digestSyntax = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func keyIDV2(algorithm string, raw []byte) string {
	sum := newLP(keyIDDomainV2).str(algorithm).add(raw).sum()
	return algorithm + "-sha256:" + hex.EncodeToString(sum[:])
}

// IDV2 is the version 2 key id of an Ed25519 key.
func (p PublicKey) IDV2() string { return keyIDV2("ed25519", p.key) }

// MLDSAPublicKey is a pinned ML-DSA-65 verification key. Its text form is
// "mldsa65:" followed by 3904 lowercase hex digits.
type MLDSAPublicKey struct{ key *mldsa.PublicKey }

// ParseMLDSAPublicKey parses the canonical text form. The length is checked
// before any decoding or parsing.
func ParseMLDSAPublicKey(raw string) (MLDSAPublicKey, error) {
	body, ok := strings.CutPrefix(strings.TrimSpace(raw), "mldsa65:")
	if !ok || len(body) != 2*MLDSA65PublicKeySize || strings.ToLower(body) != body {
		return MLDSAPublicKey{}, errors.New("sealedhook: public key must be mldsa65:<3904 lowercase hex>")
	}
	enc, err := hex.DecodeString(body)
	if err != nil {
		return MLDSAPublicKey{}, errors.New("sealedhook: public key must be mldsa65:<3904 lowercase hex>")
	}
	key, err := mldsa.NewPublicKey(mldsa.MLDSA65(), enc)
	if err != nil {
		return MLDSAPublicKey{}, errors.New("sealedhook: not a valid ML-DSA-65 public key")
	}
	return MLDSAPublicKey{key: key}, nil
}

func (p MLDSAPublicKey) valid() bool { return p.key != nil }

func (p MLDSAPublicKey) String() string {
	if p.key == nil {
		return ""
	}
	return "mldsa65:" + hex.EncodeToString(p.key.Bytes())
}

// ID is the version 2 key id.
func (p MLDSAPublicKey) ID() string { return keyIDV2("mldsa65", p.key.Bytes()) }

// MLDSASigner holds one ML-DSA-65 signing key.
type MLDSASigner struct {
	key *mldsa.PrivateKey
	pub MLDSAPublicKey
}

// Public returns the verification key.
func (s *MLDSASigner) Public() MLDSAPublicKey { return s.pub }

// ParseMLDSASigningKeyPEM accepts exactly one seed-only PKCS#8 "PRIVATE KEY"
// block holding an ML-DSA-65 key, the only private form Go reads.
func ParseMLDSASigningKeyPEM(raw []byte) (*MLDSASigner, error) {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("sealedhook: ML-DSA signing key must be exactly one PKCS#8 PRIVATE KEY PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("sealedhook: ML-DSA signing key is not valid seed-only PKCS#8")
	}
	key, ok := parsed.(*mldsa.PrivateKey)
	if !ok || key.PublicKey().Parameters() != mldsa.MLDSA65() {
		return nil, errors.New("sealedhook: signing key is not ML-DSA-65")
	}
	return &MLDSASigner{key: key, pub: MLDSAPublicKey{key: key.PublicKey()}}, nil
}

// GenerateMLDSASigningKey returns a fresh ML-DSA-65 key as seed-only PKCS#8
// PEM with its public text.
func GenerateMLDSASigningKey() (privatePEM []byte, public string, err error) {
	key, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		return nil, "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, "", err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), MLDSAPublicKey{key: key.PublicKey()}.String(), nil
}

// SignerV2 is the pair of keys that sign a version 2 message. Both always sign.
type SignerV2 struct {
	ed *Signer
	pq *MLDSASigner
}

// NewSignerV2 pairs the two signing keys.
func NewSignerV2(ed *Signer, pq *MLDSASigner) (*SignerV2, error) {
	if ed == nil || pq == nil {
		return nil, errors.New("sealedhook: version 2 needs both an Ed25519 and an ML-DSA-65 signing key")
	}
	return &SignerV2{ed: ed, pq: pq}, nil
}

func (s *SignerV2) sign(label string, msg []byte) (edSig, pqSig string, err error) {
	pq, err := s.pq.key.Sign(nil, msg, &mldsa.Options{Context: label})
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(s.ed.key, msg)), base64.StdEncoding.EncodeToString(pq), nil
}

// GenerateHybridRecipient returns a fresh age hybrid identity and recipient.
func GenerateHybridRecipient() (identity, recipient string, err error) {
	id, err := age.GenerateHybridIdentity()
	if err != nil {
		return "", "", err
	}
	return id.String(), id.Recipient().String(), nil
}

// CanonicalHybridRecipient parses an age hybrid recipient and returns its
// canonical text, refusing anything else.
func CanonicalHybridRecipient(raw string) (string, error) {
	r, err := age.ParseHybridRecipient(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("sealedhook: recipient must be an age hybrid public key (age1pq1...)")
	}
	return r.String(), nil
}

func recipientIDV2(canonical string) string {
	sum := newLP(keyIDDomainV2).str("age-mlkem768x25519").str(canonical).sum()
	return "mlkem768x25519-sha256:" + hex.EncodeToString(sum[:])
}

// FingerprintV2 is the value an instance admin verifies out of band before
// activating a version 2 target. It covers the suite, both acknowledgement
// keys, the recipient, the origin and the generation.
func FingerprintV2(origin, recipient string, ackEd PublicKey, ackPQ MLDSAPublicKey, generation int64) (string, error) {
	canonical, err := CanonicalHybridRecipient(recipient)
	if err != nil {
		return "", err
	}
	if !ackEd.valid() || !ackPQ.valid() {
		return "", errors.New("sealedhook: both acknowledgement keys are required")
	}
	if generation < 1 {
		return "", errors.New("sealedhook: generation must be positive")
	}
	hash := newLP(fingerprintDomainV2).str(origin).str(canonical).str(ackEd.String()).str(ackPQ.String()).str(SuiteV2).i64(generation).hexSum()
	return "swh2-mlkem768x25519-ed25519-mldsa65-sha256:" + hash, nil
}

// DestinationIDV2 is DestinationID under the version 2 label.
func DestinationIDV2(fingerprint, namespace string) int64 {
	return destinationID(destinationDomainV2, fingerprint, namespace)
}

// IdempotencyKeyV2 is IdempotencyKey under the version 2 label.
func IdempotencyKeyV2(p IdempotencyParts) string { return idempotencyKey(idempotencyDomainV2, p) }

// EnvelopeV2 is the wire form of one version 2 request. Variable-size members
// are strings so their encoded length is checked before decoding.
type EnvelopeV2 struct {
	V                int    `json:"v"`
	Suite            string `json:"suite"`
	KeyIDEd25519     string `json:"key_id_ed25519"`
	KeyIDMLDSA65     string `json:"key_id_mldsa65"`
	RecipientID      string `json:"recipient_id"`
	TargetID         string `json:"target_id"`
	InstanceID       string `json:"instance_id"`
	Namespace        string `json:"namespace"`
	Route            string `json:"route"`
	Generation       int64  `json:"generation"`
	Source           Source `json:"source"`
	Op               Op     `json:"op"`
	Names            []Name `json:"names"`
	Ciphertext       string `json:"ciphertext"`
	CiphertextDigest string `json:"ciphertext_digest"`
	IdempotencyKey   string `json:"idempotency_key"`
	IssuedAt         string `json:"issued_at"`
	ExpiresAt        string `json:"expires_at"`
	SigEd25519       string `json:"sig_ed25519"`
	SigMLDSA65       string `json:"sig_mldsa65"`
}

// scope is the version-neutral view the validators and the receiver share.
func (e *EnvelopeV2) scope() Envelope {
	return Envelope{
		V: VersionV2, TargetID: e.TargetID, InstanceID: e.InstanceID, Namespace: e.Namespace, Route: e.Route,
		Generation: e.Generation, Source: e.Source, Op: e.Op, Names: e.Names, IdempotencyKey: e.IdempotencyKey,
		IssuedAt: e.IssuedAt, ExpiresAt: e.ExpiresAt,
	}
}

func envelopeBytesV2(e *EnvelopeV2) []byte {
	b := newLP(envelopeDomainV2).i64(int64(e.V)).str(e.Suite).str(e.KeyIDEd25519).str(e.KeyIDMLDSA65).str(e.RecipientID).
		str(e.TargetID).str(e.InstanceID).str(e.Namespace).str(e.Route).i64(e.Generation).
		str(e.Source.Org).str(e.Source.Project).str(e.Source.Environment).i64(e.Source.Revision).
		str(string(e.Op)).i64(int64(len(e.Names)))
	for _, n := range e.Names {
		b = b.str(n.Surface).str(n.Name)
	}
	return b.str(e.CiphertextDigest).str(e.IdempotencyKey).str(e.IssuedAt).str(e.ExpiresAt)
}

func validateEnvelopeV2(e *EnvelopeV2) error {
	if e.V != VersionV2 || e.Suite != SuiteV2 {
		return fmt.Errorf("%w: unsupported version or suite", ErrMalformed)
	}
	scope := e.scope()
	if err := validateScope(&scope); err != nil {
		return err
	}
	if len(e.Ciphertext) == 0 || len(e.Ciphertext) > MaxCiphertextBase64 {
		return fmt.Errorf("%w: ciphertext size", ErrMalformed)
	}
	if !edKeyIDV2.MatchString(e.KeyIDEd25519) || !pqKeyIDV2.MatchString(e.KeyIDMLDSA65) || !recipientIDV.MatchString(e.RecipientID) ||
		!digestSyntax.MatchString(e.CiphertextDigest) || !hexKey.MatchString(e.IdempotencyKey) {
		return fmt.Errorf("%w: invalid key fields", ErrMalformed)
	}
	if len(e.SigEd25519) != ed25519SigBase64 || len(e.SigMLDSA65) != mldsa65SigBase64 {
		return fmt.Errorf("%w: missing or wrong-size signature", ErrMalformed)
	}
	return validateWindow(e.IssuedAt, e.ExpiresAt)
}

// ParseEnvelopeV2 decodes and validates an envelope's structure. It does not
// verify a signature; OpenerV2.Open does.
func ParseEnvelopeV2(body []byte) (EnvelopeV2, error) {
	if len(body) > MaxEnvelopeV2 {
		return EnvelopeV2{}, ErrTooLarge
	}
	var e EnvelopeV2
	if err := checkShape(body, envelopeV2); err != nil {
		return EnvelopeV2{}, err
	}
	if err := strictDecode(body, &e); err != nil {
		return EnvelopeV2{}, err
	}
	if err := validateEnvelopeV2(&e); err != nil {
		return EnvelopeV2{}, err
	}
	return e, nil
}

// SealerV2 builds version 2 envelopes for one target.
type SealerV2 struct {
	recipient   *age.HybridRecipient
	recipientID string
	signer      *SignerV2
}

// NewSealerV2 pins the hybrid recipient and the two signing keys.
func NewSealerV2(recipient string, signer *SignerV2) (*SealerV2, error) {
	canonical, err := CanonicalHybridRecipient(recipient)
	if err != nil {
		return nil, err
	}
	r, err := age.ParseHybridRecipient(canonical)
	if err != nil {
		return nil, err
	}
	if signer == nil {
		return nil, errors.New("sealedhook: signing keys are required")
	}
	return &SealerV2{recipient: r, recipientID: recipientIDV2(canonical), signer: signer}, nil
}

// Seal encrypts the values to the recipient and signs the envelope with both
// keys. The plaintext JSON buffer is zeroed as soon as it has been encrypted.
func (s *SealerV2) Seal(d Draft) (Sealed, error) {
	if d.Lifetime <= 0 || d.Lifetime > MaxLifetime {
		return Sealed{}, fmt.Errorf("%w: lifetime must be in (0, %s]", ErrMalformed, MaxLifetime)
	}
	issued := d.IssuedAt.UTC().Truncate(time.Second)
	e := EnvelopeV2{
		V: VersionV2, Suite: SuiteV2, KeyIDEd25519: s.signer.ed.pub.IDV2(), KeyIDMLDSA65: s.signer.pq.pub.ID(),
		RecipientID: s.recipientID, TargetID: d.TargetID, InstanceID: d.InstanceID, Namespace: d.Namespace, Route: d.Route,
		Generation: d.Generation, Source: d.Source, Op: d.Op, Names: append([]Name{}, d.Names...), IdempotencyKey: d.IdempotencyKey,
		IssuedAt: issued.Format(timeLayout), ExpiresAt: issued.Add(d.Lifetime).Format(timeLayout),
	}
	payload := Payload{IdempotencyKey: d.IdempotencyKey, Binding: d.Binding, Values: append([]Value{}, d.Values...)}
	scope := e.scope()
	if err := validatePayload(&scope, &payload); err != nil {
		return Sealed{}, err
	}
	plain, err := json.Marshal(payload, jsonwire.Stable)
	if err != nil {
		return Sealed{}, err
	}
	var ct bytes.Buffer
	w, err := age.Encrypt(&ct, s.recipient)
	if err == nil {
		_, err = w.Write(plain)
		if err == nil {
			err = w.Close()
		}
	}
	clear(plain)
	if err != nil {
		return Sealed{}, fmt.Errorf("sealedhook: encrypt: %w", err)
	}
	if ct.Len() > MaxCiphertext {
		return Sealed{}, ErrTooLarge
	}
	e.Ciphertext = base64.StdEncoding.EncodeToString(ct.Bytes())
	e.CiphertextDigest = Digest(ct.Bytes())
	e.SigEd25519, e.SigMLDSA65 = strings.Repeat("A", ed25519SigBase64), strings.Repeat("A", mldsa65SigBase64)
	if err := validateEnvelopeV2(&e); err != nil {
		return Sealed{}, err
	}
	if e.SigEd25519, e.SigMLDSA65, err = s.signer.sign(envelopeDomainV2, envelopeBytesV2(&e)); err != nil {
		return Sealed{}, err
	}
	body, err := json.Marshal(e, jsonwire.Stable)
	if err != nil {
		return Sealed{}, err
	}
	if len(body) > MaxEnvelopeV2 {
		return Sealed{}, ErrTooLarge
	}
	return Sealed{Body: body, Digest: Digest(body), IdempotencyKey: e.IdempotencyKey}, nil
}

// PinnedSenderV2 is a sender key pair a receiver trusts, optionally only until
// NotAfter. The pair is the unit of trust: both keys are always required.
type PinnedSenderV2 struct {
	Ed       PublicKey
	PQ       MLDSAPublicKey
	NotAfter time.Time
}

// OpenerV2 verifies and decrypts version 2 envelopes on the receiver side.
type OpenerV2 struct {
	identity    *age.HybridIdentity
	recipientID string
	senders     map[[2]string]PinnedSenderV2
}

// NewOpenerV2 pins the receiver's hybrid identity and the trusted sender pairs.
func NewOpenerV2(identity string, senders []PinnedSenderV2) (*OpenerV2, error) {
	id, err := age.ParseHybridIdentity(strings.TrimSpace(identity))
	if err != nil {
		return nil, errors.New("sealedhook: identity must be an age hybrid secret key")
	}
	if len(senders) == 0 {
		return nil, errors.New("sealedhook: at least one pinned sender pair is required")
	}
	pinned := make(map[[2]string]PinnedSenderV2, len(senders))
	for _, k := range senders {
		if !k.Ed.valid() || !k.PQ.valid() {
			return nil, errors.New("sealedhook: a pinned sender needs both an Ed25519 and an ML-DSA-65 key")
		}
		pinned[[2]string{k.Ed.IDV2(), k.PQ.ID()}] = k
	}
	return &OpenerV2{identity: id, recipientID: recipientIDV2(id.Recipient().String()), senders: pinned}, nil
}

// checkHybridHeader refuses any age header that is not exactly one hybrid
// stanza of the expected shape, before a key is tried. age's decrypt path has
// no label check, so this is where a classical, passphrase, ssh, plugin or
// additional recipient is excluded. The scan follows internal/crypto/backup
// (which this leaf package cannot import).
func checkHybridHeader(ct []byte) error {
	limit := ct
	if len(limit) > MaxHeaderV2+1 {
		limit = limit[:MaxHeaderV2+1]
	}
	header, err := age.ExtractHeader(bytes.NewReader(limit))
	if err != nil || len(header) > MaxHeaderV2 {
		return fmt.Errorf("%w: age header", ErrPayload)
	}
	var kinds []string
	var args [][]string
	var bodies []strings.Builder
	for _, line := range strings.Split(string(header), "\n") {
		switch {
		case strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "-> "):
			f := strings.Fields(line)
			if len(f) < 2 {
				return fmt.Errorf("%w: age header", ErrPayload)
			}
			kinds, args, bodies = append(kinds, f[1]), append(args, f[2:]), append(bodies, strings.Builder{})
		case len(bodies) > 0:
			bodies[len(bodies)-1].WriteString(line)
		}
	}
	if len(kinds) != 1 || kinds[0] != hybridStanzaType || len(args[0]) != 1 {
		return fmt.Errorf("%w: recipient stanzas", ErrPayload)
	}
	if enc, err := base64.RawStdEncoding.DecodeString(args[0][0]); err != nil || len(enc) != hybridEncapsulationBytes {
		return fmt.Errorf("%w: recipient stanza", ErrPayload)
	}
	if body, err := base64.RawStdEncoding.DecodeString(bodies[0].String()); err != nil || len(body) != hybridWrappedKeyBytes {
		return fmt.Errorf("%w: recipient stanza", ErrPayload)
	}
	return nil
}

// shape describes the exact member set of a JSON value for checkShape.
type shape struct {
	fields map[string]*shape // an object: exactly these members, case-sensitive
	item   *shape            // an array of this shape
	limit  int               // maximum array length
}

var (
	scalar      = &shape{}
	nameShape   = &shape{fields: map[string]*shape{"surface": scalar, "name": scalar}}
	sourceShape = &shape{fields: map[string]*shape{"org": scalar, "project": scalar, "environment": scalar, "revision": scalar}}
	envelopeV2  = &shape{fields: map[string]*shape{
		"v": scalar, "suite": scalar, "key_id_ed25519": scalar, "key_id_mldsa65": scalar, "recipient_id": scalar,
		"target_id": scalar, "instance_id": scalar, "namespace": scalar, "route": scalar, "generation": scalar,
		"source": sourceShape, "op": scalar, "names": {item: nameShape, limit: MaxNames}, "ciphertext": scalar,
		"ciphertext_digest": scalar, "idempotency_key": scalar, "issued_at": scalar, "expires_at": scalar,
		"sig_ed25519": scalar, "sig_mldsa65": scalar,
	}}
	ackV2 = &shape{fields: map[string]*shape{
		"v": scalar, "suite": scalar, "envelope_digest": scalar, "idempotency_key": scalar, "status": scalar, "reason": scalar,
		"key_id_ed25519": scalar, "key_id_mldsa65": scalar, "sig_ed25519": scalar, "sig_mldsa65": scalar,
	}}
)

// checkShape walks the token stream before the struct decode. encoding/json
// matches member names case-insensitively and keeps the last duplicate, and it
// decodes every array element, so a body could carry a second signature under
// another spelling or hundreds of thousands of empty names. Here every member
// must be spelled exactly, appear once, and belong to the set; arrays stop at
// their limit before anything is built from them.
func checkShape(raw []byte, want *shape) error {
	// Duplicate names are left to walkShape, which refuses them with the
	// member-set error rather than the decoder's own text.
	dec := jsontext.NewDecoder(bytes.NewReader(raw), jsontext.AllowDuplicateNames(true))
	if err := walkShape(dec, want); err != nil {
		return err
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing data", ErrMalformed)
	}
	return nil
}

func walkShape(dec *jsontext.Decoder, s *shape) error {
	bad := func() error { return fmt.Errorf("%w: member set", ErrMalformed) }
	tok, err := dec.ReadToken()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	delim := tok.Kind()
	isDelim := delim == '{' || delim == '['
	switch {
	case s.fields != nil:
		if !isDelim || delim != '{' {
			return bad()
		}
		seen := map[string]bool{}
		for dec.PeekKind() != '}' {
			key, err := dec.ReadToken()
			if err != nil || key.Kind() != '"' {
				return bad()
			}
			name := key.String()
			child := s.fields[name]
			if child == nil || seen[name] {
				return bad()
			}
			seen[name] = true
			if err := walkShape(dec, child); err != nil {
				return err
			}
		}
		_, err = dec.ReadToken() // the closing brace
	case s.item != nil:
		if !isDelim || delim != '[' {
			return bad()
		}
		for n := 0; dec.PeekKind() != ']'; n++ {
			if n >= s.limit {
				return fmt.Errorf("%w: too many array elements", ErrMalformed)
			}
			if err := walkShape(dec, s.item); err != nil {
				return err
			}
		}
		_, err = dec.ReadToken()
	case isDelim:
		return bad()
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return nil
}

func decodeExact(s string, n int) ([]byte, bool) {
	raw, err := base64.StdEncoding.Strict().DecodeString(s)
	return raw, err == nil && len(raw) == n
}

// Open verifies both signatures against a pinned sender pair, enforces the
// validity window, checks the ciphertext digest and the age header, decrypts,
// and checks the payload matches the envelope. Signatures are checked before
// any decryption. The returned Envelope is the version-neutral view (V is 2,
// Ciphertext and Sig are empty).
func (o *OpenerV2) Open(body []byte, now time.Time) (Opened, error) {
	e, err := ParseEnvelopeV2(body)
	if err != nil {
		return Opened{}, err
	}
	pinned, ok := o.senders[[2]string{e.KeyIDEd25519, e.KeyIDMLDSA65}]
	if !ok {
		return Opened{}, ErrUnknownKey
	}
	if e.RecipientID != o.recipientID {
		return Opened{}, ErrRecipient
	}
	edSig, ok1 := decodeExact(e.SigEd25519, ed25519.SignatureSize)
	pqSig, ok2 := decodeExact(e.SigMLDSA65, MLDSA65SignatureSize)
	if !ok1 || !ok2 {
		return Opened{}, fmt.Errorf("%w: signature encoding", ErrMalformed)
	}
	msg := envelopeBytesV2(&e)
	if !ed25519.Verify(pinned.Ed.key, msg, edSig) || mldsa.Verify(pinned.PQ.key, msg, pqSig, &mldsa.Options{Context: envelopeDomainV2}) != nil {
		return Opened{}, ErrSignature
	}
	issued, _ := time.Parse(timeLayout, e.IssuedAt)
	expires, _ := time.Parse(timeLayout, e.ExpiresAt)
	now = now.UTC()
	if issued.After(now.Add(ClockSkew)) || !now.Before(expires) {
		return Opened{}, ErrExpired
	}
	if !pinned.NotAfter.IsZero() && issued.After(pinned.NotAfter) {
		return Opened{}, ErrUnknownKey
	}
	// Canonical base64 only: encoding/base64 skips line breaks, and Strict
	// refuses non-zero trailing bits, so the encoded form is the one the
	// digest and the body digest cover.
	ct, err := base64.StdEncoding.Strict().DecodeString(e.Ciphertext)
	if err != nil || strings.ContainsAny(e.Ciphertext, "\r\n") || len(ct) == 0 || len(ct) > MaxCiphertext {
		return Opened{}, fmt.Errorf("%w: ciphertext encoding", ErrMalformed)
	}
	if Digest(ct) != e.CiphertextDigest {
		return Opened{}, ErrSignature
	}
	if err := checkHybridHeader(ct); err != nil {
		return Opened{}, err
	}
	r, err := age.Decrypt(bytes.NewReader(ct), o.identity)
	if err != nil {
		return Opened{}, fmt.Errorf("%w: decrypt", ErrPayload)
	}
	plain, err := io.ReadAll(io.LimitReader(r, MaxCiphertext+1))
	if err != nil || len(plain) > MaxCiphertext {
		clear(plain)
		return Opened{}, fmt.Errorf("%w: decrypt", ErrPayload)
	}
	var p Payload
	err = strictDecode(plain, &p)
	clear(plain)
	if err != nil {
		return Opened{}, ErrPayload
	}
	scope := e.scope()
	if err := validatePayload(&scope, &p); err != nil {
		return Opened{}, err
	}
	return Opened{Envelope: scope, Payload: p, Digest: Digest(body)}, nil
}

// AckV2 is the wire form of a version 2 acknowledgement.
type AckV2 struct {
	V              int       `json:"v"`
	Suite          string    `json:"suite"`
	EnvelopeDigest string    `json:"envelope_digest"`
	IdempotencyKey string    `json:"idempotency_key"`
	Status         AckStatus `json:"status"`
	Reason         string    `json:"reason,omitempty"`
	KeyIDEd25519   string    `json:"key_id_ed25519"`
	KeyIDMLDSA65   string    `json:"key_id_mldsa65"`
	SigEd25519     string    `json:"sig_ed25519"`
	SigMLDSA65     string    `json:"sig_mldsa65"`
}

func ackBytesV2(a *AckV2) []byte {
	return newLP(ackDomainV2).i64(int64(a.V)).str(a.Suite).str(a.KeyIDEd25519).str(a.KeyIDMLDSA65).
		str(a.EnvelopeDigest).str(a.IdempotencyKey).str(string(a.Status)).str(a.Reason)
}

func validateAckV2(a *AckV2) error {
	if a.V != VersionV2 || a.Suite != SuiteV2 {
		return fmt.Errorf("%w: unsupported ack version or suite", ErrMalformed)
	}
	if !digestSyntax.MatchString(a.EnvelopeDigest) || !hexKey.MatchString(a.IdempotencyKey) ||
		!edKeyIDV2.MatchString(a.KeyIDEd25519) || !pqKeyIDV2.MatchString(a.KeyIDMLDSA65) {
		return fmt.Errorf("%w: invalid ack fields", ErrMalformed)
	}
	if len(a.SigEd25519) != ed25519SigBase64 || len(a.SigMLDSA65) != mldsa65SigBase64 {
		return fmt.Errorf("%w: missing or wrong-size signature", ErrMalformed)
	}
	return validateStatus(a.Status, a.Reason)
}

// SignAck completes and signs a version 2 acknowledgement with both keys.
func (s *SignerV2) SignAck(digest, idempotencyKey string, status AckStatus, reason string) ([]byte, error) {
	a := AckV2{
		V: VersionV2, Suite: SuiteV2, EnvelopeDigest: digest, IdempotencyKey: idempotencyKey, Status: status, Reason: reason,
		KeyIDEd25519: s.ed.pub.IDV2(), KeyIDMLDSA65: s.pq.pub.ID(),
		SigEd25519: strings.Repeat("A", ed25519SigBase64), SigMLDSA65: strings.Repeat("A", mldsa65SigBase64),
	}
	if err := validateAckV2(&a); err != nil {
		return nil, err
	}
	var err error
	if a.SigEd25519, a.SigMLDSA65, err = s.sign(ackDomainV2, ackBytesV2(&a)); err != nil {
		return nil, err
	}
	return json.Marshal(a, jsonwire.Stable)
}

// ParseAckV2 decodes and structurally validates an acknowledgement without
// verifying it.
func ParseAckV2(body []byte) (AckV2, error) {
	if len(body) > MaxAck {
		return AckV2{}, ErrTooLarge
	}
	var a AckV2
	if err := checkShape(body, ackV2); err != nil {
		return AckV2{}, err
	}
	if err := strictDecode(body, &a); err != nil {
		return AckV2{}, err
	}
	if err := validateAckV2(&a); err != nil {
		return AckV2{}, err
	}
	return a, nil
}

// VerifyAckV2 accepts an acknowledgement only when both signatures verify under
// the two pinned acknowledgement keys and it is bound to exactly this
// envelope's digest and idempotency key. The returned Ack carries V 2 and the
// ML-DSA key id.
func VerifyAckV2(body []byte, ackEd PublicKey, ackPQ MLDSAPublicKey, digest, idempotencyKey string) (Ack, error) {
	if !ackEd.valid() || !ackPQ.valid() {
		return Ack{}, ErrUnknownKey
	}
	a, err := ParseAckV2(body)
	if err != nil {
		return Ack{}, err
	}
	if a.KeyIDEd25519 != ackEd.IDV2() || a.KeyIDMLDSA65 != ackPQ.ID() {
		return Ack{}, ErrUnknownKey
	}
	edSig, ok1 := decodeExact(a.SigEd25519, ed25519.SignatureSize)
	pqSig, ok2 := decodeExact(a.SigMLDSA65, MLDSA65SignatureSize)
	if !ok1 || !ok2 {
		return Ack{}, fmt.Errorf("%w: signature encoding", ErrMalformed)
	}
	msg := ackBytesV2(&a)
	if !ed25519.Verify(ackEd.key, msg, edSig) || mldsa.Verify(ackPQ.key, msg, pqSig, &mldsa.Options{Context: ackDomainV2}) != nil {
		return Ack{}, ErrSignature
	}
	if a.EnvelopeDigest != digest || a.IdempotencyKey != idempotencyKey {
		return Ack{}, ErrAckMismatch
	}
	return Ack{V: VersionV2, EnvelopeDigest: a.EnvelopeDigest, IdempotencyKey: a.IdempotencyKey, Status: a.Status, Reason: a.Reason, KeyID: a.KeyIDMLDSA65}, nil
}
