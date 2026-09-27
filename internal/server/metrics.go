package server

import (
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
)

// RED-style request metrics for the operational /metrics surface (#513).
//
// Metrics use the official Prometheus Go client behind a private registry. The
// private registry deliberately excludes the default Go/process collectors;
// counters are keyed by a CLOSED surface class, never a raw path or an ID, so
// cardinality is bounded by construction — the acceptance criterion this
// ticket exists to satisfy.

// Metric family names. Exported so the conformance drift-guard can pin them:
// a rename here fails that test rather than silently breaking a dashboard.
const (
	MetricLastPruneSuccess       = "hikyo_last_prune_success_timestamp_seconds"
	MetricPruneStale             = "hikyo_prune_stale"
	MetricProjectStoragePeak     = "hikyo_project_storage_peak_bytes"
	MetricProjectStorageWarn     = "hikyo_project_storage_warn"
	MetricDataVolumeKnown        = "hikyo_data_volume_capacity_known"
	MetricDataVolumeUsedPercent  = "hikyo_data_volume_used_percent"
	MetricDataVolumeWarn         = "hikyo_data_volume_warn"
	MetricDataVolumeCritical     = "hikyo_data_volume_critical"
	MetricRootEscrowVerified     = "hikyo_root_escrow_verified"
	MetricRootEscrowVerifiedAt   = "hikyo_root_escrow_verified_timestamp_seconds"
	MetricRootRotationPending    = "hikyo_root_rotation_pending"
	MetricReencryptPendingScopes = "hikyo_reencrypt_pending_scopes"
	MetricLastReencryptSuccess   = "hikyo_last_reencrypt_success_timestamp_seconds"
	MetricPinsExpired            = "hikyo_pins_expired"
	MetricPinsExpiringDay        = "hikyo_pins_expiring_day"
	MetricPinsExpiringWeek       = "hikyo_pins_expiring_week"
	MetricPinsExpiringMonth      = "hikyo_pins_expiring_month"
	MetricTLSCertNotAfter        = "hikyo_tls_cert_not_after_timestamp_seconds"
	MetricTLSReloadFailures      = "hikyo_tls_reload_failures_total"

	// Deployment-adapter health (#157). Label-free by construction: they
	// count targets and jobs instance-wide and never name a tenant.
	MetricAdapterTargetsFailed    = "hikyo_adapter_targets_failed"
	MetricAdapterTargetsPaused    = "hikyo_adapter_targets_paused"
	MetricAdapterTargetsAttention = "hikyo_adapter_targets_attention"
	MetricAdapterJobsQueued       = "hikyo_adapter_jobs_queued"

	MetricRequestsTotal       = "hikyo_http_requests_total"
	MetricRequestErrors       = "hikyo_http_request_errors_total"
	MetricRequestsInFlight    = "hikyo_http_requests_in_flight"
	MetricRequestDuration     = "hikyo_http_request_duration_seconds"
	MetricMCPRequestsTotal    = "hikyo_mcp_requests_total"
	MetricMCPRequestsInFlight = "hikyo_mcp_requests_in_flight"
	MetricMCPRequestDuration  = "hikyo_mcp_request_duration_seconds"

	MetricAdmissionConcurrencyLimit = "hikyo_admission_concurrency_limit"
	MetricAdmissionInFlight         = "hikyo_admission_in_flight"
	MetricAdmissionQueueDepthLimit  = "hikyo_admission_queue_depth_limit"
	MetricAdmissionQueueWaiting     = "hikyo_admission_queue_waiting"
	MetricAdmissionActiveBackoffs   = "hikyo_admission_active_backoffs"

	// Multi-node HA gauges (#146). Label-free, so their cardinality is one each
	// regardless of cluster size (the ops-spec bounded-cardinality posture: no
	// per-node labels). On a single node they report the trivial values: this
	// node is the leader, one node is seen, and the lease age is zero.
	MetricHAIsLeader       = "hikyo_ha_is_leader"
	MetricHANodesSeen      = "hikyo_ha_nodes_seen"
	MetricHALeaseAgeSecond = "hikyo_ha_lease_age_seconds"

	// Secret-change approvals (#151). Label-free, bounded-cardinality gauges:
	// how many requests are awaiting review right now, and how many have expired
	// unmerged (a monotonic-ish operational signal that a policy's reviewers are
	// not keeping up). No per-tenant labels, so cardinality is one each.
	MetricApprovalRequestsOpen    = "hikyo_approval_requests_open"
	MetricApprovalRequestsExpired = "hikyo_approval_requests_expired"
	// MetricApprovalGaugesKnown is 1 when the two approval gauges above were
	// measured on this scrape and 0 when the source failed, in which case both
	// are omitted rather than rendered as zeros (a failed count is unknown,
	// not empty). Check it before alerting on either, like capacity_known.
	MetricApprovalGaugesKnown = "hikyo_approval_gauges_known"
	// Approval-mediated temporary access (#152). Label-free like the approval
	// gauges: open access requests awaiting a decision, and temporary grants
	// currently in force (granted requests whose absolute expiry is ahead).
	MetricAccessRequestsOpen = "hikyo_access_requests_open"
	MetricAccessGrantsActive = "hikyo_access_grants_active"
	// MetricAccessGaugesKnown is 1 when the two access gauges were measured on
	// this scrape; they are omitted when it is 0.
	MetricAccessGaugesKnown = "hikyo_access_gauges_known"
	// Disaster-recovery gauges (#145, ops-spec section 11). Label-free like
	// every other operator gauge: one series each, no archive name, no path.
	MetricLastBackupExportSuccess = "hikyo_last_backup_export_success_timestamp_seconds"
	MetricBackupRPOExceeded       = "hikyo_backup_rpo_exceeded"
	MetricLastBackupPruneSuccess  = "hikyo_last_backup_prune_success_timestamp_seconds"
	MetricLastRestoreDrill        = "hikyo_last_restore_drill_timestamp_seconds"
	MetricRestoreDrillOK          = "hikyo_restore_drill_ok"

	// Dynamic-secret gauges (#147). Label-free, cardinality one each. active is
	// the count of currently usable leases; effectsUnknown is the count of lease
	// transitions stuck in an uncertain state awaiting reconcile.
	MetricDynamicLeasesActive   = "hikyo_dynamic_leases_active"
	MetricDynamicEffectsUnknown = "hikyo_dynamic_effects_unknown"
	// MetricDynamicGaugesKnown is 1 when the two dynamic gauges above were
	// measured on this scrape and 0 when the source failed, in which case both
	// are omitted: a datastore outage must not read as "no unknown effects".
	MetricDynamicGaugesKnown = "hikyo_dynamic_gauges_known"
	// SSH certificate gauges (#155). Label-free, cardinality one each. active
	// counts issued, unexpired certificates; krl_entries counts the serials
	// every KRL currently publishes (bounded per CA by sshca.MaxKRLSerials).
	MetricSSHCertificatesActive = "hikyo_ssh_certificates_active"
	MetricSSHKRLEntries         = "hikyo_ssh_krl_entries"
	// MetricSSHGaugesKnown is 1 when the two SSH gauges were measured on this
	// scrape; 0 means they are omitted rather than rendered as zeros.
	MetricSSHGaugesKnown = "hikyo_ssh_gauges_known"
	// Transit (#156): live managed keys, keys whose rotation period has
	// elapsed, and keys waiting out their deletion delay. Label-free; the
	// known flag is 0 when the scrape could not measure them, and the three
	// value gauges are then omitted rather than rendered as zeros.
	MetricTransitKeysLive            = "hikyo_transit_keys_live"
	MetricTransitKeysRotationDue     = "hikyo_transit_keys_rotation_due"
	MetricTransitKeysPendingDeletion = "hikyo_transit_keys_pending_deletion"
	MetricTransitGaugesKnown         = "hikyo_transit_gauges_known"

	// Private-PKI gauges (#154). Label-free, cardinality one each: live leaf
	// certificates, certificates in the uncertain `unknown` state (published on
	// the CRL as revoked), and issuers held after a restore. The known flag has
	// the dynamic-gauge semantics: 0 means unmeasured, and the values are omitted.
	MetricPKICertificatesLive    = "hikyo_pki_certificates_live"
	MetricPKICertificatesUnknown = "hikyo_pki_certificates_unknown"
	MetricPKIIssuersOnHold       = "hikyo_pki_issuers_on_hold"
	MetricPKIGaugesKnown         = "hikyo_pki_gauges_known"

	// MetricSeriesBudget is the ops-spec ceiling for every registered series.
	MetricSeriesBudget = 1000
)

