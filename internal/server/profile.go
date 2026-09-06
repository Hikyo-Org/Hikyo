package server

import (
	"context"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func profileResponse(p service.AccountProfile) apigen.AccountProfile {
	return apigen.AccountProfile{Username: p.Username, DisplayName: p.DisplayName, Email: p.Email, Managed: p.Managed, UsernameEditable: p.UsernameEditable}
}
func (a *API) GetMyProfile(ctx context.Context, _ apigen.GetMyProfileRequestObject) (apigen.GetMyProfileResponseObject, error) {
	profile, err := a.Auth.MyProfile(ctx, bearer(ctx))
	if err != nil {
		return nil, err
	}
	return apigen.GetMyProfile200JSONResponse(profileResponse(profile)), nil
}
func (a *API) UpdateMyProfile(ctx context.Context, req apigen.UpdateMyProfileRequestObject) (apigen.UpdateMyProfileResponseObject, error) {
	profile, err := a.Auth.UpdateMyProfile(ctx, bearer(ctx), service.AccountProfile{Username: req.Body.Username, DisplayName: req.Body.DisplayName, Email: req.Body.Email}, deref(req.Body.Proof))
	if err != nil {
		return nil, err
	}
	return apigen.UpdateMyProfile200JSONResponse(profileResponse(profile)), nil
}
