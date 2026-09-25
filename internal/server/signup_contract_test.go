package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// signupAuth records what the start handler hands the service (#607) and
// supplies the public provider list.
type signupAuth struct {
	stubAuth
	seen      *[]string
	startErr  error
	providers []service.AuthMethodProvider
}

func (s signupAuth) OIDCStart(_ context.Context, slug, purpose, intent, signupOrg, _, _, _ string, _ bool) (service.OIDCStartResult, error) {
	*s.seen = append(*s.seen, slug+"|"+purpose+"|"+intent+"|"+signupOrg)
	if s.startErr != nil {
		return service.OIDCStartResult{}, s.startErr
	}
	return service.OIDCStartResult{AuthURL: "https://idp.example/authorize", State: "st", Purpose: purpose}, nil
}

func (s signupAuth) AuthMethods(context.Context) ([]service.AuthMethodProvider, bool, error) {
	return s.providers, true, nil
}

// intent and signup_org ride the start body to the service unchanged; an
// unknown intent is a contract refusal before the service is reached.
func TestOIDCStartCarriesIntentAndSignupOrg(t *testing.T) {
	var seen []string
	srv := newTestServer(t, signupAuth{seen: &seen}, stubOrgs{})
	for _, body := range []map[string]any{
		{"purpose": "login"},
		{"purpose": "login", "intent": "sign-in"},
		{"purpose": "login", "intent": "sign-up", "signup_org": testOrgID},
	} {
		if response, payload := call(t, srv, http.MethodPost, api.PathPrefix+"/auth/oidc/corp/start", "", body); response.StatusCode != http.StatusOK {
			t.Fatalf("start %v = %d %s", body, response.StatusCode, payload)
		}
	}
	want := []string{"corp|login||", "corp|login|sign-in|", "corp|login|sign-up|" + testOrgID}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("service saw %v, want %v", seen, want)
	}
	if response, _ := call(t, srv, http.MethodPost, api.PathPrefix+"/auth/oidc/corp/start", "", map[string]any{
		"purpose": "login", "intent": "join",
	}); response.StatusCode != http.StatusBadRequest || len(seen) != 3 {
		t.Fatalf("unknown intent = %d (service calls %d), want a 400 before the service", response.StatusCode, len(seen))
	}
}

// #588 d4: a reauth start on a policy-less row is refused by name (409, the
// remedy in detail); every other start refusal stays the uniform 401.
func TestOIDCStartNamesThePolicylessReauthRefusal(t *testing.T) {
	var seen []string
	srv := newTestServer(t, signupAuth{seen: &seen, startErr: service.ErrReauthNoPolicy}, stubOrgs{})
	response, payload := call(t, srv, http.MethodPost, api.PathPrefix+"/auth/oidc/corp/start", "", map[string]any{
		"purpose": "reauth", "environment_id": "env_prod",
	})
	var body apigen.Error
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusConflict || body.Error.Detail == nil || !strings.Contains(*body.Error.Detail, "enrol WebAuthn or TOTP") {
		t.Fatalf("policy-less reauth start = %d %s, want 409 naming the remedy", response.StatusCode, payload)
	}
	uniform := newTestServer(t, signupAuth{seen: &seen, startErr: service.ErrBadPurpose}, stubOrgs{})
	if response, _ := call(t, uniform, http.MethodPost, api.PathPrefix+"/auth/oidc/corp/start", "", map[string]any{
		"purpose": "link", "intent": "sign-up",
	}); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("intent on a link start = %d, want the uniform 401", response.StatusCode)
	}
}

// The public methods list carries the presentation brand (#587 d4) and the
// open door's landing kind (the confirmation step's landing line).
func TestAuthMethodsCarriesBrandAndLanding(t *testing.T) {
	srv := httptest.NewServer(server.New(stubReady{}, &server.API{
		Auth: signupAuth{seen: &[]string{}, providers: []service.AuthMethodProvider{
			{Slug: "google", DisplayName: "Google", Kind: "oidc", Brand: "google"},
			{Slug: "contoso", DisplayName: "Contoso", Kind: "oidc", Brand: "microsoft"},
			{Slug: "corp", DisplayName: "Corp SSO", Kind: "oidc"},
		}},
		Orgs: stubOrgs{}, Providers: stubProviders{}, Version: "test",
		Registration: stubRegistration{door: service.SignupDoor{Open: true, Landing: service.LandingFreshOrg,
			Methods: []service.SignupMethod{{Kind: "oidc", Slug: "google"}}}},
	}, nil))
	t.Cleanup(srv.Close)
	response, payload := call(t, srv, http.MethodGet, api.PathPrefix+"/auth/methods", "", nil)
	var methods apigen.AuthMethods
	if response.StatusCode != http.StatusOK || json.Unmarshal(payload, &methods) != nil {
		t.Fatalf("auth methods = %d %s", response.StatusCode, payload)
	}
	brands := []string{}
	for _, p := range methods.Providers {
		if p.Brand == nil {
			brands = append(brands, "")
		} else {
			brands = append(brands, string(*p.Brand))
		}
	}
	if strings.Join(brands, ",") != "google,microsoft," || methods.SignupLanding == nil || *methods.SignupLanding != "fresh-org" {
		t.Fatalf("methods = %s", payload)
	}
}

// `GET /orgs?origin=` narrows the operator's list to one origin (#585 d9).
func TestListOrgsFiltersByOrigin(t *testing.T) {
	orgs := []service.Org{
		{ID: testOrgID, Name: "acme", Active: true, Metadata: []byte(`{}`), CreatedAt: liveIdentity.CreatedAt, Origin: "manual"},
		{ID: "org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f12", Name: "org-self", Active: true, Metadata: []byte(`{}`),
			CreatedAt: liveIdentity.CreatedAt, Origin: "registration", RegistrationPolicyID: "rpol_x"},
	}
	srv := newTestServer(t, stubAuth{identity: liveIdentityFn}, stubOrgs{
		list: func(context.Context, service.Actor) ([]service.Org, error) { return orgs, nil },
	})
	for query, want := range map[string]string{"": "acme,org-self", "?origin=manual": "acme", "?origin=registration": "org-self"} {
		response, payload := call(t, srv, http.MethodGet, api.PathPrefix+"/orgs"+query, "hik_1_cli_x", nil)
		var list apigen.OrgList
		if response.StatusCode != http.StatusOK || json.Unmarshal(payload, &list) != nil {
			t.Fatalf("list%s = %d %s", query, response.StatusCode, payload)
		}
		names := []string{}
		for _, o := range list.Items {
			names = append(names, o.Name)
		}
		if strings.Join(names, ",") != want || list.Count != len(list.Items) {
			t.Fatalf("list%s = %v (count %d), want %s", query, names, list.Count, want)
		}
	}
	if response, _ := call(t, srv, http.MethodGet, api.PathPrefix+"/orgs?origin=imported", "hik_1_cli_x", nil); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown origin filter = %d, want 400", response.StatusCode)
	}
}
