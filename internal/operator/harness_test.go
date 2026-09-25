package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
	opclient "github.com/Hikyo-Org/hikyo/internal/operator/client"
)

const (
	testNS       = "team-a"
	testOwnNS    = "hikyo-system"
	testInstance = "prod"
	testTarget   = "app-secret"
)

var testClock = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

const (
	testClusterID       = "0b3c1f6e-2d4a-4e8b-9c7d-5a6b7c8d9e0f"
	testReporterVersion = "0.0.0-test"
	advertisingMeta     = `{"server_version":"0.0.0-test","api_revision":5,"protocol_capabilities":["local-password","delivery-target-report/1"]}`
	oldServerMeta       = `{"server_version":"0.0.0-test","api_revision":4,"protocol_capabilities":["local-password"]}`
)

// deliveryStub is a programmable Hikyo delivery server. Tests set status/json
// before each reconcile and read back the query params the operator sent.
// It also serves `/meta` and the delivery-target report and tombstone routes;
// requests counts delivery fetches only.
type deliveryStub struct {
	mu             sync.Mutex
	status         int
	json           string
	lastCursor     string
	lastAck        string
	lastProjection string
	requests       int
	bearers        []string

	meta         string
	metaRequests int
	// reportStatus answers a report; nil answers 204.
	reportStatus  func(body apigen.DeliveryTargetReportRequest) int
	reportDelay   time.Duration
	reports       [][]byte
	reportBearers []string
	tombstones    [][]byte
	// events records "report" and "tombstone" receipts in order, beside any
	// test-recorded writes, under the same lock.
	events []string
}

func (s *deliveryStub) record(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *deliveryStub) reportBodies() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reports)
}

func (s *deliveryStub) set(status int, json string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.json = status, json
}

func (s *deliveryStub) handler(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/meta"):
		s.serveMeta(w)
		return
	case strings.HasSuffix(r.URL.Path, "/delivery-targets/tombstone"):
		s.serveTombstone(w, r)
		return
	case strings.HasSuffix(r.URL.Path, "/delivery-targets"):
		s.serveReport(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	s.bearers = append(s.bearers, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	q := r.URL.Query()
	s.lastCursor = q.Get("cursor")
	s.lastAck = q.Get("acknowledged_keys")
	s.lastProjection = q.Get("projection")
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status == http.StatusOK {
		_, _ = io.WriteString(w, s.json)
	}
}

func (s *deliveryStub) serveMeta(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metaRequests++
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, s.meta)
}

func (s *deliveryStub) serveReport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	delay, statusFor := s.reportDelay, s.reportStatus
	s.reports = append(s.reports, body)
	s.reportBearers = append(s.reportBearers, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	s.events = append(s.events, "report")
	s.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}
	status := http.StatusNoContent
	if statusFor != nil {
		var decoded apigen.DeliveryTargetReportRequest
		if json.Unmarshal(body, &decoded) == nil {
			status = statusFor(decoded)
		}
	}
	w.WriteHeader(status)
}

