package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

const samlInitiatorCookieTTL = 10 * time.Minute

type samlStartResponse struct {
	body   apigen.SamlStartResult
	cookie *http.Cookie
}

func (r samlStartResponse) VisitSamlStartResponse(w http.ResponseWriter) error {
	if r.cookie != nil {
		writeHTTPOnlyCookie(w, r.cookie)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(r.body)
}

func (a *API) SamlStart(ctx context.Context, req apigen.SamlStartRequestObject) (apigen.SamlStartResponseObject, error) {
	if req.Body == nil {
		return samlStartUnauthenticated(), nil
	}
	environmentID, proof := "", ""
	if req.Body.EnvironmentId != nil {
		environmentID = *req.Body.EnvironmentId
	}
	if req.Body.Proof != nil {
		proof = *req.Body.Proof
	}
	result, err := a.SAMLAuth.SAMLStart(ctx, string(req.Provider), string(req.Body.Purpose), environmentID, bearer(ctx), proof)
	if err != nil {
		switch wireErrorFor(err).code {
		case apigen.ErrorCodeTooManyRequests:
			return apigen.SamlStart429JSONResponse{TooManyRequestsJSONResponse: tooMany()}, nil
		case apigen.ErrorCodeInternal:
			a.fault(ctx, "saml start", err)
			return apigen.SamlStart500JSONResponse{InternalJSONResponse: apigen.InternalJSONResponse(errorBody(apigen.ErrorCodeInternal, ""))}, nil
		default:
			return samlStartUnauthenticated(), nil
		}
	}
	redirect, err := url.Parse(result.RedirectURL)
	if err != nil {
		a.fault(ctx, "saml start redirect", err)
		return apigen.SamlStart500JSONResponse{InternalJSONResponse: apigen.InternalJSONResponse(errorBody(apigen.ErrorCodeInternal, ""))}, nil
	}
	relayState := redirect.Query().Get("RelayState")
	if relayState == "" || result.InitiatorCookie == "" {
		a.fault(ctx, "saml start binding", errors.New("service returned no SAML transaction binding"))
		return apigen.SamlStart500JSONResponse{InternalJSONResponse: apigen.InternalJSONResponse(errorBody(apigen.ErrorCodeInternal, ""))}, nil
	}
	path := samlACSPath(string(req.Provider))
	return samlStartResponse{
		body: apigen.SamlStartResult{RedirectUrl: result.RedirectURL},
		cookie: &http.Cookie{
			Name: samlBindingCookieName(string(req.Provider), relayState), Value: result.InitiatorCookie,
			Path: path, Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode,
			MaxAge: int(samlInitiatorCookieTTL.Seconds()), Expires: time.Now().Add(samlInitiatorCookieTTL),
		},
	}, nil
}

func samlStartUnauthenticated() apigen.SamlStartResponseObject {
	return apigen.SamlStart401JSONResponse{
		UnauthenticatedJSONResponse: apigen.UnauthenticatedJSONResponse(errorBody(apigen.ErrorCodeUnauthenticated, "")),
	}
}

type samlACSResponse struct {
	inner         apigen.SamlACSResponseObject
	clearCookie   *http.Cookie
	sessionCookie *http.Cookie
}

func (r samlACSResponse) VisitSamlACSResponse(w http.ResponseWriter) error {
	if r.clearCookie != nil {
		writeHTTPOnlyCookie(w, r.clearCookie)
	}
	if r.sessionCookie != nil {
		writeHTTPOnlyCookie(w, r.sessionCookie)
	}
	return r.inner.VisitSamlACSResponse(w)
}

func (a *API) SamlACS(ctx context.Context, req apigen.SamlACSRequestObject) (apigen.SamlACSResponseObject, error) {
	if req.Body == nil {
		return apigen.SamlACS400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse(errorBody(apigen.ErrorCodeBadRequest, ""))}, nil
	}
	provider, relayState := string(req.Provider), req.Body.RelayState
	path := samlACSPath(provider)
	clear := &http.Cookie{
		Name: samlBindingCookieName(provider, relayState), Value: "", Path: path,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode, MaxAge: -1, Expires: time.Unix(1, 0),
	}
	initiator := ""
	if request := requestFrom(ctx); request != nil {
		if cookie, err := request.Cookie(clear.Name); err == nil {
			initiator = cookie.Value
		}
	}
	result, err := a.SAMLAuth.SAMLACS(ctx, provider, req.Body.SAMLResponse, relayState, initiator)
	if err != nil {
		var inner apigen.SamlACSResponseObject
		switch wireErrorFor(err).code {
		case apigen.ErrorCodeTooManyRequests:
			inner = apigen.SamlACS429JSONResponse{TooManyRequestsJSONResponse: tooMany()}
		case apigen.ErrorCodeInternal:
			a.fault(ctx, "saml acs", err)
			inner = apigen.SamlACS500JSONResponse{InternalJSONResponse: apigen.InternalJSONResponse(errorBody(apigen.ErrorCodeInternal, ""))}
		default:
			inner = apigen.SamlACS401JSONResponse{UnauthenticatedJSONResponse: apigen.UnauthenticatedJSONResponse(errorBody(apigen.ErrorCodeUnauthenticated, ""))}
		}
		return samlACSResponse{inner: inner, clearCookie: clear}, nil
	}
	response := samlACSResponse{inner: apigen.SamlACS200JSONResponse(loginResultOf(result)), clearCookie: clear}
	if result.Artifact == service.ArtifactBrowser && result.SessionToken != "" {
		response.sessionCookie = &http.Cookie{
			Name: browserSessionCookie, Value: result.SessionToken,
			Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		}
	}
	return response, nil
}

