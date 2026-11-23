package isolation

import (
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Incident owner functions (#1039 S3, #1471): the real owners driven through
// the ledger on both engines. Only a re-read that shows the target state writes
// done, and release never revives anything.

const keyContain = "account.contain|account|usr_itarget"

func (w *incidentWorld) realOwners() {
	w.svc.Owners = service.NewIncidentOwners(w.svc, service.IncidentOwnerServices{
		Identities: &service.Identities{DB: w.db},
		Invites:    &service.EmailInvitations{DB: w.db},
		SCIM:       &service.SCIM{DB: w.db},
		Dynamic:    &service.Dynamic{DB: w.db},
		Adapters:   &service.Adapters{DB: w.db},
	})
}

func (w *incidentWorld) resume(t *testing.T, actor domain.PrincipalID, id string) service.ResumeResult {
	t.Helper()
	res, err := w.svc.Resume(t.Context(), w.as(actor), id)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	return res
}

// callOwner runs one owner function directly for an item an open incident of the
// actor's org carries. The ledger row is not needed: an owner trusts only the
// incident id and the object it names.
func (w *incidentWorld) callOwner(t *testing.T, actor domain.PrincipalID, incident, owner, kind, id, scope string) service.IncidentOwnerResult {
	t.Helper()
	fn := w.svc.Owners[owner]
	if fn == nil {
		t.Fatalf("no owner registered for %s", owner)
	}
	res, err := fn(t.Context(), w.as(actor), authz.IncidentItem{IncidentID: incident, OwnerFunction: owner, ObjectKind: kind, ObjectID: id, Scope: scope})
	if err != nil {
		t.Fatalf("%s on %s: %v", owner, id, err)
	}
	return res
}

func (w *incidentWorld) openOrg(t *testing.T, actor domain.PrincipalID) string {
	t.Helper()
	v, err := w.svc.Open(t.Context(), w.as(actor), service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgA}})
	if err != nil {
		t.Fatalf("open org incident: %v", err)
	}
	return v.Incident.ID
}

func (w *incidentWorld) correlated(t *testing.T, typ, incident string) int64 {
	t.Helper()
	q := fmt.Sprintf(`SELECT (SELECT COUNT(*) FROM audit_tenant_events WHERE type='%s' AND correlation_id='%s') + (SELECT COUNT(*) FROM audit_instance_events WHERE type='%s' AND correlation_id='%s')`, typ, incident, typ, incident)
	return queryInt(t, w.db, q)
}

func (w *incidentWorld) containmentState(t *testing.T, p domain.PrincipalID) string {
	t.Helper()
	return queryString(t, w.db, `SELECT containment_state FROM principals WHERE id='`+string(p)+`'`)
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
		wantItems(t, w.itemMap(t, incResponder, id), map[string]string{keyContain: "done/", keySession: "done/", keyDevCred: "done/"})

		if s := w.containmentState(t, incTarget); s != "contained" {
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
		if again := w.resume(t, incResponder, id); again.Examined != 0 {
			t.Errorf("second resume = %+v, want nothing examined", again)
		}
		// The delegation revocation joins the incident by correlation (ADR, Audit trail).
		if n := w.correlated(t, "identity.developer_credential_revoked", id); n == 0 {
			t.Errorf("no developer revocation event carries the incident id as correlation")
		}
	})
}