func (s *deliveryStub) serveTombstone(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tombstones = append(s.tombstones, body)
	s.reportBearers = append(s.reportBearers, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	s.events = append(s.events, "tombstone")
	w.WriteHeader(http.StatusNoContent)
}

func serverCAPEM(t *testing.T, srv *httptest.Server) []byte {
	t.Helper()
	c := srv.Certificate()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
}

// stubMinter returns a fixed federation token; the fake client cannot serve the
// token subresource, so the reconciler's TokenMinter is injected in tests.
type stubMinter struct {
	token string
	err   error
	last  struct{ ns, sa, audience string }
	calls int
}

func (m *stubMinter) Mint(_ context.Context, ns, sa, audience string) (string, error) {
	m.calls++
	m.last.ns, m.last.sa, m.last.audience = ns, sa, audience
	if m.err != nil {
		return "", m.err
	}
	return m.token, nil
}

// harness wires a fake client, a reconciler, and a delivery stub.
type harness struct {
	t        *testing.T
	scheme   *runtime.Scheme
	cl       client.Client
	r        *HikyoSecretReconciler
	stub     *deliveryStub
	server   *httptest.Server
	recorder *record.FakeRecorder
	events   chan string
	minter   *stubMinter
	// clock is the reconciler's time; tests advance it for heartbeats.
	clock time.Time
	// denied collects, after every reconcile, the strings no report may carry:
	// condition messages, mapped key names, cursor, binding, stamp, managed
	// Secret UID and the bearer credentials the stub saw.
	denied map[string]bool
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	sch := runtime.NewScheme()
	if err := scheme.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	if err := hikyov1.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	return sch
}

// newHarness builds the fixture. objs are seeded into the fake client;
// interceptors are optional (write-ordering / fault-injection tests).
func newHarness(t *testing.T, interceptors interceptor.Funcs, objs ...client.Object) *harness {
	t.Helper()
	sch := testScheme(t)
	stub := &deliveryStub{meta: advertisingMeta}
	srv := httptest.NewTLSServer(http.HandlerFunc(stub.handler))
	t.Cleanup(srv.Close)
	ca := serverCAPEM(t, srv)

	// Seed the operator stamp-root so reconciles do not create it mid-flow (keeps
	// write-ordering assertions clean). Its auto-creation is covered separately.
	objs = append(objs, makeStampRoot())

	cl := fake.NewClientBuilder().
		WithScheme(sch).
		WithObjects(objs...).
		WithStatusSubresource(&hikyov1.HikyoSecret{}).
		WithInterceptorFuncs(interceptors).
		Build()

	rec := record.NewFakeRecorder(200)
	minter := &stubMinter{token: "fed-token-abc"}
	r := &HikyoSecretReconciler{
		Client: cl,
		// The fake client serves both cached and uncached reads; wiring it as the
		// Reader mirrors production's mgr.GetAPIReader() so Secret/SA reads work.
		Reader:   cl,
		Scheme:   sch,
		Recorder: rec,
		Config:   Config{OwnNamespace: testOwnNS, TriggerRollouts: true, NativeSecretTypes: true},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		NewClientForURL: func(rawURL string, _ []byte) (deliveryClient, error) {
			return opclient.NewClient(srv.URL, ca, "hikyo-operator/test")
		},
		TokenMinter: minter,
		// Reporting is on across the whole suite, so every scenario's reports
		// pass the value-free check below.
		reporter: newReporterState(testClusterID, testReporterVersion),
	}
	h := &harness{t: t, scheme: sch, cl: cl, r: r, stub: stub, server: srv, recorder: rec, events: rec.Events, minter: minter,
		clock: testClock, denied: map[string]bool{}}
	r.now = func() time.Time { return h.clock }
	t.Cleanup(h.requireValueFreeReports)
	return h
}

func (h *harness) reconcile(name string) (ctrl.Result, error) {
	h.t.Helper()
	res, err := h.r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNS, Name: name},
	})
	h.collectDenied(name)
	return res, err
}

// collectDenied records every string of the CR's status and mapping, and every
// credential presented so far, that a report must never carry.
func (h *harness) collectDenied(name string) {
	var cr hikyov1.HikyoSecret
	if err := h.cl.Get(context.Background(), types.NamespacedName{Namespace: testNS, Name: name}, &cr); err == nil {
		for _, c := range cr.Status.Conditions {
			h.denied[c.Message] = true
		}
		for _, m := range cr.Spec.Mapping {
			// Quoted: a key name must never appear as a JSON string.
			h.denied[strconv.Quote(string(m.Key))] = true
			h.denied[strconv.Quote(m.EffectiveSecretKey())] = true
		}
		for _, v := range []string{cr.Status.Cursor, cr.Status.CursorBinding, cr.Status.Stamp, cr.Status.ManagedSecretUID} {
			h.denied[v] = true
		}
	}
	h.stub.mu.Lock()
	defer h.stub.mu.Unlock()
	for _, b := range slices.Concat(h.stub.bearers, h.stub.reportBearers) {
		h.denied[b] = true
	}
}

