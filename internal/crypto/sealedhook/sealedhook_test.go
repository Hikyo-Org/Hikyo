package sealedhook

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

const plaintextMarker = "s3cr3t-VALUE-never-outside-ciphertext"

func seedSigner(b byte) *Signer {
	return newSigner(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{b}, ed25519.SeedSize)))
}

type fixture struct {
	sender   *Signer
	receiver *Signer
	identity string
	sealer   *Sealer
	opener   *Opener
	now      time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	identity, recipient, err := GenerateRecipient()
	if err != nil {
		t.Fatal(err)
	}
	sender, receiver := seedSigner(1), seedSigner(2)
	sealer, err := NewSealer(recipient, sender)
	if err != nil {
		t.Fatal(err)
	}
	opener, err := NewOpener(identity, []PinnedKey{{Key: sender.Public()}})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{sender: sender, receiver: receiver, identity: identity, sealer: sealer, opener: opener, now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
}

func upsertDraft(now time.Time) Draft {
	names := []Name{{Surface: "secret", Name: "DB_PASSWORD"}}
	return Draft{
		TargetID: "payments", InstanceID: "hikyo-eu", Namespace: "prod", Route: "tgt_1", Generation: 1,
		Source: Source{Org: "org_1", Project: "prj_1", Environment: "env_1", Revision: 7},
		Op:     OpUpsert, Names: names, Binding: "binding-token",
		Values:         []Value{{Surface: "secret", Name: "DB_PASSWORD", Value: plaintextMarker}},
		IdempotencyKey: IdempotencyKey(IdempotencyParts{TargetID: "payments", Route: "tgt_1", Generation: 1, Revision: 7, Op: OpUpsert, Surface: "secret", Name: "DB_PASSWORD"}),
		IssuedAt:       now, Lifetime: time.Minute,
	}
}

func TestSealOpenRoundTripKeepsPlaintextInsideCiphertext(t *testing.T) {
	f := newFixture(t)
	sealed, err := f.sealer.Seal(upsertDraft(f.now))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed.Body, []byte(plaintextMarker)) || bytes.Contains(sealed.Body, []byte("binding-token")) {
		t.Fatal("plaintext or binding appears outside the ciphertext")
	}
	opened, err := f.opener.Open(sealed.Body, f.now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if opened.Payload.Values[0].Value != plaintextMarker || opened.Payload.Binding != "binding-token" || opened.Digest != sealed.Digest {
		t.Fatalf("round trip mismatch: %+v", opened)
	}
}

func TestOpenRefusesTamperExpiryAndUnpinnedKeys(t *testing.T) {
	f := newFixture(t)
	sealed, err := f.sealer.Seal(upsertDraft(f.now))
	if err != nil {
		t.Fatal(err)
	}
	var e Envelope
	if err := json.Unmarshal(sealed.Body, &e); err != nil {
		t.Fatal(err)
	}
	mutate := func(fn func(*Envelope)) []byte {
		c := e
		c.Names = append([]Name(nil), e.Names...)
		fn(&c)
		raw, _ := json.Marshal(c)
		return raw
	}
	cases := map[string]struct {
		body []byte
		now  time.Time
		want error
	}{
		"namespace swap":   {mutate(func(e *Envelope) { e.Namespace = "other" }), f.now, ErrSignature},
		"revision rewind":  {mutate(func(e *Envelope) { e.Source.Revision = 6 }), f.now, ErrSignature},
		"name swap":        {mutate(func(e *Envelope) { e.Names[0].Name = "OTHER" }), f.now, ErrSignature},
		"op flip":          {mutate(func(e *Envelope) { e.Op = OpPrune }), f.now, ErrSignature},
		"ciphertext flip":  {mutate(func(e *Envelope) { e.Ciphertext = append([]byte{e.Ciphertext[0] ^ 1}, e.Ciphertext[1:]...) }), f.now, ErrSignature},
		"expired":          {sealed.Body, f.now.Add(time.Minute), ErrExpired},
		"future":           {sealed.Body, f.now.Add(-time.Minute), ErrExpired},
		"unknown key":      {mutate(func(e *Envelope) { e.KeyID = seedSigner(9).Public().ID() }), f.now, ErrUnknownKey},
		"long lifetime":    {mutate(func(e *Envelope) { e.ExpiresAt = "2026-09-01T12:10:00Z" }), f.now, ErrMalformed},
		"noncanonical ts":  {mutate(func(e *Envelope) { e.IssuedAt = "2026-09-01T12:00:00.000Z" }), f.now, ErrMalformed},
		"version":          {mutate(func(e *Envelope) { e.V = 2 }), f.now, ErrMalformed},
		"unknown field":    {bytes.Replace(sealed.Body, []byte(`{"v":1`), []byte(`{"extra":1,"v":1`), 1), f.now, ErrMalformed},
		"trailing garbage": {append(append([]byte(nil), sealed.Body...), []byte(" {}")...), f.now, ErrMalformed},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := f.opener.Open(tc.body, tc.now); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRetiringSenderKeyHonorsOverlapWindow(t *testing.T) {
	f := newFixture(t)
	opener, err := NewOpener(f.identity, []PinnedKey{{Key: f.sender.Public(), NotAfter: f.now.Add(-time.Second)}})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := f.sealer.Seal(upsertDraft(f.now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opener.Open(sealed.Body, f.now); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("retired key after its overlap accepted: %v", err)
	}
	opener, _ = NewOpener(f.identity, []PinnedKey{{Key: f.sender.Public(), NotAfter: f.now.Add(time.Hour)}})
	if _, err := opener.Open(sealed.Body, f.now); err != nil {
		t.Fatalf("retiring key inside overlap refused: %v", err)
	}
}

func TestWrongRecipientCannotDecrypt(t *testing.T) {
	f := newFixture(t)
	other, _, _ := GenerateRecipient()
	opener, _ := NewOpener(other, []PinnedKey{{Key: f.sender.Public()}})
	sealed, _ := f.sealer.Seal(upsertDraft(f.now))
	if _, err := opener.Open(sealed.Body, f.now); !errors.Is(err, ErrPayload) {
		t.Fatalf("got %v", err)
	}
}

func TestSealRefusesPayloadNameMismatch(t *testing.T) {
	f := newFixture(t)
	d := upsertDraft(f.now)
	d.Values[0].Name = "OTHER"
	if _, err := f.sealer.Seal(d); !errors.Is(err, ErrPayload) {
		t.Fatalf("got %v", err)
	}
	d = upsertDraft(f.now)
	d.Lifetime = MaxLifetime + time.Second
	if _, err := f.sealer.Seal(d); !errors.Is(err, ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestAckBindsDigestIdempotencyAndPinnedKey(t *testing.T) {
	f := newFixture(t)
	sealed, _ := f.sealer.Seal(upsertDraft(f.now))
	ack, err := f.receiver.SignAck(sealed.Digest, sealed.IdempotencyKey, AckApplied, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := VerifyAck(ack, f.receiver.Public(), sealed.Digest, sealed.IdempotencyKey); err != nil || got.Status != AckApplied {
		t.Fatalf("valid ack refused: %v", err)
	}
	if _, err := VerifyAck(ack, f.sender.Public(), sealed.Digest, sealed.IdempotencyKey); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("ack from unpinned key accepted: %v", err)
	}
	if _, err := VerifyAck(ack, f.receiver.Public(), Digest([]byte("other")), sealed.IdempotencyKey); !errors.Is(err, ErrAckMismatch) {
		t.Fatalf("ack for another envelope accepted: %v", err)
	}
	var forged Ack
	_ = json.Unmarshal(ack, &forged)
	forged.Status = AckAlreadyApplied
	raw, _ := json.Marshal(forged)
	if _, err := VerifyAck(raw, f.receiver.Public(), sealed.Digest, sealed.IdempotencyKey); !errors.Is(err, ErrSignature) {
		t.Fatalf("forged status accepted: %v", err)
	}
	if _, err := f.receiver.SignAck(sealed.Digest, sealed.IdempotencyKey, AckRejected, "Echo "+plaintextMarker); !errors.Is(err, ErrMalformed) {
		t.Fatalf("free-text reason accepted: %v", err)
	}
	if _, err := VerifyAck(append(ack, bytes.Repeat([]byte(" "), MaxAck)...), f.receiver.Public(), sealed.Digest, sealed.IdempotencyKey); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized ack accepted: %v", err)
	}
}

func TestFingerprintCoversEveryTrustInput(t *testing.T) {
	_, recipient, _ := GenerateRecipient()
	_, recipient2, _ := GenerateRecipient()
	ack := seedSigner(2).Public().String()
	base, err := Fingerprint("https://r.example", recipient, ack, 1)
	if err != nil {
		t.Fatal(err)
	}
	variants := []func() (string, error){
		func() (string, error) { return Fingerprint("https://r2.example", recipient, ack, 1) },
		func() (string, error) { return Fingerprint("https://r.example", recipient2, ack, 1) },
		func() (string, error) {
			return Fingerprint("https://r.example", recipient, seedSigner(3).Public().String(), 1)
		},
		func() (string, error) { return Fingerprint("https://r.example", recipient, ack, 2) },
	}
	for i, v := range variants {
		got, err := v()
		if err != nil || got == base {
			t.Fatalf("variant %d did not move the fingerprint: %v", i, err)
		}
	}
	if DestinationID(base, "a") == DestinationID(base, "b") || DestinationID(base, "a") <= 0 {
		t.Fatal("destination id must be positive and namespace-bound")
	}
}

// TestKnownAnswerVector freezes the signed-byte encoding, the key id, and the
// derived identities. Any change here is a protocol version bump.
func TestKnownAnswerVector(t *testing.T) {
	signer := seedSigner(7)
	e := Envelope{
		V: 1, TargetID: "payments", InstanceID: "hikyo-eu", Namespace: "prod", Route: "tgt_1", Generation: 3,
		Source: Source{Org: "org_1", Project: "prj_1", Environment: "env_1", Revision: 42},
		Op:     OpUpsert, Names: []Name{{Surface: "secret", Name: "A"}, {Surface: "variable", Name: "B"}},
		Ciphertext: []byte("fixed-ciphertext"), IdempotencyKey: strings.Repeat("ab", 32),
		IssuedAt: "2026-01-02T03:04:05Z", ExpiresAt: "2026-01-02T03:09:05Z", KeyID: signer.Public().ID(),
	}
	sig := ed25519.Sign(signer.key, envelopeBytes(&e))
	got := map[string]string{
		"public":      signer.Public().String(),
		"key_id":      signer.Public().ID(),
		"envelope":    lp(envelopeBytes(&e)).hexSum(),
		"sig":         hex.EncodeToString(sig),
		"idempotency": IdempotencyKey(IdempotencyParts{TargetID: "payments", Fingerprint: "sha256:00", Route: "tgt_1", Generation: 3, Revision: 42, Op: OpUpsert, Surface: "secret", Name: "A"}),
		"destination": strconv.FormatInt(DestinationID("sha256:00", "prod"), 10),
	}
	for k, v := range got {
		if want := knownAnswers[k]; v != want {
			t.Errorf("%s = %q, want frozen %q", k, v, want)
		}
	}
}

func FuzzParseEnvelope(f *testing.F) {
	fx := newFixture(&testing.T{})
	sealed, _ := fx.sealer.Seal(upsertDraft(fx.now))
	f.Add(sealed.Body)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		e, err := ParseEnvelope(raw)
		if err != nil {
			return
		}
		if err := validateEnvelope(&e); err != nil {
			t.Fatalf("accepted envelope fails revalidation: %v", err)
		}
		_, _ = fx.opener.Open(raw, fx.now)
	})
}

func FuzzParseAck(f *testing.F) {
	s := seedSigner(2)
	ack, _ := s.SignAck("sha256:"+strings.Repeat("0", 64), strings.Repeat("1", 64), AckConflict, "exists_unowned")
	f.Add(ack)
	f.Add([]byte(`{"v":1}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		a, err := ParseAck(raw)
		if err != nil {
			return
		}
		if err := validateAck(&a); err != nil {
			t.Fatalf("accepted ack fails revalidation: %v", err)
		}
		_, _ = VerifyAck(raw, s.Public(), a.EnvelopeDigest, a.IdempotencyKey)
	})
}

var knownAnswers = map[string]string{
	"public":      "ed25519:ea4a6c63e29c520abef5507b132ec5f9954776aebebe7b92421eea691446d22c",
	"key_id":      "sha256:fe812c12f3ab4ce6ac5db69ac352f906cb1b11ef43fb33e252ef7ff552263889",
	"envelope":    "ce90cf47440e4ed660b473e5140a6e784b512ede496906fbf6786216ac7b1c52",
	"sig":         "f215e4835581dbb0b1340dd59ac3e30dbbad5b0ae250b378b00abb584e09e99ad9cbd2a3451b7c98db2a67dac048051ee0aedf1dd8be9649a3aa5ea61cae8f0d",
	"idempotency": "bbd01e64964b3a1dddc3449c131189d5e7d024585e7572691674ea22c4725440",
	"destination": "889807860836640539",
}
