package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the real request sender: ordinary authenticated reads share the
// source allowance with authentication and MCP, including across case boundaries.
func TestTargetPacesSourceRequestsBeforeSending(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	target := &target{ctx: t.Context(), base: server.URL, client: server.Client()}
	for range 50 {
		if r, err := target.request(http.MethodGet, "/api/v1/orgs", "synthetic-session", nil); err != nil || r.status != http.StatusNoContent {
			t.Fatalf("source allowance positive control: status=%d err=%v", r.status, err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	t.Cleanup(cancel)
	target.ctx = ctx
	if _, err := target.request(http.MethodGet, "/api/v1/orgs", "synthetic-session", nil); err == nil || received.Load() != 50 {
		t.Fatalf("exhausted source allowance sent a request: received=%d err=%v", received.Load(), err)
	}
}

func TestBackendCampaignsOverlapAndJoin(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := make(chan string, 2)
	release := make(chan struct{})
	done := make(chan struct{})
	sqliteFailure := errors.New("SQLite campaign failed")
	postgresFailure := errors.New("PostgreSQL cleanup failed")
	var results []backendResult
	var resultErr error
	go func() {
		defer close(done)
		results, resultErr = runBackends(ctx, options{postgres: "synthetic configured DSN"}, func(ctx context.Context, _ options, b *backendResult) error {
			started <- b.Engine
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
			}
			b.Cleanup = "complete"
			if b.Engine == "sqlite" {
				return sqliteFailure
			}
			return postgresFailure
		})
	}()
	defer func() { cancel(); <-done }()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("datastore campaigns did not overlap within their shared deadline")
		}
	}
	if ctx.Err() != nil {
		t.Fatal("second datastore only started after the first exhausted the deadline")
	}
	close(release)
	<-done
	if len(results) != 2 || results[0].Engine != "sqlite" || results[1].Engine != "postgres" {
		t.Fatalf("receipt datastore order changed: %+v", results)
	}
	if !errors.Is(resultErr, sqliteFailure) || !errors.Is(resultErr, postgresFailure) || results[0].Cleanup != "complete" || results[1].Cleanup != "complete" {
		t.Fatalf("returned before cleanup or discarded a datastore failure: %+v, %v", results, resultErr)
	}
}

func TestBackendCampaignCancellationJoinsCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	done := make(chan struct{})
	var results []backendResult
	var resultErr error
	go func() {
		defer close(done)
		results, resultErr = runBackends(ctx, options{postgres: "synthetic configured DSN"}, func(ctx context.Context, _ options, b *backendResult) error {
			started <- struct{}{}
			<-ctx.Done()
			b.Cleanup = "complete"
			return ctx.Err()
		})
	}()
	defer func() { cancel(); <-done }()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("datastore campaign failed to start")
		}
	}
	cancel()
	<-done
	if !errors.Is(resultErr, context.Canceled) || len(results) != 2 || results[0].Cleanup != "complete" || results[1].Cleanup != "complete" {
		t.Fatalf("cancellation lost cleanup or errors: %+v, %v", results, resultErr)
	}
}

func TestBackendCampaignRequiresPostgres(t *testing.T) {
	var unconfiguredAttempt atomic.Bool
	results, err := runBackends(t.Context(), options{}, func(_ context.Context, _ options, b *backendResult) error {
		if b.Engine != "sqlite" {
			unconfiguredAttempt.Store(true)
			return errors.New("attempted an unconfigured datastore")
		}
		b.Cleanup = "complete"
		return nil
	})
	if unconfiguredAttempt.Load() || err == nil || len(results) != 2 || results[1].Engine != "postgres" || len(results[1].Cases) != len(mandatoryCases) {
		t.Fatalf("missing mandatory datastore was accepted: %+v, %v", results, err)
	}
	for _, c := range results[1].Cases {
		if c.Status != "skipped" {
			t.Fatalf("unconfigured datastore claimed execution: %+v", c)
		}
	}
}

