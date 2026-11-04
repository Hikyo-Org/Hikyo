package isolation

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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/sealedwebhook"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/sealedreceiver"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Sealed webhook protocol 2 (#1128), end to end through the real service,
// store, outbox runtime, worker and sender module, egress dialer and TLS
// against the in-tree reference receiver (which actually verifies both
// signatures, decrypts the hybrid ciphertext and signs the acknowledgement).
// The loader, module factory and push authorizer are test closures shaped like
// the production ones (internal/app/adapter_loader.go is unexported) and the
// registry file parsing is bypassed: endpoints are built with the same
// constructor the boot path uses. The receiver is local. It always runs: the
// receiver needs no external service. Both engines run it.
//
// Covered: encrypt, send, receiver verify and decrypt, dual-signed
// acknowledgement, sender reconcile; duplicate delivery; ambiguous result and
// restart; concurrent key rotation; multi-node takeover with a stale worker
// that returns after losing its lease; sender and receiver skew with no
// downgrade; and restore of target custody (an older registry) that revives
// no authority.

const (
	swNamespace = "prod"
	swPlain1    = "sw-plaintext-revision-one"
	swPlain2    = "sw-plaintext-revision-two"
	swPlain3    = "sw-plaintext-revision-three"
	swPlain4    = "sw-plaintext-revision-four"
	swBinding   = "sw-binding-credential-canary"
	swInstance  = "hikyo-isolation"
	swTargetID  = "payments"
)

// swReceiver is the reference receiver behind one swappable handler, so a test
// can rotate its pins, drop to version 1 only, or block a request, exactly as
// an operator would on a live receiver.
type swReceiver struct {
	t       *testing.T
	server  *httptest.Server
	roots   *x509.CertPool
	origin  string
	mu      sync.Mutex
	handler http.Handler
	log     []swRequest

	identityV1, identityV2 string
	edSender               *sealedhook.Signer
	pqSender               *sealedhook.MLDSASigner
	ackEd                  *sealedhook.Signer
	ackPQ                  *sealedhook.MLDSASigner
	recipientV1            string
	recipientV2            string
	receiver               *sealedreceiver.Receiver
	gate                   func(path string, body []byte) // runs before the handler; may block
}

type swRequest struct {
	path   string
	body   []byte
	status int
	resp   []byte
}

// swWriter records what the receiver answered. It passes Hijack through so the
// crash-window hook can still drop the connection.
type swWriter struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (w *swWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *swWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.buf.Write(p)
	return w.ResponseWriter.Write(p)
}

func (w *swWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.(http.Hijacker).Hijack()
}

func mustEd(t *testing.T) *sealedhook.Signer {
	t.Helper()
	pemKey, _, err := sealedhook.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := sealedhook.ParseSigningKeyPEM(pemKey)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustPQ(t *testing.T) *sealedhook.MLDSASigner {
	t.Helper()
	pemKey, _, err := sealedhook.GenerateMLDSASigningKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := sealedhook.ParseMLDSASigningKeyPEM(pemKey)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newSWReceiver(t *testing.T) *swReceiver {
	t.Helper()
	r := &swReceiver{t: t, edSender: mustEd(t), pqSender: mustPQ(t), ackEd: mustEd(t), ackPQ: mustPQ(t)}
	var err error
	if r.identityV1, r.recipientV1, err = sealedhook.GenerateRecipient(); err != nil {
		t.Fatal(err)
	}
	if r.identityV2, r.recipientV2, err = sealedhook.GenerateHybridRecipient(); err != nil {
		t.Fatal(err)
	}
	r.receiver = r.build(true, true, nil)
	r.handler = r.receiver
	r.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(body))
		r.mu.Lock()
		index := len(r.log)
		r.log = append(r.log, swRequest{path: req.URL.Path, body: body})
		gate := r.gate
		r.mu.Unlock()
		if gate != nil {
			gate(req.URL.Path, body)
		}
		// Read the handler AFTER the gate: a request held at the gate must meet
		// the receiver configuration in force when it is released.
		r.mu.Lock()
		next := r.handler
		r.mu.Unlock()
		rec := &swWriter{ResponseWriter: w}
		next.ServeHTTP(rec, req)
		r.mu.Lock()
		r.log[index].status, r.log[index].resp = rec.status, rec.buf.Bytes()
		r.mu.Unlock()
	}))
	t.Cleanup(r.server.Close)
	r.roots = x509.NewCertPool()
	r.roots.AddCert(r.server.Certificate())
	r.origin = r.server.URL // https://127.0.0.1:port
	return r
}

