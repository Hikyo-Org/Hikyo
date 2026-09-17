package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// fakeValues records the last stage or validate call and answers with a fixed
// verdict, so the transport mapping is tested without a datastore.
type fakeValues struct {
	stageErr    error
	validateErr error
	lastOp      string
	lastKey     string
	lastValue   string
	lastAcks    []string
	problems    []string
}

func (f *fakeValues) Set(_ context.Context, _ service.Actor, _ domain.Scope, keyName, value string, acks []string) (service.StagedChange, error) {
	f.lastOp, f.lastKey, f.lastValue, f.lastAcks = "set", keyName, value, acks
	if f.stageErr != nil {
		return service.StagedChange{}, f.stageErr
	}
	return service.StagedChange{
		VersionID: "pcv_1", KeyID: "key_1", Name: keyName, Classification: "config", Operation: "set",
		StagedFromRevision: 7, CreatedAt: time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC),
		Findings: []service.Finding{{RuleID: "aws-access-key", Surface: "value_write", Locator: "key_1", Acknowledgement: "ack-token"}},
	}, nil
}

func (f *fakeValues) Unset(_ context.Context, _ service.Actor, _ domain.Scope, keyName string) (service.StagedChange, error) {
	f.lastOp, f.lastKey, f.lastValue, f.lastAcks = "unset", keyName, "", nil
	if f.stageErr != nil {
		return service.StagedChange{}, f.stageErr
	}
	return service.StagedChange{VersionID: "pcv_2", KeyID: "key_1", Name: keyName, Classification: "secret", Operation: "unset", CreatedAt: time.Unix(0, 0).UTC()}, nil
}

func (f *fakeValues) ValidateSet(_ context.Context, _ service.Actor, _ domain.Scope, keyName, value string) (service.ValidatedChange, error) {
	f.lastOp, f.lastKey, f.lastValue = "validate-set", keyName, value
	if f.validateErr != nil {
		return service.ValidatedChange{}, f.validateErr
	}
	return service.ValidatedChange{
		KeyID: "key_1", Name: keyName, Classification: "config", Operation: "set",
		Valid: len(f.problems) == 0, Problems: f.problems,
		Findings: []service.Finding{{RuleID: "aws-access-key", Surface: "validate", Locator: "key_1"}},
	}, nil
}

func (f *fakeValues) ValidateUnset(_ context.Context, _ service.Actor, _ domain.Scope, keyName string) (service.ValidatedChange, error) {
	f.lastOp, f.lastKey, f.lastValue = "validate-unset", keyName, ""
	if f.validateErr != nil {
		return service.ValidatedChange{}, f.validateErr
	}
	return service.ValidatedChange{KeyID: "key_1", Name: keyName, Classification: "secret", Operation: "unset", Valid: true}, nil
}

type safeDetailError struct{ detail string }

func (e safeDetailError) Error() string      { return "service: " + e.detail }
func (e safeDetailError) SafeDetail() string { return e.detail }

func writeHandler(t *testing.T, values *fakeValues, admission AdmissionService) http.Handler {
	t.Helper()
	registry := NewRegistry()
	if err := RegisterProductionTools(registry, envServices()); err != nil {
		t.Fatal(err)
	}
	if err := RegisterWriteTools(registry, WriteServices{Admission: admission, Staging: values, Validation: values}); err != nil {
		t.Fatal(err)
	}
	return testHandler(t, registry)
}

const stageArgs = `{"org_id":"org_a","project_id":"prj_a","environment_id":"env_a","key_name":"DB_URL","operation":"set","value":"postgres://db"}`

