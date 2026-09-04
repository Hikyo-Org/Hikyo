package conformance

// Operational /metrics drift guard (#513). This is the executable registry
// behind ops-spec §10 / invariant 3: every family, closed label value, and
// maximum series count is pinned against the ACTUAL scrape of a fresh
// server.NewOperational handler, and the aggregate cannot exceed 1,000.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// metricFamily is the pinned shape of one registered metric family: its name,
// the maximum number of series it may emit (histogram buckets, sum and count
// included), and every value each closed label key may carry.
type metricFamily struct {
	Name      string
	MaxSeries int
	Labels    map[string][]string
}

type stubAdmissionSnapshot struct{}

func (stubAdmissionSnapshot) Snapshot() admission.Snapshot { return admission.Snapshot{} }

// stubRetentionHealth supplies the operational /metrics handler with a
// zero-valued health snapshot: every retention, storage, TLS, backup and
// adapter family is emitted (at zero), which is all the registry pinning needs.
type stubRetentionHealth struct{}

func (stubRetentionHealth) OperationalHealth(context.Context) (service.PruneHealth, error) {
	return service.PruneHealth{}, nil
}

// pinnedMetricRegistry is the ops-spec §10 registry, restated once here as the
// single source of truth the scrape is checked against.
func pinnedMetricRegistry() []metricFamily {
	classes := []string{"auth", "hierarchy", "values", "revisions", "delivery", "scim", "admin", "other"}
	statuses := []string{"2xx", "3xx", "4xx", "5xx", "other"}
	return []metricFamily{
		{Name: "hikyo_last_prune_success_timestamp_seconds", MaxSeries: 1},
		{Name: "hikyo_prune_stale", MaxSeries: 1},
		{Name: "hikyo_project_storage_peak_bytes", MaxSeries: 1},
		{Name: "hikyo_project_storage_warn", MaxSeries: 1},
		{Name: "hikyo_tls_cert_not_after_timestamp_seconds", MaxSeries: 1},
		{Name: "hikyo_tls_reload_failures_total", MaxSeries: 1},
		{Name: "hikyo_adapter_targets_failed", MaxSeries: 1},
		{Name: "hikyo_adapter_targets_paused", MaxSeries: 1},
		{Name: "hikyo_adapter_targets_attention", MaxSeries: 1},
		{Name: "hikyo_adapter_jobs_queued", MaxSeries: 1},
		{Name: "hikyo_http_requests_total", MaxSeries: 40, Labels: map[string][]string{"class": classes, "status": statuses}},
		{Name: "hikyo_http_request_errors_total", MaxSeries: 16, Labels: map[string][]string{"class": classes, "status": {"4xx", "5xx"}}},
		{Name: "hikyo_http_requests_in_flight", MaxSeries: 1},
		{Name: "hikyo_http_request_duration_seconds", MaxSeries: 72, Labels: map[string][]string{"class": classes, "le": {"0.005", "0.025", "0.1", "0.5", "1", "5", "+Inf"}}},
		{Name: "hikyo_admission_concurrency_limit", MaxSeries: 1},
		{Name: "hikyo_admission_in_flight", MaxSeries: 1},
		{Name: "hikyo_admission_queue_depth_limit", MaxSeries: 1},
		{Name: "hikyo_admission_queue_waiting", MaxSeries: 1},
		{Name: "hikyo_admission_active_backoffs", MaxSeries: 1},
		{Name: "hikyo_ha_is_leader", MaxSeries: 1},
		{Name: "hikyo_ha_nodes_seen", MaxSeries: 1},
		{Name: "hikyo_ha_lease_age_seconds", MaxSeries: 1},
		{Name: "hikyo_approval_requests_open", MaxSeries: 1},
		{Name: "hikyo_approval_requests_expired", MaxSeries: 1},
		// Disaster-recovery gauges (#145): label-free, one series each.
		{Name: "hikyo_last_backup_export_success_timestamp_seconds", MaxSeries: 1},
		{Name: "hikyo_backup_rpo_exceeded", MaxSeries: 1},
		{Name: "hikyo_last_backup_prune_success_timestamp_seconds", MaxSeries: 1},
		{Name: "hikyo_last_restore_drill_timestamp_seconds", MaxSeries: 1},
		{Name: "hikyo_restore_drill_ok", MaxSeries: 1},
		{Name: "hikyo_dynamic_leases_active", MaxSeries: 1},
		{Name: "hikyo_dynamic_effects_unknown", MaxSeries: 1},
	}
}

