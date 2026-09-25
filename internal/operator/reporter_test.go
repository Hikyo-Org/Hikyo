package operator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/deliverytarget"
	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

// decodeReports decodes every report body the stub received.
func decodeReports(t *testing.T, h *harness) []apigen.DeliveryTargetReportRequest {
	t.Helper()
	var out []apigen.DeliveryTargetReportRequest
	for _, b := range h.stub.reportBodies() {
		var r apigen.DeliveryTargetReportRequest
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatalf("decode report: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func deliveredHarness(t *testing.T, interceptors interceptor.Funcs, crOpts ...crOpt) *harness {
	t.Helper()
	h := newHarness(t, interceptors,
		makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok-report-bearer", true), makeCR("app", crOpts...))
	h.stub.set(200, deliveryJSON(false, "v1:cur1", "v1:t", []deliveredKey{secretVal("API_KEY", "v")}, nil))
	return h
}

func TestReportFollowsStatusWriteWithClosedPayload(t *testing.T) {
	var h *harness
	h = deliveredHarness(t, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			h.stub.record("status")
			return c.SubResource(sub).Update(ctx, obj, opts...)
		},
	})
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if want := []string{"status", "report"}; !slices.Equal(h.stub.events, want) {
		t.Fatalf("write order = %v, want %v", h.stub.events, want)
	}
	reports := decodeReports(t, h)
	r := reports[0]
	cr := h.getCR("app")
	if r.Vocabulary != 1 || r.Target.ClusterId != testClusterID || r.Target.InstanceUid != "inst-uid-1" ||
		r.Target.Namespace != testNS || r.Target.Name != "app" || r.Target.Uid != string(cr.UID) {
		t.Fatalf("report identity = %+v", r)
	}
	if r.Lifecycle != apigen.DeliveryTargetLifecycleSynced || r.ReportIntervalSeconds != 300 ||
		r.Reporter.Integration != apigen.KubernetesOperator || r.Reporter.Version != testReporterVersion ||
		!r.ReportedAt.Equal(testClock) {
		t.Fatalf("report body = %+v", r)
	}
	var want []apigen.DeliveryTargetCondition
	for _, c := range reportConditions(cr) {
		want = append(want, apigen.DeliveryTargetCondition{
			Type: c.Type, Status: apigen.DeliveryTargetConditionStatus(c.Status), Reason: c.Reason, ObservedGeneration: c.ObservedGeneration,
		})
	}
	if len(want) == 0 || !slices.Equal(r.Conditions, want) {
		t.Fatalf("report conditions = %+v, want the CR's projected %+v", r.Conditions, want)
	}
	if h.stub.reportBearers[0] != "tok-report-bearer" {
		t.Fatalf("report bearer = %q, want the CR's own credential", h.stub.reportBearers[0])
	}
}

func TestReportValueFreeForUndeliveredKeyNames(t *testing.T) {
	cr := makeCR("app", withMapping([2]string{"API_KEY", "API_KEY"}, [2]string{"HIDDEN_DATABASE_PASSWORD", "HIDDEN_DATABASE_PASSWORD"}))
	h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
	h.stub.set(200, deliveryJSON(false, "v1:c", "v1:t",
		[]deliveredKey{secretVal("API_KEY", "v"), secretPresenceOnly("HIDDEN_DATABASE_PASSWORD")}, nil))
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	requireCond(t, h.getCR("app"), hikyov1.ConditionDelivery, metav1.ConditionFalse, hikyov1.ReasonUndeliveredSecrets)
	bodies := h.stub.reportBodies()
	if len(bodies) != 1 {
		t.Fatalf("reports = %d, want 1", len(bodies))
	}
	if strings.Contains(string(bodies[0]), "HIDDEN_DATABASE_PASSWORD") || strings.Contains(string(bodies[0]), "machine-reveal") {
		t.Fatalf("report leaked a key name or message: %s", bodies[0])
	}
}

func TestValueFreeCheckCatchesALeak(t *testing.T) {
	leak := []byte(`{"vocabulary":1,"target":{"cluster_id":"c","instance_uid":"i","namespace":"n","name":"API_KEY","uid":"u"},` +
		`"generation":1,"observed_generation":1,"reported_at":"2026-08-19T12:00:00Z","report_interval_seconds":300,` +
		`"lifecycle":"Synced","conditions":[],"reporter":{"integration":"kubernetes-operator","version":"1.0.0"}}`)
	if got := valueFreeViolations([][]byte{leak}, nil, []string{`"API_KEY"`}); len(got) != 1 {
		t.Fatalf("violations = %v, want the planted key name", got)
	}
	extra := []byte(`{"target":{"cluster_id":"c","instance_uid":"i","uid":"u"},"message":"x"}`)
	if got := valueFreeViolations(nil, [][]byte{extra}, nil); len(got) != 1 {
		t.Fatalf("violations = %v, want the unknown member refused", got)
	}
}

// reconcileState is everything reporting must never change.
type reconcileState struct {
	results []ctrl.Result
	errs    []string
	status  string
	secret  string
}

func runReportScenario(t *testing.T, configure func(*harness)) (reconcileState, *harness) {
	t.Helper()
	h := deliveredHarness(t, interceptor.Funcs{})
	configure(h)
	var st reconcileState
	step := func() {
		res, err := h.reconcile("app")
		st.results = append(st.results, res)
		if err != nil {
			st.errs = append(st.errs, err.Error())
		} else {
			st.errs = append(st.errs, "")
		}
		h.clock = h.clock.Add(6 * time.Minute)
	}
	step()
	h.stub.set(200, deliveryJSON(true, "v1:cur1", "v1:t", nil, nil))
	step()
	// A failed fetch writes no cursor; the report still follows the status write.
	h.stub.set(http.StatusServiceUnavailable, "")
	step()
	cr := h.getCR("app")
	// lastTransitionTime is the wall clock (meta.SetStatusCondition), not the
	// injected one; everything else must match byte for byte.
	for i := range cr.Status.Conditions {
		cr.Status.Conditions[i].LastTransitionTime = metav1.Time{}
	}
	status, err := json.Marshal(cr.Status)
	if err != nil {
		t.Fatal(err)
	}
	st.status = string(status)
	sec, ok := h.getSecret(testNS, testTarget)
	if !ok {
		t.Fatal("managed Secret absent")
	}
	secret, err := json.Marshal(struct {
		Data            map[string][]byte
		OwnerReferences []metav1.OwnerReference
		ResourceVersion string
	}{sec.Data, sec.OwnerReferences, sec.ResourceVersion})
	if err != nil {
		t.Fatal(err)
	}
	st.secret = string(secret)
	return st, h
}

func TestReportingFailureLeavesReconcileByteIdentical(t *testing.T) {
	baseline, base := runReportScenario(t, func(h *harness) { h.stub.meta = oldServerMeta })
	if n := len(base.stub.reportBodies()); n != 0 {
		t.Fatalf("baseline sent %d reports", n)
	}
	if !strings.Contains(baseline.status, `"cursor":"v1:cur1"`) {
		t.Fatalf("baseline scenario did not deliver: %s", baseline.status)
	}
	for name, configure := range map[string]func(*harness){
		"timeout": func(h *harness) {
			h.r.reporter.timeout = 50 * time.Millisecond
			h.stub.reportDelay = 2 * time.Second
		},
		"404": func(h *harness) { h.stub.reportStatus = func(apigen.DeliveryTargetReportRequest) int { return 404 } },
		"409": func(h *harness) { h.stub.reportStatus = func(apigen.DeliveryTargetReportRequest) int { return 409 } },
		"422": func(h *harness) { h.stub.reportStatus = func(apigen.DeliveryTargetReportRequest) int { return 422 } },
		"429": func(h *harness) { h.stub.reportStatus = func(apigen.DeliveryTargetReportRequest) int { return 429 } },
	} {
		t.Run(name, func(t *testing.T) {
			got, h := runReportScenario(t, configure)
			if len(h.stub.reportBodies()) == 0 {
				t.Fatal("no report attempted")
			}
			if !hasEventReason(h.drainEvents(), ReasonStatusReportFailed) {
				t.Error("reporting failure emitted no event")
			}
			if hasConditionTypeContaining(h.getCR("app"), "Report") {
				t.Error("reporting failure wrote a condition")
			}
			if !slices.Equal(got.results, baseline.results) || !slices.Equal(got.errs, baseline.errs) {
				t.Fatalf("results = %v %v, want %v %v", got.results, got.errs, baseline.results, baseline.errs)
			}
			if got.status != baseline.status {
				t.Fatalf("status differs:\n got %s\nwant %s", got.status, baseline.status)
			}
			if got.secret != baseline.secret {
				t.Fatalf("managed Secret differs:\n got %s\nwant %s", got.secret, baseline.secret)
			}
		})
	}
}

// hasConditionTypeContaining reports whether any condition type contains the fragment.
func hasConditionTypeContaining(cr *hikyov1.HikyoSecret, fragment string) bool {
	return slices.ContainsFunc(cr.Status.Conditions, func(c metav1.Condition) bool { return strings.Contains(c.Type, fragment) })
}

func TestOldServerSendsNoReports(t *testing.T) {
	h := deliveredHarness(t, interceptor.Funcs{})
	h.stub.meta = oldServerMeta
	for range 3 {
		if _, err := h.reconcile("app"); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		h.clock = h.clock.Add(11 * time.Minute)
	}
	if n := len(h.stub.reportBodies()); n != 0 {
		t.Fatalf("old server received %d reports", n)
	}
	if hasEventReason(h.drainEvents(), ReasonStatusReportFailed) {
		t.Fatal("capability absence emitted a failure event")
	}
	if h.stub.metaRequests != 3 {
		t.Fatalf("meta probes = %d, want one per 10 min cache window", h.stub.metaRequests)
	}
}

func TestCapabilityProbeReusedWithinWindow(t *testing.T) {
	h := deliveredHarness(t, interceptor.Funcs{})
	for range 3 {
		if _, err := h.reconcile("app"); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		h.clock = h.clock.Add(time.Minute)
	}
	if h.stub.metaRequests != 1 {
		t.Fatalf("meta probes = %d, want 1 within the cache window", h.stub.metaRequests)
	}
}

func TestReport404SuppressesOnlyThatCR(t *testing.T) {
	a := makeCR("a", func(cr *hikyov1.HikyoSecret) { cr.Spec.Target.Name = "a-secret" })
	b := makeCR("b", func(cr *hikyov1.HikyoSecret) { cr.Spec.Target.Name = "b-secret" })
	h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), a, b)
	h.stub.set(200, deliveryJSON(false, "v1:c", "v1:t", []deliveredKey{secretVal("API_KEY", "v")}, nil))
	h.stub.reportStatus = func(r apigen.DeliveryTargetReportRequest) int {
		if r.Target.Name == "a" {
			return http.StatusNotFound
		}
		return http.StatusNoContent
	}
	counts := func() map[string]int {
		out := map[string]int{}
		for _, r := range decodeReports(t, h) {
			out[r.Target.Name]++
		}
		return out
	}
	both := func() {
		for _, name := range []string{"a", "b"} {
			if _, err := h.reconcile(name); err != nil {
				t.Fatalf("reconcile %s: %v", name, err)
			}
		}
	}
	both()
	// Heartbeats due for both; a stays suppressed, b is unaffected.
	for range 2 {
		h.clock = h.clock.Add(6 * time.Minute)
		both()
	}
	if got := counts(); got["a"] != 1 || got["b"] != 3 {
		t.Fatalf("reports per CR = %v, want a suppressed after one 404 and b every heartbeat", got)
	}
	// After an hour a reports again.
	h.clock = h.clock.Add(time.Hour)
	both()
	if got := counts(); got["a"] != 2 {
		t.Fatalf("reports per CR after the hour = %v, want a resumed", got)
	}
}

