package server_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
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
		"# TYPE " + server.MetricLastBackupExportSuccess + " gauge",
		"# TYPE " + server.MetricBackupRPOExceeded + " gauge",
		"# TYPE " + server.MetricLastBackupPruneSuccess + " gauge",
		"# TYPE " + server.MetricLastRestoreDrill + " gauge",
		"# TYPE " + server.MetricRestoreDrillOK + " gauge",
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
	// The full family/label/series-count pinning against a live scrape is
	// conformance.TestMetricRegistryIsPinned (ops-spec §10 / invariant 3).
}

func TestMCPMetricsAndAccessLogsUseOnlyClosedLabels(t *testing.T) {
	metrics := server.NewMetrics(nil)
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := metrics.ObserveMCP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), log, []string{"hikyo_list_definitions"})

	for _, request := range []struct {
		method string
		tool   string
	}{
		{method: "server/discover"},
		{method: "tools/call", tool: "hikyo_list_definitions"},
		{method: "tools/call", tool: "CANARY-SECRET-TOOL"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Mcp-Method", request.method)
		if request.tool != "" {
			req.Header.Set("Mcp-Name", request.tool)
		}
		req.Header.Set("Authorization", "Bearer CANARY-BEARER")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	operational := httptest.NewRecorder()
	server.NewOperational(nil, stubRetentionHealth{}, metrics).ServeHTTP(operational,
		httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := operational.Body.String()
	mustContain(t, body, "# TYPE "+server.MetricMCPRequestsTotal+" counter")
	mustContain(t, body, "# TYPE "+server.MetricMCPRequestsInFlight+" gauge")
	mustContain(t, body, "# TYPE "+server.MetricMCPRequestDuration+" histogram")
	mustContain(t, body, server.MetricMCPRequestsTotal+`{method="server/discover",status="2xx",tool="none"} 1`)
	mustContain(t, body, server.MetricMCPRequestsTotal+`{method="tools/call",status="2xx",tool="hikyo_list_definitions"} 1`)
	mustContain(t, body, server.MetricMCPRequestsTotal+`{method="tools/call",status="2xx",tool="other"} 1`)
	if strings.Contains(body, "CANARY") || strings.Contains(logs.String(), "CANARY") {
		t.Fatalf("MCP telemetry leaked request-controlled or bearer material\nmetrics:\n%s\nlogs:\n%s", body, logs.String())
	}
}

func TestMCPMetricsRecoverAndRecordPanicsWithoutLoggingPanicValue(t *testing.T) {
	metrics := server.NewMetrics(nil)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := metrics.ObserveMCP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("Bearer tenant-secret-value")
	}), log, mcpserver.ProductionToolNames())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	request.Header.Set("Mcp-Method", "tools/call")
	request.Header.Set("Mcp-Name", mcpserver.ToolListDefinitions)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("panic response = %d, want 500", recorder.Code)
	}

	operational := httptest.NewRecorder()
	server.NewOperational(nil, stubRetentionHealth{}, metrics).ServeHTTP(operational,
		httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := operational.Body.String()
	mustContain(t, body, server.MetricMCPRequestsTotal+`{method="tools/call",status="5xx",tool="hikyo_list_definitions"} 1`)
	if strings.Contains(logs.String(), "tenant-secret-value") {
		t.Fatal("MCP panic value reached the access log")
	}
}

func mustContain(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("metrics missing %q\n---\n%s", want, body)
	}
}
