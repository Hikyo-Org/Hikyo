package server

import (
	"context"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func dynamicEnvScope(org apigen.OrgID, project apigen.ProjectID, env apigen.EnvironmentID) domain.Scope {
	return domain.Scope{Org: domain.OrgID(org), Project: domain.ProjectID(project), Env: domain.EnvID(env)}
}

func parseOptTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil, fmt.Errorf("server: parse timestamp %q: %w", s, err)
	}
	return &t, nil
}

func dynamicProviderResponse(v service.DynamicProviderView) (apigen.DynamicProvider, error) {
	created, err := time.Parse(time.RFC3339Nano, v.CreatedAt)
	if err != nil {
		return apigen.DynamicProvider{}, fmt.Errorf("server: parse provider created_at: %w", err)
	}
	credentialSet, err := parseOptTime(v.CredentialSetAt)
	if err != nil {
		return apigen.DynamicProvider{}, err
	}
	return apigen.DynamicProvider{
		Id:                   apigen.ID(v.ID),
		Kind:                 apigen.DynamicProviderKind(v.Kind),
		Origin:               v.Origin,
		TlsMode:              apigen.DynamicProviderTlsMode(v.TLSMode),
		GrantRole:            v.GrantRole,
		CredentialPresent:    v.CredentialPresent,
		CredentialSetAt:      credentialSet,
		AuthorityPrincipalId: apigen.ID(v.AuthorityPrincipalID),
		State:                apigen.DynamicProviderState(v.State),
		CreatedAt:            created,
	}, nil
}

func dynamicLeaseResponse(v service.DynamicLeaseView) (apigen.DynamicLease, error) {
	created, err := time.Parse(time.RFC3339Nano, v.CreatedAt)
	if err != nil {
		return apigen.DynamicLease{}, fmt.Errorf("server: parse lease created_at: %w", err)
	}
	lastTransition, err := time.Parse(time.RFC3339Nano, v.LastTransitionAt)
	if err != nil {
		return apigen.DynamicLease{}, fmt.Errorf("server: parse lease last_transition_at: %w", err)
	}
	issued, err := parseOptTime(v.IssuedAt)
	if err != nil {
		return apigen.DynamicLease{}, err
	}
	expires, err := parseOptTime(v.ExpiresAt)
	if err != nil {
		return apigen.DynamicLease{}, err
	}
	return apigen.DynamicLease{
		Id:               apigen.ID(v.ID),
		ProviderId:       apigen.ID(v.ProviderID),
		EnvironmentId:    apigen.ID(v.EnvironmentID),
		PrincipalId:      apigen.ID(v.PrincipalID),
		PrincipalClass:   v.PrincipalClass,
		ProviderHandle:   v.ProviderHandle,
		State:            apigen.DynamicLeaseState(v.State),
		IssuedAt:         issued,
		ExpiresAt:        expires,
		MaxTtlSeconds:    v.MaxTTLSeconds,
		LastTransitionAt: lastTransition,
		CreatedAt:        created,
	}, nil
}

// --- Providers ------------------------------------------------------------

func (a *API) ListDynamicProviders(ctx context.Context, req apigen.ListDynamicProvidersRequestObject) (apigen.ListDynamicProvidersResponseObject, error) {
	rows, err := a.Dynamic.List(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project))
	if err != nil {
		return nil, err
	}
	out := apigen.DynamicProviderList{Items: []apigen.DynamicProvider{}}
	for _, row := range rows {
		resp, err := dynamicProviderResponse(row)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, resp)
	}
	return apigen.ListDynamicProviders200JSONResponse(out), nil
}

func (a *API) CreateDynamicProvider(ctx context.Context, req apigen.CreateDynamicProviderRequestObject) (apigen.CreateDynamicProviderResponseObject, error) {
	tlsMode := ""
	if req.Body.TlsMode != nil {
		tlsMode = string(*req.Body.TlsMode)
	}
	credential := []byte(req.Body.Credential)
	defer crypto.Zero(credential)
	view, err := a.Dynamic.Configure(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project), service.CreateDynamicProviderRequest{
		Kind: string(req.Body.Kind), Origin: req.Body.Origin, TLSMode: tlsMode,
		GrantRole: req.Body.GrantRole, Credential: credential,
	})
	if err != nil {
		return nil, err
	}
	resp, err := dynamicProviderResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.CreateDynamicProvider201JSONResponse(resp), nil
}

func (a *API) ShowDynamicProvider(ctx context.Context, req apigen.ShowDynamicProviderRequestObject) (apigen.ShowDynamicProviderResponseObject, error) {
	view, err := a.Dynamic.Get(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project), string(req.Provider))
	if err != nil {
		return nil, err
	}
	resp, err := dynamicProviderResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ShowDynamicProvider200JSONResponse(resp), nil
}