// latencyBucketsSeconds are the fixed, cumulative histogram boundaries for the
// request-duration family. Fixed rather than dynamic keeps the exposition shape
// deterministic and the label set closed; the values are pinned in the
// conformance registry so an edit that drifts them off the agreed grid fails
// the build there. Spread 5 ms → 5 s covers a fast read to a slow expensive
// publish without a per-endpoint tail.
var latencyBucketsSeconds = [...]float64{0.005, 0.025, 0.1, 0.5, 1, 5}

// surfaceClass is the closed set of RED classes. A request that matches none
// of the known families falls to classOther — fail-closed, never a new label.
type surfaceClass int

const (
	classAuth surfaceClass = iota
	classHierarchy
	classValues
	classRevisions
	classDelivery
	classSCIM
	classAdmin
	classOther
	numClasses
)

// classNames is the closed class label set, indexed by surfaceClass.
var classNames = [...]string{
	classAuth:      "auth",
	classHierarchy: "hierarchy",
	classValues:    "values",
	classRevisions: "revisions",
	classDelivery:  "delivery",
	classSCIM:      "scim",
	classAdmin:     "admin",
	classOther:     "other",
}

// statusBucket collapses a status code to its class. 3xx is a first-class
// bucket because the API returns redirects (the OIDC browser legs), so folding
// it into 2xx or dropping it would misfile real traffic.
type statusBucket int

