package isolation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Incident containment ledger, slice S2 (incident-containment ADR, O5 option
// B): the ledger, its enumeration, its two new listings and its audit trail.
// These tests drive the real service on both engines. No revocation exists in
// S2, so the owner functions the resume tests register are in-memory fakes that
// stand in for the owners that arrive with later slices.

const (
	incResponder = domain.PrincipalID("usr_irsp")       // credential-reset + the owner capabilities, org A
	incLimited   = domain.PrincipalID("usr_ilim")       // credential-reset only, org A
	incInstance  = domain.PrincipalID("usr_iinst")      // credential-reset at instance scope
	incOrgB      = domain.PrincipalID("usr_irspb")      // credential-reset, org B
	incTarget    = domain.PrincipalID("usr_itarget")    // the account an incident is opened on
	incHostLocal = domain.PrincipalID("usr_ihostlocal") // holds an instance capability
	incGhost     = domain.PrincipalID("usr_ighost")     // exists nowhere

	incTargetUsername = "it-target-user"
	incTargetEmail    = "it-target@example.test"
)

type incidentWorld struct {
	db  *store.DB
	svc *service.Incidents
}

func (w *incidentWorld) as(p domain.PrincipalID) service.Actor { return service.LocalPrincipal(p) }

func newIncidentWorld(t *testing.T, db *store.DB) *incidentWorld {
	t.Helper()
	for _, p := range []domain.PrincipalID{incResponder, incLimited, incInstance, incOrgB, incTarget, incHostLocal} {
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('`+string(p)+`', 'human', `+ts+`)`)
	}
	for i, g := range []struct {
		p    domain.PrincipalID
		cap  string
		org  string
		note string
	}{
		{incResponder, "credential-reset", "'org_a'", ""},
		{incResponder, "manage-members", "'org_a'", ""},
		{incResponder, "manage-identities", "'org_a'", ""},
		{incResponder, "manage-adapters", "'org_a'", ""},
		{incResponder, "read", "'org_a'", ""},
		{incLimited, "credential-reset", "'org_a'", ""},
		{incInstance, "credential-reset", "NULL", ""},
		{incOrgB, "credential-reset", "'org_b'", ""},
		{incTarget, "read", "'org_a'", ""},
		{incTarget, "edit", "'org_a'", ""},
		{incHostLocal, "instance-config", "NULL", ""},
	} {
		execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_inc_%02d', '%s', '%s', %s, %s)`, i, g.p, g.cap, g.org, ts))
	}
	seedOrigins(t, db)
	for _, a := range []struct{ id, principal, name, email string }{
		{"acc_itarget", string(incTarget), incTargetUsername, incTargetEmail},
		{"acc_ihostlocal", string(incHostLocal), "it-host-local", "it-host@example.test"},
	} {
		execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('`+a.id+`', '`+a.principal+`', '`+a.name+`', 'Incident Subject', '`+a.email+`', `+ts+`, `+ts+`)`)
	}
	execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
		`VALUES ('ses_it_1', 'usr_itarget', `+blobLit(db, []byte("verifier-it-1"))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)
	insertDeveloperCredential(t, db, "devc_it", "devp_it", string(incTarget), "org_a", "prj_a1", "env_a1", "2099-01-01T00:00:00.000000Z")
	execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_it', 'machine', 'workload', `+ts+`)`)
	execRaw(t, db, `INSERT INTO service_accounts (id, principal_id, org_id, project_id, name, kind, created_at, created_by) VALUES ('sa_it', 'mch_it', 'org_a', 'prj_a1', 'it-sa', 'workload', `+ts+`, 'usr_itarget')`)
	return &incidentWorld{db: db, svc: &service.Incidents{
		DB: db, Adapters: &service.Adapters{DB: db}, Dynamic: &service.Dynamic{DB: db}, SCIM: &service.SCIM{DB: db},
	}}
}

func insertDeveloperCredential(t *testing.T, db *store.DB, id, devPrincipal, authority, org, project, env, expires string) {
	t.Helper()
	execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('`+devPrincipal+`', 'machine', 'developer', `+ts+`)`)
	execRaw(t, db, `INSERT INTO developer_credentials (id, principal_id, authority_principal_id, org_id, project_id, env_id, verifier, prefix_hint, parent_session_id, provider_id, oauth2_provider_id, saml_provider_id, auth_method, authority_generation, credential_epoch, created_at, expires_at) `+
		`VALUES ('`+id+`', '`+devPrincipal+`', '`+authority+`', '`+org+`', '`+project+`', '`+env+`', `+blobLit(db, []byte("verifier-"+id))+`, 'dev', 'ses_x_`+id+`', '', '', '', 'password', 1, 0, `+ts+`, '`+expires+`')`)
}

// itemMap reads every item of an incident through the service, keyed by
// "owner|kind|id", valued "outcome/reason".
func (w *incidentWorld) itemMap(t *testing.T, actor domain.PrincipalID, id string) map[string]string {
	t.Helper()
	out := map[string]string{}
	after := ""
	for {
		page, err := w.svc.Items(t.Context(), w.as(actor), id, after, 500, false)
		if err != nil {
			t.Fatalf("read items: %v", err)
		}
		for _, it := range page {
			key := it.OwnerFunction + "|" + it.ObjectKind + "|" + it.ObjectID
			if _, dup := out[key]; dup {
				t.Fatalf("duplicate ledger key %s", key)
			}
			out[key] = string(it.Outcome) + "/" + string(it.Reason)
		}
		if len(page) < 500 {
			return out
		}
		after = page[len(page)-1].ID
	}
}

func (w *incidentWorld) openAccount(t *testing.T, actor domain.PrincipalID) service.IncidentView {
	t.Helper()
	v, err := w.svc.Open(t.Context(), w.as(actor), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incTarget})
	if err != nil {
		t.Fatalf("open account incident: %v", err)
	}
	return v
}

func (w *incidentWorld) events(t *testing.T, typ, incident string) int64 {
	t.Helper()
	return queryInt(t, w.db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = '`+typ+`' AND correlation_id = '`+incident+`'`)
}

