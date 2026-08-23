package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func (a *API) GetUpdateStatus(ctx context.Context, _ apigen.GetUpdateStatusRequestObject) (apigen.GetUpdateStatusResponseObject, error) {
	status, err := a.Updates.GetStatus(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	response := apigen.GetUpdateStatus200JSONResponse{
		Available:      status.Available,
		Channel:        apigen.UpdateStatusChannel(status.Channel),
		CurrentVersion: status.CurrentVersion,
		Prerelease:     status.Prerelease,
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
