package isolation

import (
	"context"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Each test here pins one thing an owner reaches or writes: what its item
// selects, where its event lands, and which row its reader returns.

// resumeOwner runs one owner the way Resume does, with the incident id as the
// audit correlation of every event the owner writes.
func (w *incidentWorld) resumeOwner(t *testing.T, actor domain.PrincipalID, incident, owner, kind, id, scope string) service.IncidentOwnerResult {
	t.Helper()
	wire := audit.FromContext(t.Context())
	wire.CorrelationID = incident
	res, err := w.svc.Owners[owner](audit.WithContext(t.Context(), wire), w.as(actor), authz.IncidentItem{IncidentID: incident, OwnerFunction: owner, ObjectKind: kind, ObjectID: id, Scope: scope})
	if err != nil {
		t.Fatalf("%s on %s: %v", owner, id, err)
	}
	return res
}

// The session owner finishes a revocation as the account holder's own explicit
// revocation does: a delegation minted from the session after the incident opened
// ends, and exactly one session event joins the incident by correlation, however
// often the item runs. A delegation from another session stays live.
func TestIncidentSessionOwnerEndsTheSessionsDelegationsAndRecordsItsEvent(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
			`VALUES ('ses_prov', '`+string(incTarget)+`', `+blobLit(db, []byte("verifier-ses-prov"))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)
		inc := w.openAccount(t, incResponder).Incident.ID
		insertDeveloperCredential(t, db, "devc_prov", "devp_prov", string(incTarget), "org_a", "prj_a1", "env_a1", "2099-01-01T00:00:00.000000Z")
		execRaw(t, db, `UPDATE developer_credentials SET parent_session_id='ses_prov' WHERE id='devc_prov'`)
		logouts := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.logout' AND object_id='ses_prov' AND correlation_id='`+inc+`'`)
		}
		for i := range 2 {
			if res := w.resumeOwner(t, incResponder, inc, domain.OwnerSessionRevoke, domain.ObjectKindSession, "ses_prov", "instance"); res != service.OwnerConfirmed {
				t.Fatalf("call %d = %v, want confirmed", i, res)
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id='devc_prov' AND revoked_at IS NULL`); n != 0 {
			t.Errorf("a delegation minted from the revoked session is still live")
		}
		if n := w.correlated(t, "identity.developer_credential_revoked", inc); n != 1 {
			t.Errorf("delegation revocation events with the incident correlation = %d, want 1", n)
		}
		if n := logouts(); n != 1 {
			t.Errorf("session events with the incident correlation = %d, want exactly 1", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id='devc_it' AND revoked_at IS NULL`); n != 1 {
			t.Errorf("a delegation minted from another session was revoked")
		}
	})
}