func (a *API) DeleteDynamicProvider(ctx context.Context, req apigen.DeleteDynamicProviderRequestObject) (apigen.DeleteDynamicProviderResponseObject, error) {
	revokeAll := req.Params.RevokeAll != nil && *req.Params.RevokeAll
	result, err := a.Dynamic.Delete(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project), string(req.Provider), revokeAll)
	if err != nil {
		return nil, err
	}
	ids := make([]apigen.ID, 0, len(result.RevokedLeaseIDs))
	for _, id := range result.RevokedLeaseIDs {
		ids = append(ids, apigen.ID(id))
	}
	return apigen.DeleteDynamicProvider200JSONResponse(apigen.DynamicProviderDeletion{
		ProviderId: apigen.ID(result.ProviderID), RevokedLeaseIds: ids,
	}), nil
}

func (a *API) SetDynamicProviderCredential(ctx context.Context, req apigen.SetDynamicProviderCredentialRequestObject) (apigen.SetDynamicProviderCredentialResponseObject, error) {
	credential := []byte(req.Body.Credential)
	defer crypto.Zero(credential)
	if err := a.Dynamic.ReplaceCredential(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project), string(req.Provider), credential); err != nil {
		return nil, err
	}
	return apigen.SetDynamicProviderCredential204Response{}, nil
}

func (a *API) RevokeDynamicProviderCredential(ctx context.Context, req apigen.RevokeDynamicProviderCredentialRequestObject) (apigen.RevokeDynamicProviderCredentialResponseObject, error) {
	if err := a.Dynamic.RevokeCredential(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project), string(req.Provider)); err != nil {
		return nil, err
	}
	return apigen.RevokeDynamicProviderCredential204Response{}, nil
}

// --- Leases ---------------------------------------------------------------

func (a *API) ListLeases(ctx context.Context, req apigen.ListLeasesRequestObject) (apigen.ListLeasesResponseObject, error) {
	rows, err := a.Dynamic.ListLeases(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.DynamicLeaseList{Items: []apigen.DynamicLease{}}
	for _, row := range rows {
		resp, err := dynamicLeaseResponse(row)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, resp)
	}
	return apigen.ListLeases200JSONResponse(out), nil
}

func (a *API) MintLease(ctx context.Context, req apigen.MintLeaseRequestObject) (apigen.MintLeaseResponseObject, error) {
	result, err := a.Dynamic.MintLease(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), service.MintLeaseRequest{
		ProviderID: string(req.Body.ProviderId), MaxTTLSeconds: req.Body.MaxTtlSeconds,
	})
	if err != nil {
		return nil, err
	}
	lease, err := dynamicLeaseResponse(service.DynamicLeaseView{DynamicLease: result.Lease})
	if err != nil {
		return nil, err
	}
	expires := result.ExpiresAt
	return apigen.MintLease200JSONResponse(apigen.MintLeaseResult{
		Lease: lease, Username: result.Username, Password: result.Password, ExpiresAt: &expires,
	}), nil
}

func (a *API) ShowLease(ctx context.Context, req apigen.ShowLeaseRequestObject) (apigen.ShowLeaseResponseObject, error) {
	view, err := a.Dynamic.GetLease(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.Lease))
	if err != nil {
		return nil, err
	}
	resp, err := dynamicLeaseResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ShowLease200JSONResponse(resp), nil
}

func (a *API) RenewLease(ctx context.Context, req apigen.RenewLeaseRequestObject) (apigen.RenewLeaseResponseObject, error) {
	var maxTTL int64
	if req.Body != nil && req.Body.MaxTtlSeconds != nil {
		maxTTL = *req.Body.MaxTtlSeconds
	}
	view, err := a.Dynamic.RenewLease(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.Lease), maxTTL)
	if err != nil {
		return nil, err
	}
	resp, err := dynamicLeaseResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.RenewLease200JSONResponse(resp), nil
}

func (a *API) RevokeLease(ctx context.Context, req apigen.RevokeLeaseRequestObject) (apigen.RevokeLeaseResponseObject, error) {
	view, err := a.Dynamic.RevokeLease(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.Lease))
	if err != nil {
		return nil, err
	}
	resp, err := dynamicLeaseResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.RevokeLease200JSONResponse(resp), nil
}

func (a *API) SettleLease(ctx context.Context, req apigen.SettleLeaseRequestObject) (apigen.SettleLeaseResponseObject, error) {
	view, err := a.Dynamic.SettleLease(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.Lease))
	if err != nil {
		return nil, err
	}
	resp, err := dynamicLeaseResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.SettleLease200JSONResponse(resp), nil
}
