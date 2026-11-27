package isolation

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Boundary negative controls for the incident owner functions. In each, the
// responder holds the capability the owner needs in the other org, so only the
// incident's own boundary refuses the item, and nothing the item names changes.

// ownerRefusedNotFound runs one owner for an item and fails unless it answers
// not found without confirming.
func (w *incidentWorld) ownerRefusedNotFound(t *testing.T, actor domain.PrincipalID, incident, owner, kind, id, scope string) {
	t.Helper()
	res, err := w.svc.Owners[owner](t.Context(), w.as(actor), authz.IncidentItem{IncidentID: incident, OwnerFunction: owner, ObjectKind: kind, ObjectID: id, Scope: scope})
	if !errors.Is(err, domain.ErrNotFound) || res == service.OwnerConfirmed {
		t.Errorf("%s on %s across the incident's boundary = %v, %v, want not found", owner, id, res, err)
	}
}

func grantOrgB(t *testing.T, db *store.DB, p domain.PrincipalID) {
	t.Helper()
	for i, c := range []string{"credential-reset", "manage-members", "manage-identities", "manage-adapters", "read"} {
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_bnd_`+string(p)+`_`+string(rune('a'+i))+`', '`+string(p)+`', '`+c+`', 'org_b', `+ts+`)`)
	}
}

// An item scoped to org B on an org A incident is refused for the tenant owners
// even though the responder holds their capabilities in org B (the tenant item's
// org must equal the incident's org).
func TestIncidentTenantOwnersRefuseAnItemOfAnotherOrg(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		grantOrgB(t, db, incResponder)
		epoch := `(SELECT credential_epoch FROM auth_instance_state WHERE id = 1)`
		execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_bnd_b', 'machine', 'workload', `+ts+`)`)
		execRaw(t, db, `INSERT INTO service_accounts (id, principal_id, org_id, project_id, name, kind, created_at, created_by) VALUES ('sa_bnd_b', 'mch_bnd_b', 'org_b', 'prj_b1', 'bnd-sa', 'workload', `+ts+`, 'usr_itarget')`)
		execRaw(t, db, `INSERT INTO machine_credentials (id, service_account_id, kind, verifier, prefix_hint, lifetime, credential_epoch, created_at, created_by) VALUES ('mcr_bnd_b', 'sa_bnd_b', 'hikyo-token', `+blobLit(db, []byte("verifier-mcr-bnd"))+`, 'hk', 'indefinite', `+epoch+`, `+ts+`, 'usr_itarget')`)
		execRaw(t, db, `INSERT INTO org_email_invitations (id, org_id, email, template, inviter_principal_id, account_id, token_verifier, credential_epoch, created_at, expires_at) VALUES ('ein_bnd_b', 'org_b', 'bnd-b@example.test', 'viewer', 'usr_irsp', NULL, `+blobLit(db, []byte("verifier-ein-bnd"))+`, 0, `+ts+`, '2099-01-01T00:00:00.000000Z')`)
		execRaw(t, db, `INSERT INTO scim_bindings(id,org_id,provider_kind,provider_id,provider_slug,provider_issuer,subject_source,connection_principal_id,created_at) VALUES ('sbn_bnd_b','org_b','oidc','sbn_bnd_b','sbn_bnd_b','https://idp.example','externalId','mch_bnd_b',`+ts+`)`)
		execRaw(t, db, `INSERT INTO scim_credentials (id, org_id, binding_id, principal_id, verifier, credential_epoch, created_at) VALUES ('scc_bnd_b','org_b','sbn_bnd_b','mch_bnd_b',`+blobLit(db, []byte("verifier-scc-bnd"))+`, `+epoch+`, `+ts+`)`)
		inc := w.openOrg(t, incResponder) // org A

		w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerMachineCredential, domain.ObjectKindMachineCredential, "mcr_bnd_b", "org_b/prj_b1")
		w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerEmailInvitation, domain.ObjectKindEmailInvitation, "ein_bnd_b", "org_b")
		w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerSCIMCredential, domain.ObjectKindSCIMCredential, "scc_bnd_b", "org_b")
		if n := queryInt(t, db, `SELECT COUNT(*) FROM machine_credentials WHERE id='mcr_bnd_b' AND revoked_at IS NULL`); n != 1 {
			t.Errorf("a refused item revoked the org B machine credential")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM org_email_invitations WHERE id='ein_bnd_b'`); n != 1 {
			t.Errorf("a refused item deleted the org B invitation")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM scim_credentials WHERE id='scc_bnd_b' AND revoked_at IS NULL`); n != 1 {
			t.Errorf("a refused item revoked the org B SCIM credential")
		}
	})
}

