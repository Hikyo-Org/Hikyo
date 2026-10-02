package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type localSignupAuth struct {
	stubAuth
	request func(string, domain.OrgID) error
	verify  func(service.SignupVerification) error
}

func (s localSignupAuth) Signup(_ context.Context, email string, org domain.OrgID) error {
	return s.request(email, org)
}
func (s localSignupAuth) VerifySignup(_ context.Context, in service.SignupVerification) error {
	return s.verify(in)
}

func TestLocalSignupWire(t *testing.T) {
	requests, verifications := 0, 0
	auth := localSignupAuth{request: func(email string, org domain.OrgID) error {
		requests++
		if email != "person@example.com" || org != testOrgID {
			t.Fatalf("request = %q,%q", email, org)
		}
		return nil
	}, verify: func(in service.SignupVerification) error {
		verifications++
		if in.Token != "opaque" || in.Password != "long enough password" || in.DisplayName != "Person" || in.OrgName != "team" || in.Landing != "fresh-org" {
			t.Fatalf("verify = %#v", in)
		}
		return nil
	}}
	srv := newTestServer(t, auth, stubOrgs{})
	response, payload := call(t, srv, http.MethodPost, api.PathPrefix+"/auth/signup", "", map[string]any{"email": "person@example.com", "org": testOrgID})
	if response.StatusCode != 202 || len(payload) != 0 || response.Header.Get("Set-Cookie") != "" {
		t.Fatalf("signup = %d %s", response.StatusCode, payload)
	}
	response, payload = call(t, srv, http.MethodPost, api.PathPrefix+"/auth/signup/verify", "", map[string]any{"token": "opaque", "password": "long enough password", "display_name": "Person", "org_name": "team", "landing": "fresh-org"})
	if response.StatusCode != 204 || len(payload) != 0 || response.Header.Get("Set-Cookie") != "" {
		t.Fatalf("verify = %d %s", response.StatusCode, payload)
	}
	if requests != 1 || verifications != 1 {
		t.Fatalf("service calls=%d,%d", requests, verifications)
	}
	response, _ = call(t, srv, http.MethodGet, api.PathPrefix+"/auth/signup/verify", "", nil)
	if response.StatusCode != http.StatusNotFound || verifications != 1 {
		t.Fatalf("GET consumed: status=%d calls=%d", response.StatusCode, verifications)
	}
}

type instanceMailStub struct{ to, proof *string }

func (instanceMailStub) Configured(context.Context, service.Actor) (bool, error) { return true, nil }
func (s instanceMailStub) Test(_ context.Context, _ service.Actor, to, proof string) error {
	*s.to, *s.proof = to, proof
	return nil
}
func TestInstanceMailWire(t *testing.T) {
	var to, proof string
	srv := httptest.NewServer(server.New(stubReady{}, &server.API{Auth: stubAuth{}, Orgs: stubOrgs{}, Mail: instanceMailStub{&to, &proof}}, nil))
	t.Cleanup(srv.Close)
	response, payload := call(t, srv, http.MethodGet, api.PathPrefix+"/instance/mail", "hik_1_cli_x", nil)
	if response.StatusCode != 200 || string(payload) != "{\"configured\":true}\n" {
		t.Fatalf("status=%d %s", response.StatusCode, payload)
	}
	response, payload = call(t, srv, http.MethodPost, api.PathPrefix+"/instance/mail/test", "hik_1_cli_x", map[string]any{"to": "recipient@example.com", "proof": "fresh proof"})
	if response.StatusCode != 204 || to != "recipient@example.com" || proof != "fresh proof" {
		t.Fatalf("test=%d %s recipient=%s", response.StatusCode, payload, to)
	}
}
