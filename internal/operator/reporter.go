package operator

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/deliverytarget"
	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

// Delivery-target condition reporting (k8s-condition-reporting ADR D3, D4,
// D9, D10). Reporting is fire-and-forget beside the reconcile: it runs only
// after the status subresource write, never gates or requeues it, and never
// writes a condition. Its outcome is an Event at most.
const (
	reportTimeout       = 5 * time.Second
	capabilityCacheTTL  = 10 * time.Minute
	reportSuppression   = time.Hour
	reportEventInterval = time.Hour
	clusterIDNamespace  = "kube-system"

	// Event reasons for reporting. They are Event reasons only, never
	// condition reasons: reporting about reporting would perturb Ready.
	ReasonStatusReportFailed  = "StatusReportFailed"
	ReasonStatusReportSkipped = "StatusReportSkipped"
)

// statusReporter holds the in-memory reporting state. Nothing is written back
// to the CR (D10: no CRD change); a restart starts from empty state, which
// costs at most one early heartbeat per CR.
type statusReporter struct {
	clusterID string
	version   string
	timeout   time.Duration

	mu           sync.Mutex
	capabilities map[types.UID]capabilityProbe
	targets      map[types.NamespacedName]targetState
}

// capabilityProbe is one cached `/meta` answer per HikyoInstance. vocabulary
// 0 means the capability is absent; err is a failed probe, cached for the
// same window so an unreachable server is probed once per window.
type capabilityProbe struct {
	vocabulary int
	err        error
	at         time.Time
}

// targetState is one CR's reporting state.
type targetState struct {
	uid types.UID
	// Tombstone identity: enough to re-acquire the CR's credential after the
	// CR is gone, since there is no finalizer (D6).
	instance    string
	instanceUID types.UID
	scope       hikyov1.Scope
	auth        hikyov1.AuthRef
	accepted    bool

	// content is the last attempted reportable content (every D4 field but
	// reported_at) and attemptAt its time. A failed attempt counts, so a
	// failing server sees one attempt per heartbeat, never a faster retry.
	content   []byte
	attemptAt time.Time
	suppress  *reportSuppressed
	eventAt   time.Time
}

// reportSuppressed is the per-CR suppression after a 401, 404, 413 or 422
// (D9). It never affects another CR.
type reportSuppressed struct {
	until        time.Time
	generation   int64
	credRef      string
	content      []byte
	unauthorized bool
	credential   string
}

// holds reports whether the suppression still applies: an hour has not
// passed and neither generation, credential reference nor reportable content
// moved, and no fetch under the same credential succeeded after a 401. The
// credential reference includes the referenced object's UID and
// resourceVersion, as the cursor binding does, so a rotated credential lifts
// it.
func (s *reportSuppressed) holds(now time.Time, generation int64, credRef, credential string, content []byte, fetchOK bool) bool {
	return now.Before(s.until) &&
		generation == s.generation &&
		credRef == s.credRef &&
		credential == s.credential &&
		bytes.Equal(content, s.content) &&
		!(s.unauthorized && fetchOK)
}

// newStatusReporter reads the cluster id once at start (D3): the kube-system
// Namespace UID, through the uncached reader.
func newStatusReporter(ctx context.Context, reader client.Reader, version string) (*statusReporter, error) {
	var ns corev1.Namespace
	if err := reader.Get(ctx, types.NamespacedName{Name: clusterIDNamespace}, &ns); err != nil {
		return nil, fmt.Errorf("read the %s Namespace UID as the cluster id (the chart grants get on it while operator.statusReporting is true): %w", clusterIDNamespace, err)
	}
	if !deliverytarget.IsUID(string(ns.UID)) {
		return nil, fmt.Errorf("the %s Namespace UID %q is not a Kubernetes UID", clusterIDNamespace, ns.UID)
	}
	return newReporterState(string(ns.UID), version), nil
}

func newReporterState(clusterID, version string) *statusReporter {
	return &statusReporter{
		clusterID:    clusterID,
		version:      version,
		timeout:      reportTimeout,
		capabilities: map[types.UID]capabilityProbe{},
		targets:      map[types.NamespacedName]targetState{},
	}
}

// reportInput is the per-reconcile context the report needs once the CR's
// credential is acquired: the instance, the credential the fetch used (a
// federation token is reused, never minted twice) and whether that fetch
// authenticated.
type reportInput struct {
	inst    *hikyov1.HikyoInstance
	cred    credential
	fetchOK bool
}