func TestOwnedDisposableDatabaseIdentifier(t *testing.T) {
	valid := "hikyo_security_0123456789abcdef01234567"
	if identifier, err := ownedDisposableDatabaseIdentifier(valid); err != nil || identifier != `"`+valid+`"` {
		t.Fatalf("canonical generated database name refused: %q, %v", identifier, err)
	}
	for _, database := range []string{
		"", "hikyo_test", "postgres", "hikyo_security_", valid + "00",
		"hikyo_security_0123456789ABCDEF01234567", "hikyo_security_0123456789abcdeg01234567",
		valid + `"; DROP DATABASE postgres; --`, "unowned_0123456789abcdef01234567",
	} {
		t.Run(database, func(t *testing.T) {
			if identifier, err := ownedDisposableDatabaseIdentifier(database); err == nil || identifier != "" {
				t.Fatalf("unsafe or unowned database name accepted: %q", database)
			}
		})
	}
}

func TestOracleRejectsLeakedCanaryAndFalseDenial(t *testing.T) {
	twin := response{status: 404, body: []byte(`{"error":{"code":"not_found"}}`)}
	for _, r := range []response{{status: 200, body: twin.body}, {status: 404, body: []byte(`{"error":{"code":"not_found"},"leak":"protected-value"}`)}, {status: 404, body: []byte(`{"error":{"code":"not_found"},"leak":"\u0070rotected-value"}`)}} {
		if sameDenial(r, twin, 404, []string{"protected-value"}) == nil {
			t.Fatal("broken denial control accepted")
		}
	}
	if err := sameDenial(twin, twin, 404, []string{"protected-value"}); err != nil {
		t.Fatal(err)
	}
}
func TestAuthorizedOracleRejectsAlwaysDenyServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":true,"structuredContent":{"definitions":[{"name":"ALLOWED_CONFIG"}]}}}`))
	}))
	defer server.Close()
	target := &target{ctx: context.Background(), base: server.URL, client: server.Client()}
	r, err := target.rpc("legitimate-fixture-token", "org", "project", "")
	if err != nil {
		t.Fatal(err)
	}
	if successfulRPC(r, "ALLOWED_CONFIG") == nil {
		t.Fatal("always-deny server accepted as authorized control")
	}
}

func TestOracleRejectsHeaderCanary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Leaked-Secret", "protected-canary")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found"}}`))
	}))
	defer server.Close()
	target := &target{ctx: context.Background(), base: server.URL, client: server.Client()}
	got, err := target.request("GET", "/protected", "issued-fixture-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	twin := response{status: 404, body: got.body, headers: http.Header{"Content-Type": []string{"application/json"}}}
	if sameDenial(got, twin, 404, []string{"protected-canary"}) == nil {
		t.Fatal("protected response header leaked without failing oracle")
	}
	if noResponseCanary(got, []string{"protected-canary"}) == nil {
		t.Fatal("authorized control header leak accepted")
	}
}

func TestOracleRejectsExistenceHeader(t *testing.T) {
	twin := response{status: 404, body: []byte(`{"error":{"code":"not_found"}}`)}
	got := twin
	got.headers = http.Header{"X-Object-Exists": []string{"true"}}
	if sameDenial(got, twin, 404, nil) == nil {
		t.Fatal("existence metadata in an extra header accepted")
	}
	got.headers = http.Header{"Date": []string{"Tue, 14 Oct 2026 12:00:00 GMT"}}
	if err := sameDenial(got, twin, 404, nil); err != nil {
		t.Fatal("transport Date must not cause false disclosure verdict")
	}
}

func TestDenialCorrelationHeader(t *testing.T) {
	const first = "019a2b3c-4d5e-7000-8000-000000000001"
	const second = "019a2b3c-4d5e-7000-8000-000000000002"
	for _, status := range []int{http.StatusNotFound, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			body := []byte(`{"error":{"code":"not_found"}}`)
			if status == http.StatusOK {
				body = []byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":true}}`)
			}
			denial := func(values []string) response {
				return response{status: status, body: body, headers: http.Header{"X-Hikyo-Request-Id": values}}
			}
			got, twin := denial([]string{first}), denial([]string{second})
			if err := sameDenial(got, twin, status, nil); err != nil {
				t.Fatalf("independent server correlation changed denial equivalence: %v", err)
			}
			for _, values := range [][]string{
				nil, {""}, {"protected-canary"}, {strings.ToUpper(first)},
				{"019a2b3c-4d5e-4000-8000-000000000001"},
				{"019a2b3c-4d5e-7000-0000-000000000001"}, {first, second},
			} {
				bad := denial(values)
				if sameDenial(bad, twin, status, nil) == nil || sameDenial(got, bad, status, nil) == nil {
					t.Fatalf("invalid correlation accepted: %q", values)
				}
			}
			missing := twin
			missing.headers = nil
			if sameDenial(got, missing, status, nil) == nil || sameDenial(missing, twin, status, nil) == nil {
				t.Fatal("correlation presence difference accepted")
			}
			for _, canary := range []string{first, second} {
				if sameDenial(got, twin, status, []string{canary}) == nil {
					t.Fatal("normalization hid a protected correlation canary")
				}
			}
			got.headers.Set("X-Object-Exists", "true")
			if sameDenial(got, twin, status, nil) == nil {
				t.Fatal("valid correlation hid another header difference")
			}
		})
	}
}

