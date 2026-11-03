package sealedwebhook

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
	"github.com/Hikyo-Org/hikyo/internal/sealedreceiver"
)

// Protocol 2 (docs/spec/sealed-webhook.md section 11) end to end: the real
// sender module, client, egress dialer and TLS against the in-tree reference
// receiver, with a fake journal standing in for the service fences (the real
// service, outbox and stores are driven by internal/isolation).

// v2Keys is everything protocol 2 adds to the version 1 harness.
type v2Keys struct {
	identity, recip string
	senderPQ        *sealedhook.MLDSASigner
	ackPQ           *sealedhook.MLDSASigner
	ackPQPub        sealedhook.MLDSAPublicKey
	acker           *sealedhook.SignerV2
}

// serves selects which protocol versions the receiver speaks.
type serves int

const (
	servesV1 serves = 1 << iota
	servesV2
	servesBoth = servesV1 | servesV2
)

func mustMLDSA(t testing.TB) *sealedhook.MLDSASigner {
	t.Helper()
	p, _, err := sealedhook.GenerateMLDSASigningKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := sealedhook.ParseMLDSASigningKeyPEM(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// newHarnessV2 builds a protocol 2 sender against a receiver that serves the
// selected versions. The version 1 keys always exist so a skew case can build
// a version 1 sender against the same receiver.
func newHarnessV2(t *testing.T, which serves) *harness {
	t.Helper()
	h := &harness{t: t, k: &v2Keys{}}
	var err error
	h.identity, h.recip, err = sealedhook.GenerateRecipient()
	if err != nil {
		t.Fatal(err)
	}
	signerPEM, _, _ := sealedhook.GenerateSigningKey()
	h.signer, _ = sealedhook.ParseSigningKeyPEM(signerPEM)
	ackPEM, ackPubText, _ := sealedhook.GenerateSigningKey()
	h.ackKey, _ = sealedhook.ParseSigningKeyPEM(ackPEM)
	h.ackPub, _ = sealedhook.ParsePublicKey(ackPubText)
	k := h.k
	if k.identity, k.recip, err = sealedhook.GenerateHybridRecipient(); err != nil {
		t.Fatal(err)
	}
	k.senderPQ, k.ackPQ = mustMLDSA(t), mustMLDSA(t)
	k.ackPQPub = k.ackPQ.Public()
	if k.acker, err = sealedhook.NewSignerV2(h.ackKey, k.ackPQ); err != nil {
		t.Fatal(err)
	}
	h.receiver = h.newReceiver(which, nil)
	h.startServer()
	h.endpoint = h.newEndpointV2(1, h.signer, k.senderPQ)
	h.module = h.newModule(h.endpoint, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, staticResolver{netip.MustParseAddr("127.0.0.1")})
	return h
}

// newReceiver builds a reference receiver from the harness keys.
func (h *harness) newReceiver(which serves, edit func(*sealedreceiver.Config)) *sealedreceiver.Receiver {
	cfg := sealedreceiver.Config{TargetID: "payments", InstanceID: "hikyo-test", Generation: 1, Bindings: map[string]string{namespace: binding}}
	if which&servesV1 != 0 {
		cfg.Identity, cfg.AckSigner, cfg.Senders = h.identity, h.ackKey, []sealedhook.PinnedKey{{Key: h.signer.Public()}}
	}
	if which&servesV2 != 0 {
		cfg.HybridIdentity, cfg.AckSignerV2 = h.k.identity, h.k.acker
		cfg.SendersV2 = []sealedhook.PinnedSenderV2{{Ed: h.signer.Public(), PQ: h.k.senderPQ.Public()}}
	}
	if edit != nil {
		edit(&cfg)
	}
	r, err := sealedreceiver.New(cfg)
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

func (h *harness) newEndpointV2(generation int64, ed *sealedhook.Signer, pq *sealedhook.MLDSASigner) *Endpoint {
	fp, err := sealedhook.FingerprintV2(h.origin, h.k.recip, h.ackPub, h.k.ackPQPub, generation)
	if err != nil {
		h.t.Fatal(err)
	}
	e, err := NewEndpoint(EndpointConfig{
		ID: "payments", InstanceID: "hikyo-test", Origin: h.origin, RecipientKey: h.k.recip, AckKey: h.ackPub.String(),
		AckKeyMLDSA65: h.k.ackPQPub.String(), Generation: generation, Timeout: 5 * time.Second, MaxResponseBytes: 8192,
		ConfirmedFingerprint: fp, Signer: ed, SignerPQ: pq, Protocol: 2,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return e
}

func (h *harness) paths() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.log {
		out = append(out, r.url)
	}
	return out
}

func idemOfV2(t *testing.T, body []byte) string {
	t.Helper()
	e, err := sealedhook.ParseEnvelopeV2(body)
	if err != nil {
		t.Fatal(err)
	}
	return e.IdempotencyKey
}

func gate(context.Context) error { return nil }

func TestV2EndToEndConvergeUpdatePruneAndTeardown(t *testing.T) {
	h := newHarnessV2(t, servesV2)
	conn, err := h.module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: gate})
	if err != nil || conn.DestinationID != h.target().Destination.NumericID || conn.Version != "sealed-webhook/v2" {
		t.Fatalf("TestConnection = %+v, %v", conn, err)
	}
	j := newJournal()
	result, err := h.sync(j, 1, manifest(marker), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"secret/DB_PASSWORD", "secret/MANAGED_BY_HIKYO", "variable/LOG_LEVEL", "variable/MANAGED_BY_HIKYO"}; !slices.Equal(h.receiver.Keys(namespace), want) {
		t.Fatalf("receiver keys = %v", h.receiver.Keys(namespace))
	}
	if h.receiver.Snapshot(namespace)["secret/DB_PASSWORD"].Value != marker {
		t.Fatal("receiver did not decrypt the exact value")
	}
	for _, p := range h.paths() {
		if p != sealedhook.SyncPathV2 {
			t.Fatalf("a request left the v2 route: %s", p)
		}
	}
	h.mu.Lock()
	for _, r := range h.log {
		if r.header.Get("Content-Type") != sealedhook.ContentTypeV2 || r.header.Get("Accept") != sealedhook.AckContentTypeV2 {
			t.Fatalf("headers = %v", r.header)
		}
		if ack, err := sealedhook.ParseAckV2(r.resp); err != nil || ack.Status != sealedhook.AckApplied {
			t.Fatalf("receiver answered %q (%v)", r.resp, err)
		}
	}
	h.mu.Unlock()
	if j.prepared[0] != "secret:MANAGED_BY_HIKYO" || j.prepared[1] != "variable:MANAGED_BY_HIKYO" {
		t.Fatalf("sentinels must be written first: %v", j.prepared)
	}
	if _, err := h.sync(j, 2, manifest(marker+"-v2"), false); err != nil || h.receiver.Snapshot(namespace)["secret/DB_PASSWORD"].Value != marker+"-v2" {
		t.Fatalf("update did not converge: %v", err)
	}
	if _, err := h.sync(j, 3, manifest(marker)[:1], false); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.receiver.Snapshot(namespace)["variable/LOG_LEVEL"]; ok {
		t.Fatal("ledger-owned name was not pruned")
	}
	j.prepared = nil
	teardown, err := h.sync(j, 3, nil, true)
	if err != nil || len(h.receiver.Keys(namespace)) != 0 || len(j.states) != 0 {
		t.Fatalf("teardown left %v / %v (%v)", h.receiver.Keys(namespace), j.states, err)
	}
	h.assertNoPlaintext(j, result, teardown)
}

func TestV2CrashWindowReplayReusesIdempotencyKey(t *testing.T) {
	h := newHarnessV2(t, servesV2)
	crashes := 1
	h.receiver.SetAfterApply(func(w http.ResponseWriter) bool {
		if crashes == 0 {
			return true
		}
		crashes--
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}
		return false
	})
	j := newJournal()
	if _, err := h.sync(j, 1, manifest(marker), false); !errors.Is(err, adapter.ErrIndeterminate) {
		t.Fatalf("crash window error = %v, want indeterminate", err)
	}
	if c := j.last(); c.Outcome != adapter.OutcomeUnknown || c.State != adapter.Dispatched {
		t.Fatalf("crash window completion = %+v, want unknown/dispatched", c)
	}
	first := idemOfV2(t, h.log[0].body)
	if _, err := h.sync(j, 1, manifest(marker), false); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if idemOfV2(t, h.log[1].body) != first {
		t.Fatal("retry did not reuse the idempotency key")
	}
	if ack, _ := sealedhook.ParseAckV2(h.log[1].resp); ack.Status != sealedhook.AckAlreadyApplied {
		t.Fatalf("replayed effect ack = %q, want already_applied", ack.Status)
	}
	if j.states["secret:MANAGED_BY_HIKYO"] != adapter.Owned {
		t.Fatal("already_applied must advance the cursor to owned")
	}
	h.assertNoPlaintext(j)
}

// respond runs the real receiver and returns its acknowledgement body.
func respond(h *harness, body []byte) []byte {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, sealedhook.SyncPathV2, bytes.NewReader(body))
	req.Header.Set("Content-Type", sealedhook.ContentTypeV2)
	h.receiver.ServeHTTP(rec, req)
	return rec.Body.Bytes()
}

