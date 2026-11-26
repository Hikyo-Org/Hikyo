package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// A contained principal is released through its incident's ledger and nothing
// else, so Close (with or without residuals) refuses while the incident holds
// one; the operator releases first, then closes.
func TestIncidentCloseRefusesWhileItHoldsAContainedPrincipal(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		id := w.containedIncident(t, true)
		for _, residuals := range []bool{true, false} {
			if _, err := w.svc.Close(ctx, w.as(incResponder), id, residuals); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("Close(withResiduals=%v) while holding a contained principal = %v, want conflict", residuals, err)
			}
		}
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("containment_state = %q after the refused closes, want contained", s)
		}
		// The way out stays open: attest, release, then close.
		if err := w.svc.Attest(ctx, w.as(incResponder), id, w.itemIDByKey(t, incResponder, id, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, ""); err != nil {
			t.Fatalf("release after the refused close: %v", err)
		}
		if _, err := w.svc.Close(ctx, w.as(incResponder), id, true); err != nil {
			t.Fatalf("close after the release: %v", err)
		}
	})
}

// A closed incident that somehow still holds a contained principal (a row from
// before this rule) answers a release with its own error, not a request to
// attest an item that can no longer be attested.
func TestIncidentReleaseOnAClosedIncidentSaysSo(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		id := w.containedIncident(t, true)
		execRaw(t, db, `UPDATE incidents SET status='closed_with_residuals', closed_at=`+ts+`, closed_by='usr_irsp' WHERE id='`+id+`'`)
		err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "")
		if !errors.Is(err, domain.ErrConflict) || errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release on a closed incident = %v, want a conflict that is not ErrReleaseNotReady", err)
		}
	})
}

// Only the external-identity enumeration cut is attestable: a rotation halt's
// truncation row is resolved by closing with residuals, never by an attestation.
func TestIncidentAttestRefusesARotationHaltEnumerationCut(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		id := w.containedIncident(t, false)
		execRaw(t, db, `INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, row_version, created_at, updated_at) VALUES ('itm_rot_cut', '`+id+`', 'rotation.halt', 'enumeration', 'org_a', 'org/org_a', 'unknown', 'enumeration_truncated', 1, `+ts+`, `+ts+`)`)
		if err := w.svc.Attest(ctx, w.as(incResponder), id, "itm_rot_cut"); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("attesting a rotation halt enumeration cut = %v, want conflict", err)
		}
		if got := queryString(t, db, `SELECT outcome FROM incident_items WHERE id='itm_rot_cut'`); got != "unknown" {
			t.Fatalf("the refused attestation left the cut %q, want unknown", got)
		}
	})
}