func (w *incidentWorld) itemID(t *testing.T, incident, object string) string {
	t.Helper()
	page, err := w.svc.Items(t.Context(), w.as(incResponder), incident, "", 500, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range page {
		if it.ObjectID == object {
			return it.ID
		}
	}
	t.Fatalf("no ledger item for %s", object)
	return ""
}

func wantItems(t *testing.T, got, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("item %s = %q, want %q", k, got[k], v)
		}
	}
}

// ---- enumeration -----------------------------------------------------------

func TestIncidentEnumeratesAccountScope(t *testing.T) { forEngines(t, runIncidentEnumeratesAccount) }

func runIncidentEnumeratesAccount(t *testing.T, db *store.DB) {
	w := newIncidentWorld(t, db)
	v := w.openAccount(t, incResponder)
	if v.Incident.Status != domain.IncidentOpen || v.Reopened || v.Incident.ScopeID != string(incTarget) || v.Incident.OrgID != orgA {
		t.Fatalf("incident = %+v reopened=%v", v.Incident, v.Reopened)
	}
	got := w.itemMap(t, incResponder, v.Incident.ID)
	want := map[string]string{
		"grant.revoke|grant|g_inc_08":                              "pending/",
		"grant.revoke|grant|g_inc_09":                              "pending/",
		"session.revoke|session|ses_it_1":                          "pending/",
		"developer_credential.revoke|developer_credential|devc_it": "pending/",
		"service_account.review|service_account|sa_it":             "pending/",
		// Objects no workflow revokes are listed, with the closed reason.
		"none|delivered_plaintext|usr_itarget": "out_of_scope/delivered_plaintext",
		"none|cache|usr_itarget":               "out_of_scope/cache",
		"none|running_process|usr_itarget":     "out_of_scope/running_process",
		"none|external_session|usr_itarget":    "out_of_scope/external_session",
	}
	wantItems(t, got, want)
	if len(got) != len(want) {
		t.Errorf("ledger holds %d items, want exactly %d: %v", len(got), len(want), got)
	}
	if v.Counts[domain.ItemPending] != 5 || v.Counts[domain.ItemOutOfScope] != 4 {
		t.Errorf("counts = %v", v.Counts)
	}
}

func TestIncidentBlockedForLimitedResponder(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		v := w.openAccount(t, incLimited)
		got := w.itemMap(t, incLimited, v.Incident.ID)
		wantItems(t, got, map[string]string{
			"grant.revoke|grant|g_inc_08":                              "blocked/needs_another_operator",
			"grant.revoke|grant|g_inc_09":                              "blocked/needs_another_operator",
			"session.revoke|session|ses_it_1":                          "pending/",
			"developer_credential.revoke|developer_credential|devc_it": "blocked/needs_another_operator",
			"service_account.review|service_account|sa_it":             "blocked/needs_another_operator",
		})
		if v.Counts[domain.ItemBlocked] != 4 {
			t.Errorf("counts = %v", v.Counts)
		}
		// An org source the caller may not read is recorded as one blocked row.
		org, err := w.svc.Open(t.Context(), w.as(incLimited), service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgA}})
		if err != nil {
			t.Fatal(err)
		}
		wantItems(t, w.itemMap(t, incLimited, org.Incident.ID), map[string]string{
			"scim_credential.revoke|org|org_a": "blocked/needs_another_operator",
		})
	})
}