func editAck(t *testing.T, raw []byte, edit func(*sealedhook.AckV2)) []byte {
	t.Helper()
	var a sealedhook.AckV2
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatalf("receiver answer %q: %v", raw, err)
	}
	edit(&a)
	out, _ := json.Marshal(a)
	return out
}

func TestV2ForgedAndStrippedAcknowledgements(t *testing.T) {
	forged := map[string]func(h *harness, body []byte) []byte{
		"wrong ml-dsa key": func(h *harness, body []byte) []byte {
			other, _ := sealedhook.NewSignerV2(h.ackKey, mustMLDSA(t))
			ack, _ := other.SignAck(sealedhook.Digest(body), idemOfV2(t, body), sealedhook.AckApplied, "")
			return ack
		},
		"wrong ed25519 key": func(h *harness, body []byte) []byte {
			rogue, _ := sealedhook.ParseSigningKeyPEM(mustPEM(t))
			other, _ := sealedhook.NewSignerV2(rogue, h.k.ackPQ)
			ack, _ := other.SignAck(sealedhook.Digest(body), idemOfV2(t, body), sealedhook.AckApplied, "")
			return ack
		},
		"classical forger: valid ed25519, forged ml-dsa": func(h *harness, body []byte) []byte {
			return editAck(t, respond(h, body), func(a *sealedhook.AckV2) {
				a.SigMLDSA65 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, sealedhook.MLDSA65SignatureSize))
			})
		},
		"tampered status": func(h *harness, body []byte) []byte {
			return bytes.Replace(respond(h, body), []byte(`"status":"applied"`), []byte(`"status":"already_applied"`), 1)
		},
		"wrong digest": func(h *harness, body []byte) []byte {
			ack, _ := h.k.acker.SignAck(sealedhook.Digest([]byte("x")), idemOfV2(t, body), sealedhook.AckApplied, "")
			return ack
		},
		"wrong idempotency key": func(h *harness, body []byte) []byte {
			ack, _ := h.k.acker.SignAck(sealedhook.Digest(body), strings.Repeat("0", 64), sealedhook.AckApplied, "")
			return ack
		},
	}
	for name, forge := range forged {
		t.Run("forged/"+name, func(t *testing.T) {
			h := newHarnessV2(t, servesV2)
			h.override = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", sealedhook.AckContentTypeV2)
				_, _ = w.Write(forge(h, body))
			})
			j := newJournal()
			_, err := h.sync(j, 1, manifest(marker), false)
			if !errors.Is(err, adapter.ErrAckForged) {
				t.Fatalf("error = %v, want forged ack", err)
			}
			if c := j.last(); c.Outcome != adapter.OutcomeFailure || c.State != adapter.Dispatched {
				t.Fatalf("completion = %+v, want failure with ownership kept", c)
			}
			if !adapter.ClassifyError(err).NeedsAttention() {
				t.Fatal("forged ack must need attention")
			}
		})
	}
	// A message that is not even well formed is never a success either; it is
	// ambiguous, so the effect stays unknown and is retried as version 2.
	stripped := map[string]func(h *harness, body []byte) []byte{
		"ml-dsa signature stripped": func(h *harness, body []byte) []byte {
			return editAck(t, respond(h, body), func(a *sealedhook.AckV2) { a.SigMLDSA65 = "" })
		},
		"ed25519 signature stripped": func(h *harness, body []byte) []byte {
			return editAck(t, respond(h, body), func(a *sealedhook.AckV2) { a.SigEd25519 = "" })
		},
		"suite stripped to ed25519": func(h *harness, body []byte) []byte {
			return editAck(t, respond(h, body), func(a *sealedhook.AckV2) { a.Suite = "ed25519" })
		},
		"version 1 acknowledgement": func(h *harness, body []byte) []byte {
			ack, _ := h.ackKey.SignAck(sealedhook.Digest(body), idemOfV2(t, body), sealedhook.AckApplied, "")
			return ack
		},
	}
	for name, strip := range stripped {
		t.Run("stripped/"+name, func(t *testing.T) {
			h := newHarnessV2(t, servesV2)
			h.override = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", sealedhook.AckContentTypeV2)
				_, _ = w.Write(strip(h, body))
			})
			j := newJournal()
			_, err := h.sync(j, 1, manifest(marker), false)
			if !errors.Is(err, adapter.ErrIndeterminate) || errors.Is(err, adapter.ErrAckForged) {
				t.Fatalf("error = %v, want indeterminate no-ack", err)
			}
			if c := j.last(); c.Outcome != adapter.OutcomeUnknown || c.State != adapter.Dispatched {
				t.Fatalf("completion = %+v, want unknown/dispatched", c)
			}
		})
	}
}

