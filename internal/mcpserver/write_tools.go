package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// This file is the write surface (mcp-write ADR, #742). Two tools, each mapped
// 1:1 to one audited authorization operation, installed only when the operator
// sets HIKYO_MCP_WRITE_ENABLED on top of HIKYO_MCP_ENABLED. The enforceable
// boundary is that this surface stages and validates and never publishes or
// delivers: a staged change is an inert pending draft until a separate
// `value.publish`, which no MCP tool exposes.

// Write tool names are stable MCP surface once released.
const (
	ToolStageChange    = "hikyo_stage_change"
	ToolValidateChange = "hikyo_validate_change"
)

type writeToolRegistration struct {
	name     string
	register func(*Registry, WriteServices) error
}

// writeToolCatalog pairs each write tool with its registration, as the read
// catalog does, so an installed tool never falls into the "other" label.
var writeToolCatalog = [...]writeToolRegistration{
	{ToolStageChange, registerStageChange},
	{ToolValidateChange, registerValidateChange},
}

// WriteToolNames returns the closed write-surface catalog. Their metric labels
// are pinned whether or not the operator installs the tools, so the label set
// never depends on a flag.
func WriteToolNames() []string {
	names := make([]string, 0, len(writeToolCatalog))
	for _, tool := range writeToolCatalog {
		names = append(names, tool.name)
	}
	return names
}

// AllToolNames is the read catalog followed by the write catalog: the closed
// telemetry label set.
func AllToolNames() []string {
	return append(ProductionToolNames(), WriteToolNames()...)
}

type (
	// StagingService is service.Values' stage ingress.
	StagingService interface {
		Set(ctx context.Context, actor service.Actor, scope domain.Scope, keyName, value string, acks []string) (service.StagedChange, error)
		Unset(ctx context.Context, actor service.Actor, scope domain.Scope, keyName string) (service.StagedChange, error)
	}
	// ValidationService is service.Values' validate operation.
	ValidationService interface {
		ValidateSet(ctx context.Context, actor service.Actor, scope domain.Scope, keyName, value string) (service.ValidatedChange, error)
		ValidateUnset(ctx context.Context, actor service.Actor, scope domain.Scope, keyName string) (service.ValidatedChange, error)
	}
)

// WriteServices are the services the write tools map onto.
type WriteServices struct {
	Admission  AdmissionService
	Staging    StagingService
	Validation ValidationService
}

// Write tool input schemas. `operation` is a closed enum and `acknowledgements`
// carries the service's finding cap. `value` deliberately carries NO schema
// constraint, not even a type: the SDK's schema validator echoes the offending
// instance in its error, so a maxLength or a type check here would put a
// proposal on the wire. A non-string value fails Go decoding, which reports
// only the JSON kind; the request-size bound and the service's byte budget
// bound the length; a service refusal collapses to the safe error.
//
// valueInput records presence separately from content (as pageSizeInput does):
// a set MUST carry a JSON string, and an omitted or null value is a missing
// proposal, never an intentional empty-string draft that would replace the
// caller's existing one. An explicit "" stays a legitimate empty proposal.
type (
	changeOperationInput string
	valueInput           struct {
		value string
		set   bool
	}
	acknowledgementsInput []string

	changeInput struct {
		OrgID         string               `json:"org_id" jsonschema:"required,the organization immutable id"`
		ProjectID     string               `json:"project_id" jsonschema:"required,the project immutable id"`
		EnvironmentID string               `json:"environment_id" jsonschema:"required,the environment immutable id"`
		KeyName       string               `json:"key_name" jsonschema:"required,the declared key name"`
		Operation     changeOperationInput `json:"operation" jsonschema:"required,set proposes a value; unset proposes a clear"`
		Value         valueInput           `json:"value,omitempty" jsonschema:"the proposed plaintext for set; omitted for unset"`
	}
	stageInput struct {
		changeInput
		Acknowledgements acknowledgementsInput `json:"acknowledgements,omitempty" jsonschema:"keep-as-config acknowledgement tokens returned by an earlier finding"`
	}
)

const (
	changeOperationSet   changeOperationInput = "set"
	changeOperationUnset changeOperationInput = "unset"
)

// errValueNotString and errAcknowledgementsNotStrings are the value-free
// decode refusals: encoding/json quotes the offending instance in its own
// error, and the SDK returns decode errors to the caller, so a proposal that
// is not a string must fail through an error that never carries it.
var (
	errValueNotString            = errors.New("value must be a JSON string")
	errAcknowledgementsNotString = errors.New("acknowledgements must be JSON strings")
	errAcknowledgementsBound     = errors.New("acknowledgements exceed the count or length bound")
)

func (v *valueInput) UnmarshalJSON(data []byte) error {
	// encoding/json hands a JSON null to UnmarshalJSON and a string decode of
	// null is a silent no-op, so null has to be refused by name here.
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errValueNotString
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errValueNotString
	}
	*v = valueInput{value: value, set: true}
	return nil
}

