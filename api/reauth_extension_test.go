package api

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// x-hikyo-reauth (#606, social-signin spec 3.1) is read, not decorative: the
// declaration must match the request body it describes, per presence rule.
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
      x-hikyo-reauth: GATE
      requestBody:
        required: BODYREQ
        content:
          application/json:
            schema:
              type: object
              required: [REQ]
              properties:
                proof: {type: string}
                password: {type: string}
                code: {type: string}
                name: {type: string}
                purpose: {type: string, x-extensible-enum: [login, link]}
                count: {type: integer}
      responses:
        "200": {description: ok}
`
	for name, tc := range map[string]struct {
		gate, req, bodyReq string
		ok                 bool
	}{
		"required":                 {gate: "{class: account-security, proof: [proof], presence: required}", req: "proof", ok: true},
		"required but optional":    {gate: "{class: account-security, proof: [proof], presence: required}", req: "name"},
		"required two members":     {gate: "{class: account-security, proof: [proof, code], presence: required}", req: "proof"},
		"selected":                 {gate: "{class: account-security, proof: [password, code], presence: selected}", req: "name", ok: true},
		"selected single":          {gate: "{class: account-security, proof: [password], presence: selected}", req: "name"},
		"selected but required":    {gate: "{class: account-security, proof: [password, code], presence: selected}", req: "code"},
		"session":                  {gate: "{class: account-security, proof: [proof], presence: session}", req: "name", ok: true},
		"session but required":     {gate: "{class: account-security, proof: [proof], presence: session}", req: "proof"},
		"when-value":               {gate: "{class: account-security, proof: [proof], presence: when-value, member: purpose, values: [link]}", req: "name", ok: true},
		"when-value undeclared":    {gate: "{class: account-security, proof: [proof], presence: when-value, member: purpose, values: [claim]}", req: "name"},
		"when-value no member":     {gate: "{class: account-security, proof: [proof], presence: when-value, member: ghost, values: [link]}", req: "name"},
		"when-changed":             {gate: "{class: account-security, proof: [proof], presence: when-changed, member: name}", req: "name", ok: true},
		"when-changed with values": {gate: "{class: account-security, proof: [proof], presence: when-changed, member: name, values: [x]}", req: "name"},
		"condition on required":    {gate: "{class: account-security, proof: [proof], presence: required, member: name}", req: "proof"},
		"proof not a string":       {gate: "{class: account-security, proof: [count], presence: session}", req: "name"},
		"proof undeclared":         {gate: "{class: account-security, proof: [token], presence: session}", req: "name"},
		"unknown class":            {gate: "{class: sudo, proof: [proof], presence: required}", req: "proof"},
		"unknown presence":         {gate: "{class: account-security, proof: [proof], presence: sometimes}", req: "proof"},
		"unknown member":           {gate: "{class: account-security, proof: [proof], presence: required, why: x}", req: "proof"},
		"scalar form":              {gate: "account-security", req: "proof"},
		"optional body":            {gate: "{class: account-security, proof: [proof], presence: required}", req: "proof", bodyReq: "false"},
	} {
		t.Run(name, func(t *testing.T) {
			bodyReq := tc.bodyReq
			if bodyReq == "" {
				bodyReq = "true"
			}
			spec := strings.NewReplacer("GATE", tc.gate, "BODYREQ", bodyReq, "REQ", tc.req).Replace(base)
			doc, err := (&openapi3.Loader{IsExternalRefsAllowed: false}).LoadFromData([]byte(spec))
			if err != nil {
				t.Fatal(err)
			}
			ops, err := collectOperations(doc)
			if tc.ok {
				if err != nil || ops["testOperation"].Reauth.Class != ReauthAccountSecurity {
					t.Fatalf("collect = %v; want the reauth gate read", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "x-hikyo-reauth") {
				t.Fatalf("collect error = %v, want a named x-hikyo-reauth refusal", err)
			}
		})
	}
}

// proofMembers are the request-body member names that carry a credential.
var proofMembers = []string{"proof", "password", "code"}

// reauthExempt are the operations whose body carries a credential member
// that is NOT a proof gating some other mutation: the credential IS the
// ceremony (signing in, establishing, recovering, confirming or presenting a
// factor, redeeming a handoff code). Each is the thing a proof would be
// checked against, not a mutation gated on one.
var reauthExempt = map[string]string{
	"localLogin":             "the password is the login itself",
	"establishCredential":    "the password being established is the new credential",
	"beginRecovery":          "the recovery code is the recovery ceremony itself",
	"loginChallengeTotp":     "the code completes the login challenge",
	"enrolTotpConfirm":       "the code confirms the factor being enrolled",
	"stepUpTotp":             "the code is the step-up ceremony itself",
	"reauthTotp":             "the code is the reauthentication ceremony that opens a window",
	"redeemCLIReauth":        "the code is a single-use handoff redemption, not a credential",
	"redeemWorkspaceHandoff": "the code is a single-use handoff redemption, not a credential",
}

// Bidirectional, statically: every operation whose request body carries a
// credential member is either marked x-hikyo-reauth or exempt by name, and
// every marked operation carries one. The runtime half (every marked
// operation's service refuses without its proof) is in internal/isolation.
func TestEveryProofBodyIsReauthMarked(t *testing.T) {
	doc, err := Doc()
	if err != nil {
		t.Fatal(err)
	}
	ops, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	var carrying []string
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			if op.RequestBody == nil || op.RequestBody.Value == nil {
				continue
			}
			media := op.RequestBody.Value.Content.Get("application/json")
			if media == nil || media.Schema == nil || media.Schema.Value == nil {
				continue
			}
			schemas := []*openapi3.Schema{media.Schema.Value}
			for _, sub := range slices.Concat(media.Schema.Value.OneOf, media.Schema.Value.AnyOf, media.Schema.Value.AllOf) {
				if sub.Value != nil {
					schemas = append(schemas, sub.Value)
				}
			}
			for _, schema := range schemas {
				if slices.ContainsFunc(proofMembers, func(name string) bool { return schema.Properties[name] != nil }) {
					carrying = append(carrying, op.OperationID)
					break
				}
			}
		}
	}
	sort.Strings(carrying)
	for _, id := range carrying {
		marked := ops[id].Reauth.Class != ""
		_, exempt := reauthExempt[id]
		switch {
		case marked && exempt:
			t.Errorf("%s is both marked x-hikyo-reauth and exempt", id)
		case !marked && !exempt:
			t.Errorf("%s carries a credential member but is neither marked x-hikyo-reauth nor exempt by name", id)
		}
	}
	for id := range reauthExempt {
		if !slices.Contains(carrying, id) {
			t.Errorf("exempt operation %s carries no credential member; drop the exemption", id)
		}
	}
	for id, op := range ops {
		if op.Reauth.Class != "" && !slices.Contains(carrying, id) {
			t.Errorf("%s is marked x-hikyo-reauth but carries no credential member", id)
		}
	}
}