const (
	status2xx statusBucket = iota
	status3xx
	status4xx
	status5xx
	statusOther
	numStatusBuckets
)

// statusNames is the closed status label set, indexed by statusBucket.
var statusNames = [...]string{
	status2xx:   "2xx",
	status3xx:   "3xx",
	status4xx:   "4xx",
	status5xx:   "5xx",
	statusOther: "other",
}

// RequestLatencyBucketsSeconds returns a copy of the fixed histogram grid.
func RequestLatencyBucketsSeconds() []float64 {
	return append([]float64(nil), latencyBucketsSeconds[:]...)
}

func bucketForStatus(code int) statusBucket {
	switch code / 100 {
	case 2:
		return status2xx
	case 3:
		return status3xx
	case 4:
		return status4xx
	case 5:
		return status5xx
	default:
		return statusOther
	}
}

// classify maps a matched chi route PATTERN (templated, e.g.
// "/api/v1/orgs/{org}/projects/{project}/values") to a surface class. It reads
// the templated pattern, never the concrete path, so no ID ever reaches a
// label. Order matters: the value/revision/delivery/scim families live UNDER
// the orgs hierarchy, so they must be recognised before the orgs prefix
// catch-all. The exact family→class table is pinned in the conformance test.
func classify(pattern string) surfaceClass {
	switch {
	case strings.Contains(pattern, "/scim"):
		return classSCIM
	case strings.Contains(pattern, "/delivery"):
		return classDelivery
	case strings.Contains(pattern, "/values"):
		return classValues
	case strings.Contains(pattern, "/revisions"),
		strings.Contains(pattern, "/publish"),
		strings.Contains(pattern, "/pending"),
		strings.Contains(pattern, "/pins"):
		return classRevisions
	case strings.HasPrefix(pattern, api.PathPrefix+"/auth"),
		strings.HasPrefix(pattern, api.PathPrefix+"/me"),
		strings.HasPrefix(pattern, api.PathPrefix+"/meta"),
		strings.HasPrefix(pattern, api.PathPrefix+"/accounts"):
		return classAuth
	case strings.HasPrefix(pattern, api.PathPrefix+"/instance"):
		return classAdmin
	case strings.HasPrefix(pattern, api.PathPrefix+"/orgs"):
		return classHierarchy
	default:
		return classOther
	}
}

// AdmissionSnapshotter is the admission-pressure source the gauges read at
// scrape time. Nil renders zeros, so the exposition shape stays deterministic
// whether or not a limiter is wired.
type AdmissionSnapshotter interface {
	Snapshot() admission.Snapshot
}

// HAStats is a point-in-time read of multi-node HA state for the label-free
// gauges. Enabled is false on a single node, where the collector emits the
// trivial values (leader, one node, zero lease age).
type HAStats struct {
	Enabled         bool
	IsLeader        bool
	NodesSeen       int
	LeaseAgeSeconds float64
}

// HASnapshotter is the HA-state source the gauges read at scrape time. A nil
// source renders the single-node defaults, so the exposition shape is
// deterministic whether or not HA is wired.
type HASnapshotter interface {
	HASnapshot() HAStats
}

