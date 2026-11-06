package server

import (
	"context"
	"encoding/json"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// An acceptance that ended the person's sessions clears the browser cookies in
// the same 200 (#1207), exactly as logout does.
func TestAcceptEmailInvitationClearsBrowserCookies(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := acceptEmailInvitationResponse{
		body:    apigen.EmailInvitationAcceptance{Org: "org_a", OrgName: "org-a", Template: apigen.RoleTemplate("viewer"), GrantsCreated: 3, SignedOut: true},
		cookies: expiredBrowserCookies(),
	}
	if err := resp.VisitAcceptEmailInvitationResponse(rec); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"signed_out":true`) {
		t.Fatalf("response %d %s", rec.Code, rec.Body.String())
	}
	cleared := map[string]bool{}
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 && c.Value == "" {
			cleared[c.Name] = true
		}
	}
	if !cleared[browserSessionCookie] || !cleared[browserCSRFCookie] {
		t.Fatalf("cookies cleared: %v", cleared)
	}
}

// The handler's own decision: cookies are cleared only when the acceptance
// ended the sessions (signed_out) and the request carried a browser session.
func TestAcceptEmailInvitationCookieClearingDecision(t *testing.T) {
	for _, tc := range []struct {
		name      string
		signedOut bool
		cookie    bool
		cleared   bool
	}{
		{"signed out with a browser session", true, true, true},
		{"signed out without a browser session", true, false, false},
		{"not signed out with a browser session", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/invitations/accept", nil)
			if tc.cookie {
				r.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: "session"})
			}
			ctx := context.WithValue(t.Context(), requestKey{}, r)
			resp := acceptEmailInvitationResult(ctx, service.EmailInvitationAcceptance{Org: "org_a", OrgName: "org-a", Template: "viewer", GrantsCreated: 1, SignedOut: tc.signedOut})
			rec := httptest.NewRecorder()
			if err := resp.VisitAcceptEmailInvitationResponse(rec); err != nil {
				t.Fatal(err)
			}
			cleared := false
			for _, c := range rec.Result().Cookies() {
				cleared = cleared || c.Name == browserSessionCookie && c.MaxAge < 0 && c.Value == ""
			}
			if rec.Code != http.StatusOK || cleared != tc.cleared {
				t.Fatalf("status %d cleared %v, want cleared %v", rec.Code, cleared, tc.cleared)
			}
		})
	}
}

type stubEmailInvitations struct{ EmailInvitationService }

func (stubEmailInvitations) Send(_ context.Context, _ service.Actor, spec service.EmailInvitationSendSpec) (service.EmailInvitationReceipt, error) {
	return service.EmailInvitationReceipt{ID: "ein_1", Email: spec.Email, Template: spec.Template}, nil
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
