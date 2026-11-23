package isolation

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Incident owner functions (#1039 S3, #1471): the real owners driven through
// the ledger on both engines. Only a re-read that shows the target state writes
// done, and release never revives anything.

const keyContain = "account.contain|account|usr_itarget"

func (w *incidentWorld) realOwners() {
	w.svc.Owners = service.NewIncidentOwners(w.svc, service.IncidentOwnerServices{})
}

func (w *incidentWorld) resume(t *testing.T, actor domain.PrincipalID, id string) service.ResumeResult {
	t.Helper()
	res, err := w.svc.Resume(t.Context(), w.as(actor), id)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	return res
}

func TestIncidentOwnersContainAccountEndToEnd(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		v := w.openAccount(t, incResponder)
		id := v.Incident.ID
		if got := w.itemMap(t, incResponder, id)[keyContain]; got != "pending/" {
			t.Fatalf("contain item = %q, want pending", got)
		}

		res := w.resume(t, incResponder, id)
		if res.Confirmed < 3 {
			t.Fatalf("resume = %+v, want contain, session and delegation confirmed", res)
		}
		got := w.itemMap(t, incResponder, id)
		wantItems(t, got, map[string]string{keyContain: "done/", keySession: "done/", keyDevCred: "done/"})

		if s := queryString(t, db, `SELECT containment_state FROM principals WHERE id='`+string(incTarget)+`'`); s != "contained" {
			t.Errorf("containment_state = %q, want contained", s)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM sessions WHERE principal_id='`+string(incTarget)+`'`); n != 0 {
			t.Errorf("%d sessions survive containment", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id='devc_it' AND revoked_at IS NULL`); n != 0 {
			t.Errorf("developer delegation survives containment")
		}
		// Containment keeps grants: release never regrants (ADR O1, O4).
		if n := queryInt(t, db, `SELECT COUNT(*) FROM grants WHERE principal_id='`+string(incTarget)+`'`); n != 2 {
			t.Errorf("grants = %d, want the 2 originals kept", n)
		}

		// Done is final: a second resume runs no owner and a re-run owner is a no-op.
		if again := w.resume(t, incResponder, id); again.Examined != 0 {
			t.Errorf("second resume = %+v, want nothing examined", again)
		}
	})
}

func TestIncidentOwnerRefusesSelfContainment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_iresp', '`+string(incResponder)+`', 'it-resp', 'Responder', 'it-resp@example.test', `+ts+`, `+ts+`)`)
		v, err := w.svc.Open(t.Context(), w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incResponder})
		if err != nil {
			t.Skipf("opening an incident on oneself is refused earlier: %v", err)
		}
		w.resume(t, incResponder, v.Incident.ID)
		if s := queryString(t, db, `SELECT containment_state FROM principals WHERE id='`+string(incResponder)+`'`); s == "contained" {
			t.Fatal("an operator contained their own account")
		}
		key := "account.contain|account|" + string(incResponder)
		if got := w.itemMap(t, incResponder, v.Incident.ID)[key]; strings.HasPrefix(got, "done") {
			t.Errorf("self-containment item = %q, want not done", got)
		}
	})
}

func TestIncidentOwnerRefusesLastManager(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		// Make the target the only manage-members holder reachable in org A.
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE capability='manage-members')`)
		execRaw(t, db, `DELETE FROM grants WHERE capability='manage-members'`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_inc_mgr', '`+string(incTarget)+`', 'manage-members', 'org_a', `+ts+`)`)
		v := w.openAccount(t, incResponder)
		w.resume(t, incResponder, v.Incident.ID)
		if s := queryString(t, db, `SELECT containment_state FROM principals WHERE id='`+string(incTarget)+`'`); s == "contained" {
			t.Fatal("the last manage-members holder was contained")
		}
		if got := w.itemMap(t, incResponder, v.Incident.ID)[keyContain]; strings.HasPrefix(got, "done") {
			t.Errorf("last-manager item = %q, want not done", got)
		}
	})
}

func TestIncidentOwnerHaltsRotationsAndReleaseNeverResumes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_ialice', '`+string(alice)+`', 'it-alice', 'Alice', 'it-alice@example.test', `+ts+`, `+ts+`)`)
		v, err := w.svc.Open(t.Context(), w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: alice})
		if err != nil {
			t.Fatal(err)
		}
		key := "rotation.halt|rotation|" + rot.ID
		if got := w.itemMap(t, incResponder, v.Incident.ID)[key]; got != "pending/" {
			t.Fatalf("rotation item = %q, want pending (items: %v)", got, w.itemMap(t, incResponder, v.Incident.ID))
		}
		w.resume(t, incResponder, v.Incident.ID)
		if got := w.itemMap(t, incResponder, v.Incident.ID)[key]; got != "done/" {
			t.Fatalf("rotation item = %q, want done", got)
		}
		state := func() string { return queryString(t, db, `SELECT state FROM rotations WHERE id='`+rot.ID+`'`) }
		if state() != store.RotationStateHalted {
			t.Fatalf("rotation state = %q, want halted", state())
		}
		if r := queryString(t, db, `SELECT halt_reason FROM rotations WHERE id='`+rot.ID+`'`); r != "authority_lost" {
			t.Errorf("halt reason = %q, want authority_lost", r)
		}
		// Release of the principal never resumes the rotation.
		execRaw(t, db, `UPDATE principals SET containment_state='none' WHERE id='`+string(alice)+`'`)
		x.drain()
		if state() != store.RotationStateHalted {
			t.Errorf("release resumed the rotation: %q", state())
		}
	})
}
