package server

import (
	"context"
	"net/http"
	"net/http/httptest"
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
