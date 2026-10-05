package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type capturingRuleReplacement struct {
	stubRules
	spec *service.ReplaceRulesSpec
}

func (s capturingRuleReplacement) Replace(_ context.Context, _ service.Actor, spec service.ReplaceRulesSpec) ([]service.RuleView, error) {
	*s.spec = spec
	return nil, nil
}

func TestRuleReplacementAcceptsOneSidedBatches(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   json.RawMessage
		revoke int
		create int
	}{
		{"removal", json.RawMessage(fmt.Sprintf(`{"principal":%q,"revoke":[%q]}`, testPrincipalID, testRuleID)), 1, 0},
		{"creation", json.RawMessage(fmt.Sprintf(`{"principal":%q,"create":[{"principal":%q,"capability":"read","where":{"projects":[%q],"environments":{"mode":"all","items":[]},"keys":{"mode":"all","items":[]}}}]}`, testPrincipalID, testPrincipalID, testProjectID)), 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var spec service.ReplaceRulesSpec
			srv := httptest.NewServer(server.New(stubReady{}, &server.API{
				Auth:  stubAuth{identity: liveIdentityFn},
				Rules: capturingRuleReplacement{spec: &spec}, Version: "test",
			}, nil))
			t.Cleanup(srv.Close)
			response, payload := call(t, srv, http.MethodPost, api.PathPrefix+"/orgs/"+testOrgID+"/rules/replace", "hik_1_cli_x", tc.body)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status %d, want 200: %s", response.StatusCode, payload)
			}
			if spec.Org != domain.OrgID(testOrgID) || spec.Target != domain.PrincipalID(testPrincipalID) || len(spec.Revoke) != tc.revoke || len(spec.Create) != tc.create {
				t.Fatalf("wrong replacement batch: %#v", spec)
			}
			if tc.revoke > 0 && spec.Revoke[0] != testRuleID {
				t.Fatalf("wrong removed rule: %v", spec.Revoke)
			}
			if tc.create > 0 && (spec.Create[0].Target != domain.PrincipalID(testPrincipalID) || spec.Create[0].Capability != domain.CapRead) {
				t.Fatalf("wrong added rule: %#v", spec.Create)
			}
		})
	}
}