func TestV2AmbiguousResponsesStayUnknownAndNeverDowngrade(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"version 1 only receiver": nil,
		"server error":            func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"redirect to the v1 path": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, sealedhook.SyncPath, http.StatusTemporaryRedirect)
		},
		"v1 media type": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", sealedhook.AckContentType)
			_, _ = w.Write([]byte(`{"v":1,"status":"applied"}`))
		},
		"oversized": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", sealedhook.AckContentTypeV2)
			_, _ = w.Write(bytes.Repeat([]byte("a"), 16384))
		},
	} {
		t.Run(name, func(t *testing.T) {
			which := servesV2
			if handler == nil {
				which = servesV1 // a receiver that was never upgraded
			}
			h := newHarnessV2(t, which)
			h.override = handler
			j := newJournal()
			for attempt := range 2 {
				_, err := h.sync(j, 1, manifest(marker), false)
				if !errors.Is(err, adapter.ErrIndeterminate) || !errors.Is(err, ErrNoAck) {
					t.Fatalf("attempt %d error = %v, want indeterminate no-ack", attempt, err)
				}
				if c := j.last(); c.Outcome != adapter.OutcomeUnknown {
					t.Fatalf("completion = %+v, want unknown", c)
				}
			}
			if got := h.paths(); len(got) != 2 || got[0] != sealedhook.SyncPathV2 || got[1] != sealedhook.SyncPathV2 {
				t.Fatalf("requests = %v: a protocol 2 target must send only version 2 requests, once per attempt", got)
			}
			if len(h.receiver.Keys(namespace)) != 0 {
				t.Fatal("the receiver applied something")
			}
		})
	}
}

