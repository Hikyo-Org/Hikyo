package conformance

// Operational /metrics drift guard (#513). This is the executable registry
// behind ops-spec §10 / invariant 3: every family, closed label value, and
// maximum series count is pinned, and the aggregate cannot exceed 1,000.

import (
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/server"
)

func TestMetricRegistryIsPinned(t *testing.T) {
	classes := []string{"auth", "hierarchy", "values", "revisions", "delivery", "scim", "admin", "other"}
	statuses := []string{"2xx", "3xx", "4xx", "5xx", "other"}
	want := []server.MetricFamily{
		{Name: "hikyo_last_prune_success_timestamp_seconds", MaxSeries: 1},
		{Name: "hikyo_prune_stale", MaxSeries: 1},
		{Name: "hikyo_project_storage_peak_bytes", MaxSeries: 1},
		{Name: "hikyo_project_storage_warn", MaxSeries: 1},
		{Name: "hikyo_tls_cert_not_after_timestamp_seconds", MaxSeries: 1},
		{Name: "hikyo_tls_reload_failures_total", MaxSeries: 1},
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
	}
	if got := server.RegisteredMetricFamilies(); !reflect.DeepEqual(got, want) {
		t.Fatalf("metric registry drifted\n got: %#v\nwant: %#v", got, want)
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
	for _, family := range server.RegisteredMetricFamilies() {
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