// Metrics is the instance-wide RED collector. One instance is shared between
// the API middleware (which writes) and the operational /metrics handler (which
// reads), so both sides see the same counters.
type Metrics struct {
	registry *prometheus.Registry

	inFlight     prometheus.Gauge
	requests     [numClasses][numStatusBuckets]prometheus.Counter
	errors       [numClasses][numStatusBuckets]prometheus.Counter
	durations    [numClasses]prometheus.Observer
	mcpRequests  *prometheus.CounterVec
	mcpInFlight  prometheus.Gauge
	mcpDurations *prometheus.HistogramVec
	ha           *haCollector
	approvals    *approvalCollector
	access       *accessCollector
	dyn          *dynamicCollector
	ssh          *sshCollector
	transit      *transitCollector
	pki          *pkiCollector
}

// SetHASource attaches the multi-node HA gauge source. It is called once
// during boot before the operational listener serves, so the collector's
// source pointer is set before any scrape reads it.
func (m *Metrics) SetHASource(source HASnapshotter) { m.ha.source.Store(&source) }

// NewMetrics builds the fixed collector set in a private pedantic registry.
// Pre-creating every closed label combination keeps the scrape shape
// deterministic even before traffic arrives.
func NewMetrics(adm AdmissionSnapshotter) *Metrics {
	registry := prometheus.NewPedanticRegistry()
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: MetricRequestsTotal,
		Help: "Total HTTP requests by closed API surface class and status bucket.",
	}, []string{"class", "status"})
	errors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: MetricRequestErrors,
		Help: "Total HTTP error responses by closed API surface class and status bucket.",
	}, []string{"class", "status"})
	inFlight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: MetricRequestsInFlight,
		Help: "Current number of API requests in flight.",
	})
	durations := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    MetricRequestDuration,
		Help:    "HTTP request duration in seconds by closed API surface class.",
		Buckets: RequestLatencyBucketsSeconds(),
	}, []string{"class"})
	mcpRequests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: MetricMCPRequestsTotal,
		Help: "Total MCP requests by closed method, tool, and status bucket.",
	}, []string{"method", "tool", "status"})
	mcpInFlight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: MetricMCPRequestsInFlight,
		Help: "Current number of MCP requests in flight.",
	})
	mcpDurations := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    MetricMCPRequestDuration,
		Help:    "MCP request duration in seconds by closed method and tool.",
		Buckets: RequestLatencyBucketsSeconds(),
	}, []string{"method", "tool"})
	ha := newHACollector()
	approvals := newApprovalCollector()
	access := newAccessCollector()
	dyn := newDynamicCollector()
	sshc := newSSHCollector()
	tr := newTransitCollector()
	pkiGauges := newPKICollector()
	registry.MustRegister(requests, errors, inFlight, durations, mcpRequests, mcpInFlight, mcpDurations, newAdmissionCollector(adm), ha, approvals, access, dyn, sshc, pkiGauges, tr)

	m := &Metrics{
		registry: registry, inFlight: inFlight,
		mcpRequests: mcpRequests, mcpInFlight: mcpInFlight, mcpDurations: mcpDurations,
		ha: ha, approvals: approvals, access: access, dyn: dyn, ssh: sshc, pki: pkiGauges, transit: tr,
	}
	for c := surfaceClass(0); c < numClasses; c++ {
		for s := statusBucket(0); s < numStatusBuckets; s++ {
			m.requests[c][s] = requests.WithLabelValues(classNames[c], statusNames[s])
		}
		for _, s := range [...]statusBucket{status4xx, status5xx} {
			m.errors[c][s] = errors.WithLabelValues(classNames[c], statusNames[s])
		}
		m.durations[c] = durations.WithLabelValues(classNames[c])
	}
	m.initializeMCPMetrics([]string{"none", "other"})
	return m
}

var mcpMetricMethods = [...]string{"server/discover", "tools/list", "tools/call", "other"}

func (m *Metrics) initializeMCPMetrics(tools []string) {
	for _, method := range mcpMetricMethods {
		for _, tool := range tools {
			for _, status := range statusNames {
				m.mcpRequests.WithLabelValues(method, tool, status).Add(0)
			}
			m.mcpDurations.WithLabelValues(method, tool)
		}
	}
}

