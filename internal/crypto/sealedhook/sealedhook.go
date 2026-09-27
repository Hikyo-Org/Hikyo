// Package sealedhook owns every cryptographic operation of the sealed webhook
// receiver protocol (docs/spec/sealed-webhook.md). Payloads are age-encrypted
// to a pinned X25519 recipient, envelopes and acknowledgements are Ed25519
// signed over a length-prefixed injective encoding, and nothing outside the
// ciphertext ever carries a secret value.
//
// The adapter transport and the reference receiver call this package; neither
// touches age or Ed25519 directly, so the encryption-model ADR seam ("the
// crypto packages are the only callers of a cryptographic primitive") holds.
package sealedhook

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"
)

// Version is the only protocol version this package speaks.
const Version = 1

const (
	// MaxLifetime bounds expires_at - issued_at. A captured envelope is
	// useless to a replayer once it expires.
	MaxLifetime = 5 * time.Minute
	// ClockSkew is the tolerance a receiver grants an issued_at in its future.
	ClockSkew = 30 * time.Second
	// MaxNames bounds names per envelope.
	MaxNames = 256
	// MaxCiphertext bounds the encrypted payload.
	MaxCiphertext = 1 << 20
	// MaxEnvelope bounds a whole request body.
	MaxEnvelope = MaxCiphertext*4/3 + 64<<10
	// MaxAck bounds an acknowledgement body; targets may configure less.
	MaxAck = 64 << 10
	// SyncPath is the only receiver path. It is fixed by the protocol so a
	// configured origin cannot smuggle a path, query, or fragment.
	SyncPath = "/hikyo/v1/sync"
	// ContentType names the envelope media type.
	ContentType = "application/vnd.hikyo.sealed-envelope.v1+json"
	// AckContentType names the acknowledgement media type.
	AckContentType = "application/vnd.hikyo.sealed-ack.v1+json"

	timeLayout = "2006-01-02T15:04:05Z"

	envelopeDomain    = "hikyo/sealed-webhook/envelope/v1"
	ackDomain         = "hikyo/sealed-webhook/ack/v1"
	fingerprintDomain = "hikyo/sealed-webhook/fingerprint/v1"
	idempotencyDomain = "hikyo/sealed-webhook/idempotency/v1"
	destinationDomain = "hikyo/sealed-webhook/destination/v1"
)

// Op is the closed operation set.
type Op string

const (
	OpUpsert Op = "upsert"
	OpPrune  Op = "prune"
	OpProbe  Op = "probe"
)

// AckStatus is the closed acknowledgement status set. Only Applied and
// AlreadyApplied are successes; every other status is a failure by name.
type AckStatus string

const (
	AckApplied        AckStatus = "applied"
	AckAlreadyApplied AckStatus = "already_applied"
	AckConflict       AckStatus = "conflict"
	AckRejected       AckStatus = "rejected"
)

var (
	ErrMalformed   = errors.New("sealedhook: malformed message")
	ErrSignature   = errors.New("sealedhook: signature verification failed")
	ErrUnknownKey  = errors.New("sealedhook: signing key is not pinned")
	ErrExpired     = errors.New("sealedhook: envelope is expired or not yet valid")
	ErrPayload     = errors.New("sealedhook: payload does not match its envelope")
	ErrAckMismatch = errors.New("sealedhook: acknowledgement is bound to a different envelope")
	ErrTooLarge    = errors.New("sealedhook: message exceeds its size bound")
)

var (
	idSyntax     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	routeSyntax  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	nameSyntax   = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,255}$`)
	hexKey       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reasonSyntax = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
)

// ValidID reports whether s is valid target, instance, or namespace syntax.
func ValidID(s string) bool { return idSyntax.MatchString(s) }

// Name is one canonical effective name on one surface. Names are
// public-class identifiers; values never appear here.
type Name struct {
	Surface string `json:"surface"`
	Name    string `json:"name"`
}

// Source pins the exact Hikyo source scope and revision.
type Source struct {
	Org         string `json:"org"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
	Revision    int64  `json:"revision"`
}

