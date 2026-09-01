package server

import (
	"net/http"
	"strconv"
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
	MetricLastPruneSuccess   = "hikyo_last_prune_success_timestamp_seconds"
	MetricPruneStale         = "hikyo_prune_stale"
	MetricProjectStoragePeak = "hikyo_project_storage_peak_bytes"
	MetricProjectStorageWarn = "hikyo_project_storage_warn"
	MetricTLSCertNotAfter    = "hikyo_tls_cert_not_after_timestamp_seconds"
	MetricTLSReloadFailures  = "hikyo_tls_reload_failures_total"

	MetricRequestsTotal    = "hikyo_http_requests_total"
	MetricRequestErrors    = "hikyo_http_request_errors_total"
	MetricRequestsInFlight = "hikyo_http_requests_in_flight"
	MetricRequestDuration  = "hikyo_http_request_duration_seconds"

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

// MetricFamily is one immutable-at-runtime registration returned as a copy to
// conformance tests. Labels maps each closed label key to every value the
// exporter can emit. MaxSeries includes histogram buckets, sum, and count.
type MetricFamily struct {
	Name      string
	MaxSeries int
	Labels    map[string][]string
}

// RegisteredMetricFamilies returns the complete static metric registry. Every
// map and slice is newly allocated, so callers cannot mutate live exporter
// state. The scrape test proves this registry and the emitted families agree.
func RegisteredMetricFamilies() []MetricFamily {
	classes := metricClassNames()
	statuses := metricStatusNames()
	errors := []string{"4xx", "5xx"}
	buckets := make([]string, 0, len(latencyBucketsSeconds)+1)
	for _, bucket := range latencyBucketsSeconds {
		buckets = append(buckets, formatFloat(bucket))
	}
	buckets = append(buckets, "+Inf")
	return []MetricFamily{
		{Name: MetricLastPruneSuccess, MaxSeries: 1},
		{Name: MetricPruneStale, MaxSeries: 1},
		{Name: MetricProjectStoragePeak, MaxSeries: 1},
		{Name: MetricProjectStorageWarn, MaxSeries: 1},
		{Name: MetricTLSCertNotAfter, MaxSeries: 1},
		{Name: MetricTLSReloadFailures, MaxSeries: 1},
		{Name: MetricRequestsTotal, MaxSeries: len(classes) * len(statuses), Labels: map[string][]string{"class": classes, "status": statuses}},
		{Name: MetricRequestErrors, MaxSeries: len(classes) * len(errors), Labels: map[string][]string{"class": classes, "status": errors}},
		{Name: MetricRequestsInFlight, MaxSeries: 1},
		{Name: MetricRequestDuration, MaxSeries: len(classes) * (len(buckets) + 2), Labels: map[string][]string{"class": classes, "le": buckets}},
		{Name: MetricAdmissionConcurrencyLimit, MaxSeries: 1},
		{Name: MetricAdmissionInFlight, MaxSeries: 1},
		{Name: MetricAdmissionQueueDepthLimit, MaxSeries: 1},
		{Name: MetricAdmissionQueueWaiting, MaxSeries: 1},
		{Name: MetricAdmissionActiveBackoffs, MaxSeries: 1},
		{Name: MetricHAIsLeader, MaxSeries: 1},
		{Name: MetricHANodesSeen, MaxSeries: 1},
		{Name: MetricHALeaseAgeSecond, MaxSeries: 1},
	}
}

// RequestLatencyBucketsSeconds returns a copy of the fixed histogram grid.
func RequestLatencyBucketsSeconds() []float64 {
	return append([]float64(nil), latencyBucketsSeconds[:]...)
}

func metricClassNames() []string { return append([]string(nil), classNames[:]...) }

func metricStatusNames() []string { return append([]string(nil), statusNames[:]...) }

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

	inFlight  prometheus.Gauge
	requests  [numClasses][numStatusBuckets]prometheus.Counter
	errors    [numClasses][numStatusBuckets]prometheus.Counter
	durations [numClasses]prometheus.Observer
	ha        *haCollector
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
	ha := newHACollector()
	registry.MustRegister(requests, errors, inFlight, durations, newAdmissionCollector(adm), ha)

	m := &Metrics{registry: registry, inFlight: inFlight, ha: ha}
	for c := surfaceClass(0); c < numClasses; c++ {
		for s := statusBucket(0); s < numStatusBuckets; s++ {
			m.requests[c][s] = requests.WithLabelValues(classNames[c], statusNames[s])
		}
		for _, s := range [...]statusBucket{status4xx, status5xx} {
			m.errors[c][s] = errors.WithLabelValues(classNames[c], statusNames[s])
		}
		m.durations[c] = durations.WithLabelValues(classNames[c])
	}
	return m
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
		prometheus.NewDesc(MetricHALeaseAgeSecond, "Age in seconds of the current scheduler lease (0 on a single node).", nil, nil),
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

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
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