// ObserveMCP wraps the feature-gated MCP handler with closed-cardinality
// metrics and debug access logs. toolNames must come from the compiled
// registry. Request-controlled method and tool strings collapse to "other".
func (m *Metrics) ObserveMCP(next http.Handler, log *slog.Logger, toolNames []string) http.Handler {
	knownTools := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		knownTools[name] = struct{}{}
	}
	m.initializeMCPMetrics(toolNames)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := mcpMetricMethod(r.Header.Get("Mcp-Method"))
		// The tool label is read after the adapter has run: the adapter
		// decodes a Base64-sentinel Mcp-Name in place once it has validated
		// the mirror, so the label reflects the routed tool, not its encoding.
		toolLabel := func() string {
			if method != "tools/call" {
				return "none"
			}
			candidate := r.Header.Get("Mcp-Name")
			if _, ok := knownTools[candidate]; ok {
				return candidate
			}
			return "other"
		}
		sw := newResponseWriter(w)
		start := time.Now()
		m.mcpInFlight.Inc()
		defer func() {
			if recover() != nil {
				if !sw.wroteHeader {
					http.Error(sw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				} else {
					sw.markRecoveredPanic()
				}
			}
			m.mcpInFlight.Dec()
			duration := time.Since(start)
			tool := toolLabel()
			status := statusNames[bucketForStatus(sw.status)]
			m.mcpRequests.WithLabelValues(method, tool, status).Inc()
			m.mcpDurations.WithLabelValues(method, tool).Observe(duration.Seconds())
			if log != nil {
				log.DebugContext(r.Context(), "mcp request",
					"method", method, "tool", tool,
					"status", sw.status, "duration_ms", duration.Milliseconds())
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

func mcpMetricMethod(candidate string) string {
	for _, method := range mcpMetricMethods[:len(mcpMetricMethods)-1] {
		if candidate == method {
			return candidate
		}
	}
	return "other"
}

func (m *Metrics) record(class surfaceClass, code int, d time.Duration) {
	sb := bucketForStatus(code)
	m.requests[class][sb].Inc()
	switch sb {
	case status4xx, status5xx:
		m.errors[class][sb].Inc()
	}
	m.durations[class].Observe(d.Seconds())
}

type admissionCollector struct {
	source AdmissionSnapshotter
	descs  [5]*prometheus.Desc
}

func newAdmissionCollector(source AdmissionSnapshotter) *admissionCollector {
	return &admissionCollector{source: source, descs: [5]*prometheus.Desc{
		prometheus.NewDesc(MetricAdmissionConcurrencyLimit, "Configured admission concurrency limit.", nil, nil),
		prometheus.NewDesc(MetricAdmissionInFlight, "Current admission work in flight.", nil, nil),
		prometheus.NewDesc(MetricAdmissionQueueDepthLimit, "Configured admission queue depth limit.", nil, nil),
		prometheus.NewDesc(MetricAdmissionQueueWaiting, "Current requests waiting for admission.", nil, nil),
		prometheus.NewDesc(MetricAdmissionActiveBackoffs, "Current active admission backoff buckets.", nil, nil),
	}}
}

func (c *admissionCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
}

func (c *admissionCollector) Collect(ch chan<- prometheus.Metric) {
	var snap admission.Snapshot
	if c.source != nil {
		snap = c.source.Snapshot()
	}
	values := [...]float64{
		float64(snap.ConcurrencyLimit),
		float64(snap.InFlight),
		float64(snap.QueueDepthLimit),
		float64(snap.Waiting),
		float64(snap.ActiveBackoffs),
	}
	for i, desc := range c.descs {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, values[i])
	}
}

// haCollector emits the three label-free multi-node HA gauges. Its source is
// an atomic pointer so boot can attach it after registration without a data
// race against a concurrent scrape.
type haCollector struct {
	source atomic.Pointer[HASnapshotter]
	descs  [3]*prometheus.Desc
}

func newHACollector() *haCollector {
	return &haCollector{descs: [3]*prometheus.Desc{
		prometheus.NewDesc(MetricHAIsLeader, "1 when this node holds the scheduler lease (always 1 on a single node).", nil, nil),
		prometheus.NewDesc(MetricHANodesSeen, "Number of live nodes in this installation (1 on a single node).", nil, nil),
		prometheus.NewDesc(MetricHALeaseAgeSecond, "Age in seconds of the current scheduler lease as sampled on this node's last tick, up to one heartbeat stale (0 on a single node).", nil, nil),
	}}
}

func (c *haCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
}

func (c *haCollector) Collect(ch chan<- prometheus.Metric) {
	stats := HAStats{Enabled: false}
	if p := c.source.Load(); p != nil && *p != nil {
		stats = (*p).HASnapshot()
	}
	leader, nodes, age := 1.0, 1.0, 0.0
	if stats.Enabled {
		if !stats.IsLeader {
			leader = 0
		}
		nodes = float64(stats.NodesSeen)
		age = stats.LeaseAgeSeconds
	}
	values := [...]float64{leader, nodes, age}
	for i, desc := range c.descs {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, values[i])
	}
}

// ApprovalStats is the label-free approval snapshot (#151): active requests
// awaiting review, and requests that have expired unmerged.
type ApprovalStats struct {
	Open    float64
	Expired float64
}

// ApprovalSnapshotter supplies the approval gauges at scrape time. It is read
// synchronously in Collect, so an implementation must be quick and must not
// block. An error marks the gauges unknown for this scrape: the collector
// omits them and renders MetricApprovalGaugesKnown as 0 instead of failing
// the scrape or inventing zeros.
type ApprovalSnapshotter interface {
	ApprovalSnapshot() (ApprovalStats, error)
}

// SetApprovalSource attaches the approval gauge source after registration, via
// an atomic pointer, so boot can wire it without racing a concurrent scrape.
func (m *Metrics) SetApprovalSource(source ApprovalSnapshotter) {
	m.approvals.source.Store(&source)
}

type approvalCollector struct {
	source atomic.Pointer[ApprovalSnapshotter]
	descs  [2]*prometheus.Desc
	known  *prometheus.Desc
}

func newApprovalCollector() *approvalCollector {
	return &approvalCollector{descs: [2]*prometheus.Desc{
		prometheus.NewDesc(MetricApprovalRequestsOpen, "Change-approval requests awaiting review (open or approved, not yet resolved).", nil, nil),
		prometheus.NewDesc(MetricApprovalRequestsExpired, "Change-approval requests that expired unmerged.", nil, nil),
	}, known: prometheus.NewDesc(MetricApprovalGaugesKnown, "Whether the approval gauges were measured on this scrape; they are omitted when 0.", nil, nil)}
}

func (c *approvalCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
	ch <- c.known
}

// AccessSnapshotter supplies the temporary-access gauges at scrape time (#152).
// Same contract as ApprovalSnapshotter: quick, non-blocking, and an error
// marks the gauges unknown for this scrape.
type AccessSnapshotter interface {
	AccessSnapshot() (open, active int64, err error)
}

// SetAccessSource attaches the temporary-access gauge source once at boot.
func (m *Metrics) SetAccessSource(source AccessSnapshotter) { m.access.source.Store(&source) }

type accessCollector struct {
	source atomic.Pointer[AccessSnapshotter]
	descs  [2]*prometheus.Desc
	known  *prometheus.Desc
}

func newAccessCollector() *accessCollector {
	return &accessCollector{descs: [2]*prometheus.Desc{
		prometheus.NewDesc(MetricAccessRequestsOpen, "Temporary-access requests awaiting a decision.", nil, nil),
		prometheus.NewDesc(MetricAccessGrantsActive, "Temporary-access grants currently in force.", nil, nil),
	}, known: prometheus.NewDesc(MetricAccessGaugesKnown, "Whether the temporary-access gauges were measured on this scrape; they are omitted when 0.", nil, nil)}
}

func (c *accessCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
	ch <- c.known
}

// Collect emits the access gauges and marks them known when the source succeeds.
// A missing source or snapshot error omits both counts and emits known = 0.
func (c *accessCollector) Collect(ch chan<- prometheus.Metric) {
	var values [2]float64
	measured := false
	if p := c.source.Load(); p != nil && *p != nil {
		if open, active, err := (*p).AccessSnapshot(); err == nil {
			values, measured = [2]float64{float64(open), float64(active)}, true
		}
	}
	collectMeasured(ch, c.descs[:], c.known, values[:], measured)
}

// DynamicSnapshotter is the dynamic-secret gauge source, read at scrape time.
// An error (or a nil source) marks the gauges unknown for this scrape: the
// collector omits them and renders MetricDynamicGaugesKnown as 0, so a
// datastore hiccup never reads as a measured zero.
type DynamicSnapshotter interface {
	DynamicSnapshot() (activeLeases, unknownEffects int64, err error)
}

// SetDynamicSource attaches the dynamic-secret gauge source once at boot.
func (m *Metrics) SetDynamicSource(source DynamicSnapshotter) { m.dyn.source.Store(&source) }

type dynamicCollector struct {
	source atomic.Pointer[DynamicSnapshotter]
	descs  [2]*prometheus.Desc
	known  *prometheus.Desc
}

func newDynamicCollector() *dynamicCollector {
	return &dynamicCollector{descs: [2]*prometheus.Desc{
		prometheus.NewDesc(MetricDynamicLeasesActive, "Number of currently usable dynamic-secret leases.", nil, nil),
		prometheus.NewDesc(MetricDynamicEffectsUnknown, "Number of dynamic-secret lease transitions in an uncertain state awaiting reconcile.", nil, nil),
	}, known: prometheus.NewDesc(MetricDynamicGaugesKnown, "Whether the dynamic-secret gauges were measured on this scrape; they are omitted when 0.", nil, nil)}
}

func (c *dynamicCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
	ch <- c.known
}

// collectMeasured renders a set of gauges plus their known flag. A failed or
// absent measurement emits only known=0: an omitted series is "unknown" to an
// alert, a zero is "healthy", and the two must never be confused. When measured
// is true, values must contain an entry for each descriptor or this panics.
func collectMeasured(ch chan<- prometheus.Metric, descs []*prometheus.Desc, known *prometheus.Desc, values []float64, measured bool) {
	if measured {
		for i, desc := range descs {
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, values[i])
		}
	}
	knownValue := 0.0
	if measured {
		knownValue = 1
	}
	ch <- prometheus.MustNewConstMetric(known, prometheus.GaugeValue, knownValue)
}