// Envelope is the wire form of one request.
type Envelope struct {
	V              int    `json:"v"`
	TargetID       string `json:"target_id"`
	InstanceID     string `json:"instance_id"`
	Namespace      string `json:"namespace"`
	Route          string `json:"route"`
	Generation     int64  `json:"generation"`
	Source         Source `json:"source"`
	Op             Op     `json:"op"`
	Names          []Name `json:"names"`
	Ciphertext     []byte `json:"ciphertext"`
	IdempotencyKey string `json:"idempotency_key"`
	IssuedAt       string `json:"issued_at"`
	ExpiresAt      string `json:"expires_at"`
	KeyID          string `json:"key_id"`
	Sig            []byte `json:"sig"`
}

// Value is one plaintext entry. It exists only inside the age ciphertext.
type Value struct {
	Surface string `json:"surface"`
	Name    string `json:"name"`
	Value   string `json:"value"`
}

// Payload is the plaintext sealed to the recipient. Binding is the tenant's
// write-only adapter credential, which lets a receiver authorize the
// namespace without the credential ever leaving the ciphertext.
type Payload struct {
	IdempotencyKey string  `json:"idempotency_key"`
	Binding        string  `json:"binding"`
	Values         []Value `json:"values"`
}

// Ack is the wire form of a receiver acknowledgement.
type Ack struct {
	V              int       `json:"v"`
	EnvelopeDigest string    `json:"envelope_digest"`
	IdempotencyKey string    `json:"idempotency_key"`
	Status         AckStatus `json:"status"`
	Reason         string    `json:"reason,omitempty"`
	KeyID          string    `json:"key_id"`
	Sig            []byte    `json:"sig"`
}

// lp is the length-prefixed injective encoding shared with the AAD schema in
// internal/crypto: each field a uint32 big-endian length then its bytes.
type lp []byte

func (b lp) add(field []byte) lp {
	if uint64(len(field)) > math.MaxUint32 {
		panic("sealedhook: length-prefixed field exceeds uint32")
	}
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(field)))
	return append(append(b, n[:]...), field...)
}

func (b lp) str(s string) lp { return b.add([]byte(s)) }
func (b lp) i64(v int64) lp  { return b.str(strconv.FormatInt(v, 10)) }
func (b lp) sum() [32]byte   { return sha256.Sum256(b) }
func (b lp) hexSum() string  { s := b.sum(); return hex.EncodeToString(s[:]) }
func newLP(domain string) lp { return lp(nil).str(domain) }

// PublicKey is a pinned Ed25519 verification key. Its text form is
// "ed25519:" followed by 64 lowercase hex digits.
type PublicKey struct{ key ed25519.PublicKey }

// ParsePublicKey parses the canonical text form.
func ParsePublicKey(raw string) (PublicKey, error) {
	body, ok := strings.CutPrefix(strings.TrimSpace(raw), "ed25519:")
	if !ok || !hexKey.MatchString(body) {
		return PublicKey{}, errors.New("sealedhook: public key must be ed25519:<64 lowercase hex>")
	}
	key, _ := hex.DecodeString(body)
	return PublicKey{key: ed25519.PublicKey(key)}, nil
}

func (p PublicKey) String() string {
	if len(p.key) == 0 {
		return ""
	}
	return "ed25519:" + hex.EncodeToString(p.key)
}

// ID is the key_id carried in every signed message.
func (p PublicKey) ID() string {
	sum := sha256.Sum256(p.key)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (p PublicKey) valid() bool { return len(p.key) == ed25519.PublicKeySize }

// Signer holds one Ed25519 signing key.
type Signer struct {
	key ed25519.PrivateKey
	pub PublicKey
}

// ParseSigningKeyPEM accepts a PKCS#8 "PRIVATE KEY" block holding an Ed25519
// key (the output of `openssl genpkey -algorithm ed25519`).
func ParseSigningKeyPEM(raw []byte) (*Signer, error) {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("sealedhook: signing key must be exactly one PKCS#8 PRIVATE KEY PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("sealedhook: signing key is not valid PKCS#8")
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("sealedhook: signing key is not Ed25519")
	}
	return newSigner(key), nil
}

func newSigner(key ed25519.PrivateKey) *Signer {
	return &Signer{key: key, pub: PublicKey{key: key.Public().(ed25519.PublicKey)}}
}

// GenerateSigningKey returns a fresh key as PKCS#8 PEM with its public text.
func GenerateSigningKey() (privatePEM []byte, public string, err error) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, "", err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), PublicKey{key: pub}.String(), nil
}

