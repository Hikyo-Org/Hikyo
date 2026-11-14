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
		`VALUES ('`+id+`', '`+devPrincipal+`', '`+authority+`', '`+org+`', '`+project+`', '`+env+`', `+blobLit(db, []byte("verifier-"+id))+`, 'dev', 'ses_x_`+id+`', '', '', '', 'password', 1, (SELECT credential_epoch FROM auth_instance_state WHERE id = 1), `+ts+`, '`+expires+`')`)
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
		"session.revoke|session|ses_it_1":                          "pending/",
		"developer_credential.revoke|developer_credential|devc_it": "pending/",
		"service_account.review|service_account|sa_it":             "pending/",
		// Objects no workflow revokes are listed, with the closed reason.
		"none|delivered_plaintext|usr_itarget": "out_of_scope/delivered_plaintext",
		"none|cache|usr_itarget":               "out_of_scope/cache",
		"none|running_process|usr_itarget":     "out_of_scope/running_process",
		"none|external_session|usr_itarget":    "out_of_scope/external_session",
		"none|issued_certificate|usr_itarget":  "out_of_scope/issued_certificate_in_use",
	}
	wantItems(t, got, want)
	if len(got) != len(want) {
		t.Errorf("ledger holds %d items, want exactly %d: %v", len(got), len(want), got)
	}
	if v.Counts[domain.ItemPending] != 3 || v.Counts[domain.ItemOutOfScope] != 5 {
		t.Errorf("counts = %v", v.Counts)
	}
	// Grants are census context, never revoke work: containment keeps them and
	// release never regrants (ADR O1, O4).
	for k := range got {
		if strings.Contains(k, "grant") {
			t.Errorf("a grant became ledger work: %s", k)
		}
	}
	if v.Census.Grants != 2 || v.Census.Rules != 0 {
		t.Errorf("census = %+v, want 2 grants and no rules", v.Census)
	}
}