// build returns a receiver serving the selected versions with the given
// sender pairs pinned (nil pins the current pair).
func (r *swReceiver) build(v1, v2 bool, pairs []sealedhook.PinnedSenderV2) *sealedreceiver.Receiver {
	cfg := sealedreceiver.Config{TargetID: swTargetID, InstanceID: swInstance, Generation: 1, Bindings: map[string]string{swNamespace: swBinding}}
	if v1 {
		cfg.Identity, cfg.AckSigner, cfg.Senders = r.identityV1, r.ackEd, []sealedhook.PinnedKey{{Key: r.edSender.Public()}}
	}
	if v2 {
		signer, err := sealedhook.NewSignerV2(r.ackEd, r.ackPQ)
		if err != nil {
			r.t.Fatal(err)
		}
		if pairs == nil {
			pairs = []sealedhook.PinnedSenderV2{{Ed: r.edSender.Public(), PQ: r.pqSender.Public()}}
		}
		cfg.HybridIdentity, cfg.AckSignerV2, cfg.SendersV2 = r.identityV2, signer, pairs
	}
	rc, err := sealedreceiver.New(cfg)
	if err != nil {
		r.t.Fatal(err)
	}
	return rc
}

// swap replaces the handler while keeping the receiver state when asked.
func (r *swReceiver) swap(next *sealedreceiver.Receiver, keepState bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if keepState && r.receiver != nil {
		next.Restore(r.receiver.State())
	}
	r.receiver, r.handler = next, next
}

func (r *swReceiver) paths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.log))
	for i, e := range r.log {
		out[i] = e.path
	}
	return out
}

func (r *swReceiver) requests() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.log)
}

// swRegistry mimics the operator registry: the endpoint a restart would build.
type swRegistry struct {
	t        *testing.T
	recv     *swReceiver
	mu       sync.Mutex
	endpoint *sealedwebhook.Endpoint
}

func (g *swRegistry) v2(generation int64, ed *sealedhook.Signer, pq *sealedhook.MLDSASigner) *sealedwebhook.Endpoint {
	g.t.Helper()
	fp, err := sealedhook.FingerprintV2(g.recv.origin, g.recv.recipientV2, g.recv.ackEd.Public(), g.recv.ackPQ.Public(), generation)
	if err != nil {
		g.t.Fatal(err)
	}
	e, err := sealedwebhook.NewEndpoint(sealedwebhook.EndpointConfig{
		ID: swTargetID, InstanceID: swInstance, Origin: g.recv.origin, RecipientKey: g.recv.recipientV2, AckKey: g.recv.ackEd.Public().String(),
		AckKeyMLDSA65: g.recv.ackPQ.Public().String(), Generation: generation, Timeout: 4 * time.Second, MaxResponseBytes: 16 << 10,
		ConfirmedFingerprint: fp, Signer: ed, SignerPQ: pq, Protocol: 2,
	})
	if err != nil {
		g.t.Fatal(err)
	}
	return e
}

func (g *swRegistry) v1(generation int64) *sealedwebhook.Endpoint {
	g.t.Helper()
	fp, err := sealedhook.Fingerprint(g.recv.origin, g.recv.recipientV1, g.recv.ackEd.Public().String(), generation)
	if err != nil {
		g.t.Fatal(err)
	}
	e, err := sealedwebhook.NewEndpoint(sealedwebhook.EndpointConfig{
		ID: swTargetID, InstanceID: swInstance, Origin: g.recv.origin, RecipientKey: g.recv.recipientV1, AckKey: g.recv.ackEd.Public().String(),
		Generation: generation, Timeout: 4 * time.Second, MaxResponseBytes: 16 << 10, ConfirmedFingerprint: fp, Signer: g.recv.edSender,
	})
	if err != nil {
		g.t.Fatal(err)
	}
	return e
}

func (g *swRegistry) set(e *sealedwebhook.Endpoint) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.endpoint = e
}

