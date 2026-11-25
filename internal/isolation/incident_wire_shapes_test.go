package isolation

import (
	"net/http"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The wire shapes the lifecycle test does not reach: opening a scope that already
// has an open incident answers reopened with the number of items it added, an
// owner's refusal shows as a refused item and a refused count apart from work not
// yet run, and closing with residuals answers 200 with the residual status.
func TestIncidentWireRefusedReopenedAndResidualShapes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		iw := newIncidentWire(t, db)
		tok := iw.session(t, wireResponder)
		openBody := map[string]string{"scope_kind": "account", "principal": string(wireTarget)}
		first := decodeWire[apigen.Incident](t, mustCall(t, iw, tok, http.MethodPost, "/incidents", openBody))
		if first.Reopened != nil || first.Added != nil {
			t.Fatalf("a first open reports reopened %v added %v", first.Reopened, first.Added)
		}
		again := decodeWire[apigen.Incident](t, mustCall(t, iw, tok, http.MethodPost, "/incidents", openBody))
		if again.Id != first.Id || again.Reopened == nil || !*again.Reopened || again.Added == nil || *again.Added != 0 {
			t.Fatalf("a second open = id %s reopened %v added %v, want the same incident, reopened, none added", again.Id, again.Reopened, again.Added)
		}

		// One owner refuses: its items stay pending with the owner's reason, and the
		// wire says refused for them and not for work that has not run.
		refuse := newFakeOwner()
		refuse.result = func(authz.IncidentItem) (service.IncidentOwnerResult, error) { return service.OwnerFailed, nil }
		iw.w.withOwner(refuse)
		iw.w.resume(t, wireResponder, first.Id)
		shown := decodeWire[apigen.Incident](t, mustCall(t, iw, tok, http.MethodGet, "/incidents/"+first.Id, nil))
		if shown.Counts.Refused == 0 || shown.Counts.Pending < shown.Counts.Refused {
			t.Fatalf("counts after a refused pass = %+v, want refused > 0 inside pending", shown.Counts)
		}
		var refused int
		for _, it := range decodeWire[apigen.IncidentItemList](t, mustCall(t, iw, tok, http.MethodGet, "/incidents/"+first.Id+"/items?limit=500", nil)).Items {
			if it.Refused {
				refused++
				if it.Outcome != "pending" || it.Reason == nil || *it.Reason != "owner_failed" {
					t.Errorf("refused item = outcome %s reason %v, want pending/owner_failed", it.Outcome, it.Reason)
				}
			} else if it.Reason != nil && *it.Reason == "owner_failed" {
				t.Errorf("item %s carries owner_failed but is not marked refused", it.Id)
			}
		}
		if int32(refused) != shown.Counts.Refused {
			t.Fatalf("items list %d refused, the counts say %d", refused, shown.Counts.Refused)
		}

		// Items are still open, so closing with residuals is a 200 with the residual status.
		code, body := iw.call(t, tok, http.MethodPost, "/incidents/"+first.Id+"/close", map[string]bool{"with_residuals": true})
		if got := decodeWire[apigen.Incident](t, body); code != http.StatusOK || got.Status != "closed_with_residuals" || got.ClosedBy == nil {
			t.Fatalf("residual close = %d %s", code, body)
		}
	})
}
