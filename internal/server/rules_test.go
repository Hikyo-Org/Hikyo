package server_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// A folder move that widens access through member access rules renders a
// conflict carrying the structured `widening` member: the count always, the
// gainers only when the service disclosed them.
func TestMoveWideningRefusalBody(t *testing.T) {
	path := api.PathPrefix + "/orgs/" + testOrgID + "/projects/" + testProjectID + "/keys/" + testKeyID
	folder := "db"
	patch := apigen.UpdateKeyMetadataRequest{FolderPath: &folder}

	countOnly := hierarchyServer(t, &service.MoveWideningError{KeyID: testKeyID, Count: 2})
	resp, raw := call(t, countOnly, http.MethodPatch, path, "hik_1_cli_x", patch)
	body := decodeError(t, raw)
	if resp.StatusCode != http.StatusConflict || body.Error.Code != apigen.ErrorCodeConflict || body.Error.Widening == nil {
		t.Fatalf("count-only refusal = %d %s", resp.StatusCode, raw)
	}
	if body.Error.Widening.Count != 2 || body.Error.Widening.Gainers != nil || bytes.Contains(raw, []byte("usr_")) {
		t.Fatalf("a refusal without disclosure named people: %s", raw)
	}

	named := hierarchyServer(t, &service.MoveWideningError{
		KeyID: testKeyID, Count: 1, Principals: []domain.PrincipalID{testPrincipalID},
		Gains: []service.WideningGain{{Principal: testPrincipalID, Name: "Carol", Capability: domain.CapReveal, Envs: []domain.EnvID{testEnvID}}},
	})
	resp, raw = call(t, named, http.MethodPatch, path, "hik_1_cli_x", patch)
	body = decodeError(t, raw)
	if resp.StatusCode != http.StatusConflict || body.Error.Widening == nil || body.Error.Widening.Gainers == nil {
		t.Fatalf("named refusal = %d %s", resp.StatusCode, raw)
	}
	g := (*body.Error.Widening.Gainers)[0]
	if g.PrincipalId != testPrincipalID || g.PrincipalName == nil || *g.PrincipalName != "Carol" ||
		g.Capability != apigen.RuleCapabilityReveal || len(g.Environments) != 1 || g.Environments[0] != testEnvID {
		t.Fatalf("gainer = %+v", g)
	}
}
