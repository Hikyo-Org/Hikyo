package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type MailService interface {
	Configured(context.Context, service.Actor) (bool, error)
	Test(context.Context, service.Actor, string, string) error
}

func (a *API) SignupRequest(ctx context.Context, req apigen.SignupRequestRequestObject) (apigen.SignupRequestResponseObject, error) {
	if req.Body == nil {
		return nil, domain.ErrInvalid
	}
	if err := a.Auth.Signup(ctx, req.Body.Email, domain.OrgID(deref(req.Body.Org))); err != nil {
		return nil, err
	}
	return apigen.SignupRequest202Response{}, nil
}
func (a *API) SignupVerify(ctx context.Context, req apigen.SignupVerifyRequestObject) (apigen.SignupVerifyResponseObject, error) {
	if req.Body == nil {
		return nil, domain.ErrInvalid
	}
	in := service.SignupVerification{Token: req.Body.Token, Password: req.Body.Password, DisplayName: req.Body.DisplayName, OrgName: deref(req.Body.OrgName)}
	if req.Body.Landing != nil {
		in.Landing = string(*req.Body.Landing)
	}
	if err := a.Auth.VerifySignup(ctx, in); err != nil {
		return nil, err
	}
	return apigen.SignupVerify204Response{}, nil
}
func (a *API) GetInstanceMail(ctx context.Context, _ apigen.GetInstanceMailRequestObject) (apigen.GetInstanceMailResponseObject, error) {
	if a.Mail == nil {
		return nil, domain.ErrNotFound
	}
	configured, err := a.Mail.Configured(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	return apigen.GetInstanceMail200JSONResponse{Configured: configured}, nil
}
func (a *API) TestInstanceMail(ctx context.Context, req apigen.TestInstanceMailRequestObject) (apigen.TestInstanceMailResponseObject, error) {
	if a.Mail == nil {
		return nil, domain.ErrNotFound
	}
	if req.Body == nil {
		return nil, domain.ErrInvalid
	}
	if err := a.Mail.Test(ctx, service.Bearer(bearer(ctx)), req.Body.To, req.Body.Proof); err != nil {
		return nil, err
	}
	return apigen.TestInstanceMail204Response{}, nil
}