func (a *acknowledgementsInput) UnmarshalJSON(data []byte) error {
	var tokens []string
	if err := json.Unmarshal(data, &tokens); err != nil {
		return errAcknowledgementsNotString
	}
	if len(tokens) > service.MaxRequestFindings {
		return errAcknowledgementsBound
	}
	for _, token := range tokens {
		if len(token) > CursorMaxBytes {
			return errAcknowledgementsBound
		}
	}
	*a = tokens
	return nil
}

func writeInputSchemaOptions() *jsonschema.ForOptions {
	options := inputSchemaOptions()
	options.TypeSchemas[reflect.TypeFor[changeOperationInput]()] = &jsonschema.Schema{
		Type: "string", Enum: []any{string(changeOperationSet), string(changeOperationUnset)},
	}
	options.TypeSchemas[reflect.TypeFor[valueInput]()] = &jsonschema.Schema{}
	// Same rule for the acknowledgement list: the schema declares only the
	// array; item type, length and count are bounded in UnmarshalJSON so a
	// refusal never quotes a token.
	options.TypeSchemas[reflect.TypeFor[acknowledgementsInput]()] = &jsonschema.Schema{Type: "array"}
	return options
}

// Write tool outputs. Neither echoes the proposed value; a finding's
// acknowledgement is a sealed, content-bound token, not material.
type (
	findingElement struct {
		RuleID          string `json:"rule_id"`
		Surface         string `json:"surface"`
		Locator         string `json:"locator"`
		Acknowledgement string `json:"acknowledgement,omitempty"`
	}
	stageOutput struct {
		OrgID              string           `json:"org_id"`
		ProjectID          string           `json:"project_id"`
		EnvironmentID      string           `json:"environment_id"`
		VersionID          string           `json:"version_id"`
		KeyID              string           `json:"key_id"`
		Name               string           `json:"name"`
		Classification     string           `json:"classification"`
		Operation          string           `json:"operation"`
		StagedFromRevision int64            `json:"staged_from_revision"`
		CreatedAt          string           `json:"created_at"`
		Findings           []findingElement `json:"findings"`
	}
	validateOutput struct {
		OrgID              string           `json:"org_id"`
		ProjectID          string           `json:"project_id"`
		EnvironmentID      string           `json:"environment_id"`
		KeyID              string           `json:"key_id"`
		Name               string           `json:"name"`
		Classification     string           `json:"classification"`
		Operation          string           `json:"operation"`
		Valid              bool             `json:"valid"`
		Problems           []string         `json:"problems"`
		ValidationDeferred bool             `json:"validation_deferred"`
		Findings           []findingElement `json:"findings"`
	}
)

// RegisterWriteTools installs the write surface onto a registry that already
// holds the read tools. It returns the first registration error, so a boot
// that cannot pin the closed surface refuses to serve.
func RegisterWriteTools(registry *Registry, services WriteServices) error {
	if services.Admission == nil || services.Staging == nil || services.Validation == nil {
		return errors.New("mcpserver: incomplete write services")
	}
	for _, tool := range writeToolCatalog {
		if err := tool.register(registry, services); err != nil {
			return err
		}
	}
	return nil
}

func writeToolSpec(name, title, description, serviceOperation, authorizationOperation string) (ToolSpec, error) {
	contract, err := operation.NewContract("mcp:"+name, authorizationOperation, []string{"edit@environment"}, []string{operation.ArtifactMachineCredential})
	if err != nil {
		return ToolSpec{}, err
	}
	return ToolSpec{
		Name: name, Title: title, Description: description,
		ServiceOperation: serviceOperation, Contract: contract, Class: ToolClassWriteSurface,
		AuditDisposition: AuditDispositionEvents, SecretPolicy: SecretPolicyNoSecretMaterial,
	}, nil
}

// registerWrite is Register with the write input-schema options.
func registerWrite[In, Out any](registry *Registry, spec ToolSpec, handler func(context.Context, Bearer, In) (Out, error)) error {
	return registerWithOptions(registry, spec, writeInputSchemaOptions(), handler)
}

func (in changeInput) validate() (domain.Scope, error) {
	switch in.Operation {
	case changeOperationSet:
		if !in.Value.set {
			return domain.Scope{}, ErrInvalidArgument
		}
	case changeOperationUnset:
		if in.Value.set {
			return domain.Scope{}, ErrInvalidArgument
		}
	default:
		return domain.Scope{}, ErrInvalidArgument
	}
	if in.KeyName == "" {
		return domain.Scope{}, ErrInvalidArgument
	}
	return domain.Scope{Org: domain.OrgID(in.OrgID), Project: domain.ProjectID(in.ProjectID), Env: domain.EnvID(in.EnvironmentID)}, nil
}