func TestWriteToolsAreUnknownUntilInstalled(t *testing.T) {
	readOnly := toolHandler(t, envServices())
	for _, name := range WriteToolNames() {
		body := bodyString(t, readOnly, name, stageArgs)
		if !strings.Contains(body, "error") || strings.Contains(body, SafeOperationError) {
			t.Fatalf("uninstalled %s answered as a registered tool: %s", name, body)
		}
	}
	response := decodeResponse(t, serve(t, readOnly, request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/list", "", modernBody(1, "tools/list", "", ""))))
	for _, tool := range response["result"].(map[string]any)["tools"].([]any) {
		if name := tool.(map[string]any)["name"].(string); name == ToolStageChange || name == ToolValidateChange {
			t.Fatalf("read-only registry advertises %s", name)
		}
	}
}

func TestWriteCatalogAdvertisesExactlyTheTwoWriteTools(t *testing.T) {
	h := writeHandler(t, &fakeValues{}, fakeAdmission{})
	response := decodeResponse(t, serve(t, h, request(http.MethodPost, "https://hikyo.example.com/mcp", "tools/list", "", modernBody(1, "tools/list", "", ""))))
	tools := response["result"].(map[string]any)["tools"].([]any)
	want := map[string]bool{}
	for _, name := range AllToolNames() {
		want[name] = true
	}
	if len(tools) != len(want) {
		t.Fatalf("catalog has %d tools, want %d", len(tools), len(want))
	}
	for _, tool := range tools {
		entry := tool.(map[string]any)
		name := entry["name"].(string)
		if !want[name] {
			t.Fatalf("unexpected tool %q", name)
		}
		if name != ToolStageChange && name != ToolValidateChange {
			continue
		}
		description := entry["description"].(string)
		for _, required := range []string{"Requires edit@environment", "ublishes nothing", "no user interaction"} {
			if !strings.Contains(description, required) {
				t.Fatalf("%s description missing %q: %q", name, required, description)
			}
		}
		annotations := entry["annotations"].(map[string]any)
		if annotations["readOnlyHint"] != false || annotations["idempotentHint"] != false || annotations["destructiveHint"] != true || annotations["openWorldHint"] != false {
			t.Fatalf("%s annotations = %v", name, annotations)
		}
		schema := entry["inputSchema"].(map[string]any)
		properties := schema["properties"].(map[string]any)
		operation := properties["operation"].(map[string]any)
		if fmt.Sprint(operation["enum"]) != "[set unset]" {
			t.Fatalf("%s operation schema = %v", name, operation)
		}
		if schema["additionalProperties"] != false {
			t.Fatalf("%s input schema allows unknown fields", name)
		}
		if _, ok := properties["acknowledgements"]; ok != (name == ToolStageChange) {
			t.Fatalf("%s acknowledgements presence = %v", name, ok)
		}
	}
}

func TestWriteRegistryRowsArePinned(t *testing.T) {
	registry := NewRegistry()
	if err := RegisterWriteTools(registry, WriteServices{Admission: fakeAdmission{}, Staging: &fakeValues{}, Validation: &fakeValues{}}); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{ serviceOp, authzOp string }{
		ToolStageChange:    {"service.Values.Set", "value.stage"},
		ToolValidateChange: {"service.Values.Validate", "value.validate"},
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
		if strings.Join(row.Formula, "+") != "edit@environment" {
			t.Fatalf("%s formula = %v", row.Name, row.Formula)
		}
		if len(row.Artifacts) != 1 || row.Artifacts[0] != "machine-credential" {
			t.Fatalf("%s artifacts = %v", row.Name, row.Artifacts)
		}
		// A write-surface tool is never audited:none, and ReadOnly is derived:
		// both operations insert their audit event, so neither is read-only.
		if row.Class != ToolClassWriteSurface || row.ReadOnly || row.AuditDisposition != AuditDispositionEvents || row.SecretPolicy != SecretPolicyNoSecretMaterial {
			t.Fatalf("%s policy = class %q readOnly %v audit %q secret %q", row.Name, row.Class, row.ReadOnly, row.AuditDisposition, row.SecretPolicy)
		}
		for _, id := range []string{"org_id", "project_id", "environment_id", "key_name", "operation"} {
			if !strings.Contains(string(row.InputSchema), `"`+id+`"`) {
				t.Fatalf("%s input schema missing %s", row.Name, id)
			}
		}
	}
}

func TestWriteToolsRequireCompleteServices(t *testing.T) {
	if err := RegisterWriteTools(NewRegistry(), WriteServices{Admission: fakeAdmission{}, Staging: &fakeValues{}}); err == nil {
		t.Fatal("incomplete write services accepted")
	}
}

func TestStageChangeMapsSetAndUnset(t *testing.T) {
	values := &fakeValues{}
	h := writeHandler(t, values, fakeAdmission{})
	out := structuredContent(t, callTool(t, h, ToolStageChange,
		`{"org_id":"org_a","project_id":"prj_a","environment_id":"env_a","key_name":"DB_URL","operation":"set","value":"postgres://db","acknowledgements":["ack-1"]}`))
	if values.lastOp != "set" || values.lastKey != "DB_URL" || values.lastValue != "postgres://db" || len(values.lastAcks) != 1 {
		t.Fatalf("service call = %+v", values)
	}
	if out["version_id"] != "pcv_1" || out["operation"] != "set" || out["staged_from_revision"] != float64(7) || out["created_at"] != "2026-09-17T08:00:00Z" {
		t.Fatalf("stage output = %v", out)
	}
	if _, echoed := out["value"]; echoed {
		t.Fatal("stage output echoes the proposed value")
	}
	findings := out["findings"].([]any)
	if len(findings) != 1 || findings[0].(map[string]any)["acknowledgement"] != "ack-token" {
		t.Fatalf("findings = %v", findings)
	}

	out = structuredContent(t, callTool(t, h, ToolStageChange,
		`{"org_id":"org_a","project_id":"prj_a","environment_id":"env_a","key_name":"DB_URL","operation":"unset"}`))
	if values.lastOp != "unset" || out["version_id"] != "pcv_2" || out["operation"] != "unset" {
		t.Fatalf("unset = %+v / %v", values, out)
	}
	if findings := out["findings"].([]any); len(findings) != 0 {
		t.Fatalf("unset findings = %v", findings)
	}
}

func TestValidateChangeReportsProblemsWithoutStaging(t *testing.T) {
	values := &fakeValues{problems: []string{`value for "DB_URL" is invalid (type: expected string)`}}
	h := writeHandler(t, values, fakeAdmission{})
	out := structuredContent(t, callTool(t, h, ToolValidateChange, stageArgs))
	if values.lastOp != "validate-set" || values.lastValue != "postgres://db" {
		t.Fatalf("service call = %+v", values)
	}
	if out["valid"] != false || len(out["problems"].([]any)) != 1 || len(out["findings"].([]any)) != 1 {
		t.Fatalf("validate output = %v", out)
	}
	values.problems = nil
	out = structuredContent(t, callTool(t, h, ToolValidateChange,
		`{"org_id":"org_a","project_id":"prj_a","environment_id":"env_a","key_name":"DB_URL","operation":"unset"}`))
	if values.lastOp != "validate-unset" || out["valid"] != true || len(out["problems"].([]any)) != 0 {
		t.Fatalf("validate unset = %+v / %v", values, out)
	}
}

func TestWriteToolArgumentsAreClosedAndBounded(t *testing.T) {
	values := &fakeValues{}
	h := writeHandler(t, values, fakeAdmission{})
	for name, args := range map[string]string{
		"unknown operation": `{"org_id":"o","project_id":"p","environment_id":"e","key_name":"K","operation":"publish"}`,
		"unset with value":  `{"org_id":"o","project_id":"p","environment_id":"e","key_name":"K","operation":"unset","value":"x"}`,
		"missing key name":  `{"org_id":"o","project_id":"p","environment_id":"e","operation":"set","value":"x"}`,
		"unknown field":     `{"org_id":"o","project_id":"p","environment_id":"e","key_name":"K","operation":"set","value":"x","publish":true}`,
		"oversized value":   `{"org_id":"o","project_id":"p","environment_id":"e","key_name":"K","operation":"set","value":"` + strings.Repeat("v", 70000) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			values.lastOp = ""
			body := bodyString(t, h, ToolStageChange, args)
			if !strings.Contains(body, "error") && !strings.Contains(body, `"isError":true`) {
				t.Fatalf("accepted: %s", body)
			}
			if values.lastOp != "" {
				t.Fatalf("service reached with %s", name)
			}
		})
	}
}

