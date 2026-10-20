package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func wireProjectCapacity(capacity service.ProjectCapacity) apigen.ProjectCapacity {
	return apigen.ProjectCapacity{
		KeyLimitOverride: capacity.KeyLimitOverride,
		InstanceDefault:  capacity.InstanceDefault, InstanceMaximum: capacity.InstanceMaximum,
		EffectiveLimit: capacity.EffectiveLimit, KeyCount: int(capacity.KeyCount),
		EnvironmentCount: int(capacity.EnvironmentCount), Revision: capacity.Revision,
		Inherited: capacity.Inherited, Capped: capacity.Capped,
	}
}

func (a *API) GetProjectCapacity(ctx context.Context, req apigen.GetProjectCapacityRequestObject) (apigen.GetProjectCapacityResponseObject, error) {
	capacity, err := a.Settings.GetCapacity(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project))
	if err != nil {
		return nil, err
	}
	return apigen.GetProjectCapacity200JSONResponse(wireProjectCapacity(capacity)), nil
}

func (a *API) SetProjectCapacity(ctx context.Context, req apigen.SetProjectCapacityRequestObject) (apigen.SetProjectCapacityResponseObject, error) {
	capacity, err := a.Settings.SetCapacity(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project), service.CapacityUpdate{
		KeyLimitOverride: req.Body.KeyLimitOverride, ExpectedRevision: req.Body.ExpectedRevision,
		ExpectedInstanceDefault: req.Body.ExpectedInstanceDefault, ExpectedInstanceMaximum: req.Body.ExpectedInstanceMaximum,
	})
	if err != nil {
		return nil, err
	}
	return apigen.SetProjectCapacity200JSONResponse(wireProjectCapacity(capacity)), nil
}
