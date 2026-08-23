package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestWirePolicyCoversEveryPublicErrorCode(t *testing.T) {
	doc, err := api.Doc()
	if err != nil {
		t.Fatal(err)
	}
	schema := doc.Components.Schemas["ErrorCode"]
	if schema == nil || schema.Value == nil {
		t.Fatal("OpenAPI ErrorCode schema is missing")
	}
	if len(schema.Value.Enum) != len(wirePolicies) {
		t.Fatalf("wire policy has %d entries for %d public codes", len(wirePolicies), len(schema.Value.Enum))
	}
	for _, raw := range schema.Value.Enum {
		value, ok := raw.(string)
		if !ok {
			t.Fatalf("OpenAPI ErrorCode member has type %T, want string", raw)
		}
		code := apigen.ErrorCode(value)
		policy, ok := wirePolicies[code]
		if !ok {
			t.Errorf("public error code %q has no wire policy", code)
			continue
		}
		if policy.code != code || policy.status == 0 || policy.message == "" {
			t.Errorf("public error code %q has incomplete wire policy: %+v", code, policy)
		}
	}
}

func TestWirePolicyCoversEveryRecognizedErrorClass(t *testing.T) {
	for i, rule := range wireErrorRules {
		got := wireErrorFor(fmt.Errorf("wrapped: %w", rule.match))
		if got.code != rule.code {
			t.Errorf("rule %d (%v) classified as %q, want %q", i, rule.match, got.code, rule.code)
		}
	}
}

func TestWirePolicyClassifiesWrappedErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   apigen.ErrorCode
	}{
		{"unauthenticated", domain.ErrUnauthenticated, http.StatusUnauthorized, apigen.ErrorCodeUnauthenticated},
		{"forbidden", domain.ErrUnauthorized, http.StatusForbidden, apigen.ErrorCodeForbidden},
		{"not found", domain.ErrNotFound, http.StatusNotFound, apigen.ErrorCodeNotFound},
		{"conflict", domain.ErrConflict, http.StatusConflict, apigen.ErrorCodeConflict},
		{"limit", domain.ErrLimitExceeded, http.StatusConflict, apigen.ErrorCodeLimitExceeded},
		{"invalid", domain.ErrInvalid, http.StatusBadRequest, apigen.ErrorCodeBadRequest},
		{"overloaded", admission.ErrOverloaded, http.StatusTooManyRequests, apigen.ErrorCodeTooManyRequests},
		{"reauth absent", service.ErrNoReauthWindow, http.StatusForbidden, apigen.ErrorCodeForbidden},
		{"reauth expired", service.ErrReauthWindowExpired, http.StatusForbidden, apigen.ErrorCodeForbidden},
		{"reauth mismatch", service.ErrReauthUnitMismatch, http.StatusForbidden, apigen.ErrorCodeForbidden},
		{"reauth spent", service.ErrReauthWindowSpent, http.StatusForbidden, apigen.ErrorCodeForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := wireErrorFor(fmt.Errorf("wrapped: %w", tc.err))
			if got.status != tc.status || got.code != tc.code {
				t.Fatalf("policy = {%d %q}, want {%d %q}", got.status, got.code, tc.status, tc.code)
			}
		})
	}
}

func TestWorkspaceHandoffInvalidHasOneExplicitLookupOverride(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", service.ErrHandoffInvalid)
	if got := wireErrorFor(err); got.code != apigen.ErrorCodeForbidden || got.status != http.StatusForbidden {
		t.Fatalf("generic handoff policy = {%d %q}, want 403 forbidden", got.status, got.code)
	}
	if got := workspaceHandoffLookupWireErrorFor(err); got.code != apigen.ErrorCodeNotFound || got.status != http.StatusNotFound {
		t.Fatalf("lookup handoff policy = {%d %q}, want 404 not_found", got.status, got.code)
	}
}

func TestWirePolicyFailsClosedForUnknownErrorsAndCodes(t *testing.T) {
	unknown := errors.New("private storage failure")
	for _, got := range []WireError{
		wireErrorFor(unknown),
		wireErrorFor(fmt.Errorf("wrapped: %w", unknown)),
		wirePolicyForCode(apigen.ErrorCode("future_public_code")),
	} {
		if got.status != http.StatusInternalServerError || got.code != apigen.ErrorCodeInternal {
			t.Errorf("unknown input policy = {%d %q}, want uniform internal", got.status, got.code)
		}
		body := got.bodyWithDetail("must not escape")
		if body.Error.Detail != nil || body.Error.Message != "internal error" {
			t.Errorf("unknown input body leaked detail: %+v", body)
		}
	}
}

