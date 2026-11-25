package server

import (
	"context"
	"fmt"
	"math"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// The incident ledger transport (#1039). It TRANSLATES and decides nothing:
// each service call authorizes the incident's owning org, or the instance for an
// incident that spans orgs, inside its own transaction.

// defaultIncidentPage is the item page size when the request names none.
const defaultIncidentPage = 100

// clamp32 narrows a ledger count; a ledger holds far fewer than 2^31 items.
func clamp32(n int64) int32 {
	if n < 0 {
		return 0
	}
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n)
}

func count(v service.IncidentView, o domain.ItemOutcome) int32 { return clamp32(v.Counts[o]) }

func wireIncident(v service.IncidentView) apigen.Incident {
	inc := v.Incident
	out := apigen.Incident{
		Id: inc.ID, ScopeKind: string(inc.ScopeKind), ScopeId: inc.ScopeID, Status: string(inc.Status),
		OpenedBy: string(inc.OpenedBy), OpenedAt: inc.OpenedAt,
		Counts: apigen.IncidentOutcomeCounts{
			Pending: count(v, domain.ItemPending), Refused: clamp32(v.Refused), Done: count(v, domain.ItemDone), Unknown: count(v, domain.ItemUnknown),
			Attested: count(v, domain.ItemAttested), Blocked: count(v, domain.ItemBlocked), OutOfScope: count(v, domain.ItemOutOfScope),
		},
		Org: optStr(string(inc.OrgID)),
	}
	if inc.ClosedBy != "" {
		closedBy := string(inc.ClosedBy)
		out.ClosedBy = &closedBy
	}
	if !inc.ClosedAt.IsZero() {
		out.ClosedAt = &inc.ClosedAt
	}
	if v.Reopened {
		out.Reopened = &v.Reopened
		added := clamp32(int64(v.Added))
		out.Added = &added
	}
	return out
}

func wireIncidentItem(v service.IncidentItemView) apigen.IncidentItem {
	out := apigen.IncidentItem{
		Id: v.ID, OwnerFunction: v.OwnerFunction, ObjectKind: v.ObjectKind, ObjectId: v.ObjectID,
		Outcome: apigen.IncidentItemOutcome(v.Outcome), Refused: v.Refused, Attestable: v.Attestable,
		Reason: optStr(string(v.Reason)), ObjectState: optStr(v.ObjectState),
		UpdatedAt: v.UpdatedAt,
	}
	if v.AttestedBy != "" {
		by := string(v.AttestedBy)
		out.AttestedBy = &by
	}
	if !v.AttestedAt.IsZero() {
		out.AttestedAt = &v.AttestedAt
	}
	return out
}

// incidentTarget maps the open request to the service target. A kind the
// service does not know is refused there.
func incidentTarget(body apigen.IncidentOpenRequest) service.IncidentTarget {
	t := service.IncidentTarget{Kind: domain.IncidentScopeKind(body.ScopeKind)}
	if body.Principal != nil {
		t.Principal = domain.PrincipalID(*body.Principal)
	}
	if body.Org != nil {
		t.Scope.Org = domain.OrgID(*body.Org)
	}
	if body.Project != nil {
		t.Scope.Project = domain.ProjectID(*body.Project)
	}
	if body.Adapter != nil {
		t.AdapterID = *body.Adapter
	}
	return t
}

func (a *API) OpenIncident(ctx context.Context, req apigen.OpenIncidentRequestObject) (apigen.OpenIncidentResponseObject, error) {
	if req.Body == nil {
		return nil, fmt.Errorf("%w: an incident needs a scope", domain.ErrInvalid)
	}
	v, err := a.Incidents.Open(ctx, service.Bearer(bearer(ctx)), incidentTarget(*req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.OpenIncident200JSONResponse(wireIncident(v)), nil
}

func (a *API) GetIncident(ctx context.Context, req apigen.GetIncidentRequestObject) (apigen.GetIncidentResponseObject, error) {
	v, err := a.Incidents.Get(ctx, service.Bearer(bearer(ctx)), string(req.Incident))
	if err != nil {
		return nil, err
	}
	return apigen.GetIncident200JSONResponse(wireIncident(v)), nil
}

func (a *API) ListIncidentItems(ctx context.Context, req apigen.ListIncidentItemsRequestObject) (apigen.ListIncidentItemsResponseObject, error) {
	limit, after, openOnly := defaultIncidentPage, "", false
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if req.Params.After != nil {
		after = *req.Params.After
	}
	if req.Params.OpenOnly != nil {
		openOnly = *req.Params.OpenOnly
	}
	items, err := a.Incidents.ItemViews(ctx, service.Bearer(bearer(ctx)), string(req.Incident), after, limit, openOnly)
	if err != nil {
		return nil, err
	}
	out := apigen.IncidentItemList{Items: make([]apigen.IncidentItem, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, wireIncidentItem(it))
	}
	// A full page may be the last; the next page is then empty.
	if len(items) == limit {
		next := items[len(items)-1].ID
		out.NextAfter = &next
	}
	return apigen.ListIncidentItems200JSONResponse(out), nil
}

func (a *API) ResumeIncident(ctx context.Context, req apigen.ResumeIncidentRequestObject) (apigen.ResumeIncidentResponseObject, error) {
	if req.Body == nil {
		return nil, domain.ErrInvalid
	}
	confirmed := req.Body.ItemsDigest
	r, err := a.Incidents.Resume(ctx, service.Bearer(bearer(ctx)), string(req.Incident), req.Body.Proof, &confirmed)
	if err != nil {
		return nil, err
	}
	return apigen.ResumeIncident200JSONResponse(apigen.IncidentResumeResult{
		Examined: clamp32(int64(r.Examined)), Confirmed: clamp32(int64(r.Confirmed)), Unconfirmed: clamp32(int64(r.Unconfirmed)),
		Failed: clamp32(int64(r.Failed)), Blocked: clamp32(int64(r.Blocked)), Stale: clamp32(int64(r.Stale)), Skipped: clamp32(int64(r.Skipped)),
	}), nil
}

func (a *API) AttestIncidentItem(ctx context.Context, req apigen.AttestIncidentItemRequestObject) (apigen.AttestIncidentItemResponseObject, error) {
	if err := a.Incidents.Attest(ctx, service.Bearer(bearer(ctx)), string(req.Incident), string(req.Item)); err != nil {
		return nil, err
	}
	return apigen.AttestIncidentItem204Response{}, nil
}

func (a *API) CloseIncident(ctx context.Context, req apigen.CloseIncidentRequestObject) (apigen.CloseIncidentResponseObject, error) {
	withResiduals := req.Body != nil && req.Body.WithResiduals != nil && *req.Body.WithResiduals
	v, err := a.Incidents.Close(ctx, service.Bearer(bearer(ctx)), string(req.Incident), withResiduals)
	if err != nil {
		return nil, err
	}
	return apigen.CloseIncident200JSONResponse(wireIncident(v)), nil
}
