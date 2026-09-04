package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/operation"
)

func TestRegistryRefusesInvalidDuplicateAndLateRows(t *testing.T) {
	registry := NewRegistry()
	valid := ToolSpec{
		Name: "echo", Description: "Echo one value.", ServiceOperation: "service.Keys.List",
		Contract: testContract(t, "echo"), AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
	}
	handler := func(context.Context, Bearer, echoInput) (echoOutput, error) { return echoOutput{}, nil }
	if err := Register(registry, valid, handler); err != nil {
		t.Fatal(err)
	}
	if err := Register(registry, valid, handler); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate registration error = %v", err)
	}
	if _, err := New(Options{Registry: registry, ExternalOrigin: "https://hikyo.example.com"}); err != nil {
		t.Fatal(err)
	}
	late := valid
	late.Name = "late"
	late.Contract = testContract(t, "late")
	if err := Register(registry, late, handler); err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("late registration error = %v", err)
	}

	humanContract, err := operation.NewContract("mcp:human", "key.list", []string{"read@project"}, []string{operation.ArtifactHumanSession})
	if err != nil {
		t.Fatal(err)
	}
	if err := Register(NewRegistry(), ToolSpec{
		Name: "human", Description: "Invalid human tool.", ServiceOperation: "service.Keys.List",
		Contract: humanContract, AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
	}, handler); err == nil || !strings.Contains(err.Error(), "only machine credentials") {
		t.Fatalf("human registration error = %v", err)
	}

	drifted, err := operation.NewContract("mcp:echo", "key.list", []string{"edit@project"}, []string{operation.ArtifactMachineCredential})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*ToolSpec)
	}{
		{name: "missing service operation", edit: func(spec *ToolSpec) { spec.ServiceOperation = "" }},
		{name: "formula drift", edit: func(spec *ToolSpec) { spec.Contract = drifted }},
		{name: "audit drift", edit: func(spec *ToolSpec) { spec.AuditDisposition = "audited:write" }},
		{name: "secret policy drift", edit: func(spec *ToolSpec) { spec.SecretPolicy = "may-return-secrets" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := valid
			tc.edit(&spec)
			if err := Register(NewRegistry(), spec, handler); err == nil {
				t.Fatal("incomplete or drifted registry row accepted")
			}
		})
	}
}

func TestRegistryRowsAreDeterministicAndImmutable(t *testing.T) {
	registry, _ := testRegistry(t, "zeta", "alpha")
	rows := registry.Rows()
	if len(rows) != 2 || rows[0].Name != "alpha" || rows[1].Name != "zeta" {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0].ServiceOperation != "service.Keys.List" || rows[0].AuthorizationOperation != "key.list" ||
		rows[0].AuditDisposition != AuditDispositionNone || !rows[0].ReadOnly ||
		rows[0].ResultBytes != MaxStructuredContentBytes || rows[0].SecretPolicy != SecretPolicyNoSecretMaterial ||
		!json.Valid(rows[0].InputSchema) || !json.Valid(rows[0].OutputSchema) {
		t.Fatalf("incomplete registry projection: %#v", rows[0])
	}
	rows[0].Formula[0] = "mutated"
	rows[0].Artifacts[0] = operation.ArtifactHumanSession
	rows[0].InputSchema[0] = 'x'
	rows[0].OutputSchema[0] = 'x'
	again := registry.Rows()
	if slices.Contains(again[0].Formula, "mutated") || slices.Contains(again[0].Artifacts, operation.ArtifactHumanSession) ||
		again[0].InputSchema[0] == 'x' || again[0].OutputSchema[0] == 'x' {
		t.Fatalf("registry projection was mutable: %#v", again[0])
	}
}

func TestEncodedToolResultBoundFailsClosed(t *testing.T) {
	registry := NewRegistry()
	if err := Register(registry, ToolSpec{
		Name: "large", Description: "Return an oversized result.", ServiceOperation: "service.Keys.List",
		Contract: testContract(t, "large"), AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
	}, func(context.Context, Bearer, echoInput) (echoOutput, error) {
		return echoOutput{Value: strings.Repeat("x", MaxStructuredContentBytes)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	h := testHandler(t, registry)
	req := request("POST", "https://hikyo.example.com/mcp", "tools/call", "large", modernBody(1, "tools/call", "large", `{"value":"x"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := serve(t, h, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), SafeOperationError) {
		t.Fatalf("oversized result = %d %q", rec.Code, rec.Body.String())
	}
}

func TestAdapterHasNoStoreSQLOrCryptoImports(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Clean(entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"internal/store", "internal/crypto", "api/apigen"} {
			if strings.Contains(string(raw), `"github.com/Hikyo-Org/hikyo/`+forbidden) {
				t.Errorf("%s imports forbidden boundary %s", entry.Name(), forbidden)
			}
		}
	}
}

func TestOfficialSDKIsPinnedExactly(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("build information unavailable")
	}
	for _, dependency := range info.Deps {
		if dependency.Path == "github.com/modelcontextprotocol/go-sdk" {
			if dependency.Version != "v1.7.0" || dependency.Replace != nil {
				t.Fatalf("SDK dependency = %s replacement %#v", dependency.Version, dependency.Replace)
			}
			return
		}
	}
	t.Fatal(errors.New("official MCP SDK dependency missing"))
}
