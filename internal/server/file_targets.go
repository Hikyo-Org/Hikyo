package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// Generic file destinations (#164). The admin half rides the human session;
// the report rides the bound workload's machine credential through Delivery.

func fileTargetKeyIDs(in *[]apigen.ID) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(*in))
	for i, id := range *in {
		out[i] = string(id)
	}
	return out
}

func fileTargetSelection(in *apigen.AdapterKeySelection) service.AdapterKeySelection {
	if sel := keySelection(in); sel != nil {
		return *sel
	}
	return service.AdapterKeySelection{}
}

func fileTargetResponse(in service.FileTarget) apigen.FileTarget {
	keys := make([]apigen.FileTargetKey, 0, len(in.Keys))
	for _, k := range in.Keys {
		keys = append(keys, apigen.FileTargetKey{KeyId: apigen.ID(k.KeyID), Name: k.Name, Classification: apigen.FileTargetKeyClassification(k.Classification)})
	}
	out := apigen.FileTarget{
		Id: apigen.ID(in.ID), EnvironmentId: apigen.ID(in.EnvironmentID), Name: in.Name,
		ServiceAccountId: apigen.ID(in.ServiceAccountID), PrincipalId: apigen.ID(in.PrincipalID),
		Generation: in.Generation, CreatedAt: in.CreatedAt, UpdatedAt: in.UpdatedAt, Keys: keys,
	}
	if r := in.Report; r != nil {
		out.Report = &apigen.FileTargetReport{
			State: apigen.FileTargetState(r.State), Revision: r.Revision, Generation: r.Generation,
			Stamp: r.Stamp, ReportedAt: r.ReportedAt, ReceivedAt: r.ReceivedAt,
		}
	}
	return out
}

func (a *API) ListFileTargets(ctx context.Context, req apigen.ListFileTargetsRequestObject) (apigen.ListFileTargetsResponseObject, error) {
	targets, err := a.FileTargets.List(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project))
	if err != nil {
		return nil, err
	}
	items := make([]apigen.FileTarget, 0, len(targets))
	for _, t := range targets {
		items = append(items, fileTargetResponse(t))
	}
	return apigen.ListFileTargets200JSONResponse{Items: items}, nil
}

func (a *API) CreateFileTarget(ctx context.Context, req apigen.CreateFileTargetRequestObject) (apigen.CreateFileTargetResponseObject, error) {
	body := req.Body
	target, err := a.FileTargets.Create(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project), service.FileTargetInput{
		EnvironmentID: string(body.EnvironmentId), Name: body.Name, ServiceAccountID: string(body.ServiceAccountId),
		KeyIDs: fileTargetKeyIDs(body.KeyIds), KeySelection: fileTargetSelection(body.KeySelection),
	})
	if err != nil {
		return nil, err
	}
	return apigen.CreateFileTarget201JSONResponse(fileTargetResponse(target)), nil
}

func (a *API) GetFileTarget(ctx context.Context, req apigen.GetFileTargetRequestObject) (apigen.GetFileTargetResponseObject, error) {
	target, err := a.FileTargets.Get(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project), string(req.FileTarget))
	if err != nil {
		return nil, err
	}
	return apigen.GetFileTarget200JSONResponse(fileTargetResponse(target)), nil
}

func (a *API) UpdateFileTarget(ctx context.Context, req apigen.UpdateFileTargetRequestObject) (apigen.UpdateFileTargetResponseObject, error) {
	body := req.Body
	target, err := a.FileTargets.UpdateKeys(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project), string(req.FileTarget),
		body.ExpectedGeneration, fileTargetKeyIDs(body.KeyIds), fileTargetSelection(body.KeySelection))
	if err != nil {
		return nil, err
	}
	return apigen.UpdateFileTarget200JSONResponse(fileTargetResponse(target)), nil
}

func (a *API) DeleteFileTarget(ctx context.Context, req apigen.DeleteFileTargetRequestObject) (apigen.DeleteFileTargetResponseObject, error) {
	if err := a.FileTargets.Delete(ctx, service.Bearer(bearer(ctx)), adapterScope(req.Org, req.Project), string(req.FileTarget)); err != nil {
		return nil, err
	}
	return apigen.DeleteFileTarget204Response{}, nil
}

func (a *API) ReportFileTarget(ctx context.Context, req apigen.ReportFileTargetRequestObject) (apigen.ReportFileTargetResponseObject, error) {
	body := req.Body
	report := service.FileTargetReport{
		State: string(body.State), Revision: body.Revision, Generation: body.Generation, ReportedAt: body.ReportedAt,
	}
	if body.Stamp != nil {
		report.Stamp = *body.Stamp
	}
	if err := a.Delivery.ReportFileTarget(ctx, bearer(ctx), envScope(req.Org, req.Project, req.Environment), string(req.FileTarget), report); err != nil {
		return nil, err
	}
	return apigen.ReportFileTarget204Response{}, nil
}
