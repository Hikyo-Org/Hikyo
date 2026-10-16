package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func (a *API) StartCLILogin(ctx context.Context, req apigen.StartCLILoginRequestObject) (apigen.StartCLILoginResponseObject, error) {
	release, err := a.enterWorkspaceAdmission(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	result, err := a.Auth.StartCLILogin(ctx, string(req.Body.Transport), req.Body.State, req.Body.PkceChallenge, req.Body.RedirectUri)
	if err != nil {
		return nil, err
	}
	return apigen.StartCLILogin201JSONResponse{State: result.State, DeviceCode: result.DeviceCode, UserCode: result.UserCode, VerificationUri: result.VerificationURI, ExpiresAt: result.ExpiresAt, Interval: result.Interval}, nil
}
func (a *API) ShowCLILoginTransaction(ctx context.Context, req apigen.ShowCLILoginTransactionRequestObject) (apigen.ShowCLILoginTransactionResponseObject, error) {
	release, err := a.enterWorkspaceAdmission(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	result, err := a.Auth.CLILoginTransaction(ctx, service.Bearer(bearer(ctx)), req.Reference)
	if err != nil {
		return nil, err
	}
	return apigen.ShowCLILoginTransaction200JSONResponse{Reference: result.Reference, Transport: apigen.CLILoginTransactionTransport(result.Transport), RedirectUri: result.RedirectURI, Instance: result.Instance, ExpiresAt: result.ExpiresAt}, nil
}
func (a *API) ApproveCLILogin(ctx context.Context, req apigen.ApproveCLILoginRequestObject) (apigen.ApproveCLILoginResponseObject, error) {
	release, err := a.enterWorkspaceAdmission(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	result, err := a.Auth.ApproveCLILogin(ctx, service.Bearer(bearer(ctx)), req.Body.Reference)
	if err != nil {
		return nil, err
	}
	return apigen.ApproveCLILogin200JSONResponse{Code: result.Code, State: result.State, RedirectUri: result.RedirectURI}, nil
}
func (a *API) RedeemCLILogin(ctx context.Context, req apigen.RedeemCLILoginRequestObject) (apigen.RedeemCLILoginResponseObject, error) {
	release, err := a.enterWorkspaceAdmission(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	result, err := a.Auth.RedeemCLILogin(ctx, req.Body.Code, req.Body.PkceVerifier, req.Body.RedirectUri)
	if err != nil {
		return nil, err
	}
	return apigen.RedeemCLILogin200JSONResponse(loginResultOf(result)), nil
}
func (a *API) PollCLIDevice(ctx context.Context, req apigen.PollCLIDeviceRequestObject) (apigen.PollCLIDeviceResponseObject, error) {
	release, err := a.enterWorkspaceAdmission(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	result, err := a.Auth.PollCLIDevice(ctx, req.Body.DeviceCode)
	if err != nil {
		return nil, err
	}
	if result.Status != "" {
		var body apigen.CLIDeviceErrorResponse
		if err := body.FromCLIDeviceError(apigen.CLIDeviceError{Error: apigen.CLIDeviceErrorError(result.Status), Interval: result.Interval}); err != nil {
			return nil, err
		}
		return apigen.PollCLIDevice400JSONResponse(body), nil
	}
	login := loginResultOf(result.Login)
	return apigen.PollCLIDevice200JSONResponse{AccessToken: result.Login.SessionToken, TokenType: apigen.Bearer, ExpiresIn: int(result.Login.AbsExpires.Sub(result.Login.CreatedAt).Seconds()), Session: login.Session, Principal: login.Principal}, nil
}

func (a *API) StartCLIDevice(ctx context.Context, req apigen.StartCLIDeviceRequestObject) (apigen.StartCLIDeviceResponseObject, error) {
	release, err := a.enterWorkspaceAdmission(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	result, err := a.Auth.StartCLILogin(ctx, "device", "", "", "")
	if err != nil {
		return nil, err
	}
	return apigen.StartCLIDevice200JSONResponse{DeviceCode: result.DeviceCode, UserCode: result.UserCode, VerificationUri: result.VerificationURI, ExpiresAt: result.ExpiresAt, ExpiresIn: int(service.CLIDeviceLifetime.Seconds()), Interval: result.Interval}, nil
}

// RFC 8628 scalar form parameters must be unambiguous. The generic form
// decoder accepts the first value of a repeated key; refusing duplicates here
// prevents it and the schema validator from interpreting different requests.
// The shared validator retains required-field, enum and unknown-member checks.
func validateCLIDeviceForm(r *http.Request) error {
	invalid := errors.New("device authorization requires one value per form parameter")
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
		return &api.ValidationError{Err: invalid}
	}
	if r.Body == nil {
		return &api.ValidationError{Err: invalid}
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return &api.ValidationError{Err: invalid}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	values, err := url.ParseQuery(string(raw))
	if err != nil {
		return &api.ValidationError{Err: invalid}
	}
	for name, value := range values {
		if len(value) != 1 {
			return &api.ValidationError{Member: name, Err: invalid}
		}
	}
	return nil
}
