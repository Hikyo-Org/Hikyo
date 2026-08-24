package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/updater"
)

func (a *API) GetUpdateStatus(ctx context.Context, _ apigen.GetUpdateStatusRequestObject) (apigen.GetUpdateStatusResponseObject, error) {
	status, err := a.Updates.GetStatus(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	response := apigen.GetUpdateStatus200JSONResponse{
		ApplySupported: status.ApplySupported,
		Available:      status.Available,
		Channel:        apigen.UpdateStatusChannel(status.Channel),
		CurrentVersion: status.CurrentVersion,
		Prerelease:     status.Prerelease,
	}
	if status.ApplyBackend != "" {
		backend := apigen.InstanceUpdateBackend(status.ApplyBackend)
		response.ApplyBackend = &backend
	}
	if status.ApplyError != "" {
		response.ApplyError = &status.ApplyError
	}
	if status.LatestVersion != "" {
		response.LatestVersion = &status.LatestVersion
	}
	if status.URL != "" {
		response.ReleaseUrl = &status.URL
	}
	if !status.PublishedAt.IsZero() {
		response.PublishedAt = &status.PublishedAt
	}
	return response, nil
}

func (a *API) RequestInstanceUpdate(ctx context.Context, req apigen.RequestInstanceUpdateRequestObject) (apigen.RequestInstanceUpdateResponseObject, error) {
	if req.Body == nil {
		return nil, domain.ErrInvalid
	}
	job, err := a.Updates.Request(ctx, service.Bearer(bearer(ctx)), req.Body.Version)
	if err != nil {
		return nil, err
	}
	return apigen.RequestInstanceUpdate202JSONResponse(wireUpdateJob(job)), nil
}

func (a *API) GetInstanceUpdateJob(ctx context.Context, req apigen.GetInstanceUpdateJobRequestObject) (apigen.GetInstanceUpdateJobResponseObject, error) {
	job, err := a.Updates.GetJob(ctx, service.Bearer(bearer(ctx)), string(req.Job))
	if err != nil {
		return nil, err
	}
	return apigen.GetInstanceUpdateJob200JSONResponse(wireUpdateJob(job)), nil
}

func wireUpdateJob(job updater.Job) apigen.InstanceUpdateJob {
	wire := apigen.InstanceUpdateJob{
		Id: apigen.ID(job.ID), Backend: apigen.InstanceUpdateBackend(job.Backend),
		Version: job.Version, State: apigen.InstanceUpdateJobState(job.State),
		Phase: apigen.InstanceUpdateJobPhase(job.Phase), RequestedAt: job.RequestedAt,
	}
	if job.FailureCode != "" {
		wire.FailureCode = &job.FailureCode
	}
	if !job.StartedAt.IsZero() {
		wire.StartedAt = &job.StartedAt
	}
	if !job.FinishedAt.IsZero() {
		wire.FinishedAt = &job.FinishedAt
	}
	return wire
}
