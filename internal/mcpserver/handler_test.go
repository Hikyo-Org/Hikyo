package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/operation"
)

type echoInput struct {
	Value string `json:"value" jsonschema:"required"`
}

type echoOutput struct {
	Value string `json:"value"`
}

type fixedAdmission bool

func (a fixedAdmission) AllowDiscovery(string) bool { return bool(a) }

func testContract(t *testing.T, name string) operation.Contract {
	t.Helper()
	contract, err := operation.NewContract("mcp:"+name, "key.list", []string{"read@project"}, []string{operation.ArtifactMachineCredential})
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func testRegistry(t *testing.T, names ...string) (*Registry, <-chan string) {
	t.Helper()
	registry := NewRegistry()
	seen := make(chan string, 10)
	for _, name := range names {
		name := name
		err := Register(registry, ToolSpec{
			Name: name, Description: "Read one test value.",
			ServiceOperation: "service.Keys.List", Contract: testContract(t, name),
			AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
		}, func(ctx context.Context, bearer Bearer, input echoInput) (echoOutput, error) {
			contract, ok := operation.FromContext(ctx)
			if !ok || contract.ID != "mcp:"+name {
				return echoOutput{}, errors.New("operation contract missing")
			}
			seen <- bearer.raw()
			return echoOutput{Value: input.Value}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return registry, seen
}

func modernBody(id int, method, name, arguments string) []byte {
	params := `{"_meta":{"io.modelcontextprotocol/protocolVersion":"` + ProtocolVersion + `","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}}`
	if method == "tools/call" {
		params = `{"name":` + strconvQuote(name) + `,"arguments":` + arguments + `,"_meta":{"io.modelcontextprotocol/protocolVersion":"` + ProtocolVersion + `","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}}`
	}
	return []byte(`{"jsonrpc":"2.0","id":` + strconvItoa(id) + `,"method":` + strconvQuote(method) + `,"params":` + params + `}`)
}

func strconvQuote(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func strconvItoa(value int) string { return strconv.FormatInt(int64(value), 10) }

func request(method, target, rpcMethod, name string, body []byte) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Host = "hikyo.example.com"
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", ProtocolVersion)
		req.Header.Set("Mcp-Method", rpcMethod)
		if name != "" {
			req.Header.Set("Mcp-Name", name)
		}
	}
	return req
}

func testHandler(t *testing.T, registry *Registry) http.Handler {
	t.Helper()
	h, err := New(Options{
		Registry:       registry,
		ExternalOrigin: "https://hikyo.example.com",
		AllowedOrigins: []string{"https://assistant.example.com"},
		Version:        "v-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func serve(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v: %q", err, rec.Body.String())
	}
	return body
}

func TestStatelessDiscoveryUsesOnlyPinnedProtocolAndCapabilities(t *testing.T) {
	registry, _ := testRegistry(t, "zeta", "alpha")
	h := testHandler(t, registry)
	req := request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", modernBody(1, "server/discover", "", ""))
	// Static metadata never resolves or validates a presented bearer.
	req.Header.Add("Authorization", "Bearer ignored-one")
	req.Header.Add("Authorization", "Bearer ignored-two")
	rec := serve(t, h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Mcp-Session-Id") != "" {
		t.Fatal("stateless discovery emitted a session id")
	}
	body := decodeResponse(t, rec)
	result := body["result"].(map[string]any)
	versions := result["supportedVersions"].([]any)
	if len(versions) != 1 || versions[0] != ProtocolVersion {
		t.Fatalf("supportedVersions = %v", versions)
	}
	capabilities := result["capabilities"].(map[string]any)
	if len(capabilities) != 1 || capabilities["tools"] == nil {
		t.Fatalf("capabilities = %v", capabilities)
	}
}

func TestAdapterPreservesOpaqueJSONRPCID(t *testing.T) {
	registry, seen := testRegistry(t, "echo")
	h := testHandler(t, registry)
	body := bytes.Replace(modernBody(1, "server/discover", "", ""), []byte(`"id":1`), []byte(`"id":9007199254740993`), 1)
	rec := serve(t, h, request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", body))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":9007199254740993`) {
		t.Fatalf("discovery id = %d %q", rec.Code, rec.Body.String())
	}

	body = bytes.Replace(modernBody(1, "tools/call", "echo", `{"value":"ok"}`), []byte(`"id":1`), []byte(`"id":9007199254740993`), 1)
	req := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "echo", body)
	req.Header.Set("Authorization", "Bearer token")
	rec = serve(t, h, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":9007199254740993`) {
		t.Fatalf("tool id = %d %q", rec.Code, rec.Body.String())
	}
	<-seen
}

func TestClosedToolCatalogIsDeterministicAndCallable(t *testing.T) {
	registry, seen := testRegistry(t, "zeta", "alpha")
	h := testHandler(t, registry)

	list := serve(t, h, request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/list", "", modernBody(1, "tools/list", "", "")))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d: %q", list.Code, list.Body.String())
	}
	result := decodeResponse(t, list)["result"].(map[string]any)
	tools := result["tools"].([]any)
	var names []string
	for _, item := range tools {
		names = append(names, item.(map[string]any)["name"].(string))
	}
	if !slices.Equal(names, []string{"alpha", "zeta"}) {
		t.Fatalf("catalog order = %v", names)
	}

	call := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "alpha", modernBody(2, "tools/call", "alpha", `{"value":"ok"}`))
	call.Header.Set("Authorization", "Bearer raw-service-account-token")
	rec := serve(t, h, call)
	if rec.Code != http.StatusOK {
		t.Fatalf("call status = %d: %q", rec.Code, rec.Body.String())
	}
	select {
	case got := <-seen:
		if got != "raw-service-account-token" {
			t.Fatalf("bearer = %q", got)
		}
	default:
		t.Fatal("registered operation was not called")
	}
	if strings.Contains(rec.Body.String(), "raw-service-account-token") {
		t.Fatal("bearer leaked into response")
	}
	structured := decodeResponse(t, rec)["result"].(map[string]any)["structuredContent"].(map[string]any)
	if structured["value"] != "ok" {
		t.Fatalf("structuredContent = %v", structured)
	}

	unknown := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "missing", modernBody(3, "tools/call", "missing", `{}`))
	unknown.Header.Set("Authorization", "Bearer raw-service-account-token")
	unknownRec := serve(t, h, unknown)
	if unknownRec.Code != http.StatusBadRequest || !strings.Contains(unknownRec.Body.String(), `"code":-32602`) || !strings.Contains(unknownRec.Body.String(), "unknown tool") {
		t.Fatalf("unknown tool = %d %q", unknownRec.Code, unknownRec.Body.String())
	}
}