func TestWirePolicyRedactsDetailUnlessExplicitlyAllowed(t *testing.T) {
	const detail = "caller-safe member"
	tests := []struct {
		name       string
		err        error
		wantDetail bool
	}{
		{"bad request", domain.ErrInvalid, true},
		{"conflict", domain.ErrConflict, true},
		{"unauthenticated", domain.ErrUnauthenticated, false},
		{"forbidden", domain.ErrUnauthorized, false},
		{"not found", domain.ErrNotFound, false},
		{"limit", domain.ErrLimitExceeded, false},
		{"overloaded", admission.ErrOverloaded, false},
		{"internal", errors.New("fault"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := wireErrorFor(tc.err).bodyWithDetail(detail)
			if tc.wantDetail && (body.Error.Detail == nil || *body.Error.Detail != detail) {
				t.Fatalf("detail = %v, want %q", body.Error.Detail, detail)
			}
			if !tc.wantDetail && body.Error.Detail != nil {
				t.Fatalf("detail = %q, want redacted", *body.Error.Detail)
			}
		})
	}
}

func renderContractError(t *testing.T, method, path string, err error) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	(&API{}).writeHandlerError(recorder, request, err)
	response := recorder.Result()
	if validationErr := api.ValidateResponse(request, response.StatusCode, response.Header, recorder.Body.Bytes()); validationErr != nil {
		t.Fatalf("%s %s -> %d violates contract: %v\nbody: %s", method, path, response.StatusCode, validationErr, recorder.Body.String())
	}
	return recorder
}

func TestCentralWirePoliciesFitMigratedOperationContracts(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		err    error
	}{
		{"TOTP already enrolled", http.MethodPost, "/api/v1/auth/totp/enrol/start", service.ErrTOTPAlreadyEnrolled},
		{"no pending TOTP", http.MethodPost, "/api/v1/auth/totp/enrol/confirm", service.ErrNoPendingTOTP},
		{"no TOTP factor", http.MethodPost, "/api/v1/auth/totp/step-up", service.ErrNoTOTPFactor},
		{"no proof credential", http.MethodDelete, "/api/v1/auth/totp", service.ErrNoProofCredential},
		{"passkey-only recovery floor", http.MethodPost, "/api/v1/auth/recovery/begin", service.ErrPasskeyOnlyViolation},
		{"TOTP account missing", http.MethodGet, "/api/v1/auth/totp", domain.ErrNotFound},
		{"TOTP status overloaded", http.MethodGet, "/api/v1/auth/totp", admission.ErrOverloaded},
		{"OIDC provider update raced", http.MethodPut, "/api/v1/instance/oidc-providers/corp", service.ErrProviderRace},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := wireErrorFor(tc.err)
			recorder := renderContractError(t, tc.method, tc.path, tc.err)
			if recorder.Code != wirePolicies[policy.code].status {
				t.Fatalf("status = %d, want central %d for %q", recorder.Code, wirePolicies[policy.code].status, policy.code)
			}
			if policy.code == apigen.ErrorCodeBadRequest {
				var body apigen.Error
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Detail != nil {
					t.Fatalf("routine TOTP refusal leaked detail %q", *body.Error.Detail)
				}
			}
		})
	}
}

func TestCentralConflictBodyIsIdenticalAcrossSAMLHandlers(t *testing.T) {
	provider := renderContractError(t, http.MethodPut, "/api/v1/instance/saml-providers/corp", service.ErrSAMLProviderRace)
	spKey := renderContractError(t, http.MethodPost, "/api/v1/instance/saml-sp-keys/rotate", service.ErrSAMLSPKeyRace)
	if provider.Code != http.StatusConflict || spKey.Code != http.StatusConflict {
		t.Fatalf("statuses = provider %d, SP key %d; want both 409", provider.Code, spKey.Code)
	}
	if provider.Body.String() != spKey.Body.String() {
		t.Fatalf("conflict bodies differ:\nprovider: %s\nSP key: %s", provider.Body.String(), spKey.Body.String())
	}
}

func TestPasswordPolicySentinelsCarryEstablishmentDetail(t *testing.T) {
	for _, refusal := range []error{service.ErrWeakPassword, service.ErrCommonPassword} {
		recorder := renderContractError(t, http.MethodPost, "/api/v1/auth/credential/establish", refusal)
		var body apigen.Error
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Detail == nil || *body.Error.Detail != "password" {
			t.Fatalf("%v detail = %v, want password", refusal, body.Error.Detail)
		}
	}
}