// A responder holding credential-reset in org B cannot contain an org B member
// through an org A incident: the member's footprint must include the incident's org.
func TestIncidentContainRefusesAMemberOutsideTheIncidentOrg(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		grantOrgB(t, db, incResponder)
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_bnd_member', 'human', `+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_bnd_member', 'usr_bnd_member', 'read', 'org_b', `+ts+`)`)
		execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_bnd_member', 'usr_bnd_member', 'bnd-member', 'Org B Member', 'bnd-member@example.test', `+ts+`, `+ts+`)`)
		inc := w.openOrg(t, incResponder) // org A
		w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerAccountContain, domain.ObjectKindAccount, "usr_bnd_member", "org_a")
		if s := w.containmentState(t, "usr_bnd_member"); s == "contained" {
			t.Fatal("an org A incident contained an org B member")
		}
		// Already contained, the member is still outside the incident: the same not
		// found, so the answer tells nothing about its containment state.
		execRaw(t, db, `UPDATE principals SET containment_state='contained' WHERE id='usr_bnd_member'`)
		w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerAccountContain, domain.ObjectKindAccount, "usr_bnd_member", "org_a")
	})
}

// An account incident revokes only the account's own delegations: one that
// belongs to another authority lies outside the incident, so the owner answers
// not found and leaves it live, as the other owners do for an object outside
// their incident.
func TestIncidentAccountIncidentLeavesAnotherAuthoritysDelegation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		insertDeveloperCredential(t, db, "devc_other", "devp_other", string(alice), "org_a", "prj_a1", "env_a1", "2099-01-01T00:00:00.000000Z")
		inc := w.openAccount(t, incResponder).Incident.ID
		res, err := w.svc.Owners[domain.OwnerDeveloperRevoke](t.Context(), w.as(incResponder), authz.IncidentItem{IncidentID: inc, OwnerFunction: domain.OwnerDeveloperRevoke, ObjectKind: domain.ObjectKindDeveloperCred, ObjectID: "devc_other", Scope: "org_a/prj_a1/env_a1"})
		if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id='devc_other' AND revoked_at IS NULL`); n != 1 {
			t.Fatalf("an account incident revoked another authority's delegation (result %v, %v)", res, err)
		}
		if !errors.Is(err, domain.ErrNotFound) || res == service.OwnerConfirmed {
			t.Errorf("another authority's delegation = %v, %v; want not found and unconfirmed", res, err)
		}
	})
}

// The delegation revoke reads the item's exact placement. An org incident has
// no authority filter, so placement alone decides: an item that names a
// delegation at another level, project or env answers not found and leaves it
// live, and the exact placement still revokes it.
func TestIncidentDeveloperRevokeRefusesAnotherPlacement(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		inc := w.openOrg(t, incResponder)
		for _, scope := range []string{"org_a", "org_a/prj_a2/env_a1", "org_a/prj_a1/env_zz"} {
			res, err := w.svc.Owners[domain.OwnerDeveloperRevoke](t.Context(), w.as(incResponder), authz.IncidentItem{IncidentID: inc, OwnerFunction: domain.OwnerDeveloperRevoke, ObjectKind: domain.ObjectKindDeveloperCred, ObjectID: "devc_it", Scope: scope})
			if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id='devc_it' AND revoked_at IS NULL`); n != 1 {
				t.Fatalf("scope %s revoked a delegation placed at org_a/prj_a1/env_a1 (result %v, %v)", scope, res, err)
			}
			if !errors.Is(err, domain.ErrNotFound) || res == service.OwnerConfirmed {
				t.Errorf("scope %s = %v, %v; want not found and unconfirmed", scope, res, err)
			}
		}
		if res := w.callOwner(t, incResponder, inc, domain.OwnerDeveloperRevoke, domain.ObjectKindDeveloperCred, "devc_it", "org_a/prj_a1/env_a1"); res != service.OwnerConfirmed {
			t.Errorf("the exact placement = %v, want confirmed", res)
		}
	})
}