// Public returns the verification key.
func (s *Signer) Public() PublicKey { return s.pub }

// GenerateRecipient returns a fresh age X25519 identity and its recipient.
func GenerateRecipient() (identity, recipient string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.String(), id.Recipient().String(), nil
}

// CanonicalRecipient parses an age X25519 recipient and returns its canonical
// text, refusing anything else (passphrase, plugin, SSH recipients).
func CanonicalRecipient(raw string) (string, error) {
	r, err := age.ParseX25519Recipient(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("sealedhook: recipient must be an age X25519 public key (age1...)")
	}
	return r.String(), nil
}

// Fingerprint is the value an instance admin verifies out of band and types
// to activate a target. It covers everything that defines the trust boundary:
// origin, recipient, acknowledgement key, and generation.
func Fingerprint(origin, recipient, ackKey string, generation int64) (string, error) {
	canonical, err := CanonicalRecipient(recipient)
	if err != nil {
		return "", err
	}
	ack, err := ParsePublicKey(ackKey)
	if err != nil {
		return "", err
	}
	if generation < 1 {
		return "", errors.New("sealedhook: generation must be positive")
	}
	return "sha256:" + newLP(fingerprintDomain).str(origin).str(canonical).str(ack.String()).i64(generation).hexSum(), nil
}

// DestinationID derives the stable positive destination identity recorded in
// the ownership ledger. Any trust-boundary change (a new fingerprint) moves
// it, so existing targets fail closed with a destination-id mismatch.
func DestinationID(fingerprint, namespace string) int64 {
	sum := newLP(destinationDomain).str(fingerprint).str(namespace).sum()
	id := int64(binary.BigEndian.Uint64(sum[:8]) & math.MaxInt64)
	if id == 0 {
		return 1
	}
	return id
}

// IdempotencyParts identify one effect. Retries of the same effect in the
// same revision derive the same key; a new revision derives a new one.
type IdempotencyParts struct {
	TargetID    string
	Fingerprint string
	Route       string
	Generation  int64
	Revision    int64
	Op          Op
	Surface     string
	Name        string
}

// IdempotencyKey derives the at-least-once identity of one effect.
func IdempotencyKey(p IdempotencyParts) string {
	return newLP(idempotencyDomain).str(p.TargetID).str(p.Fingerprint).str(p.Route).
		i64(p.Generation).i64(p.Revision).str(string(p.Op)).str(p.Surface).str(p.Name).hexSum()
}

// NewProbeKey returns a fresh random key for a connection probe.
func NewProbeKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Digest is the acknowledgement binding of an exact request body.
func Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func envelopeBytes(e *Envelope) []byte {
	b := newLP(envelopeDomain).i64(int64(e.V)).str(e.TargetID).str(e.InstanceID).str(e.Namespace).
		str(e.Route).i64(e.Generation).str(e.Source.Org).str(e.Source.Project).str(e.Source.Environment).
		i64(e.Source.Revision).str(string(e.Op)).i64(int64(len(e.Names)))
	for _, n := range e.Names {
		b = b.str(n.Surface).str(n.Name)
	}
	return b.add(e.Ciphertext).str(e.IdempotencyKey).str(e.IssuedAt).str(e.ExpiresAt).str(e.KeyID)
}

func ackBytes(a *Ack) []byte {
	return newLP(ackDomain).i64(int64(a.V)).str(a.EnvelopeDigest).str(a.IdempotencyKey).
		str(string(a.Status)).str(a.Reason).str(a.KeyID)
}

