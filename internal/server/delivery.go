package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/delivery"
	"github.com/Hikyo-Org/hikyo/internal/deliverytarget"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// The machine delivery transport (#62).
//
// This is the ONE handler that passes the raw presented artifact into the
// service rather than wrapping it in service.Bearer, and the reason is the
// artifact class: a `hik_` bearer credential resolves at the chokepoint from its
// verifier, while an externally issued OIDC ID token needs its signature checked
// against a cached JWKS first — network work that must happen before any
// transaction opens. The service owns that branch, because it owns both the JWKS
// cache and the transaction boundary; the transport's job is to hand over the
// value the caller sent, unexamined.
//
// There is little else here to get wrong: the cursor is a string the handler
// neither builds nor parses, the projection and acknowledgement are opaque
// terms the service authorizes and records, and the scope is the path. A
// delivered value is present iff the service authorized it; the handler renders
// the pointer through unchanged. Values and snapshot assertions are mapped from
// the service's authorized result.

// DeliveryService is the domain surface this transport exposes.
type DeliveryService interface {
	Fetch(ctx context.Context, presented string, scope domain.Scope, cursor string, opts service.FetchOptions) (service.FetchResult, error)
	ReconcileOfflineRecords(ctx context.Context, presented string, scope domain.Scope, records []service.OfflineRecord) (service.ReconcileResult, error)
	ReportTarget(ctx context.Context, presented string, scope domain.Scope, report deliverytarget.Report) error
	TombstoneTarget(ctx context.Context, presented string, scope domain.Scope, key service.DeliveryTargetKey) error
	RefuseOversizeReport(ctx context.Context, presented string, scope domain.Scope) error
	ListTargets(ctx context.Context, actor service.Actor, scope domain.Scope) (service.DeliveryTargetList, error)
}

func (a *API) FetchDelivery(ctx context.Context, req apigen.FetchDeliveryRequestObject) (apigen.FetchDeliveryResponseObject, error) {
	cursor := ""
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	opts := service.FetchOptions{}
	if req.Params.Parameters != nil {
		if err := json.Unmarshal([]byte(*req.Params.Parameters), &opts.Parameters); err != nil || opts.Parameters == nil {
			return nil, domain.ErrInvalid
		}
	}
	if req.Params.Projection != nil {
		opts.Projection = delivery.Mode(*req.Params.Projection)
	}
	if req.Params.AcknowledgedKeys != nil {
		opts.AcknowledgedKeys = []string(*req.Params.AcknowledgedKeys)
	}
	scope := domain.Scope{
		Org: domain.OrgID(req.Org), Project: domain.ProjectID(req.Project),
		Env: domain.EnvID(req.Environment),
	}
	res, err := a.Delivery.Fetch(ctx, bearer(ctx), scope, cursor, opts)
	if err != nil {
		return nil, err
	}
	// `keys` is a non-null empty array on the "current" disposition rather than
	// omitted: a client that has to distinguish "no keys" from "field absent"
	// would be deciding disclosure by JSON shape.
	keys := make([]apigen.DeliveredKey, 0, len(res.Keys))
	for _, k := range res.Keys {
		keys = append(keys, apigen.DeliveredKey{
			KeyId:          k.KeyID,
			Name:           k.Name,
			Classification: apigen.KeyClassification(k.Classification),
			Presence:       apigen.DeliveredKeyPresence(k.Presence),
			// Nil iff presence-only; rendered as the optional `value` member,
			// so absent means no plaintext crossed rather than an empty value.
			Value: k.Value,
		})
	}
	out := apigen.FetchDelivery200JSONResponse{
		Revision:          res.Revision,
		CredentialId:      res.CredentialID,
		Current:           res.Current,
		Cursor:            res.Cursor,
		ChangeToken:       res.ChangeToken,
		SchemaRevision:    int(res.SchemaRevision),
		Keys:              keys,
		PinExpired:        res.PinExpired,
		IssuedAt:          res.IssuedAt,
		SnapshotExpiresAt: res.SnapshotExpiresAt,
	}
	// Finite credential expiry surfaces as the optional member; the zero time
	// is an indefinite credential and stays absent.
	if !res.CredentialExpiresAt.IsZero() {
		expires := res.CredentialExpiresAt
		out.CredentialExpiresAt = &expires
	}
	if res.PinnedRevision > 0 {
		revision := res.PinnedRevision
		out.PinnedRevision = &revision
	}
	return out, nil
}

func (a *API) ReconcileOfflineRecords(ctx context.Context, req apigen.ReconcileOfflineRecordsRequestObject) (apigen.ReconcileOfflineRecordsResponseObject, error) {
	scope := domain.Scope{
		Org: domain.OrgID(req.Org), Project: domain.ProjectID(req.Project),
		Env: domain.EnvID(req.Environment),
	}
	records := make([]service.OfflineRecord, 0, len(req.Body.Records))
	for _, record := range req.Body.Records {
		records = append(records, service.OfflineRecord{
			RecordID: record.RecordId, KeyID: record.KeyId, KeyName: record.KeyName,
			Classification: string(record.Classification), OccurredAt: record.OccurredAt,
			CredentialID: record.CredentialId, Generation: record.Generation,
			ServedFrom: record.ServedFrom,
		})
	}
	res, err := a.Delivery.ReconcileOfflineRecords(ctx, bearer(ctx), scope, records)
	if err != nil {
		return nil, err
	}
	return apigen.ReconcileOfflineRecords200JSONResponse{
		Accepted: res.Accepted, Duplicates: res.Duplicates,
	}, nil
}

