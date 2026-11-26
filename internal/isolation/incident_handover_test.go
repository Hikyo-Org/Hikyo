package isolation

import (
	"context"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// crossHeld is the state two open org incidents reach when each holds one member
// the other recorded done: a holds P (b recorded it done) and b holds Q, while a still
// has Q open until finish runs (a then records Q done too).
type crossHeld struct {
	w      *incidentWorld
	op     domain.PrincipalID
	a, b   string
	q      domain.PrincipalID
	holder func(id string) string
	finish func()
}

func newCrossHeld(t *testing.T, db *store.DB) crossHeld {
	t.Helper()
	ctx := t.Context()
	w := newIncidentWorld(t, db)
	const q = domain.PrincipalID("usr_iq")
	execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_iq','human',`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_iq','usr_iq','read','org_a',`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_iqb','usr_iq','read','org_b',`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_ipb','usr_itarget','read','org_b',`+ts+`)`)
	execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_iq','usr_iq','it-q','Q','iq@example.test',`+ts+`,`+ts+`)`)
	execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, db)+`,`+ts+`)`)
	execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_q','acc_iq','oidc','https://idp.example.test','subject-q','',`+acctEpoch(t, db)+`,`+ts+`)`)
	seedOrigins(t, db)
	w.realOwners()
	op := incInstance // both members are in two orgs, so containing them needs instance scope
	a := w.openOrg(t, op)
	bv, err := w.svc.Open(ctx, w.as(op), service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgB}})
	if err != nil {
		t.Fatal(err)
	}
	b := bv.Incident.ID
	real := w.svc.Owners[domain.OwnerAccountContain]
	crash := true
	w.svc.Owners[domain.OwnerAccountContain] = func(ctx context.Context, actor service.Actor, it authz.IncidentItem) (service.IncidentOwnerResult, error) {
		if crash && it.IncidentID == a && it.ObjectID == string(q) {
			return service.OwnerUnconfirmed, errors.New("simulated owner error")
		}
		return real(ctx, actor, it)
	}
	_, _ = w.svc.Resume(ctx, w.as(op), a, "", nil) // P contained by a; Q left open
	_, _ = w.svc.Resume(ctx, w.as(op), b, "", nil) // P done (a holds it); Q contained by b
	holder := func(id string) string {
		return queryString(t, db, `SELECT COALESCE(contained_incident_id,'') FROM principals WHERE id='`+id+`'`)
	}
	finish := func() {
		crash = false
		_, _ = w.svc.Resume(ctx, w.as(op), a, "", nil) // Q done (b holds it)
	}
	return crossHeld{w: w, op: op, a: a, b: b, q: q, holder: holder, finish: finish}
}

// Two open org incidents that each hold one member the other recorded as done would
// otherwise refuse every release (each names the other) and every close (each still
// holds a principal). Closing hands the principal to the other incident, with
// containment untouched. The receiving incident's evidence for the principal is reset
// in the handover (its contain item reopens, its attested identity becomes unknown),
// so it confirms and attests again before it releases both and closes.
func TestIncidentCloseHandsAHeldPrincipalToTheOtherOpenHolder(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		h := newCrossHeld(t, db)
		w, op, a, b, q, holder := h.w, h.op, h.a, h.b, h.q, h.holder
		h.finish()
		if holder("usr_itarget") != a || holder("usr_iq") != b {
			t.Fatalf("setup did not reach the cross-held state: P %q Q %q", holder("usr_itarget"), holder("usr_iq"))
		}
		// The deadlock: each release names the other incident, each close still holds one.
		if err := w.svc.ReleasePrincipal(ctx, w.as(op), a, incTarget, ""); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release P through a = %v, want ErrReleaseNotReady naming b", err)
		}
		if _, err := w.svc.Close(ctx, w.as(op), b, true); err != nil {
			// b holds Q, which a also recorded as done: it hands Q to a.
			t.Fatalf("close b: %v", err)
		}
		if holder("usr_iq") != a {
			t.Fatalf("Q is held by %q after b closed, want a", holder("usr_iq"))
		}
		if got := w.containmentState(t, q); got != "contained" {
			t.Fatalf("Q state = %q after the handover, want contained", got)
		}
		if n := w.correlated(t, "incident.principal_contained", a); n < 2 {
			t.Fatalf("handover wrote no incident.principal_contained event correlated to a (%d)", n)
		}
		// a now holds P and Q and no other open holder remains. Its contain item for Q
		// reopened with the handover, so it confirms it, attests, releases both, closes.
		if _, err := w.svc.Resume(ctx, w.as(op), a, "", nil); err != nil {
			t.Fatalf("resume a after the handover: %v", err)
		}
		for _, x := range []string{"xid_rel", "xid_q"} {
			if err := w.svc.Attest(ctx, w.as(op), a, w.itemIDByKey(t, op, a, "external_identity.remove|external_identity|"+x)); err != nil {
				t.Fatalf("attest %s: %v", x, err)
			}
		}
		if _, err := w.svc.Close(ctx, w.as(op), a, true); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("close a while it holds P and Q with no other holder = %v, want conflict", err)
		}
		for _, p := range []domain.PrincipalID{incTarget, q} {
			if err := w.svc.ReleasePrincipal(ctx, w.as(op), a, p, ""); err != nil {
				t.Fatalf("release %s: %v", p, err)
			}
		}
		if _, err := w.svc.Close(ctx, w.as(op), a, true); err != nil {
			t.Fatalf("close a after releasing both: %v", err)
		}
	})
}

