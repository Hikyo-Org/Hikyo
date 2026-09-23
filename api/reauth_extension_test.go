package api

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// x-hikyo-reauth (#606, social-signin spec 3.1) is read, not decorative: a
// marked operation must take a required JSON body with a string `proof`, and
// the class is closed.
func TestCollectOperationsValidatesReauthExtension(t *testing.T) {
	const base = `
openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /api/v1/test:
    put:
      operationId: testOperation
      x-hikyo-class: tenant
      x-hikyo-artifacts: [human-session]
      x-hikyo-min-revision: 1
      x-hikyo-reauth: CLASS
      BODY
      responses:
        "200": {description: ok}
`
	const proofBody = `requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                proof: {type: string}`
	const noProofBody = `requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name: {type: string}`
	for name, tc := range map[string]struct {
		class, body string
		ok          bool
	}{
		"proof body":       {class: "account-security", body: proofBody, ok: true},
		"no body":          {class: "account-security"},
		"body lacks proof": {class: "account-security", body: noProofBody},
		"unknown class":    {class: "sudo", body: proofBody},
	} {
		t.Run(name, func(t *testing.T) {
			spec := strings.Replace(strings.Replace(base, "CLASS", tc.class, 1), "BODY", tc.body, 1)
			doc, err := (&openapi3.Loader{IsExternalRefsAllowed: false}).LoadFromData([]byte(spec))
			if err != nil {
				t.Fatal(err)
			}
			ops, err := collectOperations(doc)
			if tc.ok {
				if err != nil || ops["testOperation"].Reauth != ReauthAccountSecurity {
					t.Fatalf("collect = %v, %v; want the reauth class read", ops, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "x-hikyo-reauth") {
				t.Fatalf("collect error = %v, want a named x-hikyo-reauth refusal", err)
			}
		})
	}
}

// The registration-policy mutations are the reauth-gated operations of #606.
func TestRegistrationMutationsAreReauthGated(t *testing.T) {
	ops, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"putOrgRegistrationPolicy", "deleteOrgRegistrationPolicy",
		"putInstanceRegistrationPolicy", "deleteInstanceRegistrationPolicy",
	} {
		if ops[id].Reauth != ReauthAccountSecurity {
			t.Errorf("%s: x-hikyo-reauth = %q, want %q", id, ops[id].Reauth, ReauthAccountSecurity)
		}
	}
}