// requireValueFreeReports fails the test if any report or tombstone body sent
// during it is not the closed contract shape, or carries a denied string.
func (h *harness) requireValueFreeReports() {
	h.stub.mu.Lock()
	reports, tombstones := slices.Clone(h.stub.reports), slices.Clone(h.stub.tombstones)
	h.stub.mu.Unlock()
	denied := slices.Collect(maps.Keys(h.denied))

	for _, v := range valueFreeViolations(reports, tombstones, denied) {
		h.t.Error(v)
	}
}

// valueFreeViolations decodes every body strictly into its contract type and
// reports any denied string found in its bytes.
func valueFreeViolations(reports, tombstones [][]byte, denied []string) []string {
	var out []string
	check := func(kind string, body []byte, into any) {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(into); err != nil {
			out = append(out, fmt.Sprintf("%s body is not the contract shape: %v: %s", kind, err, body))
		}
		for _, d := range denied {
			if d != "" && bytes.Contains(body, []byte(d)) {
				out = append(out, fmt.Sprintf("%s body carries %q: %s", kind, d, body))
			}
		}
	}
	for _, b := range reports {
		var r apigen.DeliveryTargetReportRequest
		check("report", b, &r)
	}
	for _, b := range tombstones {
		var r apigen.DeliveryTargetTombstoneRequest
		check("tombstone", b, &r)
	}
	return out
}

func (h *harness) getCR(name string) *hikyov1.HikyoSecret {
	h.t.Helper()
	var cr hikyov1.HikyoSecret
	if err := h.cl.Get(context.Background(), types.NamespacedName{Namespace: testNS, Name: name}, &cr); err != nil {
		h.t.Fatalf("get CR %q: %v", name, err)
	}
	return &cr
}

func (h *harness) getSecret(ns, name string) (*corev1.Secret, bool) {
	h.t.Helper()
	var sec corev1.Secret
	err := h.cl.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &sec)
	if apierrors.IsNotFound(err) {
		return nil, false
	}
	if err != nil {
		h.t.Fatalf("get secret %s/%s: %v", ns, name, err)
	}
	return &sec, true
}

func (h *harness) getDeployment(name string) *appsv1.Deployment {
	return getWorkload(h, name, "deployment", &appsv1.Deployment{})
}

func (h *harness) getStatefulSet(name string) *appsv1.StatefulSet {
	return getWorkload(h, name, "statefulset", &appsv1.StatefulSet{})
}

func (h *harness) getDaemonSet(name string) *appsv1.DaemonSet {
	return getWorkload(h, name, "daemonset", &appsv1.DaemonSet{})
}

func getWorkload[T client.Object](h *harness, name, kind string, workload T) T {
	h.t.Helper()
	if err := h.cl.Get(context.Background(), types.NamespacedName{Namespace: testNS, Name: name}, workload); err != nil {
		h.t.Fatalf("get %s %q: %v", kind, name, err)
	}
	return workload
}