// A refusal leaves the contain item open and the account uncontained: pending,
// never done, and the resume counts it as failed. The session and delegation
// items are independent owners and complete on their own (ADR, Concurrency).
func TestIncidentOwnerRefusesSelfContainment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_iresp', '`+string(incResponder)+`', 'it-resp', 'Responder', 'it-resp@example.test', `+ts+`, `+ts+`)`)
		execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
			`VALUES ('ses_it_resp', '`+string(incResponder)+`', `+blobLit(db, []byte("verifier-it-resp"))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)
		v, err := w.svc.Open(t.Context(), w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incResponder})
		if err != nil {
			t.Fatalf("open on own account: %v", err)
		}
		res := w.resume(t, incResponder, v.Incident.ID)
		if res.Failed != 1 {
			t.Errorf("resume = %+v, want the contain item failed", res)
		}
		if s := w.containmentState(t, incResponder); s == "contained" {
			t.Fatal("an operator contained their own account")
		}
		if got := w.itemMap(t, incResponder, v.Incident.ID)["account.contain|account|"+string(incResponder)]; got != "pending/" {
			t.Errorf("self-containment item = %q, want pending", got)
		}
		// The refusal is the contain owner's alone: session and delegation items are
		// independent owners and run on their own (ADR, Concurrency and fencing).
		if n := queryInt(t, db, `SELECT COUNT(*) FROM grants WHERE principal_id='`+string(incResponder)+`'`); n == 0 {
			t.Errorf("a refused containment removed the operator's grants")
		}
	})
}

func TestIncidentOwnerRefusesLastManager(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		// Make the target the only manage-members holder.
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE capability='manage-members')`)
		execRaw(t, db, `DELETE FROM grants WHERE capability='manage-members'`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_inc_mgr', '`+string(incTarget)+`', 'manage-members', 'org_a', `+ts+`)`)
		v := w.openAccount(t, incResponder)
		res := w.resume(t, incResponder, v.Incident.ID)
		if res.Failed != 1 {
			t.Errorf("resume = %+v, want the contain item failed", res)
		}
		if s := w.containmentState(t, incTarget); s == "contained" {
			t.Fatal("the last manage-members holder was contained")
		}
		got := w.itemMap(t, incResponder, v.Incident.ID)
		// Only the contain owner refuses: the session and delegation items are
		// independent owners, so they complete and the grants stay (ADR, Concurrency).
		wantItems(t, got, map[string]string{keyContain: "pending/", keySession: "done/", keyDevCred: "done/"})
		if n := queryInt(t, db, `SELECT COUNT(*) FROM grants WHERE principal_id='`+string(incTarget)+`'`); n == 0 {
			t.Errorf("a refused containment removed the target's grants")
		}
	})
}

// ---- rotation.halt (#1471) -------------------------------------------------

type rotationIncident struct {
	x   *rotExec
	w   *incidentWorld
	rot service.RotationView
}

func newRotationIncident(t *testing.T, db *store.DB) rotationIncident {
	t.Helper()
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	w := newIncidentWorld(t, db)
	w.realOwners()
	execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_ialice', '`+string(alice)+`', 'it-alice', 'Alice', 'it-alice@example.test', `+ts+`, `+ts+`)`)
	return rotationIncident{x: x, w: w, rot: rot}
}

func (r rotationIncident) state(t *testing.T) string {
	t.Helper()
	return queryString(t, r.w.db, `SELECT state FROM rotations WHERE id='`+r.rot.ID+`'`)
}

func (r rotationIncident) key() string { return "rotation.halt|rotation|" + r.rot.ID }

func (r rotationIncident) scope() string {
	return string(orgA) + "/" + string(prjA1) + "/" + string(r.x.env)
}

func TestIncidentOwnerHaltsRotationsAndReleaseNeverResumes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		v, err := r.w.svc.Open(t.Context(), r.w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: alice})
		if err != nil {
			t.Fatal(err)
		}
		id := v.Incident.ID
		if got := r.w.itemMap(t, incResponder, id)[r.key()]; got != "pending/" {
			t.Fatalf("rotation item = %q, want pending", got)
		}
		r.w.resume(t, incResponder, id)
		if got := r.w.itemMap(t, incResponder, id)[r.key()]; got != "done/" {
			t.Fatalf("rotation item = %q, want done", got)
		}
		if r.state(t) != store.RotationStateHalted {
			t.Fatalf("rotation state = %q, want halted", r.state(t))
		}
		if reason := queryString(t, db, `SELECT halt_reason FROM rotations WHERE id='`+r.rot.ID+`'`); reason != "incident_contained" {
			t.Errorf("halt reason = %q, want incident_contained", reason)
		}
		// The halt event joins the incident by correlation (ADR, Audit trail).
		if n := r.w.correlated(t, "rotation.configured", id); n == 0 {
			t.Errorf("the rotation halt event does not carry the incident id as correlation")
		}
		// Release of the principal never resumes the rotation. The release is a raw
		// UPDATE because the release service is a later slice; it proves only that the
		// executor does not resume a halted rotation on its own.
		execRaw(t, db, `UPDATE principals SET containment_state='none' WHERE id='`+string(alice)+`'`)
		r.x.advance(25 * 3600 * 1e9)
		r.x.drain()
		if r.state(t) != store.RotationStateHalted {
			t.Errorf("release resumed the rotation: %q", r.state(t))
		}
	})
}