// TestV2VersionSkewMatrix is the sender by receiver matrix of the rollout.
func TestV2VersionSkewMatrix(t *testing.T) {
	cases := []struct {
		name     string
		receiver serves
		sender   int
		applies  bool
	}{
		{"v1 sender, v1-only receiver (today)", servesV1, 1, true},
		{"v1 sender, upgraded receiver serving both (reader-first)", servesBoth, 1, true},
		{"v2 sender, upgraded receiver serving both", servesBoth, 2, true},
		{"v2 sender, PQ-required receiver", servesV2, 2, true},
		{"v2 sender, v1-only receiver (sender moved too early)", servesV1, 2, false},
		{"v1 sender, PQ-required receiver (retired v1)", servesV2, 1, false},
		{"rollback: v1 sender again, receiver still serving both", servesBoth, 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarnessV2(t, tc.receiver)
			if tc.sender == 1 {
				h.endpoint = h.newEndpoint(1, h.signer)
				h.module = h.newModule(h.endpoint, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, staticResolver{netip.MustParseAddr("127.0.0.1")})
			}
			j := newJournal()
			_, err := h.sync(j, 1, manifest(marker), false)
			if tc.applies {
				if err != nil || h.receiver.Snapshot(namespace)["secret/DB_PASSWORD"].Value != marker {
					t.Fatalf("sync = %v, receiver %v", err, h.receiver.Keys(namespace))
				}
				return
			}
			if !errors.Is(err, adapter.ErrIndeterminate) || len(h.receiver.Keys(namespace)) != 0 {
				t.Fatalf("sync = %v, receiver %v: skew must leave the effect unknown and apply nothing", err, h.receiver.Keys(namespace))
			}
			want := sealedhook.SyncPath
			if tc.sender == 2 {
				want = sealedhook.SyncPathV2
			}
			for _, p := range h.paths() {
				if p != want {
					t.Fatalf("request to %s, want only %s", p, want)
				}
			}
		})
	}
}