func (g *swRegistry) build(origin, credential string) (adapter.Module, func(), error) {
	g.mu.Lock()
	endpoint := g.endpoint
	g.mu.Unlock()
	if origin != endpoint.Origin() {
		return nil, nil, errors.New("sealed-webhook: origin is not an instance-admin configured endpoint")
	}
	cfg := endpoint.ClientConfig()
	cfg.AllowedCIDRs = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	cfg.RootCAs = g.recv.roots
	client, err := sealedwebhook.NewClient(cfg)
	if err != nil {
		return nil, nil, err
	}
	return &sealedwebhook.Module{API: client, Endpoint: endpoint, Binding: credential}, client.Forget, nil
}

// swLoader is the production loader's shape: kvLoader opens the credential and
// each snapshot value, and the sync source is pinned to the leased revision.
type swLoader struct{ kvLoader }

func (l swLoader) Load(ctx context.Context, job adapter.Job, journal adapter.Journal) (adapter.LoadedSync, error) {
	out, err := l.kvLoader.Load(ctx, job, journal)
	if err == nil {
		out.Request.Source = adapter.Source{OrgID: job.OrgID, ProjectID: job.ProjectID, EnvironmentID: job.EnvironmentID, Revision: out.Revision}
	}
	return out, err
}

func TestAdapterSealedWebhookProtocolTwoEndToEnd(t *testing.T) {
	forEngines(t, runSealedWebhookProtocolTwo)
}

