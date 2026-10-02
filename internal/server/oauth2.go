package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type oauth2StartResponse struct {
	body    apigen.OidcStartResult
	cookies []*http.Cookie
}

func (r oauth2StartResponse) VisitOauth2StartResponse(w http.ResponseWriter) error {
	for _, cookie := range r.cookies {
		writeHTTPOnlyCookie(w, cookie)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(r.body)
}

// Oauth2Start forwards the purpose, login intent and optional sign-up org to the
// service. A successful anonymous start sets the browser-binding cookie; its
// expected refusals are rendered by oauth2StartError.
func (a *API) Oauth2Start(ctx context.Context, req apigen.Oauth2StartRequestObject) (apigen.Oauth2StartResponseObject, error) {
	if req.Body == nil {
		// A missing body folds into the uniform 401 like every other start
		// refusal: this endpoint never distinguishes its refusals on the wire.
		return apigen.Oauth2Start401JSONResponse{UnauthenticatedJSONResponse: apigen.UnauthenticatedJSONResponse(errorBody(apigen.ErrorCodeUnauthenticated, ""))}, nil
	}
	env, proof := "", ""
	if req.Body.EnvironmentId != nil {
		env = *req.Body.EnvironmentId
	}
	if req.Body.Proof != nil {
		proof = *req.Body.Proof
	}
	intent, signupOrg := "", strDeref(req.Body.SignupOrg)
	if req.Body.Intent != nil {
		intent = string(*req.Body.Intent)
	}
	browser := req.Body.Browser != nil && *req.Body.Browser
	result, err := a.Auth.OAuth2Start(ctx, string(req.Provider), string(req.Body.Purpose), intent, signupOrg, env, bearer(ctx), proof, browser)
	if err != nil {
		return oauth2StartError(a, ctx, err), nil
	}
	resp := oauth2StartResponse{body: apigen.OidcStartResult{AuthorizationUrl: result.AuthURL}}
	if result.BindingCookie != "" {
		resp.cookies = append(resp.cookies, &http.Cookie{
			Name: bindingCookieName(result.State), Value: result.BindingCookie,
			Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
	}
	if browser {
		resp.cookies = append(resp.cookies, oidcBrowserMarker(result.State, result.Purpose))
	}
	return resp, nil
}

// oauth2StartError renders an unsupported environment as 400 and a
// reauth attempt as 409. Other expected refusals become uniform 401 or
// shared 429 responses; unexpected faults become 500.
func oauth2StartError(a *API, ctx context.Context, err error) apigen.Oauth2StartResponseObject {
	// Every other expected start refusal collapses to one uniform 401 body:
	// an unknown or disabled slug, a bad purpose, and a reauth with no
	// environment all look identical to an unauthenticated link, so
	// a pre-auth prober cannot enumerate provider config by status (the
	// timing is uniform too : login admission runs before provider
	// resolution in the service).
	if errors.Is(err, service.ErrEnvironmentNotForPurpose) {
		return apigen.Oauth2Start400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse(errorBody(apigen.ErrorCodeBadRequest, "OAuth2 does not accept environment_id"))}
	}
	// OAuth2 cannot provide reauthentication evidence. Name the local-factor remedy.
	if errors.Is(err, service.ErrOAuth2Reauth) {
		return apigen.Oauth2Start409JSONResponse{ConflictJSONResponse: apigen.ConflictJSONResponse(errorBody(apigen.ErrorCodeConflict, err.Error()))}
	}
	policy := wireErrorFor(err)
	switch policy.code {
	case apigen.ErrorCodeTooManyRequests:
		return apigen.Oauth2Start429JSONResponse{TooManyRequestsJSONResponse: tooMany(err)}
	case apigen.ErrorCodeInternal:
		a.fault(ctx, "oauth2 start", err)
		return apigen.Oauth2Start500JSONResponse{InternalJSONResponse: apigen.InternalJSONResponse(errorBody(apigen.ErrorCodeInternal, ""))}
	default:
		return apigen.Oauth2Start401JSONResponse{UnauthenticatedJSONResponse: apigen.UnauthenticatedJSONResponse(errorBody(apigen.ErrorCodeUnauthenticated, ""))}
	}
}

func (a *API) Oauth2Callback(ctx context.Context, req apigen.Oauth2CallbackRequestObject) (apigen.Oauth2CallbackResponseObject, error) {
	code, state, iss, idpErr := strDeref(req.Params.Code), strDeref(req.Params.State), strDeref(req.Params.Iss), strDeref(req.Params.Error)
	bindingCookie, browserPurpose := "", ""
	if r := requestFrom(ctx); r != nil && state != "" {
		if c, err := r.Cookie(bindingCookieName(state)); err == nil {
			bindingCookie = c.Value
		}
		if c, cookieErr := r.Cookie(browserCookieName(state)); cookieErr == nil && validOIDCPurpose(c.Value) {
			browserPurpose = c.Value
		}
	}
	result, err := a.Auth.OAuth2Callback(ctx, string(req.Provider), code, state, iss, idpErr, bindingCookie, bearer(ctx))
	if !result.Browser && errors.Is(err, admission.ErrOverloaded) && browserPurpose != "" {
		result = service.OIDCCallbackResult{Browser: true, Purpose: browserPurpose, State: state}
	}
	if result.Browser {
		errorCode := ""
		if err != nil {
			policy := wireErrorFor(err)
			errorCode = string(policy.code)
			if policy.code == apigen.ErrorCodeInternal {
				a.fault(ctx, "oauth2 callback", err)
			}
		}
		return oauth2BrowserResponse{oidcBrowserCallbackResponse(result, errorCode)}, nil
	}
	if err != nil {
		// Every expected callback refusal is one uniform 401 body: a closed
		// reauth window, a wrong-purpose transaction (the dispatch default
		// returns ErrBadPurpose), an unknown/expired state, and every
		// oauth2_refused cause are indistinguishable on the wire, so a stolen or
		// observed state cannot be probed for the transaction's purpose or
		// lifecycle. Only a true fault is 500.
		policy := wireErrorFor(err)
		switch policy.code {
		case apigen.ErrorCodeTooManyRequests:
			return apigen.Oauth2Callback429JSONResponse{TooManyRequestsJSONResponse: tooMany(err)}, nil
		case apigen.ErrorCodeInternal:
			a.fault(ctx, "oauth2 callback", err)
			return apigen.Oauth2Callback500JSONResponse{InternalJSONResponse: apigen.InternalJSONResponse(errorBody(apigen.ErrorCodeInternal, ""))}, nil
		default:
			return apigen.Oauth2Callback401JSONResponse{UnauthenticatedJSONResponse: apigen.UnauthenticatedJSONResponse(errorBody(apigen.ErrorCodeUnauthenticated, ""))}, nil
		}
	}
	return oauth2SessionResponse{sessionResponse(result.Login)}, nil
}

type oauth2BrowserResponse struct{ oidcBrowserResponse }

func (r oauth2BrowserResponse) VisitOauth2CallbackResponse(w http.ResponseWriter) error {
	return r.VisitOidcCallbackResponse(w)
}
func oauth2ProviderViewWire(v service.OAuth2ProviderView) apigen.Oauth2Provider {
	return apigen.Oauth2Provider{
		Slug: v.Slug, DisplayName: v.DisplayName, Issuer: v.Issuer, ClientId: v.ClientID,
		Profile: apigen.Oauth2ProviderProfile(v.Profile), RedirectUri: v.RedirectURI,
		Enabled: v.Enabled,
	}
}

func (a *API) ListOauth2Providers(ctx context.Context, _ apigen.ListOauth2ProvidersRequestObject) (apigen.ListOauth2ProvidersResponseObject, error) {
	rows, err := a.OAuth2Providers.List(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	out := apigen.Oauth2ProviderList{Providers: make([]apigen.Oauth2Provider, 0, len(rows))}
	for _, v := range rows {
		out.Providers = append(out.Providers, oauth2ProviderViewWire(v))
	}
	return apigen.ListOauth2Providers200JSONResponse(out), nil
}

func (a *API) GetOauth2Provider(ctx context.Context, req apigen.GetOauth2ProviderRequestObject) (apigen.GetOauth2ProviderResponseObject, error) {
	v, err := a.OAuth2Providers.Get(ctx, service.Bearer(bearer(ctx)), string(req.Slug))
	if err != nil {
		return nil, err
	}
	return apigen.GetOauth2Provider200JSONResponse(oauth2ProviderViewWire(v)), nil
}

func (a *API) PutOauth2Provider(ctx context.Context, req apigen.PutOauth2ProviderRequestObject) (apigen.PutOauth2ProviderResponseObject, error) {
	if req.Body == nil {
		return apigen.PutOauth2Provider400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse(errorBody(apigen.ErrorCodeBadRequest, ""))}, nil
	}
	in := service.OAuth2ProviderInput{
		DisplayName: req.Body.DisplayName, Issuer: req.Body.Issuer, ClientID: req.Body.ClientId,
		ClientSecret: req.Body.ClientSecret, Profile: string(req.Body.Profile),
		Enabled: req.Body.Enabled,
	}
	v, err := a.OAuth2Providers.Put(ctx, service.Bearer(bearer(ctx)), string(req.Slug), in)
	if err != nil {
		return nil, err
	}
	return apigen.PutOauth2Provider200JSONResponse(oauth2ProviderViewWire(v)), nil
}

func (a *API) DeleteOauth2Provider(ctx context.Context, req apigen.DeleteOauth2ProviderRequestObject) (apigen.DeleteOauth2ProviderResponseObject, error) {
	err := a.OAuth2Providers.Delete(ctx, service.Bearer(bearer(ctx)), string(req.Slug))
	if err != nil {
		return nil, err
	}
	return apigen.DeleteOauth2Provider204Response{}, nil
}

type oauth2SessionResponse struct{ reissuedSessionResponse }

func (r oauth2SessionResponse) VisitOauth2CallbackResponse(w http.ResponseWriter) error {
	return r.VisitOidcCallbackResponse(w)
}