func TestV2ReceiverRefusesWithoutAcknowledgement(t *testing.T) {
	h := newHarnessV2(t, servesV2)
	j := newJournal()
	if _, err := h.sync(j, 1, manifest(marker), false); err != nil {
		t.Fatal(err)
	}
	captured := h.log[2].body
	before := h.receiver.Snapshot(namespace)
	rec := func(path, ctype string, body []byte, r *sealedreceiver.Receiver) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", ctype)
		r.ServeHTTP(w, req)
		return w
	}
	// Replay inside the window: acknowledged as already applied, no double effect.
	ack, err := sealedhook.VerifyAckV2(rec(sealedhook.SyncPathV2, sealedhook.ContentTypeV2, captured, h.receiver).Body.Bytes(), h.ackPub, h.k.ackPQPub, sealedhook.Digest(captured), idemOfV2(t, captured))
	if err != nil || ack.Status != sealedhook.AckAlreadyApplied {
		t.Fatalf("replay ack = %+v, %v", ack, err)
	}
	if len(h.receiver.Snapshot(namespace)) != len(before) {
		t.Fatal("replay changed receiver state")
	}
	var envelope map[string]any
	_ = json.Unmarshal(captured, &envelope)
	mutated := func(edit func(map[string]any)) []byte {
		m := map[string]any{}
		for k, v := range envelope {
			m[k] = v
		}
		edit(m)
		raw, _ := json.Marshal(m)
		return raw
	}
	late := h.newReceiver(servesV2, func(c *sealedreceiver.Config) {
		c.Now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	})
	oversize := bytes.Repeat([]byte(" "), sealedhook.MaxEnvelopeV2+1)
	for name, tc := range map[string]struct {
		path, ctype string
		body        []byte
		r           *sealedreceiver.Receiver
		want        int
	}{
		"expired":                   {sealedhook.SyncPathV2, sealedhook.ContentTypeV2, captured, late, http.StatusBadRequest},
		"ml-dsa signature missing":  {sealedhook.SyncPathV2, sealedhook.ContentTypeV2, mutated(func(m map[string]any) { delete(m, "sig_mldsa65") }), h.receiver, http.StatusBadRequest},
		"ed25519 signature missing": {sealedhook.SyncPathV2, sealedhook.ContentTypeV2, mutated(func(m map[string]any) { delete(m, "sig_ed25519") }), h.receiver, http.StatusBadRequest},
		"suite downgraded":          {sealedhook.SyncPathV2, sealedhook.ContentTypeV2, mutated(func(m map[string]any) { m["suite"] = "ed25519" }), h.receiver, http.StatusBadRequest},
		"route tampered":            {sealedhook.SyncPathV2, sealedhook.ContentTypeV2, mutated(func(m map[string]any) { m["route"] = "tgt_9" }), h.receiver, http.StatusBadRequest},
		"v1 media type on v2":       {sealedhook.SyncPathV2, sealedhook.ContentType, captured, h.receiver, http.StatusUnsupportedMediaType},
		"v2 body on the v1 path":    {sealedhook.SyncPath, sealedhook.ContentType, captured, h.receiver, http.StatusNotFound},
		"oversize body":             {sealedhook.SyncPathV2, sealedhook.ContentTypeV2, oversize, h.receiver, http.StatusRequestEntityTooLarge},
		"garbage":                   {sealedhook.SyncPathV2, sealedhook.ContentTypeV2, []byte("not json"), h.receiver, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			w := rec(tc.path, tc.ctype, tc.body, tc.r)
			if w.Code != tc.want || strings.Contains(w.Body.String(), "status") {
				t.Fatalf("status %d body %q, want %d and no acknowledgement", w.Code, w.Body.String(), tc.want)
			}
		})
	}
	// A declared length over the bound is refused before any read.
	req := httptest.NewRequest(http.MethodPost, sealedhook.SyncPathV2, io.LimitReader(zeros{}, 1))
	req.ContentLength = sealedhook.MaxEnvelopeV2 + 1
	req.Header.Set("Content-Type", sealedhook.ContentTypeV2)
	w := httptest.NewRecorder()
	h.receiver.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared oversize status = %d", w.Code)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestV2SenderPairRotationOverlapAndRetirement(t *testing.T) {
	h := newHarnessV2(t, servesV2)
	nextPEM, nextEd := mustPEM(t), (*sealedhook.Signer)(nil)
	nextEd, _ = sealedhook.ParseSigningKeyPEM(nextPEM)
	nextPQ := mustMLDSA(t)
	retiring := time.Now().Add(-time.Second)
	// The receiver pins the new pair and retires the old one: a pair rotates
	// as a unit, so a half-rotated pair matches nothing.
	h.override = h.newReceiver(servesV2, func(c *sealedreceiver.Config) {
		c.SendersV2 = []sealedhook.PinnedSenderV2{
			{Ed: h.signer.Public(), PQ: h.k.senderPQ.Public(), NotAfter: retiring},
			{Ed: nextEd.Public(), PQ: nextPQ.Public()},
		}
	})
	probe := func(m *Module) error {
		_, err := m.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: gate})
		return err
	}
	with := func(ed *sealedhook.Signer, pq *sealedhook.MLDSASigner) *Module {
		return &Module{API: h.module.API, Endpoint: h.newEndpointV2(1, ed, pq), Binding: binding}
	}
	if err := probe(h.module); !errors.Is(err, ErrNoAck) {
		t.Fatalf("retired pair accepted after overlap (and key files restored from backup revive nothing): %v", err)
	}
	if err := probe(with(nextEd, nextPQ)); err != nil {
		t.Fatalf("new pair refused: %v", err)
	}
	for name, m := range map[string]*Module{
		"new ed25519 with the retired ml-dsa key": with(nextEd, h.k.senderPQ),
		"retired ed25519 with the new ml-dsa key": with(h.signer, nextPQ),
	} {
		if err := probe(m); !errors.Is(err, ErrNoAck) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	// Overlap: both pairs verify while the old pair's window is open.
	h.override = h.newReceiver(servesV2, func(c *sealedreceiver.Config) {
		c.SendersV2 = []sealedhook.PinnedSenderV2{
			{Ed: h.signer.Public(), PQ: h.k.senderPQ.Public(), NotAfter: time.Now().Add(time.Hour)},
			{Ed: nextEd.Public(), PQ: nextPQ.Public()},
		}
	})
	if err := probe(h.module); err != nil {
		t.Fatalf("old pair inside its overlap window refused: %v", err)
	}
}