func TestIncidentEnumeratesOrgAndProjectScope(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		seedIncidentLeases(t, db)
		ctx := t.Context()

		org, err := w.svc.Open(ctx, w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgA}})
		if err != nil {
			t.Fatalf("open org incident: %v", err)
		}
		got := w.itemMap(t, incResponder, org.Incident.ID)
		wantItems(t, got, map[string]string{
			"account.contain|account|acc_itarget":                      "pending/",
			"developer_credential.revoke|developer_credential|devc_it": "pending/",
			"none|delivered_plaintext|org_a":                           "out_of_scope/delivered_plaintext",
			"none|cache|org_a":                                         "out_of_scope/cache",
			"none|running_process|org_a":                               "out_of_scope/running_process",
			"none|issued_certificate|org_a":                            "out_of_scope/issued_certificate_in_use",
		})
		for k := range got {
			if strings.HasPrefix(k, "scim_credential.revoke") {
				t.Errorf("the responder reads SCIM, so no source is blocked, got %s", k)
			}
			if strings.Contains(k, "org_b") || strings.Contains(k, "lse_it_b") {
				t.Errorf("org A incident lists another org's object %s", k)
			}
		}

		prj, err := w.svc.Open(ctx, w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeProject, Scope: domain.Scope{Org: orgA, Project: prjA1}})
		if err != nil {
			t.Fatalf("open project incident: %v", err)
		}
		got = w.itemMap(t, incResponder, prj.Incident.ID)
		wantItems(t, got, map[string]string{
			"developer_credential.revoke|developer_credential|devc_it":     "pending/",
			"lease.revoke|lease|lse_it_01":                                 "pending/",
			"lease.revoke|lease|lse_it_03":                                 "pending/",
			"lease.revoke|lease|lse_it_06":                                 "pending/",
			"lease.revoke|lease|lse_it_07":                                 "pending/",
			"lease.revoke|lease|lse_it_08":                                 "pending/",
			"dynamic_provider.credential_clear|dynamic_provider|dpr_it_a1": "pending/",
			"dynamic_provider.upstream_revoke|dynamic_provider|dpr_it_a1":  "unknown/upstream_not_observable",
			"none|delivered_plaintext|prj_a1":                              "out_of_scope/delivered_plaintext",
		})
		for _, terminal := range []string{"lse_it_02", "lse_it_04", "lse_it_05", "lse_it_09", "lse_it_b1"} {
			if _, ok := got["lease.revoke|lease|"+terminal]; ok {
				t.Errorf("project A1 incident lists %s, which is terminal or in another project", terminal)
			}
		}

		// A limited responder sees the lease rows blocked and the sources it may
		// not read blocked as a whole.
		lim, err := w.svc.Open(ctx, w.as(incLimited), service.IncidentTarget{Kind: domain.IncidentScopeProject, Scope: domain.Scope{Org: orgA, Project: prjA2}})
		if err != nil {
			t.Fatalf("open limited project incident: %v", err)
		}
		got = w.itemMap(t, incLimited, lim.Incident.ID)
		wantItems(t, got, map[string]string{
			"lease.revoke|lease|lse_it_09":                     "blocked/needs_another_operator",
			"adapter_target.pause|project|prj_a2":              "blocked/needs_another_operator",
			"dynamic_provider.credential_clear|project|prj_a2": "blocked/needs_another_operator",
		})

		// Adapter scope is verified through the adapter owner's read: an adapter
		// that does not exist, and a caller who cannot read adapters, answer alike.
		_, errMissing := w.svc.Open(ctx, w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAdapter, Scope: domain.Scope{Org: orgA, Project: prjA1}, AdapterID: "adp_missing"})
		_, errLimited := w.svc.Open(ctx, w.as(incLimited), service.IncidentTarget{Kind: domain.IncidentScopeAdapter, Scope: domain.Scope{Org: orgA, Project: prjA1}, AdapterID: "adp_missing"})
		assertUniformNotFound(t, errLimited, errMissing)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incidents WHERE scope_kind = 'adapter'`); n != 0 {
			t.Errorf("a refused adapter open left %d incident rows", n)
		}
	})
}

// ---- idempotent open --------------------------------------------------------

func TestIncidentOpenIsIdempotent(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		first := w.openAccount(t, incResponder)
		before := w.itemMap(t, incResponder, first.Incident.ID)

		again := w.openAccount(t, incResponder)
		if again.Incident.ID != first.Incident.ID || !again.Reopened || again.Added != 0 {
			t.Fatalf("re-open = id %s reopened=%v added=%d, want the same incident with nothing added", again.Incident.ID, again.Reopened, again.Added)
		}
		if after := w.itemMap(t, incResponder, first.Incident.ID); !slices.Equal(mapKeys(after), mapKeys(before)) {
			t.Fatalf("re-open changed the ledger: %v -> %v", before, after)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incidents`); n != 1 {
			t.Fatalf("incident rows = %d, want 1", n)
		}

		// A second responder's open lands on the same incident.
		if other := w.openAccount(t, incInstance); other.Incident.ID != first.Incident.ID {
			t.Fatalf("a second opener created incident %s, want %s", other.Incident.ID, first.Incident.ID)
		}

		// An object that appeared after the first enumeration is added; nothing
		// already recorded is touched, and a done item stays done.
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_inc_late', 'usr_itarget', 'publish', 'org_a', `+ts+`)`)
		seedOrigins(t, db)
		grown := w.openAccount(t, incResponder)
		if grown.Incident.ID != first.Incident.ID || grown.Added != 1 {
			t.Fatalf("late open = %s added=%d, want the same incident with one new item", grown.Incident.ID, grown.Added)
		}
		if _, ok := w.itemMap(t, incResponder, first.Incident.ID)["grant.revoke|grant|g_inc_late"]; !ok {
			t.Fatal("the late grant was not enumerated")
		}

		if opened := w.events(t, "incident.opened", first.Incident.ID); opened != 4 {
			t.Errorf("opened events = %d, want one per open call (4)", opened)
		}

		// Closing frees the scope: the next open is a fresh incident.
		if _, err := w.svc.Close(t.Context(), w.as(incResponder), first.Incident.ID, true); err != nil {
			t.Fatal(err)
		}
		if next := w.openAccount(t, incResponder); next.Incident.ID == first.Incident.ID || next.Reopened {
			t.Fatalf("open after close = %s reopened=%v, want a new incident", next.Incident.ID, next.Reopened)
		}
	})
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// ---- resume -----------------------------------------------------------------

// fakeOwner stands in for an owner function. applied is the owner's own state:
// the real owners are idempotent by checking their object, and so is this.
type fakeOwner struct {
	mu         sync.Mutex
	applied    map[string]bool
	actions    int
	result     func(item authz.IncidentItem) (service.IncidentOwnerResult, error)
	afterApply func(item authz.IncidentItem)
}

func newFakeOwner() *fakeOwner { return &fakeOwner{applied: map[string]bool{}} }

func (o *fakeOwner) run(_ context.Context, _ service.Actor, item authz.IncidentItem) (service.IncidentOwnerResult, error) {
	o.mu.Lock()
	first := !o.applied[item.ObjectID]
	if first {
		o.applied[item.ObjectID] = true
		o.actions++
	}
	o.mu.Unlock()
	if first && o.afterApply != nil {
		o.afterApply(item)
	}
	if o.result != nil {
		return o.result(item)
	}
	return service.OwnerConfirmed, nil
}

func (w *incidentWorld) withOwner(o *fakeOwner) {
	w.svc.Owners = map[string]service.IncidentOwner{service.OwnerGrantRevoke: o.run}
}

func TestIncidentResumeRunsOnlyOpenItemsAndOnlyConfirmedIsDone(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		v := w.openAccount(t, incResponder)
		id := v.Incident.ID
		owner := newFakeOwner()
		w.withOwner(owner)

		res, err := w.svc.Resume(t.Context(), w.as(incResponder), id)
		if err != nil {
			t.Fatal(err)
		}
		if res.Examined != 2 || res.Confirmed != 2 || owner.actions != 2 {
			t.Fatalf("resume = %+v, owner actions = %d, want the two grant items examined and confirmed", res, owner.actions)
		}
		// Items whose owner is not registered are skipped and never become done.
		if res.Skipped != 3 {
			t.Errorf("skipped = %d, want 3 (session, developer credential, service account)", res.Skipped)
		}
		got := w.itemMap(t, incResponder, id)
		wantItems(t, got, map[string]string{
			"grant.revoke|grant|g_inc_08":                              "done/",
			"grant.revoke|grant|g_inc_09":                              "done/",
			"session.revoke|session|ses_it_1":                          "pending/",
			"developer_credential.revoke|developer_credential|devc_it": "pending/",
			"service_account.review|service_account|sa_it":             "pending/",
			"none|cache|usr_itarget":                                   "out_of_scope/cache",
		})

		// A second resume re-reads the ledger and runs nothing: done is final.
		again, err := w.svc.Resume(t.Context(), w.as(incResponder), id)
		if err != nil {
			t.Fatal(err)
		}
		if again.Examined != 0 || owner.actions != 2 {
			t.Fatalf("second resume = %+v, owner actions = %d, want no owner call", again, owner.actions)
		}

		// Intent and outcome come in pairs under the incident's correlation id.
		if i, o := w.events(t, "incident.item_intent", id), w.events(t, "incident.item_outcome", id); i != 2 || o != 2 {
			t.Errorf("intent events = %d, outcome events = %d, want 2 and 2", i, o)
		}
	})
}

