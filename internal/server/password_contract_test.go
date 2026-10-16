package server_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type passwordAuth struct {
	stubAuth
	password func(context.Context, string, string, string) (service.LoginResult, error)
}

func (s passwordAuth) SetPassword(ctx context.Context, presented, password, proof string) (service.LoginResult, error) {
	return s.password(ctx, presented, password, proof)
}

func TestSetPasswordForwardsOnlyCanonicalFieldsAndOptionalProof(t *testing.T) {
	for _, proof := range []string{"", "current-password"} {
		t.Run(proof, func(t *testing.T) {
			calls := 0
			auth := passwordAuth{stubAuth: stubAuth{identity: liveIdentityFn}, password: func(_ context.Context, presented, password, gotProof string) (service.LoginResult, error) {
				calls++
				if presented != "hik_1_cli_x" || password != "new-password-secret" || gotProof != proof {
					t.Errorf("wrong password mutation arguments")
				}
				return reissuedResult(service.ArtifactCLI), nil
			}}
			srv := newTestServer(t, auth, stubOrgs{})
			body := map[string]any{"password": "new-password-secret"}
			if proof != "" {
				body["proof"] = proof
			}
			resp, payload := call(t, srv, http.MethodPut, api.PathPrefix+"/auth/password", "hik_1_cli_x", body)
			if resp.StatusCode != http.StatusOK || calls != 1 {
				t.Fatalf("status=%d calls=%d body=%s", resp.StatusCode, calls, payload)
			}
			// The legacy spelling must not reach the service, or a client/server mismatch
			// could silently change the password to an empty string.
			resp, payload = call(t, srv, http.MethodPut, api.PathPrefix+"/auth/password", "hik_1_cli_x", map[string]any{"new_password": "new-password-secret"})
			if resp.StatusCode != http.StatusBadRequest || calls != 1 {
				t.Fatalf("legacy spelling status=%d calls=%d body=%s", resp.StatusCode, calls, payload)
			}
		})
	}
}

func TestSetPasswordCredentialRaceReturnsConflict(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"credential race", service.ErrCredentialRace},
		{"wrapped credential race", fmt.Errorf("private credential write context: %w", service.ErrCredentialRace)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			auth := passwordAuth{stubAuth: stubAuth{identity: liveIdentityFn}, password: func(context.Context, string, string, string) (service.LoginResult, error) {
				calls++
				return service.LoginResult{}, tc.err
			}}
			srv := newTestServer(t, auth, stubOrgs{})
			resp, payload := call(t, srv, http.MethodPut, api.PathPrefix+"/auth/password", "hik_1_cli_x", map[string]any{
				"password": "new-password-secret", "proof": "current-password",
			})
			if resp.StatusCode != http.StatusConflict || calls != 1 {
				t.Fatalf("status=%d calls=%d body=%s", resp.StatusCode, calls, payload)
			}
			failure := decodeError(t, payload)
			if failure.Error.Code != apigen.ErrorCodeConflict || failure.Error.Detail != nil || failure.Error.Message != "the current state of this resource refuses the request" {
				t.Fatalf("credential race must use the uniform conflict response: %s", payload)
			}
		})
	}
}

type credentialRaceAuth struct{ stubAuth }

func (credentialRaceAuth) StepUpPasskeyFinish(context.Context, string, []byte) (service.LoginResult, error) {
	return service.LoginResult{}, service.ErrCredentialRace
}

func (credentialRaceAuth) ReauthPasskeyFinish(context.Context, string, []byte) (service.ReauthResult, error) {
	return service.ReauthResult{}, service.ErrCredentialRace
}

func TestCredentialRaceSiblingResponsesMatchContract(t *testing.T) {
	auth := credentialRaceAuth{stubAuth{
		identity: liveIdentityFn,
		estab: func(context.Context, string, string) error {
			return service.ErrCredentialRace
		},
		reissue: func() (service.LoginResult, error) {
			return service.LoginResult{}, service.ErrCredentialRace
		},
	}}
	srv := newTestServer(t, auth, stubOrgs{})
	for _, tc := range []struct {
		path string
		body map[string]any
	}{
		{"/auth/credential/establish", map[string]any{"authority": "hik_1_bs_single-use-authority", "password": "new-password-secret"}},
		{"/auth/recovery-codes/regenerate", map[string]any{"proof": "current-password"}},
		{"/auth/login/challenge/lch_00000000-0000-0000-0000-000000000001/webauthn/finish", map[string]any{}},
		{"/auth/webauthn/step-up/finish", map[string]any{}},
		{"/auth/webauthn/reauth/finish", map[string]any{}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			resp, payload := call(t, srv, http.MethodPost, api.PathPrefix+tc.path, "hik_1_cli_x", tc.body)
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("status=%d body=%s", resp.StatusCode, payload)
			}
			failure := decodeError(t, payload)
			if failure.Error.Code != apigen.ErrorCodeConflict || failure.Error.Detail != nil {
				t.Fatalf("credential race must use the uniform conflict response: %s", payload)
			}
		})
	}
}

type unavailableCLIReauthAuth struct{ stubAuth }

func (unavailableCLIReauthAuth) StartCLIReauth(context.Context, string, service.ReauthIntent, string, string) (service.CLIReauthStart, error) {
	return service.CLIReauthStart{}, namedRefusal{detail: "this sign-in cannot satisfy step-up: add an authenticator or passkey under Settings > Security"}
}
func TestCLIReauthUnavailableKeepsNamedRemedy(t *testing.T) {
	srv := newTestServer(t, unavailableCLIReauthAuth{stubAuth{identity: liveIdentityFn}}, stubOrgs{})
	resp, payload := call(t, srv, http.MethodPost, api.PathPrefix+"/auth/cli-reauth/start", "hik_1_cli_x", map[string]any{
		"purpose": "adapter", "operation": "adapter.sync", "environment_ids": []string{testEnvID}, "pkce_challenge": strings.Repeat("a", 43), "redirect_uri": "http://127.0.0.1:12345/callback",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d payload=%s", resp.StatusCode, payload)
	}
	failure := decodeError(t, payload)
	if failure.Error.Code != apigen.ErrorCodeBadRequest || failure.Error.Detail == nil || !strings.Contains(*failure.Error.Detail, "Settings > Security") {
		t.Fatalf("named step-up remedy lost: %s", payload)
	}
}
