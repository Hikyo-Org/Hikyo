package sealedwebhook

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
	"github.com/Hikyo-Org/hikyo/internal/sealedreceiver"
)

const (
	marker    = "PLAINTEXT-canary-7f3e"
	binding   = "binding-credential-canary-19ab"
	namespace = "prod"
)

// ---- fake journal --------------------------------------------------------

type fakeJournal struct {
	mu          sync.Mutex
	states      map[string]adapter.LedgerState
	completions []adapter.Completion
	prepared    []string
	gate        func(adapter.Effect) error
}

func newJournal() *fakeJournal { return &fakeJournal{states: map[string]adapter.LedgerState{}} }

func jkey(e adapter.Effect) string { return string(e.Surface) + ":" + e.EffectiveName }

func (j *fakeJournal) Gate(_ context.Context, e adapter.Effect) error {
	if j.gate != nil {
		return j.gate(e)
	}
	return nil
}
func (j *fakeJournal) Reserve(_ context.Context, e adapter.Effect) (adapter.LedgerState, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.states[jkey(e)] = adapter.Reserved
	return adapter.Reserved, nil
}
func (j *fakeJournal) Prepare(_ context.Context, e adapter.Effect, _ adapter.LedgerState) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.prepared = append(j.prepared, jkey(e))
	if j.states[jkey(e)] == adapter.Reserved {
		j.states[jkey(e)] = adapter.Dispatched
	}
	return nil
}
func (j *fakeJournal) Finish(_ context.Context, e adapter.Effect, c adapter.Completion) error {
	if err := adapter.ValidateCompletion(c); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.completions = append(j.completions, c)
	switch {
	case c.ReleaseLedger, c.State == adapter.Released:
		delete(j.states, jkey(e))
	default:
		j.states[jkey(e)] = c.State
	}
	return nil
}
func (j *fakeJournal) Refuse(_ context.Context, e adapter.Effect) error {
	delete(j.states, jkey(e))
	return nil
}
func (j *fakeJournal) ReleaseReservation(_ context.Context, e adapter.Effect) error {
	delete(j.states, jkey(e))
	return nil
}
func (j *fakeJournal) ledger() []adapter.LedgerEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := []adapter.LedgerEntry{}
	for k, state := range j.states {
		surface, name, _ := strings.Cut(k, ":")
		out = append(out, adapter.LedgerEntry{Surface: adapter.Surface(surface), EffectiveName: name, State: state})
	}
	return out
}
func (j *fakeJournal) last() adapter.Completion { return j.completions[len(j.completions)-1] }

// ---- harness -------------------------------------------------------------

type staticResolver []netip.Addr

func (r staticResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return slices.Clone(r), nil
}

type recorded struct {
	url    string
	header http.Header
	body   []byte
	resp   []byte
}

type harness struct {
	t        *testing.T
	receiver *sealedreceiver.Receiver
	server   *httptest.Server
	origin   string
	roots    *x509.CertPool
	signer   *sealedhook.Signer
	ackKey   *sealedhook.Signer
	ackPub   sealedhook.PublicKey
	identity string
	recip    string
	endpoint *Endpoint
	module   *Module
	mu       sync.Mutex
	log      []recorded
	override http.Handler
}

type captureWriter struct {
	http.ResponseWriter
	buf bytes.Buffer
}

func (c *captureWriter) Write(p []byte) (int, error) {
	c.buf.Write(p)
	return c.ResponseWriter.Write(p)
}

