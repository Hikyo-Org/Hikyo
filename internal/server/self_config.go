package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func (a *API) GetInstanceConfig(ctx context.Context, _ apigen.GetInstanceConfigRequestObject) (apigen.GetInstanceConfigResponseObject, error) {
	if a.SelfConfig == nil {
		return nil, domain.ErrNotFound
	}
	status, err := a.SelfConfig.Status(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	return apigen.GetInstanceConfig200JSONResponse(wireSelfConfig(status)), nil
}

func (a *API) PreviewInstanceConfigAdoption(ctx context.Context, _ apigen.PreviewInstanceConfigAdoptionRequestObject) (apigen.PreviewInstanceConfigAdoptionResponseObject, error) {
	if a.SelfConfig == nil {
		return nil, domain.ErrNotFound
	}
	preview, err := a.SelfConfig.PreviewAdoption(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	return apigen.PreviewInstanceConfigAdoption200JSONResponse{OwnerInstanceId: preview.OwnerInstanceID, SchemaVersion: preview.SchemaVersion, ConfiguredKeys: preview.ConfiguredKeys, Warnings: preview.Warnings, PreviewToken: preview.PreviewToken}, nil
}

func (a *API) AdoptInstanceConfig(ctx context.Context, req apigen.AdoptInstanceConfigRequestObject) (apigen.AdoptInstanceConfigResponseObject, error) {
	if a.SelfConfig == nil {
		return nil, domain.ErrNotFound
	}
	if req.Body == nil {
		return nil, domain.ErrInvalid
	}
	status, err := a.SelfConfig.Adopt(ctx, service.Bearer(bearer(ctx)), service.SelfConfigAdoptRequest{PreviewToken: req.Body.PreviewToken, IdempotencyKey: req.Body.IdempotencyKey})
	if err != nil {
		return nil, err
	}
	return apigen.AdoptInstanceConfig200JSONResponse(wireSelfConfig(status)), nil
}

func (a *API) ApplyInstanceConfig(ctx context.Context, req apigen.ApplyInstanceConfigRequestObject) (apigen.ApplyInstanceConfigResponseObject, error) {
	if a.SelfConfig == nil {
		return nil, domain.ErrNotFound
	}
	if req.Body == nil {
		return nil, domain.ErrInvalid
	}
	status, err := a.SelfConfig.Apply(ctx, service.Bearer(bearer(ctx)), service.SelfConfigApplyRequest{Revision: req.Body.Revision, SchemaVersion: req.Body.SchemaVersion, ExpectedGeneration: req.Body.ExpectedGeneration, IdempotencyKey: req.Body.IdempotencyKey, ConfirmRestoredCredentials: req.Body.ConfirmRestoredCredentials})
	if err != nil {
		return nil, err
	}
	return apigen.ApplyInstanceConfig202JSONResponse(wireSelfConfig(status)), nil
}

func (a *API) TestInstanceConfigMail(ctx context.Context, req apigen.TestInstanceConfigMailRequestObject) (apigen.TestInstanceConfigMailResponseObject, error) {
	if a.SelfConfig == nil {
		return nil, domain.ErrNotFound
	}
	if req.Body == nil {
		return nil, domain.ErrInvalid
	}
	result, err := a.SelfConfig.TestMail(ctx, service.Bearer(bearer(ctx)), service.SelfConfigMailTestRequest{Revision: req.Body.Revision, SchemaVersion: req.Body.SchemaVersion, ExpectedGeneration: req.Body.ExpectedGeneration, To: string(req.Body.To)})
	if err != nil {
		return nil, err
	}
	return apigen.TestInstanceConfigMail200JSONResponse{Revision: result.Revision, Sent: result.Sent}, nil
}

func wireSelfConfig(status service.SelfConfigStatus) apigen.InstanceConfigStatus {
	out := apigen.InstanceConfigStatus{OwnerInstanceId: status.OwnerInstanceID, Managed: status.Managed, Generation: status.Generation, DesiredRevision: status.DesiredRevision, LatestRevision: status.LatestRevision, State: apigen.InstanceConfigStatusState(status.State), Nodes: []apigen.InstanceConfigNode{}}
	if b := status.Binding; b != nil {
		out.Binding = &apigen.InstanceConfigBinding{OrgId: b.OrgID, ProjectId: b.ProjectID, EnvironmentId: b.EnvironmentID, SchemaVersion: b.SchemaVersion}
	}
	for _, node := range status.Nodes {
		wire := apigen.InstanceConfigNode{NodeId: node.NodeID, ActiveGeneration: node.ActiveGeneration, ActiveRevision: node.ActiveRevision, State: apigen.InstanceConfigNodeState(node.State), UpdatedAt: node.UpdatedAt}
		if node.Error != "" {
			wire.Error = &node.Error
		}
		out.Nodes = append(out.Nodes, wire)
	}
	if j := status.Job; j != nil {
		out.Job = &apigen.InstanceConfigJob{Id: j.ID, State: apigen.InstanceConfigJobState(j.State), Revision: j.Revision, Generation: j.Generation, CreatedAt: j.CreatedAt, CompletedAt: j.CompletedAt}
		if j.Error != "" {
			out.Job.Error = &j.Error
		}
	}
	return out
}