func validateNames(op Op, names []Name) error {
	if names == nil {
		return fmt.Errorf("%w: names must be an array", ErrMalformed)
	}
	if op == OpProbe {
		if len(names) != 0 {
			return fmt.Errorf("%w: probe carries no names", ErrMalformed)
		}
		return nil
	}
	if len(names) == 0 || len(names) > MaxNames {
		return fmt.Errorf("%w: %s requires 1..%d names", ErrMalformed, op, MaxNames)
	}
	seen := make(map[Name]struct{}, len(names))
	for _, n := range names {
		if (n.Surface != "secret" && n.Surface != "variable") || !nameSyntax.MatchString(n.Name) {
			return fmt.Errorf("%w: invalid name entry", ErrMalformed)
		}
		if _, dup := seen[n]; dup {
			return fmt.Errorf("%w: duplicate name entry", ErrMalformed)
		}
		seen[n] = struct{}{}
	}
	return nil
}

func validateEnvelope(e *Envelope) error {
	if e.V != Version {
		return fmt.Errorf("%w: unsupported version", ErrMalformed)
	}
	if !idSyntax.MatchString(e.TargetID) || !idSyntax.MatchString(e.InstanceID) || !idSyntax.MatchString(e.Namespace) || !routeSyntax.MatchString(e.Route) || e.Generation < 1 {
		return fmt.Errorf("%w: invalid identity fields", ErrMalformed)
	}
	switch e.Op {
	case OpProbe:
		if e.Source != (Source{}) {
			return fmt.Errorf("%w: probe carries no source", ErrMalformed)
		}
	case OpUpsert, OpPrune:
		if e.Source.Org == "" || e.Source.Project == "" || e.Source.Environment == "" || e.Source.Revision < 1 ||
			!routeSyntax.MatchString(e.Source.Org) || !routeSyntax.MatchString(e.Source.Project) || !routeSyntax.MatchString(e.Source.Environment) {
			return fmt.Errorf("%w: invalid source", ErrMalformed)
		}
	default:
		return fmt.Errorf("%w: unknown op", ErrMalformed)
	}
	if err := validateNames(e.Op, e.Names); err != nil {
		return err
	}
	if len(e.Ciphertext) == 0 || len(e.Ciphertext) > MaxCiphertext {
		return fmt.Errorf("%w: ciphertext size", ErrMalformed)
	}
	if !hexKey.MatchString(e.IdempotencyKey) || !strings.HasPrefix(e.KeyID, "sha256:") || !hexKey.MatchString(e.KeyID[len("sha256:"):]) || len(e.Sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: invalid key fields", ErrMalformed)
	}
	issued, err1 := time.Parse(timeLayout, e.IssuedAt)
	expires, err2 := time.Parse(timeLayout, e.ExpiresAt)
	if err1 != nil || err2 != nil || issued.Format(timeLayout) != e.IssuedAt || expires.Format(timeLayout) != e.ExpiresAt {
		return fmt.Errorf("%w: timestamps must be canonical UTC seconds", ErrMalformed)
	}
	if !expires.After(issued) || expires.Sub(issued) > MaxLifetime {
		return fmt.Errorf("%w: lifetime exceeds %s", ErrMalformed, MaxLifetime)
	}
	return nil
}

func validatePayload(e *Envelope, p *Payload) error {
	if p.IdempotencyKey != e.IdempotencyKey || p.Binding == "" {
		return ErrPayload
	}
	switch e.Op {
	case OpUpsert:
		if len(p.Values) != len(e.Names) {
			return ErrPayload
		}
		for i, v := range p.Values {
			if v.Surface != e.Names[i].Surface || v.Name != e.Names[i].Name {
				return ErrPayload
			}
		}
	default:
		if len(p.Values) != 0 {
			return ErrPayload
		}
	}
	return nil
}

// Sealer builds signed, recipient-encrypted envelopes for one target.
type Sealer struct {
	recipient *age.X25519Recipient
	signer    *Signer
}