func TestIncidentUnconfirmedOwnerNeverWritesDone(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		id := w.openAccount(t, incResponder).Incident.ID
		owner := newFakeOwner()
		w.withOwner(owner)

		step := func(name string, f func(authz.IncidentItem) (service.IncidentOwnerResult, error), want map[string]string) {
			t.Helper()
			owner.result = f
			if _, err := w.svc.Resume(t.Context(), w.as(incResponder), id); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			got := w.itemMap(t, incResponder, id)
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s: %s = %q, want %q", name, k, got[k], v)
				}
			}
			for k, v := range got {
				if strings.HasPrefix(v, "done/") && want[k] != v {
					t.Errorf("%s: %s became done without a confirmation", name, k)
				}
			}
		}
		key := func(object string) string { return "grant.revoke|grant|" + object }

		// The owner cannot say: unknown, with the closed reason.
		step("zero value", func(authz.IncidentItem) (service.IncidentOwnerResult, error) {
			return service.OwnerUnconfirmed, nil
		}, map[string]string{key("g_inc_08"): "unknown/owner_unconfirmed", key("g_inc_09"): "unknown/owner_unconfirmed"})

		// A transport error (a lost response, an unreachable provider) is also
		// unknown, never an inferred success.
		step("error", func(item authz.IncidentItem) (service.IncidentOwnerResult, error) {
			if item.ObjectID == "g_inc_08" {
				return service.OwnerConfirmed, errors.New("connection reset after commit")
			}
			return service.OwnerBlocked, nil
		}, map[string]string{key("g_inc_08"): "unknown/owner_unconfirmed", key("g_inc_09"): "blocked/needs_another_operator"})

		// A definite failure leaves the item as it was (here unknown), and blocked
		// and unknown items are all open, so resume runs them again.
		step("failed", func(item authz.IncidentItem) (service.IncidentOwnerResult, error) {
			if item.ObjectID == "g_inc_08" {
				return service.OwnerFailed, nil
			}
			return service.OwnerUnconfirmed, nil
		}, map[string]string{key("g_inc_08"): "unknown/owner_unconfirmed", key("g_inc_09"): "unknown/owner_unconfirmed"})

		// The failure event reports the ledger's actual state for the item (here
		// unknown), never a state the row does not hold.
		failures := queryStrings(t, db, `SELECT CAST(payload AS TEXT) || ';' FROM audit_instance_events WHERE type = 'incident.item_outcome' AND outcome = 'failure' AND correlation_id = '`+id+`'`)
		if !strings.Contains(failures, "owner_failed") || !strings.Contains(failures, "unknown") || strings.Contains(failures, "pending") {
			t.Errorf("failure outcome events = %s, want the row's actual state unknown with reason owner_failed", failures)
		}

		step("confirmed", func(authz.IncidentItem) (service.IncidentOwnerResult, error) {
			return service.OwnerConfirmed, nil
		}, map[string]string{key("g_inc_08"): "done/", key("g_inc_09"): "done/"})
	})
}