func (c *captureWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return c.ResponseWriter.(http.Hijacker).Hijack()
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t}
	var err error
	h.identity, h.recip, err = sealedhook.GenerateRecipient()
	if err != nil {
		t.Fatal(err)
	}
	signerPEM, _, err := sealedhook.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	h.signer, _ = sealedhook.ParseSigningKeyPEM(signerPEM)
	ackPEM, ackPubText, _ := sealedhook.GenerateSigningKey()
	h.ackKey, _ = sealedhook.ParseSigningKeyPEM(ackPEM)
	h.ackPub, _ = sealedhook.ParsePublicKey(ackPubText)
	h.receiver, err = sealedreceiver.New(sealedreceiver.Config{
		Identity: h.identity, AckSigner: h.ackKey, Senders: []sealedhook.PinnedKey{{Key: h.signer.Public()}},
		TargetID: "payments", InstanceID: "hikyo-test", Generation: 1, Bindings: map[string]string{namespace: binding},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		// Record at arrival so log order is request order even when a
		// handler hijacks and the client retries before this one returns.
		h.mu.Lock()
		index := len(h.log)
		h.log = append(h.log, recorded{url: r.URL.String(), header: r.Header.Clone(), body: body})
		next := http.Handler(h.receiver)
		if h.override != nil {
			next = h.override
		}
		h.mu.Unlock()
		cw := &captureWriter{ResponseWriter: w}
		next.ServeHTTP(cw, r)
		h.mu.Lock()
		h.log[index].resp = cw.buf.Bytes()
		h.mu.Unlock()
	}))
	t.Cleanup(h.server.Close)
	h.roots = x509.NewCertPool()
	h.roots.AddCert(h.server.Certificate())
	u, _ := url.Parse(h.server.URL)
	_, port, _ := net.SplitHostPort(u.Host)
	h.origin = "https://example.com:" + port
	h.endpoint = h.newEndpoint(1, h.signer)
	h.module = h.newModule(h.endpoint, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, staticResolver{netip.MustParseAddr("127.0.0.1")})
	return h
}

