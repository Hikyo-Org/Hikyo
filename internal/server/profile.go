package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// profileResponse maps the service profile to its public representation.
func profileResponse(p service.AccountProfile) apigen.AccountProfile {
	return apigen.AccountProfile{Username: p.Username, DisplayName: p.DisplayName, Email: p.Email, EmailVerified: p.EmailVerified, Managed: p.Managed, UsernameEditable: p.UsernameEditable}
}

// GetMyProfile returns the authenticated account profile over the API.
func (a *API) GetMyProfile(ctx context.Context, _ apigen.GetMyProfileRequestObject) (apigen.GetMyProfileResponseObject, error) {
	profile, err := a.Auth.MyProfile(ctx, bearer(ctx))
	if err != nil {
		return nil, err
	}
	return apigen.GetMyProfile200JSONResponse(profileResponse(profile)), nil
}

// UpdateMyProfile applies editable names without accepting email changes.
func (a *API) UpdateMyProfile(ctx context.Context, req apigen.UpdateMyProfileRequestObject) (apigen.UpdateMyProfileResponseObject, error) {
	profile, err := a.Auth.UpdateMyProfile(ctx, bearer(ctx), service.ProfileUpdate{Username: req.Body.Username, DisplayName: req.Body.DisplayName}, deref(req.Body.Proof))
	if err != nil {
		return nil, err
	}
	return apigen.UpdateMyProfile200JSONResponse(profileResponse(profile)), nil
}