// A delegation revocation event keeps its owner's trail: a project delegation's
// lands in the tenant trail (the project owner's revoke), an org delegation's in
// the instance trail (the incident's credential-reset), each exactly once.
func TestIncidentDeveloperRevokeEventLandsInItsOwnersTrail(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		insertDeveloperCredential(t, db, "devc_orglvl", "devp_orglvl", string(incTarget), "org_a", "", "", "2099-01-01T00:00:00.000000Z")
		inc := w.openOrg(t, incResponder)
		trail := func(table, id string) int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM `+table+` WHERE type='identity.developer_credential_revoked' AND object_id='`+id+`' AND correlation_id='`+inc+`'`)
		}
		for _, c := range []struct{ id, scope, tenant, instance string }{
			{"devc_it", "org_a/prj_a1/env_a1", "audit_tenant_events", "audit_instance_events"},
			{"devc_orglvl", "org_a", "audit_instance_events", "audit_tenant_events"},
		} {
			if res := w.resumeOwner(t, incResponder, inc, domain.OwnerDeveloperRevoke, domain.ObjectKindDeveloperCred, c.id, c.scope); res != service.OwnerConfirmed {
				t.Fatalf("%s = %v, want confirmed", c.id, res)
			}
			if n := trail(c.tenant, c.id); n != 1 {
				t.Errorf("%s: %s events = %d, want 1", c.id, c.tenant, n)
			}
			if n := trail(c.instance, c.id); n != 0 {
				t.Errorf("%s: %s events = %d, want 0", c.id, c.instance, n)
			}
		}
	})
}

// An account incident contains only its own account: an item naming another
// account, even a member of the same org, answers not found and leaves it as it was.
func TestIncidentAccountContainRefusesAnotherAccount(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		seedContainMember(t, db, "usr_peer", [2]string{"read", "'org_a'"})
		inc := w.openAccount(t, incResponder).Incident.ID
		w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerAccountContain, domain.ObjectKindAccount, "usr_peer", "org_a")
		if got := w.containmentState(t, "usr_peer"); got != "none" {
			t.Errorf("another account on an account incident is %q, want none", got)
		}
	})
}

// Project and adapter incidents select no account, so their rotation owner refuses
// any rotation, even one whose authority is a member of the incident's org.
func TestIncidentRotationHaltRefusesAProjectOrAdapterIncident(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		seedAdapterWork(t, db)
		for _, target := range []service.IncidentTarget{
			{Kind: domain.IncidentScopeProject, Scope: domain.Scope{Org: orgA, Project: prjA1}},
			{Kind: domain.IncidentScopeAdapter, Scope: domain.Scope{Org: orgA, Project: prjA1}, AdapterID: "adp_w1"},
		} {
			v, err := r.w.svc.Open(t.Context(), r.w.as(incResponder), target)
			if err != nil {
				t.Fatal(err)
			}
			res, err := r.w.svc.Owners[domain.OwnerRotationHalt](t.Context(), r.w.as(incResponder), authz.IncidentItem{IncidentID: v.Incident.ID, OwnerFunction: domain.OwnerRotationHalt, ObjectKind: domain.ObjectKindRotation, ObjectID: r.rot.ID, Scope: r.scope()})
			if err == nil || res == service.OwnerConfirmed {
				t.Errorf("a %s incident's rotation halt = %v, %v, want refused", target.Kind, res, err)
			}
			if r.state(t) != store.RotationStateActive {
				t.Fatalf("a %s incident changed the rotation: %q", target.Kind, r.state(t))
			}
		}
	})
}

// An org incident halts only the rotations of the accounts it selects (a grant or a
// rule in the org). An authority tied to the org only through a SCIM user is not
// selected: the incident does not list its rotation, and the owner answers not
// found and leaves it running.
func TestIncidentOrgRotationHaltRefusesAnAuthorityTheIncidentDoesNotSelect(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_scimonly', 'human', `+ts+`)`)
		execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, created_at) VALUES ('acc_scimonly', 'usr_scimonly', 'scim-only', 'Scim Only', `+ts+`)`)
		execRaw(t, db, `INSERT INTO scim_bindings(id,org_id,provider_kind,provider_id,provider_slug,provider_issuer,subject_source,connection_principal_id,created_at) VALUES ('sbn_so','org_a','oidc','sbn_so','sbn_so','https://idp.example','externalId','mch_it',`+ts+`)`)
		execRaw(t, db, `INSERT INTO scim_users (id,org_id,binding_id,account_id,user_name,user_name_lower,subject,created_at,updated_at) VALUES ('scu_so','org_a','sbn_so','acc_scimonly','scim-only','scim-only','so-subject',`+ts+`,`+ts+`)`)
		execRaw(t, db, `UPDATE rotations SET authority_principal_id='usr_scimonly' WHERE id='`+r.rot.ID+`'`)
		inc := r.w.openOrg(t, incResponder)
		if got, listed := r.w.itemMap(t, incResponder, inc)[r.key()]; listed {
			t.Fatalf("the org incident lists the rotation of an authority it does not select: %q", got)
		}
		r.w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerRotationHalt, domain.ObjectKindRotation, r.rot.ID, r.scope())
		if r.state(t) != store.RotationStateActive {
			t.Errorf("a refused halt changed the rotation: %q", r.state(t))
		}
		// A grant in the org without an account is not selected either: the incident
		// lists only accounts.
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_rot_mch', 'mch_it', 'read', 'org_a', `+ts+`)`)
		seedOrigins(t, db)
		execRaw(t, db, `UPDATE rotations SET authority_principal_id='mch_it' WHERE id='`+r.rot.ID+`'`)
		r.w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerRotationHalt, domain.ObjectKindRotation, r.rot.ID, r.scope())
		if r.state(t) != store.RotationStateActive {
			t.Errorf("a refused halt for a principal without an account changed the rotation: %q", r.state(t))
		}
	})
}

