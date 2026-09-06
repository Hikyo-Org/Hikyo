package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/scimproto"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// WriteRuntimeUnavailable answers during the supervisor's brief global drain,
// before admission to any retiring graph. This instance-wide refusal does not
// inspect credentials or reveal whether a tenant/binding exists. Once routing
// resumes, SCIM refusals again rank behind normal binding authentication.
func WriteRuntimeUnavailable(w http.ResponseWriter, r *http.Request) {
	match, err := api.MatchRequest(r)
	if err == nil && api.IsSCIMWireOperation(match.Operation().ID) {
		w.Header().Set("Retry-After", "2")
		w.Header().Set("Content-Type", scimproto.MediaType)
		w.WriteHeader(http.StatusServiceUnavailable)
		// The body consists exclusively of fixed, serializable metadata.
		_ = json.NewEncoder(w).Encode((&scimproto.Error{Status: http.StatusServiceUnavailable, Detail: "Runtime configuration is not ready; retry shortly."}).Body())
		return
	}
	writeError(w, wireErrorFor(service.ErrSelfConfigUnavailable), "")
}

// requireCurrentRuntime admits business work only against the committed
// generation. The last usable graph retains a closed set of authentication
// and protected configuration operations so administrators can repair a failed
// activation. Matching and shape validation have already run exactly once.
func (a *API) requireCurrentRuntime(w http.ResponseWriter, r *http.Request) bool {
	if a.SelfConfig == nil {
		return true
	}
	if _, err := a.SelfConfig.Capture(r.Context()); err == nil {
		return true
	}
	op, ok := api.OperationFromContext(r.Context())
	if ok && runtimeRecoveryOperation(op.ID) {
		return true
	}
	if ok && runtimeRepairHierarchyOperation(op.ID) {
		scope := domain.Scope{Org: domain.OrgID(chi.URLParam(r, "org")), Project: domain.ProjectID(chi.URLParam(r, "project")), Env: domain.EnvID(chi.URLParam(r, "environment"))}
		if err := a.SelfConfig.AuthorizeRepairScope(r.Context(), service.Bearer(bearer(r.Context())), scope); err == nil {
			return true
		}
	}
	if ok && api.IsSCIMWireOperation(op.ID) {
		// Preserve the SCIM shape and its authentication-before-refusal rule.
		w.Header().Set("Retry-After", "2")
		a.writeSCIMRequestError(w, r, &scimproto.Error{Status: http.StatusServiceUnavailable, Detail: "Runtime configuration is not ready; retry shortly."})
		return false
	}
	a.writeHandlerError(w, r, service.ErrSelfConfigUnavailable)
	return false
}

func runtimeRecoveryOperation(id string) bool {
	switch id {
	case "getMeta", "authMethods", "localLogin", "logout", "whoami", "establishCredential",
		"enrolTotpStart", "enrolTotpConfirm", "stepUpTotp", "reauthTotp", "getTotpStatus",
		"passkeyLoginStart", "passkeyLoginFinish", "enrolPasskeyStart", "enrolPasskeyFinish",
		"stepUpPasskeyStart", "stepUpPasskeyFinish", "reauthPasskeyStart", "reauthPasskeyFinish", "listPasskeys",
		"startCLIReauth", "showCLIReauthTransaction", "approveCLIReauth", "redeemCLIReauth",
		"beginRecovery", "regenerateRecoveryCodes", "getMyProfile", "listMyOrgs", "listOrgs", "listMySessions", "revokeMySession",
		"getInstanceConfig", "previewInstanceConfigAdoption", "adoptInstanceConfig", "applyInstanceConfig", "testInstanceConfigMail",
		"listRemotes", "showRemote", "startWorkspaceHandoff", "approveWorkspaceHandoff", "redeemWorkspaceHandoff", "showWorkspaceHandoff":
		return true
	default:
		return false
	}
}

func runtimeRepairHierarchyOperation(id string) bool {
	switch id {
	case "getOrg", "listProjects", "getProject", "listEnvironments", "getEnvironment",
		"listKeys", "getKey", "listKeyGroups", "getKeyGroup", "listFolders", "getFolder",
		"listValues", "getValue", "setValue", "clearValue", "listPendingDrafts", "publishPendingChanges",
		"listRevisions", "getRevision", "rollbackRevision", "diffRevisions", "revealRevisionDiff",
		"getEnvironmentSignals", "getRevealWindow", "watchProjectEvents", "revealValue", "revealValues", "listValueOccurrences":
		return true
	default:
		return false
	}
}