func TestWriteToolErrorsCollapseExceptSafeDetail(t *testing.T) {
	values := &fakeValues{stageErr: fmt.Errorf("%w: no key declared", domain.ErrNotFound)}
	h := writeHandler(t, values, fakeAdmission{})
	if body := bodyString(t, h, ToolStageChange, stageArgs); !strings.Contains(body, SafeOperationError) || strings.Contains(body, "no key declared") {
		t.Fatalf("not-found did not collapse to the safe error: %s", body)
	}
	values.stageErr = domain.ErrUnauthorized
	if body := bodyString(t, h, ToolStageChange, stageArgs); !strings.Contains(body, SafeOperationError) {
		t.Fatalf("unauthorized did not collapse to the safe error: %s", body)
	}
	values.stageErr = safeDetailError{detail: "a project holds at most 100 pending changes"}
	if body := bodyString(t, h, ToolStageChange, stageArgs); !strings.Contains(body, "at most 100 pending changes") || strings.Contains(body, "service:") {
		t.Fatalf("safe detail did not cross verbatim: %s", body)
	}
	values.stageErr = nil
	values.lastOp = ""
	if body := bodyString(t, writeHandler(t, values, fakeAdmission{err: errors.New("admission refused")}), ToolStageChange, stageArgs); !strings.Contains(body, SafeOperationError) {
		t.Fatalf("admission failure did not collapse: %s", body)
	}
	if values.lastOp != "" {
		t.Fatal("service reached without admission")
	}
}
