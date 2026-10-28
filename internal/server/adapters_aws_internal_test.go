package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestAWSAdapterTargetMutationKeepsKMSRouteAndMoveStatus(t *testing.T) {
	for _, kind := range []string{"json-object", "per-key"} {
		for _, move := range []bool{false, true} {
			name := kind + "/updated"
			if move {
				name = kind + "/move"
			}
			t.Run(name, func(t *testing.T) {
				keep := true
				body := apigen.UpdateAdapterTargetRequest{
					EnvironmentId: "env_one", DestinationKind: apigen.AdapterDestinationKind(kind),
					DestinationOwner: "123456789012", DestinationName: "prod/config/",
					DestinationEnvironment: "alias/next", NamePrefix: "PROD_",
					KeyIds: []apigen.ID{"key_one"}, ExpectedGeneration: 7, KeepRemote: &keep,
				}
				stub := &recordingTargetMutationService{result: service.TargetMutationUpdated{Target: service.AdapterTarget{ID: "tgt_one", DestinationKind: kind, DestinationEnvironment: "alias/next"}}}
				wantStatus := http.StatusOK
				if move {
					stub.result = service.TargetMutationMoveStarted{}
					wantStatus = http.StatusAccepted
				}
				response, err := updateAdapterTarget(withBearer(t.Context(), "bearer"), stub, apigen.UpdateAdapterTargetRequestObject{Org: "org_one", Project: "prj_one", Target: "tgt_one", Body: &body})
				if err != nil {
					t.Fatal(err)
				}
				input := stub.request.Target
				if input.DestinationKind != kind || input.DestinationOwner != "123456789012" || input.DestinationName != "prod/config/" || input.DestinationEnvironment != "alias/next" || input.NamePrefix != "PROD_" || len(input.KeyIDs) != 1 || input.KeyIDs[0] != "key_one" || stub.request.ExpectedGeneration != 7 || !stub.keepRemote {
					t.Fatalf("AWS mutation lost route, KMS, generation or retention: %+v", stub.request)
				}
				recorder := httptest.NewRecorder()
				if err := response.VisitUpdateAdapterTargetResponse(recorder); err != nil {
					t.Fatal(err)
				}
				if recorder.Code != wantStatus {
					t.Fatalf("status=%d want %d", recorder.Code, wantStatus)
				}
				if !move {
					var target apigen.AdapterTarget
					if err := json.Unmarshal(recorder.Body.Bytes(), &target); err != nil {
						t.Fatal(err)
					}
					if string(target.DestinationKind) != kind || target.DestinationEnvironment != "alias/next" {
						t.Fatal("AWS target response lost kind or customer KMS route")
					}
				}
			})
		}
	}
}

// Use actual provider parser refusals through the central API error renderer.
// Construction refuses before credential discovery or any provider request.
// This is renderer coverage, not endpoint dispatch: credential replacement
// seals the descriptor without constructing a provider; its auth/config error
// appears on a later connection test or sync, not necessarily on the PUT.
func TestAWSDescriptorRefusalsExposeOnlySafeDetail(t *testing.T) {
	for _, credential := range []string{
		`{"mode":"static","access_key_id":"AKIAHIKYOTEST0000001","secret_access_key":"aws-api-secret-canary","unexpected":"aws-api-private-canary"}`,
		`{"mode":"static","access_key_id":"AKIAHIKYOTEST0000001","secret_access_key":"aws-api-secret-canary"}]`,
		`{"mode":"ambient"}`,
		`{"mode":"assume-role","role_arn":"arn:aws:iam::123456789012:role/hikyo"}`,
		`{"mode":"web-identity","role_arn":"arn:aws:iam::123456789012:role/hikyo"}`,
	} {
		_, providerErr := awssm.NewClient(awssm.ClientConfig{Origin: "https://secretsmanager.eu-west-1.amazonaws.com", Credential: credential, Deadline: time.Second})
		if providerErr == nil {
			t.Fatal("invalid descriptor or disabled workload identity was accepted")
		}
		var configErr *awssm.ConfigError
		if !errors.As(providerErr, &configErr) {
			t.Fatalf("refusal lost safe provider configuration classification: %T", providerErr)
		}
		// The app factory adds ErrInvalid to configuration errors. This test
		// starts at that established boundary rather than duplicating the factory.
		for _, route := range []struct{ method, path string }{
			{http.MethodPost, "/api/v1/orgs/org_one/projects/prj_one/adapters"},
			{http.MethodPut, "/api/v1/orgs/org_one/projects/prj_one/adapters/adp_one/credential"},
		} {
			recorder := renderContractError(t, route.method, route.path, errors.Join(domain.ErrInvalid, providerErr))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want 400", recorder.Code)
			}
			for _, forbidden := range []string{"aws-api-secret-canary", "aws-api-private-canary", "AKIAHIKYOTEST0000001", "secret_access_key", "unexpected"} {
				if strings.Contains(recorder.Body.String(), forbidden) {
					t.Errorf("API refusal exposed descriptor material %q", forbidden)
				}
			}
		}
	}
}