func TestIncidentBlockedForLimitedResponder(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		v := w.openAccount(t, incLimited)
		got := w.itemMap(t, incLimited, v.Incident.ID)
		wantItems(t, got, map[string]string{
			"session.revoke|session|ses_it_1":                          "pending/",
			"developer_credential.revoke|developer_credential|devc_it": "blocked/needs_another_operator",
			"service_account.review|service_account|sa_it":             "blocked/needs_another_operator",
		})
		if v.Counts[domain.ItemBlocked] != 2 {
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
			"account.contain|account|usr_itarget":                      "pending/",
			"developer_credential.revoke|developer_credential|devc_it": "pending/",
			"none|delivered_plaintext|org_a":                           "out_of_scope/delivered_plaintext",
			"none|cache|org_a":                                         "out_of_scope/cache",
			"none|running_process|org_a":                               "out_of_scope/running_process",
			"none|external_session|org_a":                              "out_of_scope/external_session",
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

		// Adapter scope is verified through the adapter owner's read for a caller
		// who holds it: an adapter that does not exist is not found, and nothing is
		// written.
		adapterTarget := func(id string) service.IncidentTarget {
			return service.IncidentTarget{Kind: domain.IncidentScopeAdapter, Scope: domain.Scope{Org: orgA, Project: prjA1}, AdapterID: id}
		}
		_, errMissing := w.svc.Open(ctx, w.as(incResponder), adapterTarget("adp_missing"))
		if !errors.Is(errMissing, domain.ErrNotFound) {
			t.Errorf("a responder opening a missing adapter = %v, want not found", errMissing)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incidents WHERE scope_kind = 'adapter'`); n != 0 {
			t.Errorf("a refused adapter open left %d incident rows", n)
		}
		// An id that is not a plain Hikyo id never reaches the ledger.
		if _, err := w.svc.Open(ctx, w.as(incResponder), adapterTarget("adp bad;id")); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("an invalid adapter id = %v, want invalid", err)
		}
		// Credential-reset opens an adapter incident (ADR O2): a caller without the
		// adapter owner's capability gets the adapter recorded blocked, unverified.
		limited, err := w.svc.Open(ctx, w.as(incLimited), adapterTarget("adp_missing"))
		if err != nil {
			t.Fatalf("a credential-reset holder opening an adapter incident: %v", err)
		}
		wantItems(t, w.itemMap(t, incLimited, limited.Incident.ID), map[string]string{
			"adapter_target.pause|adapter|adp_missing": "blocked/needs_another_operator",
		})
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
		execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
			`VALUES ('ses_it_late', 'usr_itarget', `+blobLit(db, []byte("verifier-it-late"))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)
		grown := w.openAccount(t, incResponder)
		if grown.Incident.ID != first.Incident.ID || grown.Added != 1 {
			t.Fatalf("late open = %s added=%d, want the same incident with one new item", grown.Incident.ID, grown.Added)
		}
		if _, ok := w.itemMap(t, incResponder, first.Incident.ID)["session.revoke|session|ses_it_late"]; !ok {
			t.Fatal("the late session was not enumerated")
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

// The fake owner stands in for the session and developer credential owners; the
// service account item has no registered owner.
const (
	keySession = "session.revoke|session|ses_it_1"
	keyDevCred = "developer_credential.revoke|developer_credential|devc_it"
	keyService = "service_account.review|service_account|sa_it"
)

func (w *incidentWorld) withOwner(o *fakeOwner) {
	w.svc.Owners = map[string]service.IncidentOwner{domain.OwnerSessionRevoke: o.run, domain.OwnerDeveloperRevoke: o.run}
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
			t.Fatalf("resume = %+v, owner actions = %d, want the session and developer credential items examined and confirmed", res, owner.actions)
		}
		// Items whose owner is not registered are skipped and never become done.
		if res.Skipped != 1 {
			t.Errorf("skipped = %d, want 1 (service account)", res.Skipped)
		}
		got := w.itemMap(t, incResponder, id)
		wantItems(t, got, map[string]string{
			keySession:               "done/",
			keyDevCred:               "done/",
			keyService:               "pending/",
			"none|cache|usr_itarget": "out_of_scope/cache",
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
		ses, dev := keySession, keyDevCred

		// The owner cannot say: unknown, with the closed reason.
		step("zero value", func(authz.IncidentItem) (service.IncidentOwnerResult, error) {
			return service.OwnerUnconfirmed, nil
		}, map[string]string{ses: "unknown/owner_unconfirmed", dev: "unknown/owner_unconfirmed"})

		// A transport error (a lost response, an unreachable provider) is also
		// unknown, never an inferred success.
		step("error", func(item authz.IncidentItem) (service.IncidentOwnerResult, error) {
			if item.ObjectID == "ses_it_1" {
				return service.OwnerConfirmed, errors.New("connection reset after commit")
			}
			return service.OwnerBlocked, nil
		}, map[string]string{ses: "unknown/owner_unconfirmed", dev: "blocked/needs_another_operator"})

		// A definite failure leaves the item as it was (here unknown), and blocked
		// and unknown items are all open, so resume runs them again.
		step("failed", func(item authz.IncidentItem) (service.IncidentOwnerResult, error) {
			if item.ObjectID == "ses_it_1" {
				return service.OwnerFailed, nil
			}
			return service.OwnerUnconfirmed, nil
		}, map[string]string{ses: "unknown/owner_unconfirmed", dev: "unknown/owner_unconfirmed"})

		// The failure event reports the ledger's actual state for the item (here
		// unknown), never a state the row does not hold.
		failures := queryStrings(t, db, `SELECT CAST(payload AS TEXT) || ';' FROM audit_instance_events WHERE type = 'incident.item_outcome' AND outcome = 'failure' AND correlation_id = '`+id+`'`)
		if !strings.Contains(failures, "owner_failed") || !strings.Contains(failures, "unknown") || strings.Contains(failures, "pending") {
			t.Errorf("failure outcome events = %s, want the row's actual state unknown with reason owner_failed", failures)
		}

		step("confirmed", func(authz.IncidentItem) (service.IncidentOwnerResult, error) {
			return service.OwnerConfirmed, nil
		}, map[string]string{ses: "done/", dev: "done/"})
	})
}

// TestIncidentResumeAfterCrashBetweenOwnerCommitAndLedgerUpdate covers the lost
// update: the ledger row moves under the owner after its action committed. The
// item stays open, resume asks the owner again, the owner does not repeat its
// action, and only its own confirmation writes done. The process crash is the
// next test.
func TestIncidentResumeAfterCrashBetweenOwnerCommitAndLedgerUpdate(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		id := w.openAccount(t, incResponder).Incident.ID
		owner := newFakeOwner()
		w.withOwner(owner)
		key := keySession

		// Lost update: the owner commits its action, and before the outcome is
		// recorded the ledger row moves.
		owner.afterApply = func(item authz.IncidentItem) {
			if item.ObjectID == "ses_it_1" {
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
		unknownOutcomes := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'incident.item_outcome' AND outcome = 'unknown' AND correlation_id = '`+id+`'`)
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
			if item.ObjectID == "ses_it_1" {
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
		key := keySession
		if got := w.itemMap(t, incResponder, id)[key]; got != "pending/" {
			t.Fatalf("after the crash %s = %q, want pending", key, got)
		}
		if i, o := w.events(t, "incident.item_intent", id), w.events(t, "incident.item_outcome", id); i-o != 1 {
			t.Fatalf("after the crash intent=%d outcome=%d, want exactly one intent without an outcome", i, o)
		}

		owner.afterApply = nil
		res, err := w.svc.Resume(t.Context(), w.as(incResponder), id)
		if err != nil {
			t.Fatal(err)
		}
		if res.Confirmed != 2 || owner.actions != 2 {
			t.Fatalf("resume = %+v, owner actions = %d, want both confirmed with the committed action not repeated", res, owner.actions)
		}
		wantItems(t, w.itemMap(t, incResponder, id), map[string]string{key: "done/", keyDevCred: "done/"})
		// The interrupted item has two intents and one outcome; the trail shows
		// the gap rather than hiding it.
		if i, o := w.events(t, "incident.item_intent", id), w.events(t, "incident.item_outcome", id); i-o != 1 || o != 2 {
			t.Errorf("trail intent=%d outcome=%d, want two outcomes and one intent without one", i, o)
		}
	})
}

// ---- attest and close -------------------------------------------------------

// seedIncidentFactors gives the account target the credentials Hikyo holds and
// cannot remove administratively: a confirmed TOTP factor, a passkey, a linked
// identity, and a rule that grants no grant-visible access.
func seedIncidentFactors(t *testing.T, db *store.DB) {
	t.Helper()
	epoch := acctEpoch(t, db)
	execRaw(t, db, `INSERT INTO totp_credentials (id, account_id, seed, dek_version, credential_epoch, row_version, last_step, created_step, confirmed_at, created_at) `+
		`VALUES ('totp_it', 'acc_itarget', `+blobLit(db, []byte("seed-it"))+`, 1, `+epoch+`, 1, 0, 0, `+ts+`, `+ts+`)`)
	execRaw(t, db, `INSERT INTO webauthn_credentials (id,account_id,credential_id,public_key,aaguid,sign_count,transports,discoverable,backup_eligible,backup_state,label,credential_epoch,row_version,created_at) `+
		`VALUES ('wac_it','acc_itarget',`+blobLit(db, []byte("cred-it"))+`,`+blobLit(db, []byte("pk-it"))+`,`+blobLit(db, []byte("aa-it"))+`,0,'[]',1,0,0,'key',`+epoch+`,1,`+ts+`)`)
	for _, x := range []string{"xid_it", "xid_it2"} {
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('`+x+`','acc_itarget','oidc','https://idp.example.test','subject-`+x+`','',`+epoch+`,`+ts+`)`)
	}
	execRaw(t, db, `INSERT INTO rules (id, principal_id, capability, org_id, env_mode, key_mode, created_by, created_at) VALUES ('rul_it', 'usr_itarget', 'read', 'org_a', 'all', 'all', 'usr_alice', `+ts+`)`)
	execRaw(t, db, `INSERT INTO rule_items (id, rule_id, org_id, axis, project_id) VALUES ('rli_it', 'rul_it', 'org_a', 'project', 'prj_a1')`)
}

const (
	keyTOTP     = "factor.remove|totp_factor|totp_it"
	keyPasskey  = "factor.remove|passkey|wac_it"
	keyIdentity = "external_identity.remove|external_identity|xid_it"
	keyIdent2   = "external_identity.remove|external_identity|xid_it2"
)

func (w *incidentWorld) itemIDByKey(t *testing.T, actor domain.PrincipalID, incident, key string) string {
	t.Helper()
	page, err := w.svc.Items(t.Context(), w.as(actor), incident, "", 500, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range page {
		if it.OwnerFunction+"|"+it.ObjectKind+"|"+it.ObjectID == key {
			return it.ID
		}
	}
	t.Fatalf("no ledger item %s", key)
	return ""
}

func TestIncidentEnumeratesFactorsAndRulesAsCensus(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		seedIncidentFactors(t, db)
		v := w.openAccount(t, incResponder)
		got := w.itemMap(t, incResponder, v.Incident.ID)
		// Hikyo holds these and has no administrative removal: each is an open
		// ledger item with the closed reason that says so, never a revoke.
		wantItems(t, got, map[string]string{
			keyTOTP:     "unknown/no_revoke_owner",
			keyPasskey:  "unknown/no_revoke_owner",
			keyIdentity: "unknown/no_revoke_owner",
		})
		if v.Census.Rules != 1 || v.Census.Grants != 2 {
			t.Errorf("census = %+v, want 2 grants and 1 rule", v.Census)
		}
		for k := range got {
			if strings.Contains(k, "rul_it") || strings.Contains(k, "grant") {
				t.Errorf("a grant or rule became ledger work: %s", k)
			}
		}
		// The opened event carries the census counts.
		payload := queryStrings(t, db, `SELECT CAST(payload AS TEXT) FROM audit_instance_events WHERE type = 'incident.opened' AND correlation_id = '`+v.Incident.ID+`'`)
		if !strings.Contains(payload, `"rules":1`) || !strings.Contains(payload, `"grants":2`) {
			t.Errorf("opened payload lacks the census: %s", payload)
		}
	})
}

func TestIncidentAttestOnlyWhatHikyoCannotObserve(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		seedIncidentFactors(t, db)
		seedIncidentLeases(t, db)
		ctx := t.Context()
		id := w.openAccount(t, incResponder).Incident.ID

		// A clean close with open items is refused, and says why.
		if _, err := w.svc.Close(ctx, w.as(incResponder), id, false); !errors.Is(err, service.ErrIncidentOpenItems) {
			t.Fatalf("clean close with open items = %v, want ErrIncidentOpenItems", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM incidents WHERE status = 'open'`) != 1 {
			t.Fatal("a refused close changed the incident")
		}

		// An item Hikyo can observe (a session, a developer credential, a service
		// account, a factor row that still exists) is never attestable: the owner's
		// confirmation is the only road to done, and an attestation must not stand
		// in for it. Factor items stay open residuals in S2.
		for _, key := range []string{keySession, keyDevCred, keyService, keyTOTP, keyPasskey} {
			if err := w.svc.Attest(ctx, w.as(incResponder), id, w.itemIDByKey(t, incResponder, id, key)); !errors.Is(err, domain.ErrConflict) {
				t.Errorf("attesting observable item %s = %v, want a conflict", key, err)
			}
		}
		// Out-of-scope rows are not open.
		oos := w.itemIDByKey(t, incResponder, id, "none|cache|usr_itarget")
		if err := w.svc.Attest(ctx, w.as(incResponder), id, oos); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("attesting an out-of-scope item = %v, want a conflict", err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE outcome = 'attested'`); n != 0 {
			t.Fatalf("%d items attested by refused calls", n)
		}

		// A linked identity at its provider is attestable; attest records who and
		// when, never done.
		ident := w.itemIDByKey(t, incResponder, id, keyIdentity)
		if err := w.svc.Attest(ctx, w.as(incResponder), id, ident); err != nil {
			t.Fatal(err)
		}
		if got := w.itemMap(t, incResponder, id)[keyIdentity]; got != "attested/" {
			t.Fatalf("attested identity = %q", got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE id = '`+ident+`' AND attested_by = 'usr_irsp' AND attested_at IS NOT NULL`); n != 1 {
			t.Fatal("the attestation did not record who and when")
		}
		if err := w.svc.Attest(ctx, w.as(incResponder), id, ident); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("attesting a resolved item = %v, want a conflict", err)
		}

		// An upstream revocation Hikyo cannot see keeps its closed reason.
		prj, err := w.svc.Open(ctx, w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeProject, Scope: domain.Scope{Org: orgA, Project: prjA1}})
		if err != nil {
			t.Fatal(err)
		}
		up := "dynamic_provider.upstream_revoke|dynamic_provider|dpr_it_a1"
		if err := w.svc.Attest(ctx, w.as(incResponder), prj.Incident.ID, w.itemIDByKey(t, incResponder, prj.Incident.ID, up)); err != nil {
			t.Fatal(err)
		}
		if got := w.itemMap(t, incResponder, prj.Incident.ID)[up]; got != "attested/upstream_not_observable" {
			t.Errorf("attested upstream item = %q", got)
		}

		// A blocked item needs another operator's capability: the caller who
		// cannot act on it cannot attest it either.
		lim := w.openAccount(t, incLimited)
		if lim.Incident.ID != id {
			t.Fatalf("limited open landed on %s, want the open incident %s", lim.Incident.ID, id)
		}
		blocked := w.itemIDByKey(t, incResponder, id, keyIdent2)
		execRaw(t, db, `UPDATE incident_items SET outcome = 'blocked', reason = 'needs_another_operator' WHERE id = '`+blocked+`'`)
		if err := w.svc.Attest(ctx, w.as(incResponder), id, blocked); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("attesting a blocked item = %v, want a conflict", err)
		}
	})
}