func TestTransportSecurityAndBounds(t *testing.T) {
	registry, _ := testRegistry(t, "echo")
	h := testHandler(t, registry)
	validBody := modernBody(1, "server/discover", "", "")

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		rec := serve(t, h, request(method, "https://hikyo.example.com/mcp", "", "", nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
			t.Errorf("%s = %d Allow %q", method, rec.Code, rec.Header().Get("Allow"))
		}
	}

	badHost := request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", validBody)
	badHost.Host = "attacker.example.com"
	if rec := serve(t, h, badHost); rec.Code != http.StatusForbidden {
		t.Errorf("bad Host status = %d", rec.Code)
	}
	badOrigin := request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", validBody)
	badOrigin.Header.Set("Origin", "https://attacker.example.com")
	if rec := serve(t, h, badOrigin); rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("bad Origin = %d CORS %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
	allowedOrigin := request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", validBody)
	allowedOrigin.Header.Set("Origin", "https://assistant.example.com")
	if rec := serve(t, h, allowedOrigin); rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("allowed Origin = %d CORS %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}

	badType := request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", validBody)
	badType.Header.Set("Content-Type", "text/plain")
	if rec := serve(t, h, badType); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("content type status = %d", rec.Code)
	}
	badAccept := request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", validBody)
	badAccept.Header.Set("Accept", "application/json")
	if rec := serve(t, h, badAccept); rec.Code != http.StatusBadRequest {
		t.Errorf("Accept status = %d", rec.Code)
	}
	over := request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", bytes.Repeat([]byte("x"), MaxRequestBytes+1))
	if rec := serve(t, h, over); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize status = %d", rec.Code)
	}
}