// A rotation already halted keeps its halt as it stands: the owner confirms it
// without rewriting its reason or generation, and so does a repeat on one the
// incident halted itself (ADR amendment, #1471).
func TestIncidentRotationHaltNeverRewritesAHalt(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		acc, err := r.w.svc.Open(t.Context(), r.w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: alice})
		if err != nil {
			t.Fatal(err)
		}
		halt := func() (string, int64) {
			if res := r.w.callOwner(t, incResponder, acc.Incident.ID, domain.OwnerRotationHalt, domain.ObjectKindRotation, r.rot.ID, r.scope()); res != service.OwnerConfirmed {
				t.Fatalf("halt = %v, want confirmed", res)
			}
			return queryString(t, db, `SELECT halt_reason FROM rotations WHERE id='`+r.rot.ID+`'`), queryInt(t, db, `SELECT generation FROM rotations WHERE id='`+r.rot.ID+`'`)
		}
		execRaw(t, db, `UPDATE rotations SET state='halted', halt_reason='authority_lost', next_run_at=NULL WHERE id='`+r.rot.ID+`'`)
		before := queryInt(t, db, `SELECT generation FROM rotations WHERE id='`+r.rot.ID+`'`)
		if reason, gen := halt(); reason != "authority_lost" || gen != before {
			t.Errorf("an authority_lost halt became %q at generation %d, want authority_lost at %d", reason, gen, before)
		}
		execRaw(t, db, `UPDATE rotations SET state='active', halt_reason=NULL, next_run_at=`+ts+` WHERE id='`+r.rot.ID+`'`)
		reason, first := halt()
		if reason != "incident_contained" {
			t.Fatalf("halt reason = %q, want incident_contained", reason)
		}
		if reason, gen := halt(); reason != "incident_contained" || gen != first {
			t.Errorf("a repeat rewrote the halt: %q at generation %d, want incident_contained at %d", reason, gen, first)
		}
	})
}

// The machine credential owner's reader is bound to the item's project: a
// credential of another project of the same org is not found.
func TestIncidentMachineCredentialReaderIsBoundToItsProject(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO machine_credentials (id, service_account_id, kind, verifier, prefix_hint, lifetime, credential_epoch, created_at, created_by) VALUES ('mcr_rd', 'sa_it', 'hikyo-token', `+blobLit(db, []byte("verifier-mcr-rd"))+`, 'hk', 'indefinite', (SELECT credential_epoch FROM auth_instance_state WHERE id = 1), `+ts+`, 'usr_itarget')`)
		read := func(project domain.ProjectID) error {
			return tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				_, err := az.MachineCredentialInProject(ctx, domain.Scope{Org: orgA, Project: project}, "mcr_rd")
				return err
			})
		}
		if err := read(prjA1); err != nil {
			t.Fatalf("the credential in its own project: %v", err)
		}
		if err := read(prjA2); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("the credential read from another project of the org = %v, want not found", err)
		}
	})
}

// The SCIM owner's reader is bound to the proof's org: a credential of another org
// is not found.
func TestIncidentSCIMCredentialReaderIsBoundToItsOrg(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		newIncidentWorld(t, db)
		grantOrgB(t, db, incResponder)
		epoch := `(SELECT credential_epoch FROM auth_instance_state WHERE id = 1)`
		execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_rd_b', 'machine', 'workload', `+ts+`)`)
		execRaw(t, db, `INSERT INTO scim_bindings(id,org_id,provider_kind,provider_id,provider_slug,provider_issuer,subject_source,connection_principal_id,created_at) VALUES ('sbn_rd_b','org_b','oidc','sbn_rd_b','sbn_rd_b','https://idp.example','externalId','mch_rd_b',`+ts+`)`)
		execRaw(t, db, `INSERT INTO scim_credentials (id, org_id, binding_id, principal_id, verifier, credential_epoch, created_at) VALUES ('scc_rd_b','org_b','sbn_rd_b','mch_rd_b',`+blobLit(db, []byte("verifier-scc-rd"))+`, `+epoch+`, `+ts+`)`)
		read := func(org domain.OrgID) error {
			return tx.Write(t.Context(), db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				p, err := az.Authorize(ctx, authz.Identity{Principal: incResponder}, authz.OpSCIMCredentialRevoke, domain.Scope{Org: org})
				if err != nil {
					return err
				}
				_, err = r.SCIM().CredentialInOrg(ctx, p, "scc_rd_b")
				return err
			})
		}
		if err := read(orgB); err != nil {
			t.Fatalf("the credential in its own org: %v", err)
		}
		if err := read(orgA); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("the org B credential read under an org A proof = %v, want not found", err)
		}
	})
}

