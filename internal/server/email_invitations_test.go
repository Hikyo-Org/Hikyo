package server

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type stubEmailInvitations struct{ EmailInvitationService }

func (stubEmailInvitations) Send(_ context.Context, _ service.Actor, spec service.EmailInvitationSendSpec) (service.EmailInvitationReceipt, error) {
	return service.EmailInvitationReceipt{ID: "ein_1", Email: spec.Email, Template: spec.Template}, nil
}

func (stubEmailInvitations) Accept(_ context.Context, _ service.Actor, _ string) (service.EmailInvitationAcceptance, error) {
	return service.EmailInvitationAcceptance{Org: "org_a", OrgName: "org-a", Template: "viewer", GrantsCreated: 1, SignedOut: true}, nil
}

func (stubEmailInvitations) List(context.Context, service.Actor, domain.OrgID) ([]service.EmailInvitationEntry, error) {
	return []service.EmailInvitationEntry{{ID: "ein_1", Email: "a@example.test", Template: "viewer", InvitedBy: "usr_admin"}}, nil
}

// The real response bodies carry a closed key set, so no account-derived field
// can appear for a matched address and not for an unmatched one.
func TestEmailInvitationResponseBodiesHaveAClosedKeySet(t *testing.T) {
	api := &API{EmailInvitations: stubEmailInvitations{}}
	keys := func(raw []byte) []string {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("body %s: %v", raw, err)
		}
		return slices.Sorted(maps.Keys(m))
	}
	sent, err := api.InviteOrgMemberByEmail(t.Context(), apigen.InviteOrgMemberByEmailRequestObject{
		Org: "org_a", Body: &apigen.InviteOrgMemberByEmailJSONRequestBody{Email: "a@example.test", Template: "viewer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := sent.VisitInviteOrgMemberByEmailResponse(rec); err != nil {
		t.Fatal(err)
	}
	if got, want := keys(rec.Body.Bytes()), []string{"created_at", "email", "expires_at", "id", "template"}; !slices.Equal(got, want) {
		t.Fatalf("receipt keys = %v, want %v", got, want)
	}
	listed, err := api.ListOrgEmailInvitations(t.Context(), apigen.ListOrgEmailInvitationsRequestObject{Org: "org_a"})
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	if err := listed.VisitListOrgEmailInvitationsResponse(rec); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("list body %s: %v", rec.Body.Bytes(), err)
	}
	if got, want := keys(page.Items[0]), []string{"created_at", "email", "expires_at", "id", "invited_by", "template"}; !slices.Equal(got, want) {
		t.Fatalf("list entry keys = %v, want %v", got, want)
	}
}

// An acceptance that ended the person's sessions, called from a browser
// session, answers a plain 200 without touching its cookies (#1207): the
// browser keeps a response whose CSRF companion cookie changed only if the
// session still proves the same person.
func TestAcceptEmailInvitationLeavesBrowserCookies(t *testing.T) {
	api := &API{EmailInvitations: stubEmailInvitations{}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/me/invitations/accept", nil)
	request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: "session-secret"})
	ctx := context.WithValue(t.Context(), requestKey{}, request)
	resp, err := api.AcceptEmailInvitation(ctx, apigen.AcceptEmailInvitationRequestObject{Body: &apigen.AcceptEmailInvitationJSONRequestBody{Token: "token"}})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := resp.VisitAcceptEmailInvitationResponse(rec); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 0 || !strings.Contains(rec.Body.String(), `"signed_out":true`) {
		t.Fatalf("status %d cookies %v body %s", rec.Code, rec.Result().Cookies(), rec.Body.String())
	}
}