// Delivery-target condition reporting (#788). The report and tombstone hand
// the raw presented artifact to the service for the same reason the fetch
// does: they ride the fetch credential, bearer or federated. The body is
// already closed by the contract; the handler only renames its members.

func (a *API) ReportDeliveryTarget(ctx context.Context, req apigen.ReportDeliveryTargetRequestObject) (apigen.ReportDeliveryTargetResponseObject, error) {
	body := req.Body
	conditions := make([]deliverytarget.Condition, 0, len(body.Conditions))
	for _, c := range body.Conditions {
		conditions = append(conditions, deliverytarget.Condition{
			Type: c.Type, Status: string(c.Status), Reason: c.Reason,
			ObservedGeneration: c.ObservedGeneration,
		})
	}
	report := deliverytarget.Report{
		Vocabulary: body.Vocabulary,
		Target: deliverytarget.Target{
			ClusterID: body.Target.ClusterId, InstanceUID: body.Target.InstanceUid,
			Namespace: body.Target.Namespace, Name: body.Target.Name, UID: body.Target.Uid,
		},
		Generation: body.Generation, ObservedGeneration: body.ObservedGeneration,
		ReportedAt: body.ReportedAt, ReportIntervalSeconds: body.ReportIntervalSeconds,
		Lifecycle: string(body.Lifecycle), Conditions: conditions,
		Reporter: string(body.Reporter.Integration), ReporterVersion: body.Reporter.Version,
	}
	if err := a.Delivery.ReportTarget(ctx, bearer(ctx), envScope(req.Org, req.Project, req.Environment), report); err != nil {
		return nil, err
	}
	return apigen.ReportDeliveryTarget204Response{}, nil
}

func (a *API) TombstoneDeliveryTarget(ctx context.Context, req apigen.TombstoneDeliveryTargetRequestObject) (apigen.TombstoneDeliveryTargetResponseObject, error) {
	key := service.DeliveryTargetKey{
		ClusterID: req.Body.Target.ClusterId, InstanceUID: req.Body.Target.InstanceUid, UID: req.Body.Target.Uid,
	}
	if err := a.Delivery.TombstoneTarget(ctx, bearer(ctx), envScope(req.Org, req.Project, req.Environment), key); err != nil {
		return nil, err
	}
	return apigen.TombstoneDeliveryTarget204Response{}, nil
}

// refuseOversizeReport answers a report body over deliverytarget.MaxReportBytes
// without parsing it. Authentication and authorization still rank first, so an
// unauthorized caller gets the uniform 404, never a size answer.
func (a *API) refuseOversizeReport(w http.ResponseWriter, r *http.Request, scope domain.Scope) {
	a.writeHandlerError(w, r, a.Delivery.RefuseOversizeReport(r.Context(), bearer(r.Context()), scope))
}

func (a *API) ListDeliveryTargets(ctx context.Context, req apigen.ListDeliveryTargetsRequestObject) (apigen.ListDeliveryTargetsResponseObject, error) {
	list, err := a.Delivery.ListTargets(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.ListDeliveryTargets200JSONResponse{
		Principals: make([]apigen.DeliveryTargetPrincipal, 0, len(list.Reporters)),
		Targets:    make([]apigen.DeliveryTarget, 0, len(list.Targets)),
	}
	for _, p := range list.Reporters {
		out.Principals = append(out.Principals, apigen.DeliveryTargetPrincipal{
			PrincipalId: p.PrincipalID, LastContactAt: optionalTime(p.LastContactAt), QuotaRefusedAt: optionalTime(p.QuotaRefusedAt),
		})
	}
	for _, t := range list.Targets {
		r := t.Report
		conditions := make([]apigen.DeliveryTargetCondition, 0, len(r.Conditions))
		for _, c := range r.Conditions {
			conditions = append(conditions, apigen.DeliveryTargetCondition{
				Type: c.Type, Status: apigen.DeliveryTargetConditionStatus(c.Status),
				Reason: c.Reason, ObservedGeneration: c.ObservedGeneration,
			})
		}
		row := apigen.DeliveryTarget{
			Id: t.ID, PrincipalId: t.PrincipalID,
			Target: apigen.DeliveryTargetRef{
				ClusterId: r.Target.ClusterID, InstanceUid: r.Target.InstanceUID,
				Namespace: r.Target.Namespace, Name: r.Target.Name, Uid: r.Target.UID,
			},
			Vocabulary: r.Vocabulary, Generation: r.Generation, ObservedGeneration: r.ObservedGeneration,
			ReportedAt: r.ReportedAt, ReceivedAt: t.ReceivedAt, ReportIntervalSeconds: r.ReportIntervalSeconds,
			Lifecycle: apigen.DeliveryTargetLifecycle(r.Lifecycle), Conditions: conditions,
			Reporter: apigen.DeliveryTargetReporter{
				Integration: apigen.DeliveryTargetReporterIntegration(r.Reporter), Version: r.ReporterVersion,
			},
			State: apigen.DeliveryTargetState(t.State),
		}
		if t.RefusalCause != "" {
			row.Refusal = &struct {
				Cause     apigen.DeliveryTargetRefusalCause `json:"cause"`
				RefusedAt apigen.Timestamp                  `json:"refused_at"`
			}{Cause: apigen.DeliveryTargetRefusalCause(t.RefusalCause), RefusedAt: t.RefusedAt}
		}
		out.Targets = append(out.Targets, row)
	}
	return out, nil
}