// The delegation revoke reads its one row by id and never the whole table: a
// row of another org that no decoder can read does not reach it, and stays
// untouched. Only SQLite can hold such a row; PostgreSQL types the timestamps.
func TestIncidentDeveloperRevokeReadsOnlyItsOwnRow(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		if db.Engine() != store.EngineSQLite {
			t.Skip("an undecodable timestamp needs SQLite's TEXT column")
		}
		w := newIncidentWorld(t, db)
		w.realOwners()
		inc := w.openOrg(t, incResponder)
		insertDeveloperCredential(t, db, "devc_unreadable", "devp_unreadable", string(incOrgB), "org_b", "prj_b1", "env_b1", "not-a-time")
		if res := w.callOwner(t, incResponder, inc, domain.OwnerDeveloperRevoke, domain.ObjectKindDeveloperCred, "devc_it", "org_a/prj_a1/env_a1"); res != service.OwnerConfirmed {
			t.Errorf("devc_it = %v, want confirmed", res)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id='devc_unreadable' AND revoked_at IS NULL AND expires_at='not-a-time'`); n != 1 {
			t.Errorf("another org's delegation changed")
		}
	})
}

// Containing an account revokes the person's delegations by their authority and
// never reads the whole table: another person's row that no decoder can read does
// not reach it, and stays untouched. Only SQLite can hold such a row.
func TestIncidentAccountContainReadsOnlyItsOwnDelegations(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		if db.Engine() != store.EngineSQLite {
			t.Skip("an undecodable timestamp needs SQLite's TEXT column")
		}
		w := newIncidentWorld(t, db)
		w.realOwners()
		inc := w.openAccount(t, incResponder).Incident.ID
		insertDeveloperCredential(t, db, "devc_unreadable", "devp_unreadable", string(incOrgB), "org_b", "prj_b1", "env_b1", "not-a-time")
		if res := w.callOwner(t, incResponder, inc, domain.OwnerAccountContain, domain.ObjectKindAccount, string(incTarget), "org_a"); res != service.OwnerConfirmed {
			t.Errorf("account = %v, want confirmed", res)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id='devc_it' AND revoked_at IS NOT NULL`); n != 1 {
			t.Errorf("the contained account's delegation is still live")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id='devc_unreadable' AND revoked_at IS NULL AND expires_at='not-a-time'`); n != 1 {
			t.Errorf("another person's delegation changed")
		}
	})
}

// The incident closes between the caller's ledger read and the owner's
// transaction: the owner refuses with a conflict and acts on nothing.
func TestIncidentOwnersRefuseAnIncidentClosedUnderThem(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO org_email_invitations (id, org_id, email, template, inviter_principal_id, account_id, token_verifier, credential_epoch, created_at, expires_at) VALUES ('ein_closed', 'org_a', 'closed@example.test', 'viewer', 'usr_irsp', NULL, `+blobLit(db, []byte("verifier-ein-closed"))+`, 0, `+ts+`, '2099-01-01T00:00:00.000000Z')`)
		inc := w.openAccount(t, incResponder).Incident.ID
		if _, err := w.svc.Close(t.Context(), w.as(incResponder), inc, true); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct{ owner, kind, id, scope string }{
			{domain.OwnerAccountContain, domain.ObjectKindAccount, string(incTarget), "org_a"},
			{domain.OwnerDeveloperRevoke, domain.ObjectKindDeveloperCred, "devc_it", "org_a/prj_a1/env_a1"},
			{domain.OwnerEmailInvitation, domain.ObjectKindEmailInvitation, "ein_closed", "org_a"},
		} {
			res, err := w.svc.Owners[tc.owner](t.Context(), w.as(incResponder), authz.IncidentItem{IncidentID: inc, OwnerFunction: tc.owner, ObjectKind: tc.kind, ObjectID: tc.id, Scope: tc.scope})
			if !errors.Is(err, domain.ErrConflict) || res == service.OwnerConfirmed {
				t.Errorf("%s on a closed incident = %v, %v, want a conflict", tc.owner, res, err)
			}
		}
		if s := w.containmentState(t, incTarget); s == "contained" {
			t.Error("a closed incident contained the account")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id='devc_it' AND revoked_at IS NULL`); n != 1 {
			t.Error("a closed incident revoked the delegation")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM org_email_invitations WHERE id='ein_closed'`); n != 1 {
			t.Error("a closed incident deleted the invitation")
		}
	})
}

