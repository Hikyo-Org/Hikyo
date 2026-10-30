package server_test

import (
	"net/http"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// TestAuditOutcomeParamsAreAClosedEnumAtTheContract pins that the audit trail's
// outcome filters are CLOSED enums enforced by contract validation, not open
// (`x-extensible-enum`) ones. A stranger value is refused with a 400 naming the
// offending member BEFORE the handler runs, on the scalar `outcome`, on the
// repeatable `outcomes`, and on a single bad item within an otherwise valid
// `outcomes` set. This is why the handler's mergeOutcomes only de-duplicates:
// every value it sees is already a licensed member. A regression that made
// either param open (or dropped array-item validation) would let an
// unrecognized outcome reach the audit.query payload; this test would catch it.
func TestAuditOutcomeParamsAreAClosedEnumAtTheContract(t *testing.T) {
	srv := newTestServer(t, stubAuth{}, stubOrgs{})
	base := api.PathPrefix + "/orgs/" + testOrgID + "/audit"
	for _, tc := range []struct {
		name, query, member string
	}{
		{"scalar outcome", "?outcome=bogus", "outcome"},
		{"repeated outcomes", "?outcomes=bogus", "outcomes"},
		{"one bad item in a valid set", "?outcomes=denied&outcomes=bogus", "outcomes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, payload := call(t, srv, http.MethodGet, base+tc.query, "hik_1_cli_x", nil)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: a non-enum outcome reached past the contract", resp.StatusCode)
			}
			body := decodeError(t, payload)
			if body.Error.Code != apigen.ErrorCodeBadRequest {
				t.Errorf("code %q, want bad_request", body.Error.Code)
			}
			if body.Error.Detail == nil || *body.Error.Detail != tc.member {
				t.Errorf("detail = %v, want %q", body.Error.Detail, tc.member)
			}
		})
	}
}

func TestInstanceAuditBoundsAreValidatedBeforeReading(t *testing.T) {
	srv := newTestServer(t, stubAuth{}, stubOrgs{})
	for _, query := range []string{"limit=0", "limit=1001", "after_seq=-1", "to_seq=-1", "outcomes=made-up"} {
		t.Run(query, func(t *testing.T) {
			resp, _ := call(t, srv, http.MethodGet, api.PathPrefix+"/instance/audit?"+query, "hik_1_cli_x", nil)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("invalid instance filter %s: status %d", query, resp.StatusCode)
			}
		})
	}
}