func (h *harness) newEndpoint(generation int64, signer *sealedhook.Signer) *Endpoint {
	fp, err := sealedhook.Fingerprint(h.origin, h.recip, h.ackPub.String(), generation)
	if err != nil {
		h.t.Fatal(err)
	}
	e, err := NewEndpoint(EndpointConfig{
		ID: "payments", InstanceID: "hikyo-test", Origin: h.origin, RecipientKey: h.recip, AckKey: h.ackPub.String(),
		Generation: generation, Timeout: 5 * time.Second, MaxResponseBytes: 4096, ConfirmedFingerprint: fp, Signer: signer,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return e
}

func (h *harness) newModule(e *Endpoint, allowed []netip.Prefix, resolver staticResolver) *Module {
	cfg := e.ClientConfig()
	cfg.AllowedCIDRs = allowed
	client, err := newClient(cfg, resolver, &net.Dialer{Timeout: 5 * time.Second}, h.roots)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(client.Forget)
	return &Module{API: client, Endpoint: e, Binding: binding}
}

func (h *harness) target() adapter.Target {
	return adapter.Target{ID: "tgt_1", Generation: 1, Destination: adapter.Destination{
		Kind: adapter.Organization, Owner: namespace, NumericID: sealedhook.DestinationID(h.endpoint.Fingerprint(), namespace),
	}}
}

func (h *harness) receiverSigner() *sealedhook.Signer { return h.ackKey }

func source(revision int64) adapter.Source {
	return adapter.Source{OrgID: "org_1", ProjectID: "prj_1", EnvironmentID: "env_1", Revision: revision}
}

func manifest(value string) []adapter.ManifestEntry {
	return []adapter.ManifestEntry{
		{KeyID: "key_1", CanonicalName: "DB_PASSWORD", Classification: adapter.SecretClassification, Value: value},
		{KeyID: "key_2", CanonicalName: "LOG_LEVEL", Classification: adapter.ConfigClassification, Value: "debug"},
	}
}

func (h *harness) sync(j *fakeJournal, revision int64, entries []adapter.ManifestEntry, teardown bool) (adapter.SyncResult, error) {
	return h.module.Sync(h.t.Context(), adapter.SyncRequest{
		Target: h.target(), Manifest: entries, Ledger: j.ledger(), Teardown: teardown, Source: source(revision),
	}, j)
}

// assertNoPlaintext scans every observable boundary: request URLs, headers,
// bodies, acknowledgement bodies, completions, results, and errors.
func (h *harness) assertNoPlaintext(j *fakeJournal, extra ...any) {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, canary := range []string{marker, binding} {
		for i, r := range h.log {
			if strings.Contains(r.url, canary) || strings.Contains(fmt.Sprint(r.header), canary) || bytes.Contains(r.body, []byte(canary)) || bytes.Contains(r.resp, []byte(canary)) {
				h.t.Fatalf("request %d exposes %q outside the ciphertext", i, canary)
			}
		}
		if strings.Contains(fmt.Sprintf("%+v", j.completions), canary) {
			h.t.Fatalf("journal completion exposes %q", canary)
		}
		for _, x := range extra {
			if strings.Contains(fmt.Sprintf("%+v", x), canary) {
				h.t.Fatalf("observable %T exposes %q", x, canary)
			}
		}
	}
}

// ---- structural closure --------------------------------------------------

func TestProviderSurfaceIsDeliverOnly(t *testing.T) {
	typ := reflect.TypeOf((*API)(nil)).Elem()
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	if !slices.Equal(got, []string{"Deliver"}) {
		t.Fatalf("API = %v, want exactly Deliver", got)
	}
	if len(operationRegistry) != 1 {
		t.Fatalf("route table = %v, want one route", operationRegistry)
	}
	for name, op := range operationRegistry {
		if op.Method != http.MethodPost || op.Path != sealedhook.SyncPath {
			t.Fatalf("route %s = %+v, want only POST %s", name, op, sealedhook.SyncPath)
		}
	}
}

func TestClientTransportPolicy(t *testing.T) {
	h := newHarness(t)
	client := h.module.API.(*Client)
	transport := client.http.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.DialContext == nil || transport.TLSClientConfig.MinVersion < 0x0303 {
		t.Fatal("transport must have no proxy, the public dialer, and TLS >= 1.2")
	}
	if client.http.CheckRedirect(&http.Request{}, nil) == nil {
		t.Fatal("redirects must be refused")
	}
}

func TestEndpointActivationRequiresConfirmedFingerprintAndBounds(t *testing.T) {
	h := newHarness(t)
	fp := h.endpoint.Fingerprint()
	base := EndpointConfig{ID: "payments", InstanceID: "hikyo-test", Origin: h.origin, RecipientKey: h.recip, AckKey: h.ackPub.String(),
		Generation: 1, Timeout: time.Second, MaxResponseBytes: 1024, ConfirmedFingerprint: fp, Signer: h.signer}
	if _, err := NewEndpoint(base); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*EndpointConfig){
		"tofu":           func(c *EndpointConfig) { c.ConfirmedFingerprint = "" },
		"wrong fp":       func(c *EndpointConfig) { c.ConfirmedFingerprint = "sha256:" + strings.Repeat("0", 64) },
		"key swap":       func(c *EndpointConfig) { _, r, _ := sealedhook.GenerateRecipient(); c.RecipientKey = r },
		"host change":    func(c *EndpointConfig) { c.Origin = "https://attacker.example" },
		"generation":     func(c *EndpointConfig) { c.Generation = 2 },
		"plain http":     func(c *EndpointConfig) { c.Origin = "http://example.com" },
		"path":           func(c *EndpointConfig) { c.Origin = h.origin + "/other" },
		"userinfo":       func(c *EndpointConfig) { c.Origin = "https://u:p@example.com" },
		"query":          func(c *EndpointConfig) { c.Origin = h.origin + "?x=1" },
		"timeout":        func(c *EndpointConfig) { c.Timeout = 11 * time.Second },
		"response bound": func(c *EndpointConfig) { c.MaxResponseBytes = MaxResponseBytes + 1 },
		"no signer":      func(c *EndpointConfig) { c.Signer = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := base
			mutate(&c)
			if _, err := NewEndpoint(c); err == nil {
				t.Fatal("endpoint activated")
			}
		})
	}
}

// ---- end to end ----------------------------------------------------------

