package server

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
)

// RED-style request metrics for the operational /metrics surface (#513).
//
// The stance is deliberately dependency-free (no prometheus/expvar/otel): the
// exposition is hand-formatted in the same Prometheus text format the retention
// gauges already use, so the binary stays small and the no-egress posture is
// unchanged. Counters are keyed by a CLOSED surface class, never a raw path or
// an ID, so cardinality is bounded by construction — the acceptance criterion
// this ticket exists to satisfy.

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

// Metrics is the instance-wide RED collector. One instance is shared between
// the API middleware (which writes) and the operational /metrics handler (which
// reads), so both sides see the same counters.
type Metrics struct {
	// admission supplies the pressure gauges. Nil renders zeros.
	admission AdmissionSnapshotter

	inFlight atomic.Int64

	mu       sync.Mutex
	requests [numClasses][numStatusBuckets]uint64
	// buckets holds cumulative counts per class, one entry per
	// latencyBucketsSeconds boundary; +Inf is counts[class].
	buckets [numClasses][]uint64
	counts  [numClasses]uint64
	sums    [numClasses]float64
}

// NewMetrics builds a collector with the histogram bucket slices sized to the
// pinned boundaries.
func NewMetrics(adm AdmissionSnapshotter) *Metrics {
	m := &Metrics{admission: adm}
	for c := range m.buckets {
		m.buckets[c] = make([]uint64, len(latencyBucketsSeconds))
	}
	return m
}

func (m *Metrics) record(class surfaceClass, code int, d time.Duration) {
	sb := bucketForStatus(code)
	secs := d.Seconds()
	m.mu.Lock()
	m.requests[class][sb]++
	m.counts[class]++
	m.sums[class] += secs
	for i, b := range latencyBucketsSeconds {
		if secs <= b {
			m.buckets[class][i]++
		}
	}
	m.mu.Unlock()
}

// writeInto renders the RED families and admission gauges in Prometheus text
// exposition: one # TYPE line per family, all series of that family contiguous.
func (m *Metrics) writeInto(w io.Writer) {
	m.mu.Lock()
	requests := m.requests
	counts := m.counts
	sums := m.sums
	buckets := make([][]uint64, numClasses)
	for c := range m.buckets {
		buckets[c] = append([]uint64(nil), m.buckets[c]...)
	}
	m.mu.Unlock()

	inflight := m.inFlight.Load()
	var snap admission.Snapshot
	if m.admission != nil {
		snap = m.admission.Snapshot()
	}

	fmt.Fprintf(w, "# TYPE %s counter\n", MetricRequestsTotal)
	for c := surfaceClass(0); c < numClasses; c++ {
		for s := statusBucket(0); s < numStatusBuckets; s++ {
			fmt.Fprintf(w, "%s{class=%q,status=%q} %d\n",
				MetricRequestsTotal, classNames[c], statusNames[s], requests[c][s])
		}
	}
	fmt.Fprintf(w, "# TYPE %s counter\n", MetricRequestErrors)
	for c := surfaceClass(0); c < numClasses; c++ {
		for _, s := range [...]statusBucket{status4xx, status5xx} {
			fmt.Fprintf(w, "%s{class=%q,status=%q} %d\n",
				MetricRequestErrors, classNames[c], statusNames[s], requests[c][s])
		}
	}

	fmt.Fprintf(w, "# TYPE %s gauge\n%s %d\n",
		MetricRequestsInFlight, MetricRequestsInFlight, inflight)

	fmt.Fprintf(w, "# TYPE %s histogram\n", MetricRequestDuration)
	for c := surfaceClass(0); c < numClasses; c++ {
		for i, b := range latencyBucketsSeconds {
			fmt.Fprintf(w, "%s_bucket{class=%q,le=%q} %d\n",
				MetricRequestDuration, classNames[c], formatFloat(b), buckets[c][i])
		}
		fmt.Fprintf(w, "%s_bucket{class=%q,le=\"+Inf\"} %d\n",
			MetricRequestDuration, classNames[c], counts[c])
		fmt.Fprintf(w, "%s_sum{class=%q} %s\n",
			MetricRequestDuration, classNames[c], formatFloat(sums[c]))
		fmt.Fprintf(w, "%s_count{class=%q} %d\n",
			MetricRequestDuration, classNames[c], counts[c])
	}

	fmt.Fprintf(w, "# TYPE %s gauge\n%s %d\n",
		MetricAdmissionConcurrencyLimit, MetricAdmissionConcurrencyLimit, snap.ConcurrencyLimit)
	fmt.Fprintf(w, "# TYPE %s gauge\n%s %d\n",
		MetricAdmissionInFlight, MetricAdmissionInFlight, snap.InFlight)
	fmt.Fprintf(w, "# TYPE %s gauge\n%s %d\n",
		MetricAdmissionQueueDepthLimit, MetricAdmissionQueueDepthLimit, snap.QueueDepthLimit)
	fmt.Fprintf(w, "# TYPE %s gauge\n%s %d\n",
		MetricAdmissionQueueWaiting, MetricAdmissionQueueWaiting, snap.Waiting)
	fmt.Fprintf(w, "# TYPE %s gauge\n%s %d\n",
		MetricAdmissionActiveBackoffs, MetricAdmissionActiveBackoffs, snap.ActiveBackoffs)
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