func (a *API) SamlMetadata(ctx context.Context, req apigen.SamlMetadataRequestObject) (apigen.SamlMetadataResponseObject, error) {
	payload, err := a.SAMLAuth.SAMLMetadata(ctx, string(req.Provider))
	if err != nil {
		return nil, err
	}
	return apigen.SamlMetadata200ApplicationsamlmetadataXmlResponse{
		Body: bytes.NewReader(payload), ContentLength: int64(len(payload)),
	}, nil
}

func samlACSPath(provider string) string {
	return ContractPrefix + "/auth/saml/" + provider + "/acs"
}

func samlBindingCookieName(provider, relayState string) string {
	digest := sha256.Sum256([]byte(relayState))
	return "__Secure-hikyo-saml-tx-" + provider + "-" + hex.EncodeToString(digest[:8])
}

func samlProviderWire(v service.SAMLProviderView) apigen.SamlProvider {
	warnings := make([]apigen.SamlProviderWarning, 0, len(v.Warnings))
	for _, warning := range v.Warnings {
		warnings = append(warnings, apigen.SamlProviderWarning{
			Code: apigen.SamlProviderWarningCode(warning.Code), Severity: apigen.SamlProviderWarningSeverity(warning.Severity),
			Message: warning.Message, EffectiveAt: warning.EffectiveAt, Fingerprint: warning.Fingerprint,
		})
	}
	return apigen.SamlProvider{
		Slug: v.Slug, DisplayName: v.DisplayName, Kind: apigen.SamlProviderKindSaml,
		EntityId: v.EntityID, AcsUrl: v.ACSURL, SsoRedirectUrl: v.SSORedirectURL,
		SigningCertificateFingerprints: v.SigningCertificateFingerprints,
		AssurancePolicy:                v.AssurancePolicy, AllowEmailNameid: v.AllowEmailNameID,
		ForceSignRequests: v.ForceSignRequests, MetadataSource: apigen.SamlMetadataSource(v.MetadataSource),
		MetadataUrl: v.MetadataURL, MetadataSigned: v.MetadataSigned,
		MetadataSigningFingerprint: v.MetadataSigningFingerprint,
		MetadataValidUntil:         v.MetadataValidUntil, Warnings: warnings, Enabled: v.Enabled,
		RowVersion: int(v.RowVersion), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func samlMutationWire(result service.SAMLProviderMutationResult) apigen.SamlProviderMutationResult {
	out := apigen.SamlProviderMutationResult{
		Applied: result.Applied,
		Diff: apigen.SamlMetadataDiff{
			EndpointsAdded: result.Diff.EndpointsAdded, EndpointsRemoved: result.Diff.EndpointsRemoved,
			CertsAddedFps: result.Diff.CertsAddedFps, CertsRemovedFps: result.Diff.CertsRemovedFps,
			ValidUntil: result.Diff.ValidUntil,
		},
		RequiredFingerprints: result.RequiredFingerprints, RequiredEndpoints: result.RequiredEndpoints,
	}
	if result.Provider != nil {
		provider := samlProviderWire(*result.Provider)
		out.Provider = &provider
	}
	return out
}

func samlSPKeyWire(key service.SAMLSPKeyView) apigen.SamlSpKey {
	return apigen.SamlSpKey{
		Fingerprint: key.Fingerprint, State: apigen.SamlSpKeyState(key.State), CreatedAt: key.CreatedAt,
	}
}

func (a *API) ListSamlSpKeys(ctx context.Context, _ apigen.ListSamlSpKeysRequestObject) (apigen.ListSamlSpKeysResponseObject, error) {
	keys, err := a.SAMLProviders.ListSPKeys(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	out := apigen.SamlSpKeyList{Keys: make([]apigen.SamlSpKey, 0, len(keys))}
	for _, key := range keys {
		out.Keys = append(out.Keys, samlSPKeyWire(key))
	}
	return apigen.ListSamlSpKeys200JSONResponse(out), nil
}

func (a *API) RotateSamlSpKey(ctx context.Context, _ apigen.RotateSamlSpKeyRequestObject) (apigen.RotateSamlSpKeyResponseObject, error) {
	key, err := a.SAMLProviders.RotateSPKey(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	return apigen.RotateSamlSpKey200JSONResponse(samlSPKeyWire(key)), nil
}

func (a *API) RetireSamlSpKey(ctx context.Context, req apigen.RetireSamlSpKeyRequestObject) (apigen.RetireSamlSpKeyResponseObject, error) {
	err := a.SAMLProviders.RetireSPKey(ctx, service.Bearer(bearer(ctx)), req.Fingerprint)
	if err != nil {
		return nil, err
	}
	return apigen.RetireSamlSpKey204Response{}, nil
}

func (a *API) CompromiseRetireSamlSpKey(ctx context.Context, req apigen.CompromiseRetireSamlSpKeyRequestObject) (apigen.CompromiseRetireSamlSpKeyResponseObject, error) {
	key, err := a.SAMLProviders.CompromiseRetireSPKey(ctx, service.Bearer(bearer(ctx)), req.Fingerprint)
	if err != nil {
		return nil, err
	}
	return apigen.CompromiseRetireSamlSpKey200JSONResponse(samlSPKeyWire(key)), nil
}

func (a *API) ListSamlProviders(ctx context.Context, _ apigen.ListSamlProvidersRequestObject) (apigen.ListSamlProvidersResponseObject, error) {
	rows, err := a.SAMLProviders.List(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	out := apigen.SamlProviderList{Providers: make([]apigen.SamlProvider, 0, len(rows))}
	for _, row := range rows {
		out.Providers = append(out.Providers, samlProviderWire(row))
	}
	return apigen.ListSamlProviders200JSONResponse(out), nil
}

func (a *API) GetSamlProvider(ctx context.Context, req apigen.GetSamlProviderRequestObject) (apigen.GetSamlProviderResponseObject, error) {
	view, err := a.SAMLProviders.Get(ctx, service.Bearer(bearer(ctx)), string(req.Slug))
	if err != nil {
		return nil, err
	}
	return apigen.GetSamlProvider200JSONResponse(samlProviderWire(view)), nil
}

func (a *API) PutSamlProvider(ctx context.Context, req apigen.PutSamlProviderRequestObject) (apigen.PutSamlProviderResponseObject, error) {
	if req.Body == nil {
		return apigen.PutSamlProvider400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse(errorBody(apigen.ErrorCodeBadRequest, ""))}, nil
	}
	document := []byte(nil)
	if req.Body.MetadataDocument != nil {
		document = []byte(*req.Body.MetadataDocument)
	}
	result, err := a.SAMLProviders.Put(ctx, service.Bearer(bearer(ctx)), string(req.Slug), service.SAMLProviderInput{
		DisplayName: req.Body.DisplayName, EntityID: req.Body.EntityId,
		MetadataSource: string(req.Body.MetadataSource), MetadataDocument: document, MetadataURL: req.Body.MetadataUrl,
		AssurancePolicy: req.Body.AssurancePolicy, AllowEmailNameID: req.Body.AllowEmailNameid,
		ForceSignRequests: req.Body.ForceSignRequests, Enabled: req.Body.Enabled,
		ConfirmedFingerprints: sliceDeref(req.Body.ConfirmedFingerprints), ConfirmedEndpoints: sliceDeref(req.Body.ConfirmedEndpoints),
	})
	if err != nil {
		return nil, err
	}
	return apigen.PutSamlProvider200JSONResponse(samlMutationWire(result)), nil
}

func (a *API) PatchSamlProvider(ctx context.Context, req apigen.PatchSamlProviderRequestObject) (apigen.PatchSamlProviderResponseObject, error) {
	if req.Body == nil {
		return apigen.PatchSamlProvider400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse(errorBody(apigen.ErrorCodeBadRequest, ""))}, nil
	}
	view, err := a.SAMLProviders.Patch(ctx, service.Bearer(bearer(ctx)), string(req.Slug), service.SAMLProviderPatch{
		DisplayName: req.Body.DisplayName, AssurancePolicy: req.Body.AssurancePolicy,
		AllowEmailNameID: req.Body.AllowEmailNameid, ForceSignRequests: req.Body.ForceSignRequests, Enabled: req.Body.Enabled,
	})
	if err != nil {
		return nil, err
	}
	return apigen.PatchSamlProvider200JSONResponse(samlProviderWire(view)), nil
}

func (a *API) DeleteSamlProvider(ctx context.Context, req apigen.DeleteSamlProviderRequestObject) (apigen.DeleteSamlProviderResponseObject, error) {
	err := a.SAMLProviders.Delete(ctx, service.Bearer(bearer(ctx)), string(req.Slug))
	if err != nil {
		return nil, err
	}
	return apigen.DeleteSamlProvider204Response{}, nil
}

func (a *API) RefreshSamlProviderMetadata(ctx context.Context, req apigen.RefreshSamlProviderMetadataRequestObject) (apigen.RefreshSamlProviderMetadataResponseObject, error) {
	if req.Body == nil {
		return apigen.RefreshSamlProviderMetadata400JSONResponse{BadRequestJSONResponse: apigen.BadRequestJSONResponse(errorBody(apigen.ErrorCodeBadRequest, ""))}, nil
	}
	document := []byte(nil)
	if req.Body.MetadataDocument != nil {
		document = []byte(*req.Body.MetadataDocument)
	}
	result, err := a.SAMLProviders.RefreshMetadata(ctx, service.Bearer(bearer(ctx)), string(req.Slug), service.SAMLMetadataRefreshInput{
		MetadataDocument: document, ConfirmedFingerprints: sliceDeref(req.Body.ConfirmedFingerprints),
		ConfirmedEndpoints: sliceDeref(req.Body.ConfirmedEndpoints),
	})
	if err != nil {
		return nil, err
	}
	return apigen.RefreshSamlProviderMetadata200JSONResponse(samlMutationWire(result)), nil
}

func sliceDeref(values *[]string) []string {
	if values == nil {
		return nil
	}
	return *values
}
