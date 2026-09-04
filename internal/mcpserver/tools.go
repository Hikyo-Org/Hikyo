package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// This file is the closed phase-1 tool set (#629). Each tool maps to exactly
// one existing read service operation, repeats current authorization by passing
// the raw service-account bearer straight to the service, and emits a
// whitelisted output that carries no secret material. The service interfaces
// are narrow by construction, so the transport never reaches the datastore and
// a test can drive a tool with a fake.

// Tool names are stable MCP surface once released.
const (
	ToolListDefinitions      = "hikyo_list_definitions"
	ToolListEnvironments     = "hikyo_list_environments"
	ToolInspectConfiguration = "hikyo_inspect_configuration"
	ToolListPendingChanges   = "hikyo_list_pending_changes"
	ToolListRevisions        = "hikyo_list_revisions"
)

type productionToolRegistration struct {
	name     string
	register func(*Registry, ProductionServices) error
}

// productionToolCatalog is the single source for both installation and the
// closed telemetry label set. Pairing each name with its registration function
// prevents a newly installed tool from silently falling into the "other"
// metric label.
var productionToolCatalog = [...]productionToolRegistration{
	{ToolListDefinitions, registerListDefinitions},
	{ToolListEnvironments, registerListEnvironments},
	{ToolInspectConfiguration, registerInspectConfiguration},
	{ToolListPendingChanges, registerListPendingChanges},
	{ToolListRevisions, registerListRevisions},
}

// ProductionToolNames returns the closed phase-1 catalog for telemetry and
// interoperability checks without exposing mutable registry state.
func ProductionToolNames() []string {
	names := make([]string, 0, len(productionToolCatalog))
	for _, tool := range productionToolCatalog {
		names = append(names, tool.name)
	}
	return names
}

// Narrow service interfaces. The concrete service structs satisfy them; a test
// double implements them without a datastore.
type (
	// AdmissionService is service.MCPAdmission. It authorizes before claiming
	// shared rate/concurrency capacity; the page service authorizes again.
	AdmissionService interface {
		Acquire(context.Context, service.Actor, authz.Operation, domain.Scope) (func() error, error)
	}
	// DefinitionsService is service.Keys.
	DefinitionsService interface {
		ListPage(ctx context.Context, actor service.Actor, scope domain.Scope, afterName string, limit int) ([]service.Key, int64, error)
	}
	// EnvironmentsService is service.Environments.
	EnvironmentsService interface {
		ListPage(ctx context.Context, actor service.Actor, scope domain.Scope, afterDisplayOrder int64, afterName string, limit int) ([]service.Environment, error)
	}
	// ConfigurationService is service.Values.
	ConfigurationService interface {
		ListPage(ctx context.Context, actor service.Actor, scope domain.Scope, afterName string, limit int) ([]service.ValueCell, error)
	}
	// PendingService is service.Revisions.
	PendingService interface {
		PendingDraftsPage(ctx context.Context, actor service.Actor, scope domain.Scope, afterKeyID string, limit int) ([]service.PendingDraft, error)
	}
	// RevisionsService is service.Revisions.
	RevisionsService interface {
		HistoryPage(ctx context.Context, actor service.Actor, scope domain.Scope, beforeRevision int64, limit int) ([]service.RevisionView, error)
	}
)

// ProductionServices are the five read services the phase-1 tools map onto. The
// wiring layer supplies the concrete service structs.
type ProductionServices struct {
	Admission     AdmissionService
	Definitions   DefinitionsService
	Environments  EnvironmentsService
	Configuration ConfigurationService
	Pending       PendingService
	Revisions     RevisionsService
}