// TestIncidentResumeAfterCrashBetweenOwnerCommitAndLedgerUpdate simulates the
// two ways the ledger can lose an owner's outcome: the ledger row moves under
// the owner (a lost update), and the process dies after the owner committed.
// In both the item stays open, resume asks the owner again, the owner does not
// repeat its action, and only its own confirmation writes done.
func TestIncidentResumeAfterCrashBetweenOwnerCommitAndLedgerUpdate(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		id := w.openAccount(t, incResponder).Incident.ID
		owner := newFakeOwner()
		w.withOwner(owner)
		key := "grant.revoke|grant|g_inc_08"

		// Lost update: the owner commits its action, and before the outcome is
		// recorded the ledger row moves.
		owner.afterApply = func(item authz.IncidentItem) {
			if item.ObjectID == "g_inc_08" {
				execRaw(t, db, `UPDATE incident_items SET row_version = row_version + 1 WHERE id = '`+item.ID+`'`)
			}
		}
		res, err := w.svc.Resume(t.Context(), w.as(incResponder), id)
		if err != nil {
			t.Fatal(err)
		}
		if res.Stale != 1 {
			t.Fatalf("resume = %+v, want one stale item", res)
		}
		if got := w.itemMap(t, incResponder, id)[key]; got != "pending/" {
			t.Fatalf("after the lost update %s = %q, want pending (never a spurious done)", key, got)
		}
		if owner.actions != 2 {
			t.Fatalf("owner actions = %d, want 2", owner.actions)
		}
		var unknownOutcomes int64 = queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'incident.item_outcome' AND outcome = 'unknown' AND correlation_id = '`+id+`'`)
		if unknownOutcomes != 1 {
			t.Errorf("unknown outcome events = %d, want 1 for the lost update", unknownOutcomes)
		}

		// Resume asks the owner again; the owner sees its object already in the
		// target state, repeats nothing, and its confirmation writes done.
		owner.afterApply = nil
		if _, err := w.svc.Resume(t.Context(), w.as(incResponder), id); err != nil {
			t.Fatal(err)
		}
		if owner.actions != 2 {
			t.Fatalf("resume repeated the owner action: actions = %d, want 2", owner.actions)
		}
		if got := w.itemMap(t, incResponder, id)[key]; got != "done/" {
			t.Fatalf("after resume %s = %q, want done", key, got)
		}
	})
}

func TestIncidentResumeAfterProcessCrash(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		id := w.openAccount(t, incResponder).Incident.ID
		owner := newFakeOwner()
		w.withOwner(owner)

		// The process dies after the owner committed: the owner call panics, so
		// no outcome is ever written.
		owner.afterApply = func(item authz.IncidentItem) {
			if item.ObjectID == "g_inc_08" {
				panic("simulated process death")
			}
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("the simulated crash did not propagate")
				}
			}()
			_, _ = w.svc.Resume(t.Context(), w.as(incResponder), id)
		}()
		key := "grant.revoke|grant|g_inc_08"
		if got := w.itemMap(t, incResponder, id)[key]; got != "pending/" {
			t.Fatalf("after the crash %s = %q, want pending", key, got)
		}
		if i, o := w.events(t, "incident.item_intent", id), w.events(t, "incident.item_outcome", id); i != 1 || o != 0 {
			t.Fatalf("after the crash intent=%d outcome=%d, want the intent recorded and no outcome", i, o)
		}

		owner.afterApply = nil
		res, err := w.svc.Resume(t.Context(), w.as(incResponder), id)
		if err != nil {
			t.Fatal(err)
		}
		if res.Confirmed != 2 || owner.actions != 2 {
			t.Fatalf("resume = %+v, owner actions = %d, want both confirmed with the committed action not repeated", res, owner.actions)
		}
		wantItems(t, w.itemMap(t, incResponder, id), map[string]string{key: "done/", "grant.revoke|grant|g_inc_09": "done/"})
		// The interrupted item has two intents and one outcome; the trail shows
		// the gap rather than hiding it.
		if i, o := w.events(t, "incident.item_intent", id), w.events(t, "incident.item_outcome", id); i != 3 || o != 2 {
			t.Errorf("trail intent=%d outcome=%d, want 3 and 2", i, o)
		}
	})
}

// ---- attest and close -------------------------------------------------------

func TestIncidentAttestAndClose(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		id := w.openAccount(t, incResponder).Incident.ID
		ctx := t.Context()

		// A clean close with open items is refused, and says why.
		if _, err := w.svc.Close(ctx, w.as(incResponder), id, false); !errors.Is(err, service.ErrIncidentOpenItems) {
			t.Fatalf("clean close with open items = %v, want ErrIncidentOpenItems", err)
		}
		if got := w.itemMap(t, incResponder, id); got == nil || queryInt(t, db, `SELECT COUNT(*) FROM incidents WHERE status = 'open'`) != 1 {
			t.Fatal("a refused close changed the incident")
		}

		// Attest records who and when, never done.
		session := w.itemID(t, id, "ses_it_1")
		if err := w.svc.Attest(ctx, w.as(incResponder), id, session); err != nil {
			t.Fatal(err)
		}
		if got := w.itemMap(t, incResponder, id)["session.revoke|session|ses_it_1"]; got != "attested/" {
			t.Fatalf("attested session = %q", got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE id = '`+session+`' AND attested_by = 'usr_irsp' AND attested_at IS NOT NULL`); n != 1 {
			t.Fatal("the attestation did not record who and when")
		}
		if err := w.svc.Attest(ctx, w.as(incResponder), id, session); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("attesting a resolved item = %v, want a conflict", err)
		}
		oos := w.itemID(t, id, "usr_itarget") // the first out-of-scope row by id order is not open
		if err := w.svc.Attest(ctx, w.as(incResponder), id, oos); err == nil {
			t.Fatal("an out-of-scope item was attested")
		}

		// Closing with residuals lists exactly the open items in the trail.
		view, err := w.svc.Close(ctx, w.as(incResponder), id, true)
		if err != nil {
			t.Fatal(err)
		}
		if view.Incident.Status != domain.IncidentClosedWithResiduals || view.Incident.ClosedBy != incResponder {
			t.Fatalf("closed incident = %+v", view.Incident)
		}
		payload := queryStrings(t, db, `SELECT CAST(payload AS TEXT) FROM audit_instance_events WHERE type = 'incident.closed' AND correlation_id = '`+id+`'`)
		if !strings.Contains(payload, "closed_with_residuals") || !strings.Contains(payload, w.itemID(t, id, "g_inc_08")) {
			t.Errorf("closed event does not list the residuals: %s", payload)
		}
		if _, err := w.svc.Resume(ctx, w.as(incResponder), id); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("resume of a closed incident = %v, want a conflict", err)
		}
		if _, err := w.svc.Close(ctx, w.as(incResponder), id, true); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("second close = %v, want a conflict", err)
		}
	})
}

func TestIncidentCleanCloseWhenNothingIsOpen(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		v, err := w.svc.Open(t.Context(), w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgB}})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("org B open by an org A responder = %v, want not found", err)
		}
		_ = v
		// Attest every open item of an org B incident opened by the org B
		// responder; out-of-scope rows do not block a clean close.
		b, err := w.svc.Open(t.Context(), w.as(incOrgB), service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgB}})
		if err != nil {
			t.Fatal(err)
		}
		items, err := w.svc.Items(t.Context(), w.as(incOrgB), b.Incident.ID, "", 500, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			if err := w.svc.Attest(t.Context(), w.as(incOrgB), b.Incident.ID, it.ID); err != nil {
				t.Fatal(err)
			}
		}
		closed, err := w.svc.Close(t.Context(), w.as(incOrgB), b.Incident.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if closed.Incident.Status != domain.IncidentClosed {
			t.Fatalf("status = %s, want closed", closed.Incident.Status)
		}
	})
}

// ---- authorization ----------------------------------------------------------

