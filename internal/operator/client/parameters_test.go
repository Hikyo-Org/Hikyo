package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParameterValidationDetailReachesOperator(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"missing", `{"error":{"code":"bad_request","message":"bad request","detail":"required parameter PR_NUMBER is missing"}}`, "PR_NUMBER"},
		{"schema", `{"error":{"code":"bad_request","message":"bad request","detail":"key APP_BASE_URL failed validation"}}`, "APP_BASE_URL"},
		{"unknown", `{"error":{"code":"internal","detail":"do not echo this"}}`, "fetch validation failed"},
		{"malformed", `not JSON containing confidential text`, "fetch validation failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("parameters") != `{"PR_NUMBER":"123"}` {
					t.Errorf("wire omitted parameter object: %s", r.URL.RawQuery)
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c, err := NewClient(srv.URL, caPEM(t, srv), "parameter-test")
			if err != nil {
				t.Fatal(err)
			}
			out, outcome, err := c.Fetch(t.Context(), FetchRequest{Org: "o", Project: "p", Environment: "e", Bearer: "credential", Parameters: map[string]string{"PR_NUMBER": "123"}})
			if out != nil || outcome != OutcomeFetchFailed || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("response = %+v, %v, %v", out, outcome, err)
			}
			if strings.Contains(err.Error(), "do not echo") || strings.Contains(err.Error(), "confidential") {
				t.Fatal("untrusted response body echoed")
			}
		})
	}
}

func TestParameterizedFetchRefusesLegacyResponses(t *testing.T) {
	for _, current := range []bool{false, true} {
		for _, supplied := range []bool{false, true} {
			for _, tc := range []struct {
				name      string
				revision  any
				present   bool
				supported bool
			}{
				{name: "omitted"},
				{name: "null", present: true},
				{name: "zero", revision: 0, present: true},
				{name: "negative", revision: -1, present: true},
				{name: "selected", revision: 7, present: true, supported: true},
			} {
				t.Run(fmt.Sprintf("current=%t/parameters=%t/%s", current, supplied, tc.name), func(t *testing.T) {
					body := map[string]any{
						"current": current, "cursor": "cursor", "change_token": "token",
						"schema_revision": 1, "pin_expired": false, "keys": []DeliveredKey{},
					}
					if !current {
						value := "literal-${PR_NUMBER}"
						body["keys"] = []DeliveredKey{{Name: "URL", Classification: "config", Presence: "set", Value: &value}}
					}
					if tc.present {
						body["revision"] = tc.revision
					}
					srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if err := json.NewEncoder(w).Encode(body); err != nil {
							t.Error(err)
						}
					}))
					defer srv.Close()
					c, err := NewClient(srv.URL, caPEM(t, srv), "parameter-compatibility-test")
					if err != nil {
						t.Fatal(err)
					}
					req := FetchRequest{Org: "o", Project: "p", Environment: "e", Bearer: "credential"}
					if supplied {
						req.Parameters = map[string]string{"PR_NUMBER": "123"}
					}
					if current {
						req.Cursor = "cursor"
					}
					out, outcome, err := c.Fetch(t.Context(), req)
					if supplied && !tc.supported {
						if out != nil || outcome != OutcomeFetchFailed || err == nil || !strings.Contains(err.Error(), "API revision 3") {
							t.Fatalf("unsupported parameter delivery = %+v, %v, %v", out, outcome, err)
						}
						return
					}
					if err != nil || outcome != OutcomeOK || out == nil {
						t.Fatalf("compatible delivery = %+v, %v, %v", out, outcome, err)
					}
					if out.Current != current {
						t.Fatal("wrong response kind")
					}
					if tc.supported && (out.Revision == nil || *out.Revision != 7) {
						t.Fatal("selected revision not decoded")
					}
				})
			}
		}
	}
}
