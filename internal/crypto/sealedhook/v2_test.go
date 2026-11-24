package sealedhook

import (
	"bytes"
	"crypto/mldsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

type fixtureV2 struct {
	senderEd   *Signer
	senderPQ   *MLDSASigner
	ackEd      *Signer
	ackPQ      *MLDSASigner
	sender     *SignerV2
	acker      *SignerV2
	identity   string
	recipient  string
	sealer     *SealerV2
	opener     *OpenerV2
	now        time.Time
	otherIdent string
}

func mustPQ(t testing.TB) *MLDSASigner {
	t.Helper()
	pemKey, _, err := GenerateMLDSASigningKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseMLDSASigningKeyPEM(pemKey)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newFixtureV2(t testing.TB) fixtureV2 {
	t.Helper()
	f := fixtureV2{senderEd: seedSigner(1), ackEd: seedSigner(2), senderPQ: mustPQ(t), ackPQ: mustPQ(t), now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	var err error
	if f.identity, f.recipient, err = GenerateHybridRecipient(); err != nil {
		t.Fatal(err)
	}
	if f.otherIdent, _, err = GenerateHybridRecipient(); err != nil {
		t.Fatal(err)
	}
	f.sender, _ = NewSignerV2(f.senderEd, f.senderPQ)
	f.acker, _ = NewSignerV2(f.ackEd, f.ackPQ)
	if f.sealer, err = NewSealerV2(f.recipient, f.sender); err != nil {
		t.Fatal(err)
	}
	if f.opener, err = NewOpenerV2(f.identity, []PinnedSenderV2{{Ed: f.senderEd.Public(), PQ: f.senderPQ.Public()}}); err != nil {
		t.Fatal(err)
	}
	return f
}

func draftV2(now time.Time) Draft {
	d := upsertDraft(now)
	d.IdempotencyKey = IdempotencyKeyV2(IdempotencyParts{TargetID: "payments", Route: "tgt_1", Generation: 1, Revision: 7, Op: OpUpsert, Surface: "secret", Name: "DB_PASSWORD"})
	return d
}

// resign rebuilds a validly signed envelope from edited fields so a test can
// isolate one policy from the signature check.
func (f fixtureV2) resign(t testing.TB, ct []byte, edit func(*EnvelopeV2)) []byte {
	t.Helper()
	sealed, err := f.sealer.Seal(draftV2(f.now))
	if err != nil {
		t.Fatal(err)
	}
	e, err := ParseEnvelopeV2(sealed.Body)
	if err != nil {
		t.Fatal(err)
	}
	if ct != nil {
		e.Ciphertext, e.CiphertextDigest = base64.StdEncoding.EncodeToString(ct), Digest(ct)
	}
	if edit != nil {
		edit(&e)
	}
	e.SigEd25519, e.SigMLDSA65, err = f.sender.sign(envelopeDomainV2, envelopeBytesV2(&e))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(e, jsonwire.Stable)
	return body
}

func (f fixtureV2) payloadJSON() []byte {
	p := Payload{IdempotencyKey: draftV2(f.now).IdempotencyKey, Binding: "binding-token", Values: []Value{{Surface: "secret", Name: "DB_PASSWORD", Value: plaintextMarker}}}
	raw, _ := json.Marshal(p, jsonwire.Stable)
	return raw
}

// withStrayBits returns b64 with the final data character (the one before any
// padding) moved to the next alphabet entry. In canonical base64 the spare low
// bits of that character are zero, so +1 sets one: the decoded bytes are
// unchanged and only a Strict decoder refuses it.
func withStrayBits(b64 string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	end := strings.IndexByte(b64, '=')
	if end < 0 {
		panic("withStrayBits: the base64 has no padding, so no character has spare bits")
	}
	return b64[:end-1] + string(alphabet[strings.IndexByte(alphabet, b64[end-1])+1]) + b64[end:]
}

// sealUnaligned seals the draft, lengthening the binding until the age
// ciphertext is not a multiple of three bytes, so its base64 form has padding
// and therefore spare bits a non-strict decoder would ignore.
func (f fixtureV2) sealUnaligned(t testing.TB, d Draft) Sealed {
	t.Helper()
	for range 3 {
		sealed, err := f.sealer.Seal(d)
		if err != nil {
			t.Fatal(err)
		}
		e, _ := ParseEnvelopeV2(sealed.Body)
		if strings.Contains(e.Ciphertext, "=") {
			return sealed
		}
		d.Binding += "x"
	}
	t.Fatal("could not produce a padded ciphertext")
	return Sealed{}
}

// withMember returns a copy of a JSON object with one more member appended.
func withMember(obj []byte, member string) []byte {
	return []byte(strings.TrimSuffix(string(obj), "}") + "," + member + "}")
}

func encryptTo(t testing.TB, plain []byte, recipients ...age.Recipient) []byte {
	t.Helper()
	var ct bytes.Buffer
	w, err := age.Encrypt(&ct, recipients...)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(plain)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return ct.Bytes()
}

func TestV2SealOpenRoundTrip(t *testing.T) {
	f := newFixtureV2(t)
	sealed, err := f.sealer.Seal(draftV2(f.now))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed.Body, []byte(plaintextMarker)) || bytes.Contains(sealed.Body, []byte("binding-token")) {
		t.Fatal("plaintext leaked into the envelope")
	}
	if len(sealed.Body) > MaxEnvelopeV2 {
		t.Fatalf("envelope %d exceeds the bound %d", len(sealed.Body), MaxEnvelopeV2)
	}
	opened, err := f.opener.Open(sealed.Body, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Payload.Values[0].Value != plaintextMarker || opened.Digest != sealed.Digest || opened.Envelope.V != VersionV2 {
		t.Fatalf("opened = %+v", opened)
	}
	// A v1 opener cannot read it and a v2 opener cannot read a v1 body.
	fx := newFixture(t)
	if _, err := fx.opener.Open(sealed.Body, f.now); !errors.Is(err, ErrMalformed) {
		t.Fatalf("v1 opener accepted a v2 envelope: %v", err)
	}
	v1, _ := fx.sealer.Seal(upsertDraft(fx.now))
	if _, err := f.opener.Open(v1.Body, f.now); !errors.Is(err, ErrMalformed) {
		t.Fatalf("v2 opener accepted a v1 envelope: %v", err)
	}
}

func TestV2OpenRefusals(t *testing.T) {
	f := newFixtureV2(t)
	sealed, _ := f.sealer.Seal(draftV2(f.now))
	good, _ := ParseEnvelopeV2(sealed.Body)
	mutate := func(edit func(*EnvelopeV2)) []byte {
		e := good
		edit(&e)
		raw, _ := json.Marshal(e, jsonwire.Stable)
		return raw
	}
	strangerEd, strangerPQ := seedSigner(9), mustPQ(t)
	stranger, _ := NewSignerV2(strangerEd, strangerPQ)
	strangerSealer, _ := NewSealerV2(f.recipient, stranger)
	mixedSealer, _ := NewSealerV2(f.recipient, func() *SignerV2 { s, _ := NewSignerV2(f.senderEd, strangerPQ); return s }())
	strangerBody, _ := strangerSealer.Seal(draftV2(f.now))
	mixedBody, _ := mixedSealer.Seal(draftV2(f.now))
	otherRecipient, _ := age.ParseHybridIdentity(f.otherIdent)
	classical, _ := age.GenerateX25519Identity()
	hybridID, _ := age.ParseHybridIdentity(f.identity)
	scryptR, _ := age.NewScryptRecipient("correct horse")
	pqCtx := func(label string) []byte {
		sig, _ := f.senderPQ.key.Sign(nil, envelopeBytesV2(&good), &mldsa.Options{Context: label})
		return sig
	}

	cases := []struct {
		name string
		body []byte
		now  time.Time
		want error
	}{
		{"stripped ed25519 signature", mutate(func(e *EnvelopeV2) { e.SigEd25519 = "" }), f.now, ErrMalformed},
		{"stripped ml-dsa signature", mutate(func(e *EnvelopeV2) { e.SigMLDSA65 = "" }), f.now, ErrMalformed},
		{"short ml-dsa signature", mutate(func(e *EnvelopeV2) { e.SigMLDSA65 = e.SigMLDSA65[:len(e.SigMLDSA65)-4] }), f.now, ErrMalformed},
		{"ed25519 signature in the ml-dsa slot", mutate(func(e *EnvelopeV2) { e.SigMLDSA65 = e.SigEd25519 }), f.now, ErrMalformed},
		{"forged ed25519 signature", mutate(func(e *EnvelopeV2) { e.SigEd25519 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 64)) }), f.now, ErrSignature},
		{"forged ml-dsa signature", mutate(func(e *EnvelopeV2) {
			e.SigMLDSA65 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, MLDSA65SignatureSize))
		}), f.now, ErrSignature},
		{"valid ml-dsa signature under the ack label", mutate(func(e *EnvelopeV2) { e.SigMLDSA65 = base64.StdEncoding.EncodeToString(pqCtx(ackDomainV2)) }), f.now, ErrSignature},
		{"valid ml-dsa signature with no context", mutate(func(e *EnvelopeV2) { e.SigMLDSA65 = base64.StdEncoding.EncodeToString(pqCtx("")) }), f.now, ErrSignature},
		{"unpinned signer pair", strangerBody.Body, f.now, ErrUnknownKey},
		{"pinned ed25519 with an unpinned ml-dsa key", mixedBody.Body, f.now, ErrUnknownKey},
		{"v1 key id format", mutate(func(e *EnvelopeV2) { e.KeyIDEd25519 = f.senderEd.Public().ID() }), f.now, ErrMalformed},
		{"suite downgrade", mutate(func(e *EnvelopeV2) { e.Suite = "ed25519" }), f.now, ErrMalformed},
		{"suite unknown", mutate(func(e *EnvelopeV2) { e.Suite = "mldsa65" }), f.now, ErrMalformed},
		{"version 1", mutate(func(e *EnvelopeV2) { e.V = 1 }), f.now, ErrMalformed},
		{"version 3", mutate(func(e *EnvelopeV2) { e.V = 3 }), f.now, ErrMalformed},
		{"tampered target", mutate(func(e *EnvelopeV2) { e.TargetID = "other" }), f.now, ErrSignature},
		{"tampered route", mutate(func(e *EnvelopeV2) { e.Route = "tgt_2" }), f.now, ErrSignature},
		{"tampered generation", mutate(func(e *EnvelopeV2) { e.Generation = 2 }), f.now, ErrSignature},
		{"tampered recipient id", mutate(func(e *EnvelopeV2) { e.RecipientID = recipientIDV2(strings.Replace(f.recipient, "a", "b", 1)) }), f.now, ErrRecipient},
		{"tampered ciphertext digest", mutate(func(e *EnvelopeV2) { e.CiphertextDigest = Digest([]byte("x")) }), f.now, ErrSignature},
		{"tampered ciphertext", mutate(func(e *EnvelopeV2) {
			ct, _ := base64.StdEncoding.DecodeString(e.Ciphertext)
			ct[len(ct)-1] ^= 1
			e.Ciphertext = base64.StdEncoding.EncodeToString(ct)
		}), f.now, ErrSignature},
		{"expired", sealed.Body, f.now.Add(2 * time.Minute), ErrExpired},
		{"issued in the future", sealed.Body, f.now.Add(-time.Hour), ErrExpired},
		{"oversize ciphertext field", mutate(func(e *EnvelopeV2) { e.Ciphertext = strings.Repeat("A", MaxCiphertextBase64+4) }), f.now, ErrMalformed},
		{"oversize body", bytes.Repeat([]byte(" "), MaxEnvelopeV2+1), f.now, ErrTooLarge},
		{"truncated", sealed.Body[:len(sealed.Body)/2], f.now, ErrMalformed},
		{"unknown member", withMember(sealed.Body, `"extra":1`), f.now, ErrMalformed},
		{"duplicate signature member", withMember(sealed.Body, `"sig_mldsa65":"`+good.SigMLDSA65+`"`), f.now, ErrMalformed},
		{"classical only recipient", f.resign(t, encryptTo(t, f.payloadJSON(), classical.Recipient()), nil), f.now, ErrPayload},
		{"two hybrid recipients", f.resign(t, encryptTo(t, f.payloadJSON(), hybridID.Recipient(), otherRecipient.Recipient()), nil), f.now, ErrPayload},
		{"passphrase recipient", f.resign(t, encryptTo(t, f.payloadJSON(), scryptR), nil), f.now, ErrPayload},
		{"hybrid recipient of another receiver", f.resign(t, encryptTo(t, f.payloadJSON(), otherRecipient.Recipient()), nil), f.now, ErrPayload},
		{"payload for another idempotency key", f.resign(t, encryptTo(t, bytes.ReplaceAll(f.payloadJSON(), []byte(draftV2(f.now).IdempotencyKey), bytes.Repeat([]byte("0"), 64)), hybridID.Recipient()), nil), f.now, ErrPayload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.opener.Open(tc.body, tc.now); !errors.Is(err, tc.want) {
				t.Fatalf("Open = %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := f.opener.Open(f.resign(t, encryptTo(t, f.payloadJSON(), hybridID.Recipient()), nil), f.now); err != nil {
		t.Fatalf("control: a correctly re-signed hybrid envelope must open: %v", err)
	}
}

func TestV2SenderOverlapWindow(t *testing.T) {
	f := newFixtureV2(t)
	f.opener, _ = NewOpenerV2(f.identity, []PinnedSenderV2{{Ed: f.senderEd.Public(), PQ: f.senderPQ.Public(), NotAfter: f.now.Add(-time.Second)}})
	sealed, _ := f.sealer.Seal(draftV2(f.now))
	if _, err := f.opener.Open(sealed.Body, f.now); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("a pair retired before issue was accepted: %v", err)
	}
	f.opener, _ = NewOpenerV2(f.identity, []PinnedSenderV2{{Ed: f.senderEd.Public(), PQ: f.senderPQ.Public(), NotAfter: f.now}})
	if _, err := f.opener.Open(sealed.Body, f.now); err != nil {
		t.Fatalf("a pair retiring at the issue instant must still verify: %v", err)
	}
}

func TestV2AckBindsBothKeysDigestAndKey(t *testing.T) {
	f := newFixtureV2(t)
	digest, idem := "sha256:"+strings.Repeat("a", 64), strings.Repeat("b", 64)
	ack, err := f.acker.SignAck(digest, idem, AckApplied, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyAckV2(ack, f.ackEd.Public(), f.ackPQ.Public(), digest, idem)
	if err != nil || got.Status != AckApplied || got.V != VersionV2 {
		t.Fatalf("VerifyAckV2 = %+v, %v", got, err)
	}
	var parsed AckV2
	_ = json.Unmarshal(ack, &parsed)
	redo := func(edit func(*AckV2)) []byte {
		a := parsed
		edit(&a)
		raw, _ := json.Marshal(a, jsonwire.Stable)
		return raw
	}
	otherPQ := mustPQ(t)
	otherPair, _ := NewSignerV2(f.ackEd, otherPQ)
	foreign, _ := otherPair.SignAck(digest, idem, AckApplied, "")
	v1ack, _ := f.ackEd.SignAck(digest, idem, AckApplied, "")
	cases := []struct {
		name string
		body []byte
		want error
	}{
		{"stripped ml-dsa signature", redo(func(a *AckV2) { a.SigMLDSA65 = "" }), ErrMalformed},
		{"stripped ed25519 signature", redo(func(a *AckV2) { a.SigEd25519 = "" }), ErrMalformed},
		{"forged ml-dsa signature", redo(func(a *AckV2) {
			a.SigMLDSA65 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, MLDSA65SignatureSize))
		}), ErrSignature},
		{"forged ed25519 signature", redo(func(a *AckV2) { a.SigEd25519 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 64)) }), ErrSignature},
		{"status tampered", redo(func(a *AckV2) { a.Status = AckAlreadyApplied }), ErrSignature},
		{"unpinned ml-dsa key", foreign, ErrUnknownKey},
		{"suite changed", redo(func(a *AckV2) { a.Suite = "ed25519" }), ErrMalformed},
		{"version 1 acknowledgement", v1ack, ErrMalformed},
		{"duplicate member", withMember(ack, `"status":"applied"`), ErrMalformed},
		{"oversize", bytes.Repeat([]byte(" "), MaxAck+1), ErrTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := VerifyAckV2(tc.body, f.ackEd.Public(), f.ackPQ.Public(), digest, idem); !errors.Is(err, tc.want) {
				t.Fatalf("VerifyAckV2 = %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := VerifyAckV2(ack, f.ackEd.Public(), f.ackPQ.Public(), "sha256:"+strings.Repeat("c", 64), idem); !errors.Is(err, ErrAckMismatch) {
		t.Fatalf("other digest: %v", err)
	}
	if _, err := VerifyAckV2(ack, f.ackEd.Public(), f.ackPQ.Public(), digest, strings.Repeat("c", 64)); !errors.Is(err, ErrAckMismatch) {
		t.Fatalf("other idempotency key: %v", err)
	}
	// A version 1 verifier must not read a version 2 acknowledgement.
	if _, err := VerifyAck(ack, f.ackEd.Public(), digest, idem); !errors.Is(err, ErrMalformed) {
		t.Fatalf("v1 verifier accepted a v2 acknowledgement: %v", err)
	}
}

func TestV2KeyParsingAndBounds(t *testing.T) {
	_, pub, _ := GenerateMLDSASigningKey()
	if _, err := ParseMLDSAPublicKey(pub); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"empty": "", "short": pub[:len(pub)-2], "long": pub + "00", "upper": "mldsa65:" + strings.ToUpper(pub[8:]),
		"ed25519": "ed25519:" + strings.Repeat("0", 64), "prefix": "mldsa44:" + pub[8:], "non hex": "mldsa65:" + strings.Repeat("zz", MLDSA65PublicKeySize),
	} {
		if _, err := ParseMLDSAPublicKey(bad); err == nil {
			t.Errorf("%s: accepted %.20q", name, bad)
		}
	}
	// Signing key parsing: only seed-only ML-DSA-65 PKCS#8.
	edPEM, _, _ := GenerateSigningKey()
	if _, err := ParseMLDSASigningKeyPEM(edPEM); err == nil {
		t.Error("an Ed25519 PEM was accepted as ML-DSA")
	}
	if _, err := ParseSigningKeyPEM(func() []byte { p, _, _ := GenerateMLDSASigningKey(); return p }()); err == nil {
		t.Error("an ML-DSA PEM was accepted as Ed25519")
	}
	if _, err := ParseMLDSASigningKeyPEM([]byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n")); err == nil {
		t.Error("garbage PEM accepted")
	}
	if _, err := CanonicalHybridRecipient(func() string { _, r, _ := GenerateRecipient(); return r }()); err == nil {
		t.Error("an X25519 recipient was accepted as hybrid")
	}
	if _, err := NewSealer(func() string { _, r, _ := GenerateHybridRecipient(); return r }(), seedSigner(1)); err == nil {
		t.Error("a v1 sealer accepted a hybrid recipient")
	}
	if MaxCiphertextBase64 != 1398104 || MaxEnvelopeV2 != 1398104+128<<10 {
		t.Errorf("bounds drifted: %d %d", MaxCiphertextBase64, MaxEnvelopeV2)
	}
}