func TestIncidentAuthorizationRefusals(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		ctx := t.Context()
		orgTarget := service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgA}}

		// credential-reset at the scope is the whole gate. alice holds read, edit
		// and manage-projects in org A and is refused; so is the nobody.
		for _, p := range []domain.PrincipalID{alice, "usr_nobody", bob, incOrgB} {
			if _, err := w.svc.Open(ctx, w.as(p), orgTarget); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("%s opening an org A incident = %v, want not found", p, err)
			}
		}
		// Account targets answer one way for an unknown target, a refused one and
		// an unauthorized caller.
		_, errReal := w.svc.Open(ctx, w.as("usr_nobody"), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incTarget})
		_, errGhost := w.svc.Open(ctx, w.as("usr_nobody"), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incGhost})
		assertUniformNotFound(t, errReal, errGhost)
		_, errOtherOrg := w.svc.Open(ctx, w.as(incOrgB), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incTarget})
		assertUniformNotFound(t, errOtherOrg, errGhost)
		_, errMissing := w.svc.Open(ctx, w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incGhost})
		assertUniformNotFound(t, errMissing, errGhost)
		// A principal holding an instance capability is host-local: neither the
		// org responder nor the instance responder opens it here.
		for _, p := range []domain.PrincipalID{incResponder, incInstance} {
			_, err := w.svc.Open(ctx, w.as(p), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incHostLocal})
			assertUniformNotFound(t, err, errGhost)
		}
		// Project scope is bounded by the org the project lives in.
		_, err := w.svc.Open(ctx, w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeProject, Scope: domain.Scope{Org: orgA, Project: prjB1}})
		if err == nil {
			t.Error("a project of another org was opened under org A")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incidents`) + queryInt(t, db, `SELECT COUNT(*) FROM incident_items`); n != 0 {
			t.Fatalf("refused opens left %d ledger rows", n)
		}

		// An incident is readable and workable only by who may open its scope.
		v, err := w.svc.Open(ctx, w.as(incResponder), orgTarget)
		if err != nil {
			t.Fatal(err)
		}
		item := w.itemID(t, v.Incident.ID, "acc_itarget")
		_, errGhostID := w.svc.Get(ctx, w.as(incResponder), "inc_does_not_exist")
		for _, p := range []domain.PrincipalID{alice, "usr_nobody", incOrgB} {
			a := w.as(p)
			_, getErr := w.svc.Get(ctx, a, v.Incident.ID)
			assertUniformNotFound(t, getErr, errGhostID)
			_, itemsErr := w.svc.Items(ctx, a, v.Incident.ID, "", 10, false)
			assertUniformNotFound(t, itemsErr, errGhostID)
			_, resumeErr := w.svc.Resume(ctx, a, v.Incident.ID)
			assertUniformNotFound(t, resumeErr, errGhostID)
			_, closeErr := w.svc.Close(ctx, a, v.Incident.ID, true)
			assertUniformNotFound(t, closeErr, errGhostID)
			attestErr := w.svc.Attest(ctx, a, v.Incident.ID, item)
			assertUniformNotFound(t, attestErr, errGhostID)
		}
		// The instance responder holds credential-reset everywhere.
		if _, err := w.svc.Get(ctx, w.as(incInstance), v.Incident.ID); err != nil {
			t.Errorf("instance responder reading an org incident: %v", err)
		}
		if got := w.itemMap(t, incResponder, v.Incident.ID)["account.contain|account|acc_itarget"]; got != "pending/" {
			t.Errorf("refused callers changed the ledger: %q", got)
		}
	})
}

// ---- audit ------------------------------------------------------------------

// runIncidentAuditLifecycle gives every incident event type a real emitter for
// the registry-wide emitter check: open, resume through a confirming owner,
// close.
func runIncidentAuditLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	w := newIncidentWorld(t, db)
	id := w.openAccount(t, incResponder).Incident.ID
	w.withOwner(newFakeOwner())
	if _, err := w.svc.Resume(t.Context(), w.as(incResponder), id); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Close(t.Context(), w.as(incResponder), id, true); err != nil {
		t.Fatal(err)
	}
}

func TestIncidentAuditTrail(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		v := w.openAccount(t, incResponder)
		id := v.Incident.ID
		w.withOwner(newFakeOwner())
		if _, err := w.svc.Resume(t.Context(), w.as(incResponder), id); err != nil {
			t.Fatal(err)
		}
		if _, err := w.svc.Close(t.Context(), w.as(incResponder), id, true); err != nil {
			t.Fatal(err)
		}

		// Every incident event carries the incident id as its correlation id and
		// lands on the instance trail.
		total := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type LIKE 'incident.%'`)
		correlated := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type LIKE 'incident.%' AND correlation_id = '`+id+`'`)
		if total == 0 || total != correlated {
			t.Fatalf("incident events = %d, correlated to the incident = %d", total, correlated)
		}
		for typ, want := range map[string]int64{"incident.opened": 1, "incident.item_intent": 2, "incident.item_outcome": 2, "incident.closed": 1} {
			if got := w.events(t, typ, id); got != want {
				t.Errorf("%s events = %d, want %d", typ, got, want)
			}
		}
		// The trail reads opened, then each item's intent before its outcome, then
		// closed; every event has the outcome its kind implies.
		sequence := queryStrings(t, db, `SELECT type || ':' || outcome || ',' FROM audit_instance_events WHERE type LIKE 'incident.%' ORDER BY seq`)
		wantSequence := "incident.opened:success,incident.item_intent:intent,incident.item_outcome:success,incident.item_intent:intent,incident.item_outcome:success,incident.closed:success,"
		if sequence != wantSequence {
			t.Errorf("incident trail = %s, want %s", sequence, wantSequence)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type LIKE 'incident.%' AND actor_id = 'usr_irsp'`); n != total {
			t.Errorf("events attributed to the responder = %d of %d", n, total)
		}

		// Payloads carry ids, counts and closed reasons only: nothing the target
		// chose (its username, its address) and no secret reaches the trail.
		payloads := queryStrings(t, db, `SELECT CAST(payload AS TEXT) || ';' FROM audit_instance_events WHERE type LIKE 'incident.%' ORDER BY seq`)
		for _, forbidden := range []string{incTargetUsername, incTargetEmail, "verifier", "password"} {
			if strings.Contains(payloads, forbidden) {
				t.Errorf("incident payloads contain %q: %s", forbidden, payloads)
			}
		}
		ledger := queryStrings(t, db, `SELECT owner_function || object_kind || object_id || scope || reason FROM incident_items`)
		for _, forbidden := range []string{incTargetUsername, incTargetEmail, "verifier"} {
			if strings.Contains(ledger, forbidden) {
				t.Errorf("the ledger contains %q", forbidden)
			}
		}
	})
}