func TestEndToEndConvergeUpdatePruneAndTeardown(t *testing.T) {
	h := newHarness(t)
	conn, err := h.module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: func(context.Context) error { return nil }})
	if err != nil || conn.DestinationID != h.target().Destination.NumericID {
		t.Fatalf("TestConnection = %+v, %v", conn, err)
	}
	j := newJournal()
	result, err := h.sync(j, 1, manifest(marker), false)
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"secret/DB_PASSWORD", "secret/MANAGED_BY_HIKYO", "variable/LOG_LEVEL", "variable/MANAGED_BY_HIKYO"}
	if got := h.receiver.Keys(namespace); !slices.Equal(got, wantKeys) {
		t.Fatalf("receiver keys = %v", got)
	}
	if h.receiver.Snapshot(namespace)["secret/DB_PASSWORD"].Value != marker {
		t.Fatal("receiver did not decrypt the exact value")
	}
	if j.prepared[0] != "secret:MANAGED_BY_HIKYO" || j.prepared[1] != "variable:MANAGED_BY_HIKYO" {
		t.Fatalf("sentinels must be written first: %v", j.prepared)
	}
	for k, s := range j.states {
		if s != adapter.Owned {
			t.Fatalf("%s = %s, want owned", k, s)
		}
	}

	if _, err := h.sync(j, 2, manifest(marker+"-v2"), false); err != nil {
		t.Fatal(err)
	}
	if h.receiver.Snapshot(namespace)["secret/DB_PASSWORD"].Value != marker+"-v2" {
		t.Fatal("update did not converge")
	}

	if _, err := h.sync(j, 3, manifest(marker + "-v2")[:1], false); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.receiver.Snapshot(namespace)["variable/LOG_LEVEL"]; ok {
		t.Fatal("ledger-owned name was not pruned")
	}

	j.prepared = nil
	teardown, err := h.sync(j, 3, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.receiver.Keys(namespace)) != 0 || len(j.states) != 0 {
		t.Fatalf("teardown left %v / %v", h.receiver.Keys(namespace), j.states)
	}
	if !strings.HasSuffix(j.prepared[len(j.prepared)-1], adapter.SentinelName) || !strings.HasSuffix(j.prepared[len(j.prepared)-2], adapter.SentinelName) {
		t.Fatalf("sentinels must be removed last: %v", j.prepared)
	}
	h.assertNoPlaintext(j, result, teardown)
}

func TestCrashWindowReplayReusesIdempotencyKey(t *testing.T) {
	h := newHarness(t)
	crashes := 1
	cfgAfter := func(w http.ResponseWriter) bool {
		if crashes == 0 {
			return true
		}
		crashes--
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
		return false
	}
	h.receiver.SetAfterApply(cfgAfter)
	j := newJournal()
	_, err := h.sync(j, 1, manifest(marker), false)
	if !errors.Is(err, adapter.ErrIndeterminate) {
		t.Fatalf("crash window error = %v, want indeterminate", err)
	}
	if c := j.last(); c.Outcome != adapter.OutcomeUnknown || c.State != adapter.Dispatched {
		t.Fatalf("crash window completion = %+v, want unknown/dispatched", c)
	}
	if got := h.receiver.Keys(namespace); !slices.Equal(got, []string{"secret/MANAGED_BY_HIKYO"}) {
		t.Fatalf("receiver applied before crash = %v", got)
	}
	firstKey := idemOf(t, h.log[0].body)
	if _, err := h.sync(j, 1, manifest(marker), false); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if idemOf(t, h.log[1].body) != firstKey {
		t.Fatal("retry did not reuse the idempotency key")
	}
	if ack, _ := sealedhook.ParseAck(h.log[1].resp); ack.Status != sealedhook.AckAlreadyApplied {
		t.Fatalf("replayed effect ack = %q, want already_applied", ack.Status)
	}
	if j.states["secret:MANAGED_BY_HIKYO"] != adapter.Owned {
		t.Fatal("already_applied must advance the cursor to owned")
	}
	h.assertNoPlaintext(j)
}

func idemOf(t *testing.T, body []byte) string {
	e, err := sealedhook.ParseEnvelope(body)
	if err != nil {
		t.Fatal(err)
	}
	return e.IdempotencyKey
}