// drainEvents collects the reasons emitted so far without blocking.
func (h *harness) drainEvents() []string {
	var out []string
	for {
		select {
		case e := <-h.events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func hasEventReason(events []string, reason string) bool {
	for _, e := range events {
		if strings.Contains(e, reason) {
			return true
		}
	}
	return false
}

// ---- object builders ----

func makeInstance(audience string) *hikyov1.HikyoInstance {
	return &hikyov1.HikyoInstance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstance, UID: "inst-uid-1"},
		Spec:       hikyov1.HikyoInstanceSpec{URL: "https://placeholder.invalid", Audience: audience},
	}
}

type crOpt func(*hikyov1.HikyoSecret)

func withMapping(pairs ...[2]string) crOpt {
	return func(cr *hikyov1.HikyoSecret) {
		cr.Spec.Mapping = nil
		for _, p := range pairs {
			cr.Spec.Mapping = append(cr.Spec.Mapping, hikyov1.Mapping{Key: hikyov1.KeyName(p[0]), SecretKey: p[1]})
		}
	}
}

func withSA(name string) crOpt {
	return func(cr *hikyov1.HikyoSecret) {
		cr.Spec.Auth = hikyov1.AuthRef{ServiceAccountRef: &hikyov1.LocalObjectRef{Name: name}}
	}
}

func withProjection(p hikyov1.Projection) crOpt {
	return func(cr *hikyov1.HikyoSecret) { cr.Spec.Projection = p }
}

func withPolicy(p hikyov1.CreationPolicy) crOpt {
	return func(cr *hikyov1.HikyoSecret) { cr.Spec.Target.CreationPolicy = p }
}

func withCreation(ts time.Time, uid string) crOpt {
	return func(cr *hikyov1.HikyoSecret) {
		cr.CreationTimestamp = metav1.NewTime(ts)
		cr.UID = types.UID(uid)
	}
}

func makeCR(name string, opts ...crOpt) *hikyov1.HikyoSecret {
	cr := &hikyov1.HikyoSecret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNS, Name: name, UID: types.UID("cr-uid-" + name),
			CreationTimestamp: metav1.NewTime(testClock),
		},
		Spec: hikyov1.HikyoSecretSpec{
			InstanceRef: hikyov1.InstanceRef{Name: testInstance},
			Auth:        hikyov1.AuthRef{SecretRef: &hikyov1.LocalObjectRef{Name: "boot"}},
			Scope:       hikyov1.Scope{Org: "acme", Project: "web", Environment: "prod"},
			Mapping:     []hikyov1.Mapping{{Key: "API_KEY", SecretKey: "API_KEY"}},
			Target:      hikyov1.Target{Name: testTarget, CreationPolicy: hikyov1.CreationPolicyOwner},
			Projection:  hikyov1.ProjectionFull,
		},
	}
	for _, o := range opts {
		o(cr)
	}
	return cr
}

// makeBootstrapSecret builds a designated (or not) bootstrap Secret.
func makeBootstrapSecret(name, instanceLabel, token string, designate bool) *corev1.Secret {
	labels := map[string]string{}
	if designate {
		labels[hikyov1.LabelDelivery] = hikyov1.LabelDeliveryValue
		labels[hikyov1.LabelInstance] = instanceLabel
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: name, UID: types.UID("boot-uid-" + name), Labels: labels},
		Data:       map[string][]byte{hikyov1.BootstrapTokenKey: []byte(token)},
	}
}

func makeStampRoot() *corev1.Secret {
	root := make([]byte, 32)
	for i := range root {
		root[i] = byte(i + 1)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testOwnNS, Name: hikyov1.StampRootSecretName, UID: "stamp-root-uid"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{hikyov1.StampRootKey: root},
	}
}

func makeServiceAccount(name, instanceLabel string, designate bool) *corev1.ServiceAccount {
	labels := map[string]string{}
	if designate {
		labels[hikyov1.LabelDelivery] = hikyov1.LabelDeliveryValue
		labels[hikyov1.LabelInstance] = instanceLabel
	}
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: name, UID: types.UID("sa-uid-" + name), Labels: labels},
	}
}

// makeOwnedSecret builds a managed Secret already controlled by cr, with data.
func makeOwnedSecret(t *testing.T, sch *runtime.Scheme, cr *hikyov1.HikyoSecret, data map[string][]byte) *corev1.Secret {
	t.Helper()
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: cr.Spec.Target.Name, UID: "managed-uid-1"},
		Type:       corev1.SecretTypeOpaque,
		Data:       data,
	}
	if err := ctrl.SetControllerReference(cr, sec, sch); err != nil {
		t.Fatalf("set controller ref: %v", err)
	}
	return sec
}

// makeOptedInDeployment builds a Deployment consuming the target (opt-in).
func makeOptedInDeployment(name string, consumesTargets ...string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: name, Annotations: workloadAnnotations(consumesTargets)},
		Spec: appsv1.DeploymentSpec{
			Template: emptyPodTemplate(),
		},
	}
}