func TestIncidentTargetCannotAttestItsOwnIncident(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		const self = domain.PrincipalID("usr_iself")
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_iself', 'human', `+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_inc_self', 'usr_iself', 'credential-reset', 'org_a', `+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_inc_self2', 'usr_iself', 'read', 'org_a', `+ts+`)`)
		execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_iself', 'usr_iself', 'it-self', 'Self', 'it-self@example.test', `+ts+`, `+ts+`)`)
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_self','acc_iself','oidc','https://idp.example.test','subject-self','',`+acctEpoch(t, db)+`,`+ts+`)`)
		v, err := w.svc.Open(t.Context(), w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: self})
		if err != nil {
			t.Fatal(err)
		}
		item := w.itemIDByKey(t, incResponder, v.Incident.ID, "external_identity.remove|external_identity|xid_self")
		// The person an incident is about does not mark their own containment
		// resolved, even holding credential-reset: the answer is the uniform one.
		errSelf := w.svc.Attest(t.Context(), w.as(self), v.Incident.ID, item)
		errGhost := w.svc.Attest(t.Context(), w.as(self), "inc_does_not_exist", item)
		assertUniformNotFound(t, errSelf, errGhost)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE outcome = 'attested'`); n != 0 {
			t.Fatalf("a refused self-attest wrote %d attestations", n)
		}
		// The same holds for closing it with residuals: the target does not end
		// their own containment record either.
		_, errSelfClose := w.svc.Close(t.Context(), w.as(self), v.Incident.ID, true)
		_, errGhostClose := w.svc.Close(t.Context(), w.as(self), "inc_does_not_exist", true)
		assertUniformNotFound(t, errSelfClose, errGhostClose)
		if st := queryStrings(t, db, `SELECT status FROM incidents WHERE id = '`+string(v.Incident.ID)+`'`); st != "open" {
			t.Fatalf("a refused self-close left status %q", st)
		}
		if err := w.svc.Attest(t.Context(), w.as(incResponder), v.Incident.ID, item); err != nil {
			t.Fatalf("another responder attesting: %v", err)
		}
	})
}

func TestIncidentCloseResiduals(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		ctx := t.Context()
		id := w.openAccount(t, incResponder).Incident.ID
		// A thousand and more open items: the closing event lists a bounded set and
		// says how many it left out.
		execRaw(t, db, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 1100) `+
			`INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, created_at, updated_at) `+
			`SELECT 'itm_bulk_' || i, '`+id+`', 'session.revoke', 'session', 'ses_bulk_' || i, 'instance', 'pending', '', `+ts+`, `+ts+` FROM n`)
		view, err := w.svc.Close(ctx, w.as(incResponder), id, true)
		if err != nil {
			t.Fatal(err)
		}
		if view.Incident.Status != domain.IncidentClosedWithResiduals || view.Incident.ClosedBy != incResponder {
			t.Fatalf("closed incident = %+v", view.Incident)
		}
		payload := queryStrings(t, db, `SELECT CAST(payload AS TEXT) FROM audit_instance_events WHERE type = 'incident.closed' AND correlation_id = '`+id+`'`)
		if !strings.Contains(payload, "closed_with_residuals") {
			t.Errorf("closed event lacks the status: %s", payload)
		}
		open := queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE incident_id = '`+id+`' AND outcome IN ('pending','unknown','blocked')`)
		if want := fmt.Sprintf(`"residuals_omitted":%d`, open-1000); !strings.Contains(payload, want) {
			t.Errorf("closed event does not report %d omitted residuals (%d open): %s", open-1000, open, payload)
		}
		if _, err := w.svc.Resume(ctx, w.as(incResponder), id); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("resume of a closed incident = %v, want a conflict", err)
		}
		if _, err := w.svc.Close(ctx, w.as(incResponder), id, true); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("second close = %v, want a conflict", err)
		}
	})
}

func TestIncidentCleanCloseNeedsEveryObservableItemConfirmed(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		ctx := t.Context()
		// An incident of another org is not this responder's to open or close.
		if _, err := w.svc.Open(ctx, w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgB}}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("org B open by an org A responder = %v, want not found", err)
		}
		id := w.openAccount(t, incResponder).Incident.ID
		if _, err := w.svc.Close(ctx, w.as(incResponder), id, false); !errors.Is(err, service.ErrIncidentOpenItems) {
			t.Fatalf("clean close with observable items open = %v, want ErrIncidentOpenItems", err)
		}
		// Every observable item is confirmed by its owner; out-of-scope rows do not
		// block a clean close.
		owner := newFakeOwner()
		w.withOwner(owner)
		w.svc.Owners[domain.OwnerServiceAccountReview] = owner.run
		res, err := w.svc.Resume(ctx, w.as(incResponder), id)
		if err != nil || res.Confirmed != 3 {
			t.Fatalf("resume = %+v, %v, want the three observable items confirmed", res, err)
		}
		closed, err := w.svc.Close(ctx, w.as(incResponder), id, false)
		if err != nil {
			t.Fatal(err)
		}
		if closed.Incident.Status != domain.IncidentClosed {
			t.Fatalf("status = %s, want closed", closed.Incident.Status)
		}
	})
}

// ---- the open slot is per org ------------------------------------------------

func TestIncidentReopenAfterFootprintGrowth(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		ctx := t.Context()
		first := w.openAccount(t, incResponder)
		if first.Incident.OrgID != orgA {
			t.Fatalf("incident org = %q, want org A", first.Incident.OrgID)
		}
		before := w.itemMap(t, incResponder, first.Incident.ID)

		// The person gains a footprint in org B. Org A's responder no longer reaches
		// the whole person; the instance responder's open must not drop org B's
		// objects into the incident org A's holders read.
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_inc_b', 'usr_itarget', 'read', 'org_b', `+ts+`)`)
		execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
			`VALUES ('ses_it_b', 'usr_itarget', `+blobLit(db, []byte("verifier-it-b"))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)
		target := service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incTarget}
		_, errGhost := w.svc.Open(ctx, w.as("usr_nobody"), target)
		_, errA := w.svc.Open(ctx, w.as(incResponder), target)
		assertUniformNotFound(t, errA, errGhost)

		fresh, err := w.svc.Open(ctx, w.as(incInstance), target)
		if err != nil {
			t.Fatal(err)
		}
		if fresh.Incident.ID == first.Incident.ID || fresh.Incident.OrgID != "" || fresh.Reopened {
			t.Fatalf("instance open = %+v reopened=%v, want a new instance-owned incident", fresh.Incident, fresh.Reopened)
		}
		if _, ok := w.itemMap(t, incInstance, fresh.Incident.ID)["session.revoke|session|ses_it_b"]; !ok {
			t.Error("the instance incident lacks the org B session")
		}
		if again, err := w.svc.Open(ctx, w.as(incInstance), target); err != nil || again.Incident.ID != fresh.Incident.ID || !again.Reopened {
			t.Fatalf("second instance open = %+v, %v, want the same incident", again.Incident, err)
		}

		// Org A's incident is untouched and still its own; it never sees the
		// instance incident.
		if after := w.itemMap(t, incResponder, first.Incident.ID); !slices.Equal(mapKeys(after), mapKeys(before)) {
			t.Fatalf("the instance open changed org A's ledger: %v -> %v", before, after)
		}
		_, errRead := w.svc.Get(ctx, w.as(incResponder), fresh.Incident.ID)
		_, errMissing := w.svc.Get(ctx, w.as(incResponder), "inc_does_not_exist")
		assertUniformNotFound(t, errRead, errMissing)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incidents WHERE status = 'open'`); n != 2 {
			t.Fatalf("open incidents = %d, want org A's and the instance's", n)
		}
	})
}

func TestIncidentReopenAfterFootprintShrink(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		ctx := t.Context()
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_inc_b', 'usr_itarget', 'read', 'org_b', `+ts+`)`)
		target := service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incTarget}
		inst, err := w.svc.Open(ctx, w.as(incInstance), target)
		if err != nil {
			t.Fatal(err)
		}
		if inst.Incident.OrgID != "" {
			t.Fatalf("incident org = %q, want an instance-owned incident", inst.Incident.OrgID)
		}
		// The org B footprint goes away: org A's responder now reaches the whole
		// person and gets its own incident, without taking over the instance's.
		execRaw(t, db, `DELETE FROM grants WHERE id = 'g_inc_b'`)
		own := w.openAccount(t, incResponder)
		if own.Incident.ID == inst.Incident.ID || own.Incident.OrgID != orgA || own.Reopened {
			t.Fatalf("org A open = %+v reopened=%v, want its own new incident", own.Incident, own.Reopened)
		}
		_, errRead := w.svc.Get(ctx, w.as(incResponder), inst.Incident.ID)
		_, errMissing := w.svc.Get(ctx, w.as(incResponder), "inc_does_not_exist")
		assertUniformNotFound(t, errRead, errMissing)
	})
}

