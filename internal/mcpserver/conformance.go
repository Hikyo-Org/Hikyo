package mcpserver

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterConformanceDiagnostics adds only the three upstream SEP-2575 probe
// tools. It is for scripts/ci/mcp-conformance-server and must never be called
// by production wiring. Keeping the fixture on New still exercises Hikyo's
// actual host, origin, metadata, header, statelessness, and status adapters.
func RegisterConformanceDiagnostics(registry *Registry) error {
	if registry == nil {
		return errors.New("mcpserver: nil conformance registry")
	}
	diagnostics := []struct {
		name        string
		description string
		handler     mcp.ToolHandlerFor[any, any]
	}{
		{
			name:        "test_missing_capability",
			description: "Requires the sampling capability for the SEP-2575 conformance probe.",
			handler:     missingCapabilityDiagnostic,
		},
		{
			name:        "test_streaming_elicitation",
			description: "Returns one result frame for the SEP-2575 response-stream conformance probe.",
			handler:     resultOnlyDiagnostic,
		},
		{
			name:        "test_logging_tool",
			description: "Returns without logging for the SEP-2575 log-level conformance probe.",
			handler:     resultOnlyDiagnostic,
		},
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.frozen {
		return errors.New("mcpserver: registry is frozen")
	}
	for _, diagnostic := range diagnostics {
		if _, exists := registry.registrations[diagnostic.name]; exists {
			return errors.New("mcpserver: duplicate conformance diagnostic")
		}
		diagnostic := diagnostic
		registry.registrations[diagnostic.name] = registration{
			row: RegistryRow{Name: diagnostic.name},
			install: func(server *mcp.Server) {
				mcp.AddTool(server, &mcp.Tool{
					Name: diagnostic.name, Description: diagnostic.description,
					InputSchema: map[string]any{"type": "object", "additionalProperties": false},
				}, diagnostic.handler)
			},
		}
	}
	return nil
}

func missingCapabilityDiagnostic(_ context.Context, request *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	capabilities := request.ClientCapabilities()
	if capabilities != nil && capabilities.Sampling != nil {
		return diagnosticResult(), nil, nil
	}
	data, err := json.Marshal(mcp.MissingRequiredClientCapabilityData{
		RequiredCapabilities: &mcp.ClientCapabilities{Sampling: &mcp.SamplingCapabilities{}},
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, nil, &jsonrpc.Error{
		Code: mcp.CodeMissingRequiredClientCapabilities, Message: "sampling capability required", Data: data,
	}
}

func resultOnlyDiagnostic(context.Context, *mcp.CallToolRequest, any) (*mcp.CallToolResult, any, error) {
	return diagnosticResult(), nil, nil
}

func diagnosticResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "conformance diagnostic completed"}}}
}