func TestV2FingerprintCoversEveryTrustInput(t *testing.T) {
	f := newFixtureV2(t)
	base, err := FingerprintV2("https://r.example", f.recipient, f.ackEd.Public(), f.ackPQ.Public(), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, otherRecipient, _ := GenerateHybridRecipient()
	variants := map[string]func() (string, error){
		"origin": func() (string, error) {
			return FingerprintV2("https://s.example", f.recipient, f.ackEd.Public(), f.ackPQ.Public(), 1)
		},
		"recipient": func() (string, error) {
			return FingerprintV2("https://r.example", otherRecipient, f.ackEd.Public(), f.ackPQ.Public(), 1)
		},
		"ed25519": func() (string, error) {
			return FingerprintV2("https://r.example", f.recipient, seedSigner(5).Public(), f.ackPQ.Public(), 1)
		},
		"ml-dsa": func() (string, error) {
			return FingerprintV2("https://r.example", f.recipient, f.ackEd.Public(), mustPQ(t).Public(), 1)
		},
		"generation": func() (string, error) {
			return FingerprintV2("https://r.example", f.recipient, f.ackEd.Public(), f.ackPQ.Public(), 2)
		},
	}
	for name, fn := range variants {
		got, err := fn()
		if err != nil || got == base {
			t.Errorf("%s did not change the fingerprint (%v)", name, err)
		}
	}
	v1, _ := Fingerprint("https://r.example", func() string { _, r, _ := GenerateRecipient(); return r }(), f.ackEd.Public().String(), 1)
	if v1 == base || !strings.HasPrefix(base, "swh2-mlkem768x25519-ed25519-mldsa65-sha256:") || strings.HasPrefix(v1, "swh2") {
		t.Errorf("v1 and v2 fingerprint namespaces overlap: %q %q", v1, base)
	}
	if DestinationIDV2(base, "prod") == DestinationID(base, "prod") {
		t.Error("destination ids must differ by version for the same fingerprint text")
	}
	if IdempotencyKeyV2(IdempotencyParts{TargetID: "a"}) == IdempotencyKey(IdempotencyParts{TargetID: "a"}) {
		t.Error("idempotency keys must differ by version")
	}
}

func TestV2HeaderBound(t *testing.T) {
	f := newFixtureV2(t)
	// An age file whose first line never ends inside the bound is refused
	// before any stanza is parsed or key tried.
	long := append([]byte("age-encryption.org/v1\n-> mlkem768x25519 "), bytes.Repeat([]byte("A"), MaxHeaderV2*2)...)
	if err := checkHybridHeader(long); err == nil {
		t.Fatal("an overlong header was accepted")
	}
	if err := checkHybridHeader([]byte("not age")); err == nil {
		t.Fatal("a non-age ciphertext was accepted")
	}
	sealed, _ := f.sealer.Seal(draftV2(f.now))
	e, _ := ParseEnvelopeV2(sealed.Body)
	ct, _ := base64.StdEncoding.DecodeString(e.Ciphertext)
	if err := checkHybridHeader(ct); err != nil {
		t.Fatalf("a real hybrid header was refused: %v", err)
	}
}

func FuzzParseEnvelopeV2(f *testing.F) {
	fx := newFixtureV2(f)
	sealed, _ := fx.sealer.Seal(draftV2(fx.now))
	f.Add(sealed.Body)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"v":2,"suite":"ed25519+mldsa65"}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		e, err := ParseEnvelopeV2(raw)
		if err != nil {
			return
		}
		if err := validateEnvelopeV2(&e); err != nil {
			t.Fatalf("accepted envelope fails revalidation: %v", err)
		}
		_, _ = fx.opener.Open(raw, fx.now)
	})
}