// An org incident inherits the rotations of the accounts it selects (#1471 B).
func TestIncidentOrgListsAndHaltsSelectedMembersRotations(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		id := r.w.openOrg(t, incResponder)
		if got := r.w.itemMap(t, incResponder, id)[r.key()]; got != "pending/" {
			t.Fatalf("org incident rotation item = %q, want pending (items: %v)", got, r.w.itemMap(t, incResponder, id))
		}
		r.w.resume(t, incResponder, id)
		if r.state(t) != store.RotationStateHalted {
			t.Errorf("rotation state = %q, want halted", r.state(t))
		}
	})
}

func TestIncidentRotationHaltNegativeControls(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		w := r.w
		run := func(actor domain.PrincipalID, incident string) service.IncidentOwnerResult {
			return w.callOwner(t, actor, incident, domain.OwnerRotationHalt, domain.ObjectKindRotation, r.rot.ID, r.scope())
		}
		// The authority is alice, not the incident's account: refused, untouched.
		other, err := w.svc.Open(t.Context(), w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incTarget})
		if err != nil {
			t.Fatal(err)
		}
		if res, err := w.svc.Owners[domain.OwnerRotationHalt](t.Context(), w.as(incResponder), authz.IncidentItem{IncidentID: other.Incident.ID, OwnerFunction: domain.OwnerRotationHalt, ObjectKind: domain.ObjectKindRotation, ObjectID: r.rot.ID, Scope: r.scope()}); err == nil || res == service.OwnerConfirmed {
			t.Errorf("a rotation of another authority = %v, %v, want refused", res, err)
		}
		if r.state(t) != store.RotationStateActive {
			t.Fatalf("a refused halt changed the rotation: %q", r.state(t))
		}
		// An org B responder cannot reach an org A incident or its rotation.
		acc, err := w.svc.Open(t.Context(), w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: alice})
		if err != nil {
			t.Fatal(err)
		}
		if res, err := w.svc.Owners[domain.OwnerRotationHalt](t.Context(), w.as(incOrgB), authz.IncidentItem{IncidentID: acc.Incident.ID, OwnerFunction: domain.OwnerRotationHalt, ObjectKind: domain.ObjectKindRotation, ObjectID: r.rot.ID, Scope: r.scope()}); err == nil || res == service.OwnerConfirmed {
			t.Errorf("an org B responder = %v, %v, want refused", res, err)
		}
		if r.state(t) != store.RotationStateActive {
			t.Fatalf("a refused halt changed the rotation: %q", r.state(t))
		}
		// A restore hold stays open: the rotation is not halted.
		execRaw(t, db, `UPDATE rotations SET restore_hold=TRUE WHERE id='`+r.rot.ID+`'`)
		if res := run(incResponder, acc.Incident.ID); res == service.OwnerConfirmed {
			t.Errorf("a restore-held rotation was confirmed")
		}
		execRaw(t, db, `UPDATE rotations SET restore_hold=FALSE WHERE id='`+r.rot.ID+`'`)
		// A paused rotation is halted too, and a retry is idempotent.
		execRaw(t, db, `UPDATE rotations SET state='paused', next_run_at=NULL WHERE id='`+r.rot.ID+`'`)
		if res := run(incResponder, acc.Incident.ID); res != service.OwnerConfirmed || r.state(t) != store.RotationStateHalted {
			t.Errorf("paused rotation: result %v state %q, want confirmed and halted", res, r.state(t))
		}
		if res := run(incResponder, acc.Incident.ID); res != service.OwnerConfirmed {
			t.Errorf("retry = %v, want confirmed", res)
		}
		// A deleted rotation is confirmed: it schedules nothing.
		if res := w.callOwner(t, incResponder, acc.Incident.ID, domain.OwnerRotationHalt, domain.ObjectKindRotation, "rot_missing", r.scope()); res != service.OwnerConfirmed {
			t.Errorf("a missing rotation = %v, want confirmed", res)
		}
	})
}