func runSealedWebhookProtocolTwo(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	recv := newSWReceiver(t)
	reg := &swRegistry{t: t, recv: recv}
	endpointA := reg.v2(1, recv.edSender, recv.pqSender)
	reg.set(endpointA)

	scope := domain.Scope{Org: orgA, Project: prjA1}
	envScope := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_sw_manage','usr_alice','manage-adapters','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_sw_reveal','usr_alice','reveal','org_a','prj_a1','env_a1',`+ts+`)`)
	kr := probeKeyring(t, db)
	svc := &service.Adapters{DB: db, Keyring: kr, ModuleFactory: func(provider adapter.Provider, config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		if provider != adapter.SealedWebhookProvider {
			return nil, fmt.Errorf("unexpected provider %s", provider)
		}
		module, release, err := reg.build(config.Origin, credential)
		if err != nil {
			return nil, err
		}
		return adapter.NewModuleLease(module, release)
	}}
	operator := service.LocalPrincipal(alice)
	created, err := svc.Create(ctx, operator, scope, service.CreateAdapterRequest{
		Provider: string(adapter.SealedWebhookProvider), Origin: recv.origin, Credential: []byte(swBinding),
		Target: service.AdapterTargetInput{
			EnvironmentID: string(envA1), DestinationKind: string(adapter.Organization), DestinationOwner: swNamespace,
			KeySelection: &service.AdapterKeySelection{Include: []string{"SHARED_*"}},
		},
	})
	if err != nil {
		t.Fatalf("create sealed-webhook adapter: %v", err)
	}
	target := created.Targets[0]
	if want := endpointA.DestinationID(swNamespace); target.DestinationID != want {
		t.Fatalf("target destination %d, want the protocol 2 derivation %d", target.DestinationID, want)
	}

	publisher := service.LocalPrincipal(custodian)
	values := &service.Values{DB: db, Keyring: kr}
	revisions := &service.Revisions{DB: db, Keyring: kr}
	publish := func(plaintext string) {
		t.Helper()
		staged, err := values.Set(ctx, publisher, envScope, "SHARED_KEY", plaintext, nil)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		if _, err := revisions.PublishPlanned(ctx, publisher, envScope, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	authorizePush := func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
		return tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, domain.Scope{
				Org: domain.OrgID(job.OrgID), Project: domain.ProjectID(job.ProjectID), Env: domain.EnvID(job.EnvironmentID),
			})
			return err
		})
	}
	var clockMu sync.Mutex
	clock := time.Now().UTC()
	now := func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return clock }
	advance := func(d time.Duration) { clockMu.Lock(); clock = clock.Add(d); clockMu.Unlock() }
	newWorker := func(id string) *adapter.Worker {
		runtime := store.NewAdapterRuntime(db, authorizePush)
		return &adapter.Worker{
			Store: runtime, ID: id, Now: now, Jitter: func(time.Duration) time.Duration { return 0 },
			Loader: swLoader{kvLoader{runtime: runtime, keyring: kr, build: reg.build}},
		}
	}
	worker := newWorker("sw-node-1")
	drain := func(w *adapter.Worker) {
		t.Helper()
		for range 8 {
			advance(time.Second)
			worked, err := w.RunOnce(ctx)
			if err != nil {
				t.Fatalf("worker: %v", err)
			}
			if !worked {
				return
			}
		}
		t.Fatal("worker never drained the outbox")
	}
	// attempts runs a bounded number of passes for phases that are expected to
	// keep failing: a refused target is retried by the outbox, so it never
	// drains.
	attempts := func(w *adapter.Worker, n int) {
		t.Helper()
		for range n {
			advance(adapter.LeaseTime + time.Minute)
			if _, err := w.RunOnce(ctx); err != nil {
				t.Fatalf("worker: %v", err)
			}
		}
	}
	inspect := func() store.AdapterTarget {
		t.Helper()
		view, err := svc.InspectTarget(ctx, operator, scope, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		return view.Target
	}
	converged := func(revision int64, why string) {
		t.Helper()
		if got := inspect(); got.Health() != adapter.HealthConverged || got.ConvergedRevision == nil || *got.ConvergedRevision != revision {
			t.Fatalf("%s: health %q class %q converged %v, want revision %d", why, got.Health(), got.LastErrorClass, got.ConvergedRevision, revision)
		}
	}
	value := func() string {
		snap := recv.receiver.Snapshot(swNamespace)
		if v, ok := snap["secret/SHARED_KEY"]; ok {
			return v.Value
		}
		return snap["variable/SHARED_KEY"].Value
	}
	applied := func() int { return len(recv.receiver.State().Seen) }

	// 1. encrypt -> send -> actual receiver verify and decrypt -> signed
	// acknowledgement -> sender reconcile, through the real outbox.
	publish(swPlain1)
	drain(worker)
	converged(1, "first publish")
	if value() != swPlain1 {
		t.Fatalf("receiver decrypted %q, want %q", value(), swPlain1)
	}
	for _, p := range recv.paths() {
		if p != sealedhook.SyncPathV2 {
			t.Fatalf("a request left the version 2 route: %s", p)
		}
	}
	if open := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE target_id='%s' AND outcome IS NULL`, target.ID)); open != 0 {
		t.Fatalf("open effects after converge = %d", open)
	}
	intents := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='adapter.push_intent'`)
	outcomes := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='adapter.push_outcome'`)
	if intents == 0 || intents != outcomes {
		t.Fatalf("durable audit INTENT=%d OUTCOME=%d, want equal and non-zero", intents, outcomes)
	}
	appliedAfterFirst := applied()

	// logFrom returns a copy of the receiver log from an index.
	logFrom := func(from int) []swRequest {
		recv.mu.Lock()
		defer recv.mu.Unlock()
		return append([]swRequest(nil), recv.log[from:]...)
	}
	keyOf := func(e swRequest) string { return idemOfV2Body(t, e.body) }
	statusOf := func(e swRequest) string {
		if e.status != http.StatusOK {
			return fmt.Sprintf("http-%d", e.status)
		}
		ack, err := sealedhook.ParseAckV2(e.resp)
		if err != nil {
			return "unparsed"
		}
		return string(ack.Status)
	}
	// outboxPending is the durable retry state: one live job for the target.
	outboxPending := func(why string) {
		t.Helper()
		if n := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_outbox WHERE target_id='%s' AND state IN ('queued','running')`, target.ID)); n != 1 {
			t.Fatalf("%s: %d live outbox jobs, want exactly 1 awaiting retry", why, n)
		}
		if got := inspect(); got.LastErrorClass == "" || got.Health() == adapter.HealthConverged {
			t.Fatalf("%s: health %q class %q, want a recorded failure", why, got.Health(), got.LastErrorClass)
		}
	}
	// appliedOnce fails when any idempotency key was acknowledged "applied"
	// more than once: that would be a double apply.
	appliedOnce := func(why string) {
		t.Helper()
		applies := map[string]int{}
		for _, e := range logFrom(0) {
			if e.path == sealedhook.SyncPathV2 && statusOf(e) == string(sealedhook.AckApplied) {
				if e.resp != nil {
					if env, err := sealedhook.ParseEnvelopeV2(e.body); err == nil && env.Op != sealedhook.OpProbe {
						applies[env.IdempotencyKey]++
					}
				}
			}
		}
		for key, n := range applies {
			if n > 1 {
				t.Fatalf("%s: idempotency key %.12s was applied %d times", why, key, n)
			}
		}
	}

	// 2. Duplicate delivery: replaying a recorded request is answered
	// already_applied without a second effect.
	captured := logFrom(recv.requests() - 1)[0]
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, sealedhook.SyncPathV2, bytes.NewReader(captured.body))
	req.Header.Set("Content-Type", sealedhook.ContentTypeV2)
	recv.receiver.ServeHTTP(rec, req)
	if ack, err := sealedhook.ParseAckV2(rec.Body.Bytes()); err != nil || ack.Status != sealedhook.AckAlreadyApplied {
		t.Fatalf("duplicate delivery answered %q (%v)", rec.Body.String(), err)
	}
	if applied() != appliedAfterFirst || value() != swPlain1 {
		t.Fatalf("duplicate delivery changed receiver state: %d -> %d effects, value %q", appliedAfterFirst, applied(), value())
	}
	// A manual resync fences the target with a new generation, which is an
	// input of the idempotency key, so its effects carry NEW keys and the
	// receiver applies them again (existing, protocol-blind behavior). Same
	// value, no conflict, exactly one more set of effects.
	if _, err := svc.SyncTarget(ctx, operator, scope, target.ID); err != nil {
		t.Fatal(err)
	}
	drain(worker)
	converged(1, "resync of the same revision")
	if value() != swPlain1 || applied() != 2*appliedAfterFirst {
		t.Fatalf("resync: value %q, %d effects, want the same value and %d effects (new generation, new keys)", value(), applied(), 2*appliedAfterFirst)
	}

	// 3. Ambiguous result and restart: the receiver applies, then the
	// connection dies before the acknowledgement. The effect is unknown, the
	// restarted worker retries under the same idempotency key, and the
	// receiver answers already_applied.
	crashes := 1
	recv.receiver.SetAfterApply(func(w http.ResponseWriter) bool {
		if crashes == 0 {
			return true
		}
		crashes--
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}
		return false
	})
	before := recv.requests()
	publish(swPlain2)
	advance(time.Second)
	if _, err := worker.RunOnce(ctx); err != nil { // the attempt whose acknowledgement is lost
		t.Fatalf("first attempt: %v", err)
	}
	if got := inspect(); got.Health() == adapter.HealthConverged && got.ConvergedRevision != nil && *got.ConvergedRevision == 2 {
		t.Fatal("a delivery whose acknowledgement was lost was settled as converged")
	}
	worker = newWorker("sw-node-2") // process restart
	advance(adapter.LeaseTime + time.Minute)
	drain(worker)
	converged(2, "retry after restart")
	if value() != swPlain2 {
		t.Fatalf("value after retry = %q", value())
	}
	entries := logFrom(before)
	lostKey := keyOf(entries[0])
	var answers []string
	for _, e := range entries {
		if keyOf(e) == lostKey {
			answers = append(answers, statusOf(e))
		}
	}
	if len(answers) < 2 || answers[0] != "unparsed" && answers[0] != "http-0" || answers[len(answers)-1] != string(sealedhook.AckAlreadyApplied) {
		t.Fatalf("answers for the key whose acknowledgement was lost = %v, want the dropped attempt then already_applied", answers)
	}
	if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects e JOIN audit_tenant_events o ON o.id=e.outcome_audit_id WHERE e.target_id='%s' AND e.outcome='unknown' AND o.type='adapter.push_outcome'`, target.ID)); got == 0 {
		t.Fatal("the ambiguous attempt left no unknown OUTCOME audit row")
	}

	// 4. Concurrent key rotation. The first revision 3 request is HELD at the
	// receiver while the receiver retires the sender pair and pins a new one;
	// it is then released into the new configuration, refused with no
	// acknowledgement, and the retry under the same idempotency key succeeds
	// once the registry carries the new pair.
	nextEd, nextPQ := mustEd(t), mustPQ(t)
	var held atomic.Bool
	heldIn, heldRelease := make(chan struct{}), make(chan struct{})
	recv.mu.Lock()
	recv.gate = func(string, []byte) {
		if held.CompareAndSwap(false, true) {
			close(heldIn)
			<-heldRelease
		}
	}
	recv.mu.Unlock()
	rotationFrom := recv.requests()
	publish(swPlain3)
	rotating := make(chan error, 1)
	go func() {
		advance(time.Second)
		_, err := newWorker("sw-node-rotating").RunOnce(ctx)
		rotating <- err
	}()
	select {
	case <-heldIn:
	case <-time.After(30 * time.Second):
		t.Fatal("the first revision 3 request never reached the receiver")
	}
	recv.swap(recv.build(false, true, []sealedhook.PinnedSenderV2{
		{Ed: recv.edSender.Public(), PQ: recv.pqSender.Public(), NotAfter: time.Now().Add(-time.Hour)}, // the receiver judges with real time
		{Ed: nextEd.Public(), PQ: nextPQ.Public()},
	}), true)
	close(heldRelease)
	if err := <-rotating; err != nil {
		t.Fatalf("worker holding the request across the rotation: %v", err)
	}
	heldEntry := logFrom(rotationFrom)[0]
	if statusOf(heldEntry) != "http-400" {
		t.Fatalf("the request held across the rotation was answered %q, want a refusal with no acknowledgement", statusOf(heldEntry))
	}
	heldKey := keyOf(heldEntry)
	outboxPending("after the held request was refused")
	recv.mu.Lock()
	recv.gate = nil
	recv.mu.Unlock()
	endpointRotated := reg.v2(1, nextEd, nextPQ)
	if endpointRotated.Fingerprint() != endpointA.Fingerprint() {
		t.Fatal("rotating the sender pair must not change the fingerprint")
	}
	reg.set(endpointRotated) // the restart that loads the new key files
	advance(adapter.LeaseTime + time.Minute)
	drain(newWorker("sw-node-after-rotation"))
	converged(3, "rotation")
	if value() != swPlain3 {
		t.Fatalf("value after rotation = %q", value())
	}
	var retry swRequest
	for _, e := range logFrom(rotationFrom + 1) {
		if keyOf(e) == heldKey {
			retry = e
			break
		}
	}
	retryEnv, err := sealedhook.ParseEnvelopeV2(retry.body)
	if err != nil || retryEnv.KeyIDEd25519 != nextEd.Public().IDV2() || retryEnv.KeyIDMLDSA65 != nextPQ.Public().ID() || statusOf(retry) != string(sealedhook.AckApplied) {
		t.Fatalf("retry of the held key under the new pair = %+v, status %q", retryEnv.KeyIDEd25519, statusOf(retry))
	}

	// 5. Multi-node takeover. Node 1's request is applied by the receiver and
	// then STALLED before the acknowledgement (a process that dies after the
	// apply, before the OUTCOME). Its lease expires; node 2 takes over, settles
	// the open effect as unknown, retries and gets already_applied; node 1
	// then returns to the fence, which refuses it. Each effect is applied once.
	var stalled atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	recv.receiver.SetAfterApply(func(http.ResponseWriter) bool {
		if stalled.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		return true
	})
	takeoverFrom := recv.requests()
	publish(swPlain4)
	node1Done := make(chan error, 1)
	go func() {
		advance(time.Second)
		_, err := newWorker("sw-node-1b").RunOnce(ctx)
		node1Done <- err
	}()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("node 1's request was never applied")
	}
	advance(2 * adapter.LeaseTime)
	drain(newWorker("sw-node-2b"))
	converged(4, "takeover by node 2")
	appliedByNode2 := applied()
	close(release)
	if err := <-node1Done; !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatalf("node 1 after losing its lease = %v, want the superseded fence", err)
	}
	converged(4, "after the stale worker returned")
	if applied() != appliedByNode2 || value() != swPlain4 {
		t.Fatalf("stale worker changed receiver state: %d -> %d effects, value %q", appliedByNode2, applied(), value())
	}
	stalledKey := keyOf(logFrom(takeoverFrom)[0])
	var sawAlreadyApplied bool
	for _, e := range logFrom(takeoverFrom) {
		sawAlreadyApplied = sawAlreadyApplied || keyOf(e) == stalledKey && statusOf(e) == string(sealedhook.AckAlreadyApplied)
	}
	if !sawAlreadyApplied {
		t.Fatal("node 2's retry of the applied-but-unacknowledged effect was not answered already_applied")
	}
	appliedOnce("takeover")
	if open := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE target_id='%s' AND outcome IS NULL`, target.ID)); open != 0 {
		t.Fatalf("open effects after takeover = %d", open)
	}

	// 6. Skew with no downgrade: the receiver is rolled back to a build that
	// reads version 1 only. The version 2 target stays unconverged and
	// retries as version 2; no version 1 request is ever sent. Re-upgrading
	// the receiver converges it. The retired sender pair stays pinned with a
	// not_after in the past from here on.
	retiredPin := sealedhook.PinnedSenderV2{Ed: recv.edSender.Public(), PQ: recv.pqSender.Public(), NotAfter: time.Now().Add(-time.Hour)}
	currentPin := sealedhook.PinnedSenderV2{Ed: nextEd.Public(), PQ: nextPQ.Public()}
	recv.swap(recv.build(true, false, nil), true)
	skewFrom := recv.requests()
	publish("sw-plaintext-skew")
	attempts(worker, 3)
	skewed := logFrom(skewFrom)
	if len(skewed) == 0 {
		t.Fatal("no delivery attempt reached the version 1 only receiver: the phase proved nothing")
	}
	for _, e := range skewed {
		if e.path != sealedhook.SyncPathV2 || e.status != http.StatusNotFound {
			t.Fatalf("a protocol 2 target sent %s and was answered %d: it must send only version 2 and be refused", e.path, e.status)
		}
	}
	outboxPending("version 1 only receiver")
	recv.swap(recv.build(true, true, []sealedhook.PinnedSenderV2{retiredPin, currentPin}), true)
	advance(adapter.LeaseTime + time.Minute)
	drain(worker)
	converged(5, "after the receiver is upgraded again")

	// 7. Restore of target custody. An older registry (another generation, the
	// version 1 entry, or the retired sender pair's key files restored from a
	// backup) revives nothing: a changed boundary fails closed before any
	// request, and the retired pair is refused by the receiver for its
	// not_after, not merely because it is unknown.
	restoreFrom := recv.requests()
	reg.set(reg.v2(2, nextEd, nextPQ)) // a different generation is a different trust boundary
	publish("sw-plaintext-restore-1")
	attempts(worker, 3)
	reg.set(reg.v1(1)) // registry restored to the version 1 entry of the same origin
	attempts(worker, 3)
	if recv.requests() != restoreFrom {
		t.Fatalf("a changed trust boundary reached the receiver (%d requests)", recv.requests()-restoreFrom)
	}
	outboxPending("changed trust boundary")
	reg.set(reg.v2(1, recv.edSender, recv.pqSender)) // the retired pair's key files, restored
	attempts(worker, 3)
	retired := logFrom(restoreFrom)
	if len(retired) == 0 {
		t.Fatal("the retired pair never reached the receiver: the phase proved nothing")
	}
	for _, e := range retired {
		env, err := sealedhook.ParseEnvelopeV2(e.body)
		if err != nil || env.KeyIDEd25519 != recv.edSender.Public().IDV2() || statusOf(e) != "http-400" {
			t.Fatalf("retired pair request answered %q", statusOf(e))
		}
	}
	outboxPending("retired sender pair")
	reg.set(endpointRotated)
	advance(adapter.LeaseTime + time.Minute)
	drain(worker)
	converged(6, "restored current authority")
	appliedOnce("whole run")

	// No audit payload carries a value, the binding or ciphertext.
	for _, secret := range []string{swPlain1, swPlain2, swPlain3, swPlain4, swBinding, "AGE-SECRET-KEY"} {
		if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM audit_tenant_events WHERE payload LIKE '%%%s%%'`, secret)); got != 0 {
			t.Fatalf("audit payload carries %q", secret)
		}
	}
	recv.mu.Lock()
	defer recv.mu.Unlock()
	for _, e := range recv.log {
		for _, secret := range []string{swPlain1, swPlain2, swPlain3, swPlain4, swBinding} {
			if strings.Contains(string(e.body), secret) {
				t.Fatalf("a request body carries %q in the clear", secret)
			}
		}
	}
}

func idemOfV2Body(t *testing.T, body []byte) string {
	t.Helper()
	e, err := sealedhook.ParseEnvelopeV2(body)
	if err != nil {
		t.Fatal(err)
	}
	return e.IdempotencyKey
}
