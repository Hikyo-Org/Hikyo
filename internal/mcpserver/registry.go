package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/operation"
)

const (
	// MaxStructuredContentBytes is the phase-1 encoded tool-result bound.
	MaxStructuredContentBytes = 256 << 10
	// MaxCompatibilityTextBytes is the phase-1 compatibility-summary bound.
	MaxCompatibilityTextBytes = 4 << 10
	// SafeOperationError is the only fallback text emitted for an unexpected
	// service error. Domain-specific safe errors can join this closed policy in
	// the tool ticket; raw errors never cross the transport.
	SafeOperationError = "Hikyo operation refused"
)

// ErrRateLimited lets an authenticated service callback request the transport's
// uniform HTTP 429 response without exposing internal error details.
var ErrRateLimited = errors.New("mcpserver: rate limited")

type AuditDisposition string

const AuditDispositionNone AuditDisposition = "audited:none"

type SecretPolicy string

const SecretPolicyNoSecretMaterial SecretPolicy = "no-secret-material"

const compatibilitySummary = "Hikyo returned structured data."

// Bearer wraps the raw presented service-account artifact. Formatting is
// always redacted; only this adapter package can unwrap it for a service call.
type Bearer struct{ value string }

func newBearer(value string) Bearer { return Bearer{value: value} }
func (b Bearer) raw() string        { return b.value }
func (Bearer) String() string       { return "[REDACTED]" }
func (Bearer) GoString() string     { return "mcpserver.Bearer([REDACTED])" }

// ToolSpec is the security and wire policy for one explicitly registered
// operation. Input and output schemas are inferred once from the generic
// Register types and pinned into the immutable registry row.
type ToolSpec struct {
	Name             string
	Title            string
	Description      string
	ServiceOperation string
	Contract         operation.Contract
	AuditDisposition AuditDisposition
	SecretPolicy     SecretPolicy
}

// RegistryRow is the immutable, reviewable projection used by closure tests.
type RegistryRow struct {
	Name                   string
	InputSchema            json.RawMessage
	OutputSchema           json.RawMessage
	ServiceOperation       string
	AuthorizationOperation string
	Formula                []string
	Artifacts              []string
	AuditDisposition       AuditDisposition
	ReadOnly               bool
	ResultBytes            int
	SecretPolicy           SecretPolicy
}

type registration struct {
	row     RegistryRow
	install func(*mcp.Server)
}

// Registry is a closed tool set. New freezes it before serving.
type Registry struct {
	mu            sync.Mutex
	registrations map[string]registration
	frozen        bool
}

func NewRegistry() *Registry {
	return &Registry{registrations: make(map[string]registration)}
}

var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func inputSchemaOptions() *jsonschema.ForOptions {
	return &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[pageSizeInput](): {
			Type: "integer", Minimum: jsonschema.Ptr(float64(PageSizeMin)), Maximum: jsonschema.Ptr(float64(PageSizeMax)),
		},
		reflect.TypeFor[cursorInput](): {Type: "string", MaxLength: jsonschema.Ptr(CursorMaxBytes)},
	}}
}