// Release evidence is per containment episode. b recorded P, attested its identity and
// released it; a then contained P again and closed, which hands P to b because b's
// contain item still reads done. That item and b's attestation belong to the earlier
// episode, so the handover reopens them: b's release is refused until b confirms the
// containment and attests the identity again.
func TestIncidentHandoverDoesNotCarryEvidenceFromAnEarlierEpisode(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_ipb','usr_itarget','read','org_b',`+ts+`)`)
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, db)+`,`+ts+`)`)
		seedOrigins(t, db)
		w.realOwners()
		op := incInstance
		a := w.openOrg(t, op)
		bv, err := w.svc.Open(ctx, w.as(op), service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgB}})
		if err != nil {
			t.Fatal(err)
		}
		b := bv.Incident.ID
		holder := func() string {
			return queryString(t, db, `SELECT COALESCE(contained_incident_id,'') FROM principals WHERE id='usr_itarget'`)
		}
		key := "external_identity.remove|external_identity|xid_rel"
		// Episode one: b alone contains P, its identity is attested, b releases P.
		if _, err := w.svc.Resume(ctx, w.as(op), b, "", nil); err != nil {
			t.Fatal(err)
		}
		if holder() != b {
			t.Fatalf("setup: P held by %q, want b", holder())
		}
		if err := w.svc.Attest(ctx, w.as(op), b, w.itemIDByKey(t, op, b, key)); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(op), b, incTarget, ""); err != nil {
			t.Fatalf("release in episode one: %v", err)
		}
		// Episode two: a contains P and closes. b's earlier done item makes it the receiver.
		if _, err := w.svc.Resume(ctx, w.as(op), a, "", nil); err != nil {
			t.Fatal(err)
		}
		if holder() != a {
			t.Fatalf("setup: P held by %q, want a", holder())
		}
		if _, err := w.svc.Close(ctx, w.as(op), a, true); err != nil {
			t.Fatalf("close a: %v", err)
		}
		if holder() != b {
			t.Fatalf("P is held by %q after a closed, want the handover to b", holder())
		}
		// b's old evidence does not count.
		if err := w.svc.ReleasePrincipal(ctx, w.as(op), b, incTarget, ""); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release on the earlier episode's evidence = %v, want ErrReleaseNotReady", err)
		}
		if _, err := w.svc.Resume(ctx, w.as(op), b, "", nil); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(op), b, incTarget, ""); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release before the identity is attested again = %v, want ErrReleaseNotReady", err)
		}
		if err := w.svc.Attest(ctx, w.as(op), b, w.itemIDByKey(t, op, b, key)); err != nil {
			t.Fatalf("attest again: %v", err)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(op), b, incTarget, ""); err != nil {
			t.Fatalf("release with fresh evidence: %v", err)
		}
	})
}

// A close that is refused commits nothing: not the handover, not its audit events.
func TestIncidentRefusedCloseDoesNotHandOver(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		h := newCrossHeld(t, db)
		w, op, a, b, holder := h.w, h.op, h.a, h.b, h.holder
		if holder("usr_itarget") != a {
			t.Fatalf("setup: P held by %q, want a", holder("usr_itarget"))
		}
		before := w.correlated(t, "incident.principal_contained", b)
		// a still has Q open, so a clean close is refused; P would be handed to b first.
		if _, err := w.svc.Close(ctx, w.as(op), a, false); !errors.Is(err, service.ErrIncidentOpenItems) {
			t.Fatalf("close a with open items and no residuals = %v, want ErrIncidentOpenItems", err)
		}
		if got := holder("usr_itarget"); got != a {
			t.Fatalf("a refused close moved P to %q", got)
		}
		if got := w.correlated(t, "incident.principal_contained", b); got != before {
			t.Fatalf("a refused close wrote %d handover event(s)", got-before)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE incident_id='`+b+`' AND owner_function='account.contain' AND object_id='usr_itarget' AND outcome='done'`); n != 1 {
			t.Fatalf("a refused close reopened b's contain item (%d done rows)", n)
		}
	})
}