// ---- tenant owners (M4) ----------------------------------------------------
//
// Each owner is run directly, for an item of an open org incident: confirmed on
// a live object, confirmed again on retry, confirmed on one already gone, and
// blocked, with the object untouched, for an operator without the owner's own
// capability (incLimited holds credential-reset only).

func TestIncidentOwnerMachineCredential(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO machine_credentials (id, service_account_id, kind, verifier, prefix_hint, lifetime, credential_epoch, created_at, created_by) VALUES ('mcr_it', 'sa_it', 'hikyo-token', `+blobLit(db, []byte("verifier-mcr-it"))+`, 'hk', 'indefinite', (SELECT credential_epoch FROM auth_instance_state WHERE id = 1), `+ts+`, 'usr_itarget')`)
		inc := w.openOrg(t, incResponder)
		limited := w.openOrg(t, incLimited)
		const scope = "org_a/prj_a1"
		call := func(actor domain.PrincipalID, incident, id string) service.IncidentOwnerResult {
			return w.callOwner(t, actor, incident, domain.OwnerMachineCredential, domain.ObjectKindMachineCredential, id, scope)
		}
		live := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM machine_credentials WHERE id='mcr_it' AND revoked_at IS NULL`)
		}
		if res := call(incLimited, limited, "mcr_it"); res != service.OwnerBlocked || live() != 1 {
			t.Fatalf("limited operator = %v live=%d, want blocked and untouched", res, live())
		}
		if res := call(incResponder, inc, "mcr_it"); res != service.OwnerConfirmed || live() != 0 {
			t.Fatalf("revoke = %v live=%d, want confirmed and revoked", res, live())
		}
		if res := call(incResponder, inc, "mcr_it"); res != service.OwnerConfirmed {
			t.Errorf("retry = %v, want confirmed", res)
		}
		if res := call(incResponder, inc, "mcr_missing"); res != service.OwnerConfirmed {
			t.Errorf("already gone = %v, want confirmed", res)
		}
	})
}