func makeOptedInStatefulSet(name string, consumesTargets ...string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: name, Annotations: workloadAnnotations(consumesTargets)},
		Spec: appsv1.StatefulSetSpec{
			Template: emptyPodTemplate(),
		},
	}
}

func makeOptedInDaemonSet(name string, consumesTargets ...string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: name, Annotations: workloadAnnotations(consumesTargets)},
		Spec: appsv1.DaemonSetSpec{
			Template: emptyPodTemplate(),
		},
	}
}

func workloadAnnotations(consumesTargets []string) map[string]string {
	annotations := map[string]string{}
	if len(consumesTargets) > 0 {
		annotations[hikyov1.AnnotationWorkloadSecrets] = strings.Join(consumesTargets, ",")
	}
	return annotations
}

func emptyPodTemplate() corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{}}
}

// deliveryJSON builds a 200 response body.
func deliveryJSON(current bool, cursor, changeToken string, keys []deliveredKey, credExpiresAt *time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"current":%t,"cursor":%q,"change_token":%q,"schema_revision":3,"revision":1,"pin_expired":false`, current, cursor, changeToken)
	if credExpiresAt != nil {
		fmt.Fprintf(&b, `,"credential_expires_at":%q`, credExpiresAt.Format(time.RFC3339))
	}
	b.WriteString(`,"keys":[`)
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"name":%q,"classification":%q,"presence":"set"`, k.name, k.classification)
		if k.hasValue {
			fmt.Fprintf(&b, `,"value":%q`, k.value)
		}
		b.WriteString("}")
	}
	b.WriteString("]}")
	return b.String()
}

type deliveredKey struct {
	name           string
	classification string
	value          string
	hasValue       bool
}

func configVal(name, value string) deliveredKey {
	return deliveredKey{name: name, classification: "config", value: value, hasValue: true}
}
func secretVal(name, value string) deliveredKey {
	return deliveredKey{name: name, classification: "secret", value: value, hasValue: true}
}
func secretPresenceOnly(name string) deliveredKey {
	return deliveredKey{name: name, classification: "secret", hasValue: false}
}

func condStatus(cr *hikyov1.HikyoSecret, condType string) (metav1.ConditionStatus, string, bool) {
	for _, c := range cr.Status.Conditions {
		if c.Type == condType {
			return c.Status, c.Reason, true
		}
	}
	return "", "", false
}

func requireCond(t *testing.T, cr *hikyov1.HikyoSecret, condType string, status metav1.ConditionStatus, reason string) {
	t.Helper()
	s, r, ok := condStatus(cr, condType)
	if !ok {
		t.Fatalf("condition %q absent; conditions=%v", condType, cr.Status.Conditions)
	}
	if s != status || r != reason {
		t.Fatalf("condition %q = (%s/%s), want (%s/%s)", condType, s, r, status, reason)
	}
}

func hasControllerRef(sec *corev1.Secret, cr *hikyov1.HikyoSecret) bool {
	return metav1.IsControlledBy(sec, cr)
}

func stampAnnotation(d *appsv1.Deployment) string {
	if d.Spec.Template.Annotations == nil {
		return ""
	}
	return d.Spec.Template.Annotations[hikyov1.StampAnnotationPrefix+testTarget]
}

// secretWrites counts Create/Update/Patch calls against the managed target
// Secret, so a test can assert "no write at all" rather than inferring it from
// final state (a metadata-only write or a failed attempt is invisible to a
// state check).
type secretWrites struct{ n int }

func (w *secretWrites) interceptors() interceptor.Funcs {
	hit := func(obj client.Object) {
		if s, ok := obj.(*corev1.Secret); ok && s.Namespace == testNS && s.Name == testTarget {
			w.n++
		}
	}
	return interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			hit(obj)
			return c.Create(ctx, obj, opts...)
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			hit(obj)
			return c.Update(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			hit(obj)
			return c.Patch(ctx, obj, patch, opts...)
		},
	}
}