func TestAuditOracleRequiresMatchingDurableEvent(t *testing.T) {
	for _, body := range []string{`{"items":[],"note":"grant.denied"}`, `{"items":[{"type":"grant.denied","actor_id":"wrong-principal","org_id":"org","project_id":"project","origin":"mcp","outcome":"denied","payload":{"operation":"key.list"}}]}`} {
		if requireDenialAudit(response{status: 200, body: []byte(body)}, "principal", "org", "project") == nil {
			t.Fatal("unmatched or string-only audit evidence accepted")
		}
	}
	valid := response{status: 200, body: []byte(`{"items":[{"type":"grant.denied","actor_id":"principal","org_id":"org","project_id":"project","origin":"mcp","outcome":"denied","payload":{"operation":"key.list"}}]}`)}
	if err := requireDenialAudit(valid, "principal", "org", "project"); err != nil {
		t.Fatal(err)
	}
}

func TestConditionalOracleRejectsUnconditionalSnapshot(t *testing.T) {
	for _, body := range []string{`{"current":false,"cursor":"c","keys":[]}`, `{"current":true,"cursor":"","keys":[]}`, `{"current":true,"cursor":"c","keys":[{"name":"LEAKED_NAME"}]}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		target := &target{ctx: context.Background(), base: server.URL, client: server.Client()}
		if requireCurrentDelivery(target, "/delivery?cursor=c", "token", nil) == nil {
			t.Fatal("broken current cursor response accepted")
		}
		server.Close()
	}
}

func TestRevisionOracleRejectsEmptySuccessfulResponse(t *testing.T) {
	if requireRevisionMetadata(response{status: 200, body: []byte(`{}`)}) == nil {
		t.Fatal("empty revision endpoint passed positive control")
	}
	valid := response{status: 200, body: []byte(`{"revision":2,"schema_revision":3,"change_token":"v1:fixture","keys":[{"name":"ALLOWED_CONFIG","classification":"config"},{"name":"SECOND_CONFIG","classification":"config"},{"name":"PROTECTED_SECRET","classification":"secret"}]}`)}
	if err := requireRevisionMetadata(valid); err != nil {
		t.Fatal(err)
	}
}
func TestSuccessfulControlRequiresStructuredContent(t *testing.T) {
	for _, body := range []string{`{}`, `{"result":{"content":[{"text":"ALLOWED_CONFIG"}]}}`, `{"error":{"code":1},"result":{"structuredContent":{"name":"ALLOWED_CONFIG"}}}`} {
		if successfulRPC(response{status: 200, body: []byte(body)}, "ALLOWED_CONFIG") == nil {
			t.Fatalf("false success accepted: %s", body)
		}
	}
	if err := successfulRPC(response{status: 200, body: []byte(`{"result":{"structuredContent":{"definitions":[{"name":"ALLOWED_CONFIG"}]}}}`)}, "ALLOWED_CONFIG"); err != nil {
		t.Fatal(err)
	}
}
func TestSeedReplayAndFixedLifecycle(t *testing.T) {
	a := generate(rand.New(rand.NewSource(314)), 24)
	b := generate(rand.New(rand.NewSource(314)), 24)
	if strings.Join(a, ",") != strings.Join(b, ",") || strings.Join(a[:4], ",") != "remove-read,probe,restore-read,probe" {
		t.Fatal("lifecycle replay changed")
	}
	c := generate(rand.New(rand.NewSource(315)), 24)
	if strings.Join(a, ",") == strings.Join(c, ",") {
		t.Fatal("seed does not vary campaign")
	}
	if strings.Join(a[len(a)-4:], ",") != "revoke-generated,restore-read,restart,probe" {
		t.Fatal("revocation must follow live randomized transitions")
	}
}

func TestGeneratedRestartCoverageAndBudget(t *testing.T) {
	sawRemovedLiveRestart, sawRestoredLiveRestart := false, false
	for steps := 8; steps <= 24; steps++ {
		for seed := int64(0); seed < 256; seed++ {
			actions := generate(rand.New(rand.NewSource(seed)), steps)
			if len(actions) != steps {
				t.Fatalf("seed %d steps %d: got %d actions", seed, steps, len(actions))
			}
			live, read, randomRestarts := true, true, 0
			for i, action := range actions {
				switch action {
				case "remove-read":
					read = false
				case "restore-read":
					read = true
				case "revoke-generated":
					live = false
				case "restart":
					if i >= 4 && i < steps-4 {
						randomRestarts++
						if !live {
							t.Fatal("randomized grant persistence restart used a revoked token")
						}
						sawRemovedLiveRestart = sawRemovedLiveRestart || !read
						sawRestoredLiveRestart = sawRestoredLiveRestart || read
					}
				}
			}
			if randomRestarts > 2 {
				t.Fatalf("seed %d steps %d exceeded randomized restart budget: %d", seed, steps, randomRestarts)
			}
			if live || !read {
				t.Fatal("fixed tail must retain restored grant and revoked credential")
			}
		}
	}
	if !sawRemovedLiveRestart || !sawRestoredLiveRestart {
		t.Fatal("seeded restarts never exercise both removed and restored grants with a live credential")
	}
}

func TestLiveCredentialControlRejectsFalsePositive(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for _, r := range []response{
		{status: 404, body: []byte(`{"items":[{"id":"fixture"}]}`)},
		{status: 200, body: []byte(`{}`)},
		{status: 200, body: []byte(`{"items":[{"id":"other"}]}`)},
		{status: 200, body: []byte(`{"items":[{"id":"fixture","revoked_at":"2026-10-14T11:00:00Z"}]}`)},
		{status: 200, body: []byte(`{"items":[{"id":"fixture","expires_at":"2026-10-14T12:00:00Z"}]}`)},
		{status: 200, body: []byte(`{"items":[{"id":"fixture","expires_at":"malformed"}]}`)},
	} {
		if requireLiveCredential(r, "fixture", now) == nil {
			t.Fatal("missing, denied, revoked or expired credential accepted as live")
		}
	}
	valid := response{status: 200, body: []byte(`{"items":[{"id":"fixture","expires_at":"2026-10-15T12:00:00Z"}]}`)}
	if err := requireLiveCredential(valid, "fixture", now); err != nil {
		t.Fatal(err)
	}
	if requireLiveCredential(valid, "", now) == nil {
		t.Fatal("empty expected credential ID accepted")
	}
}

func TestReadinessRequiresOperationalBarrier(t *testing.T) {
	metaCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/meta" {
			metaCalls++
			w.WriteHeader(200)
			return
		}
		if r.URL.Path != "/readyz" {
			t.Errorf("unexpected readiness route %s", r.URL.Path)
		}
		w.WriteHeader(503)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	target := &target{ctx: ctx, base: server.URL, operational: server.URL, client: server.Client()}
	if target.waitReady() == nil || metaCalls != 0 {
		t.Fatal("unready runtime was admitted through meta")
	}
}
func TestChildEnvironmentDropsCredentials(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "do-not-inherit")
	t.Setenv("HIKYO_DB", "production-do-not-inherit")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "do-not-inherit")
	t.Setenv("HTTPS_PROXY", "do-not-inherit")
	for _, env := range cleanEnv(t.TempDir()) {
		if strings.Contains(env, "do-not-inherit") {
			t.Fatal("ambient credential inherited")
		}
	}
}

func TestPostgresDSNRejectsEffectiveTargetOverrides(t *testing.T) {
	base := "postgres://fixture:fixture@127.0.0.1:5432/disposable?sslmode=disable"
	for _, key := range []string{"host", "hostaddr", "port", "dbname", "user", "password", "options", "search_path", "service", "servicefile"} {
		if _, _, err := validatePostgresDSN(base + "&" + key + "=production"); err == nil {
			t.Fatalf("effective target override %s accepted", key)
		}
	}
	u, config, err := validatePostgresDSN(base)
	if err != nil || u.Hostname() != config.Host || config.Database != "disposable" {
		t.Fatalf("explicit fixture URL rejected: %v", err)
	}
}
func TestFailureDoesNotReportPlannedAssertionsAsPassed(t *testing.T) {
	var b backendResult
	err := record(&b, "example", "three assertions", 3, func() error { return errors.New("intentional negative control") })
	if err == nil || b.Cases[0].Status != "failed" || b.Cases[0].Assertions != 0 {
		t.Fatal("failed case reported successful assertions")
	}
}
func TestMissingExecutableProducesFailedReceipt(t *testing.T) {
	r, err := execute(options{binary: t.TempDir() + "/absent", seed: 1, steps: 8, timeout: time.Second})
	if err == nil || r.Status != "failed" || r.CompletedAt.IsZero() {
		t.Fatal("unexecuted artifact campaign passed")
	}
	raw, err := json.Marshal(r)
	if err != nil || !bytes.Contains(raw, []byte(`"status":"failed"`)) {
		t.Fatal("failure receipt absent")
	}
}

func TestFailureReplayOnlyPrintsAllowlistedMetadata(t *testing.T) {
	var output bytes.Buffer
	emitReplay(&output, receipt{ArtifactSHA256: strings.Repeat("a", 64), Seed: 7, Steps: 12, BinaryVersion: "private-version", Backends: []backendResult{{Engine: "sqlite", Cleanup: "complete", Sequence: []string{"remove-read", "private-canary"}, FailurePrefix: []string{"probe", "private-token"}, Cases: []caseResult{{ID: "generated-sequence", Status: "failed", Error: "private-response-body"}}}}})
	text := output.String()
	if strings.Contains(text, "private") || !strings.Contains(text, `"case":"generated-sequence"`) || !strings.Contains(text, `"seed":7`) {
		t.Fatal("replay log exposed private material or omitted replay identity")
	}
}

// Opt-in actual artifact regression. CI supplies verified bytes and both stores;
// ordinary package tests exercise the oracle's deliberately broken controls.
func TestProductionArtifactCampaign(t *testing.T) {
	binary := os.Getenv("HIKYO_SECURITY_BINARY")
	if binary == "" {
		t.Skip("set HIKYO_SECURITY_BINARY and verified HIKYO_SECURITY_BUNDLE to execute production artifact regression")
	}
	r, err := execute(options{binary: binary, bundle: os.Getenv("HIKYO_SECURITY_BUNDLE"), postgres: os.Getenv("HIKYO_SECURITY_POSTGRES_DSN"), seed: 20261014, steps: 8, timeout: 5 * time.Minute})
	if err != nil {
		t.Fatalf("actual artifact campaign failed: %v", err)
	}
	if r.Status != "passed" || len(r.Backends) != 2 {
		t.Fatal("incomplete artifact campaign")
	}
}