func TestSuppressionLiftedByGenerationChange(t *testing.T) {
	h := deliveredHarness(t, interceptor.Funcs{})
	h.stub.reportStatus = func(apigen.DeliveryTargetReportRequest) int { return http.StatusUnprocessableEntity }
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	h.clock = h.clock.Add(6 * time.Minute)
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n := len(h.stub.reportBodies()); n != 1 {
		t.Fatalf("reports = %d, want 1 while suppressed", n)
	}
	cr := h.getCR("app")
	cr.Generation++
	if err := h.cl.Update(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n := len(h.stub.reportBodies()); n != 2 {
		t.Fatalf("reports = %d, want a report once the generation moved", n)
	}
}

func TestSuppressionAfter401LiftedBySuccessfulFetch(t *testing.T) {
	h := deliveredHarness(t, interceptor.Funcs{})
	h.stub.reportStatus = func(apigen.DeliveryTargetReportRequest) int { return http.StatusUnauthorized }
	h.stub.set(http.StatusUnauthorized, "")
	if _, err := h.reconcile("app"); err == nil {
		t.Fatal("a 401 fetch must fail the reconcile")
	}
	h.clock = h.clock.Add(6 * time.Minute)
	if _, err := h.reconcile("app"); err == nil {
		t.Fatal("a 401 fetch must fail the reconcile")
	}
	if n := len(h.stub.reportBodies()); n != 1 {
		t.Fatalf("reports = %d, want 1 while the credential is refused", n)
	}
	h.stub.reportStatus = nil
	h.stub.set(200, deliveryJSON(false, "v1:cur1", "v1:t", []deliveredKey{secretVal("API_KEY", "v")}, nil))
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n := len(h.stub.reportBodies()); n != 2 {
		t.Fatalf("reports = %d, want reporting resumed after the fetch succeeded", n)
	}
}

func TestSuppressionAfter401LiftedByRotatedCredential(t *testing.T) {
	h := deliveredHarness(t, interceptor.Funcs{})
	h.stub.reportStatus = func(apigen.DeliveryTargetReportRequest) int { return http.StatusUnauthorized }
	h.stub.set(http.StatusUnauthorized, "")
	if _, err := h.reconcile("app"); err == nil {
		t.Fatal("a 401 fetch must fail the reconcile")
	}
	// Same name, new data: a rotated credential under the same reference.
	var boot corev1.Secret
	if err := h.cl.Get(context.Background(), types.NamespacedName{Namespace: testNS, Name: "boot"}, &boot); err != nil {
		t.Fatal(err)
	}
	boot.Data[hikyov1.BootstrapTokenKey] = []byte("tok-rotated-bearer")
	if err := h.cl.Update(context.Background(), &boot); err != nil {
		t.Fatal(err)
	}
	h.stub.reportStatus = nil
	h.clock = h.clock.Add(time.Minute)
	_, _ = h.reconcile("app")
	if got := h.stub.reportBearers; len(got) != 2 || got[1] != "tok-rotated-bearer" {
		t.Fatalf("report bearers = %v, want reporting resumed under the rotated credential", got)
	}
}

func TestBuiltReportMeetsServerContract(t *testing.T) {
	inst := makeInstance("")
	inst.UID = "5e1d2c3b-4a59-4687-9a1b-2c3d4e5f6a7b"
	cr := makeCR("app")
	cr.UID, cr.Generation = "7c6b5a49-3827-4165-8f4e-3d2c1b0a9f8e", 1
	h := newHarness(t, interceptor.Funcs{}, inst, makeBootstrapSecret("boot", testInstance, "tok", true), cr)
	h.stub.set(200, deliveryJSON(false, "v1:c", "v1:t", []deliveredKey{secretVal("API_KEY", "v")}, nil))
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	h.r.reporter.version = "1.4.0-rc.1+build.7"
	cr = h.getCR("app")
	cr.Status.Conditions = append(cr.Status.Conditions, metav1.Condition{Type: hikyov1.ConditionRollout, Status: metav1.ConditionFalse, Reason: hikyov1.ReasonStalled, ObservedGeneration: 1})
	body := h.r.reporter.buildReport(cr, inst, 1, reportConditions(cr), h.r.reportInterval(cr))
	body.ReportedAt = testClock
	for _, r := range append(decodeReports(t, h), body) {
		report := deliverytarget.Report{
			Vocabulary: r.Vocabulary,
			Target: deliverytarget.Target{
				ClusterID: r.Target.ClusterId, InstanceUID: r.Target.InstanceUid,
				Namespace: r.Target.Namespace, Name: r.Target.Name, UID: r.Target.Uid,
			},
			Generation: r.Generation, ObservedGeneration: r.ObservedGeneration, ReportedAt: r.ReportedAt,
			ReportIntervalSeconds: r.ReportIntervalSeconds, Lifecycle: string(r.Lifecycle),
			Reporter: string(r.Reporter.Integration), ReporterVersion: r.Reporter.Version,
		}
		for _, c := range r.Conditions {
			report.Conditions = append(report.Conditions, deliverytarget.Condition{
				Type: c.Type, Status: string(c.Status), Reason: c.Reason, ObservedGeneration: c.ObservedGeneration,
			})
		}
		if err := deliverytarget.CheckShape(report); err != nil {
			t.Fatalf("server would refuse the report shape: %v (%+v)", err, report)
		}
		if err := deliverytarget.CheckVocabulary(report); err != nil {
			t.Fatalf("server would refuse the report vocabulary: %v", err)
		}
	}
}

func TestHeartbeatFloorWithShortResync(t *testing.T) {
	h := deliveredHarness(t, interceptor.Funcs{}, func(cr *hikyov1.HikyoSecret) { cr.Spec.ResyncInterval = "30s" })
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Reach steady state: Delivered then Current is a real content change.
	h.stub.set(200, deliveryJSON(true, "v1:cur1", "v1:t", nil, nil))
	h.clock = h.clock.Add(30 * time.Second)
	res, err := h.reconcile("app")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != 30*time.Second {
		t.Fatalf("requeue = %v, want the 30s resync untouched", res.RequeueAfter)
	}
	steady := len(h.stub.reportBodies())
	for range 20 { // ten minutes of 30 s resyncs
		h.clock = h.clock.Add(30 * time.Second)
		if _, err := h.reconcile("app"); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}
	if heartbeats := len(h.stub.reportBodies()) - steady; heartbeats > 2 {
		t.Fatalf("heartbeats over 10 min = %d, want at most one per 5 min", heartbeats)
	}
	if r := decodeReports(t, h)[0]; r.ReportIntervalSeconds != 300 {
		t.Fatalf("report_interval_seconds = %d, want the 5 min floor", r.ReportIntervalSeconds)
	}
}

func TestLongResyncRequeuesWithin24h(t *testing.T) {
	long := func(cr *hikyov1.HikyoSecret) { cr.Spec.ResyncInterval = "72h" }
	h := deliveredHarness(t, interceptor.Funcs{}, long)
	res, err := h.reconcile("app")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != 24*time.Hour {
		t.Fatalf("requeue = %v, want 24h while reporting is enabled", res.RequeueAfter)
	}
	if r := decodeReports(t, h)[0]; r.ReportIntervalSeconds != 86400 {
		t.Fatalf("report_interval_seconds = %d, want the 24 h cap", r.ReportIntervalSeconds)
	}

	// ADR D9: "with reporting enabled the operator requeues at
	// min(spec.resyncInterval, 24 h)". Enabled is the operator setting, not
	// the server's answer, so capability absence and a failed probe clamp too.
	for name, meta := range map[string]string{"capability absent": oldServerMeta, "probe failure": "{"} {
		h := deliveredHarness(t, interceptor.Funcs{}, long)
		h.stub.meta = meta
		if res, err := h.reconcile("app"); err != nil || res.RequeueAfter != 24*time.Hour {
			t.Fatalf("%s: requeue = %v (err %v), want 24h while reporting is enabled", name, res.RequeueAfter, err)
		}
	}

	disabled := deliveredHarness(t, interceptor.Funcs{}, long)
	disabled.r.reporter = nil
	if res, err := disabled.reconcile("app"); err != nil || res.RequeueAfter != 72*time.Hour {
		t.Fatalf("reporting disabled: requeue = %v (err %v), want the 72h resync", res.RequeueAfter, err)
	}
}

func TestConditionOutsideVocabularySkipsWholeReport(t *testing.T) {
	cr := makeCR("app")
	cr.Status.Conditions = []metav1.Condition{{Type: "FutureCondition", Status: metav1.ConditionTrue, Reason: "Novel", Message: "m"}}
	h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
	h.stub.set(200, deliveryJSON(false, "v1:c", "v1:t", []deliveredKey{secretVal("API_KEY", "v")}, nil))
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n := len(h.stub.reportBodies()); n != 0 {
		t.Fatalf("reports = %d, want the whole report skipped", n)
	}
	if !hasEventReason(h.drainEvents(), ReasonStatusReportSkipped) {
		t.Fatal("skipped report emitted no event")
	}
	requireCond(t, h.getCR("app"), hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
}

func TestStatusWriteFailureSendsNoReport(t *testing.T) {
	h := deliveredHarness(t, interceptor.Funcs{
		SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
			return errors.New("status write refused")
		},
	})
	if _, err := h.reconcile("app"); err == nil {
		t.Fatal("a failed status write must fail the reconcile")
	}
	if n := len(h.stub.reportBodies()); n != 0 {
		t.Fatalf("reports = %d, want none for an unwritten status", n)
	}
}

func TestFederationTokenReusedForReport(t *testing.T) {
	cr := makeCR("app", withSA("worker"))
	h := newHarness(t, interceptor.Funcs{}, makeInstance("aud"), makeServiceAccount("worker", testInstance, true), cr)
	h.stub.set(200, deliveryJSON(false, "v1:c", "v1:t", []deliveredKey{secretVal("API_KEY", "v")}, nil))
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if h.minter.calls != 1 {
		t.Fatalf("minted %d tokens, want the fetch token reused", h.minter.calls)
	}
	if got := h.stub.reportBearers; len(got) != 1 || got[0] != "fed-token-abc" {
		t.Fatalf("report bearers = %v", got)
	}
}

func TestTombstoneOnceOnDeletion(t *testing.T) {
	h := deliveredHarness(t, interceptor.Funcs{})
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	cr := h.getCR("app")
	if len(cr.Finalizers) != 0 {
		t.Fatalf("reporting added finalizers %v", cr.Finalizers)
	}
	if err := h.cl.Delete(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := h.reconcile("app"); err != nil {
			t.Fatalf("reconcile after delete: %v", err)
		}
	}
	if len(h.stub.tombstones) != 1 {
		t.Fatalf("tombstones = %d, want exactly one", len(h.stub.tombstones))
	}
	var body apigen.DeliveryTargetTombstoneRequest
	if err := json.Unmarshal(h.stub.tombstones[0], &body); err != nil {
		t.Fatal(err)
	}
	if body.Target != (apigen.DeliveryTargetIdentity{ClusterId: testClusterID, InstanceUid: "inst-uid-1", Uid: string(cr.UID)}) {
		t.Fatalf("tombstone target = %+v", body.Target)
	}
	if last := h.stub.reportBearers[len(h.stub.reportBearers)-1]; last != "tok-report-bearer" {
		t.Fatalf("tombstone bearer = %q", last)
	}
}

func TestTombstoneFailureIsAnEventOnly(t *testing.T) {
	h := deliveredHarness(t, interceptor.Funcs{})
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// The bootstrap Secret goes with the CR: no credential, no tombstone.
	var boot corev1.Secret
	if err := h.cl.Get(context.Background(), types.NamespacedName{Namespace: testNS, Name: "boot"}, &boot); err != nil {
		t.Fatal(err)
	}
	if err := h.cl.Delete(context.Background(), &boot); err != nil {
		t.Fatal(err)
	}
	if err := h.cl.Delete(context.Background(), h.getCR("app")); err != nil {
		t.Fatal(err)
	}
	h.drainEvents()
	if _, err := h.reconcile("app"); err != nil {
		t.Fatalf("a failed tombstone must not fail the reconcile: %v", err)
	}
	if len(h.stub.tombstones) != 0 {
		t.Fatal("tombstone sent without a credential")
	}
	if !hasEventReason(h.drainEvents(), ReasonStatusReportFailed) {
		t.Fatal("failed tombstone emitted no event")
	}
}

func TestNewStatusReporterReadsClusterID(t *testing.T) {
	sch := testScheme(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: testClusterID}}
	rep, err := newStatusReporter(context.Background(), fake.NewClientBuilder().WithScheme(sch).WithObjects(ns).Build(), "1.2.3")
	if err != nil {
		t.Fatalf("newStatusReporter: %v", err)
	}
	if rep.clusterID != testClusterID || rep.version != "1.2.3" || rep.timeout != 5*time.Second {
		t.Fatalf("reporter = %+v", rep)
	}
	if _, err := newStatusReporter(context.Background(), fake.NewClientBuilder().WithScheme(sch).Build(), "1.2.3"); err == nil {
		t.Fatal("an unreadable cluster id must be a hard error")
	}
	for _, version := range []string{"dev", "v1.2.3", ""} {
		_, err := newStatusReporter(context.Background(), fake.NewClientBuilder().WithScheme(sch).WithObjects(ns).Build(), version)
		if err == nil || !strings.Contains(err.Error(), "HIKYO_OPERATOR_STATUS_REPORTING=false") {
			t.Fatalf("version %q: err = %v, want a refusal naming the remedy", version, err)
		}
	}
}