type reportInputKey struct{}

func withReportInput(ctx context.Context, in *reportInput) context.Context {
	return context.WithValue(ctx, reportInputKey{}, in)
}

func reportInputFrom(ctx context.Context) *reportInput {
	in, _ := ctx.Value(reportInputKey{}).(*reportInput)
	return in
}

func (s *statusReporter) load(key types.NamespacedName) (targetState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.targets[key]
	return st, ok
}

func (s *statusReporter) store(key types.NamespacedName, st targetState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets[key] = st
}

func (s *statusReporter) take(key types.NamespacedName) (targetState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.targets[key]
	delete(s.targets, key)
	return st, ok
}

// vocabulary returns the vocabulary to report in for an instance: the highest
// version both the server advertises and this operator knows, or 0 when the
// capability is absent. Capability absence is the only instance-wide switch.
func (s *statusReporter) vocabulary(ctx context.Context, dc deliveryClient, inst *hikyov1.HikyoInstance, now time.Time) (int, error) {
	s.mu.Lock()
	probe, ok := s.capabilities[inst.UID]
	s.mu.Unlock()
	if ok && now.Sub(probe.at) < capabilityCacheTTL {
		return probe.vocabulary, probe.err
	}
	advertised, err := dc.Capabilities(ctx)
	probe = capabilityProbe{err: err, at: now}
	for _, v := range deliverytarget.Vocabularies() {
		if err == nil && slices.Contains(advertised, fmt.Sprintf("%s/%d", deliverytarget.Capability, v)) {
			probe.vocabulary = max(probe.vocabulary, v)
		}
	}
	s.mu.Lock()
	s.capabilities[inst.UID] = probe
	s.mu.Unlock()
	return probe.vocabulary, probe.err
}

// reportInterval is the CR's heartbeat, max(spec.resyncInterval, 5 min)
// capped at 24 h (D9).
func (r *HikyoSecretReconciler) reportInterval(cr *hikyov1.HikyoSecret) time.Duration {
	return min(max(r.resyncResult(cr).RequeueAfter, deliverytarget.MinReportInterval), deliverytarget.MaxReportInterval)
}

// credentialRef names the CR's credential reference, whose change lifts a
// suppression.
func credentialRef(cr *hikyov1.HikyoSecret) string {
	switch {
	case cr.Spec.Auth.SecretRef != nil:
		return "secret/" + cr.Spec.Auth.SecretRef.Name
	case cr.Spec.Auth.ServiceAccountRef != nil:
		return "serviceaccount/" + cr.Spec.Auth.ServiceAccountRef.Name
	default:
		return ""
	}
}

// reportConditions projects the CR's conditions onto the closed D4 shape:
// type, status, reason and observed generation only, sorted by type so the
// reportable content does not depend on condition order.
func reportConditions(cr *hikyov1.HikyoSecret) []deliverytarget.Condition {
	out := make([]deliverytarget.Condition, 0, len(cr.Status.Conditions))
	for _, c := range cr.Status.Conditions {
		out = append(out, deliverytarget.Condition{
			Type: c.Type, Status: string(c.Status), Reason: c.Reason, ObservedGeneration: c.ObservedGeneration,
		})
	}
	slices.SortFunc(out, func(a, b deliverytarget.Condition) int { return cmp.Compare(a.Type, b.Type) })
	return out
}

// buildReport builds the D4 body from the status just written. Every member
// is a closed enum, a UID, a Kubernetes name, a number or the time: there is
// no member that could carry a message, key name, cursor or credential.
func (s *statusReporter) buildReport(cr *hikyov1.HikyoSecret, inst *hikyov1.HikyoInstance, vocabulary int, conditions []deliverytarget.Condition, interval time.Duration) apigen.DeliveryTargetReportRequest {
	wire := make([]apigen.DeliveryTargetCondition, 0, len(conditions))
	for _, c := range conditions {
		wire = append(wire, apigen.DeliveryTargetCondition{
			Type: c.Type, Status: apigen.DeliveryTargetConditionStatus(c.Status), Reason: c.Reason, ObservedGeneration: c.ObservedGeneration,
		})
	}
	return apigen.DeliveryTargetReportRequest{
		Vocabulary: vocabulary,
		Target: apigen.DeliveryTargetRef{
			ClusterId: s.clusterID, InstanceUid: string(inst.UID),
			Namespace: cr.Namespace, Name: cr.Name, Uid: string(cr.UID),
		},
		Generation:            cr.Generation,
		ObservedGeneration:    cr.Status.ObservedGeneration,
		ReportIntervalSeconds: int64(interval / time.Second),
		Lifecycle:             apigen.DeliveryTargetLifecycle(cr.Status.Lifecycle),
		Conditions:            wire,
		Reporter: apigen.DeliveryTargetReporter{
			Integration: apigen.KubernetesOperator,
			Version:     s.version,
		},
	}
}

