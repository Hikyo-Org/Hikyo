package isolation

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// rotationView reads the one rotation item of an incident through ItemViews.
func (w *incidentWorld) rotationView(t *testing.T, actor domain.PrincipalID, incident, rotation string) service.IncidentItemView {
	t.Helper()
	views, err := w.svc.ItemViews(t.Context(), w.as(actor), incident, "", 500, false)
	if err != nil {
		t.Fatalf("item views: %v", err)
	}
	for _, v := range views {
		if v.ObjectKind == "rotation" && v.ObjectID == rotation {
			return v
		}
	}
	t.Fatalf("no rotation item %s in the incident", rotation)
	return service.IncidentItemView{}
}

func (w *incidentWorld) resume(t *testing.T, actor domain.PrincipalID, id string) service.ResumeResult {
	t.Helper()
	res, err := w.svc.Resume(t.Context(), w.as(actor), id, "", nil)
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

// The delegation revoke confirms by one scoped lookup, so it holds in an org with
// more live delegations than a paged scan could read (20 pages of 500). An org
// incident has no single authority, so the fillers (another member's, sorting
// before the target) all sat in the old scan's way.
func TestIncidentOwnersRevokeConfirmsAmongManyLiveDelegations(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		w.realOwners()
		inc := w.openOrg(t, incResponder)
		const rows = 10001
		cols := `org_id, project_id, env_id, prefix_hint, parent_session_id, provider_id, oauth2_provider_id, saml_provider_id, auth_method, authority_generation, credential_epoch, created_at, expires_at`
		if db.Engine() == store.EnginePostgres {
			execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) SELECT 'devp_fill_' || i, 'machine', 'developer', `+ts+` FROM generate_series(1, `+fmt.Sprint(rows)+`) AS i`)
			execRaw(t, db, `INSERT INTO developer_credentials (id, principal_id, authority_principal_id, verifier, `+cols+`) SELECT 'a_fill_' || i, 'devp_fill_' || i, '`+string(incLimited)+`', convert_to('v' || i, 'UTF8'), `+cols+` FROM developer_credentials, generate_series(1, `+fmt.Sprint(rows)+`) AS i WHERE id = 'devc_it'`)
		} else {
			series := `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ` + fmt.Sprint(rows) + `) `
			execRaw(t, db, series+`INSERT INTO principals (id, kind, class, created_at) SELECT 'devp_fill_' || i, 'machine', 'developer', `+ts+` FROM n`)
			execRaw(t, db, series+`INSERT INTO developer_credentials (id, principal_id, authority_principal_id, verifier, `+cols+`) SELECT 'a_fill_' || i, 'devp_fill_' || i, '`+string(incLimited)+`', CAST('v' || i AS BLOB), `+cols+` FROM developer_credentials, n WHERE id = 'devc_it'`)
		}
		w.resume(t, incResponder, inc)
		if got := w.itemMap(t, incResponder, inc)[keyDevCred]; got != "done/" {
			t.Fatalf("delegation item = %q, want done", got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id = 'devc_it' AND revoked_at IS NULL`); n != 0 {
			t.Errorf("the target delegation survives the revoke")
		}
	})
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
		if got := w.itemMap(t, incResponder, v.Incident.ID)["account.contain|account|"+string(incResponder)]; got != "pending/owner_failed" {
			t.Errorf("self-containment item = %q, want pending/owner_failed", got)
		}
		// A refused item reads as refused, apart from one that has not run yet.
		views, err := w.svc.ItemViews(t.Context(), w.as(incResponder), v.Incident.ID, "", 500, false)
		if err != nil {
			t.Fatal(err)
		}
		refused := 0
		for _, view := range views {
			if view.Refused {
				refused++
			}
		}
		if refused != 1 {
			t.Errorf("refused items = %d, want the one containment", refused)
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
		wantItems(t, got, map[string]string{keyContain: "pending/owner_failed", keySession: "done/", keyDevCred: "done/"})
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
		// The item view reads the rotation's own state live; nothing is stored.
		if got := r.w.rotationView(t, incResponder, id, r.rot.ID); got.ObjectState != store.RotationStateActive || got.Refused {
			t.Fatalf("before resume the view = state %q refused %v, want active and not refused", got.ObjectState, got.Refused)
		}
		r.w.resume(t, incResponder, id)
		if got := r.w.itemMap(t, incResponder, id)[r.key()]; got != "done/" {
			t.Fatalf("rotation item = %q, want done", got)
		}
		if got := r.w.rotationView(t, incResponder, id, r.rot.ID); got.ObjectState != store.RotationStateHalted {
			t.Fatalf("after resume the view state = %q, want halted", got.ObjectState)
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
		// UPDATE because the release service is a later slice. Beyond staying halted,
		// the halt must be untouched: same reason, same generation, and no run started
		// after the executor was given a full day to act on a principal whose
		// authority is back.
		generation := queryInt(t, db, `SELECT generation FROM rotations WHERE id='`+r.rot.ID+`'`)
		runs := queryInt(t, db, `SELECT COUNT(*) FROM rotation_runs WHERE rotation_id='`+r.rot.ID+`'`)
		execRaw(t, db, `UPDATE principals SET containment_state='none' WHERE id='`+string(alice)+`'`)
		r.x.advance(25 * 3600 * 1e9)
		r.x.drain()
		if r.state(t) != store.RotationStateHalted {
			t.Errorf("release resumed the rotation: %q", r.state(t))
		}
		if got := queryInt(t, db, `SELECT generation FROM rotations WHERE id='`+r.rot.ID+`'`); got != generation {
			t.Errorf("generation %d -> %d after release, want unchanged", generation, got)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM rotation_runs WHERE rotation_id='`+r.rot.ID+`'`); got != runs {
			t.Errorf("runs %d -> %d after release, want unchanged", runs, got)
		}
		if reason := queryString(t, db, `SELECT halt_reason FROM rotations WHERE id='`+r.rot.ID+`'`); reason != "incident_contained" {
			t.Errorf("halt reason %q after release, want incident_contained", reason)
		}
	})
}

// An account whose footprint spans two orgs, or none, needs an instance holder, who
// lists its rotations across orgs and halts them under the same credential-reset the
// incident was opened with.
func TestIncidentInstanceAccountIncidentListsAndHaltsRotations(t *testing.T) {
	for name, prepare := range map[string]func(*testing.T, *store.DB){
		"two orgs": func(t *testing.T, db *store.DB) {
			execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_rot_b', '`+string(alice)+`', 'read', 'org_b', `+ts+`)`)
		},
		"no org": func(t *testing.T, db *store.DB) {
			execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id='`+string(alice)+`')`)
			execRaw(t, db, `DELETE FROM grants WHERE principal_id='`+string(alice)+`'`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				r := newRotationIncident(t, db)
				prepare(t, db)
				target := service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: alice}
				if _, err := r.w.svc.Open(t.Context(), r.w.as(incResponder), target); err == nil {
					t.Fatal("an org-scope responder opened an incident that spans orgs")
				}
				v, err := r.w.svc.Open(t.Context(), r.w.as(incInstance), target)
				if err != nil {
					t.Fatal(err)
				}
				if v.Incident.OrgID != "" {
					t.Fatalf("incident org = %q, want an instance-owned incident", v.Incident.OrgID)
				}
				if got := r.w.itemMap(t, incInstance, v.Incident.ID)[r.key()]; got != "pending/" {
					t.Fatalf("instance rotation item = %q, want pending (items: %v)", got, r.w.itemMap(t, incInstance, v.Incident.ID))
				}
				r.w.resume(t, incInstance, v.Incident.ID)
				if r.state(t) != store.RotationStateHalted {
					t.Errorf("rotation state = %q, want halted", r.state(t))
				}
			})
		})
	}
}

// Project and adapter scopes select no account, so they list no rotation of their own.
func TestIncidentProjectScopeListsNoRotation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		v, err := r.w.svc.Open(t.Context(), r.w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeProject, Scope: domain.Scope{Org: orgA, Project: prjA1}})
		if err != nil {
			t.Fatal(err)
		}
		for k := range r.w.itemMap(t, incResponder, v.Incident.ID) {
			if strings.HasPrefix(k, "rotation.halt|") {
				t.Errorf("a project incident lists a rotation item %q", k)
			}
		}
	})
}

// The live state is read under the caller's own authority over the rotation's
// scope. A rotation the ledger names that no longer exists reads "gone"; one
// whose scope the caller cannot reach reads nothing at all, so the presentation
// read never tells a caller whether a rotation outside their reach exists
// (#1471 m8). The ledger row is edited directly: the scope and id columns are the
// only inputs the read takes from it.
func TestIncidentRotationViewGoneAndUnreadable(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		v, err := r.w.svc.Open(t.Context(), r.w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: alice})
		if err != nil {
			t.Fatal(err)
		}
		id := v.Incident.ID
		if got := r.w.rotationView(t, incResponder, id, r.rot.ID); got.ObjectState != store.RotationStateActive {
			t.Fatalf("a live rotation read %q, want active", got.ObjectState)
		}
		// Gone: the rotation row no longer exists.
		execRaw(t, db, `UPDATE incident_items SET object_id='rot_gone' WHERE incident_id='`+id+`' AND object_id='`+r.rot.ID+`'`)
		if got := r.w.rotationView(t, incResponder, id, "rot_gone"); got.ObjectState != "gone" {
			t.Errorf("a rotation that no longer exists read %q, want gone", got.ObjectState)
		}
		if got := r.w.rotationView(t, incResponder, id, "rot_gone"); got.Attestable {
			t.Errorf("a halt item must not be offered for attestation")
		}
		// Unreadable: the item's scope is an environment the caller holds nothing on.
		execRaw(t, db, `UPDATE incident_items SET scope='`+string(orgB)+`/`+string(prjB1)+`/`+string(envB1)+`' WHERE incident_id='`+id+`' AND object_id='rot_gone'`)
		if got := r.w.rotationView(t, incResponder, id, "rot_gone"); got.ObjectState != "" {
			t.Errorf("a rotation outside the caller's reach read %q, want nothing", got.ObjectState)
		}
		// A scope that does not parse reads nothing either.
		execRaw(t, db, `UPDATE incident_items SET scope='not-a-scope' WHERE incident_id='`+id+`' AND object_id='rot_gone'`)
		if got := r.w.rotationView(t, incResponder, id, "rot_gone"); got.ObjectState != "" {
			t.Errorf("a malformed scope read %q, want nothing", got.ObjectState)
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

// An item scoped to another org is refused for the incident's own org even when
// the operator holds the capability in both orgs: the owner never acts outside
// the org the incident was opened on.
func TestIncidentOwnerRefusesAnItemOutsideTheIncidentOrg(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		w := r.w
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_inc_resp_b', '`+string(incResponder)+`', 'credential-reset', 'org_b', `+ts+`)`)
		// A real org B rotation whose authority (alice) has a footprint in org A and
		// whose scope the responder can act in: only the org binding refuses it.
		execRaw(t, db, `INSERT INTO dynamic_providers (id,org_id,project_id,kind,origin,tls_mode,grant_role,authority_principal_id,state,created_at) VALUES ('dpr_orgb_rot','org_b','prj_b1','postgres','postgres://orgb.example/db','verify-full','reader','`+string(alice)+`','active',`+ts+`)`)
		execRaw(t, db, `INSERT INTO rotations (id, org_id, project_id, environment_id, provider_id, key_id, username_key_id, interval_seconds, revocation_delay_seconds, authority_principal_id, created_by, state, next_run_at, generation, created_at, updated_at) VALUES ('rot_orgb','org_b','prj_b1','env_b1','dpr_orgb_rot','rk_orgb_pw','rk_orgb_user',86400,3600,'`+string(alice)+`','`+string(alice)+`','active',`+ts+`,1,`+ts+`,`+ts+`)`)
		orgInc := w.openOrg(t, incResponder)
		res, err := w.svc.Owners[domain.OwnerRotationHalt](t.Context(), w.as(incResponder), authz.IncidentItem{IncidentID: orgInc, OwnerFunction: domain.OwnerRotationHalt, ObjectKind: domain.ObjectKindRotation, ObjectID: "rot_orgb", Scope: "org_b/prj_b1/env_b1"})
		if !errors.Is(err, domain.ErrNotFound) || res == service.OwnerConfirmed {
			t.Errorf("an org B rotation on an org A incident = %v, %v, want ErrNotFound", res, err)
		}
		if got := queryString(t, db, `SELECT state FROM rotations WHERE id='rot_orgb'`); got != store.RotationStateActive {
			t.Errorf("a refused halt changed the org B rotation: %q", got)
		}
		if r.state(t) != store.RotationStateActive {
			t.Errorf("a refused halt changed the rotation: %q", r.state(t))
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
		// An org incident reaches only a rotation whose authority has a footprint in
		// that org: with the authority moved to a principal that has none there, the
		// halt is refused and the rotation untouched.
		orgInc := w.openOrg(t, incResponder)
		execRaw(t, db, `UPDATE rotations SET authority_principal_id='`+string(incOrgB)+`' WHERE id='`+r.rot.ID+`'`)
		if res, err := w.svc.Owners[domain.OwnerRotationHalt](t.Context(), w.as(incResponder), authz.IncidentItem{IncidentID: orgInc, OwnerFunction: domain.OwnerRotationHalt, ObjectKind: domain.ObjectKindRotation, ObjectID: r.rot.ID, Scope: r.scope()}); err == nil || res == service.OwnerConfirmed {
			t.Errorf("an org incident halted a rotation whose authority has no footprint in the org: %v, %v", res, err)
		}
		if r.state(t) != store.RotationStateActive {
			t.Fatalf("a refused halt changed the rotation: %q", r.state(t))
		}
		execRaw(t, db, `UPDATE rotations SET authority_principal_id='`+string(alice)+`' WHERE id='`+r.rot.ID+`'`)
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
		// An absent id must find nothing, whatever else is live: a lookup that ignored the
		// id would hand back one of these (mcr_it, the first row, is still live here) and revoke or mistake it for the item.
		execRaw(t, db, `INSERT INTO machine_credentials (id, service_account_id, kind, verifier, prefix_hint, lifetime, credential_epoch, created_at, created_by) VALUES ('mcr_same_project', 'sa_it', 'hikyo-token', `+blobLit(db, []byte("verifier-mcr-same"))+`, 'hk', 'indefinite', (SELECT credential_epoch FROM auth_instance_state WHERE id = 1), `+ts+`, 'usr_itarget')`)
		execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_it_b', 'machine', 'workload', `+ts+`)`)
		execRaw(t, db, `INSERT INTO service_accounts (id, principal_id, org_id, project_id, name, kind, created_at, created_by) VALUES ('sa_it_b', 'mch_it_b', 'org_a', 'prj_a2', 'it-sa-b', 'workload', `+ts+`, 'usr_itarget')`)
		execRaw(t, db, `INSERT INTO machine_credentials (id, service_account_id, kind, verifier, prefix_hint, lifetime, credential_epoch, created_at, created_by) VALUES ('mcr_other_project', 'sa_it_b', 'hikyo-token', `+blobLit(db, []byte("verifier-mcr-other"))+`, 'hk', 'indefinite', (SELECT credential_epoch FROM auth_instance_state WHERE id = 1), `+ts+`, 'usr_itarget')`)
		liveOthers := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM machine_credentials WHERE id IN ('mcr_same_project','mcr_other_project') AND revoked_at IS NULL`)
		}
		if res := call(incResponder, inc, "mcr_missing"); res != service.OwnerConfirmed || liveOthers() != 2 || live() != 1 {
			t.Errorf("already gone = %v, live others = %d, want confirmed and both untouched", res, liveOthers())
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
		// A credential of another project is not this project's: it is not found here
		// and stays live.
		if res := call(incResponder, inc, "mcr_other_project"); res != service.OwnerConfirmed || liveOthers() != 2 {
			t.Errorf("a credential of another project = %v, live others = %d, want it left alone", res, liveOthers())
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

// The lease and target owners treat not-found as absent, but the authorizer masks
// a lost grant as not found too. The two predicates they confirm absence with
// authorize first, so absent is true only for an authorized caller and a missing
// row; a caller without authority is ErrUnauthorized, and a store failure is its
// own error, never absence.
func TestIncidentAbsencePredicatesAuthorizeFirst(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		seedIncidentLeases(t, db)
		execRaw(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_it','org_a','prj_a1','forgejo','https://git.example','usr_alice','active',`+ts+`)`)
		execRaw(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_it','org_a','prj_a1','env_a1','adp_it','repository','acme','app',41,'',1,'active','converged',`+ts+`)`)
		dyn, adp := &service.Dynamic{DB: db}, &service.Adapters{DB: db}
		leaseScope := domain.Scope{Org: "org_a", Project: "prj_a1", Env: "env_a1"}
		projectScope := domain.Scope{Org: "org_a", Project: "prj_a1"}
		holder, stranger := w.as(incResponder), w.as(incOrgB)

		lease := func(a service.Actor, id string) (string, bool, error) {
			return dyn.LeaseStateIfAuthorized(t.Context(), a, leaseScope, id)
		}
		target := func(a service.Actor, id string) (bool, error) {
			return adp.TargetAbsentIfAuthorized(t.Context(), a, projectScope, id)
		}
		if state, absent, err := lease(holder, "lse_it_01"); err != nil || absent || state != "active" {
			t.Errorf("an existing lease = %q, absent=%v, %v; want active and present", state, absent, err)
		}
		if _, absent, err := lease(holder, "lse_it_missing"); err != nil || !absent {
			t.Errorf("a missing lease for an authorized caller = absent=%v, %v; want absent", absent, err)
		}
		if _, absent, err := lease(stranger, "lse_it_missing"); !errors.Is(err, domain.ErrUnauthorized) || absent {
			t.Errorf("a missing lease for a caller without authority = absent=%v, %v; want ErrUnauthorized and not absent", absent, err)
		}
		if _, absent, err := lease(stranger, "lse_it_01"); !errors.Is(err, domain.ErrUnauthorized) || absent {
			t.Errorf("an existing lease for a caller without authority = absent=%v, %v; want ErrUnauthorized and not absent", absent, err)
		}
		if absent, err := target(holder, "tgt_it"); err != nil || absent {
			t.Errorf("an existing target = absent=%v, %v; want present", absent, err)
		}
		if absent, err := target(holder, "tgt_missing"); err != nil || !absent {
			t.Errorf("a missing target for an authorized caller = absent=%v, %v; want absent", absent, err)
		}
		if absent, err := target(stranger, "tgt_missing"); !errors.Is(err, domain.ErrUnauthorized) || absent {
			t.Errorf("a missing target for a caller without authority = absent=%v, %v; want ErrUnauthorized and not absent", absent, err)
		}

		// A store failure on the read is neither absence nor a lost grant.
		execRaw(t, db, "ALTER TABLE dynamic_leases RENAME TO dynamic_leases_broken")
		_, absent, err := lease(holder, "lse_it_01")
		execRaw(t, db, "ALTER TABLE dynamic_leases_broken RENAME TO dynamic_leases")
		if err == nil || absent || errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("a failing lease read = absent=%v, %v; want a store error", absent, err)
		}
		execRaw(t, db, "ALTER TABLE adapter_targets RENAME TO adapter_targets_broken")
		absent, err = target(holder, "tgt_it")
		execRaw(t, db, "ALTER TABLE adapter_targets_broken RENAME TO adapter_targets")
		if err == nil || absent || errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("a failing target read = absent=%v, %v; want a store error", absent, err)
		}
	})
}