func FuzzParseAckV2(f *testing.F) {
	fx := newFixtureV2(f)
	ack, _ := fx.acker.SignAck("sha256:"+strings.Repeat("0", 64), strings.Repeat("1", 64), AckConflict, "exists_unowned")
	f.Add(ack)
	f.Add([]byte(`{"v":2}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		a, err := ParseAckV2(raw)
		if err != nil {
			return
		}
		if err := validateAckV2(&a); err != nil {
			t.Fatalf("accepted ack fails revalidation: %v", err)
		}
		_, _ = VerifyAckV2(raw, fx.ackEd.Public(), fx.ackPQ.Public(), a.EnvelopeDigest, a.IdempotencyKey)
	})
}

// vectorFile is the published protocol vector (docs/spec/sealed-webhook.md
// section 11). Keys marked test-only exist only here.
type vectorFile struct {
	Description  string            `json:"description"`
	Now          string            `json:"now"`
	Identity     string            `json:"receiver_identity_test_only"`
	Recipient    string            `json:"receiver_recipient"`
	SenderEd     string            `json:"sender_ed25519"`
	SenderPQ     string            `json:"sender_mldsa65"`
	AckEd        string            `json:"ack_ed25519"`
	AckPQ        string            `json:"ack_mldsa65"`
	Derived      map[string]string `json:"derived"`
	Envelope     string            `json:"envelope"`
	EnvelopeSign string            `json:"envelope_signed_bytes_hex"`
	Ack          string            `json:"ack"`
	AckDigest    string            `json:"ack_envelope_digest"`
	AckSign      string            `json:"ack_signed_bytes_hex"`
	Negative     []vectorNegative  `json:"negative"`
}

type vectorNegative struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Body string `json:"body"`
	Want string `json:"want"`
}

const vectorPath = "testdata/sealed-webhook-v2-vectors.json"

var errorClasses = map[string]error{
	"malformed": ErrMalformed, "signature": ErrSignature, "unknown_key": ErrUnknownKey, "expired": ErrExpired,
	"payload": ErrPayload, "recipient": ErrRecipient, "too_large": ErrTooLarge, "ack_mismatch": ErrAckMismatch,
}

func classOf(err error) string {
	for name, target := range errorClasses {
		if errors.Is(err, target) {
			return name
		}
	}
	return "other"
}

func TestV2GenerateVectors(t *testing.T) {
	if os.Getenv("HIKYO_UPDATE_VECTORS") != "1" {
		t.Skip("set HIKYO_UPDATE_VECTORS=1 to regenerate " + vectorPath)
	}
	f := newFixtureV2(t)
	d := draftV2(f.now)
	d.TargetID, d.InstanceID = "payments", "hikyo-eu"
	sealed, err := f.sealer.Seal(d)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := ParseEnvelopeV2(sealed.Body)
	ack, _ := f.acker.SignAck(sealed.Digest, e.IdempotencyKey, AckApplied, "")
	parsedAck, _ := ParseAckV2(ack)
	fp, _ := FingerprintV2("https://receiver.example", f.recipient, f.ackEd.Public(), f.ackPQ.Public(), 3)
	vf := vectorFile{
		Description: "Sealed webhook protocol v2 (proposed). Verify the envelope with the pinned sender pair and the receiver identity at `now`, verify the acknowledgement with the pinned acknowledgement pair, recompute the signed bytes from the parsed members, and require every negative body to be refused with its class. Keys marked test-only exist for these vectors only.",
		Now:         f.now.Format(time.RFC3339), Identity: f.identity, Recipient: f.recipient,
		SenderEd: f.senderEd.Public().String(), SenderPQ: f.senderPQ.Public().String(), AckEd: f.ackEd.Public().String(), AckPQ: f.ackPQ.Public().String(),
		Derived: map[string]string{
			"sender_key_id_ed25519": f.senderEd.Public().IDV2(), "sender_key_id_mldsa65": f.senderPQ.Public().ID(),
			"ack_key_id_ed25519": f.ackEd.Public().IDV2(), "ack_key_id_mldsa65": f.ackPQ.Public().ID(),
			"recipient_id": recipientIDV2(f.recipient), "fingerprint": fp,
			"fingerprint_inputs": "origin=https://receiver.example generation=3 suite=ed25519+mldsa65", "idempotency_key": e.IdempotencyKey,
		},
		Envelope: string(sealed.Body), EnvelopeSign: hex.EncodeToString(envelopeBytesV2(&e)),
		Ack: string(ack), AckDigest: sealed.Digest, AckSign: hex.EncodeToString(ackBytesV2(&parsedAck)),
	}
	vf.Derived["destination_id_namespace_prod"] = strconv.FormatInt(DestinationIDV2(fp, "prod"), 10)
	mut := func(edit func(*EnvelopeV2)) string {
		x := e
		edit(&x)
		raw, _ := json.Marshal(x, jsonwire.Stable)
		return string(raw)
	}
	hybridID, _ := age.ParseHybridIdentity(f.identity)
	classical, _ := age.GenerateX25519Identity()
	neg := []vectorNegative{
		{"stripped ed25519 signature", "envelope", mut(func(x *EnvelopeV2) { x.SigEd25519 = "" }), "malformed"},
		{"stripped ml-dsa signature", "envelope", mut(func(x *EnvelopeV2) { x.SigMLDSA65 = "" }), "malformed"},
		{"tampered route", "envelope", mut(func(x *EnvelopeV2) { x.Route = "tgt_2" }), "signature"},
		{"suite downgrade", "envelope", mut(func(x *EnvelopeV2) { x.Suite = "ed25519" }), "malformed"},
		{"version 1 member set", "envelope", mut(func(x *EnvelopeV2) { x.V = 1 }), "malformed"},
		{"classical-only recipient with valid signatures", "envelope", string(f.resign(t, encryptTo(t, f.payloadJSON(), classical.Recipient()), func(x *EnvelopeV2) { x.TargetID, x.InstanceID = "payments", "hikyo-eu" })), "payload"},
		{"valid signatures but another receiver's recipient id", "envelope", string(f.resign(t, nil, func(x *EnvelopeV2) {
			x.TargetID, x.InstanceID, x.RecipientID = "payments", "hikyo-eu", recipientIDV2("age1pq1other")
		})), "recipient"},
		{"hybrid ciphertext with an extra stanza", "envelope", string(f.resign(t, encryptTo(t, f.payloadJSON(), hybridID.Recipient(), func() age.Recipient { i, _ := age.ParseHybridIdentity(f.otherIdent); return i.Recipient() }()), func(x *EnvelopeV2) { x.TargetID, x.InstanceID = "payments", "hikyo-eu" })), "payload"},
		{"non-canonical ciphertext base64 tail (stray trailing bits)", "envelope", func() string {
			d := draftV2(f.now)
			d.TargetID, d.InstanceID = "payments", "hikyo-eu"
			e, _ := ParseEnvelopeV2(f.sealUnaligned(t, d).Body)
			e.Ciphertext = withStrayBits(e.Ciphertext)
			raw, _ := json.Marshal(e, jsonwire.Stable)
			return string(raw)
		}(), "malformed"},
		{"non-canonical ed25519 signature tail (stray trailing bits)", "envelope", func() string {
			good := string(sealed.Body)
			i := strings.Index(good, `"sig_ed25519":"`) + len(`"sig_ed25519":"`)
			return good[:i] + withStrayBits(good[i:i+ed25519SigBase64]) + good[i+ed25519SigBase64:]
		}(), "malformed"},
		{"case-variant member name", "envelope", strings.Replace(string(sealed.Body), `"v":2`, `"V":2`, 1), "malformed"},
		{"upper-case duplicate of a signature member", "envelope", string(withMember(sealed.Body, `"SIG_ED25519":"`+strings.Repeat("A", ed25519SigBase64)+`"`)), "malformed"},
		{"duplicate member inside source", "envelope", strings.Replace(string(sealed.Body), `"revision":7`, `"revision":7,"revision":8`, 1), "malformed"},
		{"line break inside the ciphertext base64", "envelope", string(f.resign(t, nil, func(x *EnvelopeV2) {
			x.TargetID, x.InstanceID = "payments", "hikyo-eu"
			x.Ciphertext = x.Ciphertext[:8] + "\n" + x.Ciphertext[8:]
		})), "malformed"},
		{"ack with a case-variant member", "ack", strings.Replace(string(ack), `"status"`, `"STATUS"`, 1), "malformed"},
		{"ack with the ml-dsa signature stripped", "ack", func() string {
			a := parsedAck
			a.SigMLDSA65 = ""
			raw, _ := json.Marshal(a, jsonwire.Stable)
			return string(raw)
		}(), "malformed"},
		{"ack with a forged ed25519 signature", "ack", func() string {
			a := parsedAck
			a.SigEd25519 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 64))
			raw, _ := json.Marshal(a, jsonwire.Stable)
			return string(raw)
		}(), "signature"},
		{"ack for another envelope", "ack", string(func() []byte {
			other, _ := f.acker.SignAck("sha256:"+strings.Repeat("0", 64), e.IdempotencyKey, AckApplied, "")
			return other
		}()), "ack_mismatch"},
	}
	vf.Negative = neg
	raw, _ := json.Marshal(vf, jsonwire.Stable, jsontext.WithIndent("  "))
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vectorPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestV2PublishedVectors proves the committed vector file verifies with this
// implementation. It is the same file an independent implementation reads.
func TestV2PublishedVectors(t *testing.T) {
	raw, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatal(err)
	}
	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatal(err)
	}
	now, _ := time.Parse(time.RFC3339, vf.Now)
	senderEd, err1 := ParsePublicKey(vf.SenderEd)
	senderPQ, err2 := ParseMLDSAPublicKey(vf.SenderPQ)
	ackEd, err3 := ParsePublicKey(vf.AckEd)
	ackPQ, err4 := ParseMLDSAPublicKey(vf.AckPQ)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		t.Fatal(err)
	}
	opener, err := NewOpenerV2(vf.Identity, []PinnedSenderV2{{Ed: senderEd, PQ: senderPQ}})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := opener.Open([]byte(vf.Envelope), now)
	if err != nil || opened.Payload.Values[0].Value != plaintextMarker {
		t.Fatalf("published envelope does not open: %v", err)
	}
	e, _ := ParseEnvelopeV2([]byte(vf.Envelope))
	if hex.EncodeToString(envelopeBytesV2(&e)) != vf.EnvelopeSign {
		t.Error("envelope signed bytes drifted: any change is a protocol version bump")
	}
	a, err := VerifyAckV2([]byte(vf.Ack), ackEd, ackPQ, vf.AckDigest, e.IdempotencyKey)
	if err != nil || a.Status != AckApplied {
		t.Fatalf("published acknowledgement does not verify: %v", err)
	}
	pa, _ := ParseAckV2([]byte(vf.Ack))
	if hex.EncodeToString(ackBytesV2(&pa)) != vf.AckSign {
		t.Error("ack signed bytes drifted")
	}
	if got := Digest([]byte(vf.Envelope)); got != vf.AckDigest {
		t.Errorf("ack digest %s is not the digest of the envelope %s", vf.AckDigest, got)
	}
	d := vf.Derived
	if senderEd.IDV2() != d["sender_key_id_ed25519"] || senderPQ.ID() != d["sender_key_id_mldsa65"] || ackEd.IDV2() != d["ack_key_id_ed25519"] || ackPQ.ID() != d["ack_key_id_mldsa65"] {
		t.Error("key ids drifted")
	}
	if recipientIDV2(vf.Recipient) != d["recipient_id"] {
		t.Error("recipient id drifted")
	}
	fp, err := FingerprintV2("https://receiver.example", vf.Recipient, ackEd, ackPQ, 3)
	if err != nil || fp != d["fingerprint"] || strconv.FormatInt(DestinationIDV2(fp, "prod"), 10) != d["destination_id_namespace_prod"] {
		t.Errorf("fingerprint or destination drifted: %q %v", fp, err)
	}
	for _, n := range vf.Negative {
		t.Run(n.Name, func(t *testing.T) {
			var err error
			switch n.Kind {
			case "envelope":
				_, err = opener.Open([]byte(n.Body), now)
			case "ack":
				_, err = VerifyAckV2([]byte(n.Body), ackEd, ackPQ, vf.AckDigest, e.IdempotencyKey)
			}
			if err == nil || classOf(err) != n.Want {
				t.Fatalf("got %v (%s), want class %s", err, classOf(err), n.Want)
			}
		})
	}
}

// TestV2LargestEnvelopeFitsTheRecomputedBound seals the largest payload the
// protocol allows: 256 maximum-length names on the longer surface and a value
// sized by bisection until the age ciphertext sits at the 1 MiB limit. The
// body must fit MaxEnvelopeV2, and one more byte of plaintext must be refused.
func TestV2LargestEnvelopeFitsTheRecomputedBound(t *testing.T) {
	f := newFixtureV2(t)
	build := func(valueSize int) Draft {
		d := draftV2(f.now)
		// Every identifier and integer at its maximum, not only the names.
		d.TargetID, d.InstanceID, d.Namespace = strings.Repeat("a", 63), strings.Repeat("b", 63), strings.Repeat("c", 63)
		d.Route, d.Generation = strings.Repeat("r", 128), math.MaxInt64
		d.Source = Source{Org: strings.Repeat("o", 128), Project: strings.Repeat("p", 128), Environment: strings.Repeat("e", 128), Revision: math.MaxInt64}
		d.Names, d.Values = nil, nil
		for i := range MaxNames {
			name := "N" + strings.Repeat("X", 251) + fmt.Sprintf("%04d", i) // 256 characters
			d.Names = append(d.Names, Name{Surface: "variable", Name: name})
			v := "v"
			if i == 0 {
				v = strings.Repeat("v", valueSize)
			}
			d.Values = append(d.Values, Value{Surface: "variable", Name: name, Value: v})
		}
		return d
	}
	lo, hi := 0, MaxCiphertext
	var best Sealed
	for lo < hi { // largest value size that still seals
		mid := (lo + hi + 1) / 2
		sealed, err := f.sealer.Seal(build(mid))
		switch {
		case err == nil:
			lo, best = mid, sealed
		case errors.Is(err, ErrTooLarge):
			hi = mid - 1
		default:
			t.Fatal(err)
		}
	}
	e, err := ParseEnvelopeV2(best.Body)
	if err != nil {
		t.Fatal(err)
	}
	ct, _ := base64.StdEncoding.DecodeString(e.Ciphertext)
	if MaxCiphertext-len(ct) > 1024 || len(ct) > MaxCiphertext {
		t.Fatalf("ciphertext %d is not at the %d limit", len(ct), MaxCiphertext)
	}
	if len(best.Body) > MaxEnvelopeV2 {
		t.Fatalf("largest envelope %d exceeds MaxEnvelopeV2 %d", len(best.Body), MaxEnvelopeV2)
	}
	if _, err := f.opener.Open(best.Body, f.now); err != nil {
		t.Fatalf("largest envelope does not open: %v", err)
	}
	t.Logf("largest envelope: %d bytes of %d allowed (ciphertext %d of %d, %d names of 256 characters)", len(best.Body), MaxEnvelopeV2, len(ct), MaxCiphertext, len(e.Names))
	if _, err := f.sealer.Seal(build(lo + 2048)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize payload = %v, want ErrTooLarge", err)
	}
}

func BenchmarkV2SealOpen(b *testing.B) {
	f := newFixtureV2(b)
	d := draftV2(f.now)
	sealed, _ := f.sealer.Seal(d)
	b.Run("seal", func(b *testing.B) {
		for range b.N {
			if _, err := f.sealer.Seal(d); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("open", func(b *testing.B) {
		for range b.N {
			if _, err := f.opener.Open(sealed.Body, f.now); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Logf("one-value envelope: %d bytes", len(sealed.Body))
}

// TestV2MemberSetIsExactCaseAndBoundedBeforeDecoding covers review findings 4
// and 7: encoding/json matches member names case-insensitively and decodes
// every array element, so the exact-case member set and the names bound must be
// enforced on the token stream before the struct decode.
func TestV2MemberSetIsExactCaseAndBoundedBeforeDecoding(t *testing.T) {
	f := newFixtureV2(t)
	sealed, _ := f.sealer.Seal(draftV2(f.now))
	ack, _ := f.acker.SignAck("sha256:"+strings.Repeat("a", 64), strings.Repeat("b", 64), AckApplied, "")
	edit := func(body []byte, from, to string) []byte {
		if !strings.Contains(string(body), from) {
			t.Fatalf("fixture lacks %q", from)
		}
		return []byte(strings.Replace(string(body), from, to, 1))
	}
	good := string(sealed.Body)
	i := strings.Index(good, `"sig_ed25519":"`)
	realSig := good[i+len(`"sig_ed25519":"`) : i+len(`"sig_ed25519":"`)+ed25519SigBase64]
	cases := map[string][]byte{
		"upper-case duplicate signature":       withMember(sealed.Body, `"SIG_ED25519":"`+strings.Repeat("A", ed25519SigBase64)+`"`),
		"upper-case version member":            edit(sealed.Body, `"v":2`, `"V":2`),
		"mixed-case suite member":              edit(sealed.Body, `"suite"`, `"Suite"`),
		"upper-case nested source member":      edit(sealed.Body, `"org"`, `"ORG"`),
		"duplicate nested source member":       edit(sealed.Body, `"revision":7`, `"revision":7,"revision":8`),
		"case-variant name member":             edit(sealed.Body, `"surface"`, `"Surface"`),
		"unknown nested source member":         edit(sealed.Body, `"revision":7`, `"revision":7,"extra":1`),
		"duplicate real signature":             withMember(sealed.Body, `"sig_ed25519":"`+realSig+`"`),
		"non-canonical ed25519 signature tail": edit(sealed.Body, realSig, withStrayBits(realSig)),
		"non-canonical ciphertext base64 tail": func() []byte {
			unaligned := f.sealUnaligned(t, draftV2(f.now))
			e, _ := ParseEnvelopeV2(unaligned.Body)
			e.Ciphertext = withStrayBits(e.Ciphertext)
			raw, _ := json.Marshal(e, jsonwire.Stable)
			return raw
		}(),
		"ack upper-case member":             edit(ack, `"status"`, `"STATUS"`),
		"ack duplicate member":              withMember(ack, `"status":"applied"`),
		"ack case-variant signature member": withMember(ack, `"SIG_MLDSA65":"x"`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var err error
			if strings.HasPrefix(name, "ack") {
				_, err = ParseAckV2(body)
			} else {
				_, err = f.opener.Open(body, f.now)
			}
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("accepted or wrong class: %v", err)
			}
		})
	}

	// Names are bounded on the token stream: a body of hundreds of thousands of
	// empty name objects must not be decoded into structs first.
	var big strings.Builder
	big.WriteString(`{"v":2,"suite":"ed25519+mldsa65","names":[`)
	for big.Len() < MaxEnvelopeV2-8 {
		big.WriteString(`{},`)
	}
	big.WriteString(`{}]}`)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := ParseEnvelopeV2([]byte(big.String()))
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize names = %v", err)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 4<<20 {
		t.Fatalf("parsing %d bytes of empty names allocated %d bytes cumulatively; the bound must apply before decoding", big.Len(), grew)
	}

	// Ciphertext base64 must be canonical: no line breaks, no stray padding bits.
	e, _ := ParseEnvelopeV2(sealed.Body)
	withBreak := f.resign(t, nil, func(x *EnvelopeV2) { x.Ciphertext = x.Ciphertext[:8] + "\n" + x.Ciphertext[8:] })
	if _, err := f.opener.Open(withBreak, f.now); !errors.Is(err, ErrMalformed) {
		t.Fatalf("a line break in the ciphertext base64 opened: %v", err)
	}
	_ = e
}

func TestV2LargestAcknowledgementFitsTheMinimumResponseBound(t *testing.T) {
	f := newFixtureV2(t)
	largest := 0
	for _, c := range []struct {
		status AckStatus
		reason string
	}{{AckApplied, ""}, {AckAlreadyApplied, ""}, {AckConflict, strings.Repeat("a", 64)}, {AckRejected, strings.Repeat("z", 64)}} {
		ack, err := f.acker.SignAck("sha256:"+strings.Repeat("a", 64), strings.Repeat("b", 64), c.status, c.reason)
		if err != nil {
			t.Fatal(err)
		}
		largest = max(largest, len(ack))
	}
	t.Logf("largest legal acknowledgement: %d bytes (minimum bound %d)", largest, MinResponseBytesV2)
	if largest > MinResponseBytesV2 || largest < 4900 {
		t.Fatalf("largest ack %d vs bound %d: update the constant, the guide and the spec together", largest, MinResponseBytesV2)
	}
}