func TestTrustedProxyAuthorityIsExact(t *testing.T) {
	registry, _ := testRegistry(t, "echo")
	_, proxyNet, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{
		Registry: registry, ExternalOrigin: "https://hikyo.example.com",
		TrustedProxies: []*net.IPNet{proxyNet}, Version: "v-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	valid := request(http.MethodPost, "https://internal/mcp", "server/discover", "", modernBody(1, "server/discover", "", ""))
	valid.Host = "internal:8080"
	valid.RemoteAddr = "10.0.0.2:1234"
	valid.Header.Set("X-Forwarded-Host", "hikyo.example.com")
	valid.Header.Set("X-Forwarded-Proto", "https")
	if rec := serve(t, h, valid); rec.Code != http.StatusOK {
		t.Fatalf("trusted authority = %d %q", rec.Code, rec.Body.String())
	}

	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Set("X-Forwarded-Host", "attacker.example.com") },
		func(r *http.Request) { r.Header.Add("X-Forwarded-Host", "hikyo.example.com") },
		func(r *http.Request) { r.Header.Del("X-Forwarded-Host") },
		func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "http") },
		func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https,http") },
		func(r *http.Request) { r.Header.Del("X-Forwarded-Proto") },
	} {
		req := valid.Clone(valid.Context())
		req.Body = io.NopCloser(bytes.NewReader(modernBody(1, "server/discover", "", "")))
		mutate(req)
		if rec := serve(t, h, req); rec.Code != http.StatusForbidden {
			t.Fatalf("ambiguous forwarded authority = %d %q", rec.Code, rec.Body.String())
		}
	}

	untrusted := valid.Clone(valid.Context())
	untrusted.Body = io.NopCloser(bytes.NewReader(modernBody(1, "server/discover", "", "")))
	untrusted.RemoteAddr = "192.0.2.1:1234"
	if rec := serve(t, h, untrusted); rec.Code != http.StatusForbidden {
		t.Fatalf("untrusted forwarded authority = %d", rec.Code)
	}
}

func TestHandlerConfigurationRejectsNonCanonicalOrigins(t *testing.T) {
	for _, options := range []Options{
		{ExternalOrigin: "https://user@hikyo.example.com"},
		{ExternalOrigin: "ftp://hikyo.example.com"},
		{ExternalOrigin: "https://hikyo.example.com/path"},
		{ExternalOrigin: "https://hikyo.example.com", AllowedOrigins: []string{"*"}},
		{ExternalOrigin: "https://hikyo.example.com", AllowedOrigins: []string{"https://assistant.example.com/path"}},
	} {
		if _, err := New(options); err == nil {
			t.Fatalf("invalid options accepted: %#v", options)
		}
	}
}

func TestProtocolMirrorHeadersAreRequiredAndExact(t *testing.T) {
	registry, _ := testRegistry(t, "echo")
	h := testHandler(t, registry)
	base := request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", modernBody(1, "server/discover", "", ""))

	cases := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{name: "missing version", mutate: func(r *http.Request) { r.Header.Del("Mcp-Protocol-Version") }},
		{name: "missing method", mutate: func(r *http.Request) { r.Header.Del("Mcp-Method") }},
		{name: "mismatched method", mutate: func(r *http.Request) { r.Header.Set("Mcp-Method", "tools/list") }},
		{name: "ambiguous method", mutate: func(r *http.Request) { r.Header.Add("Mcp-Method", "server/discover") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base.Clone(base.Context())
			req.Body = io.NopCloser(bytes.NewReader(modernBody(1, "server/discover", "", "")))
			tc.mutate(req)
			rec := serve(t, h, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":-32020`) {
				t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
			}
		})
	}

	call := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "wrong", modernBody(2, "tools/call", "echo", `{"value":"ok"}`))
	call.Header.Set("Authorization", "Bearer token")
	if rec := serve(t, h, call); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":-32020`) {
		t.Fatalf("mismatched name = %d %q", rec.Code, rec.Body.String())
	}

	session := request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", modernBody(3, "server/discover", "", ""))
	session.Header.Set("Mcp-Session-Id", "attacker-state")
	if rec := serve(t, h, session); rec.Code != http.StatusBadRequest || rec.Header().Get("Mcp-Session-Id") != "" {
		t.Fatalf("session header = %d response session %q", rec.Code, rec.Header().Get("Mcp-Session-Id"))
	}
}

