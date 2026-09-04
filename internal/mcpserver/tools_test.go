package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// fakeEnvironments is a keyset-honouring environment source: it returns items
// strictly past the display-order/name tuple, up to limit, so a test drives
// real pagination without a datastore.
type fakeEnvironments struct{ items []service.Environment }

func (f fakeEnvironments) ListPage(_ context.Context, _ service.Actor, _ domain.Scope, afterOrder int64, afterName string, limit int) ([]service.Environment, error) {
	var out []service.Environment
	for _, e := range f.items {
		if e.DisplayOrder > afterOrder || (e.DisplayOrder == afterOrder && e.Name > afterName) {
			out = append(out, e)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

type fakeConfiguration struct{ cells []service.ValueCell }

func (f fakeConfiguration) ListPage(_ context.Context, _ service.Actor, _ domain.Scope, afterName string, limit int) ([]service.ValueCell, error) {
	var out []service.ValueCell
	for _, c := range f.cells {
		if c.Name > afterName {
			out = append(out, c)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// Unused-in-test stubs so ProductionServices is complete.
type fakeDefinitions struct{}

func (fakeDefinitions) ListPage(context.Context, service.Actor, domain.Scope, string, int) ([]service.Key, int64, error) {
	return nil, 0, nil
}

type fakeRevisions struct{}

func (fakeRevisions) PendingDraftsPage(context.Context, service.Actor, domain.Scope, string, int) ([]service.PendingDraft, error) {
	return nil, nil
}
func (fakeRevisions) HistoryPage(context.Context, service.Actor, domain.Scope, int64, int) ([]service.RevisionView, error) {
	return nil, nil
}

type fakeAdmission struct {
	err        error
	releaseErr error
}

func (f fakeAdmission) Acquire(context.Context, service.Actor, authz.Operation, domain.Scope) (func() error, error) {
	if f.err != nil {
		return nil, f.err
	}
	return func() error { return f.releaseErr }, nil
}

func toolHandler(t *testing.T, services ProductionServices) http.Handler {
	t.Helper()
	registry := NewRegistry()
	if err := RegisterProductionTools(registry, services); err != nil {
		t.Fatal(err)
	}
	return testHandler(t, registry)
}

func envServices(items ...service.Environment) ProductionServices {
	return ProductionServices{
		Admission:   fakeAdmission{},
		Definitions: fakeDefinitions{}, Environments: fakeEnvironments{items: items},
		Configuration: fakeConfiguration{}, Pending: fakeRevisions{}, Revisions: fakeRevisions{},
	}
}

// callTool posts one tools/call and returns the decoded JSON-RPC response.
func callTool(t *testing.T, h http.Handler, name, arguments string) map[string]any {
	t.Helper()
	req := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", name, modernBody(1, "tools/call", name, arguments))
	req.Header.Set("Authorization", "Bearer service-account-token")
	rec := serve(t, h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/call %s = %d %q", name, rec.Code, rec.Body.String())
	}
	return decodeResponse(t, rec)
}

func structuredContent(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result: %v", response)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("result has no structuredContent: %v", result)
	}
	return structured
}

func env(name string) service.Environment {
	return service.Environment{ID: "env_" + name, Name: name, Note: "n-" + name, DisplayOrder: 1}
}

func TestProductionCatalogListsExactlyTheFiveTools(t *testing.T) {
	h := toolHandler(t, envServices())
	response := decodeResponse(t, serve(t, h, request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/list", "", modernBody(1, "tools/list", "", ""))))
	tools := response["result"].(map[string]any)["tools"].([]any)
	var names []string
	for _, tool := range tools {
		entry := tool.(map[string]any)
		names = append(names, entry["name"].(string))
		description, _ := entry["description"].(string)
		for _, required := range []string{"Read-only. Requires read@", "publishes nothing", "requires no user interaction"} {
			if !strings.Contains(description, required) {
				t.Fatalf("%s description missing %q: %q", entry["name"], required, description)
			}
		}
	}
	want := map[string]bool{
		ToolListDefinitions: true, ToolListEnvironments: true, ToolInspectConfiguration: true,
		ToolListPendingChanges: true, ToolListRevisions: true,
	}
	if len(names) != len(want) {
		t.Fatalf("catalog = %v", names)
	}
	for _, name := range names {
		if !want[name] {
			t.Fatalf("unexpected tool %q in %v", name, names)
		}
	}
}

func TestEnvironmentToolPagesThroughCursor(t *testing.T) {
	h := toolHandler(t, envServices(env("a"), env("b"), env("c"), env("d"), env("e")))
	var collected []string
	cursor := ""
	for range 10 {
		args := fmt.Sprintf(`{"org_id":"org_a","project_id":"prj_a","page_size":2,"cursor":%q}`, cursor)
		structured := structuredContent(t, callTool(t, h, ToolListEnvironments, args))
		if structured["org_id"] != "org_a" || structured["project_id"] != "prj_a" {
			t.Fatalf("addressed ids not repeated: %v", structured)
		}
		for _, item := range structured["environments"].([]any) {
			collected = append(collected, item.(map[string]any)["name"].(string))
		}
		next, ok := structured["next_cursor"].(string)
		if !ok || next == "" {
			cursor = ""
			break
		}
		cursor = next
	}
	if strings.Join(collected, ",") != "a,b,c,d,e" {
		t.Fatalf("paged environments = %v", collected)
	}
}

func TestEnvironmentToolLastPageOmitsCursor(t *testing.T) {
	h := toolHandler(t, envServices(env("a"), env("b")))
	structured := structuredContent(t, callTool(t, h, ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a","page_size":25}`))
	if _, present := structured["next_cursor"]; present {
		t.Fatalf("final page carried a cursor: %v", structured)
	}
	if len(structured["environments"].([]any)) != 2 {
		t.Fatalf("environments = %v", structured["environments"])
	}
}

func TestCursorTamperIsRejected(t *testing.T) {
	h := toolHandler(t, envServices(env("a"), env("b"), env("c")))
	structured := structuredContent(t, callTool(t, h, ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a","page_size":1}`))
	cursor := structured["next_cursor"].(string)
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	raw[len(raw)-1] ^= 0xFF // flip one MAC byte
	tampered := base64.RawURLEncoding.EncodeToString(raw)
	args := fmt.Sprintf(`{"org_id":"org_a","project_id":"prj_a","page_size":1,"cursor":%q}`, tampered)
	body := bodyString(t, h, ToolListEnvironments, args)
	if !strings.Contains(body, ErrInvalidCursor.Error()) {
		t.Fatalf("tampered cursor response = %q", body)
	}
}

func TestCursorBoundToScope(t *testing.T) {
	h := toolHandler(t, envServices(env("a"), env("b"), env("c")))
	structured := structuredContent(t, callTool(t, h, ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a","page_size":1}`))
	cursor := structured["next_cursor"].(string)
	// Same cursor, different project: authentication succeeds but the scope
	// binding does not match, so it is the one safe invalid-cursor error.
	args := fmt.Sprintf(`{"org_id":"org_a","project_id":"prj_OTHER","page_size":1,"cursor":%q}`, cursor)
	body := bodyString(t, h, ToolListEnvironments, args)
	if !strings.Contains(body, ErrInvalidCursor.Error()) {
		t.Fatalf("cross-scope cursor response = %q", body)
	}
}

func TestCursorBoundToPageSize(t *testing.T) {
	h := toolHandler(t, envServices(env("a"), env("b"), env("c")))
	structured := structuredContent(t, callTool(t, h, ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a","page_size":1}`))
	cursor := structured["next_cursor"].(string)
	body := bodyString(t, h, ToolListEnvironments,
		fmt.Sprintf(`{"org_id":"org_a","project_id":"prj_a","page_size":2,"cursor":%q}`, cursor))
	if !strings.Contains(body, ErrInvalidCursor.Error()) {
		t.Fatalf("changed page size response = %q", body)
	}
}

func TestPageSizeOutOfRangeIsRejected(t *testing.T) {
	h := toolHandler(t, envServices(env("a")))
	for _, size := range []int{101, 0, -1} {
		args := fmt.Sprintf(`{"org_id":"org_a","project_id":"prj_a","page_size":%d}`, size)
		body := bodyString(t, h, ToolListEnvironments, args)
		if !strings.Contains(body, `"isError":true`) {
			t.Fatalf("page_size %d response = %q", size, body)
		}
	}
}

func TestSharedAdmissionLimitUsesUniformHTTP429(t *testing.T) {
	services := envServices(env("a"))
	services.Admission = fakeAdmission{err: fmt.Errorf("shared coordinator: %w", admission.ErrOverloaded)}
	h := toolHandler(t, services)
	req := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", ToolListEnvironments,
		modernBody(1, "tools/call", ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a"}`))
	req.Header.Set("Authorization", "Bearer service-account-token")
	rec := serve(t, h, req)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("shared admission response = %d Retry-After %q body %q", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "shared coordinator") {
		t.Fatalf("rate-limit detail leaked: %q", rec.Body.String())
	}
}

func TestSharedAdmissionReleaseFailureFailsClosed(t *testing.T) {
	services := envServices(env("a"))
	services.Admission = fakeAdmission{releaseErr: errors.New("CANARY-RELEASE-DETAIL")}
	h := toolHandler(t, services)
	body := bodyString(t, h, ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a"}`)
	if !strings.Contains(body, SafeOperationError) || strings.Contains(body, "CANARY-RELEASE-DETAIL") {
		t.Fatalf("release failure response = %q", body)
	}
}

func TestUnknownArgumentFieldIsRejected(t *testing.T) {
	h := toolHandler(t, envServices(env("a")))
	body := bodyString(t, h, ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a","reveal":true}`)
	// The inferred schema forbids additional properties, so an injected field is
	// refused with an error before any service work.
	if !strings.Contains(body, `"isError":true`) || !strings.Contains(body, "additional properties") {
		t.Fatalf("unknown field not rejected as an error: %q", body)
	}
	if strings.Contains(body, `"id":"env_a"`) {
		t.Fatalf("unknown field was accepted: %q", body)
	}
}

func TestInspectDropsSecretPlaintext(t *testing.T) {
	// The service never opens a secret cell, but the mapping is defence in depth:
	// even a cell that arrives carrying secret plaintext must emit none.
	services := ProductionServices{
		Admission:    fakeAdmission{},
		Definitions:  fakeDefinitions{},
		Environments: fakeEnvironments{},
		Configuration: fakeConfiguration{cells: []service.ValueCell{
			{KeyID: "k_cfg", Name: "CONFIG_KEY", Classification: string(schema.Config), Set: true, Value: "cfg-plaintext", Revealed: true},
			{KeyID: "k_sec", Name: "SECRET_KEY", Classification: string(schema.Secret), Set: true, Value: "CANARY-SECRET-PLAINTEXT"},
		}},
		Pending: fakeRevisions{}, Revisions: fakeRevisions{},
	}
	h := toolHandler(t, services)
	response := callTool(t, h, ToolInspectConfiguration, `{"org_id":"org_a","project_id":"prj_a","environment_id":"env_a"}`)
	raw, _ := json.Marshal(response)
	if strings.Contains(string(raw), "CANARY-SECRET-PLAINTEXT") {
		t.Fatalf("secret plaintext leaked into inspect result: %s", raw)
	}
	cells := structuredContent(t, response)["configuration"].([]any)
	for _, cell := range cells {
		c := cell.(map[string]any)
		if c["classification"] == string(schema.Secret) {
			if _, hasValue := c["value"]; hasValue {
				t.Fatalf("secret cell carried a value: %v", c)
			}
		}
		if c["name"] == "CONFIG_KEY" && c["value"] != "cfg-plaintext" {
			t.Fatalf("config plaintext dropped: %v", c)
		}
	}
}

func TestResultItemTooLargeRefuses(t *testing.T) {
	huge := service.Environment{ID: "env_big", Name: "big", Note: strings.Repeat("x", 300<<10)}
	h := toolHandler(t, envServices(huge))
	body := bodyString(t, h, ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a","page_size":25}`)
	if !strings.Contains(body, ErrResultItemTooLarge.Error()) {
		t.Fatalf("oversized single item response = %q", body[:min(len(body), 400)])
	}
}

func TestOversizedItemAfterAFittingItemTruncates(t *testing.T) {
	small := service.Environment{ID: "env_a", Name: "a", Note: "small"}
	huge := service.Environment{ID: "env_b", Name: "b", Note: strings.Repeat("x", 300<<10)}
	h := toolHandler(t, envServices(small, huge))
	structured := structuredContent(t, callTool(t, h, ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a","page_size":25}`))
	if items := structured["environments"].([]any); len(items) != 1 || items[0].(map[string]any)["name"] != "a" {
		t.Fatalf("byte-fit did not truncate to the first item: %v", items)
	}
	if next, _ := structured["next_cursor"].(string); next == "" {
		t.Fatal("byte-truncated page carried no continuation cursor")
	}
}

func TestTraversalLimitReached(t *testing.T) {
	items := make([]service.Environment, 0, 1100)
	for i := range 1100 {
		name := fmt.Sprintf("env-%04d", i)
		items = append(items, service.Environment{ID: "id_" + name, Name: name})
	}
	h := toolHandler(t, envServices(items...))
	cursor := ""
	pages := 0
	for {
		args := fmt.Sprintf(`{"org_id":"org_a","project_id":"prj_a","page_size":100,"cursor":%q}`, cursor)
		body := bodyString(t, h, ToolListEnvironments, args)
		if strings.Contains(body, ErrTraversalLimitReached.Error()) {
			if strings.Contains(body, "next_cursor") {
				t.Fatalf("traversal limit carried a continuation cursor: %q", body[:min(len(body), 300)])
			}
			break
		}
		structured := structuredContent(t, decode(t, body))
		pages++
		next, ok := structured["next_cursor"].(string)
		if !ok || next == "" {
			t.Fatalf("chain ended without reaching the traversal bound after %d pages", pages)
		}
		cursor = next
		if pages > 12 {
			t.Fatal("chain never hit the 10-page bound")
		}
	}
	if pages != MaxChainPages-1 {
		t.Fatalf("returned %d pages before the bound, want %d", pages, MaxChainPages-1)
	}
}

func TestFinalPageMayReachTraversalPageBound(t *testing.T) {
	items := make([]service.Environment, 0, MaxChainPages)
	for i := range MaxChainPages {
		name := fmt.Sprintf("env-%04d", i)
		items = append(items, service.Environment{ID: "id_" + name, Name: name})
	}
	h := toolHandler(t, envServices(items...))
	cursor := ""
	for page := range MaxChainPages {
		body := bodyString(t, h, ToolListEnvironments,
			fmt.Sprintf(`{"org_id":"org_a","project_id":"prj_a","page_size":1,"cursor":%q}`, cursor))
		if strings.Contains(body, ErrTraversalLimitReached.Error()) {
			t.Fatalf("final page %d refused at exact bound: %q", page+1, body[:min(len(body), 300)])
		}
		structured := structuredContent(t, decode(t, body))
		next, _ := structured["next_cursor"].(string)
		if page == MaxChainPages-1 {
			if next != "" {
				t.Fatalf("final bounded page carried cursor: %q", next)
			}
			continue
		}
		if next == "" {
			t.Fatalf("page %d ended early", page+1)
		}
		cursor = next
	}
}

func TestTraversalByteLimitCountsExactStructuredContent(t *testing.T) {
	items := make([]service.Environment, 0, 6)
	for i := range 6 {
		name := fmt.Sprintf("env-%d", i)
		items = append(items, service.Environment{ID: "id_" + name, Name: name, Note: strings.Repeat("x", 220<<10)})
	}
	h := toolHandler(t, envServices(items...))
	cursor := ""
	cumulative := 0
	pages := 0
	for {
		body := bodyString(t, h, ToolListEnvironments,
			fmt.Sprintf(`{"org_id":"org_a","project_id":"prj_a","page_size":1,"cursor":%q}`, cursor))
		if strings.Contains(body, ErrTraversalLimitReached.Error()) {
			if strings.Contains(body, "next_cursor") {
				t.Fatalf("byte limit error carried cursor: %q", body[:min(len(body), 400)])
			}
			break
		}
		structured := structuredContent(t, decode(t, body))
		encoded, err := json.Marshal(structured)
		if err != nil {
			t.Fatal(err)
		}
		cumulative += len(encoded)
		pages++
		cursor = structured["next_cursor"].(string)
		state, err := decodeCursor(t.Context(), testCursorSealer, CursorScope{
			Tool: ToolListEnvironments, Org: "org_a", Project: "prj_a", PageSize: 1,
		}, cursor, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if state.Bytes != cumulative {
			t.Fatalf("cursor bytes = %d, exact structuredContent = %d", state.Bytes, cumulative)
		}
	}
	if pages != 4 {
		t.Fatalf("returned %d large pages before 1 MiB bound, want 4", pages)
	}
}

func TestProductionRegistryRowsArePinned(t *testing.T) {
	registry := NewRegistry()
	if err := RegisterProductionTools(registry, envServices()); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		serviceOp, authzOp string
		formula            []string
	}{
		ToolListDefinitions:      {"service.Keys.List", "key.list", []string{"read@project"}},
		ToolListEnvironments:     {"service.Environments.List", "environment.list", []string{"read@project"}},
		ToolInspectConfiguration: {"service.Values.List", "value.list", []string{"read@environment"}},
		ToolListPendingChanges:   {"service.Revisions.PendingDrafts", "value.pending-list", []string{"read@environment"}},
		ToolListRevisions:        {"service.Revisions.History", "revision.list", []string{"read@environment"}},
	}
	rows := registry.Rows()
	if len(rows) != len(want) {
		t.Fatalf("registry has %d rows, want %d", len(rows), len(want))
	}
	for _, row := range rows {
		expected, ok := want[row.Name]
		if !ok {
			t.Fatalf("unexpected tool %q", row.Name)
		}
		if row.ServiceOperation != expected.serviceOp || row.AuthorizationOperation != expected.authzOp {
			t.Fatalf("%s ops = %q / %q", row.Name, row.ServiceOperation, row.AuthorizationOperation)
		}
		if strings.Join(row.Formula, "+") != strings.Join(expected.formula, "+") {
			t.Fatalf("%s formula = %v", row.Name, row.Formula)
		}
		if len(row.Artifacts) != 1 || row.Artifacts[0] != "machine-credential" {
			t.Fatalf("%s artifacts = %v", row.Name, row.Artifacts)
		}
		if !row.ReadOnly || row.AuditDisposition != AuditDispositionNone || row.SecretPolicy != SecretPolicyNoSecretMaterial {
			t.Fatalf("%s policy = readOnly %v audit %q secret %q", row.Name, row.ReadOnly, row.AuditDisposition, row.SecretPolicy)
		}
		schema := string(row.InputSchema)
		if !strings.Contains(schema, `"additionalProperties":false`) {
			t.Fatalf("%s input schema allows unknown fields: %s", row.Name, schema)
		}
		for _, id := range []string{"org_id", "project_id"} {
			if !strings.Contains(schema, `"`+id+`"`) {
				t.Fatalf("%s input schema missing %s: %s", row.Name, id, schema)
			}
		}
		for _, constraint := range []string{`"minimum":1`, `"maximum":100`, `"maxLength":2048`} {
			if !strings.Contains(schema, constraint) {
				t.Fatalf("%s input schema missing %s: %s", row.Name, constraint, schema)
			}
		}
	}
}

func decode(t *testing.T, body string) map[string]any {
	t.Helper()
	var response map[string]any
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return response
}

func bodyString(t *testing.T, h http.Handler, name, arguments string) string {
	t.Helper()
	req := request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/call", name, modernBody(1, "tools/call", name, arguments))
	req.Header.Set("Authorization", "Bearer service-account-token")
	return serve(t, h, req).Body.String()
}
