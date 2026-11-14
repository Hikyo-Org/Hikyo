package audit

import "github.com/Hikyo-Org/hikyo/internal/domain"

// Incident ledger events (incident-containment ADR, "Audit trail"; #1039 S2).
// They ride the instance trail. The incident id is the correlation id, so an
// owner's own revocation event joins to the incident by correlation and an item's
// intent and outcome pair up as adapter push intent and outcome do. Principal
// contained and released events belong to the containment service, not the
// ledger.
//
// Payloads carry the actor (the event's own actor), the scope, closed reasons,
// counts and Hikyo object ids only: no secret, no free text and no
// attacker-supplied string.
const (
	EventIncidentOpened     EventType = "incident.opened"
	EventIncidentItemIntent EventType = "incident.item_intent"
	// EventIncidentItemOutcome is licensed for success (done or attested),
	// unknown, denied (blocked), and failure (the owner reported a failure and
	// the item stays open).
	EventIncidentItemOutcome EventType = "incident.item_outcome"
	EventIncidentClosed      EventType = "incident.closed"
)

// MaxIncidentResiduals bounds the residual item ids one incident.closed event
// lists. The close counts the ones it left out, so the cut is never silent.
const MaxIncidentResiduals = 1000

var incidentCounts = Schema{
	"pending":      {Kind: KindInt, Required: true, NonNegative: true},
	"done":         {Kind: KindInt, Required: true, NonNegative: true},
	"unknown":      {Kind: KindInt, Required: true, NonNegative: true},
	"attested":     {Kind: KindInt, Required: true, NonNegative: true},
	"blocked":      {Kind: KindInt, Required: true, NonNegative: true},
	"out_of_scope": {Kind: KindInt, Required: true, NonNegative: true},
}

var incidentScopeKinds = []string{"account", "org", "project", "adapter"}

var incidentItemReasons = []string{
	"none", "needs_another_operator", "owner_unconfirmed", "owner_failed", "enumeration_truncated",
	"no_revoke_owner", "delivered_plaintext", "cache", "running_process", "external_session",
	"issued_certificate_in_use", "upstream_not_observable",
}

func incidentSchema(extra Schema) Schema {
	out := Schema{}
	for k, v := range incidentCounts {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

var incidentSpecs = map[EventType]TypeSpec{
	EventIncidentOpened: {
		SchemaVersion: 1, Retention: RetentionSecurity,
		Outcomes: map[Outcome]bool{OutcomeSuccess: true}, Trails: map[Trail]bool{TrailInstance: true},
		Schema: incidentSchema(Schema{
			"scope_kind": {Kind: KindString, Required: true, Enum: incidentScopeKinds},
			"scope_id":   {Kind: KindString, Required: true},
			"org":        {Kind: KindString},
			// reopened is true when the scope already had an open incident: the
			// enumeration ran again and added only objects the ledger lacked.
			"reopened": {Kind: KindBool, Required: true},
			"added":    {Kind: KindInt, Required: true, NonNegative: true},
			// grants and rules count the access rows the census read. They decide
			// the footprint and are never work items: containment keeps them.
			"grants": {Kind: KindInt, Required: true, NonNegative: true},
			"rules":  {Kind: KindInt, Required: true, NonNegative: true},
		}),
	},
	EventIncidentItemIntent: {
		SchemaVersion: 1, Retention: RetentionSecurity,
		Outcomes: map[Outcome]bool{OutcomeIntent: true}, Trails: map[Trail]bool{TrailInstance: true},
		Schema: Schema{
			"item_id":        {Kind: KindString, Required: true},
			"owner_function": {Kind: KindString, Required: true, Enum: domain.IncidentOwnerFunctions},
			"object_kind":    {Kind: KindString, Required: true, Enum: domain.IncidentObjectKinds},
			"object_id":      {Kind: KindString, Required: true},
		},
	},
	EventIncidentItemOutcome: {
		SchemaVersion: 1, Retention: RetentionSecurity,
		Outcomes: map[Outcome]bool{OutcomeSuccess: true, OutcomeDenied: true, OutcomeFailure: true, OutcomeUnknown: true},
		Trails:   map[Trail]bool{TrailInstance: true},
		Schema: Schema{
			"item_id":        {Kind: KindString, Required: true},
			"owner_function": {Kind: KindString, Required: true, Enum: domain.IncidentOwnerFunctions},
			"object_kind":    {Kind: KindString, Required: true, Enum: domain.IncidentObjectKinds},
			"object_id":      {Kind: KindString, Required: true},
			"result":         {Kind: KindString, Required: true, Enum: []string{"pending", "done", "unknown", "attested", "blocked"}},
			"reason":         {Kind: KindString, Required: true, Enum: incidentItemReasons},
		},
	},
	EventIncidentClosed: {
		SchemaVersion: 1, Retention: RetentionSecurity,
		Outcomes: map[Outcome]bool{OutcomeSuccess: true}, Trails: map[Trail]bool{TrailInstance: true},
		Schema: incidentSchema(Schema{
			"status": {Kind: KindString, Required: true, Enum: []string{"closed", "closed_with_residuals"}},
			// residuals lists the items still open when the incident closed with
			// residuals, by item id and bounded; an incident closed clean lists none.
			"residuals": {Kind: KindStringList, Required: true, MaxLen: MaxIncidentResiduals, MaxBytes: 256},
			// residuals_omitted counts the open items past the bound that the list
			// does not name; zero means the list is complete.
			"residuals_omitted": {Kind: KindInt, Required: true, NonNegative: true},
		}),
	},
}