// seedScimOnly ties an account to org_a only through a SCIM user: it is in the
// principal's footprint, but the org incident does not select it.
func seedScimOnly(t *testing.T, db *store.DB, id string) {
	t.Helper()
	execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('`+id+`', 'human', `+ts+`)`)
	execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, created_at) VALUES ('acc_`+id+`', '`+id+`', '`+id+`', 'Scim Only', `+ts+`)`)
	execRaw(t, db, `INSERT INTO scim_bindings(id,org_id,provider_kind,provider_id,provider_slug,provider_issuer,subject_source,connection_principal_id,created_at) VALUES ('sbn_`+id+`','org_a','oidc','sbn_`+id+`','sbn_`+id+`','https://idp.example','externalId','mch_it',`+ts+`)`)
	execRaw(t, db, `INSERT INTO scim_users (id,org_id,binding_id,account_id,user_name,user_name_lower,subject,created_at,updated_at) VALUES ('scu_`+id+`','org_a','sbn_`+id+`','acc_`+id+`','`+id+`','`+id+`','`+id+`-subject',`+ts+`,`+ts+`)`)
}

// An org incident contains only the members it selects. An account tied to the
// org only through a SCIM user is not listed, and its contain item answers not
// found and leaves it uncontained.
func TestIncidentOrgContainRefusesAnAccountTheIncidentDoesNotSelect(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		seedScimOnly(t, db, "usr_scimonly")
		inc := w.openOrg(t, incResponder)
		if got, listed := w.itemMap(t, incResponder, inc)["account.contain|account|usr_scimonly"]; listed {
			t.Fatalf("the org incident lists an account it does not select: %q", got)
		}
		w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerAccountContain, domain.ObjectKindAccount, "usr_scimonly", "org_a")
		if got := w.containmentState(t, "usr_scimonly"); got != "none" {
			t.Errorf("an unselected account is %q, want none", got)
		}
	})
}

// An org incident selects a member only as a human with an account row whose
// privacy is active. A privacy-restricted human, a human with no account row and
// a machine principal with an account row all hold a grant in the org, yet none
// is listed, and both owners answer not found and leave them as they were.
func TestIncidentOrgOwnersRefuseAMemberTheIncidentDoesNotSelect(t *testing.T) {
	for _, c := range []struct{ name, setup string }{
		{"restricted", `UPDATE principals SET privacy_state='restricted' WHERE id='usr_unsel'`},
		{"no_account", `DELETE FROM accounts WHERE principal_id='usr_unsel'`},
		{"machine", `UPDATE principals SET kind='machine', class='workload' WHERE id='usr_unsel'`},
	} {
		t.Run(c.name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				r := newRotationIncident(t, db)
				seedContainMember(t, db, "usr_unsel", [2]string{"read", "'org_a'"})
				execRaw(t, db, c.setup)
				execRaw(t, db, `UPDATE rotations SET authority_principal_id='usr_unsel' WHERE id='`+r.rot.ID+`'`)
				inc := r.w.openOrg(t, incResponder)
				items := r.w.itemMap(t, incResponder, inc)
				for _, key := range []string{r.key(), "account.contain|account|usr_unsel"} {
					if got, listed := items[key]; listed {
						t.Errorf("the org incident lists %s: %q", key, got)
					}
				}
				r.w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerRotationHalt, domain.ObjectKindRotation, r.rot.ID, r.scope())
				if r.state(t) != store.RotationStateActive {
					t.Errorf("a refused halt changed the rotation: %q", r.state(t))
				}
				r.w.ownerRefusedNotFound(t, incResponder, inc, domain.OwnerAccountContain, domain.ObjectKindAccount, "usr_unsel", "org_a")
				if got := r.w.containmentState(t, "usr_unsel"); got != "none" {
					t.Errorf("an unselected member is %q, want none", got)
				}
			})
		})
	}
}