func TestIncidentOwnerEmailInvitation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO org_email_invitations (id, org_id, email, template, inviter_principal_id, account_id, token_verifier, credential_epoch, created_at, expires_at) VALUES ('ein_it', 'org_a', 'inv-it@example.test', 'viewer', 'usr_irsp', NULL, `+blobLit(db, []byte("verifier-ein-it"))+`, 0, `+ts+`, '2099-01-01T00:00:00.000000Z')`)
		inc := w.openOrg(t, incResponder)
		limited := w.openOrg(t, incLimited)
		call := func(actor domain.PrincipalID, incident, id string) service.IncidentOwnerResult {
			return w.callOwner(t, actor, incident, domain.OwnerEmailInvitation, domain.ObjectKindEmailInvitation, id, "org_a")
		}
		present := func() int64 { return queryInt(t, db, `SELECT COUNT(*) FROM org_email_invitations WHERE id='ein_it'`) }
		if res := call(incLimited, limited, "ein_it"); res != service.OwnerBlocked || present() != 1 {
			t.Fatalf("limited operator = %v present=%d, want blocked and untouched", res, present())
		}
		if res := call(incResponder, inc, "ein_it"); res != service.OwnerConfirmed || present() != 0 {
			t.Fatalf("revoke = %v present=%d, want confirmed and deleted", res, present())
		}
		if res := call(incResponder, inc, "ein_it"); res != service.OwnerConfirmed {
			t.Errorf("retry = %v, want confirmed", res)
		}
		if res := call(incResponder, inc, "ein_missing"); res != service.OwnerConfirmed {
			t.Errorf("already gone = %v, want confirmed", res)
		}
	})
}

func TestIncidentOwnerSCIMCredential(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO scim_bindings(id,org_id,provider_kind,provider_id,provider_slug,provider_issuer,subject_source,connection_principal_id,created_at) VALUES ('sbn_it','org_a','oidc','sbn_it','sbn_it','https://idp.example','externalId','mch_it',`+ts+`)`)
		execRaw(t, db, `INSERT INTO scim_credentials (id, org_id, binding_id, principal_id, verifier, credential_epoch, created_at) VALUES ('scc_it','org_a','sbn_it','mch_it',`+blobLit(db, []byte("verifier-scc-it"))+`, (SELECT credential_epoch FROM auth_instance_state WHERE id = 1), `+ts+`)`)
		inc := w.openOrg(t, incResponder)
		limited := w.openOrg(t, incLimited)
		call := func(actor domain.PrincipalID, incident, id string) service.IncidentOwnerResult {
			return w.callOwner(t, actor, incident, domain.OwnerSCIMCredential, domain.ObjectKindSCIMCredential, id, "org_a")
		}
		live := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM scim_credentials WHERE id='scc_it' AND revoked_at IS NULL`)
		}
		if res := call(incLimited, limited, "scc_it"); res != service.OwnerBlocked || live() != 1 {
			t.Fatalf("limited operator = %v live=%d, want blocked and untouched", res, live())
		}
		if res := call(incResponder, inc, "scc_it"); res != service.OwnerConfirmed || live() != 0 {
			t.Fatalf("revoke = %v live=%d, want confirmed and revoked", res, live())
		}
		if res := call(incResponder, inc, "scc_it"); res != service.OwnerConfirmed {
			t.Errorf("retry = %v, want confirmed", res)
		}
		if res := call(incResponder, inc, "scc_missing"); res != service.OwnerConfirmed {
			t.Errorf("already gone = %v, want confirmed", res)
		}
	})
}

// A lease is revoked by the provider worker, so the owner only queues it: an
// active lease stays unconfirmed (revoking) and a retry queues nothing more.
func TestIncidentOwnerLease(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		seedIncidentLeases(t, db)
		inc := w.openOrg(t, incResponder)
		limited := w.openOrg(t, incLimited)
		const scope = "org_a/prj_a1/env_a1"
		call := func(actor domain.PrincipalID, incident, id string) service.IncidentOwnerResult {
			return w.callOwner(t, actor, incident, domain.OwnerLeaseRevoke, domain.ObjectKindLease, id, scope)
		}
		state := func(id string) string {
			return queryString(t, db, `SELECT state FROM dynamic_leases WHERE id='`+id+`'`)
		}
		if res := call(incLimited, limited, "lse_it_01"); res != service.OwnerBlocked || state("lse_it_01") != "active" {
			t.Fatalf("limited operator = %v state=%s, want blocked and untouched", res, state("lse_it_01"))
		}
		if res := call(incResponder, inc, "lse_it_01"); res == service.OwnerConfirmed || state("lse_it_01") != "revoking" {
			t.Fatalf("active lease = %v state=%s, want queued (unconfirmed, revoking)", res, state("lse_it_01"))
		}
		if res := call(incResponder, inc, "lse_it_01"); res == service.OwnerConfirmed || state("lse_it_01") != "revoking" {
			t.Errorf("retry = %v state=%s, want still revoking and not confirmed", res, state("lse_it_01"))
		}
		if res := call(incResponder, inc, "lse_it_02"); res != service.OwnerConfirmed {
			t.Errorf("a revoked lease = %v, want confirmed", res)
		}
		if res := call(incResponder, inc, "lse_it_missing"); res != service.OwnerConfirmed {
			t.Errorf("already gone = %v, want confirmed", res)
		}
	})
}

func TestIncidentOwnerAdapterTargetPause(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_it','org_a','prj_a1','forgejo','https://git.example','usr_alice','active',`+ts+`)`)
		execRaw(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_it','org_a','prj_a1','env_a1','adp_it','repository','acme','app',41,'',1,'active','converged',`+ts+`)`)
		inc := w.openOrg(t, incResponder)
		limited := w.openOrg(t, incLimited)
		call := func(actor domain.PrincipalID, incident, id string) service.IncidentOwnerResult {
			return w.callOwner(t, actor, incident, domain.OwnerAdapterTargetPause, domain.ObjectKindAdapterTarget, id, "org_a/prj_a1")
		}
		paused := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_it' AND paused_at IS NOT NULL`)
		}
		if res := call(incLimited, limited, "tgt_it"); res != service.OwnerBlocked || paused() != 0 {
			t.Fatalf("limited operator = %v paused=%d, want blocked and untouched", res, paused())
		}
		if res := call(incResponder, inc, "tgt_it"); res != service.OwnerConfirmed || paused() != 1 {
			t.Fatalf("pause = %v paused=%d, want confirmed and paused", res, paused())
		}
		if res := call(incResponder, inc, "tgt_it"); res != service.OwnerConfirmed {
			t.Errorf("retry = %v, want confirmed", res)
		}
		if res := call(incResponder, inc, "tgt_missing"); res != service.OwnerConfirmed {
			t.Errorf("already gone = %v, want confirmed", res)
		}
	})
}

