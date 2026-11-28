package server_test

import (
	"bytes"
	"context"
	"net/http"
	"slices"
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

// An environment rename or create that widens access through rules selecting
// environments by name (#1447) renders the same structured refusal.
func TestEnvironmentWideningRefusalBody(t *testing.T) {
	path := api.PathPrefix + "/orgs/" + testOrgID + "/projects/" + testProjectID + "/environments/" + testEnvID
	countOnly := hierarchyServer(t, &service.EnvWideningError{EnvName: "prod", Count: 3})
	resp, raw := call(t, countOnly, http.MethodPatch, path, "hik_1_cli_x", apigen.RenameRequest{Name: "prod"})
	body := decodeError(t, raw)
	if resp.StatusCode != http.StatusConflict || body.Error.Widening == nil || body.Error.Widening.Count != 3 ||
		body.Error.Widening.Gainers != nil || bytes.Contains(raw, []byte("usr_")) {
		t.Fatalf("count-only environment refusal = %d %s", resp.StatusCode, raw)
	}
	named := hierarchyServer(t, &service.EnvWideningError{
		EnvName: "prod", Count: 1, Principals: []domain.PrincipalID{testPrincipalID},
		Gains: []service.WideningGain{{Principal: testPrincipalID, Name: "Carol", Capability: domain.CapRead, Envs: []domain.EnvID{testEnvID}}},
	})
	resp, raw = call(t, named, http.MethodPost, api.PathPrefix+"/orgs/"+testOrgID+"/projects/"+testProjectID+"/environments", "hik_1_cli_x", apigen.CreateEnvironmentRequest{Name: "prod"})
	body = decodeError(t, raw)
	if resp.StatusCode != http.StatusConflict || body.Error.Widening == nil || body.Error.Widening.Gainers == nil ||
		(*body.Error.Widening.Gainers)[0].PrincipalId != testPrincipalID {
		t.Fatalf("named environment refusal = %d %s", resp.StatusCode, raw)
	}
	// A delete carries its confirmation as a query parameter and is refused
	// the same way.
	refusal := &service.EnvWideningError{EnvName: "prod", Deleting: true, Count: 2}
	got := make(chan []domain.PrincipalID, 1)
	deleting := hierarchyServerWithEnvs(t, refusal, confirmRecordingEnvs{stubEnvs: stubEnvs{stubHierarchy{err: refusal}}, got: got})
	resp, raw = call(t, deleting, http.MethodDelete, path+"?confirm_widening="+testPrincipalID, "hik_1_cli_x", nil)
	body = decodeError(t, raw)
	if resp.StatusCode != http.StatusConflict || body.Error.Widening == nil || body.Error.Widening.Count != 2 ||
		!bytes.Contains(raw, []byte(`deleting the environment \"prod\"`)) {
		t.Fatalf("delete environment refusal = %d %s", resp.StatusCode, raw)
	}
	// The handler sent before it answered, so the value is ready; the
	// channel orders its write before this read.
	var forwarded []domain.PrincipalID
	select {
	case forwarded = <-got:
	default:
	}
	if !slices.Equal(forwarded, []domain.PrincipalID{testPrincipalID}) {
		t.Fatalf("the delete handler forwarded confirm_widening %v, want [%s]", forwarded, testPrincipalID)
	}

	// The clients send several ids as one comma-separated value (the CLI
	// joins them, the TypeScript client uses explode: false); each id is
	// forwarded, in order.
	const second = "usr_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f98"
	got2 := make(chan []domain.PrincipalID, 1)
	both := hierarchyServerWithEnvs(t, refusal, confirmRecordingEnvs{stubEnvs: stubEnvs{stubHierarchy{err: refusal}}, got: got2})
	if resp, raw := call(t, both, http.MethodDelete, path+"?confirm_widening="+testPrincipalID+","+second, "hik_1_cli_x", nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("two-id delete = %d %s", resp.StatusCode, raw)
	}
	forwarded = nil
	select {
	case forwarded = <-got2:
	default:
	}
	if !slices.Equal(forwarded, []domain.PrincipalID{testPrincipalID, second}) {
		t.Fatalf("the delete handler forwarded confirm_widening %v, want [%s %s]", forwarded, testPrincipalID, second)
	}

	// A repeated id fails the parameter's uniqueItems: 400 bad_request, the
	// response the operation declares, and nothing is forwarded.
	got3 := make(chan []domain.PrincipalID, 1)
	repeated := hierarchyServerWithEnvs(t, refusal, confirmRecordingEnvs{stubEnvs: stubEnvs{stubHierarchy{err: refusal}}, got: got3})
	resp, raw = call(t, repeated, http.MethodDelete, path+"?confirm_widening="+testPrincipalID+","+testPrincipalID, "hik_1_cli_x", nil)
	if body := decodeError(t, raw); resp.StatusCode != http.StatusBadRequest || body.Error.Code != apigen.ErrorCodeBadRequest {
		t.Fatalf("a repeated confirm_widening id = %d %s, want 400 bad_request", resp.StatusCode, raw)
	}
	select {
	case ids := <-got3:
		t.Fatalf("a refused request forwarded confirm_widening %v", ids)
	default:
	}
}

// confirmRecordingEnvs records the confirmation a delete handler forwards.
type confirmRecordingEnvs struct {
	stubEnvs
	got chan<- []domain.PrincipalID
}

func (s confirmRecordingEnvs) DeleteConfirming(_ context.Context, _ service.Actor, _ domain.Scope, confirm []domain.PrincipalID) error {
	s.got <- confirm
	return s.outcome()
}
