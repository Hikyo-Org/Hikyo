package server

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// DeveloperCredentialService preserves the raw presented human artifact. The
// service owns self-delegation, live authorization, reauthentication and policy.
type DeveloperCredentialService interface {
	Mint(context.Context, service.Actor, domain.Scope, service.MintDeveloperCredentialRequest) (service.MintedDeveloperCredential, error)
	List(context.Context, service.Actor, domain.Scope) ([]service.DeveloperCredential, error)
	Revoke(context.Context, service.Actor, domain.Scope, string, bool) error
	Policy(context.Context, service.Actor) (time.Duration, error)
	SetPolicy(context.Context, service.Actor, time.Duration) (time.Duration, error)
}

func (a *API) MintDeveloperCredential(ctx context.Context, req apigen.MintDeveloperCredentialRequestObject) (apigen.MintDeveloperCredentialResponseObject, error) {
	if req.Body == nil || !req.Body.ConsentCurrentAndFuture {
		return nil, domain.ErrInvalid
	}
	want := service.MintDeveloperCredentialRequest{ConsentCurrentAndFuture: bool(req.Body.ConsentCurrentAndFuture)}
	for _, id := range req.Body.KeyIds {
		want.KeyIDs = append(want.KeyIDs, string(id))
	}
	if req.Body.LifetimeSeconds != nil {
		if *req.Body.LifetimeSeconds <= 0 {
			return nil, domain.ErrInvalid
		}
		lifetime, err := credentialLifetime(*req.Body.LifetimeSeconds)
		if err != nil {
			return nil, err
		}
		want.Lifetime = lifetime
	}
	result, err := a.DeveloperCredentials.Mint(ctx, service.Bearer(bearer(ctx)), domain.Scope{Org: domain.OrgID(req.Org), Project: domain.ProjectID(req.Project), Env: domain.EnvID(req.Environment)}, want)
	if err != nil {
		return nil, err
	}
	return apigen.MintDeveloperCredential200JSONResponse{Value: result.Value, Credential: wireDeveloperCredential(result.Credential)}, nil
}
func wireDeveloperCredential(c service.DeveloperCredential) apigen.DeveloperCredential {
	out := apigen.DeveloperCredential{Id: c.ID, PrincipalId: string(c.PrincipalID), AuthorityPrincipalId: string(c.AuthorityPrincipal), OrgId: string(c.Scope.Org), ProjectId: string(c.Scope.Project), EnvironmentId: string(c.Scope.Env), CreatedAt: c.CreatedAt, ExpiresAt: c.ExpiresAt, PrefixHint: c.PrefixHint}
	if !c.RevokedAt.IsZero() {
		t := c.RevokedAt
		out.RevokedAt = &t
	}
	return out
}
func developerCredentialList(items []service.DeveloperCredential) apigen.DeveloperCredentialList {
	out := apigen.DeveloperCredentialList{Items: make([]apigen.DeveloperCredential, 0, len(items)), Count: len(items)}
	for _, item := range items {
		out.Items = append(out.Items, wireDeveloperCredential(item))
	}
	return out
}
func (a *API) ListMyDeveloperCredentials(ctx context.Context, _ apigen.ListMyDeveloperCredentialsRequestObject) (apigen.ListMyDeveloperCredentialsResponseObject, error) {
	items, err := a.DeveloperCredentials.List(ctx, service.Bearer(bearer(ctx)), domain.Scope{})
	if err != nil {
		return nil, err
	}
	return apigen.ListMyDeveloperCredentials200JSONResponse(developerCredentialList(items)), nil
}
func (a *API) ListProjectDeveloperCredentials(ctx context.Context, req apigen.ListProjectDeveloperCredentialsRequestObject) (apigen.ListProjectDeveloperCredentialsResponseObject, error) {
	items, err := a.DeveloperCredentials.List(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project))
	if err != nil {
		return nil, err
	}
	return apigen.ListProjectDeveloperCredentials200JSONResponse(developerCredentialList(items)), nil
}
func (a *API) RevokeMyDeveloperCredential(ctx context.Context, req apigen.RevokeMyDeveloperCredentialRequestObject) (apigen.RevokeMyDeveloperCredentialResponseObject, error) {
	if err := a.DeveloperCredentials.Revoke(ctx, service.Bearer(bearer(ctx)), domain.Scope{}, req.Credential, false); err != nil {
		return nil, err
	}
	return apigen.RevokeMyDeveloperCredential204Response{}, nil
}
func (a *API) RevokeAllMyDeveloperCredentials(ctx context.Context, _ apigen.RevokeAllMyDeveloperCredentialsRequestObject) (apigen.RevokeAllMyDeveloperCredentialsResponseObject, error) {
	if err := a.DeveloperCredentials.Revoke(ctx, service.Bearer(bearer(ctx)), domain.Scope{}, "", true); err != nil {
		return nil, err
	}
	return apigen.RevokeAllMyDeveloperCredentials204Response{}, nil
}
func (a *API) RevokeProjectDeveloperCredential(ctx context.Context, req apigen.RevokeProjectDeveloperCredentialRequestObject) (apigen.RevokeProjectDeveloperCredentialResponseObject, error) {
	if err := a.DeveloperCredentials.Revoke(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project), req.Credential, false); err != nil {
		return nil, err
	}
	return apigen.RevokeProjectDeveloperCredential204Response{}, nil
}
func (a *API) GetDeveloperCredentialPolicy(ctx context.Context, _ apigen.GetDeveloperCredentialPolicyRequestObject) (apigen.GetDeveloperCredentialPolicyResponseObject, error) {
	ttl, err := a.DeveloperCredentials.Policy(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	return apigen.GetDeveloperCredentialPolicy200JSONResponse{MaxLifetimeSeconds: int64(ttl / time.Second)}, nil
}
func (a *API) SetDeveloperCredentialPolicy(ctx context.Context, req apigen.SetDeveloperCredentialPolicyRequestObject) (apigen.SetDeveloperCredentialPolicyResponseObject, error) {
	if req.Body == nil {
		return nil, domain.ErrInvalid
	}
	ttl, err := credentialLifetime(req.Body.MaxLifetimeSeconds)
	if err != nil {
		return nil, err
	}
	ttl, err = a.DeveloperCredentials.SetPolicy(ctx, service.Bearer(bearer(ctx)), ttl)
	if err != nil {
		return nil, err
	}
	return apigen.SetDeveloperCredentialPolicy200JSONResponse{MaxLifetimeSeconds: int64(ttl / time.Second)}, nil
}
func developerCredentialReauthIntent(environment string, keys []string, value apigen.DeveloperCredentialReauthIntent) (service.ReauthIntent, error) {
	if !value.ConsentCurrentAndFuture {
		return service.ReauthIntent{}, domain.ErrInvalid
	}
	if value.LifetimeSeconds < 0 || value.LifetimeSeconds > 28800 {
		return service.ReauthIntent{}, domain.ErrInvalid
	}
	ttl := time.Duration(value.LifetimeSeconds) * time.Second
	return service.NewDeveloperCredentialReauthIntent(environment, keys, ttl, bool(value.ConsentCurrentAndFuture))
}
