package service

import (
	"encoding/json"
	"testing"
)

func rawClaims(t *testing.T, doc string) map[string]json.RawMessage {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

// #598 d3/d4: a non-empty string email beside a recognised claim that is
// the JSON boolean true; any recognised claim present and not true refuses.
func TestVerifiedEmail(t *testing.T) {
	for _, c := range []struct {
		doc, by string
		ok      bool
	}{
		{`{"email":"a@b.test","email_verified":true}`, "email_verified", true},
		{`{"email":"a@b.test","xms_edov":true}`, "xms_edov", true},
		{`{"email":"a@b.test","email_verified":true,"xms_edov":true}`, "email_verified", true},
		{`{"email":"a@b.test"}`, "", false},
		{`{"email":"a@b.test","email_verified":false}`, "", false},
		{`{"email":"a@b.test","email_verified":"true"}`, "", false},
		{`{"email":"a@b.test","email_verified":1}`, "", false},
		{`{"email":"a@b.test","email_verified":null}`, "", false},
		{`{"email":"a@b.test","email_verified":false,"xms_edov":true}`, "", false},
		{`{"email":"a@b.test","email_verified":true,"xms_edov":"true"}`, "", false},
		{`{"email":"","email_verified":true}`, "", false},
		{`{"email":["a@b.test"],"email_verified":true}`, "", false},
		{`{"email_verified":true}`, "", false},
	} {
		address, by, ok := verifiedEmail(rawClaims(t, c.doc))
		if ok != c.ok || by != c.by || (ok && address != "a@b.test") {
			t.Errorf("%s = (%q, %q, %v), want (%q, %v)", c.doc, address, by, ok, c.by, c.ok)
		}
	}
}

func TestClaimAdmitted(t *testing.T) {
	values := []string{"acme.example"}
	for doc, want := range map[string]bool{
		`{"hd":"acme.example"}`:   true,
		`{"hd":"other.example"}`:  false,
		`{"hd":["acme.example"]}`: false,
		`{"hd":null}`:             false,
		`{}`:                      false,
	} {
		if got := claimAdmitted(rawClaims(t, doc), "hd", values); got != want {
			t.Errorf("%s admitted = %v, want %v", doc, got, want)
		}
	}
}

func TestProviderBrand(t *testing.T) {
	for issuer, want := range map[string]string{
		"https://accounts.google.com":  "google",
		"https://accounts.google.com/": "",
		"https://login.microsoftonline.com/72f988bf-86f1-41af-91ab-2d7cd011db47/v2.0": "microsoft",
		"https://sso.corp.example": "",
	} {
		if got := providerBrand(issuer); got != want {
			t.Errorf("providerBrand(%q) = %q, want %q", issuer, got, want)
		}
	}
}

func TestSignupDisplayName(t *testing.T) {
	for doc, want := range map[string]string{
		`{"name":"Dana Scully"}`: "Dana Scully",
		`{"name":" padded"}`:     "oidc-acc_x",
		`{"name":"bad\u0007"}`:   "oidc-acc_x",
		`{"name":7}`:             "oidc-acc_x",
		`{}`:                     "oidc-acc_x",
	} {
		got, from := signupDisplayName(rawClaims(t, doc), "oidc-acc_x")
		wantFrom := "name-claim"
		if want == "oidc-acc_x" {
			wantFrom = "handle"
		}
		if got != want || from != wantFrom {
			t.Errorf("%s display name = (%q, %q), want (%q, %q)", doc, got, from, want, wantFrom)
		}
	}
}