func TestForgedAcknowledgementsAreTerminal(t *testing.T) {
	for name, forge := range map[string]func(h *harness, body []byte) []byte{
		"wrong key": func(h *harness, body []byte) []byte {
			other, _ := sealedhook.ParseSigningKeyPEM(mustPEM(t))
			e, _ := sealedhook.ParseEnvelope(body)
			ack, _ := other.SignAck(sealedhook.Digest(body), e.IdempotencyKey, sealedhook.AckApplied, "")
			return ack
		},
		"tampered status": func(h *harness, body []byte) []byte {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, sealedhook.SyncPath, bytes.NewReader(body))
			req.Header.Set("Content-Type", sealedhook.ContentType)
			h.receiver.ServeHTTP(rec, req)
			return bytes.Replace(rec.Body.Bytes(), []byte(`"status":"applied"`), []byte(`"status":"already_applied"`), 1)
		},
		"wrong digest": func(h *harness, body []byte) []byte {
			e, _ := sealedhook.ParseEnvelope(body)
			ack, _ := h.receiverSigner().SignAck(sealedhook.Digest([]byte("x")), e.IdempotencyKey, sealedhook.AckApplied, "")
			return ack
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.override = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", sealedhook.AckContentType)
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
			if adapter.ClassifyError(err) != adapter.ErrorClassProviderAmbiguous || !adapter.ClassifyError(err).NeedsAttention() {
				t.Fatal("forged ack must need attention")
			}
		})
	}
}

func mustPEM(t *testing.T) []byte {
	p, _, err := sealedhook.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAmbiguousResponsesStayUnknown(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			// Same origin, so a followed redirect would be observed here.
			http.Redirect(w, r, sealedhook.SyncPath+"?redirected=1", http.StatusTemporaryRedirect)
		},
		"server error": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"oversized": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", sealedhook.AckContentType)
			_, _ = w.Write(bytes.Repeat([]byte("a"), 8192))
		},
		"malformed": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", sealedhook.AckContentType)
			_, _ = w.Write([]byte(`{"v":1,"status":"applied"}`))
		},
		"media type": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>" + marker + "</html>"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.override = handler
			j := newJournal()
			result, err := h.sync(j, 1, manifest(marker), false)
			if !errors.Is(err, adapter.ErrIndeterminate) || !errors.Is(err, ErrNoAck) {
				t.Fatalf("error = %v, want indeterminate no-ack", err)
			}
			if c := j.last(); c.Outcome != adapter.OutcomeUnknown {
				t.Fatalf("completion = %+v, want unknown", c)
			}
			if len(h.log) != 1 {
				t.Fatalf("requests = %d, want exactly one (no redirect follow)", len(h.log))
			}
			if name == "oversized" && !errors.Is(err, ErrOversizedResponse) {
				t.Fatalf("oversized error = %v", err)
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatal("response body leaked into the error")
			}
			_ = result
		})
	}
}

func TestEgressRefusesPrivateLinkLocalAndMetadataDestinations(t *testing.T) {
	h := newHarness(t)
	for _, addr := range []string{"127.0.0.1", "10.0.0.5", "169.254.169.254", "fd00::1", "::1"} {
		t.Run(addr, func(t *testing.T) {
			m := h.newModule(h.endpoint, nil, staticResolver{netip.MustParseAddr(addr)})
			_, err := m.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: func(context.Context) error { return nil }})
			var transportError *url.Error
			if !errors.Is(err, ErrNoAck) || !errors.As(err, &transportError) || !strings.Contains(transportError.Err.Error(), "non-public") {
				t.Fatalf("error = %v, want non-public refusal", err)
			}
		})
	}
	// DNS rebinding: one public and one private answer is refused as a whole.
	m := h.newModule(h.endpoint, nil, staticResolver{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")})
	if _, err := m.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: func(context.Context) error { return nil }}); err == nil {
		t.Fatal("mixed public/private answer accepted")
	}
	if len(h.log) != 0 {
		t.Fatalf("refused egress still reached the receiver %d times", len(h.log))
	}
}

