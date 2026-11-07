package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func (a *API) ListPersonalValueCounts(ctx context.Context, req apigen.ListPersonalValueCountsRequestObject) (apigen.ListPersonalValueCountsResponseObject, error) {
	counts, err := a.PersonalValues.Counts(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.PersonalValueCounts{Eligible: counts.Eligible, Items: make([]apigen.PersonalValueCount, 0, len(counts.Items))}
	for _, c := range counts.Items {
		out.Items = append(out.Items, apigen.PersonalValueCount{KeyId: c.KeyID, Name: c.Name, Holders: c.Holders, Notices: c.Notices, Required: c.Required})
	}
	return apigen.ListPersonalValueCounts200JSONResponse(out), nil
}

func (a *API) GetPersonalOffboardingReport(ctx context.Context, req apigen.GetPersonalOffboardingReportRequestObject) (apigen.GetPersonalOffboardingReportResponseObject, error) {
	scope := domain.Scope{Org: domain.OrgID(req.Org), Project: domain.ProjectID(req.Project)}
	r, err := a.PersonalValues.OffboardingReport(ctx, service.Bearer(bearer(ctx)), scope, req.Member)
	if err != nil {
		return nil, err
	}
	out := apigen.PersonalOffboardingReport{
		PrincipalId:     string(r.Principal),
		Username:        r.Username,
		EvidenceFrom:    r.EvidenceFrom,
		GeneratedAt:     r.GeneratedAt,
		Received:        make([]apigen.PersonalOffboardingReceipt, 0, len(r.Received)),
		ServiceAccounts: make([]apigen.PersonalOffboardingServiceAccount, 0, len(r.ServiceAccounts)),
		Limits:          append([]string{}, r.Limits...),
	}
	for _, x := range r.Received {
		out.Received = append(out.Received, apigen.PersonalOffboardingReceipt{
			EnvironmentId: string(x.EnvironmentID), EnvironmentName: x.EnvironmentName, KeyName: x.KeyName,
			LastReceivedAt: x.LastReceivedAt, ChangedSince: x.ChangedSince,
		})
	}
	for _, sa := range r.ServiceAccounts {
		out.ServiceAccounts = append(out.ServiceAccounts, apigen.PersonalOffboardingServiceAccount{Id: sa.ID, Name: sa.Name, CreatedAt: sa.CreatedAt})
	}
	return apigen.GetPersonalOffboardingReport200JSONResponse(out), nil
}