// TestIncidentOrgsHaveTheirOwnSlot: an open incident in org A on a
// person who has since moved to org B does not block org B, and org B learns
// nothing about it.
func TestIncidentOrgsHaveTheirOwnSlot(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		ctx := t.Context()
		a := w.openAccount(t, incResponder)
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id = 'usr_itarget')`)
		execRaw(t, db, `DELETE FROM grants WHERE principal_id = 'usr_itarget'`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_inc_b', 'usr_itarget', 'read', 'org_b', `+ts+`)`)
		b, err := w.svc.Open(ctx, w.as(incOrgB), service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: incTarget})
		if err != nil {
			t.Fatalf("org B opening on the moved person: %v", err)
		}
		if b.Incident.ID == a.Incident.ID || b.Incident.OrgID != orgB || b.Reopened {
			t.Fatalf("org B incident = %+v reopened=%v", b.Incident, b.Reopened)
		}
		if _, err := w.svc.Get(ctx, w.as(incOrgB), a.Incident.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("org B reading org A's incident = %v, want not found", err)
		}
		if _, err := w.svc.Get(ctx, w.as(incResponder), a.Incident.ID); err != nil {
			t.Errorf("org A lost its own incident: %v", err)
		}
	})
}

// ---- the lookup budget ------------------------------------------------------

