package server

import (
	"context"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// Scheduled credential rotation configuration (#148, slice S1). The handlers
// are thin: scope, authorization, validation and audit live in the service.

func credentialRotationResponse(v service.RotationView) (apigen.CredentialRotation, error) {
	created, err := time.Parse(time.RFC3339Nano, v.CreatedAt)
	if err != nil {
		return apigen.CredentialRotation{}, fmt.Errorf("server: parse rotation created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339Nano, v.UpdatedAt)
	if err != nil {
		return apigen.CredentialRotation{}, fmt.Errorf("server: parse rotation updated_at: %w", err)
	}
	next, err := parseOptTime(v.NextRunAt)
	if err != nil {
		return apigen.CredentialRotation{}, err
	}
	expires, err := parseOptTime(v.CredentialExpiresAt)
	if err != nil {
		return apigen.CredentialRotation{}, err
	}
	return apigen.CredentialRotation{
		Id:                     apigen.ID(v.ID),
		EnvironmentId:          apigen.ID(v.EnvironmentID),
		ProviderId:             apigen.ID(v.ProviderID),
		KeyId:                  apigen.ID(v.KeyID),
		UsernameKeyId:          apigen.ID(v.UsernameKeyID),
		IntervalSeconds:        v.IntervalSeconds,
		RevocationDelaySeconds: v.RevocationDelaySeconds,
		AuthorityPrincipalId:   apigen.ID(v.AuthorityPrincipalID),
		CreatedBy:              apigen.ID(v.CreatedBy),
		State:                  apigen.CredentialRotationState(v.State),
		NextRunAt:              next,
		CredentialExpiresAt:    expires,
		Generation:             v.Generation,
		CreatedAt:              created,
		UpdatedAt:              updated,
	}, nil
}

func (a *API) ListRotations(ctx context.Context, req apigen.ListRotationsRequestObject) (apigen.ListRotationsResponseObject, error) {
	includeDisabled := req.Params.IncludeDisabled != nil && *req.Params.IncludeDisabled
	rows, err := a.CredentialRotations.List(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), includeDisabled)
	if err != nil {
		return nil, err
	}
	out := apigen.CredentialRotationList{Items: []apigen.CredentialRotation{}}
	for _, row := range rows {
		resp, err := credentialRotationResponse(row)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, resp)
	}
	return apigen.ListRotations200JSONResponse(out), nil
}

func (a *API) CreateRotation(ctx context.Context, req apigen.CreateRotationRequestObject) (apigen.CreateRotationResponseObject, error) {
	view, err := a.CredentialRotations.Create(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), service.CreateRotationRequest{
		ProviderID: string(req.Body.ProviderId), KeyID: string(req.Body.KeyId), UsernameKeyID: string(req.Body.UsernameKeyId),
		IntervalSeconds: req.Body.IntervalSeconds, RevocationDelaySeconds: req.Body.RevocationDelaySeconds,
	})
	if err != nil {
		return nil, err
	}
	resp, err := credentialRotationResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.CreateRotation201JSONResponse(resp), nil
}

func (a *API) ShowRotation(ctx context.Context, req apigen.ShowRotationRequestObject) (apigen.ShowRotationResponseObject, error) {
	view, err := a.CredentialRotations.Get(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.Rotation))
	if err != nil {
		return nil, err
	}
	resp, err := credentialRotationResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ShowRotation200JSONResponse(resp), nil
}

func expectedGeneration(g *int64) int64 {
	if g == nil {
		return 0
	}
	return *g
}

func (a *API) PauseRotation(ctx context.Context, req apigen.PauseRotationRequestObject) (apigen.PauseRotationResponseObject, error) {
	var expected *int64
	if req.Body != nil {
		expected = req.Body.ExpectedGeneration
	}
	view, err := a.CredentialRotations.Pause(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment),
		service.RotationStateRequest{RotationID: string(req.Rotation), ExpectedGeneration: expectedGeneration(expected)})
	if err != nil {
		return nil, err
	}
	resp, err := credentialRotationResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.PauseRotation200JSONResponse(resp), nil
}

func (a *API) ResumeRotation(ctx context.Context, req apigen.ResumeRotationRequestObject) (apigen.ResumeRotationResponseObject, error) {
	var expected *int64
	if req.Body != nil {
		expected = req.Body.ExpectedGeneration
	}
	view, err := a.CredentialRotations.Resume(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment),
		service.RotationStateRequest{RotationID: string(req.Rotation), ExpectedGeneration: expectedGeneration(expected)})
	if err != nil {
		return nil, err
	}
	resp, err := credentialRotationResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ResumeRotation200JSONResponse(resp), nil
}

func (a *API) DeleteRotation(ctx context.Context, req apigen.DeleteRotationRequestObject) (apigen.DeleteRotationResponseObject, error) {
	view, err := a.CredentialRotations.Delete(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment),
		service.RotationStateRequest{RotationID: string(req.Rotation), ExpectedGeneration: expectedGeneration(req.Params.ExpectedGeneration)})
	if err != nil {
		return nil, err
	}
	resp, err := credentialRotationResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.DeleteRotation200JSONResponse(resp), nil
}