// A revoke that answers not found while the invitation still exists (a masked
// refusal, or a delete that removed nothing) must not confirm the item: the owner
// re-reads before it believes the object is gone.
func TestIncidentInvitationRevokeNotFoundWhileItStillExists(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO org_email_invitations (id, org_id, email, template, inviter_principal_id, account_id, token_verifier, credential_epoch, created_at, expires_at) VALUES ('ein_stuck', 'org_a', 'stuck@example.test', 'viewer', 'usr_irsp', NULL, `+blobLit(db, []byte("verifier-ein-stuck"))+`, 0, `+ts+`, '2099-01-01T00:00:00.000000Z')`)
		if db.Engine() == store.EnginePostgres {
			execRaw(t, db, `CREATE FUNCTION bnd_skip_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$`)
			execRaw(t, db, `CREATE TRIGGER bnd_skip_delete BEFORE DELETE ON org_email_invitations FOR EACH ROW EXECUTE FUNCTION bnd_skip_delete()`)
		} else {
			execRaw(t, db, `CREATE TRIGGER bnd_skip_delete BEFORE DELETE ON org_email_invitations BEGIN SELECT RAISE(IGNORE); END`)
		}
		inc := w.openOrg(t, incResponder)
		call := func() service.IncidentOwnerResult {
			return w.callOwner(t, incResponder, inc, domain.OwnerEmailInvitation, domain.ObjectKindEmailInvitation, "ein_stuck", "org_a")
		}
		if res := call(); res == service.OwnerConfirmed {
			t.Fatal("an invitation that still exists was confirmed gone")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM org_email_invitations WHERE id='ein_stuck'`); n != 1 {
			t.Fatal("the fixture deleted the invitation")
		}
		execRaw(t, db, `DROP TRIGGER bnd_skip_delete`+map[bool]string{true: ` ON org_email_invitations`, false: ``}[db.Engine() == store.EnginePostgres])
		if res := call(); res != service.OwnerConfirmed {
			t.Errorf("revoke after the fixture is gone = %v, want confirmed", res)
		}
	})
}