// Tool input schemas. Every schema requires the full immutable id chain for its
// operation and accepts optional page_size and cursor. Unknown fields are
// rejected by the inferred schema's additionalProperties:false.
type (
	pageSizeInput struct {
		value int
		set   bool
	}
	cursorInput string

	projectInput struct {
		OrgID     string        `json:"org_id" jsonschema:"required,the organization immutable id"`
		ProjectID string        `json:"project_id" jsonschema:"required,the project immutable id"`
		PageSize  pageSizeInput `json:"page_size,omitempty" jsonschema:"page size, 1 to 100, default 25"`
		Cursor    cursorInput   `json:"cursor,omitempty" jsonschema:"opaque continuation from a previous page"`
	}
	envInput struct {
		OrgID         string        `json:"org_id" jsonschema:"required,the organization immutable id"`
		ProjectID     string        `json:"project_id" jsonschema:"required,the project immutable id"`
		EnvironmentID string        `json:"environment_id" jsonschema:"required,the environment immutable id"`
		PageSize      pageSizeInput `json:"page_size,omitempty" jsonschema:"page size, 1 to 100, default 25"`
		Cursor        cursorInput   `json:"cursor,omitempty" jsonschema:"opaque continuation from a previous page"`
	}
)

func (p *pageSizeInput) UnmarshalJSON(data []byte) error {
	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	p.value = value
	p.set = true
	return nil
}

// Tool output elements. Each carries only the mcp-server ADR-approved non-secret fields.
type (
	definitionElement struct {
		Name            string               `json:"name"`
		Description     string               `json:"description"`
		Classification  string               `json:"classification"`
		Deprecated      bool                 `json:"deprecated"`
		DeprecationNote string               `json:"deprecation_note,omitempty"`
		GroupID         string               `json:"group_id,omitempty"`
		Declaration     schema.Declaration   `json:"declaration"`
		Presence        schema.PresenceRules `json:"presence"`
	}
	environmentElement struct {
		ID           string    `json:"id"`
		Name         string    `json:"name"`
		Note         string    `json:"note"`
		DisplayOrder int64     `json:"display_order"`
		CreatedAt    time.Time `json:"created_at"`
	}
	configurationElement struct {
		KeyID          string    `json:"key_id"`
		Name           string    `json:"name"`
		Classification string    `json:"classification"`
		Set            bool      `json:"set"`
		Value          string    `json:"value,omitempty"`
		UpdatedAt      time.Time `json:"updated_at,omitzero"`
		UpdatedBy      string    `json:"updated_by,omitempty"`
	}
	pendingElement struct {
		VersionID          string    `json:"version_id"`
		KeyID              string    `json:"key_id"`
		Name               string    `json:"name"`
		Classification     string    `json:"classification"`
		Operation          string    `json:"operation"`
		StagedFromRevision int64     `json:"staged_from_revision"`
		CreatedAt          time.Time `json:"created_at"`
		Value              string    `json:"value,omitempty"`
	}
	changedKeyElement struct {
		Name   string `json:"name"`
		Change string `json:"change"`
	}
	revisionElement struct {
		Revision        int64               `json:"revision"`
		SchemaRevision  int64               `json:"schema_revision"`
		PublishedBy     string              `json:"published_by"`
		PublishedAt     time.Time           `json:"published_at"`
		ChangedKeys     []changedKeyElement `json:"changed_keys"`
		PayloadPresent  bool                `json:"payload_present"`
		CollectedPolicy string              `json:"collected_policy,omitempty"`
	}
)

// Tool outputs repeat the addressed ids and expose next_cursor only when
// another page exists.
type (
	definitionsOutput struct {
		OrgID          string              `json:"org_id"`
		ProjectID      string              `json:"project_id"`
		SchemaRevision int64               `json:"schema_revision"`
		Definitions    []definitionElement `json:"definitions"`
		NextCursor     string              `json:"next_cursor,omitempty"`
	}
	environmentsOutput struct {
		OrgID        string               `json:"org_id"`
		ProjectID    string               `json:"project_id"`
		Environments []environmentElement `json:"environments"`
		NextCursor   string               `json:"next_cursor,omitempty"`
	}
	configurationOutput struct {
		OrgID         string                 `json:"org_id"`
		ProjectID     string                 `json:"project_id"`
		EnvironmentID string                 `json:"environment_id"`
		Configuration []configurationElement `json:"configuration"`
		NextCursor    string                 `json:"next_cursor,omitempty"`
	}
	pendingOutput struct {
		OrgID          string           `json:"org_id"`
		ProjectID      string           `json:"project_id"`
		EnvironmentID  string           `json:"environment_id"`
		PendingChanges []pendingElement `json:"pending_changes"`
		NextCursor     string           `json:"next_cursor,omitempty"`
	}
	revisionsOutput struct {
		OrgID         string            `json:"org_id"`
		ProjectID     string            `json:"project_id"`
		EnvironmentID string            `json:"environment_id"`
		Revisions     []revisionElement `json:"revisions"`
		NextCursor    string            `json:"next_cursor,omitempty"`
	}
)

