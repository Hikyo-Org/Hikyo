package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/server"
)

type stubAdmissionSnapshot struct{ snap admission.Snapshot }

func (s stubAdmissionSnapshot) Snapshot() admission.Snapshot { return s.snap }

// The RED metrics appear on the operational listener in Prometheus format,
// carry only closed-enum labels (class/status/le), and reflect traffic driven
// through the public API stack — the same collector shared across both. This is
// the server-level presence-and-shape test the acceptance criteria demand.
func TestMetricsExposeREDCountersAndAdmissionGauges(t *testing.T) {
	metrics := server.NewMetrics(stubAdmissionSnapshot{snap: admission.Snapshot{
		ConcurrencyLimit: 8, InFlight: 2, QueueDepthLimit: 16, Waiting: 3, ActiveBackoffs: 1,
	}})
	apiSrv := &server.API{
		Auth: stubAuth{}, Orgs: stubOrgs{}, Providers: stubProviders{}, Version: "test",
		Projects: stubHierarchy{}, Environments: stubEnvs{}, Values: stubValues{}, Folders: stubFolders{},
		Metrics: metrics,
	}
	public := httptest.NewServer(server.New(stubReady{}, apiSrv, nil))
	t.Cleanup(public.Close)
	operational := httptest.NewServer(server.NewOperational(stubReady{}, stubRetentionHealth{}, metrics))
	t.Cleanup(operational.Close)

	// Drive three unauthenticated hierarchy reads: stubOrgs refuses with 401, a
	// deterministic hierarchy/4xx cell.
	for range 3 {
		resp, err := public.Client().Get(public.URL + api.PathPrefix + "/orgs")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	// Unmatched API traffic is still load and must land in the closed `other`
	// class instead of disappearing outside route-group middleware.
	for _, request := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: api.PathPrefix + "/does-not-exist"},
		{method: http.MethodTrace, path: api.PathPrefix + "/orgs"},
	} {
		req, err := http.NewRequest(request.method, public.URL+request.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		unmatched, err := public.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = unmatched.Body.Close()
	}
	preflight, err := http.NewRequest(http.MethodOptions, public.URL+api.PathPrefix+"/orgs", nil)
	if err != nil {
		t.Fatal(err)
	}
	preflight.Header.Set("Origin", "https://hostile.example")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodGet)
	preflightResp, err := public.Client().Do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	_ = preflightResp.Body.Close()

	resp, err := operational.Client().Get(operational.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	// The retention block is still present (shared endpoint, appended output).
	mustContain(t, body, "hikyo_last_prune_success_timestamp_seconds")

	// RED families, each with its single TYPE line.
	for _, family := range []string{
		"# TYPE " + server.MetricRequestsTotal + " counter",
		"# TYPE " + server.MetricRequestErrors + " counter",
		"# TYPE " + server.MetricRequestsInFlight + " gauge",
		"# TYPE " + server.MetricRequestDuration + " histogram",
		"# TYPE " + server.MetricAdmissionConcurrencyLimit + " gauge",
		"# TYPE " + server.MetricAdmissionInFlight + " gauge",
		"# TYPE " + server.MetricAdmissionQueueDepthLimit + " gauge",
		"# TYPE " + server.MetricAdmissionQueueWaiting + " gauge",
		"# TYPE " + server.MetricAdmissionActiveBackoffs + " gauge",
		"# TYPE " + server.MetricHAIsLeader + " gauge",
		"# TYPE " + server.MetricHANodesSeen + " gauge",
		"# TYPE " + server.MetricHALeaseAgeSecond + " gauge",
	} {
		mustContain(t, body, family)
	}

	// The three requests landed in exactly one closed cell.
	mustContain(t, body, server.MetricRequestsTotal+`{class="hierarchy",status="4xx"} 3`)
	mustContain(t, body, server.MetricRequestErrors+`{class="hierarchy",status="4xx"} 3`)
	mustContain(t, body, server.MetricRequestsTotal+`{class="other",status="4xx"} 2`)
	mustContain(t, body, server.MetricRequestErrors+`{class="other",status="4xx"} 2`)
	mustContain(t, body, server.MetricRequestsTotal+`{class="other",status="2xx"} 1`)
	// A cell that saw no traffic is still present at zero (deterministic shape).
	mustContain(t, body, server.MetricRequestsTotal+`{class="delivery",status="2xx"} 0`)
	// Histogram: +Inf bucket, sum, count for the class that saw traffic.
	mustContain(t, body, server.MetricRequestDuration+`_bucket{class="hierarchy",le="+Inf"} 3`)
	mustContain(t, body, server.MetricRequestDuration+`_count{class="hierarchy"} 3`)
	// Admission gauges reflect the snapshot.
	mustContain(t, body, server.MetricAdmissionConcurrencyLimit+" 8")
	mustContain(t, body, server.MetricAdmissionInFlight+" 2")
	mustContain(t, body, server.MetricAdmissionQueueDepthLimit+" 16")
	mustContain(t, body, server.MetricAdmissionQueueWaiting+" 3")
	mustContain(t, body, server.MetricAdmissionActiveBackoffs+" 1")

	// HA gauges render the single-node defaults when no HA source is wired, and
	// carry no per-node label (bounded cardinality: one series each).
	mustContain(t, body, server.MetricHAIsLeader+" 1")
	mustContain(t, body, server.MetricHANodesSeen+" 1")
	mustContain(t, body, server.MetricHALeaseAgeSecond+" 0")
	for _, line := range strings.Split(body, "\n") {
		for _, name := range []string{server.MetricHAIsLeader, server.MetricHANodesSeen, server.MetricHALeaseAgeSecond} {
			if strings.HasPrefix(line, name) && strings.Contains(line, "{") {
				t.Fatalf("HA gauge %q carries a label (unbounded cardinality): %q", name, line)
			}
		}
	}

	// No high-cardinality label: no raw path, no ID shape ever reaches a label.
	if strings.Contains(body, "/api/") || strings.Contains(body, "org_") {
		t.Fatalf("metrics leaked a raw path or ID into a label:\n%s", body)
	}
	assertMetricRegistryMatchesScrape(t, body)
}

func assertMetricRegistryMatchesScrape(t *testing.T, body string) {
	t.Helper()
	registry := map[string]server.MetricFamily{}
	for _, family := range server.RegisteredMetricFamilies() {
		registry[family.Name] = family
	}
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
			if !ok || !containsString(allowed, value) {
				t.Fatalf("family %q emitted unregistered label %s=%q", family, parts[0], value)
			}
		}
		series[family]++
	}
	if len(types) != len(registry) {
		t.Fatalf("TYPE families = %d, registry = %d: %v", len(types), len(registry), types)
	}
	for name, family := range registry {
		if !types[name] || series[name] != family.MaxSeries {
			t.Errorf("family %q: TYPE=%v series=%d, want %d", name, types[name], series[name], family.MaxSeries)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func mustContain(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("metrics missing %q\n---\n%s", want, body)
	}
}