func TestV2TrustBoundaryChangesFailClosedBeforeAnyRequest(t *testing.T) {
	h := newHarnessV2(t, servesBoth)
	boundTo := func(endpoint *Endpoint) adapter.Target {
		return adapter.Target{ID: "tgt_1", Generation: 1, Destination: adapter.Destination{Kind: adapter.Organization, Owner: namespace, NumericID: endpoint.destinationID(namespace)}}
	}
	v1Endpoint := h.newEndpoint(1, h.signer)
	v2Endpoint := h.endpoint
	cases := map[string]struct {
		registry *Endpoint // what the operator registry holds now
		bound    adapter.Target
	}{
		"bound under v1, registry moved to v2 (restored database, new registry)": {v2Endpoint, boundTo(v1Endpoint)},
		"bound under v2, registry rolled back to v1 (restored registry)":         {v1Endpoint, boundTo(v2Endpoint)},
		"bound under v2 generation 1, registry at generation 2":                  {h.newEndpointV2(2, h.signer, h.k.senderPQ), boundTo(v2Endpoint)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			before := len(h.paths())
			m := &Module{API: h.module.API, Endpoint: tc.registry, Binding: binding}
			j := newJournal()
			_, err := m.Sync(t.Context(), adapter.SyncRequest{Target: tc.bound, Manifest: manifest(marker), Source: source(1), Ledger: j.ledger()}, j)
			if !errors.Is(err, adapter.ErrDestinationID) || len(h.paths()) != before || len(j.prepared) != 0 {
				t.Fatalf("error = %v, requests %d -> %d: a target bound under another boundary must fail closed with no request", err, before, len(h.paths()))
			}
		})
	}
	if v1Endpoint.Fingerprint() == v2Endpoint.Fingerprint() || v1Endpoint.destinationID(namespace) == v2Endpoint.destinationID(namespace) {
		t.Fatal("one origin must derive different fingerprints and destinations per protocol")
	}
}