// NewSealer pins the recipient and the signing key.
func NewSealer(recipient string, signer *Signer) (*Sealer, error) {
	r, err := age.ParseX25519Recipient(strings.TrimSpace(recipient))
	if err != nil {
		return nil, errors.New("sealedhook: recipient must be an age X25519 public key (age1...)")
	}
	if signer == nil {
		return nil, errors.New("sealedhook: a signing key is required")
	}
	return &Sealer{recipient: r, signer: signer}, nil
}

// Draft is everything a sealed envelope needs before encryption.
type Draft struct {
	TargetID       string
	InstanceID     string
	Namespace      string
	Route          string
	Generation     int64
	Source         Source
	Op             Op
	Names          []Name
	Binding        string
	Values         []Value
	IdempotencyKey string
	IssuedAt       time.Time
	Lifetime       time.Duration
}

// Sealed is the exact request body and its acknowledgement binding.
type Sealed struct {
	Body           []byte
	Digest         string
	IdempotencyKey string
}

// Seal encrypts the values to the recipient and signs the envelope. The
// plaintext JSON buffer is zeroed as soon as it has been encrypted.
func (s *Sealer) Seal(d Draft) (Sealed, error) {
	if d.Lifetime <= 0 || d.Lifetime > MaxLifetime {
		return Sealed{}, fmt.Errorf("%w: lifetime must be in (0, %s]", ErrMalformed, MaxLifetime)
	}
	issued := d.IssuedAt.UTC().Truncate(time.Second)
	names := append([]Name{}, d.Names...)
	e := Envelope{
		V: Version, TargetID: d.TargetID, InstanceID: d.InstanceID, Namespace: d.Namespace, Route: d.Route,
		Generation: d.Generation, Source: d.Source, Op: d.Op, Names: names, IdempotencyKey: d.IdempotencyKey,
		IssuedAt: issued.Format(timeLayout), ExpiresAt: issued.Add(d.Lifetime).Format(timeLayout),
		KeyID: s.signer.pub.ID(),
	}
	payload := Payload{IdempotencyKey: d.IdempotencyKey, Binding: d.Binding, Values: append([]Value{}, d.Values...)}
	if err := validatePayload(&e, &payload); err != nil {
		return Sealed{}, err
	}
	plain, err := json.Marshal(payload)
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
	e.Ciphertext = ct.Bytes()
	e.Sig = make([]byte, ed25519.SignatureSize)
	if err := validateEnvelope(&e); err != nil {
		return Sealed{}, err
	}
	e.Sig = ed25519.Sign(s.signer.key, envelopeBytes(&e))
	body, err := json.Marshal(e)
	if err != nil {
		return Sealed{}, err
	}
	if len(body) > MaxEnvelope {
		return Sealed{}, ErrTooLarge
	}
	return Sealed{Body: body, Digest: Digest(body), IdempotencyKey: e.IdempotencyKey}, nil
}

func strictDecode(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing data", ErrMalformed)
	}
	return nil
}