// reportStatus runs after a successful status write (D9). It sends a report
// when the reportable content changed or the heartbeat is due, and clamps the
// requeue to 24 h while the instance advertises reporting, so a long
// resyncInterval still heartbeats. It never changes the reconcile error.
func (r *HikyoSecretReconciler) reportStatus(ctx context.Context, cr *hikyov1.HikyoSecret, in *reportInput, res ctrl.Result) ctrl.Result {
	rep := r.reporter
	now := r.clock()
	key := client.ObjectKeyFromObject(cr)
	st, ok := rep.load(key)
	if !ok || st.uid != cr.UID {
		// A recreated CR is a new target; its predecessor's row ages out (D3).
		st = targetState{uid: cr.UID}
	}
	st.instance, st.instanceUID = in.inst.Name, in.inst.UID
	st.scope, st.auth = cr.Spec.Scope, *cr.Spec.Auth.DeepCopy()
	defer func() { rep.store(key, st) }()

	ctx, cancel := context.WithTimeout(ctx, rep.timeout)
	defer cancel()
	dc, err := r.instanceClient(in.inst)
	if err != nil {
		r.reportEvent(cr, &st, now, ReasonStatusReportFailed, "status report: %v", err)
		return res
	}
	vocabulary, err := rep.vocabulary(ctx, dc, in.inst, now)
	if err != nil {
		r.reportEvent(cr, &st, now, ReasonStatusReportFailed, "status report: capability probe failed: %v", err)
		return res
	}
	if vocabulary == 0 {
		return res
	}
	if res.RequeueAfter > deliverytarget.MaxReportInterval {
		res.RequeueAfter = deliverytarget.MaxReportInterval
	}

	conditions := reportConditions(cr)
	body := rep.buildReport(cr, in.inst, vocabulary, conditions, r.reportInterval(cr))
	content, err := json.Marshal(body)
	if err != nil {
		r.reportEvent(cr, &st, now, ReasonStatusReportFailed, "status report: encode: %v", err)
		return res
	}
	credRef := credentialRef(cr)
	credID := in.cred.uid + "/" + in.cred.resourceVersion
	if st.suppress != nil {
		if st.suppress.holds(now, cr.Generation, credRef, credID, content, in.fetchOK) {
			return res
		}
		// Lifted: report now rather than waiting for the next heartbeat, so a
		// newly granted or rotated credential shows within one reconcile.
		st.suppress, st.attemptAt = nil, time.Time{}
	}
	if bytes.Equal(content, st.content) && now.Sub(st.attemptAt) < r.reportInterval(cr) {
		return res
	}
	// A condition outside the advertised vocabulary skips the whole report:
	// dropping it and sending the rest could read as healthy (D4).
	if err := deliverytarget.CheckVocabulary(deliverytarget.Report{Vocabulary: vocabulary, Conditions: conditions}); err != nil {
		r.reportEvent(cr, &st, now, ReasonStatusReportSkipped,
			"status report skipped: a condition is outside the server's vocabulary %d (%v); the target reads stale, never healthy", vocabulary, err)
		return res
	}

	st.content, st.attemptAt = content, now
	body.ReportedAt = now.UTC().Truncate(time.Microsecond)
	status, err := dc.Report(ctx, string(cr.Spec.Scope.Org), string(cr.Spec.Scope.Project), string(cr.Spec.Scope.Environment), in.cred.token, body)
	switch {
	case err != nil:
		r.reportEvent(cr, &st, now, ReasonStatusReportFailed, "status report failed: %v", err)
	case status >= 200 && status < 300:
		st.accepted = true
	case status == 401 || status == 404 || status == 413 || status == 422:
		st.suppress = &reportSuppressed{
			until: now.Add(reportSuppression), generation: cr.Generation, credRef: credRef,
			content: content, unauthorized: status == 401, credential: credID,
		}
		r.reportEvent(cr, &st, now, ReasonStatusReportFailed,
			"status report refused with status %d; reporting for this HikyoSecret pauses for %s or until its generation, credential or reported conditions change", status, reportSuppression)
	default:
		r.reportEvent(cr, &st, now, ReasonStatusReportFailed, "status report failed with status %d", status)
	}
	return res
}