// The org and instance rotation listings skip a disabled rotation, and the
// delegation reads skip an expired one.
func TestIncidentListingsSkipDisabledRotationsAndExpiredDelegations(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		pw, user := rotationKeyPair(t, db, 40)
		disabled := r.x.create(pw, user)
		execRaw(t, db, `UPDATE rotations SET state='disabled', next_run_at=NULL WHERE id='`+disabled.ID+`'`)
		disabledKey := "rotation.halt|rotation|" + disabled.ID
		// An expired delegation of the account is neither listed nor a live one to confirm.
		insertDeveloperCredential(t, db, "devc_expired", "devp_expired", string(alice), "org_a", "prj_a1", "env_a1", time.Now().Add(-time.Hour).UTC().Format("2006-01-02T15:04:05.000000Z"))
		expiredKey := "developer_credential.revoke|developer_credential|devc_expired"

		// Org incident (the org listing).
		org := r.w.openOrg(t, incResponder)
		items := r.w.itemMap(t, incResponder, org)
		if _, ok := items[disabledKey]; ok {
			t.Errorf("the org listing lists a disabled rotation: %v", items)
		}
		if _, ok := items[r.key()]; !ok {
			t.Errorf("the org listing lost the live rotation: %v", items)
		}
		if _, ok := items[expiredKey]; ok {
			t.Errorf("the delegation listing lists an expired delegation: %v", items)
		}
		res := r.w.callOwner(t, incResponder, org, domain.OwnerDeveloperRevoke, domain.ObjectKindDeveloperCred, "devc_expired", "org_a/prj_a1/env_a1")
		if res != service.OwnerConfirmed {
			t.Errorf("an expired delegation = %v, want confirmed (it holds no authority)", res)
		}

		// Instance incident (the instance listing): the account spans two orgs.
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_bnd_alice_b', '`+string(alice)+`', 'read', 'org_b', `+ts+`)`)
		v, err := r.w.svc.Open(t.Context(), r.w.as(incInstance), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: alice})
		if err != nil {
			t.Fatal(err)
		}
		items = r.w.itemMap(t, incInstance, v.Incident.ID)
		if _, ok := items[disabledKey]; ok {
			t.Errorf("the instance listing lists a disabled rotation: %v", items)
		}
		if _, ok := items[r.key()]; !ok {
			t.Errorf("the instance listing lost the live rotation: %v", items)
		}
		if strings.Contains(strings.Join(mapKeys(items), " "), "devc_expired") {
			t.Errorf("the instance incident lists an expired delegation: %v", items)
		}
	})
}