// ParseEnvelope decodes and validates an envelope's structure. It does not
// verify the signature; callers use Opener.Open for that.
func ParseEnvelope(body []byte) (Envelope, error) {
	if len(body) > MaxEnvelope {
		return Envelope{}, ErrTooLarge
	}
	var e Envelope
	if err := strictDecode(body, &e); err != nil {
		return Envelope{}, err
	}
	if err := validateEnvelope(&e); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// PinnedKey is a sender verification key a receiver trusts, optionally only
// until NotAfter (the overlap window of a retiring key).
type PinnedKey struct {
	Key      PublicKey
	NotAfter time.Time
}

// Opener verifies and decrypts envelopes on the receiver side.
type Opener struct {
	identity *age.X25519Identity
	senders  map[string]PinnedKey
}

// NewOpener pins the receiver identity and the trusted sender keys.
func NewOpener(identity string, senders []PinnedKey) (*Opener, error) {
	id, err := age.ParseX25519Identity(strings.TrimSpace(identity))
	if err != nil {
		return nil, errors.New("sealedhook: identity must be an age X25519 secret key")
	}
	if len(senders) == 0 {
		return nil, errors.New("sealedhook: at least one pinned sender key is required")
	}
	pinned := make(map[string]PinnedKey, len(senders))
	for _, k := range senders {
		if !k.Key.valid() {
			return nil, errors.New("sealedhook: invalid pinned sender key")
		}
		pinned[k.Key.ID()] = k
	}
	return &Opener{identity: id, senders: pinned}, nil
}

// Opened is a verified, decrypted envelope.
type Opened struct {
	Envelope Envelope
	Payload  Payload
	Digest   string
}

// Open verifies the signature against a pinned sender key, enforces the
// validity window, decrypts, and checks the payload matches the envelope.
// The signature is checked before any decryption.
func (o *Opener) Open(body []byte, now time.Time) (Opened, error) {
	e, err := ParseEnvelope(body)
	if err != nil {
		return Opened{}, err
	}
	pinned, ok := o.senders[e.KeyID]
	if !ok {
		return Opened{}, ErrUnknownKey
	}
	if !ed25519.Verify(pinned.Key.key, envelopeBytes(&e), e.Sig) {
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
	r, err := age.Decrypt(bytes.NewReader(e.Ciphertext), o.identity)
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
	if err := validatePayload(&e, &p); err != nil {
		return Opened{}, err
	}
	return Opened{Envelope: e, Payload: p, Digest: Digest(body)}, nil
}

// SignAck completes and signs an acknowledgement for an envelope digest.
func (s *Signer) SignAck(digest, idempotencyKey string, status AckStatus, reason string) ([]byte, error) {
	a := Ack{V: Version, EnvelopeDigest: digest, IdempotencyKey: idempotencyKey, Status: status, Reason: reason, KeyID: s.pub.ID()}
	a.Sig = make([]byte, ed25519.SignatureSize)
	if err := validateAck(&a); err != nil {
		return nil, err
	}
	a.Sig = ed25519.Sign(s.key, ackBytes(&a))
	return json.Marshal(a)
}

func validateAck(a *Ack) error {
	if a.V != Version {
		return fmt.Errorf("%w: unsupported ack version", ErrMalformed)
	}
	digest, ok := strings.CutPrefix(a.EnvelopeDigest, "sha256:")
	if !ok || !hexKey.MatchString(digest) || !hexKey.MatchString(a.IdempotencyKey) || len(a.Sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: invalid ack fields", ErrMalformed)
	}
	switch a.Status {
	case AckApplied, AckAlreadyApplied:
		if a.Reason != "" {
			return fmt.Errorf("%w: success carries no reason", ErrMalformed)
		}
	case AckConflict, AckRejected:
		if !reasonSyntax.MatchString(a.Reason) {
			return fmt.Errorf("%w: failure reason must match [a-z0-9_]{1,64}", ErrMalformed)
		}
	default:
		return fmt.Errorf("%w: unknown ack status", ErrMalformed)
	}
	return nil
}

// ParseAck decodes and structurally validates an acknowledgement without
// verifying it.
func ParseAck(body []byte) (Ack, error) {
	if len(body) > MaxAck {
		return Ack{}, ErrTooLarge
	}
	var a Ack
	if err := strictDecode(body, &a); err != nil {
		return Ack{}, err
	}
	if err := validateAck(&a); err != nil {
		return Ack{}, err
	}
	return a, nil
}

// VerifyAck accepts an acknowledgement only when it is signed by the pinned
// acknowledgement key and bound to exactly this envelope's digest and
// idempotency key.
func VerifyAck(body []byte, ackKey PublicKey, digest, idempotencyKey string) (Ack, error) {
	if !ackKey.valid() {
		return Ack{}, ErrUnknownKey
	}
	a, err := ParseAck(body)
	if err != nil {
		return Ack{}, err
	}
	if a.KeyID != ackKey.ID() {
		return Ack{}, ErrUnknownKey
	}
	if !ed25519.Verify(ackKey.key, ackBytes(&a), a.Sig) {
		return Ack{}, ErrSignature
	}
	if a.EnvelopeDigest != digest || a.IdempotencyKey != idempotencyKey {
		return Ack{}, ErrAckMismatch
	}
	return a, nil
}
