package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// An unattested cut holds the release even when every identity the account has
// is listed and attested: the cut says identities went unlisted, and nothing but
// its own attestation records that an operator looked.
func TestIncidentReleaseUnattestedCutHoldsEvenWhenEveryIdentityIsAttested(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, db)+`,`+ts+`)`)
		w.realOwners()
		id := w.openOrg(t, incResponder)
		w.resume(t, incResponder, id)
		if err := w.svc.Attest(ctx, w.as(incResponder), id, w.itemIDByKey(t, incResponder, id, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, row_version, created_at, updated_at) VALUES ('itm_cut_only', '`+id+`', 'external_identity.remove', 'enumeration', '`+string(incTarget)+`', 'org/org_a', 'unknown', 'enumeration_truncated', 1, `+ts+`, `+ts+`)`)
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "") }
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with every identity attested and the cut open = %v, want ErrReleaseNotReady", err)
		}
		if err := w.svc.Attest(ctx, w.as(incResponder), id, "itm_cut_only"); err != nil {
			t.Fatal(err)
		}
		if err := release(); err != nil {
			t.Fatalf("release with the cut attested: %v", err)
		}
	})
}