// Owner decision (gone means gone from the instance): an item scoped to the
// incident's own org that names another org's object answers not found and leaves
// that object live, though the responder holds nothing in the other org. Deleted
// from the instance, the same ids confirm.
func TestIncidentOwnersKeepAnObjectOfAnotherOrgLive(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		epoch := `(SELECT credential_epoch FROM auth_instance_state WHERE id = 1)`
		seedIncidentLeases(t, db) // lse_it_b1 is active in org_b/prj_b1/env_b1
		execRaw(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_xo_b','org_b','prj_b1','forgejo','https://git.example','usr_bob','active',`+ts+`)`)
		execRaw(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_xo_b','org_b','prj_b1','env_b1','adp_xo_b','repository','acme','app',41,'',1,'active','converged',`+ts+`)`)
		execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_xo_b', 'machine', 'workload', `+ts+`)`)
		execRaw(t, db, `INSERT INTO service_accounts (id, principal_id, org_id, project_id, name, kind, created_at, created_by) VALUES ('sa_xo_b', 'mch_xo_b', 'org_b', 'prj_b1', 'xo-sa', 'workload', `+ts+`, 'usr_itarget')`)
		execRaw(t, db, `INSERT INTO machine_credentials (id, service_account_id, kind, verifier, prefix_hint, lifetime, credential_epoch, created_at, created_by) VALUES ('mcr_xo_b', 'sa_xo_b', 'hikyo-token', `+blobLit(db, []byte("verifier-mcr-xo"))+`, 'hk', 'indefinite', `+epoch+`, `+ts+`, 'usr_itarget')`)
		execRaw(t, db, `INSERT INTO org_email_invitations (id, org_id, email, template, inviter_principal_id, account_id, token_verifier, credential_epoch, created_at, expires_at) VALUES ('ein_xo_b', 'org_b', 'xo-b@example.test', 'viewer', 'usr_irsp', NULL, `+blobLit(db, []byte("verifier-ein-xo"))+`, 0, `+ts+`, '2099-01-01T00:00:00.000000Z')`)
		execRaw(t, db, `INSERT INTO scim_bindings(id,org_id,provider_kind,provider_id,provider_slug,provider_issuer,subject_source,connection_principal_id,created_at) VALUES ('sbn_xo_b','org_b','oidc','sbn_xo_b','sbn_xo_b','https://idp.example','externalId','mch_xo_b',`+ts+`)`)
		execRaw(t, db, `INSERT INTO scim_credentials (id, org_id, binding_id, principal_id, verifier, credential_epoch, created_at) VALUES ('scc_xo_b','org_b','sbn_xo_b','mch_xo_b',`+blobLit(db, []byte("verifier-scc-xo"))+`, `+epoch+`, `+ts+`)`)
		inc := w.openOrg(t, incResponder) // org A
		items := []struct{ owner, kind, id, scope, del string }{
			{domain.OwnerMachineCredential, domain.ObjectKindMachineCredential, "mcr_xo_b", "org_a/prj_a1", `DELETE FROM machine_credentials WHERE id='mcr_xo_b'`},
			{domain.OwnerEmailInvitation, domain.ObjectKindEmailInvitation, "ein_xo_b", "org_a", `DELETE FROM org_email_invitations WHERE id='ein_xo_b'`},
			{domain.OwnerSCIMCredential, domain.ObjectKindSCIMCredential, "scc_xo_b", "org_a", `DELETE FROM scim_credentials WHERE id='scc_xo_b'`},
			{domain.OwnerLeaseRevoke, domain.ObjectKindLease, "lse_it_b1", "org_a/prj_a1/env_a1", `DELETE FROM dynamic_leases WHERE id='lse_it_b1'`},
			{domain.OwnerAdapterTargetPause, domain.ObjectKindAdapterTarget, "tgt_xo_b", "org_a/prj_a1", `DELETE FROM adapter_targets WHERE id='tgt_xo_b'`},
		}
		for _, c := range items {
			w.ownerRefusedNotFound(t, incResponder, inc, c.owner, c.kind, c.id, c.scope)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM machine_credentials WHERE id='mcr_xo_b' AND revoked_at IS NULL`); n != 1 {
			t.Errorf("the org B machine credential was revoked")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM org_email_invitations WHERE id='ein_xo_b'`); n != 1 {
			t.Errorf("the org B invitation was deleted")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM scim_credentials WHERE id='scc_xo_b' AND revoked_at IS NULL`); n != 1 {
			t.Errorf("the org B SCIM credential was revoked")
		}
		if got := queryString(t, db, `SELECT state FROM dynamic_leases WHERE id='lse_it_b1'`); got != "active" {
			t.Errorf("the org B lease changed: %s", got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_xo_b' AND paused_at IS NULL`); n != 1 {
			t.Errorf("the org B target was paused")
		}
		for _, c := range items {
			execRaw(t, db, c.del)
			if res := w.callOwner(t, incResponder, inc, c.owner, c.kind, c.id, c.scope); res != service.OwnerConfirmed {
				t.Errorf("%s on %s deleted from the instance = %v, want confirmed", c.owner, c.id, res)
			}
		}
	})
}

// Owner decision (gone means gone from the instance): another principal's live
// session named on an account incident answers not found and stays live. Deleted,
// it confirms.
func TestIncidentSessionOwnerLeavesAnotherPrincipalsSessionLive(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
			`VALUES ('ses_xo_alice', '`+string(alice)+`', `+blobLit(db, []byte("verifier-xo-alice"))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)
		inc := w.openAccount(t, incResponder).Incident.ID // incTarget's incident
		w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerSessionRevoke, domain.ObjectKindSession, "ses_xo_alice", "instance")
		if n := queryInt(t, db, `SELECT COUNT(*) FROM sessions WHERE id='ses_xo_alice'`); n != 1 {
			t.Fatalf("another principal's session was revoked")
		}
		execRaw(t, db, `DELETE FROM sessions WHERE id='ses_xo_alice'`)
		if res := w.callOwner(t, incResponder, inc, domain.OwnerSessionRevoke, domain.ObjectKindSession, "ses_xo_alice", "instance"); res != service.OwnerConfirmed {
			t.Errorf("a session gone from the instance = %v, want confirmed", res)
		}
	})
}