// A contained account's rotations are not listed as incident items (the account
// grants reader skips a contained principal), so the safety rests on the executor:
// a rotation whose authority is contained halts at its gate without minting.
func TestIncidentContainedAuthorityRotationHaltsAtTheExecutorGate(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		execRaw(t, db, `UPDATE principals SET containment_state='contained' WHERE id='`+string(alice)+`'`)
		before := x.ctl.count("create:")
		x.advance(48 * time.Hour)
		x.drain()
		if x.ctl.count("create:") != before {
			t.Fatal("a rotation whose authority is contained minted a candidate")
		}
		if got := queryString(t, db, `SELECT state FROM rotations WHERE id='`+rot.ID+`'`); got != store.RotationStateHalted {
			t.Fatalf("state = %s, want halted", got)
		}
		if n := x.halts("authority_lost"); n != 1 {
			t.Fatalf("%d authority_lost halts, want 1", n)
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
// truncation row, at every budget. The one exception is a budget that runs out
// inside the truncation path itself: its account reads are charged to the budget
// too, so the members left are covered by a single scope-level row (never one
// per member, which would be unbounded). The row is the right answer there: the
// candidates left are principals the spent budget could not tell from accounts, so
// they have no row of their own to name, and a member without a row is accepted
// only when that scope row exists.
func TestIncidentOrgTruncationNamesEveryUnreachedMember(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newRotationIncident(t, db)
		sawUnreached, sawScopeRow := false, false
		var members []domain.PrincipalID
		for _, p := range strings.Fields(queryStrings(t, db, `SELECT DISTINCT a.principal_id || ' ' FROM accounts a JOIN grants g ON g.principal_id = a.principal_id WHERE g.org_id = 'org_a'`)) {
			members = append(members, domain.PrincipalID(p))
		}
		for budget := 3; budget <= 30; budget++ {
			r.w.svc.MaxLookups = budget
			id := r.w.openOrg(t, incResponder)
			got := r.w.itemMap(t, incResponder, id)
			_, scopeRow := got["rotation.halt|enumeration|"+string(orgA)]
			sawScopeRow = sawScopeRow || scopeRow
			for _, p := range members {
				_, listed := got["account.contain|account|"+string(p)]
				_, truncated := got["rotation.halt|enumeration|"+string(p)]
				if !listed && !truncated {
					if !scopeRow {
						t.Errorf("budget %d: member %s neither listed nor marked truncated: %v", budget, p, got)
					}
				}
				if !listed {
					sawUnreached = true
				}
			}
		}
		if !sawUnreached {
			t.Fatal("no budget left a member unreached")
		}
		// The account reads of the truncation path are charged to the budget: at some
		// budget it runs out there, which only the scope-level row can report.
		if !sawScopeRow {
			t.Fatal("no budget ran out inside the truncation path: its reads are not charged")
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

// A temporary grant is checked against each transaction's own clock, so an owner
// can lose its authority between two of its steps with no stub: the Incidents
// clock (real time) probes the grant before it expires, and the owner service's
// clock reads past the expiry. Dynamic.Now and Adapters.Now are read once per
// transaction, so a counter chooses the step at which the grant is lost.
//
// The target owner cannot be driven this way: pausing a target needs manage-adapters
// at project level, and a temporary grant is always environment-scoped
// (access_requests.project_id/environment_id and access_grants.env_id are NOT NULL),
// so no temporary grant can satisfy it and the grant cannot expire between the
// Incidents probe and the pause. Its lost-grant branches (a pause that answers not
// found and an absence read that answers unauthorized, absent, or a conflict) are
// covered by the stubbed TestPauseTargetFlow cases in the service package.

// tempIncidentGrant gives the principal a capability in environment env_a1 until expiry.
func tempIncidentGrant(t *testing.T, db *store.DB, principal domain.PrincipalID, capability string, expiry time.Time) {
	t.Helper()
	stamp, exp := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z"), expiry.UTC().Format("2006-01-02T15:04:05.000000Z")
	execRaw(t, db, fmt.Sprintf(`INSERT INTO access_requests(id,org_id,project_id,environment_id,policy_id,policy_version,requester_principal_id,capabilities,duration_seconds,reason,state,created_at,review_expires_at,granted_at,expires_at) VALUES ('ar_inc_%[1]s','org_a','prj_a1','env_a1','policy_fixture',1,'%[2]s','["%[1]s"]',3600,'incident owner clock fixture','granted','%[3]s','%[4]s','%[3]s','%[4]s')`, capability, principal, stamp, exp))
	execRaw(t, db, fmt.Sprintf(`INSERT INTO access_grants(id,principal_id,capability,org_id,project_id,env_id,request_id,created_at,expires_at) VALUES ('ag_inc_%[1]s','%[2]s','%[1]s','org_a','prj_a1','env_a1','ar_inc_%[1]s','%[3]s','%[4]s')`, capability, principal, stamp, exp))
}

// lossClock reads before the grant's expiry for its first `live` calls and after it
// from then on; calls counts every read.
func lossClock(live int32, expiry time.Time, calls *atomic.Int32) func() time.Time {
	before, after := expiry.Add(-30*time.Minute), expiry.Add(time.Hour)
	return func() time.Time {
		if calls.Add(1) <= live {
			return before
		}
		return after
	}
}

func TestIncidentLeaseOwnerBlocksWhenTheGrantIsLostAtEachStep(t *testing.T) {
	for _, tc := range []struct {
		name  string
		live  int32
		state string // the lease after the call: only a revoke under authority moves it
		reads int32  // clock reads, so each case stops at the step it names
	}{
		{"before the first read", 0, "active", 1},
		{"between the first read and the revoke", 1, "active", 3},
		{"between the revoke and the confirming read", 2, "revoking", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				w := newIncidentWorld(t, db)
				seedIncidentLeases(t, db)
				expiry := time.Now().Add(time.Hour)
				tempIncidentGrant(t, db, incLimited, "read", expiry)
				var reads atomic.Int32
				w.svc.Owners = service.NewIncidentOwners(w.svc, service.IncidentOwnerServices{
					Dynamic: &service.Dynamic{DB: db, Now: lossClock(tc.live, expiry, &reads)},
				})
				inc := w.openOrg(t, incLimited)
				res := w.callOwner(t, incLimited, inc, domain.OwnerLeaseRevoke, domain.ObjectKindLease, "lse_it_01", "org_a/prj_a1/env_a1")
				if res != service.OwnerBlocked {
					t.Fatalf("a grant lost %s = %v, want blocked", tc.name, res)
				}
				if got := queryString(t, db, `SELECT state FROM dynamic_leases WHERE id='lse_it_01'`); got != tc.state {
					t.Fatalf("a grant lost %s left the lease %s, want %s", tc.name, got, tc.state)
				}
				if got := reads.Load(); got != tc.reads {
					t.Fatalf("a grant lost %s read the clock %d times, want %d", tc.name, got, tc.reads)
				}
			})
		})
	}
}