func TestReceiverRefusesReplayedExpiredAndUnpinnedEnvelopes(t *testing.T) {
	h := newHarness(t)
	j := newJournal()
	if _, err := h.sync(j, 1, manifest(marker), false); err != nil {
		t.Fatal(err)
	}
	captured := h.log[2].body // secret DB_PASSWORD upsert
	before := h.receiver.Snapshot(namespace)
	// Replay inside the validity window: acknowledged as already applied, no double effect.
	if ack := post(t, h, captured); ack.Status != sealedhook.AckAlreadyApplied {
		t.Fatalf("replay ack = %q", ack.Status)
	}
	if !reflect.DeepEqual(before, h.receiver.Snapshot(namespace)) {
		t.Fatal("replay changed receiver state")
	}
	// Expired replay: refused with no signed acknowledgement.
	late, _ := sealedreceiver.New(sealedreceiver.Config{
		Identity: h.identity, AckSigner: h.receiverSigner(), Senders: []sealedhook.PinnedKey{{Key: h.signer.Public()}},
		TargetID: "payments", InstanceID: "hikyo-test", Generation: 1, Bindings: map[string]string{namespace: binding},
		Now: func() time.Time { return time.Now().Add(10 * time.Minute) },
	})
	if code := serve(late, captured); code != http.StatusBadRequest {
		t.Fatalf("expired replay status = %d", code)
	}
	// Envelope signed by a key the receiver never pinned.
	rogue, _ := sealedhook.ParseSigningKeyPEM(mustPEM(t))
	m := &Module{API: h.module.API, Endpoint: h.newEndpoint(1, rogue), Binding: binding}
	if _, err := m.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: func(context.Context) error { return nil }}); !errors.Is(err, ErrNoAck) {
		t.Fatalf("unpinned sender error = %v", err)
	}
}

func post(t *testing.T, h *harness, body []byte) sealedhook.Ack {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, sealedhook.SyncPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", sealedhook.ContentType)
	h.receiver.ServeHTTP(rec, req)
	ack, err := sealedhook.VerifyAck(rec.Body.Bytes(), h.ackPub, sealedhook.Digest(body), idemOf(t, body))
	if err != nil {
		t.Fatalf("ack: %v (%d)", err, rec.Code)
	}
	return ack
}

func serve(r *sealedreceiver.Receiver, body []byte) int {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, sealedhook.SyncPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", sealedhook.ContentType)
	r.ServeHTTP(rec, req)
	return rec.Code
}

func TestUnownedNameIsAConflictAndNeverOverwritten(t *testing.T) {
	h := newHarness(t)
	h.receiver.Seed(namespace, "secret", "DB_PASSWORD", "theirs")
	j := newJournal()
	result, err := h.sync(j, 1, manifest(marker), false)
	if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 {
		t.Fatalf("error = %v conflicts = %v", err, result.Conflicts)
	}
	if c := j.last(); !c.Conflict || !c.ReleaseLedger {
		t.Fatalf("completion = %+v, want released conflict", c)
	}
	if h.receiver.Snapshot(namespace)["secret/DB_PASSWORD"].Value != "theirs" {
		t.Fatal("unowned value overwritten")
	}
}

func TestKeyOverlapRotationAndTrustBoundaryChange(t *testing.T) {
	h := newHarness(t)
	newPEM := mustPEM(t)
	next, _ := sealedhook.ParseSigningKeyPEM(newPEM)
	// Receiver pins both keys; the old key retires after the overlap window.
	rotated, _ := sealedreceiver.New(sealedreceiver.Config{
		Identity: h.identity, AckSigner: h.receiverSigner(),
		Senders:  []sealedhook.PinnedKey{{Key: h.signer.Public(), NotAfter: time.Now().Add(-time.Second)}, {Key: next.Public()}},
		TargetID: "payments", InstanceID: "hikyo-test", Generation: 1, Bindings: map[string]string{namespace: binding},
	})
	h.override = rotated
	gate := func(context.Context) error { return nil }
	if _, err := h.module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: gate}); !errors.Is(err, ErrNoAck) {
		t.Fatalf("retired key accepted after overlap: %v", err)
	}
	m := &Module{API: h.module.API, Endpoint: h.newEndpoint(1, next), Binding: binding}
	if _, err := m.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: gate}); err != nil {
		t.Fatalf("new key refused: %v", err)
	}
	// A generation bump (recipient/ack key rotation) is a new trust boundary:
	// targets bound under the old fingerprint fail closed before any request.
	h.override = nil
	requests := len(h.log)
	bumped := &Module{API: h.module.API, Endpoint: h.newEndpoint(2, h.signer), Binding: binding}
	_, err := bumped.Sync(t.Context(), adapter.SyncRequest{Target: h.target(), Manifest: manifest(marker), Source: source(1)}, newJournal())
	if !errors.Is(err, adapter.ErrDestinationID) || len(h.log) != requests {
		t.Fatalf("error = %v, requests %d -> %d", err, requests, len(h.log))
	}
}

