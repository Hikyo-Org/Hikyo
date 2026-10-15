package server_test

import (
	"context"
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