func TestV2EndpointActivationRequiresConfirmedFingerprintAndConsistentMaterial(t *testing.T) {
	h := newHarnessV2(t, servesV2)
	base := EndpointConfig{
		ID: "payments", InstanceID: "hikyo-test", Origin: h.origin, RecipientKey: h.k.recip, AckKey: h.ackPub.String(), AckKeyMLDSA65: h.k.ackPQPub.String(),
		Generation: 1, Timeout: time.Second, MaxResponseBytes: 1024, ConfirmedFingerprint: h.endpoint.Fingerprint(), Signer: h.signer, SignerPQ: h.k.senderPQ, Protocol: 2,
	}
	if _, err := NewEndpoint(base); err != nil {
		t.Fatal(err)
	}
	_, x25519, _ := sealedhook.GenerateRecipient()
	_, otherHybrid, _ := sealedhook.GenerateHybridRecipient()
	v1fp, _ := sealedhook.Fingerprint(h.origin, x25519, h.ackPub.String(), 1)
	for name, mutate := range map[string]func(*EndpointConfig){
		"no confirmation":              func(c *EndpointConfig) { c.ConfirmedFingerprint = "" },
		"v1 fingerprint on v2":         func(c *EndpointConfig) { c.ConfirmedFingerprint = v1fp },
		"recipient swapped":            func(c *EndpointConfig) { c.RecipientKey = otherHybrid },
		"classical recipient":          func(c *EndpointConfig) { c.RecipientKey = x25519 },
		"ml-dsa ack key swapped":       func(c *EndpointConfig) { c.AckKeyMLDSA65 = mustMLDSA(t).Public().String() },
		"ml-dsa ack key missing":       func(c *EndpointConfig) { c.AckKeyMLDSA65 = "" },
		"ml-dsa ack key malformed":     func(c *EndpointConfig) { c.AckKeyMLDSA65 = c.AckKeyMLDSA65[:20] },
		"ml-dsa signer missing":        func(c *EndpointConfig) { c.SignerPQ = nil },
		"ed25519 signer missing":       func(c *EndpointConfig) { c.Signer = nil },
		"generation":                   func(c *EndpointConfig) { c.Generation = 2 },
		"host change":                  func(c *EndpointConfig) { c.Origin = "https://attacker.example" },
		"unknown protocol":             func(c *EndpointConfig) { c.Protocol = 3 },
		"protocol 1 with v2 key":       func(c *EndpointConfig) { c.Protocol = 1 },
		"protocol 1, hybrid recipient": func(c *EndpointConfig) { c.Protocol, c.SignerPQ, c.AckKeyMLDSA65 = 1, nil, "" },
	} {
		t.Run(name, func(t *testing.T) {
			c := base
			mutate(&c)
			if _, err := NewEndpoint(c); err == nil {
				t.Fatal("endpoint activated")
			}
		})
	}
	// Client construction refuses a half-configured protocol too.
	cfg := h.endpoint.ClientConfig()
	for name, mutate := range map[string]func(*ClientConfig){
		"protocol 2 without the ml-dsa ack key": func(c *ClientConfig) { c.AckKeyMLDSA65 = sealedhook.MLDSAPublicKey{} },
		"protocol 1 with the ml-dsa ack key":    func(c *ClientConfig) { c.Protocol = 1 },
		"protocol 3":                            func(c *ClientConfig) { c.Protocol = 3 },
	} {
		t.Run("client/"+name, func(t *testing.T) {
			c := cfg
			mutate(&c)
			if _, err := NewClient(c); err == nil {
				t.Fatal("client built")
			}
		})
	}
}

