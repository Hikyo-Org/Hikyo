package isolation

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Only an other incident that recorded the principal's containment as done holds the
// release: one whose account.contain item is still pending has not contained or
// claimed anything, so the containing incident releases without waiting for it.
func TestIncidentReleaseIgnoresAnOtherIncidentWhoseContainItemIsNotDone(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, db)+`,`+ts+`)`)
		w.realOwners()
		first := w.openAccount(t, incResponder).Incident.ID
		second := w.openOrg(t, incResponder)
		w.resume(t, incResponder, first)
		if got := w.itemMap(t, incResponder, second)["account.contain|account|usr_itarget"]; got == "done/" || got == "" {
			t.Fatalf("the second incident's account.contain item = %q, want an item that is not done", got)
		}
		if err := w.svc.Attest(ctx, w.as(incResponder), first, w.itemIDByKey(t, incResponder, first, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), first, incTarget, ""); err != nil {
			t.Fatalf("release with the other incident's contain item pending: %v", err)
		}
	})
}
