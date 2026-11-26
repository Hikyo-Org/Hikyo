package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// A contained principal that holds an instance capability is host-local: release
// answers not found for every actor, even one with instance scope, because releasing
// it is the operator's own act on the host.
func TestIncidentReleaseRefusesAnInstanceCapabilityTarget(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		id := w.containedIncident(t, true)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_target_inst','`+string(incTarget)+`','instance-config',NULL,`+ts+`)`)
		for _, actor := range []domain.PrincipalID{incResponder, incInstance} {
			if err := w.svc.ReleasePrincipal(ctx, w.as(actor), id, incTarget, ""); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("release of an instance-capability target by %s = %v, want not found", actor, err)
			}
		}
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("containment_state = %q, want contained", s)
		}
	})
}