func TestV2BindingConflictAndFencedWorker(t *testing.T) {
	h := newHarnessV2(t, servesV2)
	h.module.Binding = "not-the-binding"
	if _, err := h.module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: gate}); !errors.Is(err, adapter.ErrProviderAuth) {
		t.Fatalf("binding refusal = %v, want provider auth", err)
	}
	h = newHarnessV2(t, servesV2)
	h.receiver.Seed(namespace, "secret", "DB_PASSWORD", "theirs")
	j := newJournal()
	result, err := h.sync(j, 1, manifest(marker), false)
	if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 || h.receiver.Snapshot(namespace)["secret/DB_PASSWORD"].Value != "theirs" {
		t.Fatalf("conflict = %v %v", err, result.Conflicts)
	}
	h = newHarnessV2(t, servesV2)
	j = newJournal()
	j.gate = func(adapter.Effect) error { return adapter.ErrSuperseded }
	if _, err := h.sync(j, 1, manifest(marker), false); !errors.Is(err, adapter.ErrSuperseded) || len(h.paths()) != 0 {
		t.Fatalf("fenced worker: %v, %d requests", err, len(h.paths()))
	}
}

func TestV2ReceiverPersistFailureRollsBackAndRetriesWithTheSameKey(t *testing.T) {
	h := newHarnessV2(t, servesV2)
	h.receiver.SetPersist(func(sealedreceiver.State) error { return errors.New("disk full") })
	j := newJournal()
	if _, err := h.sync(j, 1, manifest(marker), false); !errors.Is(err, adapter.ErrIndeterminate) {
		t.Fatalf("persist failure error = %v, want indeterminate", err)
	}
	if got := h.receiver.Keys(namespace); len(got) != 0 {
		t.Fatalf("receiver kept unpersisted values: %v", got)
	}
	h.receiver.SetPersist(nil)
	if _, err := h.sync(j, 1, manifest(marker), false); err != nil {
		t.Fatalf("retry after recovery: %v", err)
	}
	if idemOfV2(t, h.log[0].body) != idemOfV2(t, h.log[1].body) {
		t.Fatal("retry did not reuse the idempotency key")
	}
}

func TestV2EgressPolicyStillRefusesNonPublicDestinations(t *testing.T) {
	h := newHarnessV2(t, servesV2)
	for _, addr := range []string{"127.0.0.1", "10.0.0.5", "169.254.169.254", "fd00::1"} {
		m := h.newModule(h.endpoint, nil, staticResolver{netip.MustParseAddr(addr)})
		if _, err := m.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: gate}); !errors.Is(err, ErrNoAck) {
			t.Fatalf("%s: error = %v, want a non-public refusal", addr, err)
		}
	}
	if len(h.paths()) != 0 {
		t.Fatal("refused egress reached the receiver")
	}
}