func TestIncidentEnumerationIsBoundedByTheLookupBudget(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		seedIncidentLeases(t, db)
		w.svc.MaxLookups = 1
		ctx := t.Context()
		// Two service accounts in the project: the second one's credential lookup
		// is past the budget.
		execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_it2', 'machine', 'workload', `+ts+`)`)
		execRaw(t, db, `INSERT INTO service_accounts (id, principal_id, org_id, project_id, name, kind, created_at, created_by) VALUES ('sa_it2', 'mch_it2', 'org_a', 'prj_a1', 'it-sa2', 'workload', `+ts+`, 'usr_itarget')`)

		org, err := w.svc.Open(ctx, w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgA}})
		if err != nil {
			t.Fatal(err)
		}
		if got := w.itemMap(t, incResponder, org.Incident.ID)["account.contain|enumeration|org_a"]; got != "unknown/enumeration_truncated" {
			t.Errorf("org enumeration over budget = %q, want an enumeration_truncated row", got)
		}
		prj, err := w.svc.Open(ctx, w.as(incResponder), service.IncidentTarget{Kind: domain.IncidentScopeProject, Scope: domain.Scope{Org: orgA, Project: prjA1}})
		if err != nil {
			t.Fatal(err)
		}
		if got := w.itemMap(t, incResponder, prj.Incident.ID)["machine_credential.revoke|enumeration|prj_a1"]; got != "unknown/enumeration_truncated" {
			t.Errorf("project enumeration over budget = %q, want an enumeration_truncated row", got)
		}
		// A truncated row is open: a clean close is refused until it is dealt with.
		if _, err := w.svc.Close(ctx, w.as(incResponder), org.Incident.ID, false); !errors.Is(err, service.ErrIncidentOpenItems) {
			t.Errorf("clean close of a truncated incident = %v, want ErrIncidentOpenItems", err)
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
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("a project of another org opened under org A = %v, want not found", err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incidents`) + queryInt(t, db, `SELECT COUNT(*) FROM incident_items`); n != 0 {
			t.Fatalf("refused opens left %d ledger rows", n)
		}

		// An incident is readable and workable only by who may open its scope.
		v, err := w.svc.Open(ctx, w.as(incResponder), orgTarget)
		if err != nil {
			t.Fatal(err)
		}
		item := w.itemID(t, v.Incident.ID, "usr_itarget")
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
		if got := w.itemMap(t, incResponder, v.Incident.ID)["account.contain|account|usr_itarget"]; got != "pending/" {
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
		// Tenant operations answer the uniform not found; the instance listing,
		// whose scope has no tenant to hide, answers unauthorized.
		instanceRefusal := map[string]error{"org responder, instance": domain.ErrUnauthorized, "nobody, instance": domain.ErrUnauthorized}
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
			wantErr := instanceRefusal[name]
			if wantErr == nil {
				wantErr = domain.ErrNotFound
			}
			if err := probe(); !errors.Is(err, wantErr) {
				t.Errorf("%s: listing = %v, want %v", name, err, wantErr)
			}
		}
		for name, limit := range map[string]int{"zero": 0, "negative": -1, "over the bound": authz.MaxIncidentPage + 1} {
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
		// A delegation from before the current credential epoch is dead: it is not
		// listed as live.
		execRaw(t, db, `UPDATE developer_credentials SET credential_epoch = 999 WHERE id = 'devc_a2'`)
		stale, err := w.svc.DelegationsInOrg(ctx, w.as(incResponder), orgA, "", 500)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(ids(stale), []string{"devc_a1", "devc_it"}) {
			t.Fatalf("org A delegations after an epoch move = %v, want devc_a2 gone", ids(stale))
		}

		for name, probe := range map[string]func() error{
			"nobody":        func() error { _, e := w.svc.DelegationsInOrg(ctx, w.as("usr_nobody"), orgA, "", 10); return e },
			"alice":         func() error { _, e := w.svc.DelegationsInOrg(ctx, w.as(alice), orgA, "", 10); return e },
			"limited holds": func() error { _, e := w.svc.DelegationsInOrg(ctx, w.as(incOrgB), orgA, "", 10); return e },
		} {
			if err := probe(); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("%s: listing = %v, want not found", name, err)
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
				`VALUES ('`+id+`', 'inc_ok','session.revoke', 'session', '`+object+`', 'org:org_a', '`+outcome+`', '`+reason+`', `+ts+`, `+ts+`)`)
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

// ---- adapter incidents -------------------------------------------------------

func TestIncidentAdapterScope(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		ctx := t.Context()
		adapterTarget := func(org domain.OrgID, project domain.ProjectID, id string) service.IncidentTarget {
			return service.IncidentTarget{Kind: domain.IncidentScopeAdapter, Scope: domain.Scope{Org: org, Project: project}, AdapterID: id}
		}

		// An adapter in the middle of a route move is visible on the adapter
		// record, so it is a ledger item the adapter owner resumes.
		execRaw(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_it_mv','org_a','prj_a1','forgejo','https://git.example','usr_alice','moving',`+ts+`)`)
		v, err := w.svc.Open(ctx, w.as(incResponder), adapterTarget(orgA, prjA1, "adp_it_mv"))
		if err != nil {
			t.Fatal(err)
		}
		got := w.itemMap(t, incResponder, v.Incident.ID)
		wantItems(t, got, map[string]string{
			"adapter_move.resume|adapter|adp_it_mv":                "pending/",
			"adapter_credential.upstream_revoke|adapter|adp_it_mv": "unknown/upstream_not_observable",
			// Target-level moves and scrub jobs are not read yet (the S4 reader), so
			// every adapter carries an open row that says so.
			"adapter_move.resume|enumeration|adp_it_mv": "unknown/enumeration_truncated",
		})
		if _, err := w.svc.Close(ctx, w.as(incResponder), v.Incident.ID, false); !errors.Is(err, service.ErrIncidentOpenItems) {
			t.Errorf("clean close with the move and scrub source unread = %v, want ErrIncidentOpenItems", err)
		}

		// A credential-reset holder without the adapter capability records an
		// unverified adapter id. The open-incident slot is qualified by the project
		// the caller proved is its own, so it cannot occupy another org's adapter.
		squat, err := w.svc.Open(ctx, w.as(incLimited), adapterTarget(orgA, prjA1, "adp_victim"))
		if err != nil {
			t.Fatal(err)
		}
		own, err := w.svc.Open(ctx, w.as(incOrgB), adapterTarget(orgB, prjB1, "adp_victim"))
		if err != nil {
			t.Fatalf("org B opening its own adapter incident after org A recorded the same id: %v", err)
		}
		if own.Incident.ID == squat.Incident.ID || own.Incident.OrgID != orgB || squat.Incident.OrgID != orgA {
			t.Fatalf("incidents = %+v and %+v, want two incidents, one per org", squat.Incident, own.Incident)
		}
		if _, err := w.svc.Get(ctx, w.as(incOrgB), squat.Incident.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("org B reading org A's adapter incident = %v, want not found", err)
		}
	})
}
