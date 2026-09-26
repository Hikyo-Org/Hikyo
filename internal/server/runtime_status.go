package server

import (
	"context"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type RuntimeStatusSource interface {
	RuntimeStatus(context.Context) (service.RuntimeStatus, error)
}

// NewMaintenance exposes only public availability and static UI. It constructs
// no tenant API, auth service, workspace directory, or MCP handler.
func NewMaintenance(source RuntimeStatusSource, ui fs.FS, options PublicOptions) http.Handler {
	options.MCP = nil
	assets := NewPublic(nil, nil, ui, options)
	a := &API{Runtime: source}
	return securityHeaders(options.HSTS)(boundPublicRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/api/v1/runtime/status" && r.Method == http.MethodGet {
			response, err := a.GetRuntimeStatus(r.Context(), apigen.GetRuntimeStatusRequestObject{})
			if err != nil {
				response = runtimeUnavailable()
			}
			_ = response.VisitGetRuntimeStatusResponse(w)
			return
		}
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp" || strings.HasPrefix(r.URL.Path, "/mcp/") {
			writeError(w, wirePolicyForCode(apigen.ErrorCodeServiceUnavailable), "")
			return
		}
		assets.ServeHTTP(w, r)
	})))
}

func (a *API) GetRuntimeStatus(ctx context.Context, _ apigen.GetRuntimeStatusRequestObject) (apigen.GetRuntimeStatusResponseObject, error) {
	if a.Admission != nil {
		if err := a.Admission.AdmitDiscovery(audit.FromContext(ctx).SourceIP); err != nil {
			return runtimeStatusResponse{apigen.GetRuntimeStatus429JSONResponse{TooManyRequestsJSONResponse: tooMany(err)}}, nil
		}
	}
	if a.Runtime == nil {
		return runtimeUnavailable(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	status, err := a.Runtime.RuntimeStatus(ctx)
	if err != nil {
		return runtimeUnavailable(), nil
	}
	out := apigen.RuntimeStatus{State: apigen.RuntimeStatusState(status.State)}
	if status.Phase != "" {
		phase := apigen.RuntimeStatusPhase(status.Phase)
		out.Phase = &phase
	}
	if !out.State.Valid() || (out.Phase != nil && (!out.Phase.Valid() || *out.Phase == apigen.RuntimeStatusPhaseLessThannil)) || (out.State != apigen.RuntimeStatusStateMaintenance && out.Phase != nil) || (out.State == apigen.RuntimeStatusStateMaintenance && out.Phase == nil) {
		return runtimeUnavailable(), nil
	}
	return runtimeStatusResponse{apigen.GetRuntimeStatus200JSONResponse(out)}, nil
}

type runtimeStatusResponse struct {
	apigen.GetRuntimeStatusResponseObject
}

func (r runtimeStatusResponse) VisitGetRuntimeStatusResponse(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "no-store")
	return r.GetRuntimeStatusResponseObject.VisitGetRuntimeStatusResponse(w)
}

func runtimeUnavailable() apigen.GetRuntimeStatusResponseObject {
	policy := wirePolicyForCode(apigen.ErrorCodeServiceUnavailable)
	return runtimeStatusResponse{apigen.GetRuntimeStatus503JSONResponse{ServiceUnavailableJSONResponse: apigen.ServiceUnavailableJSONResponse{
		Body:    policy.bodyWithDetail(""),
		Headers: apigen.ServiceUnavailableResponseHeaders{RetryAfter: 2},
	}}}
}