// Collect emits approval counts and known=1, or only known=0 when the source
// is absent or its snapshot fails.
func (c *approvalCollector) Collect(ch chan<- prometheus.Metric) {
	var values [2]float64
	measured := false
	if p := c.source.Load(); p != nil && *p != nil {
		if stats, err := (*p).ApprovalSnapshot(); err == nil {
			values, measured = [2]float64{stats.Open, stats.Expired}, true
		}
	}
	collectMeasured(ch, c.descs[:], c.known, values[:], measured)
}

// Collect emits dynamic lease counts and known=1, or only known=0 when the
// source is absent or its snapshot fails.
func (c *dynamicCollector) Collect(ch chan<- prometheus.Metric) {
	var values [2]float64
	measured := false
	if p := c.source.Load(); p != nil && *p != nil {
		if active, unknown, err := (*p).DynamicSnapshot(); err == nil {
			values, measured = [2]float64{float64(active), float64(unknown)}, true
		}
	}
	collectMeasured(ch, c.descs[:], c.known, values[:], measured)
}

// SSHSnapshotter is the SSH certificate gauge source, read at scrape time.
// An error (or a nil source) marks the gauges unknown for this scrape.
type SSHSnapshotter interface {
	SSHSnapshot() (activeCertificates, krlEntries int64, err error)
}