// scrapeOperationalMetrics returns the /metrics body of a fresh operational
// handler. NewMetrics pre-registers every label combination eagerly, so the
// scrape emits the complete series set (at zero) without driving any traffic.
func scrapeOperationalMetrics(t *testing.T) string {
	t.Helper()
	metrics := server.NewMetrics(stubAdmissionSnapshot{})
	handler := server.NewOperational(nil, stubRetentionHealth{}, metrics)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics returned %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestMetricRegistryIsPinned(t *testing.T) {
	want := pinnedMetricRegistry()
	registry := map[string]metricFamily{}
	for _, family := range want {
		registry[family.Name] = family
	}

	body := scrapeOperationalMetrics(t)
	types := map[string]bool{}
	series := map[string]int{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# TYPE ") {
			fields := strings.Fields(line)
			if len(fields) == 4 {
				types[fields[2]] = true
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		seriesToken := strings.Fields(line)[0]
		name := seriesToken
		labels := ""
		if open := strings.IndexByte(seriesToken, '{'); open >= 0 {
			name = seriesToken[:open]
			labels = seriesToken[open+1 : len(seriesToken)-1]
		}
		family := name
		if _, ok := registry[family]; !ok {
			for _, suffix := range []string{"_bucket", "_sum", "_count"} {
				if strings.HasSuffix(name, suffix) {
					family = strings.TrimSuffix(name, suffix)
					break
				}
			}
		}
		if _, ok := registry[family]; !ok {
			t.Fatalf("scrape emitted unregistered series %q", name)
		}
		for _, pair := range strings.Split(labels, ",") {
			if pair == "" {
				continue
			}
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) != 2 {
				t.Fatalf("invalid label pair %q in %q", pair, line)
			}
			allowed, ok := registry[family].Labels[parts[0]]
			value := strings.Trim(parts[1], `"`)
			if !ok || !slices.Contains(allowed, value) {
				t.Fatalf("family %q emitted unregistered label %s=%q", family, parts[0], value)
			}
		}
		series[family]++
	}

	if len(types) != len(registry) {
		t.Fatalf("TYPE families = %d, registry = %d: %v", len(types), len(registry), types)
	}
	for _, family := range want {
		if !types[family.Name] || series[family.Name] != family.MaxSeries {
			t.Errorf("family %q: TYPE=%v series=%d, want %d", family.Name, types[family.Name], series[family.Name], family.MaxSeries)
		}
	}

	if got, want := server.RequestLatencyBucketsSeconds(), []float64{0.005, 0.025, 0.1, 0.5, 1, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("latency buckets drifted\n got: %v\nwant: %v", got, want)
	}
}

func TestMetricRegistryStaysWithinCardinalityBudget(t *testing.T) {
	if server.MetricSeriesBudget != 1000 {
		t.Fatalf("metric series budget = %d, ops-spec requires 1000", server.MetricSeriesBudget)
	}
	allowedLabelKeys := map[string]bool{"class": true, "status": true, "le": true}
	forbidden := map[string]bool{"key": true, "principal": true, "credential": true, "env": true, "org": true, "project": true}
	seen := map[string]bool{}
	total := 0
	for _, family := range pinnedMetricRegistry() {
		if family.Name == "" || family.MaxSeries < 1 || seen[family.Name] {
			t.Fatalf("invalid metric registration: %+v", family)
		}
		seen[family.Name] = true
		total += family.MaxSeries
		for key, values := range family.Labels {
			if forbidden[key] || !allowedLabelKeys[key] || len(values) == 0 {
				t.Fatalf("metric %q has forbidden or open label %q: %v", family.Name, key, values)
			}
		}
	}
	if total > server.MetricSeriesBudget {
		t.Fatalf("registered metric series = %d, budget = %d", total, server.MetricSeriesBudget)
	}
}