// reportEvent emits a reporting Event at most once per CR per interval.
func (r *HikyoSecretReconciler) reportEvent(cr *hikyov1.HikyoSecret, st *targetState, now time.Time, reason, format string, args ...any) {
	if !st.eventAt.IsZero() && now.Sub(st.eventAt) < reportEventInterval {
		return
	}
	st.eventAt = now
	r.event(cr, corev1.EventTypeWarning, reason, format, args...)
}

// instanceClient builds the client for an instance's origin and trust anchors.
func (r *HikyoSecretReconciler) instanceClient(inst *hikyov1.HikyoInstance) (deliveryClient, error) {
	caBundle, err := decodeCABundle(inst.Spec.CABundle)
	if err != nil {
		return nil, fmt.Errorf("instance caBundle: %w", err)
	}
	return r.clientFactory()(inst.Spec.URL, caBundle)
}

// tombstone sends one best-effort tombstone for a deleted CR (D6). There is
// no finalizer: the CR is already gone, so the credential is re-acquired from
// the reference its last report recorded, through the same designation checks
// as the active path. Failure is an Event, nothing more.
func (r *HikyoSecretReconciler) tombstone(ctx context.Context, key types.NamespacedName) {
	if r.reporter == nil {
		return
	}
	st, ok := r.reporter.take(key)
	if !ok || !st.accepted {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, r.reporter.timeout)
	defer cancel()
	if err := r.sendTombstone(ctx, key.Namespace, st); err != nil {
		gone := &hikyov1.HikyoSecret{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name, UID: st.uid}}
		r.event(gone, corev1.EventTypeWarning, ReasonStatusReportFailed, "status tombstone failed: %v", err)
	}
}

func (r *HikyoSecretReconciler) sendTombstone(ctx context.Context, namespace string, st targetState) error {
	var inst hikyov1.HikyoInstance
	if err := r.Get(ctx, types.NamespacedName{Name: st.instance}, &inst); err != nil {
		return fmt.Errorf("get HikyoInstance %q: %w", st.instance, err)
	}
	if inst.UID != st.instanceUID {
		return fmt.Errorf("HikyoInstance %q was recreated since the last report", st.instance)
	}
	dc, err := r.instanceClient(&inst)
	if err != nil {
		return err
	}
	vocabulary, err := r.reporter.vocabulary(ctx, dc, &inst, r.clock())
	if err != nil {
		return fmt.Errorf("capability probe: %w", err)
	}
	if vocabulary == 0 {
		return nil
	}
	var cred credential
	var refusal *designationFailure
	switch {
	case st.auth.SecretRef != nil:
		cred, refusal, err = r.bootstrapCredential(ctx, namespace, st.auth.SecretRef.Name, inst.Name)
	case st.auth.ServiceAccountRef != nil:
		var sa *corev1.ServiceAccount
		sa, refusal, err = r.federationSubject(ctx, namespace, st.auth.ServiceAccountRef.Name, &inst)
		if err == nil && refusal == nil {
			if r.TokenMinter == nil {
				return errNoTokenMinter
			}
			cred.token, err = r.TokenMinter.Mint(ctx, namespace, sa.Name, inst.Spec.Audience)
		}
	default:
		return fmt.Errorf("no credential reference recorded")
	}
	switch {
	case err != nil:
		return err
	case refusal != nil:
		return fmt.Errorf("credential refused: %s", refusal.msg)
	}
	status, err := dc.Tombstone(ctx, string(st.scope.Org), string(st.scope.Project), string(st.scope.Environment), cred.token,
		apigen.DeliveryTargetTombstoneRequest{Target: apigen.DeliveryTargetIdentity{
			ClusterId: r.reporter.clusterID, InstanceUid: string(st.instanceUID), Uid: string(st.uid),
		}})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("server answered status %d", status)
	}
	return nil
}