// SetSSHSource attaches the SSH certificate gauge source once at boot.
func (m *Metrics) SetSSHSource(source SSHSnapshotter) { m.ssh.source.Store(&source) }

type sshCollector struct {
	source atomic.Pointer[SSHSnapshotter]
	descs  [2]*prometheus.Desc
	known  *prometheus.Desc
}

func newSSHCollector() *sshCollector {
	return &sshCollector{descs: [2]*prometheus.Desc{
		prometheus.NewDesc(MetricSSHCertificatesActive, "Number of issued, unexpired SSH user certificates.", nil, nil),
		prometheus.NewDesc(MetricSSHKRLEntries, "Number of serials published across every SSH CA's key revocation list.", nil, nil),
	}, known: prometheus.NewDesc(MetricSSHGaugesKnown, "Whether the SSH certificate gauges were measured on this scrape; they are omitted when 0.", nil, nil)}
}

func (c *sshCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
	ch <- c.known
}

// PKISnapshotter is the private-PKI gauge source, read at scrape time, with
// the DynamicSnapshotter failure semantics.
type PKISnapshotter interface {
	PKISnapshot() (live, unknown, held int64, err error)
}

// SetPKISource attaches the private-PKI gauge source once at boot.
func (m *Metrics) SetPKISource(source PKISnapshotter) { m.pki.source.Store(&source) }

type pkiCollector struct {
	source atomic.Pointer[PKISnapshotter]
	descs  [3]*prometheus.Desc
	known  *prometheus.Desc
}

func newPKICollector() *pkiCollector {
	return &pkiCollector{descs: [3]*prometheus.Desc{
		prometheus.NewDesc(MetricPKICertificatesLive, "Number of issued, unexpired private-PKI leaf certificates.", nil, nil),
		prometheus.NewDesc(MetricPKICertificatesUnknown, "Number of private-PKI certificates in the uncertain unknown state, published on the CRL as revoked.", nil, nil),
		prometheus.NewDesc(MetricPKIIssuersOnHold, "Number of private-PKI issuer versions held after a restore until reconciled.", nil, nil),
	}, known: prometheus.NewDesc(MetricPKIGaugesKnown, "Whether the private-PKI gauges were measured on this scrape; they are omitted when 0.", nil, nil)}
}

func (c *pkiCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
	ch <- c.known
}

