package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func (a *API) ListEnvironmentParameters(ctx context.Context, req apigen.ListEnvironmentParametersRequestObject) (apigen.ListEnvironmentParametersResponseObject, error) {
	out, err := a.Environments.Parameters(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	return apigen.ListEnvironmentParameters200JSONResponse(out), nil
}

func (a *API) ChangeEnvironmentParameter(ctx context.Context, req apigen.ChangeEnvironmentParameterRequestObject) (apigen.ChangeEnvironmentParameterResponseObject, error) {
	pattern := ""
	if req.Body.Pattern != nil {
		pattern = *req.Body.Pattern
	}
	if err := a.Environments.SetParameter(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), req.Body.Name, pattern, string(req.Body.Action) == "delete"); err != nil {
		return nil, err
	}
	return apigen.ChangeEnvironmentParameter204Response{}, nil
}
