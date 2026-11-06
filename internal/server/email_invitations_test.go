package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
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