func TestBindingRefusalIsProviderAuth(t *testing.T) {
	h := newHarness(t)
	h.module.Binding = "not-the-binding"
	_, err := h.module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: h.target().Destination, Gate: func(context.Context) error { return nil }})
	if !errors.Is(err, adapter.ErrProviderAuth) {
		t.Fatalf("error = %v, want provider auth", err)
	}
}

func TestStaleWorkerCannotDispatchAfterLosingAuthority(t *testing.T) {
	h := newHarness(t)
	j := newJournal()
	j.gate = func(adapter.Effect) error { return adapter.ErrSuperseded }
	if _, err := h.sync(j, 1, manifest(marker), false); !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatalf("error = %v", err)
	}
	if len(h.log) != 0 {
		t.Fatal("fenced worker reached the receiver")
	}
}

func TestPlanIsNetworkFreeAndValueBlind(t *testing.T) {
	h := newHarness(t)
	plan, err := h.module.Plan(t.Context(), adapter.PlanRequest{Target: h.target(), Manifest: manifest(""), Gate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range plan.Changes {
		if c.Disposition != adapter.Unknown {
			t.Fatalf("unclaimed %s planned as %s, want unknown-until-sync", c.EffectiveName, c.Disposition)
		}
	}
	if len(h.log) != 0 {
		t.Fatal("plan made a request")
	}
}

func TestRevisionZeroIsANoOpOnlyWhileTheRouteHoldsNoName(t *testing.T) {
	h := newHarness(t)
	j := newJournal()
	result, err := h.sync(j, 0, nil, false)
	h.mu.Lock()
	requests := len(h.log)
	h.mu.Unlock()
	if err != nil || len(result.Changes) != 0 || requests != 0 {
		t.Fatalf("empty route at revision 0 = %+v, %v, %d requests", result, err, requests)
	}
	if _, err := h.sync(j, 1, manifest(marker), false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sync(j, 0, nil, false); err == nil {
		t.Fatal("route holding names accepted an unsigned revision 0 converge")
	}
}

func TestReceiverPersistFailureRollsBackAndWithholdsAcknowledgement(t *testing.T) {
	h := newHarness(t)
	h.receiver.SetPersist(func(sealedreceiver.State) error { return errors.New("disk full") })
	j := newJournal()
	_, err := h.sync(j, 1, manifest(marker), false)
	if !errors.Is(err, adapter.ErrIndeterminate) {
		t.Fatalf("persist failure error = %v, want indeterminate", err)
	}
	if c := j.last(); c.Outcome != adapter.OutcomeUnknown || c.State != adapter.Dispatched {
		t.Fatalf("persist failure completion = %+v, want unknown/dispatched", c)
	}
	if got := h.receiver.Keys(namespace); len(got) != 0 {
		t.Fatalf("receiver kept unpersisted values in memory: %v", got)
	}
	h.receiver.SetPersist(nil)
	if _, err := h.sync(j, 1, manifest(marker), false); err != nil {
		t.Fatalf("retry after persist recovery: %v", err)
	}
	h.mu.Lock()
	first, retry := idemOf(t, h.log[0].body), idemOf(t, h.log[1].body)
	h.mu.Unlock()
	if first != retry {
		t.Fatal("retry did not reuse the idempotency key")
	}
	if h.receiver.Snapshot(namespace)["secret/DB_PASSWORD"].Value != marker {
		t.Fatal("retry did not apply the value")
	}
}