// runChange is the shared handler shape: validate the closed arguments, admit
// under the mapped operation, then dispatch set or unset to the service.
func runChange[Out any](ctx context.Context, bearer Bearer, admission AdmissionService, in changeInput, op authz.Operation,
	set func(context.Context, service.Actor, domain.Scope) (Out, error),
	unset func(context.Context, service.Actor, domain.Scope) (Out, error)) (Out, error) {
	var zero Out
	scope, err := in.validate()
	if err != nil {
		return zero, err
	}
	actor := service.Bearer(bearer.raw())
	var out Out
	err = withAdmission(ctx, admission, actor, op, scope, func() error {
		var err error
		if in.Operation == changeOperationSet {
			out, err = set(ctx, actor, scope)
		} else {
			out, err = unset(ctx, actor, scope)
		}
		return err
	})
	if err != nil {
		return zero, err
	}
	return out, nil
}

func mapFindings(findings []service.Finding) []findingElement {
	out := make([]findingElement, 0, len(findings))
	for _, f := range findings {
		out = append(out, findingElement{RuleID: f.RuleID, Surface: f.Surface, Locator: f.Locator, Acknowledgement: f.Acknowledgement})
	}
	return out
}

func registerStageChange(registry *Registry, services WriteServices) error {
	spec, err := writeToolSpec(ToolStageChange, "Stage a change",
		"Mutating. Requires edit@environment for explicit org_id/project_id/environment_id. Stages one set or unset of a declared key as the caller's own pending draft and returns its version id and any secret-scanner findings. Publishes nothing and delivers nothing: the draft is inert until a separate value.publish, which no MCP tool exposes. Requires no user interaction.",
		"service.Values.Set/Unset", "value.stage")
	if err != nil {
		return err
	}
	return registerWrite(registry, spec, func(ctx context.Context, bearer Bearer, in stageInput) (stageOutput, error) {
		staged, err := runChange(ctx, bearer, services.Admission, in.changeInput, authz.OpValueStage,
			func(ctx context.Context, actor service.Actor, scope domain.Scope) (service.StagedChange, error) {
				return services.Staging.Set(ctx, actor, scope, in.KeyName, in.Value.value, in.Acknowledgements)
			},
			func(ctx context.Context, actor service.Actor, scope domain.Scope) (service.StagedChange, error) {
				return services.Staging.Unset(ctx, actor, scope, in.KeyName)
			})
		if err != nil {
			return stageOutput{}, err
		}
		return stageOutput{
			OrgID: in.OrgID, ProjectID: in.ProjectID, EnvironmentID: in.EnvironmentID,
			VersionID: staged.VersionID, KeyID: staged.KeyID, Name: staged.Name,
			Classification: staged.Classification, Operation: staged.Operation,
			StagedFromRevision: staged.StagedFromRevision, CreatedAt: staged.CreatedAt.UTC().Format(time.RFC3339Nano),
			Findings: mapFindings(staged.Findings),
		}, nil
	})
}

func registerValidateChange(registry *Registry, services WriteServices) error {
	spec, err := writeToolSpec(ToolValidateChange, "Validate a change",
		"Audited, non-mutating. Requires edit@environment for explicit org_id/project_id/environment_id. Evaluates a proposed set or unset of a declared key against the key's schema, its required_in/forbidden_in presence rules, and the secret scanner, reporting the per-key refusals publish would raise; group all-or-none presence is evaluated only at publish. Stages nothing, publishes nothing, and requires no user interaction.",
		"service.Values.ValidateSet/ValidateUnset", "value.validate")
	if err != nil {
		return err
	}
	return registerWrite(registry, spec, func(ctx context.Context, bearer Bearer, in changeInput) (validateOutput, error) {
		verdict, err := runChange(ctx, bearer, services.Admission, in, authz.OpValueValidate,
			func(ctx context.Context, actor service.Actor, scope domain.Scope) (service.ValidatedChange, error) {
				return services.Validation.ValidateSet(ctx, actor, scope, in.KeyName, in.Value.value)
			},
			func(ctx context.Context, actor service.Actor, scope domain.Scope) (service.ValidatedChange, error) {
				return services.Validation.ValidateUnset(ctx, actor, scope, in.KeyName)
			})
		if err != nil {
			return validateOutput{}, err
		}
		problems := verdict.Problems
		if problems == nil {
			problems = []string{}
		}
		return validateOutput{
			OrgID: in.OrgID, ProjectID: in.ProjectID, EnvironmentID: in.EnvironmentID,
			KeyID: verdict.KeyID, Name: verdict.Name, Classification: verdict.Classification,
			Operation: verdict.Operation, Valid: verdict.Valid, Problems: problems,
			ValidationDeferred: verdict.ValidationDeferred, Findings: mapFindings(verdict.Findings),
		}, nil
	})
}