// ---- the two new listings ---------------------------------------------------

func seedIncidentLeases(t *testing.T, db *store.DB) {
	t.Helper()
	for _, p := range []struct{ id, org, project, authority string }{
		{"dpr_it_a1", "org_a", "prj_a1", "usr_alice"},
		{"dpr_it_a2", "org_a", "prj_a2", "usr_alice"},
		{"dpr_it_b1", "org_b", "prj_b1", "usr_bob"},
	} {
		execRaw(t, db, `INSERT INTO dynamic_providers (id,org_id,project_id,kind,origin,tls_mode,grant_role,authority_principal_id,state,created_at) VALUES ('`+p.id+`','`+p.org+`','`+p.project+`','postgres','postgres://`+p.id+`.example/db','verify-full','reader','`+p.authority+`','active',`+ts+`)`)
	}
	lease := func(id, org, project, env, provider, state string) {
		execRaw(t, db, `INSERT INTO dynamic_leases (id,org_id,project_id,environment_id,provider_id,principal_id,principal_class,provider_handle,state,max_ttl_seconds,last_transition_at,next_attempt_at,created_at) `+
			`VALUES ('`+id+`','`+org+`','`+project+`','`+env+`','`+provider+`','mch_a1','automation','h_`+id+`','`+state+`',3600,`+ts+`,`+ts+`,`+ts+`)`)
	}
	for i, state := range []string{"active", "revoked", "minting", "expired", "failed", "unknown", "revoking", "renewing"} {
		lease(fmt.Sprintf("lse_it_%02d", i+1), "org_a", "prj_a1", "env_a1", "dpr_it_a1", state)
	}
	lease("lse_it_09", "org_a", "prj_a2", "env_a2", "dpr_it_a2", "active")
	lease("lse_it_b1", "org_b", "prj_b1", "env_b1", "dpr_it_b1", "active")
}