// The rotation listing spends the lookup budget: past it a member's rotations
// are an open enumeration_truncated row, never silently dropped (#1471, S5). The
// budget is raised until the member is reached but its rotation page is not.
func TestIncidentRotationListingIsBoundedByTheLookupBudget(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *store.DB{"sqlite": openSQLite, "postgres": openPostgres} {
		t.Run(name, func(t *testing.T) {
			_ = open(t) // an engine the run does not configure skips here, not per budget
			for budget := 3; budget <= 30; budget++ {
				found := false
				t.Run(fmt.Sprint(budget), func(t *testing.T) {
					r := newRotationIncident(t, seededDB(t, open))
					r.w.svc.MaxLookups = budget
					id := r.w.openOrg(t, incResponder)
					got := r.w.itemMap(t, incResponder, id)
					_, listed := got[r.key()]
					_, truncated := got["rotation.halt|enumeration|"+string(alice)]
					if listed && truncated {
						t.Errorf("rotation both listed and truncated: %v", got)
					}
					found = truncated
					if !listed && !truncated && !hasTruncatedRow(got) {
						t.Errorf("budget %d dropped the rotation silently: %v", budget, got)
					}
				})
				if found {
					return
				}
			}
			t.Fatal("no budget truncated the rotation listing")
		})
	}
}

// A member the lookup budget cannot reach is not silently skipped: each human
// member is either listed as an account item or carries an open rotation
// truncation row, at every budget (CodeRabbit on #1538).
func TestIncidentOrgTruncationNamesEveryUnreachedMember(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		sawUnreached := false
		for budget := 3; budget <= 30; budget++ {
			r.w.svc.MaxLookups = budget
			id := r.w.openOrg(t, incResponder)
			got := r.w.itemMap(t, incResponder, id)
			for _, p := range []domain.PrincipalID{alice, incTarget} {
				_, listed := got["account.contain|account|"+string(p)]
				_, truncated := got["rotation.halt|enumeration|"+string(p)]
				if !listed && !truncated {
					t.Errorf("budget %d: member %s neither listed nor marked truncated: %v", budget, p, got)
				}
				if !listed {
					sawUnreached = true
				}
			}
		}
		if !sawUnreached {
			t.Fatal("no budget left a member unreached")
		}
	})
}

func hasTruncatedRow(items map[string]string) bool {
	for _, v := range items {
		if v == "unknown/enumeration_truncated" {
			return true
		}
	}
	return false
}
