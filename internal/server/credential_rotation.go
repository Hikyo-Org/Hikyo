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
	out := apigen.CredentialRotation{
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
		RestoreHold:            v.RestoreHold,
		NextRunAt:              next,
		CredentialExpiresAt:    expires,
		Generation:             v.Generation,
		CreatedAt:              created,
		UpdatedAt:              updated,
		// The two counts of runs that owe a role a drop: the summary read for the
		// whole page, the same definition the CLI and the gauges use.
		OwedDropRuns:                v.OwedDropRuns,
		OwedDropRunsVersionsChanged: v.OwedDropRunsVersionsChanged,
	}
	if v.HaltReason != "" {
		reason := apigen.RotationHaltReason(v.HaltReason)
		out.HaltReason = &reason
	}
	if v.LastRunSummary != nil {
		last := apigen.RotationLastRun{Id: apigen.ID(v.LastRunSummary.ID), Phase: apigen.RotationRunPhase(v.LastRunSummary.Phase), PhaseAt: v.LastRunSummary.PhaseAt}
		if v.LastRunSummary.ErrorCode != "" {
			last.ErrorCode = &v.LastRunSummary.ErrorCode
		}
		out.LastRun = &last
	}
	return out, nil
}

func (a *API) ListRotations(ctx context.Context, req apigen.ListRotationsRequestObject) (apigen.ListRotationsResponseObject, error) {
	page := service.RotationListRequest{IncludeDisabled: req.Params.IncludeDisabled != nil && *req.Params.IncludeDisabled}
	if req.Params.Limit != nil {
		page.Limit = *req.Params.Limit
	}
	if req.Params.After != nil {
		page.After = *req.Params.After
	}
	got, err := a.CredentialRotations.List(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), page)
	if err != nil {
		return nil, err
	}
	out := apigen.CredentialRotationList{Items: []apigen.CredentialRotation{}}
	for _, row := range got.Items {
		resp, err := credentialRotationResponse(row)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, resp)
	}
	if got.NextAfter != "" {
		out.NextAfter = &got.NextAfter
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

func (a *API) ReleaseRotationHold(ctx context.Context, req apigen.ReleaseRotationHoldRequestObject) (apigen.ReleaseRotationHoldResponseObject, error) {
	var expected *int64
	if req.Body != nil {
		expected = req.Body.ExpectedGeneration
	}
	view, err := a.CredentialRotations.ReleaseHold(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment),
		service.RotationStateRequest{RotationID: string(req.Rotation), ExpectedGeneration: expectedGeneration(expected)})
	if err != nil {
		return nil, err
	}
	resp, err := credentialRotationResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ReleaseRotationHold200JSONResponse(resp), nil
}

func (a *API) DeleteRotation(ctx context.Context, req apigen.DeleteRotationRequestObject) (apigen.DeleteRotationResponseObject, error) {
	view, err := a.CredentialRotations.Delete(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment),
		service.RotationStateRequest{RotationID: string(req.Rotation), ExpectedGeneration: expectedGeneration(req.Params.ExpectedGeneration), Abandon: req.Params.Abandon != nil && *req.Params.Abandon})
	if err != nil {
		return nil, err
	}
	resp, err := credentialRotationResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.DeleteRotation200JSONResponse(resp), nil
}

func (a *API) RunRotationNow(ctx context.Context, req apigen.RunRotationNowRequestObject) (apigen.RunRotationNowResponseObject, error) {
	// The body is required, so the strict handler never passes a nil one.
	run := service.RunNowRequest{
		RotationID:         string(req.Rotation),
		ExpectedGeneration: expectedGeneration(req.Body.ExpectedGeneration),
		SkipOverlap:        req.Body.SkipOverlap != nil && *req.Body.SkipOverlap,
		ConfirmDestructive: req.Body.ConfirmDestructive,
	}
	got, err := a.CredentialRotations.RunNow(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), run)
	if err != nil {
		return nil, err
	}
	return apigen.RunRotationNow202JSONResponse(apigen.RotationRunRequested{RotationId: apigen.ID(got.RotationID), RunId: apigen.ID(got.RunID), SkipOverlap: got.SkipOverlap}), nil
}

func (a *API) ListRotationRuns(ctx context.Context, req apigen.ListRotationRunsRequestObject) (apigen.ListRotationRunsResponseObject, error) {
	page := service.RotationRunsRequest{RotationID: string(req.Rotation)}
	if req.Params.Limit != nil {
		page.Limit = *req.Params.Limit
	}
	if req.Params.After != nil {
		page.After = *req.Params.After
	}
	got, err := a.CredentialRotations.Runs(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), page)
	if err != nil {
		return nil, err
	}
	out := apigen.RotationRunList{Items: []apigen.RotationRun{}}
	for _, row := range got.Items {
		out.Items = append(out.Items, rotationRunBody(row))
	}
	if got.NextAfter != "" {
		out.NextAfter = &got.NextAfter
	}
	return apigen.ListRotationRuns200JSONResponse(out), nil
}

// rotationRunBody maps a run's view onto its wire shape; the lower-bound flag
// is only ever sent with an expiry, and only when true.
func rotationRunBody(row service.RotationRunView) apigen.RotationRun {
	run := apigen.RotationRun{
		Id: apigen.ID(row.ID), Trigger: apigen.RotationRunTrigger(row.Trigger), Phase: apigen.RotationRunPhase(row.Phase),
		RevocationDelaySeconds: row.RevocationDelaySeconds, StartedAt: row.StartedAt, PhaseAt: row.PhaseAt, NotConvergedAdapterTargets: row.NotConvergedAdapterTargets,
	}
	if row.PublishedRevision != 0 {
		run.PublishedRevision = &row.PublishedRevision
	}
	if row.ErrorCode != "" {
		run.ErrorCode = &row.ErrorCode
	}
	if !row.CredentialValidUntil.IsZero() {
		run.CredentialValidUntil = &row.CredentialValidUntil
		if row.CredentialValidUntilLowerBound {
			run.CredentialValidUntilLowerBound = &row.CredentialValidUntilLowerBound
		}
	}
	return run
}