// Register adds one typed service-operation mapping to the closed registry.
func Register[In, Out any](registry *Registry, spec ToolSpec, handler func(context.Context, Bearer, In) (Out, error)) error {
	if registry == nil {
		return errors.New("mcpserver: nil registry")
	}
	if handler == nil {
		return errors.New("mcpserver: nil tool handler")
	}
	if !toolNamePattern.MatchString(spec.Name) {
		return fmt.Errorf("mcpserver: invalid tool name %q", spec.Name)
	}
	if spec.Description == "" {
		return fmt.Errorf("mcpserver: tool %q has no description", spec.Name)
	}
	if spec.ServiceOperation == "" {
		return fmt.Errorf("mcpserver: tool %q has no service operation", spec.Name)
	}
	if spec.AuditDisposition != AuditDispositionNone {
		return fmt.Errorf("mcpserver: tool %q has an unsupported audit disposition", spec.Name)
	}
	if spec.SecretPolicy != SecretPolicyNoSecretMaterial {
		return fmt.Errorf("mcpserver: tool %q has an unsupported secret policy", spec.Name)
	}
	if spec.Contract.ID != "mcp:"+spec.Name || spec.Contract.AuthorizationOperation == "" ||
		len(spec.Contract.Formula()) == 0 || len(spec.Contract.Artifacts()) == 0 {
		return fmt.Errorf("mcpserver: tool %q has an incomplete or mismatched operation contract", spec.Name)
	}
	if !spec.Contract.AdmitsArtifact(operation.ArtifactMachineCredential) || len(spec.Contract.Artifacts()) != 1 {
		return fmt.Errorf("mcpserver: tool %q must admit only machine credentials", spec.Name)
	}
	policy, ok := authz.LookupNetworkOperationPolicy(spec.Contract.AuthorizationOperation)
	if !ok || !slices.Equal(policy.Formula, spec.Contract.Formula()) {
		return fmt.Errorf("mcpserver: tool %q authorization formula does not match the registry", spec.Name)
	}
	if !policy.ReadOnly || !policy.AuditedNone {
		return fmt.Errorf("mcpserver: tool %q authorization operation is not an audited-none read", spec.Name)
	}
	inputSchema, err := jsonschema.For[In](inputSchemaOptions())
	if err != nil {
		return fmt.Errorf("mcpserver: tool %q input schema: %w", spec.Name, err)
	}
	outputSchema, err := jsonschema.For[Out](nil)
	if err != nil {
		return fmt.Errorf("mcpserver: tool %q output schema: %w", spec.Name, err)
	}
	inputSchemaJSON, err := json.Marshal(inputSchema)
	if err != nil {
		return fmt.Errorf("mcpserver: tool %q input schema encoding: %w", spec.Name, err)
	}
	outputSchemaJSON, err := json.Marshal(outputSchema)
	if err != nil {
		return fmt.Errorf("mcpserver: tool %q output schema encoding: %w", spec.Name, err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.frozen {
		return errors.New("mcpserver: registry is frozen")
	}
	if _, exists := registry.registrations[spec.Name]; exists {
		return fmt.Errorf("mcpserver: duplicate tool %q", spec.Name)
	}

	row := RegistryRow{
		Name: spec.Name, InputSchema: inputSchemaJSON, OutputSchema: outputSchemaJSON,
		ServiceOperation: spec.ServiceOperation, AuthorizationOperation: spec.Contract.AuthorizationOperation,
		Formula: spec.Contract.Formula(), Artifacts: spec.Contract.Artifacts(),
		AuditDisposition: spec.AuditDisposition, ReadOnly: true,
		ResultBytes: MaxStructuredContentBytes, SecretPolicy: spec.SecretPolicy,
	}
	registry.registrations[spec.Name] = registration{
		row: row,
		install: func(server *mcp.Server) {
			closedWorld := false
			nondestructive := false
			mcp.AddTool(server, &mcp.Tool{
				Name: spec.Name, Title: spec.Title, Description: spec.Description,
				InputSchema: json.RawMessage(inputSchemaJSON), OutputSchema: json.RawMessage(outputSchemaJSON),
				Annotations: &mcp.ToolAnnotations{
					ReadOnlyHint: true, IdempotentHint: true,
					DestructiveHint: &nondestructive, OpenWorldHint: &closedWorld,
				},
			}, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
				ctx = operation.WithContract(ctx, spec.Contract)
				bearer, _ := ctx.Value(bearerContextKey{}).(Bearer)
				output, err := handler(ctx, bearer, input)
				if err != nil {
					var zero Out
					if errors.Is(err, ErrRateLimited) {
						markRateLimited(ctx)
					}
					// Named cursor, bound, and argument errors are tenant-safe,
					// so their exact token crosses the transport; every other
					// failure collapses to one indistinguishable safe error, so
					// an unauthorized read stays indistinguishable from a
					// nonexistent one.
					if msg := publicErrorMessage(err); msg != "" {
						return nil, zero, errors.New(msg)
					}
					return nil, zero, errors.New(SafeOperationError)
				}
				encoded, err := json.Marshal(output)
				if err != nil || len(encoded) > MaxStructuredContentBytes {
					var zero Out
					return nil, zero, errors.New(SafeOperationError)
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: compatibilitySummary}}}, output, nil
			})
		},
	}
	return nil
}

func (r *Registry) freeze() []registration {
	return r.snapshot(true)
}

func (r *Registry) snapshot(freeze bool) []registration {
	if r == nil {
		r = NewRegistry()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if freeze {
		r.frozen = true
	}
	out := make([]registration, 0, len(r.registrations))
	for _, item := range r.registrations {
		out = append(out, item)
	}
	slices.SortFunc(out, func(a, b registration) int { return strings.Compare(a.row.Name, b.row.Name) })
	return out
}

// Rows returns the deterministic policy projection.
func (r *Registry) Rows() []RegistryRow {
	items := r.snapshot(false)
	rows := make([]RegistryRow, 0, len(items))
	for _, item := range items {
		row := item.row
		row.Formula = slices.Clone(row.Formula)
		row.Artifacts = slices.Clone(row.Artifacts)
		row.InputSchema = slices.Clone(row.InputSchema)
		row.OutputSchema = slices.Clone(row.OutputSchema)
		rows = append(rows, row)
	}
	return rows
}