// The selection reads no containment, so an org incident's contain item resumed
// after it contained its member confirms again instead of losing the member.
func TestIncidentOrgContainResumesOnAContainedMember(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		seedContainMember(t, db, "usr_member", [2]string{"read", "'org_a'"})
		inc := w.openOrg(t, incResponder)
		if _, listed := w.itemMap(t, incResponder, inc)["account.contain|account|usr_member"]; !listed {
			t.Fatal("the org incident does not list its member")
		}
		for i := range 2 {
			if res := w.resumeOwner(t, incResponder, inc, domain.OwnerAccountContain, domain.ObjectKindAccount, "usr_member", "org_a"); res != service.OwnerConfirmed {
				t.Fatalf("call %d = %v, want confirmed", i, res)
			}
		}
		if got := w.containmentState(t, "usr_member"); got != "contained" {
			t.Errorf("the member is %q, want contained", got)
		}
	})
}

// A workspace session ends as its holder's explicit revocation does: exactly one
// workspace revocation event joins the incident by correlation, however often the
// item runs, and no logout event is written for it.
func TestIncidentSessionOwnerRecordsTheWorkspaceRevocationEvent(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent, requesting_origin, handoff_id) `+
			`VALUES ('ses_ws', '`+string(incTarget)+`', `+blobLit(db, []byte("verifier-ses-ws"))+`, 'workspace', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '', 'https://ws.example', 'hof_ws')`)
		inc := w.openAccount(t, incResponder).Incident.ID
		for i := range 2 {
			if res := w.resumeOwner(t, incResponder, inc, domain.OwnerSessionRevoke, domain.ObjectKindSession, "ses_ws", "instance"); res != service.OwnerConfirmed {
				t.Fatalf("call %d = %v, want confirmed", i, res)
			}
		}
		count := func(typ audit.EventType) int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='`+string(typ)+`' AND object_id='ses_ws' AND correlation_id='`+inc+`'`)
		}
		if n := count(audit.EventRemoteWorkspaceSessionRevoked); n != 1 {
			t.Errorf("workspace revocation events with the incident correlation = %d, want exactly 1", n)
		}
		if n := count(audit.EventAuthLogout); n != 0 {
			t.Errorf("logout events for a workspace session = %d, want 0", n)
		}
	})
}

// Only an account incident selects sessions: an org incident's session item is
// invalid, whether or not the session exists, and the session stays live.
func TestIncidentOrgSessionItemIsInvalid(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
			`VALUES ('ses_org', '`+string(incTarget)+`', `+blobLit(db, []byte("verifier-ses-org"))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)
		inc := w.openOrg(t, incResponder)
		for _, id := range []string{"ses_org", "ses_absent"} {
			res, err := w.svc.Owners[domain.OwnerSessionRevoke](t.Context(), w.as(incResponder), authz.IncidentItem{IncidentID: inc, OwnerFunction: domain.OwnerSessionRevoke, ObjectKind: domain.ObjectKindSession, ObjectID: id, Scope: "instance"})
			if !errors.Is(err, domain.ErrInvalid) || res == service.OwnerConfirmed {
				t.Errorf("an org incident's session item %s = %v, %v, want invalid", id, res, err)
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM sessions WHERE id='ses_org'`); n != 1 {
			t.Errorf("an org incident's session item revoked the session")
		}
	})
}

// A rotation deleted before its item runs is gone (option A): the owner confirms
// it without halting anything.
func TestIncidentRotationHaltConfirmsADeletedRotation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		acc, err := r.w.svc.Open(t.Context(), r.w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: alice})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.x.svc.Delete(tctx(t), r.x.actor, r.x.scope, service.RotationStateRequest{RotationID: r.rot.ID, Abandon: true}); err != nil {
			t.Fatal(err)
		}
		if r.state(t) != store.RotationStateDisabled {
			t.Fatalf("the deleted rotation is %q, want disabled", r.state(t))
		}
		if res := r.w.callOwner(t, incResponder, acc.Incident.ID, domain.OwnerRotationHalt, domain.ObjectKindRotation, r.rot.ID, r.scope()); res != service.OwnerConfirmed {
			t.Errorf("halt of a deleted rotation = %v, want confirmed", res)
		}
		if r.state(t) != store.RotationStateDisabled {
			t.Errorf("the owner changed a deleted rotation: %q", r.state(t))
		}
	})
}