func TestDiscoveryAdmissionAndToolConcurrencyAreBounded(t *testing.T) {
	registry, _ := testRegistry(t, "echo")
	h, err := New(Options{
		Registry: registry, ExternalOrigin: "https://hikyo.example.com",
		Admission: fixedAdmission(false), Version: "v-test", MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"server/discover", "tools/list"} {
		rec := serve(t, h, request(http.MethodPost, "https://hikyo.example.com/mcp", method, "", modernBody(1, method, "", "")))
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("%s admission = %d Retry-After %q", method, rec.Code, rec.Header().Get("Retry-After"))
		}
	}

	blocking := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	if err := Register(blocking, ToolSpec{
		Name: "wait", Description: "Wait for release.", ServiceOperation: "service.Keys.List",
		Contract: testContract(t, "wait"), AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
	}, func(context.Context, Bearer, echoInput) (echoOutput, error) {
		close(started)
		<-release
		return echoOutput{Value: "ok"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	h, err = New(Options{Registry: blocking, ExternalOrigin: "https://hikyo.example.com", Version: "v-test", MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	first := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "wait", modernBody(2, "tools/call", "wait", `{"value":"x"}`))
	first.Header.Set("Authorization", "Bearer token")
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, first)
		firstDone <- rec
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first operation did not start")
	}
	second := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "wait", modernBody(3, "tools/call", "wait", `{"value":"x"}`))
	second.Header.Set("Authorization", "Bearer token")
	if rec := serve(t, h, second); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("concurrent call = %d Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	close(release)
	select {
	case rec := <-firstDone:
		if rec.Code != http.StatusOK {
			t.Fatalf("first response = %d %q", rec.Code, rec.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("first operation did not return")
	}
}

func TestToolDeadlineAndAuthorizationHeaderAreBounded(t *testing.T) {
	registry := NewRegistry()
	remaining := make(chan time.Duration, 1)
	called := make(chan struct{}, 1)
	if err := Register(registry, ToolSpec{
		Name: "deadline", Description: "Report deadline.", ServiceOperation: "service.Keys.List",
		Contract: testContract(t, "deadline"), AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
	}, func(ctx context.Context, _ Bearer, _ echoInput) (echoOutput, error) {
		called <- struct{}{}
		deadline, ok := ctx.Deadline()
		if !ok {
			return echoOutput{}, errors.New("missing deadline")
		}
		remaining <- time.Until(deadline)
		return echoOutput{Value: "ok"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	h := testHandler(t, registry)

	missing := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "deadline", modernBody(1, "tools/call", "deadline", `{"value":"x"}`))
	if rec := serve(t, h, missing); rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("missing bearer = %d challenge %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
	select {
	case <-called:
		t.Fatal("missing bearer reached operation")
	default:
	}

	duplicate := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "deadline", modernBody(2, "tools/call", "deadline", `{"value":"x"}`))
	duplicate.Header.Add("Authorization", "Bearer one")
	duplicate.Header.Add("Authorization", "Bearer two")
	if rec := serve(t, h, duplicate); rec.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate bearer = %d", rec.Code)
	}

	valid := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "deadline", modernBody(3, "tools/call", "deadline", `{"value":"x"}`))
	valid.Header.Set("Authorization", "Bearer token")
	if rec := serve(t, h, valid); rec.Code != http.StatusOK {
		t.Fatalf("valid call = %d %q", rec.Code, rec.Body.String())
	}
	select {
	case got := <-remaining:
		if got <= 29*time.Second || got > ToolExecutionTimeout {
			t.Fatalf("deadline remaining = %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("operation did not report deadline")
	}
}

func TestValidatedStaticNotificationReturns202WithoutBody(t *testing.T) {
	registry, _ := testRegistry(t, "echo")
	h := testHandler(t, registry)
	for _, method := range []string{"server/discover", "tools/list"} {
		body := bytes.Replace(modernBody(1, method, "", ""), []byte(`"id":1,`), nil, 1)
		rec := serve(t, h, request(http.MethodPost, "https://hikyo.example.com/mcp", method, "", body))
		if rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
			t.Fatalf("%s notification = %d %q", method, rec.Code, rec.Body.String())
		}
	}

	malformed := bytes.Replace(modernBody(1, "tools/list", "", ""), []byte(`"id":1,`), nil, 1)
	malformed = bytes.Replace(malformed, []byte(`"params":{`), []byte(`"params":{"cursor":123,`), 1)
	rec := serve(t, h, request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/list", "", malformed))
	if rec.Code == http.StatusAccepted || strings.Contains(rec.Body.String(), `"id":0`) {
		t.Fatalf("notification = %d %q", rec.Code, rec.Body.String())
	}

	largeRegistry := NewRegistry()
	if err := Register(largeRegistry, ToolSpec{
		Name: "large", Description: strings.Repeat("x", MaxStaticResponseBytes), ServiceOperation: "service.Keys.List",
		Contract: testContract(t, "large"), AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
	}, func(context.Context, Bearer, echoInput) (echoOutput, error) { return echoOutput{}, nil }); err != nil {
		t.Fatal(err)
	}
	largeHandler := testHandler(t, largeRegistry)
	largeBody := bytes.Replace(modernBody(1, "tools/list", "", ""), []byte(`"id":1,`), nil, 1)
	rec = serve(t, largeHandler, request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/list", "", largeBody))
	if rec.Code != http.StatusInternalServerError || rec.Body.Len() > 256 {
		t.Fatalf("oversized notification = %d bytes=%d", rec.Code, rec.Body.Len())
	}
}

func TestLegacyAndUnregisteredMethodsAreExplicitlyRefused(t *testing.T) {
	registry, _ := testRegistry(t, "echo")
	h := testHandler(t, registry)

	legacy := modernBody(1, "server/discover", "", "")
	legacy = bytes.ReplaceAll(legacy, []byte(ProtocolVersion), []byte("2025-11-25"))
	req := request(http.MethodPost, "https://hikyo.example.com/mcp", "server/discover", "", legacy)
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	rec := serve(t, h, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":-32022`) || !strings.Contains(rec.Body.String(), ProtocolVersion) {
		t.Fatalf("legacy response = %d %q", rec.Code, rec.Body.String())
	}

	unknownBody := modernBody(2, "resources/list", "", "")
	unknown := request(http.MethodPost, "https://hikyo.example.com/mcp", "resources/list", "", unknownBody)
	unknownRec := serve(t, h, unknown)
	if unknownRec.Code != http.StatusNotFound || !strings.Contains(unknownRec.Body.String(), `"code":-32601`) {
		t.Fatalf("unregistered method = %d %q", unknownRec.Code, unknownRec.Body.String())
	}
}

func TestMalformedJSONRPCPrecedesUnknownMethodPolicy(t *testing.T) {
	registry, _ := testRegistry(t, "echo")
	h := testHandler(t, registry)
	validUnknown := modernBody(1, "resources/list", "", "")
	for _, body := range [][]byte{
		bytes.Replace(validUnknown, []byte(`"jsonrpc":"2.0"`), []byte(`"jsonrpc":"1.0"`), 1),
		bytes.Replace(validUnknown, []byte(`"jsonrpc":"2.0",`), nil, 1),
	} {
		req := request(http.MethodPost, "https://hikyo.example.com/mcp", "resources/list", "", body)
		rec := serve(t, h, req)
		if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), `"code":-32601`) {
			t.Fatalf("malformed unknown method = %d %q", rec.Code, rec.Body.String())
		}
	}
}

func TestToolErrorsAreSafeAndExecutionIsBounded(t *testing.T) {
	registry := NewRegistry()
	err := Register(registry, ToolSpec{
		Name: "failure", Description: "Fail safely.", ServiceOperation: "service.Keys.List",
		Contract: testContract(t, "failure"), AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
	}, func(context.Context, Bearer, echoInput) (echoOutput, error) {
		return echoOutput{}, errors.New("database row contains CANARY-SECRET")
	})
	if err != nil {
		t.Fatal(err)
	}
	h := testHandler(t, registry)
	req := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "failure", modernBody(1, "tools/call", "failure", `{"value":"x"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := serve(t, h, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), SafeOperationError) || strings.Contains(rec.Body.String(), "CANARY-SECRET") {
		t.Fatalf("safe error response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestAuthenticatedRateLimitUsesUniformHTTPResponse(t *testing.T) {
	registry := NewRegistry()
	err := Register(registry, ToolSpec{
		Name: "limited", Description: "Refuse a limited call.", ServiceOperation: "service.Keys.List",
		Contract: testContract(t, "limited"), AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
	}, func(context.Context, Bearer, echoInput) (echoOutput, error) {
		return echoOutput{}, fmt.Errorf("CANARY-RATE-DETAIL: %w", ErrRateLimited)
	})
	if err != nil {
		t.Fatal(err)
	}
	h := testHandler(t, registry)
	req := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "limited", modernBody(1, "tools/call", "limited", `{"value":"x"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := serve(t, h, req)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "60" ||
		strings.Contains(rec.Body.String(), "CANARY-RATE-DETAIL") {
		t.Fatalf("rate response = %d Retry-After %q body %q", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

func TestCancellationReachesRegisteredOperation(t *testing.T) {
	registry := NewRegistry()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	err := Register(registry, ToolSpec{
		Name: "wait", Description: "Wait for cancellation.", ServiceOperation: "service.Keys.List",
		Contract: testContract(t, "wait"), AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
	}, func(ctx context.Context, _ Bearer, _ echoInput) (echoOutput, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return echoOutput{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	h := testHandler(t, registry)
	ctx, cancel := context.WithCancel(context.Background())
	req := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "wait", modernBody(1, "tools/call", "wait", `{"value":"x"}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer token")
	done := make(chan struct{})
	go func() {
		serve(t, h, req)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("operation did not start")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not reach operation")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled request did not return")
	}
}

func TestSuccessiveRequestsCanAlternateReplicas(t *testing.T) {
	registryA, seenA := testRegistry(t, "echo")
	registryB, seenB := testRegistry(t, "echo")
	replicas := []http.Handler{testHandler(t, registryA), testHandler(t, registryB)}
	seen := []<-chan string{seenA, seenB}
	for i, replica := range replicas {
		req := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", "echo", modernBody(i+1, "tools/call", "echo", `{"value":"ok"}`))
		req.Header.Set("Authorization", "Bearer token")
		rec := serve(t, replica, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Mcp-Session-Id") != "" {
			t.Fatalf("replica %d = %d session %q", i, rec.Code, rec.Header().Get("Mcp-Session-Id"))
		}
		<-seen[i]
	}
}

func TestBearerFormattingAlwaysRedacts(t *testing.T) {
	b := newBearer("CANARY-TOKEN")
	for _, formatted := range []string{b.String(), fmt.Sprintf("%v", b), fmt.Sprintf("%#v", b)} {
		if strings.Contains(formatted, "CANARY-TOKEN") {
			t.Fatalf("bearer formatting leaked: %q", formatted)
		}
	}
}