// normalizePageSize applies the default and enforces the range. JSON Schema
// rejects invalid wire input; this check keeps direct typed calls fail-closed.
func normalizePageSize(input pageSizeInput) (int, error) {
	if !input.set {
		return PageSizeDefault, nil
	}
	n := input.value
	if n < PageSizeMin || n > PageSizeMax {
		return 0, ErrInvalidArgument
	}
	return n, nil
}

// RegisterProductionTools registers the five phase-1 read tools onto a fresh
// registry. It returns the first registration error, so a boot that cannot pin
// the closed surface refuses to serve.
func RegisterProductionTools(registry *Registry, services ProductionServices) error {
	if services.Admission == nil || services.Definitions == nil || services.Environments == nil ||
		services.Configuration == nil || services.Pending == nil || services.Revisions == nil {
		return errors.New("mcpserver: incomplete production services")
	}
	for _, tool := range productionToolCatalog {
		if err := tool.register(registry, services); err != nil {
			return err
		}
	}
	return nil
}

func withAdmission(ctx context.Context, svc AdmissionService, actor service.Actor, op authz.Operation, scope domain.Scope, call func() error) (err error) {
	release, err := svc.Acquire(ctx, actor, op, scope)
	if err != nil {
		if errors.Is(err, admission.ErrOverloaded) {
			return ErrRateLimited
		}
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	return call()
}

func toolSpec(name, title, description, serviceOperation, authorizationOperation string, formula []string) (ToolSpec, error) {
	contract, err := operation.NewContract("mcp:"+name, authorizationOperation, formula, []string{operation.ArtifactMachineCredential})
	if err != nil {
		return ToolSpec{}, err
	}
	return ToolSpec{
		Name: name, Title: title, Description: description,
		ServiceOperation: serviceOperation, Contract: contract,
		AuditDisposition: AuditDispositionNone, SecretPolicy: SecretPolicyNoSecretMaterial,
	}, nil
}

type toolPageSpec[Row, Elem, Out any] struct {
	pageSize    pageSizeInput
	cursor      cursorInput
	operation   authz.Operation
	scope       domain.Scope
	cursorScope CursorScope
	fetch       func(context.Context, service.Actor, string, int) ([]Row, error)
	mapRow      func(Row) (Elem, error)
	key         func(Row) string
	output      func([]Elem, string) Out
}

func executeToolPage[Row, Elem, Out any](ctx context.Context, bearer Bearer, admissionService AdmissionService, spec toolPageSpec[Row, Elem, Out]) (Out, error) {
	var zero Out
	pageSize, err := normalizePageSize(spec.pageSize)
	if err != nil {
		return zero, err
	}
	actor := service.Bearer(bearer.raw())
	var out Out
	err = withAdmission(ctx, admissionService, actor, spec.operation, spec.scope, func() error {
		elements, next, err := Paginate(ctx, PageSpec[Row, Elem]{
			Scope: spec.cursorScope, Cursor: string(spec.cursor), PageSize: pageSize,
			Fetch: func(ctx context.Context, after string, limit int) ([]Row, error) {
				return spec.fetch(ctx, actor, after, limit)
			},
			Map: spec.mapRow, Key: spec.key,
			StructuredSize: func(items []Elem, next string) (int, error) {
				return encodedSize(spec.output(items, next))
			},
		})
		if err != nil {
			return err
		}
		out = spec.output(elements, next)
		return nil
	})
	return out, err
}

func registerListDefinitions(registry *Registry, services ProductionServices) error {
	spec, err := toolSpec(ToolListDefinitions, "List key definitions",
		"Read-only. Requires read@project for explicit org_id/project_id. Lists key declarations, descriptions, classifications, rules, presence policy, and group ids, with no values. Creates no draft, publishes nothing, and requires no user interaction.",
		"service.Keys.List", "key.list", []string{"read@project"})
	if err != nil {
		return err
	}
	return Register(registry, spec, func(ctx context.Context, bearer Bearer, in projectInput) (definitionsOutput, error) {
		scope := domain.Scope{Org: domain.OrgID(in.OrgID), Project: domain.ProjectID(in.ProjectID)}
		var revision int64
		return executeToolPage(ctx, bearer, services.Admission, toolPageSpec[service.Key, definitionElement, definitionsOutput]{
			pageSize: in.PageSize, cursor: in.Cursor, operation: authz.OpKeyList, scope: scope,
			cursorScope: CursorScope{Tool: ToolListDefinitions, Org: in.OrgID, Project: in.ProjectID},
			fetch: func(ctx context.Context, actor service.Actor, after string, limit int) ([]service.Key, error) {
				keys, rev, err := services.Definitions.ListPage(ctx, actor, scope, after, limit)
				revision = rev
				return keys, err
			},
			mapRow: mapDefinition, key: func(k service.Key) string { return k.Name },
			output: func(items []definitionElement, next string) definitionsOutput {
				return definitionsOutput{OrgID: in.OrgID, ProjectID: in.ProjectID, SchemaRevision: revision, Definitions: items, NextCursor: next}
			},
		})
	})
}

func registerListEnvironments(registry *Registry, services ProductionServices) error {
	spec, err := toolSpec(ToolListEnvironments, "List environments",
		"Read-only. Requires read@project for explicit org_id/project_id. Lists environments with identity, name, note, order, and creation metadata. Creates no draft, publishes nothing, and requires no user interaction.",
		"service.Environments.List", "environment.list", []string{"read@project"})
	if err != nil {
		return err
	}
	return Register(registry, spec, func(ctx context.Context, bearer Bearer, in projectInput) (environmentsOutput, error) {
		scope := domain.Scope{Org: domain.OrgID(in.OrgID), Project: domain.ProjectID(in.ProjectID)}
		return executeToolPage(ctx, bearer, services.Admission, toolPageSpec[service.Environment, environmentElement, environmentsOutput]{
			pageSize: in.PageSize, cursor: in.Cursor, operation: authz.OpEnvList, scope: scope,
			cursorScope: CursorScope{Tool: ToolListEnvironments, Org: in.OrgID, Project: in.ProjectID},
			fetch: func(ctx context.Context, actor service.Actor, after string, limit int) ([]service.Environment, error) {
				afterOrder, afterName, err := parseEnvironmentPosition(after)
				if err != nil {
					return nil, err
				}
				return services.Environments.ListPage(ctx, actor, scope, afterOrder, afterName, limit)
			},
			mapRow: mapEnvironment, key: environmentPosition,
			output: func(items []environmentElement, next string) environmentsOutput {
				return environmentsOutput{OrgID: in.OrgID, ProjectID: in.ProjectID, Environments: items, NextCursor: next}
			},
		})
	})
}

func registerInspectConfiguration(registry *Registry, services ProductionServices) error {
	spec, err := toolSpec(ToolInspectConfiguration, "Inspect configuration",
		"Read-only. Requires read@environment for explicit org_id/project_id/environment_id. Lists configuration cells: config plaintext may appear; a secret carries only classification and set/absent presence, never plaintext. Creates no draft, publishes nothing, and requires no user interaction.",
		"service.Values.List", "value.list", []string{"read@environment"})
	if err != nil {
		return err
	}
	return Register(registry, spec, func(ctx context.Context, bearer Bearer, in envInput) (configurationOutput, error) {
		scope := domain.Scope{Org: domain.OrgID(in.OrgID), Project: domain.ProjectID(in.ProjectID), Env: domain.EnvID(in.EnvironmentID)}
		return executeToolPage(ctx, bearer, services.Admission, toolPageSpec[service.ValueCell, configurationElement, configurationOutput]{
			pageSize: in.PageSize, cursor: in.Cursor, operation: authz.OpValueList, scope: scope,
			cursorScope: CursorScope{Tool: ToolInspectConfiguration, Org: in.OrgID, Project: in.ProjectID, Env: in.EnvironmentID},
			fetch: func(ctx context.Context, actor service.Actor, after string, limit int) ([]service.ValueCell, error) {
				return services.Configuration.ListPage(ctx, actor, scope, after, limit)
			},
			mapRow: mapConfiguration, key: func(c service.ValueCell) string { return c.Name },
			output: func(items []configurationElement, next string) configurationOutput {
				return configurationOutput{OrgID: in.OrgID, ProjectID: in.ProjectID, EnvironmentID: in.EnvironmentID, Configuration: items, NextCursor: next}
			},
		})
	})
}

func registerListPendingChanges(registry *Registry, services ProductionServices) error {
	spec, err := toolSpec(ToolListPendingChanges, "List pending changes",
		"Read-only. Requires read@environment for explicit org_id/project_id/environment_id. Lists the caller's own pending drafts: config draft plaintext may appear; a secret or unset draft never carries material. Creates no draft, publishes nothing, and requires no user interaction.",
		"service.Revisions.PendingDrafts", "value.pending-list", []string{"read@environment"})
	if err != nil {
		return err
	}
	return Register(registry, spec, func(ctx context.Context, bearer Bearer, in envInput) (pendingOutput, error) {
		scope := domain.Scope{Org: domain.OrgID(in.OrgID), Project: domain.ProjectID(in.ProjectID), Env: domain.EnvID(in.EnvironmentID)}
		return executeToolPage(ctx, bearer, services.Admission, toolPageSpec[service.PendingDraft, pendingElement, pendingOutput]{
			pageSize: in.PageSize, cursor: in.Cursor, operation: authz.OpValuePendingList, scope: scope,
			cursorScope: CursorScope{Tool: ToolListPendingChanges, Org: in.OrgID, Project: in.ProjectID, Env: in.EnvironmentID},
			fetch: func(ctx context.Context, actor service.Actor, after string, limit int) ([]service.PendingDraft, error) {
				return services.Pending.PendingDraftsPage(ctx, actor, scope, after, limit)
			},
			mapRow: mapPending, key: func(d service.PendingDraft) string { return d.KeyID },
			output: func(items []pendingElement, next string) pendingOutput {
				return pendingOutput{OrgID: in.OrgID, ProjectID: in.ProjectID, EnvironmentID: in.EnvironmentID, PendingChanges: items, NextCursor: next}
			},
		})
	})
}

func registerListRevisions(registry *Registry, services ProductionServices) error {
	spec, err := toolSpec(ToolListRevisions, "List revisions",
		"Read-only. Requires read@environment for explicit org_id/project_id/environment_id. Lists revision metadata, changed key names, publisher, time, schema revision, and payload-presence state, with no values or change token. Creates no draft, publishes nothing, and requires no user interaction.",
		"service.Revisions.History", "revision.list", []string{"read@environment"})
	if err != nil {
		return err
	}
	return Register(registry, spec, func(ctx context.Context, bearer Bearer, in envInput) (revisionsOutput, error) {
		scope := domain.Scope{Org: domain.OrgID(in.OrgID), Project: domain.ProjectID(in.ProjectID), Env: domain.EnvID(in.EnvironmentID)}
		return executeToolPage(ctx, bearer, services.Admission, toolPageSpec[service.RevisionView, revisionElement, revisionsOutput]{
			pageSize: in.PageSize, cursor: in.Cursor, operation: authz.OpRevisionList, scope: scope,
			cursorScope: CursorScope{Tool: ToolListRevisions, Org: in.OrgID, Project: in.ProjectID, Env: in.EnvironmentID},
			fetch: func(ctx context.Context, actor service.Actor, after string, limit int) ([]service.RevisionView, error) {
				return services.Revisions.HistoryPage(ctx, actor, scope, beforeRevisionFrom(after), limit)
			},
			mapRow: mapRevision, key: func(v service.RevisionView) string { return strconv.FormatInt(v.Revision, 10) },
			output: func(items []revisionElement, next string) revisionsOutput {
				return revisionsOutput{OrgID: in.OrgID, ProjectID: in.ProjectID, EnvironmentID: in.EnvironmentID, Revisions: items, NextCursor: next}
			},
		})
	})
}

func encodedSize(value any) (int, error) {
	encoded, err := json.Marshal(value)
	return len(encoded), err
}

// beforeRevisionFrom decodes the revision keyset position. The first page ("")
// starts above the newest revision; a continuation carries the last returned
// revision as a decimal string.
func beforeRevisionFrom(after string) int64 {
	if after == "" {
		return math.MaxInt64
	}
	value, err := strconv.ParseInt(after, 10, 64)
	if err != nil {
		return math.MaxInt64
	}
	return value
}

func environmentPosition(environment service.Environment) string {
	return strconv.FormatInt(environment.DisplayOrder, 10) + "\x00" + environment.Name
}

func parseEnvironmentPosition(position string) (int64, string, error) {
	if position == "" {
		return -1, "", nil
	}
	orderText, name, ok := strings.Cut(position, "\x00")
	if !ok || name == "" {
		return 0, "", ErrInvalidCursor
	}
	order, err := strconv.ParseInt(orderText, 10, 64)
	if err != nil || order < 0 {
		return 0, "", ErrInvalidCursor
	}
	return order, name, nil
}

func mapDefinition(k service.Key) (definitionElement, error) {
	return definitionElement{
		Name: k.Name, Description: k.Description, Classification: k.Classification,
		Deprecated: k.Deprecated, DeprecationNote: k.DeprecationNote, GroupID: k.GroupID,
		Declaration: k.Declaration, Presence: k.Presence,
	}, nil
}

func mapEnvironment(e service.Environment) (environmentElement, error) {
	return environmentElement{
		ID: e.ID, Name: e.Name, Note: e.Note, DisplayOrder: e.DisplayOrder, CreatedAt: e.CreatedAt,
	}, nil
}

func mapConfiguration(c service.ValueCell) (configurationElement, error) {
	element := configurationElement{
		KeyID: c.KeyID, Name: c.Name, Classification: c.Classification,
		Set: c.Set, UpdatedAt: c.UpdatedAt, UpdatedBy: c.UpdatedBy,
	}
	// Defense in depth: only a `config` cell carries plaintext, and the service
	// never opens a `secret` cell on this reveal-false path.
	if c.Classification == string(schema.Config) {
		element.Value = c.Value
	}
	return element, nil
}

func mapPending(d service.PendingDraft) (pendingElement, error) {
	element := pendingElement{
		VersionID: d.VersionID, KeyID: d.KeyID, Name: d.Name, Classification: d.Classification,
		Operation: d.Operation, StagedFromRevision: d.StagedFromRevision, CreatedAt: d.CreatedAt,
	}
	if d.Classification == string(schema.Config) {
		element.Value = d.Value
	}
	return element, nil
}

func mapRevision(v service.RevisionView) (revisionElement, error) {
	changed := make([]changedKeyElement, 0, len(v.ChangedKeys))
	for _, key := range v.ChangedKeys {
		changed = append(changed, changedKeyElement{Name: key.Name, Change: key.Change})
	}
	return revisionElement{
		Revision: v.Revision, SchemaRevision: v.SchemaRevision,
		PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt, ChangedKeys: changed,
		PayloadPresent: v.PayloadPresent, CollectedPolicy: v.CollectedPolicy,
	}, nil
}