func TestIncidentNonTerminalLeaseListing(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		seedIncidentLeases(t, db)
		ctx := t.Context()
		a1 := domain.Scope{Org: orgA, Project: prjA1}
		ids := func(rows []store.DynamicLeaseRef) []string {
			out := make([]string, 0, len(rows))
			for _, r := range rows {
				out = append(out, r.ID)
			}
			return out
		}
		leasesBefore := queryInt(t, db, `SELECT COUNT(*) FROM dynamic_leases`)

		// Non-terminal means not revoked, expired or failed; the order is stable
		// by id and the page bounded by the limit.
		all, err := w.svc.NonTerminalLeases(ctx, w.as(incResponder), a1, "", 500)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"lse_it_01", "lse_it_03", "lse_it_06", "lse_it_07", "lse_it_08"}
		if !slices.Equal(ids(all), want) {
			t.Fatalf("project listing = %v, want %v", ids(all), want)
		}
		var paged []string
		after := ""
		for {
			page, err := w.svc.NonTerminalLeases(ctx, w.as(incResponder), a1, after, 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) > 2 {
				t.Fatalf("page of %d rows exceeds the limit 2", len(page))
			}
			paged = append(paged, ids(page)...)
			if len(page) < 2 {
				break
			}
			after = page[len(page)-1].ID
		}
		if !slices.Equal(paged, want) {
			t.Fatalf("paged project listing = %v, want %v", paged, want)
		}
		for _, r := range all {
			if r.OrgID != "org_a" || r.ProjectID != "prj_a1" || r.EnvironmentID != "env_a1" || r.ProviderID != "dpr_it_a1" {
				t.Errorf("row %+v is not scoped to the project", r)
			}
		}

		// Instance level lists every org, still bounded and ordered.
		inst, err := w.svc.NonTerminalLeases(ctx, w.as(incInstance), domain.Scope{}, "", 500)
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(inst); !slices.Equal(got, []string{"lse_it_01", "lse_it_03", "lse_it_06", "lse_it_07", "lse_it_08", "lse_it_09", "lse_it_b1"}) {
			t.Fatalf("instance listing = %v", got)
		}
		if page, err := w.svc.NonTerminalLeases(ctx, w.as(incInstance), domain.Scope{}, "lse_it_08", 1); err != nil || len(page) != 1 || page[0].ID != "lse_it_09" {
			t.Fatalf("instance page after lse_it_08 = %v, %v", page, err)
		}

		// Refusals: the project listing needs credential-reset at its org, the
		// instance listing needs it at instance scope.
		for name, probe := range map[string]func() error{
			"nobody, project": func() error { _, e := w.svc.NonTerminalLeases(ctx, w.as("usr_nobody"), a1, "", 10); return e },
			"alice, project":  func() error { _, e := w.svc.NonTerminalLeases(ctx, w.as(alice), a1, "", 10); return e },
			"org B responder": func() error { _, e := w.svc.NonTerminalLeases(ctx, w.as(incOrgB), a1, "", 10); return e },
			"org A responder, B": func() error {
				_, e := w.svc.NonTerminalLeases(ctx, w.as(incResponder), domain.Scope{Org: orgB, Project: prjB1}, "", 10)
				return e
			},
			"org responder, instance": func() error {
				_, e := w.svc.NonTerminalLeases(ctx, w.as(incResponder), domain.Scope{}, "", 10)
				return e
			},
			"nobody, instance": func() error {
				_, e := w.svc.NonTerminalLeases(ctx, w.as("usr_nobody"), domain.Scope{}, "", 10)
				return e
			},
		} {
			if err := probe(); err == nil {
				t.Errorf("%s: the listing was served", name)
			}
		}
		for name, limit := range map[string]int{"zero": 0, "negative": -1, "over the bound": store.MaxDynamicLeasePage + 1} {
			if _, err := w.svc.NonTerminalLeases(ctx, w.as(incResponder), a1, "", limit); !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("limit %s = %v, want invalid", name, err)
			}
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM dynamic_leases`); got != leasesBefore {
			t.Errorf("a read-only listing changed the lease table: %d -> %d", leasesBefore, got)
		}
	})
}

func TestIncidentDelegationListing(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		ctx := t.Context()
		insertDeveloperCredential(t, db, "devc_a1", "devp_a1", "usr_alice", "org_a", "prj_a1", "env_a1", "2099-01-01T00:00:00.000000Z")
		insertDeveloperCredential(t, db, "devc_a2", "devp_a2", "usr_bob", "org_a", "prj_a2", "env_a2", "2099-01-01T00:00:00.000000Z")
		insertDeveloperCredential(t, db, "devc_old", "devp_old", "usr_alice", "org_a", "prj_a1", "env_a1", "2026-01-02T00:00:00.000000Z")
		insertDeveloperCredential(t, db, "devc_rev", "devp_rev", "usr_alice", "org_a", "prj_a1", "env_a1", "2099-01-01T00:00:00.000000Z")
		execRaw(t, db, `UPDATE developer_credentials SET revoked_at = `+ts+` WHERE id = 'devc_rev'`)
		insertDeveloperCredential(t, db, "devc_b1", "devp_b1", "usr_bob", "org_b", "prj_b1", "env_b1", "2099-01-01T00:00:00.000000Z")

		ids := func(rows []authz.DeveloperCredential) []string {
			out := make([]string, 0, len(rows))
			for _, r := range rows {
				out = append(out, r.ID)
			}
			return out
		}
		// Live delegations of every principal in the org, in id order: not the
		// expired, not the revoked and not another org's.
		all, err := w.svc.DelegationsInOrg(ctx, w.as(incResponder), orgA, "", 500)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"devc_a1", "devc_a2", "devc_it"}
		if !slices.Equal(ids(all), want) {
			t.Fatalf("org A delegations = %v, want %v", ids(all), want)
		}
		var paged []string
		after := ""
		for {
			page, err := w.svc.DelegationsInOrg(ctx, w.as(incResponder), orgA, after, 1)
			if err != nil {
				t.Fatal(err)
			}
			paged = append(paged, ids(page)...)
			if len(page) < 1 {
				break
			}
			after = page[len(page)-1].ID
		}
		if !slices.Equal(paged, want) {
			t.Fatalf("paged delegations = %v, want %v", paged, want)
		}
		if b, err := w.svc.DelegationsInOrg(ctx, w.as(incOrgB), orgB, "", 500); err != nil || !slices.Equal(ids(b), []string{"devc_b1"}) {
			t.Fatalf("org B delegations = %v, %v", ids(b), err)
		}

		for name, probe := range map[string]func() error{
			"nobody":        func() error { _, e := w.svc.DelegationsInOrg(ctx, w.as("usr_nobody"), orgA, "", 10); return e },
			"alice":         func() error { _, e := w.svc.DelegationsInOrg(ctx, w.as(alice), orgA, "", 10); return e },
			"limited holds": func() error { _, e := w.svc.DelegationsInOrg(ctx, w.as(incOrgB), orgA, "", 10); return e },
		} {
			if err := probe(); err == nil {
				t.Errorf("%s: the listing was served", name)
			}
		}
		if _, err := w.svc.DelegationsInOrg(ctx, w.as(incResponder), orgA, "", 0); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("limit 0 = %v, want invalid", err)
		}
		if _, err := w.svc.DelegationsInOrg(ctx, w.as(incResponder), orgA, "", 501); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("limit 501 = %v, want invalid", err)
		}
	})
}

// ---- the ledger itself ------------------------------------------------------

func TestIncidentLedgerRejectsOutsideTheClosedVocabulary(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		insertIncident := func(id, status, closedAt string) error {
			return execRawErr(t, db, `INSERT INTO incidents (id, scope_kind, scope_id, org_id, status, opened_by, opened_at, closed_at) VALUES ('`+id+`', 'org', 'org_a', 'org_a', '`+status+`', 'usr_irsp', `+ts+`, `+closedAt+`)`)
		}
		if err := insertIncident("inc_ok", "open", "NULL"); err != nil {
			t.Fatal(err)
		}
		if insertIncident("inc_dup", "open", "NULL") == nil {
			t.Error("a second open incident for the same scope was accepted")
		}
		if insertIncident("inc_bad", "paused", "NULL") == nil {
			t.Error("a status outside the closed set was accepted")
		}
		if insertIncident("inc_inconsistent", "closed", "NULL") == nil {
			t.Error("a closed incident without a close time was accepted")
		}
		if err := insertIncident("inc_closed", "closed", ts); err != nil {
			t.Errorf("a closed incident beside an open one was refused: %v", err)
		}

		item := func(id, object, outcome, reason string) error {
			return execRawErr(t, db, `INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, created_at, updated_at) `+
				`VALUES ('`+id+`', 'inc_ok', 'grant.revoke', 'grant', '`+object+`', 'org:org_a', '`+outcome+`', '`+reason+`', `+ts+`, `+ts+`)`)
		}
		if err := item("itm_ok", "g1", "pending", ""); err != nil {
			t.Fatal(err)
		}
		if item("itm_dup", "g1", "pending", "") == nil {
			t.Error("a duplicate (incident, owner, kind, id) was accepted")
		}
		if item("itm_outcome", "g2", "revoked", "") == nil {
			t.Error("an outcome outside the closed set was accepted")
		}
		if item("itm_reason", "g3", "unknown", "the user typed this") == nil {
			t.Error("a free-text reason was accepted")
		}
		if item("itm_oos", "g4", "out_of_scope", "owner_failed") == nil {
			t.Error("an out-of-scope row with an in-scope reason was accepted")
		}
		if item("itm_attest", "g5", "attested", "") == nil {
			t.Error("an attested row without who and when was accepted")
		}
	})
}
