package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type stubSAMLAuth struct {
	startResult service.SAMLStartResult
	acsResult   service.LoginResult
	metadata    []byte
	acsCookie   string
	acsError    error
	acsRelay    string
}

func (s *stubSAMLAuth) SAMLStart(context.Context, string, string, string, string, string) (service.SAMLStartResult, error) {
	return s.startResult, nil
}

func (s *stubSAMLAuth) SAMLACS(_ context.Context, _, _, relayState, initiatorCookie string) (service.LoginResult, error) {
	s.acsCookie = initiatorCookie
	s.acsRelay = relayState
	return s.acsResult, s.acsError
}

func (s *stubSAMLAuth) SAMLMetadata(context.Context, string) ([]byte, error) {
	return s.metadata, nil
}

func TestSAMLStartSetsCrossSitePathScopedInitiatorCookie(t *testing.T) {
	stub := &stubSAMLAuth{startResult: service.SAMLStartResult{
		RedirectURL:     "https://idp.example/sso?SAMLRequest=request&RelayState=relay-state",
		InitiatorCookie: "initiator-secret",
	}}
	api := &API{SAMLAuth: stub}
	response, err := api.SamlStart(context.Background(), apigen.SamlStartRequestObject{
		Provider: "corp", Body: &apigen.SamlStartRequest{Purpose: "login"},
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitSamlStartResponse(recorder); err != nil {
		t.Fatal(err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.SameSite != http.SameSiteNoneMode || !cookie.Secure || !cookie.HttpOnly {
		t.Fatalf("cookie attributes = SameSite %v Secure %v HttpOnly %v", cookie.SameSite, cookie.Secure, cookie.HttpOnly)
	}
	if cookie.Path != "/api/v1/auth/saml/corp/acs" || cookie.MaxAge != 600 {
		t.Fatalf("cookie scope = Path %q MaxAge %d", cookie.Path, cookie.MaxAge)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	acs := mustURL(t, "https://hikyo.example/api/v1/auth/saml/corp/acs")
	jar.SetCookies(acs, cookies)
	if got := jar.Cookies(mustURL(t, "https://hikyo.example/api/v1/auth/saml/corp/acs")); len(got) != 1 {
		t.Fatalf("ACS cookies = %d, want 1", len(got))
	}
	if got := jar.Cookies(mustURL(t, "https://hikyo.example/api/v1/whoami")); len(got) != 0 {
		t.Fatalf("unrelated path received %d SAML cookies", len(got))
	}
}

func TestSAMLACSConsumesInitiatorAndMintsOrdinaryBrowserCookie(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	stub := &stubSAMLAuth{acsResult: service.LoginResult{
		SessionToken: "br.session", SessionID: "session", Artifact: service.ArtifactBrowser,
		CreatedAt: now, IdleExpires: now.Add(time.Hour), AbsExpires: now.Add(8 * time.Hour), Principal: "principal",
	}}
	provider, relay := "corp", "relay-state"
	request := httptest.NewRequest(http.MethodPost, "https://hikyo.example"+samlACSPath(provider), nil)
	request.AddCookie(&http.Cookie{Name: samlBindingCookieName(provider, relay), Value: "initiator-secret"})
	ctx := context.WithValue(context.Background(), requestKey{}, request)
	api := &API{SAMLAuth: stub}
	response, err := api.SamlACS(ctx, apigen.SamlACSRequestObject{
		Provider: apigen.ProviderSlug(provider),
		Body:     &apigen.SamlACSRequest{RelayState: relay, SAMLResponse: "response"},
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitSamlACSResponse(recorder); err != nil {
		t.Fatal(err)
	}
	if stub.acsCookie != "initiator-secret" {
		t.Fatalf("initiator cookie = %q", stub.acsCookie)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies = %d, want clear + session", len(cookies))
	}
	if cookies[0].MaxAge >= 0 || cookies[0].SameSite != http.SameSiteNoneMode {
		t.Fatalf("initiator clear cookie = %#v", cookies[0])
	}
	if cookies[1].Name != browserSessionCookie || cookies[1].SameSite != http.SameSiteLaxMode || cookies[1].Path != "/" {
		t.Fatalf("session cookie = %#v", cookies[1])
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, leaked := body["session_token"]; leaked {
		t.Fatal("browser session token leaked in ACS JSON")
	}
}

func samlHTTPRequest(t *testing.T, stub *stubSAMLAuth, accept, relay string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"SAMLResponse": {"c2lnbmVkLXJlc3BvbnNl"}, "RelayState": {relay}}
	request := httptest.NewRequest(http.MethodPost, "https://hikyo.example"+samlACSPath("corp"), strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", accept)
	request.AddCookie(&http.Cookie{Name: samlBindingCookieName("corp", relay), Value: "initiator-secret"})
	recorder := httptest.NewRecorder()
	New(&API{SAMLAuth: stub}, nil).ServeHTTP(recorder, request)
	if err := api.ValidateResponse(request, recorder.Code, recorder.Header(), recorder.Body.Bytes()); err != nil {
		t.Fatalf("SAML HTTP response violates contract: %v; status=%d body=%s", err, recorder.Code, recorder.Body)
	}
	return recorder
}

func TestSAMLACSHTTPBrowserCompletionRedirectsWithSharedCookiePair(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	relay := "validated-relay-state"
	stub := &stubSAMLAuth{acsResult: service.LoginResult{
		SessionToken: "browser-secret", CSRFToken: "csrf-secret", SessionID: "session", Artifact: service.ArtifactBrowser,
		CreatedAt: now, IdleExpires: now.Add(time.Hour), AbsExpires: now.Add(8 * time.Hour), Principal: "principal",
	}}
	recorder := samlHTTPRequest(t, stub, "text/html,application/xhtml+xml,*/*;q=0.8", relay)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/auth/saml/done?state="+relay {
		t.Fatalf("browser completion = %d Location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if recorder.Body.Len() != 0 || strings.Contains(recorder.Header().Get("Location"), "secret") {
		t.Fatal("browser redirect leaked session material or emitted a response body")
	}
	if stub.acsRelay != relay || stub.acsCookie != "initiator-secret" {
		t.Fatal("HTTP completion changed the SAML transaction/initiator binding")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 3 || cookies[0].MaxAge >= 0 {
		t.Fatalf("cookies = %#v, want initiator clear plus shared browser pair", cookies)
	}
	if session := cookies[1]; session.Name != browserSessionCookie || session.Value != "browser-secret" || !session.Secure || !session.HttpOnly || session.Path != "/" || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %#v", session)
	}
	if csrf := cookies[2]; csrf.Name != browserCSRFCookie || csrf.Value != "csrf-secret" || !csrf.Secure || csrf.HttpOnly || csrf.Path != "/" || csrf.SameSite != http.SameSiteStrictMode {
		t.Fatalf("CSRF cookie = %#v", csrf)
	}
}

func TestSAMLACSHTTPJSONCompletionRemainsJSON(t *testing.T) {
	for _, accept := range []string{"", "application/json", "*/*", "text/html;q=0,application/json"} {
		t.Run(accept, func(t *testing.T) {
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			stub := &stubSAMLAuth{acsResult: service.LoginResult{
				SessionToken: "browser-secret", CSRFToken: "csrf-secret", SessionID: "ses_01980000-0000-7000-8000-000000000001", Artifact: service.ArtifactBrowser,
				CreatedAt: now, IdleExpires: now.Add(time.Hour), AbsExpires: now.Add(8 * time.Hour), Principal: "usr_01980000-0000-7000-8000-000000000002",
				Assurance: service.Assurance{Method: "saml:corp", Factors: []string{"saml"}, AuthenticatedAt: now},
			}}
			recorder := samlHTTPRequest(t, stub, accept, "validated-relay-state")
			if recorder.Code != http.StatusOK || recorder.Header().Get("Location") != "" || recorder.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("JSON completion = %d Location %q type %q", recorder.Code, recorder.Header().Get("Location"), recorder.Header().Get("Content-Type"))
			}
			if strings.Contains(recorder.Body.String(), "browser-secret") || strings.Contains(recorder.Body.String(), "csrf-secret") {
				t.Fatal("browser credential leaked into JSON")
			}
			if cookies := recorder.Result().Cookies(); len(cookies) != 3 {
				t.Fatalf("JSON cookie pair omitted: %#v", cookies)
			}
		})
	}
}

func TestSAMLACSHTTPInvalidStateNeverRedirectsOrMintsCookies(t *testing.T) {
	stub := &stubSAMLAuth{acsError: domain.ErrUnauthenticated}
	recorder := samlHTTPRequest(t, stub, "text/html", "invalid-relay-state")
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("Location") != "" {
		t.Fatalf("invalid state = %d Location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == browserSessionCookie || cookie.Name == browserCSRFCookie || cookie.MaxAge >= 0 {
			t.Fatalf("invalid state minted a cookie: %#v", cookie)
		}
	}
}

func TestSAMLMetadataUsesMetadataMediaType(t *testing.T) {
	stub := &stubSAMLAuth{metadata: []byte("<EntityDescriptor/>")}
	response, err := (&API{SAMLAuth: stub}).SamlMetadata(context.Background(), apigen.SamlMetadataRequestObject{Provider: "corp"})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitSamlMetadataResponse(recorder); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/samlmetadata+xml" {
		t.Fatalf("Content-Type = %q", got)
	}
	if strings.TrimSpace(recorder.Body.String()) != "<EntityDescriptor/>" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