// Collect emits SSH certificate counts and known=1, or only known=0 when the
// source is absent or its snapshot fails.
func (c *sshCollector) Collect(ch chan<- prometheus.Metric) {
	var values [2]float64
	measured := false
	if p := c.source.Load(); p != nil && *p != nil {
		if active, krl, err := (*p).SSHSnapshot(); err == nil {
			values, measured = [2]float64{float64(active), float64(krl)}, true
		}
	}
	collectMeasured(ch, c.descs[:], c.known, values[:], measured)
}

// Collect emits transit key counts and known=1, or only known=0 when the
// source is absent or its snapshot fails.
func (c *transitCollector) Collect(ch chan<- prometheus.Metric) {
	measured := false
	var values [3]float64
	if p := c.source.Load(); p != nil && *p != nil {
		if live, due, pending, err := (*p).TransitSnapshot(); err == nil {
			values, measured = [3]float64{float64(live), float64(due), float64(pending)}, true
		}
	}
	collectMeasured(ch, c.descs[:], c.known, values[:], measured)
}

func (c *pkiCollector) Collect(ch chan<- prometheus.Metric) {
	known := 0.0
	if p := c.source.Load(); p != nil && *p != nil {
		if live, unknown, held, err := (*p).PKISnapshot(); err == nil {
			for i, value := range []int64{live, unknown, held} {
				ch <- prometheus.MustNewConstMetric(c.descs[i], prometheus.GaugeValue, float64(value))
			}
			known = 1
		}
	}
	ch <- prometheus.MustNewConstMetric(c.known, prometheus.GaugeValue, known)
}

// observe is the outer public-router leg for /api/v1 traffic. Its placement
// before CORS and route matching makes unmatched paths, unsupported methods,
// and preflights visible as class=other. It also leads recoverPanics, so a
// recovered panic is recorded as 5xx even after wire bytes were committed.
func (a *API) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, api.PathPrefix+"/") && r.URL.Path != api.PathPrefix {
			next.ServeHTTP(w, r)
			return
		}
		sw := newResponseWriter(w)
		start := time.Now()
		if a.Metrics != nil {
			a.Metrics.inFlight.Add(1)
			defer a.Metrics.inFlight.Add(-1)
		}
		next.ServeHTTP(sw, r)
		dur := time.Since(start)

		class := classOther
		if rc := chi.RouteContext(r.Context()); !sw.unmatched && rc != nil && rc.RoutePattern() != "" {
			class = classify(rc.RoutePattern())
		}
		if a.Metrics != nil {
			a.Metrics.record(class, sw.status, dur)
		}
		// Debug level so the access log appears under --dev (text handler at
		// LevelDebug) and is absent in the production default (JSON at Info).
		// The class, never the raw path, keeps the log free of path echo — the
		// same discipline the counters enforce.
		if a.Log != nil {
			a.Log.DebugContext(r.Context(), "request",
				"method", r.Method,
				"class", classNames[class],
				"status", sw.status,
				"duration_ms", dur.Milliseconds())
		}
	})
}

// TransitSnapshotter is the transit gauge source, read at scrape time. An
// error (or a nil source) marks the gauges unknown for this scrape.
type TransitSnapshotter interface {
	TransitSnapshot() (live, rotationDue, pendingDeletion int64, err error)
}

// SetTransitSource attaches the transit gauge source once at boot.
func (m *Metrics) SetTransitSource(source TransitSnapshotter) { m.transit.source.Store(&source) }

type transitCollector struct {
	source atomic.Pointer[TransitSnapshotter]
	descs  [3]*prometheus.Desc
	known  *prometheus.Desc
}

// newTransitCollector defines the transit key gauges without tenant labels.
// The collector reports unknown until a source supplies a successful snapshot.
func newTransitCollector() *transitCollector {
	return &transitCollector{descs: [3]*prometheus.Desc{
		prometheus.NewDesc(MetricTransitKeysLive, "Number of transit keys that are not destroyed.", nil, nil),
		prometheus.NewDesc(MetricTransitKeysRotationDue, "Number of active transit keys whose rotation period has elapsed.", nil, nil),
		prometheus.NewDesc(MetricTransitKeysPendingDeletion, "Number of transit keys waiting out their deletion delay.", nil, nil),
	}, known: prometheus.NewDesc(MetricTransitGaugesKnown, "Whether the transit gauges were measured on this scrape; they are omitted when 0.", nil, nil)}
}

// Describe sends the transit gauge descriptors and their known-status descriptor.
func (c *transitCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
	ch <- c.known
}
